package cmd

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
)

// repoPR pairs a pull request with the repository it came from, so a report
// spanning several repositories can label and deduplicate entries.
type repoPR struct {
	Repo string
	PR   *github.PullRequest
}

// statsFilters narrows the set of PRs and reviewers included in a report.
// Authors and Reviewers are lowercase login sets (empty = no restriction).
// From and To are period bounds (zero = unbounded).
type statsFilters struct {
	Authors     map[string]bool
	Reviewers   map[string]bool
	From        time.Time
	To          time.Time
	State       string
	IncludeBots bool
	Top         int
	ListPRs     bool
}

// MatrixCell aggregates the PRs of one author x reviewer combination.
type MatrixCell struct {
	Count  int
	Avg    string
	Median string
	Pct    string       // share of the row author's PRs the reviewer responded to
	Bg     template.CSS // generated heat colour, safe for style attributes
}

// MatrixRow is one author row of the matrix, with cells aligned to the
// matrix's reviewer columns.
type MatrixRow struct {
	Author string
	Total  int
	Cells  []*MatrixCell
}

// MatrixCol is one reviewer column header of the matrix.
type MatrixCol struct {
	Reviewer  string
	Total     int
	NoReviews bool
}

// Matrix is the author (rows) x reviewer (columns) table at the heart of the
// report.
type Matrix struct {
	Cols []*MatrixCol
	Rows []*MatrixRow
}

// AuthorSummary aggregates one author's PRs for the per-author table.
type AuthorSummary struct {
	Login               string
	PRs                 int
	Merged              int
	MedianFirstResponse string
	AvgFirstResponse    string
}

// ReviewerSummary aggregates one reviewer's review activity for the
// per-reviewer table. PRsReviewed counts PRs where the reviewer submitted a
// review, not every PR they responded to with an issue comment.
type ReviewerSummary struct {
	Login       string
	PRsReviewed int
	Median      string
	Avg         string
}

// NotablePR references one pull request in a notable-PRs list.
type NotablePR struct {
	Repo   string
	Number int
	Title  string
	URL    string
	Author string
	Value  string
}

// MonthTrend aggregates PR activity for one calendar month.
type MonthTrend struct {
	Month       string // "2026-03"
	Opened      int
	Merged      int
	MedianMerge string

	// medianHrs is the median merge time in hours, feeding the trend chart.
	medianHrs float64
}

// AuthorMonthStat aggregates one author's PR activity for one calendar month.
type AuthorMonthStat struct {
	Month  string // "2026-03"
	Opened int
	Merged int
	Closed int
}

// AuthorTrend holds one author's monthly opened/merged/closed series, rendered
// as a per-author chart.
type AuthorTrend struct {
	Login  string
	Months []*AuthorMonthStat
}

// SizeRow aggregates one PR size bucket for the size-versus-lead-time table.
type SizeRow struct {
	Bucket      string // xs, s, m, l, xl
	Count       int
	MergedCount int
	MedianMerge string
}

// MergeSpeedRow is one cumulative bucket of the merge-speed table, counting
// merged PRs that landed within the bucket's window.
type MergeSpeedRow struct {
	Bucket string // "Within 1 hour", "Within 1 day", "Within 1 week"
	Count  int
	Pct    string
}

// MergePercentileRow is one percentile of the time from opening to merge.
type MergePercentileRow struct {
	Label string // p50, p90, p99
	Value string
}

// AuthorMergeSpeedRow holds per-author merge-speed statistics: how many of the
// author's merged PRs landed within 1h/1d/1w of being opened (count with
// share) and the p50/p90/p99 of their time-to-merge distribution.
type AuthorMergeSpeedRow struct {
	Login       string
	MergedCount int
	Within1h    string
	Within1d    string
	Within1w    string
	P50         string
	P90         string
	P99         string
}

// LeadTimeRow aggregates merged-PR lead times for one author.
type LeadTimeRow struct {
	Login              string
	MergedCount        int
	MedianMerge        string
	AvgMerge           string
	MedianDraft        string
	MedianFirstReview  string
	MedianApprove      string
	MedianApproveMerge string
}

// sizeBucketCount is the number of PR size buckets (xs, s, m, l, xl).
const sizeBucketCount = 5

// sizeBucketNames labels the size buckets in order.
var sizeBucketNames = [sizeBucketCount]string{"xs", "s", "m", "l", "xl"}

// sizeBucketThresholds bound each bucket by total changed lines:
// xs <100, s <500, m <1000, l <2000, xl >=2000.
var sizeBucketThresholds = [sizeBucketCount]int{100, 500, 1000, 2000}

// sizeBucket maps a total changed-line count to its bucket index.
func sizeBucket(changed int) int {
	for i, t := range sizeBucketThresholds {
		if changed < t {
			return i
		}
	}
	return sizeBucketCount - 1
}

// ContributionRow aggregates workload for one author.
type ContributionRow struct {
	Login            string
	Opened           int
	Merged           int
	Closed           int
	MergeRate        string
	NoReview         int
	CommentsReceived int
	Additions        int
	Deletions        int
	HasSizeData      bool
	SizeCounts       [sizeBucketCount]int
}

// ReviewerEngagementRow aggregates formal review activity and response times
// for one reviewer. PRsCommented counts PRs where the reviewer left issue
// comments; PRsReviewed counts PRs where they submitted a review.
type ReviewerEngagementRow struct {
	Login                   string
	PRsCommented            int
	Comments                int
	PRsReviewed             int
	Reviews                 int
	Approvals               int
	ChangesRequested        int
	CommentOnly             int
	MedianOpenResponse      string
	MedianRequestResponse   string
	MedianRerequestResponse string
}

// PRRow is one row of the appendix table listing every PR in the period.
type PRRow struct {
	Repo      string
	Number    int
	Title     string
	URL       string
	Author    string
	State     string
	CreatedAt string
	MergedAt  string
}

