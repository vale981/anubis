package policy

import (
	"log/slog"

	"github.com/vale981/anubis/lib/policy/config"
)

type CheckResult struct {
	Name string
	Rule config.Rule
}

func (cr CheckResult) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("name", cr.Name),
		slog.String("rule", string(cr.Rule)))
}
