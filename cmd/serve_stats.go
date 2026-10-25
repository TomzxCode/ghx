package cmd

import (
	"fmt"
	"strings"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/logging"
	"github.com/tomzxcode/ghx/internal/webui"
)

// serveStatsFunc adapts the cached pull requests into the stats report HTML.
// Unlike the stats command, it never falls back to the GitHub API: the UI is
// read-only against the cache, so run `ghx cache` (or use the refresh action)
// first for complete data. Reports render with the interactive period form,
// which reloads /stats with new from/to/state values.
func serveStatsFunc(store cache.Store) webui.StatsFunc {
	return func(q webui.StatsQuery) (string, error) {
		from, err := parseStatsTime(q.From, false)
		if err != nil {
			return "", err
		}
		to, err := parseStatsTime(q.To, true)
		if err != nil {
			return "", err
		}
		if !from.IsZero() && !to.IsZero() && to.Before(from) {
			return "", fmt.Errorf("to (%s) is before from (%s)", q.To, q.From)
		}
		repos, repoPRs, err := loadStatsRepoPRs(store, q.Repos)
		if err != nil {
			return "", err
		}
		if len(repos) == 0 {
			return "", fmt.Errorf("no cached repositories: run `ghx cache` first")
		}
		report := buildReport(repos, repoPRs, statsFilters{
			Authors:     toLowerSet(q.Authors),
			Reviewers:   toLowerSet(q.Reviewers),
			From:        from,
			To:          to,
			State:       q.State,
			IncludeBots: q.IncludeBots,
			ListPRs:     q.ListPRs,
			Top:         q.Top,
		})
		report.Interactive = true
		report.AllRepos = len(q.Repos) == 0
		return renderReport(report)
	}
}

// requireCachedRepos verifies every requested repository has something in the
// cache. Only-viewed repositories (items without cache metadata) count, like
// the webui hasRepo check.
func requireCachedRepos(store cache.Store, coords [][3]string) error {
	cached, err := store.ListCachedRepos()
	if err != nil {
		return err
	}
	for _, c := range coords {
		found := false
		for _, cr := range cached {
			if strings.EqualFold(cr.Host, c[0]) && strings.EqualFold(cr.Owner, c[1]) && strings.EqualFold(cr.Repo, c[2]) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("repository %s/%s/%s is not cached", c[0], c[1], c[2])
		}
	}
	return nil
}

// loadStatsRepoPRs resolves the requested repositories (every cached one when
// names is empty) and loads their cached pull requests. Explicit names must be
// cached, matching the other repository-scoped endpoints.
func loadStatsRepoPRs(store cache.Store, names []string) ([]*gitremote.Repo, []*repoPR, error) {
	coords := make([][3]string, 0, len(names))
	if len(names) == 0 {
		cached, err := store.ListCachedRepos()
		if err != nil {
			return nil, nil, err
		}
		for _, cr := range cached {
			coords = append(coords, [3]string{cr.Host, cr.Owner, cr.Repo})
		}
	} else {
		// ParseRepo normalizes the coordinates, so a request that differs only
		// by casing targets the same cache entry as the CLI.
		seen := make(map[string]bool, len(names))
		for _, name := range names {
			parts := strings.Split(name, "/")
			if len(parts) != 3 {
				return nil, nil, fmt.Errorf("invalid repository %q: expected host/owner/repo", name)
			}
			repo, err := gitremote.ParseRepo(name)
			if err != nil {
				return nil, nil, err
			}
			key := repo.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			coords = append(coords, [3]string{repo.Host, repo.Owner, repo.Name})
		}
		if err := requireCachedRepos(store, coords); err != nil {
			return nil, nil, err
		}
	}

	repos := make([]*gitremote.Repo, 0, len(coords))
	var prs []*repoPR
	for _, c := range coords {
		repo, err := gitremote.ParseRepo(c[0] + "/" + c[1] + "/" + c[2])
		if err != nil {
			return nil, nil, err
		}
		cached, err := store.LoadAllPRs(repo.Host, repo.Owner, repo.Name)
		if err != nil {
			return nil, nil, err
		}
		repos = append(repos, repo)
		for _, pr := range cached {
			prs = append(prs, &repoPR{Repo: repo.Owner + "/" + repo.Name, PR: pr})
		}
		logging.Debug("stats: loaded pull requests", "count", len(cached), "repo", repo.Owner+"/"+repo.Name)
	}
	return repos, prs, nil
}