// Report holds everything the HTML template renders.
type Report struct {
	Repos []string
	// RepoIDs holds host/owner/repo identifiers for the interactive period
	// form's hidden inputs; empty unless the form is rendered.
	RepoIDs []string
	// Interactive marks a report served by the web UI: the template renders
	// the period form that reloads /stats with new filters. CLI output stays
	// a static document.
	Interactive bool
	// AllRepos reports whether the request covered every cached repository
	// (no explicit repo list), so the period form can resubmit without
	// pinning the repository set.
	AllRepos bool
	// FromValue and ToValue are the period bounds formatted for date inputs
	// (YYYY-MM-DD, empty when unbounded).
	FromValue     string
	ToValue       string
	Period        string
	State         string
	GeneratedAt   string
	IncludeBots   bool
	ShowPRs       bool
	TotalPRs      int
	Merged        int
	Open          int
	Closed        int
	AuthorCount   int
	ReviewerCount int
	OverallMedian string
	OverallAvg    string
	AuthorRows    []*AuthorSummary
	ReviewerRows  []*ReviewerSummary
	Matrix        *Matrix
	PRs           []*PRRow

	MedianMerge         string
	MergeSpeedRows      []*MergeSpeedRow
	MergePercentileRows []*MergePercentileRow
	TotalReviews        int
	TotalAdditions      int
	TotalDeletions      int
	LegacyCache         bool
	TopN                int
	LeadRows            []*LeadTimeRow
	AuthorMergeSpeed    []*AuthorMergeSpeedRow
	// MergeTrendSamples holds one entry per merged PR, [merge unix time,
	// merge duration in minutes], feeding the client-side trend charts.
	MergeTrendSamples [][2]float64
	ContributionRows  []*ContributionRow
	EngagementRows    []*ReviewerEngagementRow
	SizeRows          []*SizeRow
	Trends            []*MonthTrend
	AuthorTrends      []*AuthorTrend
	ActivityHours     []int
	NotableAwaiting   []*NotablePR
	NotableMerge      []*NotablePR
	NotableCommented  []*NotablePR
}

// chartsPayload is the data bundle serialized to JSON for the Chart.js
// scripts in the rendered report.
type chartsPayload struct {
	Months    []string  `json:"months"`
	Opened    []int     `json:"opened"`
	Merged    []int     `json:"merged"`
	MedianHrs []float64 `json:"medianHrs"`
	Hours     []int     `json:"hours"`
	Sizes     []int     `json:"sizes"`
	// MergeSamples holds one entry per merged PR, [merge unix time,
	// merge duration in minutes], so the client can re-bucket the merge
	// speed and percentile trends by day, week or month.
	MergeSamples [][2]float64 `json:"mergeSamples"`
	// AuthorTrends holds one monthly series per author, feeding the
	// per-author opened/merged/closed charts.
	AuthorTrends []authorTrendPayload `json:"authorTrends"`
}

// authorTrendPayload is the JSON shape of one author's monthly trend series.
type authorTrendPayload struct {
	Login  string   `json:"login"`
	Months []string `json:"months"`
	Opened []int    `json:"opened"`
	Merged []int    `json:"merged"`
	Closed []int    `json:"closed"`
}

// reviewActivity is one submitted review on a PR, normalized for aggregation.
type reviewActivity struct {
	Login       string // lowercase reviewer login
	State       string // APPROVED, CHANGES_REQUESTED or COMMENTED
	SubmittedAt time.Time
}

// statReviewerAllowed reports whether a reviewer participates in the report:
// bot accounts are excluded unless requested, and an explicit reviewer filter
// restricts the set. Bot detection uses the account type GitHub reports, so
// automation under a human account still counts as human.
func statReviewerAllowed(a github.Actor, pr *github.PullRequest, f statsFilters) bool {
	lower := strings.ToLower(a.Login)
	if lower == "" || lower == strings.ToLower(pr.Author.Login) {
		return false
	}
	if !f.IncludeBots && a.Type == "Bot" {
		return false
	}
	if len(f.Reviewers) > 0 && !f.Reviewers[lower] {
		return false
	}
	return true
}

// authorComments counts issue comments written by the PR author.
func authorComments(pr *github.PullRequest) int {
	n := 0
	author := strings.ToLower(pr.Author.Login)
	for _, c := range pr.Comments {
		if strings.ToLower(c.Author.Login) == author {
			n++
		}
	}
	return n
}

// draftDuration reconstructs total time spent in draft from timeline events.
// A ready-for-review event without a preceding convert-to-draft event implies
// the PR was created as a draft, so the draft interval starts at creation.
// The remaining interval of a still-draft PR is measured to its last update.
// ok is false when the PR has no draft/ready timeline data at all.
func draftDuration(pr *github.PullRequest) (time.Duration, bool) {
	if len(pr.Timeline) == 0 {
		return 0, false
	}
	events := make([]github.TimelineEvent, len(pr.Timeline))
	copy(events, pr.Timeline)
	sort.Slice(events, func(i, j int) bool { return events[i].CreatedAt.Before(events[j].CreatedAt) })

	var total time.Duration
	var draftStart time.Time
	inDraft := false
	for _, ev := range events {
		switch ev.Kind {
		case github.TimelineConvertedToDraft:
			if !inDraft {
				inDraft = true
				draftStart = ev.CreatedAt
			}
		case github.TimelineReadyForReview:
			if inDraft {
				total += durationSince(draftStart, ev.CreatedAt)
				inDraft = false
			} else {
				// No convert-to-draft before this event: the PR started as
				// a draft when it was created.
				total += durationSince(pr.CreatedAt, ev.CreatedAt)
			}
		}
	}
	if inDraft {
		total += durationSince(draftStart, pr.UpdatedAt)
	}
	return total, true
}

// earliestReviewBy returns the earliest review the given reviewer submitted at
// or after at (zero time = no bound), or false when there is none.
func earliestReviewBy(reviews []reviewActivity, login string, at time.Time) (time.Time, bool) {
	var best time.Time
	for _, r := range reviews {
		if r.Login != login {
			continue
		}
		if !at.IsZero() && r.SubmittedAt.Before(at) {
			continue
		}
		if best.IsZero() || r.SubmittedAt.Before(best) {
			best = r.SubmittedAt
		}
	}
	return best, !best.IsZero()
}

// buildReport filters the given PRs and computes every statistic rendered by
// the HTML template.
// reportEntry is the per-PR bookkeeping used while assembling the report.
type reportEntry struct {
	item *repoPR
	// reviewers maps a reviewer login to their first response time, the
	// earlier of their first issue comment and their first submitted
	// review.
	reviewers map[string]time.Time
	// commentCounts counts non-author issue comments per reviewer login.
	commentCounts map[string]int
	// reviews holds the PR's submitted reviews (author and bots excluded,
	// honouring the reviewer filter).
	reviews             []reviewActivity
	firstReviewAt       time.Time
	firstApproveAt      time.Time
	hasChangesRequested bool
	mergeDuration       time.Duration
	closeDuration       time.Duration
	draftDuration       time.Duration
	draftKnown          bool
	requestTimes        map[string][]time.Time // reviewer -> request event times (re-requests included)
	commentsReceived    int                    // non-author issue comments on the PR
	sizeLines           int
	sizeKnown           bool
	hasNewData          bool // PR carries review, size or timeline data
}

