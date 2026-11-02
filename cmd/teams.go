package cmd

import (
	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
)

// teamResolver loads the cached team memberships for a host at most once, so a
// search spanning several repositories on one host reads them once. The teams
// are recorded by "ghx cache"; search itself never calls the GitHub API.
type teamResolver struct {
	store  cache.Store
	byHost map[string][]github.ViewerTeam
}

// teams returns the team memberships cached for host. A host with no recorded
// teams yields an empty list, so "@me" still matches the user alone.
func (r *teamResolver) teams(host string) ([]github.ViewerTeam, error) {
	if r == nil || r.store == nil {
		return nil, nil
	}
	if r.byHost == nil {
		r.byHost = map[string][]github.ViewerTeam{}
	}
	teams, seen := r.byHost[host]
	if !seen {
		var err error
		teams, err = r.store.LoadViewerTeams(host)
		if err != nil {
			return nil, err
		}
		r.byHost[host] = teams
	}
	return teams, nil
}
