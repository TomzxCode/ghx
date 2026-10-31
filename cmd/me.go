package cmd

import (
	"fmt"
	"os/exec"
	"strings"
)

// meShorthand is the value --author and --assignee accept for the
// authenticated user, as in gh.
const meShorthand = "@me"

// ghConfigGetUser reads the login gh stores locally for host. It is a local
// read of gh's config, never an API call. The variable lets tests stub the
// lookup.
var ghConfigGetUser = func(host string) string {
	out, err := exec.Command("gh", "config", "get", "-h", host, "user").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// meResolver resolves the "@me" shorthand to a login, looking it up at most
// once per host.
type meResolver struct {
	logins map[string]string
}

// resolve returns value unchanged unless it is "@me", in which case it returns
// the login gh has stored for host. gh has no stored user when authentication
// comes from GH_TOKEN or GITHUB_TOKEN alone, which is reported as an error
// rather than silently matching nothing.
func (m *meResolver) resolve(value, host string) (string, error) {
	if value != meShorthand {
		return value, nil
	}
	if m.logins == nil {
		m.logins = map[string]string{}
	}
	login, seen := m.logins[host]
	if !seen {
		login = ghConfigGetUser(host)
		m.logins[host] = login
	}
	if login == "" {
		return "", fmt.Errorf(`cannot resolve "@me" for %s: gh has no stored user; run "gh auth login" or pass a login`, host)
	}
	return login, nil
}
