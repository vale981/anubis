package web

import (
	"github.com/a-h/templ"

	"github.com/vale981/anubis/lib/policy/config"
)

func Base(title string, body templ.Component) templ.Component {
	return base(title, body, nil, nil)
}

func BaseWithChallengeAndOGTags(title string, body templ.Component, challenge string, rules *config.ChallengeRules, ogTags map[string]string) (templ.Component, error) {
	return base(title, body, struct {
		Challenge string                 `json:"challenge"`
		Rules     *config.ChallengeRules `json:"rules"`
	}{
		Challenge: challenge,
		Rules:     rules,
	}, ogTags), nil
}

func Index() templ.Component {
	return index()
}

func ErrorPage(msg string, mail string) templ.Component {
	return errorPage(msg, mail)
}

func Bench() templ.Component {
	return bench()
}
