package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"text/template"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/logging"
)

// ---------------------------------------------------------------------------
// Command tree
// ---------------------------------------------------------------------------

var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search cached issues and pull requests",
	Long: `Searches the issues and pull requests already in the local cache.

The search never calls the GitHub API: run "ghx cache", or any list or view
command, to fill the cache first. Every cached repository is searched by
default; pass --repo to limit the search to one repository.

Query words are matched case-insensitively against the title and body, and
every word must match. Quote a phrase ("memory leak") to require it as written.
Use --match to include comments or restrict the match to one field.
With no query words, the command lists every cached item that matches the
filters.

Filters that GitHub evaluates server-side but the cache cannot answer
(reactions, interactions, locked, projects, repository archived/visibility/
language, semantic search, --jq, --web) are not supported.`,
}

// search issues flags
var (
	searchIssuesApp          string
	searchIssuesAssignee     string
	searchIssuesAuthor       string
	searchIssuesClosed       string
	searchIssuesCommenter    string
	searchIssuesComments     string
	searchIssuesCreated      string
	searchIssuesIncludePRs   bool
	searchIssuesInvolves     string
	searchIssuesLabels       []string
	searchIssuesLimit        int
	searchIssuesMatch        []string
	searchIssuesMentions     string
	searchIssuesMilestone    string
	searchIssuesNoAssignee   bool
	searchIssuesNoLabel      bool
	searchIssuesNoMilestone  bool
	searchIssuesOrder        string
	searchIssuesOwner        []string
	searchIssuesSort         string
	searchIssuesState        string
	searchIssuesTeamMentions string
	searchIssuesUpdated      string
	searchIssuesJSON         string
	searchIssuesTemplate     string
	searchIssuesNoTruncate   bool
)

// search prs flags
var (
	searchPRsApp             string
	searchPRsAssignee        string
	searchPRsAuthor          string
	searchPRsBase            string
	searchPRsChecks          string
	searchPRsClosed          string
	searchPRsCommenter       string
	searchPRsComments        string
	searchPRsCreated         string
	searchPRsDraft           bool
	searchPRsHead            string
	searchPRsInvolves        string
	searchPRsLabels          []string
	searchPRsLimit           int
	searchPRsMatch           []string
	searchPRsMentions        string
	searchPRsMerged          bool
	searchPRsMergedAt        string
	searchPRsMilestone       string
	searchPRsNoAssignee      bool
	searchPRsNoLabel         bool
	searchPRsNoMilestone     bool
	searchPRsOrder           string
	searchPRsOwner           []string
	searchPRsReview          string
	searchPRsReviewRequested string
	searchPRsReviewedBy      string
	searchPRsSort            string
	searchPRsState           string
	searchPRsTeamMentions    string
	searchPRsUpdated         string
	searchPRsJSON            string
	searchPRsTemplate        string
	searchPRsNoTruncate      bool
)

// searchJSONAll is the sentinel for a bare --json: every supported field.
const searchJSONAll = "all"

var searchIssuesCmd = &cobra.Command{
	Use:   "issues [<query>...]",
	Short: "Search cached issues",
	RunE:  runSearchIssues,
}

var searchPRsCmd = &cobra.Command{
	Use:   "prs [<query>...]",
	Short: "Search cached pull requests",
	RunE:  runSearchPRs,
}

