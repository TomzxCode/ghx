package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/tomzxcode/ghx/internal/github"
)

// searchItem is one cached issue or pull request, flattened so both types share
// the filtering, sorting, and output paths. Exactly one of the record pointers
// in the constructors fills it.
type searchItem struct {
	repo         searchRepository
	isPR         bool
	number       int
	title        string
	state        string
	body         string
	author       github.Actor
	assignees    []github.Actor
	labels       []github.Label
	milestone    *github.Milestone
	createdAt    time.Time
	updatedAt    time.Time
	closedAt     *time.Time
	url          string
	comments     []github.Comment
	commentCount int

	// Pull-request-only fields.
	isDraft        bool
	baseRefName    string
	headRefName    string
	headRefOid     string
	mergedAt       *time.Time
	reviewDecision string
	additions      int
	deletions      int
	reviews        []github.PRReview
	timeline       []github.TimelineEvent
	reviewRequests []github.ReviewRequest
	checks         *github.PRChecks
	reviewThreads  *github.PRThreadCounts
}

func newIssueItem(repo searchRepository, issue *github.Issue) *searchItem {
	count := issue.CommentCount
	if count < len(issue.Comments) {
		count = len(issue.Comments)
	}
	return &searchItem{
		repo:         repo,
		number:       issue.Number,
		title:        issue.Title,
		state:        issue.State,
		body:         issue.Body,
		author:       issue.Author,
		assignees:    issue.Assignees,
		labels:       issue.Labels,
		milestone:    issue.Milestone,
		createdAt:    issue.CreatedAt,
		updatedAt:    issue.UpdatedAt,
		closedAt:     issue.ClosedAt,
		url:          issue.URL,
		comments:     issue.Comments,
		commentCount: count,
	}
}

func newPRItem(repo searchRepository, pr *github.PullRequest) *searchItem {
	count := pr.CommentCount
	if count < len(pr.Comments) {
		count = len(pr.Comments)
	}
	return &searchItem{
		repo:           repo,
		isPR:           true,
		number:         pr.Number,
		title:          pr.Title,
		state:          pr.State,
		body:           pr.Body,
		author:         pr.Author,
		assignees:      pr.Assignees,
		labels:         pr.Labels,
		milestone:      pr.Milestone,
		createdAt:      pr.CreatedAt,
		updatedAt:      pr.UpdatedAt,
		closedAt:       pr.ClosedAt,
		url:            pr.URL,
		comments:       pr.Comments,
		commentCount:   count,
		isDraft:        pr.IsDraft,
		baseRefName:    pr.BaseRefName,
		headRefName:    pr.HeadRefName,
		headRefOid:     pr.HeadRefOid,
		mergedAt:       pr.MergedAt,
		reviewDecision: pr.ReviewDecision,
		additions:      pr.Additions,
		deletions:      pr.Deletions,
		reviews:        pr.Reviews,
		timeline:       pr.Timeline,
		reviewRequests: pr.ReviewRequests,
		checks:         pr.Checks,
		reviewThreads:  pr.ReviewThreads,
	}
}

// searchFilter is the compiled form of every filter flag. Each field is an
// optional predicate; a zero value does not filter anything.
type searchFilter struct {
	state      string
	author     string
	assignee   string
	labels     []string
	milestone  string
	app        string
	noAssignee bool
	noLabel    bool
	noMile     bool
	owners     []string

	created  *dateFilter
	updated  *dateFilter
	closed   *dateFilter
	mergedAt *dateFilter
	comments *numberFilter

	terms        []string
	match        []string
	commenter    string
	mentions     string
	teamMentions string
	involves     string

	// Pull-request-only filters.
	base            string
	head            string
	draft           bool
	merged          bool
	checks          string
	review          string
	reviewRequested string
	reviewedBy      string
}

// forHost resolves the "@me" shorthands against one repository host, so a
// search spanning several hosts filters with each host's own login.
func (f *searchFilter) forHost(me *meResolver, host string) (*searchFilter, error) {
	clone := *f
	for _, target := range []*string{
		&clone.author, &clone.assignee, &clone.commenter,
		&clone.mentions, &clone.involves,
		&clone.reviewRequested, &clone.reviewedBy,
	} {
		resolved, err := me.resolve(*target, host)
		if err != nil {
			return nil, err
		}
		*target = resolved
	}
	return &clone, nil
}

// matchesOwner reports whether the repository owner passes --owner.
func (f *searchFilter) matchesOwner(owner string) bool {
	if len(f.owners) == 0 {
		return true
	}
	for _, want := range f.owners {
		if strings.EqualFold(want, owner) {
			return true
		}
	}
	return false
}