// authorStats aggregates one author's merged PRs, lead times, and monthly
// activity.
type authorStats struct {
	merged      int
	opened      int
	closed      int
	noReview    int
	commentsRcv int
	additions   int
	deletions   int
	hasSize     bool
	sizes       [sizeBucketCount]int
	mergeDurs   []time.Duration
	draftDurs   []time.Duration
	firstRev    []time.Duration
	approveDurs []time.Duration
	// approveMergeDurs measures from the first approving review to the
	// merge, for PRs approved before they were merged.
	approveMergeDurs []time.Duration
	// monthOpened/monthMerged/monthClosed bucket the author's PRs by
	// creation, merge and close month respectively, feeding the
	// per-author trend charts.
	monthOpened map[string]int
	monthMerged map[string]int
	monthClosed map[string]int
}

// engagementStats aggregates one reviewer's comment and review activity.
type engagementStats struct {
	prsCommented int
	comments     int
	prsReviewed  int
	reviews      int
	approvals    int
	changesReq   int
	commentOnly  int
	openResp     []time.Duration
	reqResp      []time.Duration
	rereqResp    []time.Duration
}

// reportBuilder holds the state shared between report sections. Each section
// is a method, so a change to one metric cannot spill into another.
type reportBuilder struct {
	report  *Report
	filters statsFilters

	entries []*reportEntry
	authors []string

	authorStats    map[string]*authorStats
	engagement     map[string]*engagementStats
	mergeAll       []time.Duration
	sizeCounts     [sizeBucketCount]int
	sizeMergeDurs  [][]time.Duration
	monthOpened    map[string]int
	monthMerged    map[string]int
	monthMergeDurs map[string][]time.Duration
	activityHours  []int
	noDataCount    int

	now time.Time
	top int
}

// buildReport assembles the report one section at a time.
func buildReport(repos []*gitremote.Repo, repoPRs []*repoPR, f statsFilters) *Report {
	b := newReportBuilder(repos, f)
	b.collectEntries(repoPRs)
	b.buildMatrix()
	b.aggregateReviews()
	b.buildMergeStats()
	b.buildLeadTime()
	b.buildContributions()
	b.buildEngagement()
	b.buildSizes()
	b.buildTrends()
	b.buildNotables()
	b.buildPRAppendix()
	b.finish()
	return b.report
}

func newReportBuilder(repos []*gitremote.Repo, f statsFilters) *reportBuilder {
	r := &Report{
		State:       f.State,
		IncludeBots: f.IncludeBots,
		Matrix:      &Matrix{},
	}

	r.Repos = make([]string, 0, len(repos))
	r.RepoIDs = make([]string, 0, len(repos))
	for _, repo := range repos {
		r.Repos = append(r.Repos, repo.Owner+"/"+repo.Name)
		r.RepoIDs = append(r.RepoIDs, repo.Host+"/"+repo.Owner+"/"+repo.Name)
	}

	top := f.Top
	if top <= 0 {
		top = 5
	}
	r.TopN = top

	return &reportBuilder{report: r, filters: f, top: top}
}

// collectEntries filters the PRs to the report's period and state, and parses
// the per-PR facts every later section reads.
func (b *reportBuilder) collectEntries(repoPRs []*repoPR) {
	r := b.report
	f := b.filters

	var entries []*reportEntry
	for _, rp := range repoPRs {
		pr := rp.PR
		if !prInPeriod(pr, f) || !prMatchesState(pr, f.State) {
			continue
		}
		if len(f.Authors) > 0 && !f.Authors[strings.ToLower(pr.Author.Login)] {
			continue
		}

		e := &reportEntry{
			item:          rp,
			reviewers:     map[string]time.Time{},
			commentCounts: map[string]int{},
			requestTimes:  map[string][]time.Time{},
		}
		for _, c := range pr.Comments {
			if !statReviewerAllowed(c.Author, pr, f) {
				continue
			}
			lower := strings.ToLower(c.Author.Login)
			e.commentCounts[lower]++
			if _, ok := e.reviewers[lower]; !ok || c.CreatedAt.Before(e.reviewers[lower]) {
				e.reviewers[lower] = c.CreatedAt
			}
		}
		for _, rev := range pr.Reviews {
			if !statReviewerAllowed(rev.Author, pr, f) {
				continue
			}
			switch strings.ToUpper(rev.State) {
			case "APPROVED", "CHANGES_REQUESTED", "COMMENTED":
			default:
				continue // DISMISSED, PENDING and unknown states are not activity
			}
			e.reviews = append(e.reviews, reviewActivity{Login: strings.ToLower(rev.Author.Login), State: strings.ToUpper(rev.State), SubmittedAt: rev.SubmittedAt})
			// A submitted review is reviewer activity even when the reviewer
			// left no issue comment: approvals, change requests and inline
			// review comments all arrive as review objects, not comments.
			login := strings.ToLower(rev.Author.Login)
			if first, ok := e.reviewers[login]; !ok || rev.SubmittedAt.Before(first) {
				e.reviewers[login] = rev.SubmittedAt
			}
			if e.firstReviewAt.IsZero() || rev.SubmittedAt.Before(e.firstReviewAt) {
				e.firstReviewAt = rev.SubmittedAt
			}
			if strings.EqualFold(rev.State, "APPROVED") {
				if e.firstApproveAt.IsZero() || rev.SubmittedAt.Before(e.firstApproveAt) {
					e.firstApproveAt = rev.SubmittedAt
				}
			}
			if strings.EqualFold(rev.State, "CHANGES_REQUESTED") {
				e.hasChangesRequested = true
			}
		}
		for _, ev := range pr.Timeline {
			if ev.Kind != github.TimelineReviewRequested {
				continue
			}
			if !statReviewerAllowed(ev.RequestedReviewer, pr, f) {
				continue
			}
			login := strings.ToLower(ev.RequestedReviewer.Login)
			e.requestTimes[login] = append(e.requestTimes[login], ev.CreatedAt)
		}
		e.commentsReceived = len(pr.Comments) - authorComments(pr)
		if pr.MergedAt != nil {
			e.mergeDuration = durationSince(pr.CreatedAt, *pr.MergedAt)
		} else if pr.ClosedAt != nil && strings.EqualFold(pr.State, "CLOSED") {
			e.closeDuration = durationSince(pr.CreatedAt, *pr.ClosedAt)
		}
		e.draftDuration, e.draftKnown = draftDuration(pr)
		e.sizeLines = pr.Additions + pr.Deletions
		e.sizeKnown = pr.Additions > 0 || pr.Deletions > 0
		if e.reviews != nil || e.sizeKnown || len(pr.Timeline) > 0 {
			// At least one new-style field carried data; the PR is not from a
			// pre-analytics cache.
			e.hasNewData = true
		}

		entries = append(entries, e)
		r.TotalPRs++
		switch strings.ToUpper(pr.State) {
		case "MERGED":
			r.Merged++
		case "OPEN":
			r.Open++
		case "CLOSED":
			r.Closed++
		}
	}

	b.entries = entries
}

