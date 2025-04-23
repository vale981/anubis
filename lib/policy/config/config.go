package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"regexp"
	"strings"

	"github.com/vale981/anubis/data"
	"k8s.io/apimachinery/pkg/util/yaml"
)

var (
	ErrNoBotRulesDefined                 = errors.New("config: must define at least one (1) bot rule")
	ErrBotMustHaveName                   = errors.New("config.Bot: must set name")
	ErrBotMustHaveUserAgentOrPath        = errors.New("config.Bot: must set either user_agent_regex, path_regex, headers_regex, or remote_addresses")
	ErrBotMustHaveUserAgentOrPathNotBoth = errors.New("config.Bot: must set either user_agent_regex, path_regex, and not both")
	ErrUnknownAction                     = errors.New("config.Bot: unknown action")
	ErrInvalidUserAgentRegex             = errors.New("config.Bot: invalid user agent regex")
	ErrInvalidPathRegex                  = errors.New("config.Bot: invalid path regex")
	ErrInvalidHeadersRegex               = errors.New("config.Bot: invalid headers regex")
	ErrInvalidCIDR                       = errors.New("config.Bot: invalid CIDR")
	ErrInvalidImportStatement            = errors.New("config.ImportStatement: invalid source file")
	ErrCantSetBotAndImportValuesAtOnce   = errors.New("config.BotOrImport: can't set bot rules and import values at the same time")
	ErrMustSetBotOrImportRules           = errors.New("config.BotOrImport: rule definition is invalid, you must set either bot rules or an import statement, not both")
)

type Rule string

const (
	RuleUnknown   Rule = ""
	RuleAllow     Rule = "ALLOW"
	RuleDeny      Rule = "DENY"
	RuleChallenge Rule = "CHALLENGE"
	RuleBenchmark Rule = "DEBUG_BENCHMARK"
)

type Algorithm string

const (
	AlgorithmUnknown Algorithm = ""
	AlgorithmFast    Algorithm = "fast"
	AlgorithmSlow    Algorithm = "slow"
)

type BotConfig struct {
	Name           string            `json:"name"`
	UserAgentRegex *string           `json:"user_agent_regex"`
	PathRegex      *string           `json:"path_regex"`
	HeadersRegex   map[string]string `json:"headers_regex"`
	Action         Rule              `json:"action"`
	RemoteAddr     []string          `json:"remote_addresses"`
	Challenge      *ChallengeRules   `json:"challenge,omitempty"`
}

func (b BotConfig) Zero() bool {
	for _, cond := range []bool{
		b.Name != "",
		b.UserAgentRegex != nil,
		b.PathRegex != nil,
		len(b.HeadersRegex) != 0,
		b.Action != "",
		len(b.RemoteAddr) != 0,
		b.Challenge != nil,
	} {
		if cond {
			return false
		}
	}

	return true
}

func (b BotConfig) Valid() error {
	var errs []error

	if b.Name == "" {
		errs = append(errs, ErrBotMustHaveName)
	}

	if b.UserAgentRegex == nil && b.PathRegex == nil && len(b.RemoteAddr) == 0 && len(b.HeadersRegex) == 0 {
		errs = append(errs, ErrBotMustHaveUserAgentOrPath)
	}

	if b.UserAgentRegex != nil && b.PathRegex != nil {
		errs = append(errs, ErrBotMustHaveUserAgentOrPathNotBoth)
	}

	if b.UserAgentRegex != nil {
		if _, err := regexp.Compile(*b.UserAgentRegex); err != nil {
			errs = append(errs, ErrInvalidUserAgentRegex, err)
		}
	}

	if b.PathRegex != nil {
		if _, err := regexp.Compile(*b.PathRegex); err != nil {
			errs = append(errs, ErrInvalidPathRegex, err)
		}
	}

	if len(b.HeadersRegex) > 0 {
		for name, expr := range b.HeadersRegex {
			if name == "" {
				continue
			}

			if _, err := regexp.Compile(expr); err != nil {
				errs = append(errs, ErrInvalidHeadersRegex, err)
			}
		}
	}

	if len(b.RemoteAddr) > 0 {
		for _, cidr := range b.RemoteAddr {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				errs = append(errs, ErrInvalidCIDR, err)
			}
		}
	}

	switch b.Action {
	case RuleAllow, RuleBenchmark, RuleChallenge, RuleDeny:
		// okay
	default:
		errs = append(errs, fmt.Errorf("%w: %q", ErrUnknownAction, b.Action))
	}

	if b.Action == RuleChallenge && b.Challenge != nil {
		if err := b.Challenge.Valid(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("config: bot entry for %q is not valid:\n%w", b.Name, errors.Join(errs...))
	}

	return nil
}

type ChallengeRules struct {
	Difficulty int       `json:"difficulty"`
	ReportAs   int       `json:"report_as"`
	Algorithm  Algorithm `json:"algorithm"`
}