// matchesItem reports whether item satisfies every configured filter.
func (f *searchFilter) matchesItem(item *searchItem) bool {
	if !f.matchesState(item) {
		return false
	}
	if f.author != "" && !strings.EqualFold(item.author.Login, f.author) {
		return false
	}
	if f.assignee != "" && !actorIn(item.assignees, f.assignee) {
		return false
	}
	if len(f.labels) > 0 && !hasAllNamedLabels(item.labels, f.labels) {
		return false
	}
	if f.milestone != "" && !matchesMilestone(item.milestone, f.milestone) {
		return false
	}
	if f.noAssignee && len(item.assignees) > 0 {
		return false
	}
	if f.noLabel && len(item.labels) > 0 {
		return false
	}
	if f.noMile && item.milestone != nil {
		return false
	}
	if f.app != "" && !matchesApp(item.author, f.app) {
		return false
	}
	if !f.matchesDates(item) {
		return false
	}
	if f.comments != nil && !f.comments.matches(item.commentCount) {
		return false
	}
	if !item.matchesTerms(f.terms, f.match) {
		return false
	}
	if f.commenter != "" && !item.commentedBy(f.commenter) {
		return false
	}
	if f.mentions != "" && !item.mentionsUser(f.mentions) {
		return false
	}
	if f.teamMentions != "" && !item.mentionsTeam(f.teamMentions) {
		return false
	}
	if f.involves != "" && !item.involves(f.involves) {
		return false
	}
	return f.matchesPR(item)
}

func (f *searchFilter) matchesState(item *searchItem) bool {
	if f.state == "" || strings.EqualFold(f.state, "all") {
		return true
	}
	if strings.EqualFold(item.state, f.state) {
		return true
	}
	// A merged pull request appears as closed when included in issue search.
	return strings.EqualFold(f.state, "closed") && item.mergedAt != nil
}

func (f *searchFilter) matchesDates(item *searchItem) bool {
	for _, tc := range []struct {
		filter *dateFilter
		value  *time.Time
	}{
		{f.created, &item.createdAt},
		{f.updated, &item.updatedAt},
		{f.closed, item.closedAt},
		{f.mergedAt, item.mergedAt},
	} {
		if tc.filter == nil {
			continue
		}
		if tc.value == nil || !tc.filter.matches(*tc.value) {
			return false
		}
	}
	return true
}

func (f *searchFilter) matchesPR(item *searchItem) bool {
	if f.base != "" && !strings.EqualFold(item.baseRefName, f.base) {
		return false
	}
	if f.head != "" && !strings.EqualFold(item.headRefName, f.head) {
		return false
	}
	if f.draft && !item.isDraft {
		return false
	}
	if f.merged && item.mergedAt == nil {
		return false
	}
	if f.checks != "" && !matchesChecks(item.checks, f.checks) {
		return false
	}
	if f.review != "" && !matchesReviewDecision(item.reviewDecision, f.review) {
		return false
	}
	if f.reviewRequested != "" && !item.reviewRequestedFrom(f.reviewRequested) {
		return false
	}
	if f.reviewedBy != "" && !item.reviewedBy(f.reviewedBy) {
		return false
	}
	return true
}

func actorIn(actors []github.Actor, login string) bool {
	for _, a := range actors {
		if strings.EqualFold(a.Login, login) {
			return true
		}
	}
	return false
}