// buildMatrix produces the author x reviewer matrix and the per-author and
// per-reviewer summary tables.
func (b *reportBuilder) buildMatrix() {
	r := b.report
	entries := b.entries

	firstResponseAll := []time.Duration{}
	// reviewerReviewed and reviewerReviewFirst are review-only, for the
	// per-reviewer table: PRs where the reviewer submitted a review, and the
	// first response time on those PRs. Issue-comment activity is kept
	// separate in the engagement table.
	reviewerReviewed := map[string]int{}
	reviewerReviewFirst := map[string][]time.Duration{}

	// Reviewer aggregation per PR: fold into the matrix accumulators.
	type combo struct {
		count     int
		durations []time.Duration
	}
	matrixCombos := map[string]*combo{} // author\x00reviewer
	authorTotals := map[string]int{}    // author -> PRs in period
	reviewerTotals := map[string]int{}  // reviewer -> PRs reviewed
	noReviewCounts := map[string]int{}  // author -> PRs without reviewer activity

	for _, e := range entries {
		author := strings.ToLower(e.item.PR.Author.Login)
		authorTotals[author]++
		if len(e.reviewers) == 0 {
			noReviewCounts[author]++
		}
		for reviewer, first := range e.reviewers {
			c, ok := matrixCombos[author+"\x00"+reviewer]
			if !ok {
				c = &combo{}
				matrixCombos[author+"\x00"+reviewer] = c
			}
			c.count++
			c.durations = append(c.durations, durationSince(e.item.PR.CreatedAt, first))
			reviewerTotals[reviewer]++
		}
		// Review-only counts for the per-reviewer table: one per PR, even
		// when the reviewer submitted several reviews on it.
		reviewedBy := map[string]bool{}
		for _, rev := range e.reviews {
			if reviewedBy[rev.Login] {
				continue
			}
			reviewedBy[rev.Login] = true
			reviewerReviewed[rev.Login]++
			if first, ok := e.reviewers[rev.Login]; ok {
				reviewerReviewFirst[rev.Login] = append(reviewerReviewFirst[rev.Login], durationSince(e.item.PR.CreatedAt, first))
			}
		}
		if len(e.reviewers) > 0 {
			d := earliestResponse(e.reviewers, e.item.PR.CreatedAt)
			firstResponseAll = append(firstResponseAll, d)
		}
	}

	// Column order: reviewers sorted by PRs responded to desc, then login.
	reviewers := make([]string, 0, len(reviewerTotals))
	for reviewer := range reviewerTotals {
		reviewers = append(reviewers, reviewer)
	}
	sort.Slice(reviewers, func(i, j int) bool {
		if reviewerTotals[reviewers[i]] != reviewerTotals[reviewers[j]] {
			return reviewerTotals[reviewers[i]] > reviewerTotals[reviewers[j]]
		}
		return reviewers[i] < reviewers[j]
	})

	// Row order: authors sorted by PRs in period desc, then login.
	authors := make([]string, 0, len(authorTotals))
	for author := range authorTotals {
		authors = append(authors, author)
	}
	sort.Slice(authors, func(i, j int) bool {
		if authorTotals[authors[i]] != authorTotals[authors[j]] {
			return authorTotals[authors[i]] > authorTotals[authors[j]]
		}
		return authors[i] < authors[j]
	})

	maxCount := 0
	for _, c := range matrixCombos {
		if c.count > maxCount {
			maxCount = c.count
		}
	}

	r.AuthorCount = len(authors)
	r.ReviewerCount = len(reviewers)

	r.Matrix.Cols = make([]*MatrixCol, 0, len(reviewers)+1)
	r.Matrix.Cols = append(r.Matrix.Cols, &MatrixCol{Reviewer: "(no reviews)", Total: 0, NoReviews: true})
	for _, reviewer := range reviewers {
		r.Matrix.Cols = append(r.Matrix.Cols, &MatrixCol{Reviewer: reviewer, Total: reviewerTotals[reviewer]})
	}

	r.Matrix.Rows = make([]*MatrixRow, 0, len(authors))
	for _, author := range authors {
		row := &MatrixRow{Author: author, Total: authorTotals[author]}
		row.Cells = append(row.Cells, &MatrixCell{Count: noReviewCounts[author]})
		for _, reviewer := range reviewers {
			c := matrixCombos[author+"\x00"+reviewer]
			cell := &MatrixCell{}
			if c != nil {
				cell.Count = c.count
				cell.Avg = formatStatDuration(avgDuration(c.durations))
				cell.Median = formatStatDuration(medianDuration(c.durations))
				cell.Pct = formatReviewPct(c.count, row.Total)
				cell.Bg = heatColor(c.count, maxCount)
			}
			row.Cells = append(row.Cells, cell)
		}
		r.Matrix.Rows = append(r.Matrix.Rows, row)
	}

	// Per-author table.
	r.AuthorRows = make([]*AuthorSummary, 0, len(authors))
	for _, author := range authors {
		row := &AuthorSummary{Login: author, PRs: authorTotals[author]}
		for _, e := range entries {
			if strings.ToLower(e.item.PR.Author.Login) != author {
				continue
			}
			if strings.EqualFold(e.item.PR.State, "MERGED") {
				row.Merged++
			}
		}
		// First-comment durations for this author's PRs.
		var ds []time.Duration
		for _, e := range entries {
			if strings.ToLower(e.item.PR.Author.Login) == author && len(e.reviewers) > 0 {
				ds = append(ds, earliestResponse(e.reviewers, e.item.PR.CreatedAt))
			}
		}
		if len(ds) > 0 {
			row.MedianFirstResponse = formatStatDuration(medianDuration(ds))
			row.AvgFirstResponse = formatStatDuration(avgDuration(ds))
		}
		r.AuthorRows = append(r.AuthorRows, row)
	}

	// Per-reviewer table: review-only, so it does not duplicate the
	// comment-based figures in the engagement table. Comment-only responders
	// keep their matrix column but get no row here.
	reviewedLogins := make([]string, 0, len(reviewerReviewed))
	for reviewer := range reviewerReviewed {
		reviewedLogins = append(reviewedLogins, reviewer)
	}
	sort.Slice(reviewedLogins, func(i, j int) bool {
		if reviewerReviewed[reviewedLogins[i]] != reviewerReviewed[reviewedLogins[j]] {
			return reviewerReviewed[reviewedLogins[i]] > reviewerReviewed[reviewedLogins[j]]
		}
		return reviewedLogins[i] < reviewedLogins[j]
	})
	r.ReviewerRows = make([]*ReviewerSummary, 0, len(reviewedLogins))
	for _, reviewer := range reviewedLogins {
		ds := reviewerReviewFirst[reviewer]
		row := &ReviewerSummary{Login: reviewer, PRsReviewed: reviewerReviewed[reviewer]}
		if len(ds) > 0 {
			row.Median = formatStatDuration(medianDuration(ds))
			row.Avg = formatStatDuration(avgDuration(ds))
		}
		r.ReviewerRows = append(r.ReviewerRows, row)
	}

	if len(firstResponseAll) > 0 {
		r.OverallMedian = formatStatDuration(medianDuration(firstResponseAll))
		r.OverallAvg = formatStatDuration(avgDuration(firstResponseAll))
	} else {
		r.OverallMedian = "-"
		r.OverallAvg = "-"
	}

	b.authors = authors
}