func init() {
	searchCmd.AddCommand(searchIssuesCmd)
	searchCmd.AddCommand(searchPRsCmd)

	searchIssuesCmd.Flags().StringVar(&searchIssuesApp, "app", "", "Filter by GitHub App author")
	searchIssuesCmd.Flags().StringVarP(&searchIssuesAssignee, "assignee", "a", "", "Filter by assignee (login or @me)")
	searchIssuesCmd.Flags().StringVarP(&searchIssuesAuthor, "author", "A", "", "Filter by author (login or @me)")
	searchIssuesCmd.Flags().StringVar(&searchIssuesClosed, "closed", "", "Filter on closed date (YYYY-MM-DD, >DATE, >=DATE, <DATE, <=DATE, or START..END)")
	searchIssuesCmd.Flags().StringVar(&searchIssuesCommenter, "commenter", "", "Filter based on comments by user")
	searchIssuesCmd.Flags().StringVar(&searchIssuesComments, "comments", "", "Filter on number of comments (N, >N, >=N, <N, <=N, or START..END)")
	searchIssuesCmd.Flags().StringVar(&searchIssuesCreated, "created", "", "Filter on created date (YYYY-MM-DD, >DATE, >=DATE, <DATE, <=DATE, or START..END)")
	searchIssuesCmd.Flags().BoolVar(&searchIssuesIncludePRs, "include-prs", false, "Include cached pull requests in results")
	searchIssuesCmd.Flags().StringVar(&searchIssuesInvolves, "involves", "", "Filter based on involvement of user")
	searchIssuesCmd.Flags().StringSliceVarP(&searchIssuesLabels, "label", "l", nil, "Filter by label (repeat for AND logic)")
	searchIssuesCmd.Flags().IntVarP(&searchIssuesLimit, "limit", "L", 30, "Maximum number of results (0 for no limit)")
	searchIssuesCmd.Flags().StringSliceVar(&searchIssuesMatch, "match", nil, "Restrict the query to fields: title, body, or comments (default all three)")
	searchIssuesCmd.Flags().StringVar(&searchIssuesMentions, "mentions", "", "Filter based on user mentions (login or @me)")
	searchIssuesCmd.Flags().StringVarP(&searchIssuesMilestone, "milestone", "m", "", "Filter by milestone number or title")
	searchIssuesCmd.Flags().BoolVar(&searchIssuesNoAssignee, "no-assignee", false, "Filter on missing assignee")
	searchIssuesCmd.Flags().BoolVar(&searchIssuesNoLabel, "no-label", false, "Filter on missing label")
	searchIssuesCmd.Flags().BoolVar(&searchIssuesNoMilestone, "no-milestone", false, "Filter on missing milestone")
	searchIssuesCmd.Flags().StringVar(&searchIssuesOrder, "order", "desc", "Order of results: asc or desc")
	searchIssuesCmd.Flags().StringSliceVar(&searchIssuesOwner, "owner", nil, "Filter on repository owner")
	searchIssuesCmd.Flags().StringVar(&searchIssuesSort, "sort", "", "Sort results: updated, created, or comments (default updated)")
	searchIssuesCmd.Flags().StringVarP(&searchIssuesState, "state", "s", "all", "Filter by state: {open|closed|all}")
	searchIssuesCmd.Flags().StringVar(&searchIssuesTeamMentions, "team-mentions", "", "Filter based on team mentions (org/team)")
	searchIssuesCmd.Flags().StringVar(&searchIssuesUpdated, "updated", "", "Filter on last updated date (YYYY-MM-DD, >DATE, >=DATE, <DATE, <=DATE, or START..END)")
	searchIssuesCmd.Flags().StringVar(&searchIssuesJSON, "json", "", "Output JSON with the specified fields (use --json=field1,field2; omit for all)")
	searchIssuesCmd.Flags().Lookup("json").NoOptDefVal = searchJSONAll
	searchIssuesCmd.Flags().StringVarP(&searchIssuesTemplate, "template", "t", "", "Format each result with a Go template over the JSON fields")
	searchIssuesCmd.Flags().BoolVar(&searchIssuesNoTruncate, "no-truncate", false, "Don't truncate long titles")

	searchPRsCmd.Flags().StringVar(&searchPRsApp, "app", "", "Filter by GitHub App author")
	searchPRsCmd.Flags().StringVarP(&searchPRsAssignee, "assignee", "a", "", "Filter by assignee (login or @me)")
	searchPRsCmd.Flags().StringVarP(&searchPRsAuthor, "author", "A", "", "Filter by author (login or @me)")
	searchPRsCmd.Flags().StringVarP(&searchPRsBase, "base", "B", "", "Filter by base branch")
	searchPRsCmd.Flags().StringVar(&searchPRsChecks, "checks", "", "Filter by check status: {pending|success|failure}")
	searchPRsCmd.Flags().StringVar(&searchPRsClosed, "closed", "", "Filter on closed date (YYYY-MM-DD, >DATE, >=DATE, <DATE, <=DATE, or START..END)")
	searchPRsCmd.Flags().StringVar(&searchPRsCommenter, "commenter", "", "Filter based on comments by user")
	searchPRsCmd.Flags().StringVar(&searchPRsComments, "comments", "", "Filter on number of comments (N, >N, >=N, <N, <=N, or START..END)")
	searchPRsCmd.Flags().StringVar(&searchPRsCreated, "created", "", "Filter on created date (YYYY-MM-DD, >DATE, >=DATE, <DATE, <=DATE, or START..END)")
	searchPRsCmd.Flags().BoolVarP(&searchPRsDraft, "draft", "d", false, "Filter by draft state")
	searchPRsCmd.Flags().StringVarP(&searchPRsHead, "head", "H", "", "Filter by head branch")
	searchPRsCmd.Flags().StringVar(&searchPRsInvolves, "involves", "", "Filter based on involvement of user")
	searchPRsCmd.Flags().StringSliceVarP(&searchPRsLabels, "label", "l", nil, "Filter by label (repeat for AND logic)")
	searchPRsCmd.Flags().IntVarP(&searchPRsLimit, "limit", "L", 30, "Maximum number of results (0 for no limit)")
	searchPRsCmd.Flags().StringSliceVar(&searchPRsMatch, "match", nil, "Restrict the query to fields: title, body, or comments (default all three)")
	searchPRsCmd.Flags().StringVar(&searchPRsMentions, "mentions", "", "Filter based on user mentions (login or @me)")
	searchPRsCmd.Flags().BoolVar(&searchPRsMerged, "merged", false, "Filter on merged state")
	searchPRsCmd.Flags().StringVar(&searchPRsMergedAt, "merged-at", "", "Filter on merged date (YYYY-MM-DD, >DATE, >=DATE, <DATE, <=DATE, or START..END)")
	searchPRsCmd.Flags().StringVarP(&searchPRsMilestone, "milestone", "m", "", "Filter by milestone number or title")
	searchPRsCmd.Flags().BoolVar(&searchPRsNoAssignee, "no-assignee", false, "Filter on missing assignee")
	searchPRsCmd.Flags().BoolVar(&searchPRsNoLabel, "no-label", false, "Filter on missing label")
	searchPRsCmd.Flags().BoolVar(&searchPRsNoMilestone, "no-milestone", false, "Filter on missing milestone")
	searchPRsCmd.Flags().StringVar(&searchPRsOrder, "order", "desc", "Order of results: asc or desc")
	searchPRsCmd.Flags().StringSliceVar(&searchPRsOwner, "owner", nil, "Filter on repository owner")
	searchPRsCmd.Flags().StringVar(&searchPRsReview, "review", "", "Filter by review status: {none|required|approved|changes_requested}")
	searchPRsCmd.Flags().StringVar(&searchPRsReviewRequested, "review-requested", "", "Filter on user or team requested to review (login, org/team, or @me)")
	searchPRsCmd.Flags().StringVar(&searchPRsReviewedBy, "reviewed-by", "", "Filter on user who reviewed (login or @me)")
	searchPRsCmd.Flags().StringVar(&searchPRsSort, "sort", "", "Sort results: updated, created, or comments (default updated)")
	searchPRsCmd.Flags().StringVarP(&searchPRsState, "state", "s", "all", "Filter by state: {open|closed|merged|all}")
	searchPRsCmd.Flags().StringVar(&searchPRsTeamMentions, "team-mentions", "", "Filter based on team mentions (org/team)")
	searchPRsCmd.Flags().StringVar(&searchPRsUpdated, "updated", "", "Filter on last updated date (YYYY-MM-DD, >DATE, >=DATE, <DATE, <=DATE, or START..END)")
	searchPRsCmd.Flags().StringVar(&searchPRsJSON, "json", "", "Output JSON with the specified fields (use --json=field1,field2; omit for all)")
	searchPRsCmd.Flags().Lookup("json").NoOptDefVal = searchJSONAll
	searchPRsCmd.Flags().StringVarP(&searchPRsTemplate, "template", "t", "", "Format each result with a Go template over the JSON fields")
	searchPRsCmd.Flags().BoolVar(&searchPRsNoTruncate, "no-truncate", false, "Don't truncate long titles")
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func runSearchIssues(cmd *cobra.Command, args []string) error {
	filter, sortKey, asc, err := buildIssueSearchFilter(args)
	if err != nil {
		return err
	}

	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()

	targets, err := searchTargets(store)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return printNoCachedRepos("search issues")
	}

	me := &meResolver{}
	items := []*searchItem{}
	for _, target := range targets {
		if !filter.matchesOwner(target.Owner) {
			continue
		}
		targetFilter, err := filter.forHost(me, target.Host)
		if err != nil {
			return err
		}
		repo := newSearchRepository(target)
		issues, err := store.QueryIssues(target.Host, target.Owner, target.Repo, cache.IssueQuery{})
		if err != nil {
			return fmt.Errorf("searching %s: %w", repo.label(), err)
		}
		for _, issue := range issues {
			if item := newIssueItem(repo, issue); targetFilter.matchesItem(item) {
				items = append(items, item)
			}
		}
		if searchIssuesIncludePRs {
			prs, err := store.QueryPRs(target.Host, target.Owner, target.Repo, cache.PRQuery{})
			if err != nil {
				return fmt.Errorf("searching %s: %w", repo.label(), err)
			}
			for _, pr := range prs {
				if item := newPRItem(repo, pr); targetFilter.matchesItem(item) {
					items = append(items, item)
				}
			}
		}
	}

	sortSearchItems(items, sortKey, asc)
	total := len(items)
	if searchIssuesLimit > 0 && len(items) > searchIssuesLimit {
		items = items[:searchIssuesLimit]
	}
	logging.Debug("search issues: searched the cache", "repos", len(targets), "matches", total)

	jsonFields, err := resolveSearchJSONFields(searchIssuesJSON, searchIssueJSONFields)
	if err != nil {
		return err
	}
	return printSearchItems(items, total, searchOutput{
		jsonFields: jsonFields,
		template:   searchIssuesTemplate,
		noTruncate: searchIssuesNoTruncate,
		markPRs:    searchIssuesIncludePRs,
	}, searchIssueJSONFields, "issues")
}

