package cache

import "time"

// freshnessScope selects which portion of a repository's cache a check covers.
type freshnessScope int

const (
	freshnessAll freshnessScope = iota
	freshnessIssues
	freshnessPRs
)

// checkCacheFresh loads the repository's metadata and reports whether the
// requested scope is fresh. A nil duration uses the window stored with the
// cache; a non-nil value overrides it. Only a full-scope check requires a
// completed fetch. Both store backends delegate here so they cannot disagree
// about what "fresh" means.
func checkCacheFresh(load func(host, owner, repo string) (*CacheInfo, error), host, owner, repo string, duration *int, scope freshnessScope) (bool, error) {
	info, err := load(host, owner, repo)
	if err != nil {
		return false, err
	}

	window := info.Duration
	if duration != nil {
		window = *duration
	}
	if scope == freshnessAll && !info.Complete {
		return false, nil
	}

	now := time.Now()
	d := time.Duration(window) * time.Minute
	issuesFresh := now.Sub(info.IssuesUpdatedAt()) < d
	prsFresh := now.Sub(info.PRsUpdatedAt()) < d

	switch scope {
	case freshnessIssues:
		return issuesFresh, nil
	case freshnessPRs:
		return prsFresh, nil
	default:
		return issuesFresh && prsFresh, nil
	}
}