// aggregateReviews folds the per-PR facts into author, reviewer, merge-speed,
// size, month, and hour aggregates.
func (b *reportBuilder) aggregateReviews() {
	r := b.report
	entries := b.entries
	b.now = time.Now()

	authorAggs := map[string]*authorStats{}

	engagement := map[string]*engagementStats{}
	ensureEng := func(login string) *engagementStats {
		g := engagement[login]
		if g == nil {
			g = &engagementStats{}
			engagement[login] = g
		}
		return g
	}

	mergeAll := []time.Duration{}
	sizeCounts := [sizeBucketCount]int{}
	sizeMergeDurs := make([][]time.Duration, sizeBucketCount)
	monthOpened := map[string]int{}
	monthMerged := map[string]int{}
	monthMergeDurs := map[string][]time.Duration{}
	activityHours := make([]int, 24)
	noDataCount := 0

	for _, e := range entries {
		pr := e.item.PR
		author := strings.ToLower(pr.Author.Login)
		agg := authorAggs[author]
		if agg == nil {
			agg = &authorStats{}
			authorAggs[author] = agg
		}
		agg.opened++
		agg.commentsRcv += e.commentsReceived
		if len(e.reviewers) == 0 {
			agg.noReview++
		}
		if e.sizeKnown {
			agg.hasSize = true
			agg.additions += pr.Additions
			agg.deletions += pr.Deletions
			agg.sizes[sizeBucket(e.sizeLines)]++
		}
		if !e.hasNewData {
			noDataCount++
		}
		r.TotalReviews += len(e.reviews)
		r.TotalAdditions += pr.Additions
		r.TotalDeletions += pr.Deletions

		merged := pr.MergedAt != nil && strings.EqualFold(pr.State, "MERGED")
		if agg.monthOpened == nil {
			agg.monthOpened = map[string]int{}
			agg.monthMerged = map[string]int{}
			agg.monthClosed = map[string]int{}
		}
		agg.monthOpened[pr.CreatedAt.Format("2006-01")]++
		if merged {
			agg.merged++
			agg.mergeDurs = append(agg.mergeDurs, e.mergeDuration)
			agg.monthMerged[pr.MergedAt.Format("2006-01")]++
			mergeAll = append(mergeAll, e.mergeDuration)
			r.MergeTrendSamples = append(r.MergeTrendSamples, [2]float64{
				float64(pr.MergedAt.Unix()),
				e.mergeDuration.Minutes(),
			})
			if e.sizeKnown {
				b := sizeBucket(e.sizeLines)
				sizeCounts[b]++
				sizeMergeDurs[b] = append(sizeMergeDurs[b], e.mergeDuration)
			}
			if e.draftKnown {
				agg.draftDurs = append(agg.draftDurs, e.draftDuration)
			}
			if !e.firstReviewAt.IsZero() {
				agg.firstRev = append(agg.firstRev, durationSince(pr.CreatedAt, e.firstReviewAt))
			}
			if !e.firstApproveAt.IsZero() {
				agg.approveDurs = append(agg.approveDurs, durationSince(pr.CreatedAt, e.firstApproveAt))
				// Approval-to-merge is only defined when the approval
				// preceded the merge (a post-merge approval is not part of
				// the lead time).
				if e.firstApproveAt.Before(*pr.MergedAt) {
					agg.approveMergeDurs = append(agg.approveMergeDurs, durationSince(e.firstApproveAt, *pr.MergedAt))
				}
			}
		} else if strings.EqualFold(pr.State, "CLOSED") {
			agg.closed++
			if pr.ClosedAt != nil {
				agg.monthClosed[pr.ClosedAt.Format("2006-01")]++
			}
		}

		monthOpened[pr.CreatedAt.Format("2006-01")]++
		activityHours[pr.CreatedAt.Hour()%24]++
		if merged {
			mm := pr.MergedAt.Format("2006-01")
			monthMerged[mm]++
			monthMergeDurs[mm] = append(monthMergeDurs[mm], e.mergeDuration)
		}

		// Comment-based engagement: issue comments only. A reviewer who only
		// submitted a formal review must not be counted as a commenter.
		for login, count := range e.commentCounts {
			if count == 0 {
				continue
			}
			g := ensureEng(login)
			g.prsCommented++
			g.comments += count
		}
		perReviewer := map[string][]reviewActivity{}
		for _, rev := range e.reviews {
			g := ensureEng(rev.Login)
			g.reviews++
			switch rev.State {
			case "APPROVED":
				g.approvals++
			case "CHANGES_REQUESTED":
				g.changesReq++
			case "COMMENTED":
				g.commentOnly++
			}
			perReviewer[rev.Login] = append(perReviewer[rev.Login], rev)
		}
		for login, revs := range perReviewer {
			g := ensureEng(login)
			g.prsReviewed++
			if first, ok := earliestReviewBy(revs, login, time.Time{}); ok {
				g.openResp = append(g.openResp, durationSince(pr.CreatedAt, first))
			}
			// Split the reviewer's request events into the initial request
			// and re-requests (events after a review already happened).
			requests := append([]time.Time(nil), e.requestTimes[login]...)
			sort.Slice(requests, func(i, j int) bool { return requests[i].Before(requests[j]) })
			if len(requests) > 0 {
				firstReq := requests[0]
				if firstReview, ok := earliestReviewBy(revs, login, firstReq); ok {
					g.reqResp = append(g.reqResp, durationSince(firstReq, firstReview))
					for _, reqAt := range requests[1:] {
						if reqAt.After(firstReview) {
							if next, ok := earliestReviewBy(revs, login, reqAt); ok {
								g.rereqResp = append(g.rereqResp, durationSince(reqAt, next))
							}
						}
					}
				}
			}
		}
	}

	if len(mergeAll) > 0 {
		r.MedianMerge = formatStatDuration(medianDuration(mergeAll))
	} else {
		r.MedianMerge = "-"
	}

	b.authorStats = authorAggs
	b.engagement = engagement
	b.mergeAll = mergeAll
	b.sizeCounts = sizeCounts
	b.sizeMergeDurs = sizeMergeDurs
	b.monthOpened = monthOpened
	b.monthMerged = monthMerged
	b.monthMergeDurs = monthMergeDurs
	b.activityHours = activityHours
	b.noDataCount = noDataCount
}