func runSearchPRs(cmd *cobra.Command, args []string) error {
	filter, sortKey, asc, err := buildPRSearchFilter(args)
	if err != nil {
		return err
	}

	store, err := newStore()
	if err != nil {
		return err
	}
	defer store.Close()

	targets, err := searchTargets(store)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return printNoCachedRepos("search prs")
	}

	me := &meResolver{}
	items := []*searchItem{}
	for _, target := range targets {
		if !filter.matchesOwner(target.Owner) {
			continue
		}
		targetFilter, err := filter.forHost(me, target.Host)
		if err != nil {
			return err
		}
		repo := newSearchRepository(target)
		prs, err := store.QueryPRs(target.Host, target.Owner, target.Repo, cache.PRQuery{})
		if err != nil {
			return fmt.Errorf("searching %s: %w", repo.label(), err)
		}
		for _, pr := range prs {
			if item := newPRItem(repo, pr); targetFilter.matchesItem(item) {
				items = append(items, item)
			}
		}
	}

	sortSearchItems(items, sortKey, asc)
	total := len(items)
	if searchPRsLimit > 0 && len(items) > searchPRsLimit {
		items = items[:searchPRsLimit]
	}
	logging.Debug("search prs: searched the cache", "repos", len(targets), "matches", total)

	jsonFields, err := resolveSearchJSONFields(searchPRsJSON, searchPRJSONFields)
	if err != nil {
		return err
	}
	return printSearchItems(items, total, searchOutput{
		jsonFields: jsonFields,
		template:   searchPRsTemplate,
		noTruncate: searchPRsNoTruncate,
	}, searchPRJSONFields, "pull requests")
}

