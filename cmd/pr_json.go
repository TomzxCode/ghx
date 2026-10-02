package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tomzxcode/ghx/internal/github"
)

// prJSONAll is the sentinel set when --json is given without a field list. It
// keeps the legacy `ghx pr list --json` form working alongside the gh-compatible
// `--json field1,field2` form (gh requires the field list; ghx treats a bare
// --json as "every field").
const prJSONAll = "all"

// prJSONFields maps a gh-compatible JSON field name to its value on a pull
// request. Only fields ghx can populate are registered; anything else is
// rejected so typos fail loudly instead of silently emitting nulls.
var prJSONFields = map[string]func(*github.PullRequest) any{
	"additions":      func(pr *github.PullRequest) any { return pr.Additions },
	"assignees":      func(pr *github.PullRequest) any { return pr.Assignees },
	"author":         func(pr *github.PullRequest) any { return pr.Author },
	"baseRefName":    func(pr *github.PullRequest) any { return pr.BaseRefName },
	"body":           func(pr *github.PullRequest) any { return pr.Body },
	"closedAt":       func(pr *github.PullRequest) any { return pr.ClosedAt },
	"comments":       func(pr *github.PullRequest) any { return prCommentCount(pr) },
	"createdAt":      func(pr *github.PullRequest) any { return pr.CreatedAt },
	"deletions":      func(pr *github.PullRequest) any { return pr.Deletions },
	"headRefName":    func(pr *github.PullRequest) any { return pr.HeadRefName },
	"headRefOid":     func(pr *github.PullRequest) any { return pr.HeadRefOid },
	"isDraft":        func(pr *github.PullRequest) any { return pr.IsDraft },
	"labels":         func(pr *github.PullRequest) any { return pr.Labels },
	"mergedAt":       func(pr *github.PullRequest) any { return pr.MergedAt },
	"milestone":      func(pr *github.PullRequest) any { return pr.Milestone },
	"number":         func(pr *github.PullRequest) any { return pr.Number },
	"reviewDecision": func(pr *github.PullRequest) any { return pr.ReviewDecision },
	"reviews":        func(pr *github.PullRequest) any { return pr.Reviews },
	"state":          func(pr *github.PullRequest) any { return pr.State },
	"title":          func(pr *github.PullRequest) any { return pr.Title },
	"updatedAt":      func(pr *github.PullRequest) any { return pr.UpdatedAt },
	"url":            func(pr *github.PullRequest) any { return pr.URL },
}

func prCommentCount(pr *github.PullRequest) int {
	if pr.CommentCount > 0 {
		return pr.CommentCount
	}
	return len(pr.Comments)
}

// resolvePRJSONFields validates a comma-separated field list and returns the
// field names to emit (deduplicated). An empty list, or the prJSONAll sentinel,
// selects every supported field.
func resolvePRJSONFields(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == prJSONAll {
		return sortedPRJSONFields(), nil
	}

	var fields []string
	seen := map[string]bool{}
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := prJSONFields[name]; !ok {
			return nil, fmt.Errorf("unknown JSON field: %q\nAvailable fields:\n  %s",
				name, strings.Join(sortedPRJSONFields(), "\n  "))
		}
		if !seen[name] {
			seen[name] = true
			fields = append(fields, name)
		}
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("no JSON fields specified")
	}
	return fields, nil
}

// sortedPRJSONFields returns every supported field name in lexical order, used
// for help output and the "available fields" error listing.
func sortedPRJSONFields() []string {
	names := make([]string, 0, len(prJSONFields))
	for name := range prJSONFields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolvePRListJSONFields resolves the --json flag value for `pr list`. pflag
// leaves the token after a value-less flag as a positional argument, so a bare
// `--json` still selects every field while `--json field1,field2` (space form,
// as gh documents it) is read from args.
func resolvePRListJSONFields(args []string) ([]string, error) {
	raw := prListJSON
	if raw == prJSONAll && len(args) > 0 {
		raw = args[0]
	}
	if raw == "" {
		return nil, nil
	}
	return resolvePRJSONFields(raw)
}

// printPRListJSON renders selected fields for each pull request as a compact
// JSON array. Keys are alphabetically ordered by encoding/json, matching gh.
func printPRListJSON(prs []*github.PullRequest, fields []string) error {
	out := make([]map[string]any, 0, len(prs))
	for _, pr := range prs {
		obj := make(map[string]any, len(fields))
		for _, field := range fields {
			obj[field] = prJSONFields[field](pr)
		}
		out = append(out, obj)
	}

	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(out)
}