func hasAllNamedLabels(labels []github.Label, want []string) bool {
	for _, name := range want {
		found := false
		for _, l := range labels {
			if strings.EqualFold(l.Name, name) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func matchesMilestone(m *github.Milestone, want string) bool {
	if m == nil {
		return false
	}
	return strings.EqualFold(m.Title, want) || strconv.Itoa(m.Number) == want
}

// matchesApp matches a GitHub App author, approximately: the cache stores the
// actor type, and GitHub Apps act as bot accounts whose login ends in "[bot]".
func matchesApp(a github.Actor, app string) bool {
	if !strings.EqualFold(a.Type, "Bot") && !strings.EqualFold(a.Type, "App") {
		return false
	}
	login := strings.ToLower(a.Login)
	login = strings.TrimSuffix(login, "[bot]")
	login = strings.TrimPrefix(login, "app/")
	return login == strings.ToLower(app)
}

func matchesChecks(checks *github.PRChecks, want string) bool {
	if checks == nil {
		return false
	}
	got := strings.ToLower(checks.State)
	switch strings.ToLower(want) {
	case "success":
		return got == "success"
	case "failure":
		return got == "failure" || got == "error"
	case "pending":
		return got == "pending" || got == "expected"
	default:
		return false
	}
}

func matchesReviewDecision(decision, want string) bool {
	switch strings.ToLower(want) {
	case "none":
		return decision == ""
	case "required":
		return strings.EqualFold(decision, "REVIEW_REQUIRED")
	case "approved":
		return strings.EqualFold(decision, "APPROVED")
	case "changes_requested":
		return strings.EqualFold(decision, "CHANGES_REQUESTED")
	default:
		return false
	}
}

// matchesTerms reports whether every term appears in one of the selected
// fields. With no terms, every item matches.
func (i *searchItem) matchesTerms(terms, match []string) bool {
	if len(terms) == 0 {
		return true
	}
	fields := match
	if len(fields) == 0 {
		fields = []string{"title", "body", "comments"}
	}
	var parts []string
	for _, field := range fields {
		switch strings.ToLower(field) {
		case "title":
			parts = append(parts, i.title)
		case "body":
			parts = append(parts, i.body)
		case "comments":
			for _, c := range i.comments {
				parts = append(parts, c.Body)
			}
		}
	}
	text := strings.ToLower(strings.Join(parts, "\n"))
	for _, term := range terms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

func (i *searchItem) commentedBy(login string) bool {
	for _, c := range i.comments {
		if strings.EqualFold(c.Author.Login, login) {
			return true
		}
	}
	return false
}

func (i *searchItem) mentionsUser(login string) bool {
	return containsHandle(i.textWithComments(), "@"+strings.TrimPrefix(login, "@"))
}

func (i *searchItem) mentionsTeam(team string) bool {
	return containsHandle(i.textWithComments(), "@"+strings.TrimPrefix(team, "@"))
}

// involves reports whether the user is the author, an assignee, a commenter,
// or mentioned in the body or comments.
func (i *searchItem) involves(login string) bool {
	return strings.EqualFold(i.author.Login, login) ||
		actorIn(i.assignees, login) ||
		i.commentedBy(login) ||
		i.mentionsUser(login)
}

func (i *searchItem) reviewRequestedFrom(who string) bool {
	want := strings.ToLower(strings.TrimPrefix(who, "@"))
	for _, rr := range i.reviewRequests {
		if rr.Login != "" && strings.EqualFold(rr.Login, want) {
			return true
		}
		if rr.Name != "" && (strings.EqualFold(rr.Name, want) || strings.HasSuffix(want, "/"+strings.ToLower(rr.Name))) {
			return true
		}
	}
	return false
}

func (i *searchItem) reviewedBy(login string) bool {
	for _, r := range i.reviews {
		if strings.EqualFold(r.Author.Login, login) && !strings.EqualFold(r.State, "PENDING") {
			return true
		}
	}
	return false
}

func (i *searchItem) textWithComments() string {
	parts := []string{i.body}
	for _, c := range i.comments {
		parts = append(parts, c.Body)
	}
	return strings.Join(parts, "\n")
}

// containsHandle reports whether text mentions handle as a complete login, so
// "@alice" does not match "@alice-bot".
func containsHandle(text, handle string) bool {
	lower := strings.ToLower(text)
	handle = strings.ToLower(handle)
	for {
		idx := strings.Index(lower, handle)
		if idx < 0 {
			return false
		}
		end := idx + len(handle)
		if end >= len(lower) || !isHandleChar(rune(lower[end])) {
			return true
		}
		lower = lower[end:]
	}
}

func isHandleChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_'
}

// ---------------------------------------------------------------------------
// Sort
// ---------------------------------------------------------------------------

type searchSortKey int

const (
	sortUpdated searchSortKey = iota
	sortCreated
	sortComments
)

// parseSearchSort maps --sort to a cached field. GitHub's server-side orderings
// (best-match, reactions, interactions) have no local equivalent.
func parseSearchSort(value string) (searchSortKey, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "updated":
		return sortUpdated, nil
	case "created":
		return sortCreated, nil
	case "comments":
		return sortComments, nil
	default:
		return 0, fmt.Errorf("invalid --sort %q: use updated, created, or comments", value)
	}
}

// sortSearchItems orders items by the chosen key, then by repository and
// number so ties are deterministic. A nil order means descending.
func sortSearchItems(items []*searchItem, key searchSortKey, asc bool) {
	sort.SliceStable(items, func(a, b int) bool {
		x, y := items[a], items[b]
		cmp := 0
		switch key {
		case sortCreated:
			cmp = x.createdAt.Compare(y.createdAt)
		case sortComments:
			cmp = x.commentCount - y.commentCount
		default:
			cmp = x.updatedAt.Compare(y.updatedAt)
		}
		if cmp != 0 {
			if asc {
				return cmp < 0
			}
			return cmp > 0
		}
		if x.repo.NameWithOwner != y.repo.NameWithOwner {
			return x.repo.NameWithOwner < y.repo.NameWithOwner
		}
		return x.number < y.number
	})
}

// ---------------------------------------------------------------------------
// Date and number filter grammar
// ---------------------------------------------------------------------------

// dateFilter matches a timestamp against inclusive and exclusive bounds.
type dateFilter struct {
	after  *time.Time // t >= after
	before *time.Time // t < before
}

// parseDateFilter compiles GitHub's date qualifier grammar: DATE, >DATE,
// >=DATE, <DATE, <=DATE, and START..END (either end optional).
func parseDateFilter(flag, expr string) (*dateFilter, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, nil
	}
	fail := func(reason string) error {
		return fmt.Errorf("invalid %s value %q: %s; use YYYY-MM-DD, >DATE, >=DATE, <DATE, <=DATE, or START..END", flag, expr, reason)
	}

	if strings.Contains(expr, "..") {
		start, end, _ := strings.Cut(expr, "..")
		f := &dateFilter{}
		if start = strings.TrimSpace(start); start != "" {
			day, err := parseDate(start)
			if err != nil {
				return nil, fail("the start date must be YYYY-MM-DD")
			}
			f.after = &day
		}
		if end = strings.TrimSpace(end); end != "" {
			day, err := parseDate(end)
			if err != nil {
				return nil, fail("the end date must be YYYY-MM-DD")
			}
			next := day.Add(24 * time.Hour)
			f.before = &next
		}
		if f.after == nil && f.before == nil {
			return nil, fail("the range needs at least one end")
		}
		return f, nil
	}

	op := ""
	for _, candidate := range []string{">=", "<=", ">", "<"} {
		if strings.HasPrefix(expr, candidate) {
			op = candidate
			expr = strings.TrimSpace(strings.TrimPrefix(expr, candidate))
			break
		}
	}
	day, err := parseDate(expr)
	if err != nil {
		return nil, fail("the date must be YYYY-MM-DD")
	}

	next := day.Add(24 * time.Hour)
	switch op {
	case ">=":
		return &dateFilter{after: &day}, nil
	case ">":
		return &dateFilter{after: &next}, nil
	case "<":
		return &dateFilter{before: &day}, nil
	case "<=":
		return &dateFilter{before: &next}, nil
	default:
		return &dateFilter{after: &day, before: &next}, nil
	}
}

func parseDate(value string) (time.Time, error) {
	return time.Parse("2006-01-02", value)
}

func (f *dateFilter) matches(t time.Time) bool {
	if f.after != nil && t.Before(*f.after) {
		return false
	}
	if f.before != nil && !t.Before(*f.before) {
		return false
	}
	return true
}

// numberFilter matches an integer against minimum and maximum bounds.
type numberFilter struct {
	min *int
	max *int
}

// parseNumberFilter compiles GitHub's number qualifier grammar: N, >N, >=N,
// <N, <=N, and START..END (either end optional).
func parseNumberFilter(flag, expr string) (*numberFilter, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, nil
	}
	fail := func() error {
		return fmt.Errorf("invalid %s value %q: use N, >N, >=N, <N, <=N, or START..END", flag, expr)
	}

	if strings.Contains(expr, "..") {
		start, end, _ := strings.Cut(expr, "..")
		f := &numberFilter{}
		if start = strings.TrimSpace(start); start != "" {
			n, err := strconv.Atoi(start)
			if err != nil {
				return nil, fail()
			}
			f.min = &n
		}
		if end = strings.TrimSpace(end); end != "" {
			n, err := strconv.Atoi(end)
			if err != nil {
				return nil, fail()
			}
			f.max = &n
		}
		if f.min == nil && f.max == nil {
			return nil, fail()
		}
		return f, nil
	}

	op := ""
	for _, candidate := range []string{">=", "<=", ">", "<"} {
		if strings.HasPrefix(expr, candidate) {
			op = candidate
			expr = strings.TrimSpace(strings.TrimPrefix(expr, candidate))
			break
		}
	}
	n, err := strconv.Atoi(expr)
	if err != nil {
		return nil, fail()
	}
	switch op {
	case ">=":
		return &numberFilter{min: &n}, nil
	case ">":
		min := n + 1
		return &numberFilter{min: &min}, nil
	case "<":
		max := n - 1
		return &numberFilter{max: &max}, nil
	case "<=":
		return &numberFilter{max: &n}, nil
	default:
		return &numberFilter{min: &n, max: &n}, nil
	}
}

func (f *numberFilter) matches(n int) bool {
	if f.min != nil && n < *f.min {
		return false
	}
	if f.max != nil && n > *f.max {
		return false
	}
	return true
}