// buildIssueSearchFilter validates the issue search flags and compiles them.
func buildIssueSearchFilter(args []string) (*searchFilter, searchSortKey, bool, error) {
	if err := validateLimit("--limit", searchIssuesLimit); err != nil {
		return nil, 0, false, err
	}
	if err := validateState(searchIssuesState, "open", "closed", "all"); err != nil {
		return nil, 0, false, err
	}
	if err := validateMatch(searchIssuesMatch); err != nil {
		return nil, 0, false, err
	}
	sortKey, err := parseSearchSort(searchIssuesSort)
	if err != nil {
		return nil, 0, false, err
	}
	asc, err := parseOrder(searchIssuesOrder)
	if err != nil {
		return nil, 0, false, err
	}

	created, err := parseDateFilter("--created", searchIssuesCreated)
	if err != nil {
		return nil, 0, false, err
	}
	updated, err := parseDateFilter("--updated", searchIssuesUpdated)
	if err != nil {
		return nil, 0, false, err
	}
	closed, err := parseDateFilter("--closed", searchIssuesClosed)
	if err != nil {
		return nil, 0, false, err
	}
	comments, err := parseNumberFilter("--comments", searchIssuesComments)
	if err != nil {
		return nil, 0, false, err
	}

	return &searchFilter{
		state:        searchIssuesState,
		author:       searchIssuesAuthor,
		assignee:     searchIssuesAssignee,
		labels:       searchIssuesLabels,
		milestone:    searchIssuesMilestone,
		app:          searchIssuesApp,
		noAssignee:   searchIssuesNoAssignee,
		noLabel:      searchIssuesNoLabel,
		noMile:       searchIssuesNoMilestone,
		owners:       searchIssuesOwner,
		created:      created,
		updated:      updated,
		closed:       closed,
		comments:     comments,
		terms:        searchTerms(args),
		match:        searchIssuesMatch,
		commenter:    searchIssuesCommenter,
		mentions:     searchIssuesMentions,
		teamMentions: searchIssuesTeamMentions,
		involves:     searchIssuesInvolves,
	}, sortKey, asc, nil
}