func (b *reportBuilder) buildMergeStats() {
	r := b.report
	mergeAll := b.mergeAll
	noDataCount := b.noDataCount

	// Merge speed: cumulative time-to-merge buckets and percentiles, over
	// every merged PR in the period.
	if len(mergeAll) > 0 {
		speed := mergeSpeedRows(mergeAll)
		pctiles := mergePercentileRows(mergeAll)
		r.MergeSpeedRows = speed
		r.MergePercentileRows = pctiles
	}

	// Flag a mostly-stale cache: PRs legitimately without analytics data
	// (empty diffs) exist, so only a large share triggers the notice.
	r.LegacyCache = r.TotalPRs > 0 && noDataCount*5 > r.TotalPRs
}

func (b *reportBuilder) buildLeadTime() {
	r := b.report
	authors := b.authors
	authorAggs := b.authorStats

	// Lead time rows: authors with merged PRs, busiest first.
	for _, author := range authors {
		agg := authorAggs[author]
		if agg == nil || len(agg.mergeDurs) == 0 {
			continue
		}
		row := &LeadTimeRow{
			Login:       author,
			MergedCount: len(agg.mergeDurs),
			MedianMerge: formatStatDuration(medianDuration(agg.mergeDurs)),
			AvgMerge:    formatStatDuration(avgDuration(agg.mergeDurs)),
		}
		if len(agg.draftDurs) > 0 {
			row.MedianDraft = formatStatDuration(medianDuration(agg.draftDurs))
		}
		if len(agg.firstRev) > 0 {
			row.MedianFirstReview = formatStatDuration(medianDuration(agg.firstRev))
		}
		if len(agg.approveDurs) > 0 {
			row.MedianApprove = formatStatDuration(medianDuration(agg.approveDurs))
		}
		if len(agg.approveMergeDurs) > 0 {
			row.MedianApproveMerge = formatStatDuration(medianDuration(agg.approveMergeDurs))
		}
		r.LeadRows = append(r.LeadRows, row)
	}

	// Per-author merge speed: the same window and percentile stats as the
	// overall merge-speed section, restricted to each author's merged PRs.
	for _, author := range authors {
		agg := authorAggs[author]
		if agg == nil || len(agg.mergeDurs) == 0 {
			continue
		}
		speed := mergeSpeedRows(agg.mergeDurs)
		pctiles := mergePercentileRows(agg.mergeDurs)
		r.AuthorMergeSpeed = append(r.AuthorMergeSpeed, &AuthorMergeSpeedRow{
			Login:       author,
			MergedCount: len(agg.mergeDurs),
			Within1h:    fmt.Sprintf("%d (%s)", speed[0].Count, speed[0].Pct),
			Within1d:    fmt.Sprintf("%d (%s)", speed[1].Count, speed[1].Pct),
			Within1w:    fmt.Sprintf("%d (%s)", speed[2].Count, speed[2].Pct),
			P50:         pctiles[0].Value,
			P90:         pctiles[1].Value,
			P99:         pctiles[2].Value,
		})
	}
}

func (b *reportBuilder) buildContributions() {
	r := b.report
	authors := b.authors
	authorAggs := b.authorStats

	// Contribution rows: one per author, in the same order as the matrix.
	for _, author := range authors {
		agg := authorAggs[author]
		if agg == nil {
			continue
		}
		rate := "-"
		if agg.opened > 0 {
			rate = fmt.Sprintf("%d%%", agg.merged*100/agg.opened)
		}
		row := &ContributionRow{
			Login:            author,
			Opened:           agg.opened,
			Merged:           agg.merged,
			Closed:           agg.closed,
			MergeRate:        rate,
			NoReview:         agg.noReview,
			CommentsReceived: agg.commentsRcv,
			Additions:        agg.additions,
			Deletions:        agg.deletions,
			HasSizeData:      agg.hasSize,
			SizeCounts:       agg.sizes,
		}
		r.ContributionRows = append(r.ContributionRows, row)
	}
}

func (b *reportBuilder) buildEngagement() {
	r := b.report
	engagement := b.engagement

	// Engagement rows: comment-based and review-based reviewers combined.
	engRows := make([]*ReviewerEngagementRow, 0, len(engagement))
	for login, g := range engagement {
		row := &ReviewerEngagementRow{
			Login:            login,
			PRsCommented:     g.prsCommented,
			Comments:         g.comments,
			PRsReviewed:      g.prsReviewed,
			Reviews:          g.reviews,
			Approvals:        g.approvals,
			ChangesRequested: g.changesReq,
			CommentOnly:      g.commentOnly,
		}
		if len(g.openResp) > 0 {
			row.MedianOpenResponse = formatStatDuration(medianDuration(g.openResp))
		}
		if len(g.reqResp) > 0 {
			row.MedianRequestResponse = formatStatDuration(medianDuration(g.reqResp))
		}
		if len(g.rereqResp) > 0 {
			row.MedianRerequestResponse = formatStatDuration(medianDuration(g.rereqResp))
		}
		engRows = append(engRows, row)
	}
	sort.Slice(engRows, func(i, j int) bool {
		ti := engRows[i].Reviews + engRows[i].Comments
		tj := engRows[j].Reviews + engRows[j].Comments
		if ti != tj {
			return ti > tj
		}
		return engRows[i].Login < engRows[j].Login
	})
	r.EngagementRows = engRows
}

