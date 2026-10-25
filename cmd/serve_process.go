package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/processflow"
	"github.com/tomzxcode/ghx/internal/processreport"
	"github.com/tomzxcode/ghx/internal/webui"
)

// serveProcessFunc adapts the cached issues and pull requests into the
// process report HTML. Like the stats renderer it never falls back to the
// GitHub API: the UI is read-only against the cache, so run `ghx cache` (or
// use the refresh action) first for complete data.
func serveProcessFunc(store cache.Store) webui.ProcessFunc {
	return func(q webui.ProcessQuery) (string, error) {
		coords, err := resolveProcessCoords(store, q.Repos)
		if err != nil {
			return "", err
		}
		if len(coords) == 0 {
			return "", fmt.Errorf("no cached repositories: run `ghx cache` first")
		}
		var items []processflow.Item
		labels := make([]string, 0, len(coords))
		for _, c := range coords {
			issues, err := store.LoadAllIssues(c[0], c[1], c[2])
			if err != nil {
				return "", err
			}
			prs, err := store.LoadAllPRs(c[0], c[1], c[2])
			if err != nil {
				return "", err
			}
			items = append(items, processflow.FromIssues(issues)...)
			items = append(items, processflow.FromPRs(prs)...)
			labels = append(labels, c[1]+"/"+c[2])
		}
		report := processflow.Build(items, time.Now().UTC(), q.Days)
		return processreport.Render(&report, processreport.Meta{Repos: labels})
	}
}

// resolveProcessCoords resolves requested "host/owner/repo" names against the
// cache; an empty list selects every cached repository. An explicitly named
// repository that is not cached is an error, matching the stats renderer.
func resolveProcessCoords(store cache.Store, names []string) ([][3]string, error) {
	cached, err := store.ListCachedRepos()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		coords := make([][3]string, 0, len(cached))
		for _, cr := range cached {
			coords = append(coords, [3]string{cr.Host, cr.Owner, cr.Repo})
		}
		return coords, nil
	}
	coords := make([][3]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		repo, err := gitremote.ParseRepo(name)
		if err != nil {
			return nil, err
		}
		key := strings.ToLower(repo.Host + "/" + repo.Owner + "/" + repo.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		found := false
		for _, cr := range cached {
			if strings.EqualFold(cr.Host, repo.Host) && strings.EqualFold(cr.Owner, repo.Owner) && strings.EqualFold(cr.Repo, repo.Name) {
				coords = append(coords, [3]string{cr.Host, cr.Owner, cr.Repo})
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("repository %s is not cached", repo.Host+"/"+repo.Owner+"/"+repo.Name)
		}
	}
	return coords, nil
}