// buildPRSearchFilter validates the PR search flags and compiles them.
func buildPRSearchFilter(args []string) (*searchFilter, searchSortKey, bool, error) {
	if err := validateLimit("--limit", searchPRsLimit); err != nil {
		return nil, 0, false, err
	}
	if err := validateState(searchPRsState, "open", "closed", "merged", "all"); err != nil {
		return nil, 0, false, err
	}
	if err := validateMatch(searchPRsMatch); err != nil {
		return nil, 0, false, err
	}
	if err := validateChoice("--checks", searchPRsChecks, "pending", "success", "failure"); err != nil {
		return nil, 0, false, err
	}
	if err := validateChoice("--review", searchPRsReview, "none", "required", "approved", "changes_requested"); err != nil {
		return nil, 0, false, err
	}
	sortKey, err := parseSearchSort(searchPRsSort)
	if err != nil {
		return nil, 0, false, err
	}
	asc, err := parseOrder(searchPRsOrder)
	if err != nil {
		return nil, 0, false, err
	}

	created, err := parseDateFilter("--created", searchPRsCreated)
	if err != nil {
		return nil, 0, false, err
	}
	updated, err := parseDateFilter("--updated", searchPRsUpdated)
	if err != nil {
		return nil, 0, false, err
	}
	closed, err := parseDateFilter("--closed", searchPRsClosed)
	if err != nil {
		return nil, 0, false, err
	}
	mergedAt, err := parseDateFilter("--merged-at", searchPRsMergedAt)
	if err != nil {
		return nil, 0, false, err
	}
	comments, err := parseNumberFilter("--comments", searchPRsComments)
	if err != nil {
		return nil, 0, false, err
	}

	return &searchFilter{
		state:           searchPRsState,
		author:          searchPRsAuthor,
		assignee:        searchPRsAssignee,
		labels:          searchPRsLabels,
		milestone:       searchPRsMilestone,
		app:             searchPRsApp,
		noAssignee:      searchPRsNoAssignee,
		noLabel:         searchPRsNoLabel,
		noMile:          searchPRsNoMilestone,
		owners:          searchPRsOwner,
		created:         created,
		updated:         updated,
		closed:          closed,
		mergedAt:        mergedAt,
		comments:        comments,
		terms:           searchTerms(args),
		match:           searchPRsMatch,
		commenter:       searchPRsCommenter,
		mentions:        searchPRsMentions,
		teamMentions:    searchPRsTeamMentions,
		involves:        searchPRsInvolves,
		base:            searchPRsBase,
		head:            searchPRsHead,
		draft:           searchPRsDraft,
		merged:          searchPRsMerged,
		checks:          searchPRsChecks,
		review:          searchPRsReview,
		reviewRequested: searchPRsReviewRequested,
		reviewedBy:      searchPRsReviewedBy,
	}, sortKey, asc, nil
}