func (b *reportBuilder) buildSizes() {
	r := b.report
	sizeCounts := b.sizeCounts
	sizeMergeDurs := b.sizeMergeDurs

	// Size buckets: all five rows for a stable table.
	for i, name := range sizeBucketNames {
		row := &SizeRow{Bucket: name, Count: sizeCounts[i]}
		if ds := sizeMergeDurs[i]; len(ds) > 0 {
			row.MergedCount = len(ds)
			row.MedianMerge = formatStatDuration(medianDuration(ds))
		}
		r.SizeRows = append(r.SizeRows, row)
	}
}

func (b *reportBuilder) buildTrends() {
	r := b.report
	authors := b.authors
	authorAggs := b.authorStats
	monthOpened := b.monthOpened
	monthMerged := b.monthMerged
	monthMergeDurs := b.monthMergeDurs
	activityHours := b.activityHours

	// Monthly trends across the union of opened and merged months.
	monthSet := map[string]bool{}
	for m := range monthOpened {
		monthSet[m] = true
	}
	for m := range monthMerged {
		monthSet[m] = true
	}
	months := make([]string, 0, len(monthSet))
	for m := range monthSet {
		months = append(months, m)
	}
	sort.Strings(months)
	for _, m := range months {
		row := &MonthTrend{Month: m, Opened: monthOpened[m], Merged: monthMerged[m]}
		if ds := monthMergeDurs[m]; len(ds) > 0 {
			row.MedianMerge = formatStatDuration(medianDuration(ds))
			row.medianHrs = medianDuration(ds).Hours()
		}
		r.Trends = append(r.Trends, row)
	}
	r.ActivityHours = activityHours

	// Per-author monthly trends: one chart per author showing how many PRs
	// they opened, merged and closed each month.
	for _, author := range authors {
		agg := authorAggs[author]
		if agg == nil {
			continue
		}
		monthSet := map[string]bool{}
		for m := range agg.monthOpened {
			monthSet[m] = true
		}
		for m := range agg.monthMerged {
			monthSet[m] = true
		}
		for m := range agg.monthClosed {
			monthSet[m] = true
		}
		if len(monthSet) == 0 {
			continue
		}
		authorMonths := make([]string, 0, len(monthSet))
		for m := range monthSet {
			authorMonths = append(authorMonths, m)
		}
		sort.Strings(authorMonths)
		trend := &AuthorTrend{Login: author}
		for _, m := range authorMonths {
			trend.Months = append(trend.Months, &AuthorMonthStat{
				Month:  m,
				Opened: agg.monthOpened[m],
				Merged: agg.monthMerged[m],
				Closed: agg.monthClosed[m],
			})
		}
		r.AuthorTrends = append(r.AuthorTrends, trend)
	}
}

func (b *reportBuilder) buildNotables() {
	r := b.report
	entries := b.entries
	now := b.now
	top := b.top

	// Notable PR lists: outstanding cases first, ranked by the list metric.
	type notable struct {
		row *NotablePR
		key time.Duration
	}
	var awaiting, longest, mostCommented []notable
	for _, e := range entries {
		pr := e.item.PR
		base := func(value string) *NotablePR {
			return &NotablePR{Repo: e.item.Repo, Number: pr.Number, Title: pr.Title, URL: pr.URL, Author: pr.Author.Login, Value: value}
		}
		if strings.EqualFold(pr.State, "OPEN") && len(e.reviewers) == 0 {
			age := now.Sub(pr.CreatedAt)
			if age < 0 {
				age = 0
			}
			awaiting = append(awaiting, notable{row: base(formatStatDuration(age) + " awaiting"), key: age})
		}
		if pr.MergedAt != nil && strings.EqualFold(pr.State, "MERGED") {
			longest = append(longest, notable{row: base(formatStatDuration(e.mergeDuration) + " to merge"), key: e.mergeDuration})
		}
		comments := pr.CommentCount
		if comments == 0 {
			comments = len(pr.Comments)
		}
		if comments > 0 {
			mostCommented = append(mostCommented, notable{
				row: base(fmt.Sprintf("%d comments", comments)),
				key: time.Duration(comments),
			})
		}
	}
	pickNotable := func(list []notable) []*NotablePR {
		sort.Slice(list, func(i, j int) bool {
			if list[i].key != list[j].key {
				return list[i].key > list[j].key
			}
			if list[i].row.Repo != list[j].row.Repo {
				return list[i].row.Repo < list[j].row.Repo
			}
			return list[i].row.Number < list[j].row.Number
		})
		if len(list) > top {
			list = list[:top]
		}
		rows := make([]*NotablePR, 0, len(list))
		for _, n := range list {
			rows = append(rows, n.row)
		}
		return rows
	}
	r.NotableAwaiting = pickNotable(awaiting)
	r.NotableMerge = pickNotable(longest)
	r.NotableCommented = pickNotable(mostCommented)
}

func (b *reportBuilder) buildPRAppendix() {
	r := b.report
	f := b.filters
	entries := b.entries

	// Appendix: every PR in the period, only when requested since the table
	// can be very large.
	r.ShowPRs = f.ListPRs
	if f.ListPRs {
		r.PRs = make([]*PRRow, 0, len(entries))
		for _, e := range entries {
			pr := e.item.PR
			row := &PRRow{
				Repo:      e.item.Repo,
				Number:    pr.Number,
				Title:     pr.Title,
				URL:       pr.URL,
				Author:    pr.Author.Login,
				State:     strings.ToLower(pr.State),
				CreatedAt: pr.CreatedAt.Format("2006-01-02 15:04"),
			}
			if pr.MergedAt != nil {
				row.MergedAt = pr.MergedAt.Format("2006-01-02 15:04")
			}
			r.PRs = append(r.PRs, row)
		}
		sort.Slice(r.PRs, func(i, j int) bool {
			if r.PRs[i].Repo != r.PRs[j].Repo {
				return r.PRs[i].Repo < r.PRs[j].Repo
			}
			return r.PRs[i].Number < r.PRs[j].Number
		})
	}
}

func (b *reportBuilder) finish() {
	r := b.report
	f := b.filters

	r.Period = formatPeriod(f.From, f.To)
	if !f.From.IsZero() {
		r.FromValue = f.From.Format("2006-01-02")
	}
	if !f.To.IsZero() {
		r.ToValue = f.To.Format("2006-01-02")
	}
	r.GeneratedAt = time.Now().Format("2006-01-02 15:04")
}

// prInPeriod reports whether the PR's creation date falls inside the filter's
// period.
func prInPeriod(pr *github.PullRequest, f statsFilters) bool {
	if !f.From.IsZero() && pr.CreatedAt.Before(f.From) {
		return false
	}
	if !f.To.IsZero() && pr.CreatedAt.After(f.To) {
		return false
	}
	return true
}