var (
	ErrChallengeRuleHasWrongAlgorithm = errors.New("config.Bot.ChallengeRules: algorithm is invalid")
	ErrChallengeDifficultyTooLow      = errors.New("config.Bot.ChallengeRules: difficulty is too low (must be >= 1)")
	ErrChallengeDifficultyTooHigh     = errors.New("config.Bot.ChallengeRules: difficulty is too high (must be <= 64)")
)

func (cr ChallengeRules) Valid() error {
	var errs []error

	if cr.Difficulty < 1 {
		errs = append(errs, fmt.Errorf("%w, got: %d", ErrChallengeDifficultyTooLow, cr.Difficulty))
	}

	if cr.Difficulty > 64 {
		errs = append(errs, fmt.Errorf("%w, got: %d", ErrChallengeDifficultyTooHigh, cr.Difficulty))
	}

	switch cr.Algorithm {
	case AlgorithmFast, AlgorithmSlow, AlgorithmUnknown:
		// do nothing, it's all good
	default:
		errs = append(errs, fmt.Errorf("%w: %q", ErrChallengeRuleHasWrongAlgorithm, cr.Algorithm))
	}

	if len(errs) != 0 {
		return fmt.Errorf("config: challenge rules entry is not valid:\n%w", errors.Join(errs...))
	}

	return nil
}

type ImportStatement struct {
	Import string `json:"import"`
	Bots   []BotConfig
}

func (is *ImportStatement) open() (fs.File, error) {
	if strings.HasPrefix(is.Import, "(data)/") {
		fname := strings.TrimPrefix(is.Import, "(data)/")
		fin, err := data.BotPolicies.Open(fname)
		return fin, err
	}

	return os.Open(is.Import)
}

func (is *ImportStatement) load() error {
	fin, err := is.open()
	if err != nil {
		return fmt.Errorf("can't open %s: %w", is.Import, err)
	}
	defer fin.Close()

	var result []BotConfig

	if err := yaml.NewYAMLToJSONDecoder(fin).Decode(&result); err != nil {
		return fmt.Errorf("can't parse %s: %w", is.Import, err)
	}

	var errs []error

	for _, b := range result {
		if err := b.Valid(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("config %s is not valid:\n%w", is.Import, errors.Join(errs...))
	}

	is.Bots = result

	return nil
}

func (is *ImportStatement) Valid() error {
	return is.load()
}

type BotOrImport struct {
	*BotConfig       `json:",inline"`
	*ImportStatement `json:",inline"`
}

func (boi *BotOrImport) Valid() error {
	if boi.BotConfig != nil && boi.ImportStatement != nil {
		return ErrCantSetBotAndImportValuesAtOnce
	}

	if boi.BotConfig != nil {
		return boi.BotConfig.Valid()
	}

	if boi.ImportStatement != nil {
		return boi.ImportStatement.Valid()
	}

	return ErrMustSetBotOrImportRules
}

type fileConfig struct {
	Bots  []BotOrImport `json:"bots"`
	DNSBL bool          `json:"dnsbl"`
}

func (c fileConfig) Valid() error {
	var errs []error

	if len(c.Bots) == 0 {
		errs = append(errs, ErrNoBotRulesDefined)
	}

	for _, b := range c.Bots {
		if err := b.Valid(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("config is not valid:\n%w", errors.Join(errs...))
	}

	return nil
}

func Load(fin io.Reader, fname string) (*Config, error) {
	var c fileConfig
	if err := yaml.NewYAMLToJSONDecoder(fin).Decode(&c); err != nil {
		return nil, fmt.Errorf("can't parse policy config YAML %s: %w", fname, err)
	}

	if err := c.Valid(); err != nil {
		return nil, err
	}

	result := &Config{
		DNSBL: c.DNSBL,
	}

	var validationErrs []error

	for _, boi := range c.Bots {
		if boi.ImportStatement != nil {
			if err := boi.load(); err != nil {
				validationErrs = append(validationErrs, err)
				continue
			}

			result.Bots = append(result.Bots, boi.ImportStatement.Bots...)
		}

		if boi.BotConfig != nil {
			if err := boi.BotConfig.Valid(); err != nil {
				validationErrs = append(validationErrs, err)
				continue
			}

			result.Bots = append(result.Bots, *boi.BotConfig)
		}
	}

	if len(validationErrs) > 0 {
		return nil, fmt.Errorf("errors validating policy config %s: %w", fname, errors.Join(validationErrs...))
	}

	return result, nil
}

type Config struct {
	Bots  []BotConfig
	DNSBL bool
}

func (c Config) Valid() error {
	var errs []error

	if len(c.Bots) == 0 {
		errs = append(errs, ErrNoBotRulesDefined)
	}

	for _, b := range c.Bots {
		if err := b.Valid(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return fmt.Errorf("config is not valid:\n%w", errors.Join(errs...))
	}

	return nil
}