// ---------------------------------------------------------------------------
// Flag validation and shared helpers
// ---------------------------------------------------------------------------

func validateLimit(flag string, limit int) error {
	if limit < 0 {
		return fmt.Errorf("invalid %s %d: use 0 for no limit or a positive number", flag, limit)
	}
	return nil
}

func validateState(value string, allowed ...string) error {
	return validateChoice("--state", value, allowed...)
}

func validateChoice(flag, value string, allowed ...string) error {
	if value == "" {
		return nil
	}
	for _, a := range allowed {
		if strings.EqualFold(value, a) {
			return nil
		}
	}
	return fmt.Errorf("invalid %s %q: use one of %s", flag, value, strings.Join(allowed, ", "))
}

func validateMatch(fields []string) error {
	for _, field := range fields {
		if err := validateChoice("--match", field, "title", "body", "comments"); err != nil {
			return err
		}
	}
	return nil
}

func parseOrder(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "desc":
		return false, nil
	case "asc":
		return true, nil
	default:
		return false, fmt.Errorf("invalid --order %q: use asc or desc", value)
	}
}

// searchTargets resolves the repositories a search runs over. Without --repo it
// searches every cached repository, matching gh search's account-wide default;
// with --repo it searches only that repository, cached or not.
func searchTargets(store cache.Store) ([]cache.CachedRepo, error) {
	if repoFlag != "" {
		repo, err := gitremote.ParseRepo(repoFlag)
		if err != nil {
			return nil, err
		}
		return []cache.CachedRepo{{Host: repo.Host, Owner: repo.Owner, Repo: repo.Name}}, nil
	}
	return store.ListCachedRepos()
}

// searchTerms normalizes the positional query arguments. Each argument is one
// term, so a quoted argument stays a phrase.
func searchTerms(args []string) []string {
	terms := make([]string, 0, len(args))
	for _, arg := range args {
		if term := strings.ToLower(strings.TrimSpace(arg)); term != "" {
			terms = append(terms, term)
		}
	}
	return terms
}