// prMatchesState reports whether the PR matches the state filter, following
// the same semantics as the pr list command ("all" accepts everything).
func prMatchesState(pr *github.PullRequest, state string) bool {
	if state == "" || state == "all" {
		return true
	}
	return strings.EqualFold(pr.State, state)
}

// durationSince returns the non-negative duration from created to at.
func durationSince(created, at time.Time) time.Duration {
	d := at.Sub(created)
	if d < 0 {
		return 0
	}
	return d
}

// earliestResponse returns the shortest time from PR creation to any
// reviewer's first response (issue comment or submitted review).
func earliestResponse(reviewers map[string]time.Time, createdAt time.Time) time.Duration {
	best := time.Duration(0)
	first := true
	for _, at := range reviewers {
		d := durationSince(createdAt, at)
		if first || d < best {
			best = d
			first = false
		}
	}
	return best
}

// medianDuration computes the median of the given durations. The input slice
// is sorted in place.
func medianDuration(ds []time.Duration) time.Duration {
	n := len(ds)
	if n == 0 {
		return 0
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	if n%2 == 1 {
		return ds[n/2]
	}
	return (ds[n/2-1] + ds[n/2]) / 2
}

// avgDuration computes the mean of the given durations.
func avgDuration(ds []time.Duration) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range ds {
		total += d
	}
	return total / time.Duration(len(ds))
}

// percentileDuration returns the p-th percentile (0-100) of the given
// durations, interpolating linearly between the closest ranks. The input
// slice is sorted in place.
func percentileDuration(ds []time.Duration, p int) time.Duration {
	n := len(ds)
	if n == 0 {
		return 0
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	if n == 1 {
		return ds[0]
	}
	rank := float64(p) / 100 * float64(n-1)
	lo := int(math.Floor(rank))
	hi := lo + 1
	if hi > n-1 {
		hi = n - 1
	}
	frac := rank - float64(lo)
	return ds[lo] + time.Duration(float64(ds[hi]-ds[lo])*frac)
}

// mergeSpeedRows counts how many of the given merge durations fall within
// 1 hour, 1 day and 1 week, each with its share of the total.
func mergeSpeedRows(ds []time.Duration) []*MergeSpeedRow {
	var within1h, within1d, within1w int
	for _, d := range ds {
		if d <= time.Hour {
			within1h++
		}
		if d <= 24*time.Hour {
			within1d++
		}
		if d <= 7*24*time.Hour {
			within1w++
		}
	}
	pct := func(n int) string {
		return fmt.Sprintf("%d%%", n*100/len(ds))
	}
	return []*MergeSpeedRow{
		{Bucket: "Within 1 hour", Count: within1h, Pct: pct(within1h)},
		{Bucket: "Within 1 day", Count: within1d, Pct: pct(within1d)},
		{Bucket: "Within 1 week", Count: within1w, Pct: pct(within1w)},
	}
}

// mergePercentileRows formats the p50/p90/p99 of the given merge durations.
func mergePercentileRows(ds []time.Duration) []*MergePercentileRow {
	return []*MergePercentileRow{
		{Label: "p50", Value: formatStatDuration(percentileDuration(ds, 50))},
		{Label: "p90", Value: formatStatDuration(percentileDuration(ds, 90))},
		{Label: "p99", Value: formatStatDuration(percentileDuration(ds, 99))},
	}
}

// heatColor maps a cell count to a background colour, shading from pale to
// saturated as the count approaches the matrix maximum.
func heatColor(count, max int) template.CSS {
	if count <= 0 || max <= 0 {
		return ""
	}
	ratio := float64(count) / float64(max)
	lightness := 92 - int(ratio*32) // 92% (light) down to 60% (strong)
	return template.CSS(fmt.Sprintf("hsl(214, 80%%, %d%%)", lightness))
}

// formatPeriod renders the report period for the header.
func formatPeriod(from, to time.Time) string {
	switch {
	case from.IsZero() && to.IsZero():
		return "all time"
	case from.IsZero():
		return "up to " + to.Format("2006-01-02 15:04")
	case to.IsZero():
		return "since " + from.Format("2006-01-02 15:04")
	default:
		return from.Format("2006-01-02") + " to " + to.Format("2006-01-02 15:04")
	}
}

// formatDuration renders a duration in a compact human form (e.g. 2d 3h,
// 4h 12m, 35m).
func formatStatDuration(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

// formatReviewPct renders a cell's share of the row author's PRs as a
// percentage (e.g. "33%"), or "<1%" for small non-zero shares.
func formatReviewPct(count, total int) string {
	if total <= 0 || count <= 0 {
		return ""
	}
	pct := float64(count) / float64(total) * 100
	if pct < 1 {
		return "<1%"
	}
	return fmt.Sprintf("%.0f%%", pct)
}

// ---------------------------------------------------------------------------
// HTML rendering
// ---------------------------------------------------------------------------

//go:embed stats_template.html
var reportTemplate string

// reportView is the template-facing view of a Report, with pre-formatted
// labels.
type reportView struct {
	*Report
	ReposLabel string
	ChartsJSON template.JS
}

// renderReport renders the report to a complete HTML document.
func renderReport(r *Report) (string, error) {
	tmpl, err := template.New("report").Parse(reportTemplate)
	if err != nil {
		return "", err
	}
	view := &reportView{Report: r, ReposLabel: strings.Join(r.Repos, ", ")}
	payload := chartsPayload{Hours: r.ActivityHours, MergeSamples: r.MergeTrendSamples}
	for _, t := range r.Trends {
		payload.Months = append(payload.Months, t.Month)
		payload.Opened = append(payload.Opened, t.Opened)
		payload.Merged = append(payload.Merged, t.Merged)
		payload.MedianHrs = append(payload.MedianHrs, t.medianHrs)
	}
	for _, s := range r.SizeRows {
		payload.Sizes = append(payload.Sizes, s.Count)
	}
	for _, t := range r.AuthorTrends {
		at := authorTrendPayload{Login: t.Login}
		for _, m := range t.Months {
			at.Months = append(at.Months, m.Month)
			at.Opened = append(at.Opened, m.Opened)
			at.Merged = append(at.Merged, m.Merged)
			at.Closed = append(at.Closed, m.Closed)
		}
		payload.AuthorTrends = append(payload.AuthorTrends, at)
	}
	if b, err := json.Marshal(payload); err == nil {
		view.ChartsJSON = template.JS(b)
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, view); err != nil {
		return "", err
	}
	return sb.String(), nil
}
