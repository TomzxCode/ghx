package cmd

import (
	"encoding/json"
	"time"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/telemetry"
)

// repoJSONAll is the sentinel set when --json is given without a field list, so
// a bare `repo list --json` emits every supported field.
const repoJSONAll = "all"

// repoHitRate is the JSON form of one retrieval operation's cache hit rate.
type repoHitRate struct {
	Hits    int `json:"hits"`
	Misses  int `json:"misses"`
	Total   int `json:"total"`
	Percent int `json:"percent"`
}

// repoJSONRow is everything one cached repository's JSON fields read, so every
// field function takes the same input. Now is fixed per invocation so every row
// reports the same age and freshness.
type repoJSONRow struct {
	Repo  cache.CachedRepo
	Rates map[string]telemetry.CacheHitRate
	Now   time.Time
}

// repoJSONFields maps a JSON field name to its value on a cached repository.
// Only fields ghx can populate are registered; anything else is rejected so
// typos fail loudly instead of silently emitting nulls.
var repoJSONFields = map[string]func(repoJSONRow) any{
	"cacheAge": func(row repoJSONRow) any {
		if row.Repo.Info == nil {
			return nil
		}
		return formatDuration(row.Now.Sub(row.Repo.Info.CachedAt))
	},
	"cacheDurationMinutes": func(row repoJSONRow) any {
		if row.Repo.Info == nil {
			return nil
		}
		return row.Repo.Info.Duration
	},
	"cachedAt": func(row repoJSONRow) any {
		if row.Repo.Info == nil {
			return nil
		}
		return row.Repo.Info.CachedAt
	},
	"hitRates": func(row repoJSONRow) any {
		return repoHitRates(row.Rates, row.Repo.Host, row.Repo.Owner, row.Repo.Repo)
	},
	"host":       func(row repoJSONRow) any { return row.Repo.Host },
	"issueCount": func(row repoJSONRow) any { return row.Repo.IssueCount },
	"name":       func(row repoJSONRow) any { return row.Repo.Repo },
	"nameWithOwner": func(row repoJSONRow) any {
		return repoLabel(row.Repo.Host, row.Repo.Owner, row.Repo.Repo)
	},
	"owner":   func(row repoJSONRow) any { return row.Repo.Owner },
	"prCount": func(row repoJSONRow) any { return row.Repo.PRCount },
	"status":  func(row repoJSONRow) any { return repoStatus(row.Repo.Info, row.Now) },
}

// repoHitRateOperations lists the JSON key, table column, and telemetry
// operation of every hit rate `repo list` reports. One list drives both the
// table and the JSON output so the two cannot drift.
var repoHitRateOperations = []struct {
	field     string
	column    string
	operation string
}{
	{"issueList", "ISSUE-LIST", telemetry.RetrievalIssueList},
	{"issueView", "ISSUE-VIEW", telemetry.RetrievalIssueView},
	{"prList", "PR-LIST", telemetry.RetrievalPRList},
	{"prView", "PR-VIEW", telemetry.RetrievalPRView},
}

// resolveRepoJSONFields validates a comma-separated field list and returns the
// field names to emit (deduplicated). An empty list, or the repoJSONAll
// sentinel, selects every supported field.
func resolveRepoJSONFields(raw string) ([]string, error) {
	return resolveJSONFieldList(raw, repoJSONFields, repoJSONAll)
}

// resolveRepoListJSONFields resolves the --json flag value for `repo list`.
// pflag leaves the token after a value-less flag as a positional argument, so a
// bare `--json` still selects every field while `--json field1,field2` (space
// form) is read from args.
func resolveRepoListJSONFields(args []string) ([]string, error) {
	raw := repoListJSON
	if raw == repoJSONAll && len(args) > 0 {
		raw = args[0]
	}
	if raw == "" {
		return nil, nil
	}
	return resolveRepoJSONFields(raw)
}

// repoHitRates builds the per-operation hit rates for one repository. An
// operation with no recorded retrievals is omitted, matching the dash the table
// prints for it.
func repoHitRates(rates map[string]telemetry.CacheHitRate, host, owner, repo string) map[string]repoHitRate {
	out := make(map[string]repoHitRate, len(repoHitRateOperations))
	for _, op := range repoHitRateOperations {
		r, ok := rates[hitRateKey(host, owner+"/"+repo, op.operation)]
		if !ok || r.Hits+r.Misses == 0 {
			continue
		}
		out[op.field] = repoHitRate{
			Hits:    r.Hits,
			Misses:  r.Misses,
			Total:   r.Hits + r.Misses,
			Percent: hitRatePercent(r.Hits, r.Misses),
		}
	}
	return out
}

// printRepoListJSON renders selected fields for each cached repository as a
// compact JSON array. Keys are alphabetically ordered by encoding/json.
func printRepoListJSON(cmd *cobra.Command, repos []cache.CachedRepo, fields []string) error {
	// Hit rates cost a telemetry read, so read them only when requested.
	var rates map[string]telemetry.CacheHitRate
	for _, field := range fields {
		if field == "hitRates" {
			rates = retrievalHitRates()
			break
		}
	}

	now := time.Now()
	out := make([]map[string]any, 0, len(repos))
	for _, repo := range repos {
		row := repoJSONRow{Repo: repo, Rates: rates, Now: now}
		obj := make(map[string]any, len(fields))
		for _, field := range fields {
			obj[field] = repoJSONFields[field](row)
		}
		out = append(out, obj)
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	return enc.Encode(out)
}