func printNoCachedRepos(command string) error {
	fmt.Printf("No cached repositories found. Run \"ghx cache\" before \"ghx %s\".\n", command)
	return nil
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

// searchRepository identifies the repository a search result belongs to.
type searchRepository struct {
	Host          string `json:"host"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	NameWithOwner string `json:"nameWithOwner"`
}

func newSearchRepository(r cache.CachedRepo) searchRepository {
	return searchRepository{
		Host:          r.Host,
		Owner:         r.Owner,
		Name:          r.Repo,
		NameWithOwner: r.Owner + "/" + r.Repo,
	}
}

// label formats the repository coordinates the way the table shows them,
// qualifying the host when it is not github.com.
func (r searchRepository) label() string {
	return repoLabel(r.Host, r.Owner, r.Name)
}

// templateValue exposes the repository as a map so a --template can read it
// with the same lowercase keys the JSON output uses.
func (r searchRepository) templateValue() map[string]any {
	return map[string]any{
		"host":          r.Host,
		"owner":         r.Owner,
		"name":          r.Name,
		"nameWithOwner": r.NameWithOwner,
	}
}

// searchJSONFields maps a gh-compatible JSON field name to its value on a
// search result. Only fields ghx can populate are registered, so typos fail
// loudly instead of silently emitting nulls.
var searchIssueJSONFields = map[string]func(*searchItem) any{
	"assignees":     func(i *searchItem) any { return i.assignees },
	"author":        func(i *searchItem) any { return i.author },
	"body":          func(i *searchItem) any { return i.body },
	"closedAt":      func(i *searchItem) any { return i.closedAt },
	"comments":      func(i *searchItem) any { return i.comments },
	"commentsCount": func(i *searchItem) any { return i.commentCount },
	"createdAt":     func(i *searchItem) any { return i.createdAt },
	"isPullRequest": func(i *searchItem) any { return i.isPR },
	"labels":        func(i *searchItem) any { return i.labels },
	"milestone":     func(i *searchItem) any { return i.milestone },
	"number":        func(i *searchItem) any { return i.number },
	"repository":    func(i *searchItem) any { return i.repo.templateValue() },
	"state":         func(i *searchItem) any { return i.state },
	"title":         func(i *searchItem) any { return i.title },
	"updatedAt":     func(i *searchItem) any { return i.updatedAt },
	"url":           func(i *searchItem) any { return i.url },
}

// searchPRJSONFields is the pull-request field set: the issue fields plus the
// pull request record's own fields.
var searchPRJSONFields = func() map[string]func(*searchItem) any {
	fields := map[string]func(*searchItem) any{
		"additions":      func(i *searchItem) any { return i.additions },
		"baseRefName":    func(i *searchItem) any { return i.baseRefName },
		"checks":         func(i *searchItem) any { return i.checks },
		"deletions":      func(i *searchItem) any { return i.deletions },
		"headRefName":    func(i *searchItem) any { return i.headRefName },
		"headRefOid":     func(i *searchItem) any { return i.headRefOid },
		"isDraft":        func(i *searchItem) any { return i.isDraft },
		"mergedAt":       func(i *searchItem) any { return i.mergedAt },
		"reviewDecision": func(i *searchItem) any { return i.reviewDecision },
		"reviewRequests": func(i *searchItem) any { return i.reviewRequests },
		"reviewThreads":  func(i *searchItem) any { return i.reviewThreads },
		"reviews":        func(i *searchItem) any { return i.reviews },
		"timeline":       func(i *searchItem) any { return i.timeline },
	}
	for name, value := range searchIssueJSONFields {
		fields[name] = value
	}
	return fields
}()

// resolveSearchJSONFields validates --json and returns the field names to emit.
// An empty value disables JSON output; the "all" sentinel selects every field.
func resolveSearchJSONFields(raw string, supported map[string]func(*searchItem) any) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	return resolveJSONFieldList(raw, supported, searchJSONAll)
}

// searchOutput describes how results are rendered.
type searchOutput struct {
	jsonFields []string
	template   string
	noTruncate bool
	markPRs    bool
}

func printSearchItems(items []*searchItem, total int, out searchOutput, supported map[string]func(*searchItem) any, noun string) error {
	fields := out.jsonFields
	if out.template != "" && fields == nil {
		fields = sortedJSONFieldNames(supported)
	}
	if out.template != "" {
		return renderSearchTemplate(items, fields, out.template, supported)
	}
	if out.jsonFields != nil {
		rows := make([]map[string]any, 0, len(items))
		for _, item := range items {
			rows = append(rows, searchRow(item, fields, supported))
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}

	if len(items) == 0 {
		fmt.Printf("No cached %s matched your search.\n", noun)
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	for _, item := range items {
		title := item.title
		if out.markPRs && item.isPR {
			title = "[PR] " + title
		}
		if !out.noTruncate {
			title = truncate(title, 60)
		}
		fmt.Fprintf(w, "%s#%d\t%s\t%s\t%s\n",
			item.repo.label(),
			item.number,
			title,
			strings.ToLower(item.state),
			item.updatedAt.Format("2006-01-02"),
		)
	}
	w.Flush()

	if total > len(items) {
		logging.Info("showing results", "shown", len(items), "total", total)
	}
	return nil
}

// searchRow projects one item onto the selected JSON fields.
func searchRow(item *searchItem, fields []string, supported map[string]func(*searchItem) any) map[string]any {
	row := make(map[string]any, len(fields))
	for _, name := range fields {
		if value, ok := supported[name]; ok {
			row[name] = value(item)
		}
	}
	return row
}

// renderSearchTemplate executes the user's template once per result over the
// same data --json would emit.
func renderSearchTemplate(items []*searchItem, fields []string, text string, supported map[string]func(*searchItem) any) error {
	tmpl, err := template.New("search").Parse(text)
	if err != nil {
		return fmt.Errorf("invalid --template: %w", err)
	}
	for _, item := range items {
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, searchRow(item, fields, supported)); err != nil {
			return fmt.Errorf("executing --template: %w", err)
		}
		os.Stdout.Write(buf.Bytes())
		if !strings.HasSuffix(buf.String(), "\n") {
			fmt.Println()
		}
	}
	return nil
}
