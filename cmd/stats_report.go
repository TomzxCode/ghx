package cmd

import (
	"encoding/json"
	"fmt"
	"html/template"
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
	Reviewer   string
	Total      int
	NoComments bool
}

// Matrix is the author (rows) x reviewer (columns) table at the heart of the
// report.
type Matrix struct {
	Cols []*MatrixCol
	Rows []*MatrixRow
}

// AuthorSummary aggregates one author's PRs for the per-author table.
type AuthorSummary struct {
	Login              string
	PRs                int
	Merged             int
	MedianFirstComment string
	AvgFirstComment    string
}

// ReviewerSummary aggregates one reviewer's activity for the per-reviewer
// table.
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

// SizeRow aggregates one PR size bucket for the size-versus-lead-time table.
type SizeRow struct {
	Bucket      string // xs, s, m, l, xl
	Count       int
	MergedCount int
	MedianMerge string
}

// LeadTimeRow aggregates merged-PR lead times for one author.
type LeadTimeRow struct {
	Login             string
	MergedCount       int
	MedianMerge       string
	AvgMerge          string
	MedianDraft       string
	MedianFirstReview string
	MedianApprove     string
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
	Repos         []string
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

	MedianMerge      string
	TotalReviews     int
	TotalAdditions   int
	TotalDeletions   int
	LegacyCache      bool
	TopN             int
	LeadRows         []*LeadTimeRow
	ContributionRows []*ContributionRow
	EngagementRows   []*ReviewerEngagementRow
	SizeRows         []*SizeRow
	Trends           []*MonthTrend
	ActivityHours    []int
	NotableAwaiting  []*NotablePR
	NotableMerge     []*NotablePR
	NotableCommented []*NotablePR
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
}

// reviewerComment is a comment left on a PR by someone other than its author.
type reviewerComment struct {
	Login string
	At    time.Time
}

// reviewActivity is one submitted review on a PR, normalized for aggregation.
type reviewActivity struct {
	Login       string // lowercase reviewer login
	State       string // APPROVED, CHANGES_REQUESTED or COMMENTED
	SubmittedAt time.Time
}

// statReviewerAllowed reports whether a reviewer login participates in the
// report: bots are excluded unless requested, and an explicit reviewer filter
// restricts the set.
func statReviewerAllowed(login string, pr *github.PullRequest, f statsFilters) bool {
	lower := strings.ToLower(login)
	if lower == "" || lower == strings.ToLower(pr.Author.Login) {
		return false
	}
	if !f.IncludeBots && isBotLogin(login) {
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
func buildReport(repos []*gitremote.Repo, repoPRs []*repoPR, f statsFilters) *Report {
	r := &Report{
		State:       f.State,
		IncludeBots: f.IncludeBots,
		Matrix:      &Matrix{},
	}

	r.Repos = make([]string, 0, len(repos))
	for _, repo := range repos {
		r.Repos = append(r.Repos, repo.Owner+"/"+repo.Name)
	}

	// Per-PR bookkeeping.
	type prEntry struct {
		item      *repoPR
		reviewers map[string]time.Time // reviewer login -> first comment time
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
	var entries []*prEntry
	firstCommentAll := []time.Duration{}
	reviewerFirst := map[string][]time.Duration{}

	for _, rp := range repoPRs {
		pr := rp.PR
		if !prInPeriod(pr, f) || !prMatchesState(pr, f.State) {
			continue
		}
		if len(f.Authors) > 0 && !f.Authors[strings.ToLower(pr.Author.Login)] {
			continue
		}

		e := &prEntry{
			item:          rp,
			reviewers:     map[string]time.Time{},
			commentCounts: map[string]int{},
			requestTimes:  map[string][]time.Time{},
		}
		for _, c := range pr.Comments {
			login := c.Author.Login
			lower := strings.ToLower(login)
			if lower == strings.ToLower(pr.Author.Login) {
				continue // the author's own comments are not reviews
			}
			if !statReviewerAllowed(login, pr, f) {
				continue
			}
			e.commentCounts[lower]++
			if _, ok := e.reviewers[lower]; !ok || c.CreatedAt.Before(e.reviewers[lower]) {
				e.reviewers[lower] = c.CreatedAt
			}
		}
		for _, rev := range pr.Reviews {
			if !statReviewerAllowed(rev.Author.Login, pr, f) {
				continue
			}
			switch strings.ToUpper(rev.State) {
			case "APPROVED", "CHANGES_REQUESTED", "COMMENTED":
			default:
				continue // DISMISSED, PENDING and unknown states are not activity
			}
			e.reviews = append(e.reviews, reviewActivity{Login: strings.ToLower(rev.Author.Login), State: strings.ToUpper(rev.State), SubmittedAt: rev.SubmittedAt})
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
			if !statReviewerAllowed(ev.RequestedReviewer.Login, pr, f) {
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

	// Reviewer aggregation per PR: fold into the matrix accumulators.
	type combo struct {
		count     int
		durations []time.Duration
	}
	matrixCombos := map[string]*combo{} // author\x00reviewer
	authorTotals := map[string]int{}    // author -> PRs in period
	reviewerTotals := map[string]int{}  // reviewer -> PRs reviewed
	noCommentCounts := map[string]int{} // author -> PRs without reviewer comments

	for _, e := range entries {
		author := strings.ToLower(e.item.PR.Author.Login)
		authorTotals[author]++
		if len(e.reviewers) == 0 {
			noCommentCounts[author]++
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
			reviewerFirst[reviewer] = append(reviewerFirst[reviewer], durationSince(e.item.PR.CreatedAt, first))
		}
		if len(e.reviewers) > 0 {
			d := earliestFirstComment(e.reviewers, e.item.PR.CreatedAt)
			firstCommentAll = append(firstCommentAll, d)
		}
	}

	// Column order: reviewers sorted by PRs reviewed desc, then login.
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
	r.Matrix.Cols = append(r.Matrix.Cols, &MatrixCol{Reviewer: "(no comments)", Total: 0, NoComments: true})
	for _, reviewer := range reviewers {
		r.Matrix.Cols = append(r.Matrix.Cols, &MatrixCol{Reviewer: reviewer, Total: reviewerTotals[reviewer]})
	}

	r.Matrix.Rows = make([]*MatrixRow, 0, len(authors))
	for _, author := range authors {
		row := &MatrixRow{Author: author, Total: authorTotals[author]}
		row.Cells = append(row.Cells, &MatrixCell{Count: noCommentCounts[author]})
		for _, reviewer := range reviewers {
			c := matrixCombos[author+"\x00"+reviewer]
			cell := &MatrixCell{}
			if c != nil {
				cell.Count = c.count
				cell.Avg = formatStatDuration(avgDuration(c.durations))
				cell.Median = formatStatDuration(medianDuration(c.durations))
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
				ds = append(ds, earliestFirstComment(e.reviewers, e.item.PR.CreatedAt))
			}
		}
		if len(ds) > 0 {
			row.MedianFirstComment = formatStatDuration(medianDuration(ds))
			row.AvgFirstComment = formatStatDuration(avgDuration(ds))
		}
		r.AuthorRows = append(r.AuthorRows, row)
	}

	// Per-reviewer table.
	r.ReviewerRows = make([]*ReviewerSummary, 0, len(reviewers))
	for _, reviewer := range reviewers {
		ds := reviewerFirst[reviewer]
		row := &ReviewerSummary{Login: reviewer, PRsReviewed: reviewerTotals[reviewer]}
		row.Median = formatStatDuration(medianDuration(ds))
		row.Avg = formatStatDuration(avgDuration(ds))
		r.ReviewerRows = append(r.ReviewerRows, row)
	}

	if len(firstCommentAll) > 0 {
		r.OverallMedian = formatStatDuration(medianDuration(firstCommentAll))
		r.OverallAvg = formatStatDuration(avgDuration(firstCommentAll))
	} else {
		r.OverallMedian = "-"
		r.OverallAvg = "-"
	}

	// ------------------------------------------------------------------
	// Review analytics: lead time, contribution, engagement, trends.
	// ------------------------------------------------------------------
	now := time.Now()
	top := f.Top
	if top <= 0 {
		top = 5
	}
	r.TopN = top

	type authorAgg struct {
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
	}
	authorAggs := map[string]*authorAgg{}

	type engAgg struct {
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
	engagement := map[string]*engAgg{}
	ensureEng := func(login string) *engAgg {
		g := engagement[login]
		if g == nil {
			g = &engAgg{}
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
			agg = &authorAgg{}
			authorAggs[author] = agg
		}
		agg.opened++
		agg.commentsRcv += e.commentsReceived
		if len(e.reviewers) == 0 && len(e.reviews) == 0 {
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
		if merged {
			agg.merged++
			agg.mergeDurs = append(agg.mergeDurs, e.mergeDuration)
			mergeAll = append(mergeAll, e.mergeDuration)
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
			}
		} else if strings.EqualFold(pr.State, "CLOSED") {
			agg.closed++
		}

		monthOpened[pr.CreatedAt.Format("2006-01")]++
		activityHours[pr.CreatedAt.Hour()%24]++
		if merged {
			mm := pr.MergedAt.Format("2006-01")
			monthMerged[mm]++
			monthMergeDurs[mm] = append(monthMergeDurs[mm], e.mergeDuration)
		}

		// Reviewer engagement on this PR.
		for login := range e.reviewers {
			g := ensureEng(login)
			g.prsCommented++
			g.comments += e.commentCounts[login]
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

	// Flag a mostly-stale cache: PRs legitimately without analytics data
	// (empty diffs) exist, so only a large share triggers the notice.
	r.LegacyCache = r.TotalPRs > 0 && noDataCount*5 > r.TotalPRs

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
		r.LeadRows = append(r.LeadRows, row)
	}

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

	// Size buckets: all five rows for a stable table.
	for i, name := range sizeBucketNames {
		row := &SizeRow{Bucket: name, Count: sizeCounts[i]}
		if ds := sizeMergeDurs[i]; len(ds) > 0 {
			row.MergedCount = len(ds)
			row.MedianMerge = formatStatDuration(medianDuration(ds))
		}
		r.SizeRows = append(r.SizeRows, row)
	}

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
		if strings.EqualFold(pr.State, "OPEN") && len(e.reviewers) == 0 && len(e.reviews) == 0 {
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

	r.Period = formatPeriod(f.From, f.To)
	r.GeneratedAt = time.Now().Format("2006-01-02 15:04")
	return r
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

// isBotLogin reports whether a login looks like a GitHub App bot, whose
// comments should not count as human reviews.
func isBotLogin(login string) bool {
	return strings.HasSuffix(login, "[bot]") || strings.HasSuffix(login, "-bot") || strings.EqualFold(login, "github-actions")
}

// durationSince returns the non-negative duration from created to at.
func durationSince(created, at time.Time) time.Duration {
	d := at.Sub(created)
	if d < 0 {
		return 0
	}
	return d
}

// earliestFirstComment returns the shortest time from PR creation to any
// reviewer's first comment.
func earliestFirstComment(reviewers map[string]time.Time, createdAt time.Time) time.Duration {
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

// ---------------------------------------------------------------------------
// HTML rendering
// ---------------------------------------------------------------------------

const reportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Repository activity report</title>
<link href="https://cdn.jsdelivr.net/npm/tom-select@2.6.2/dist/css/tom-select.css" rel="stylesheet">
<script src="https://cdn.jsdelivr.net/npm/tom-select@2.6.2/dist/js/tom-select.complete.min.js"></script>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4"></script>
<script>
(function() {
  var stored = null;
  try { stored = localStorage.getItem("ghx-theme"); } catch (e) {}
  var dark = stored ? stored === "dark" : window.matchMedia("(prefers-color-scheme: dark)").matches;
  if (dark) document.documentElement.classList.add("dark");
})();
</script>
<style>
  :root {
    color-scheme: light;
    --bg: #f6f8fa;
    --fg: #1f2328;
    --muted: #59636e;
    --border: #d0d7de;
    --table-border: #d8dee4;
    --surface: #ffffff;
    --surface-alt: #f6f8fa;
    --empty: #8c959f;
    --link: #0969da;
    --dur: #424a53;
    --state-merged: #8250df;
    --state-open: #1a7f37;
    --state-closed: #cf222e;
    --btn-bg: #ffffff;
  }
  html.dark {
    color-scheme: dark;
    --bg: #0d1117;
    --fg: #e6edf3;
    --muted: #8d96a0;
    --border: #30363d;
    --table-border: #30363d;
    --surface: #161b22;
    --surface-alt: #21262d;
    --empty: #6e7681;
    --link: #58a6ff;
    --dur: #a5b1bd;
    --state-merged: #ab7df8;
    --state-open: #3fb950;
    --state-closed: #f85149;
    --btn-bg: #21262d;
  }
  * { box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
    margin: 0; padding: 2rem; background: var(--bg); color: var(--fg);
  }
  .header { display: flex; align-items: center; justify-content: space-between; gap: 1rem; flex-wrap: wrap; }
  h1 { margin: 0 0 0.25rem; font-size: 1.5rem; }
  h2 { margin: 2.5rem 0 0.75rem; font-size: 1.15rem; border-bottom: 1px solid var(--border); padding-bottom: 0.35rem; }
  .meta { color: var(--muted); margin-bottom: 1.5rem; }
  #theme-toggle {
    background: var(--btn-bg); color: var(--fg); border: 1px solid var(--border); border-radius: 6px;
    padding: 0.35rem 0.9rem; font-size: 0.8rem; cursor: pointer; margin-bottom: 0.5rem;
  }
  #theme-toggle:hover { border-color: var(--muted); }
  .cards { display: flex; flex-wrap: wrap; gap: 0.75rem; margin-bottom: 0.5rem; }
  .card {
    background: var(--surface); border: 1px solid var(--border); border-radius: 8px;
    padding: 0.75rem 1.25rem; min-width: 8rem;
  }
  .card .value { font-size: 1.4rem; font-weight: 600; }
  .card .label { font-size: 0.75rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.04em; }
  table { border-collapse: collapse; background: var(--surface); border: 1px solid var(--border); border-radius: 8px; width: 100%; font-size: 0.85rem; }
  th, td { border: 1px solid var(--table-border); padding: 0.4rem 0.6rem; text-align: left; vertical-align: top; }
  th { background: var(--surface-alt); font-weight: 600; white-space: nowrap; }
  td.num, th.num { text-align: right; }
  .matrix td.cell { text-align: center; min-width: 5.5rem; }
  .matrix td .count { font-weight: 600; display: block; }
  .matrix td .dur { font-size: 0.72rem; color: var(--dur); display: block; }
  .matrix td.cell[style] { color: #1f2328; }
  .matrix td.cell[style] .dur { color: #3d4450; }
  .matrix td.total { background: var(--surface-alt); font-weight: 600; text-align: center; }
  .matrix th.author, .matrix td.author { white-space: nowrap; }
  .matrix-filters { display: flex; gap: 1rem; align-items: center; margin-bottom: 0.75rem; flex-wrap: wrap; }
  .matrix-filters label { font-size: 0.8rem; color: var(--muted); }
  .matrix-filters input {
    background: var(--surface); color: var(--fg); border: 1px solid var(--border); border-radius: 6px;
    padding: 0.3rem 0.6rem; font-size: 0.8rem; width: 12rem; margin-left: 0.4rem;
  }
  .matrix-filters input:focus { outline: none; border-color: var(--link); }
  #matrix-filter-hint { font-size: 0.75rem; color: var(--muted); }
  .empty { color: var(--empty); }
  .prlist td.num { white-space: nowrap; }
  a { color: var(--link); text-decoration: none; }
  a:hover { text-decoration: underline; }
  footer { margin-top: 2.5rem; color: var(--muted); font-size: 0.8rem; }
  .state-merged { color: var(--state-merged); font-weight: 600; }
  .state-open { color: var(--state-open); font-weight: 600; }
  .state-closed { color: var(--state-closed); font-weight: 600; }
  .matrix-filters .ts-wrapper { width: 12rem; }
  .ts-wrapper .ts-control {
    background: var(--surface); color: var(--fg); border-color: var(--border);
    border-radius: 6px; font-size: 0.8rem; min-height: 0; padding: 0.25rem 0.5rem;
  }
  .ts-wrapper.focus .ts-control { border-color: var(--link); box-shadow: none; background: var(--surface); }
  .ts-wrapper.multi .ts-control > div { background: var(--surface-alt); border-color: var(--border); color: var(--fg); }
  .ts-wrapper.multi .ts-control > div.active { background: var(--surface-alt); color: var(--fg); }
  .ts-wrapper .ts-control input { color: var(--fg); }
  .ts-wrapper .ts-control input::placeholder { color: var(--muted); }
  .ts-dropdown { background: var(--surface); color: var(--fg); border: 1px solid var(--border); border-radius: 6px; font-size: 0.8rem; }
  .ts-dropdown .option { color: var(--fg); }
  .ts-dropdown .option.active, .ts-dropdown .option:hover { background: var(--surface-alt); color: var(--fg); }
  .ts-dropdown .dropdown-input { background: var(--surface); color: var(--fg); border-color: var(--border); }
  .ts-dropdown .dropdown-input::placeholder { color: var(--muted); }
  .notice {
    background: var(--surface); border: 1px solid var(--border); border-left: 3px solid #d29922;
    border-radius: 6px; padding: 0.6rem 0.9rem; margin-bottom: 1.25rem; font-size: 0.85rem; color: var(--fg);
  }
  .chart-box { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 1rem; max-width: 900px; }
  .chart-wrap { position: relative; height: 340px; }
  .chart-note { font-size: 0.75rem; color: var(--muted); margin-top: 0.5rem; }
  .notable { display: flex; flex-wrap: wrap; gap: 1.5rem; }
  .notable > div { flex: 1 1 260px; min-width: 240px; }
  .notable h3 { font-size: 0.85rem; margin: 0 0 0.5rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.04em; }
  .notable ol { margin: 0; padding-left: 1.4rem; font-size: 0.85rem; }
  .notable li { margin-bottom: 0.4rem; }
  .notable .value { color: var(--muted); font-size: 0.75rem; display: block; }
  h2 .section-hint { font-size: 0.75rem; color: var(--muted); font-weight: 400; margin-left: 0.5rem; }
</style>
</head>
<body>
<div class="header">
  <h1>Repository activity report</h1>
  <button id="theme-toggle" type="button">Theme</button>
</div>
<div class="meta">
  Repos: {{.ReposLabel}} &nbsp;•&nbsp; Period: {{.Period}} &nbsp;•&nbsp; State: {{.State}} &nbsp;•&nbsp; Generated: {{.GeneratedAt}}
</div>

<div class="cards">
  <div class="card"><div class="value">{{.TotalPRs}}</div><div class="label">Pull requests</div></div>
  <div class="card"><div class="value">{{.Merged}}</div><div class="label">Merged</div></div>
  <div class="card"><div class="value">{{.Open}}</div><div class="label">Open</div></div>
  <div class="card"><div class="value">{{.Closed}}</div><div class="label">Closed (unmerged)</div></div>
  <div class="card"><div class="value">{{.AuthorCount}}</div><div class="label">Authors</div></div>
  <div class="card"><div class="value">{{.ReviewerCount}}</div><div class="label">Reviewers</div></div>
  <div class="card"><div class="value">{{.OverallMedian}}</div><div class="label">Median first comment</div></div>
  <div class="card"><div class="value">{{.OverallAvg}}</div><div class="label">Average first comment</div></div>
  <div class="card"><div class="value">{{.MedianMerge}}</div><div class="label">Median time to merge</div></div>
  <div class="card"><div class="value">{{.TotalReviews}}</div><div class="label">Reviews submitted</div></div>
</div>

{{if .LegacyCache}}
<div class="notice">Some pull requests were cached before review, size and timeline data was collected.
Run <code>ghx cache --force</code> for these repositories to enable the full set of metrics.</div>
{{end}}

{{if .Trends}}
<h2>Monthly trend</h2>
<div class="chart-box">
  <div class="chart-wrap"><canvas id="trend-chart"></canvas></div>
  <div class="chart-note">Pull requests opened and merged per month, with the median time from opening to merge.</div>
</div>
{{end}}

<h2>Author × reviewer matrix</h2>
<p class="meta">Each cell counts the PRs authored by the row author that the column reviewer commented on,
with the average and median time from PR creation to that reviewer's first comment.
Reviewers exclude the PR author{{if not .IncludeBots}} and bot accounts{{end}}.</p>
{{if .Matrix.Rows}}
<div class="matrix-filters">
  <label for="author-filter">Authors</label>
  <select id="author-filter" multiple placeholder="Filter authors" autocomplete="off">
    {{range .Matrix.Rows}}<option value="{{.Author}}">{{.Author}}</option>{{end}}
  </select>
  <label for="reviewer-filter">Reviewers</label>
  <select id="reviewer-filter" multiple placeholder="Filter reviewers" autocomplete="off">
    {{range .Matrix.Cols}}{{if not .NoComments}}<option value="{{.Reviewer}}">{{.Reviewer}}</option>{{end}}{{end}}
  </select>
  <span id="matrix-filter-hint"></span>
</div>
<table class="matrix" id="matrix-table">
  <thead>
    <tr>
      <th class="author">Author</th>
      {{range .Matrix.Cols}}{{if .NoComments}}<th class="num" data-role="nocomments">{{.Reviewer}}</th>{{else}}<th class="num" data-reviewer="{{.Reviewer}}">{{.Reviewer}}</th>{{end}}{{end}}
      <th class="num">Total</th>
    </tr>
  </thead>
  <tbody>
    {{range .Matrix.Rows}}
    <tr>
      <td class="author" data-author="{{.Author}}">{{.Author}}</td>
      {{range .Cells}}
      <td class="cell"{{if .Bg}} style="background: {{.Bg}}"{{end}}>
        {{if .Count}}
          <span class="count">{{.Count}}</span>
          <span class="dur">avg {{.Avg}}</span>
          <span class="dur">med {{.Median}}</span>
        {{else}}
          <span class="empty">·</span>
        {{end}}
      </td>
      {{end}}
      <td class="total">{{.Total}}</td>
    </tr>
    {{end}}
  </tbody>
</table>
{{else}}
<p class="empty">No pull requests matched the given filters.</p>
{{end}}

<h2>Per-author summary</h2>
{{if .AuthorRows}}
<table>
  <thead>
    <tr><th>Author</th><th class="num">PRs</th><th class="num">Merged</th><th class="num">Median first comment</th><th class="num">Average first comment</th></tr>
  </thead>
  <tbody>
    {{range .AuthorRows}}
    <tr>
      <td>{{.Login}}</td>
      <td class="num">{{.PRs}}</td>
      <td class="num">{{.Merged}}</td>
      <td class="num">{{if .MedianFirstComment}}{{.MedianFirstComment}}{{else}}<span class="empty">-</span>{{end}}</td>
      <td class="num">{{if .AvgFirstComment}}{{.AvgFirstComment}}{{else}}<span class="empty">-</span>{{end}}</td>
    </tr>
    {{end}}
  </tbody>
</table>
{{else}}
<p class="empty">No authors matched.</p>
{{end}}

{{if .LeadRows}}
<h2>Lead time <span class="section-hint">merged PRs only</span></h2>
<table>
  <thead>
    <tr><th>Author</th><th class="num">Merged PRs</th><th class="num">Median time to merge</th><th class="num">Average time to merge</th><th class="num">Median time in draft</th><th class="num">Median time to first review</th><th class="num">Median time to approve</th></tr>
  </thead>
  <tbody>
    {{range .LeadRows}}
    <tr>
      <td>{{.Login}}</td>
      <td class="num">{{.MergedCount}}</td>
      <td class="num">{{.MedianMerge}}</td>
      <td class="num">{{.AvgMerge}}</td>
      <td class="num">{{if .MedianDraft}}{{.MedianDraft}}{{else}}<span class="empty">-</span>{{end}}</td>
      <td class="num">{{if .MedianFirstReview}}{{.MedianFirstReview}}{{else}}<span class="empty">-</span>{{end}}</td>
      <td class="num">{{if .MedianApprove}}{{.MedianApprove}}{{else}}<span class="empty">-</span>{{end}}</td>
    </tr>
    {{end}}
  </tbody>
</table>
<p class="meta">Draft time is reconstructed from ready-for-review and convert-to-draft timeline events;
review and approve times come from submitted reviews. A dash means the underlying events were not recorded.</p>
{{end}}

{{if .ContributionRows}}
<h2>Contribution</h2>
<table>
  <thead>
    <tr><th>Author</th><th class="num">Opened</th><th class="num">Merged</th><th class="num">Closed</th><th class="num">Merge rate</th><th class="num">PRs w/o review</th><th class="num">Comments received</th><th class="num">+/-</th><th class="num">Size xs/s/m/l/xl</th></tr>
  </thead>
  <tbody>
    {{range .ContributionRows}}
    <tr>
      <td>{{.Login}}</td>
      <td class="num">{{.Opened}}</td>
      <td class="num">{{.Merged}}</td>
      <td class="num">{{.Closed}}</td>
      <td class="num">{{.MergeRate}}</td>
      <td class="num">{{.NoReview}}</td>
      <td class="num">{{.CommentsReceived}}</td>
      <td class="num">{{if .HasSizeData}}+{{.Additions}}/-{{.Deletions}}{{else}}<span class="empty">-</span>{{end}}</td>
      <td class="num">{{if .HasSizeData}}{{index .SizeCounts 0}}/{{index .SizeCounts 1}}/{{index .SizeCounts 2}}/{{index .SizeCounts 3}}/{{index .SizeCounts 4}}{{else}}<span class="empty">-</span>{{end}}</td>
    </tr>
    {{end}}
  </tbody>
</table>
<p class="meta">Size buckets count changed lines per PR: xs &lt;100, s &lt;500, m &lt;1000, l &lt;2000, xl &ge;2000.
"PRs w/o review" counts PRs that received no reviewer comment and no review.</p>
{{end}}

<h2>Per-reviewer summary</h2>
{{if .ReviewerRows}}
<table>
  <thead>
    <tr><th>Reviewer</th><th class="num">PRs reviewed</th><th class="num">Median first comment</th><th class="num">Average first comment</th></tr>
  </thead>
  <tbody>
    {{range .ReviewerRows}}
    <tr>
      <td>{{.Login}}</td>
      <td class="num">{{.PRsReviewed}}</td>
      <td class="num">{{.Median}}</td>
      <td class="num">{{.Avg}}</td>
    </tr>
    {{end}}
  </tbody>
</table>
{{else}}
<p class="empty">No reviewer comments matched.</p>
{{end}}

{{if .EngagementRows}}
<h2>Reviewer engagement</h2>
<table>
  <thead>
    <tr><th>Reviewer</th><th class="num">PRs commented</th><th class="num">Comments</th><th class="num">PRs reviewed</th><th class="num">Reviews</th><th class="num">Approvals</th><th class="num">Changes requested</th><th class="num">Comment-only</th><th class="num">Median open to review</th><th class="num">Median request to review</th><th class="num">Median re-request to review</th></tr>
  </thead>
  <tbody>
    {{range .EngagementRows}}
    <tr>
      <td>{{.Login}}</td>
      <td class="num">{{.PRsCommented}}</td>
      <td class="num">{{.Comments}}</td>
      <td class="num">{{.PRsReviewed}}</td>
      <td class="num">{{.Reviews}}</td>
      <td class="num">{{.Approvals}}</td>
      <td class="num">{{.ChangesRequested}}</td>
      <td class="num">{{.CommentOnly}}</td>
      <td class="num">{{if .MedianOpenResponse}}{{.MedianOpenResponse}}{{else}}<span class="empty">-</span>{{end}}</td>
      <td class="num">{{if .MedianRequestResponse}}{{.MedianRequestResponse}}{{else}}<span class="empty">-</span>{{end}}</td>
      <td class="num">{{if .MedianRerequestResponse}}{{.MedianRerequestResponse}}{{else}}<span class="empty">-</span>{{end}}</td>
    </tr>
    {{end}}
  </tbody>
</table>
<p class="meta">"Median open to review" measures PR creation to the reviewer's first review;
"request to review" and "re-request to review" measure from the review request timeline event.
Request columns need review-request events in the cached data.</p>
{{end}}

{{if .SizeRows}}
<h2>PR size vs merge time</h2>
<table>
  <thead>
    <tr><th>Size</th><th class="num">PRs</th><th class="num">Merged</th><th class="num">Median time to merge</th></tr>
  </thead>
  <tbody>
    {{range .SizeRows}}
    <tr>
      <td>{{.Bucket}}</td>
      <td class="num">{{.Count}}</td>
      <td class="num">{{.MergedCount}}</td>
      <td class="num">{{if .MedianMerge}}{{.MedianMerge}}{{else}}<span class="empty">-</span>{{end}}</td>
    </tr>
    {{end}}
  </tbody>
</table>
<div class="chart-box" style="margin-top: 0.75rem;">
  <div class="chart-wrap" style="height: 260px;"><canvas id="size-chart"></canvas></div>
</div>
{{end}}

{{if .ActivityHours}}
<h2>Peak activity <span class="section-hint">PR opens by hour of day</span></h2>
<div class="chart-box">
  <div class="chart-wrap"><canvas id="activity-chart"></canvas></div>
  <div class="chart-note">Hours use the timezone recorded on each pull request's creation timestamp.</div>
</div>
{{end}}

{{if or .NotableAwaiting .NotableMerge .NotableCommented}}
<h2>Notable pull requests</h2>
<div class="notable">
  {{if .NotableAwaiting}}
  <div>
    <h3>Longest awaiting first review</h3>
    <ol>
      {{range .NotableAwaiting}}
      <li><a href="{{.URL}}">{{.Repo}}#{{.Number}}</a> {{.Title}}<span class="value">{{.Value}} · by {{.Author}}</span></li>
      {{end}}
    </ol>
  </div>
  {{end}}
  {{if .NotableMerge}}
  <div>
    <h3>Longest time to merge</h3>
    <ol>
      {{range .NotableMerge}}
      <li><a href="{{.URL}}">{{.Repo}}#{{.Number}}</a> {{.Title}}<span class="value">{{.Value}} · by {{.Author}}</span></li>
      {{end}}
    </ol>
  </div>
  {{end}}
  {{if .NotableCommented}}
  <div>
    <h3>Most discussed</h3>
    <ol>
      {{range .NotableCommented}}
      <li><a href="{{.URL}}">{{.Repo}}#{{.Number}}</a> {{.Title}}<span class="value">{{.Value}} · by {{.Author}}</span></li>
      {{end}}
    </ol>
  </div>
  {{end}}
</div>
{{end}}

{{if .ShowPRs}}
<h2>Pull requests in period</h2>
{{if .PRs}}
<table class="prlist">
  <thead>
    <tr><th>Repo</th><th class="num">PR</th><th>Title</th><th>Author</th><th>State</th><th>Created</th><th>Merged</th></tr>
  </thead>
  <tbody>
    {{range .PRs}}
    <tr>
      <td>{{.Repo}}</td>
      <td class="num"><a href="{{.URL}}">#{{.Number}}</a></td>
      <td>{{.Title}}</td>
      <td>{{.Author}}</td>
      <td><span class="state-{{.State}}">{{.State}}</span></td>
      <td>{{.CreatedAt}}</td>
      <td>{{if .MergedAt}}{{.MergedAt}}{{else}}<span class="empty">-</span>{{end}}</td>
    </tr>
    {{end}}
  </tbody>
</table>
{{else}}
<p class="empty">No pull requests in the period.</p>
{{end}}
{{end}}

<footer>Generated by <code>ghx stats</code>. Reviewers are derived from PR comments and reviews cached by <code>ghx cache</code>.</footer>
<script>
(function() {
  var root = document.documentElement;
  var btn = document.getElementById("theme-toggle");
  function apply(theme) {
    root.classList.toggle("dark", theme === "dark");
    btn.textContent = theme === "dark" ? "Light theme" : "Dark theme";
    try { localStorage.setItem("ghx-theme", theme); } catch (e) {}
    document.dispatchEvent(new Event("ghx-theme"));
  }
  apply(root.classList.contains("dark") ? "dark" : "light");
  btn.addEventListener("click", function() {
    apply(root.classList.contains("dark") ? "light" : "dark");
  });
})();
(function() {
  var table = document.getElementById("matrix-table");
  if (!table) return;
  var authorEl = document.getElementById("author-filter");
  var reviewerEl = document.getElementById("reviewer-filter");
  var hint = document.getElementById("matrix-filter-hint");
  var headCells = table.tHead.rows[0].cells;
  var bodyRows = table.tBodies[0].rows;
  var reviewerCols = [];
  for (var i = 0; i < headCells.length; i++) {
    if (headCells[i].getAttribute("data-reviewer") !== null) reviewerCols.push(i);
  }
  if (typeof TomSelect !== "undefined") {
    var config = { plugins: ["remove_button", "dropdown_input", "clear_button"], maxOptions: 1000 };
    new TomSelect(authorEl, config);
    new TomSelect(reviewerEl, config);
  }
  function selected(el) {
    if (el.tomselect) return el.tomselect.getValue();
    return Array.prototype.map.call(el.selectedOptions, function(o) { return o.value; });
  }
  function apply() {
    var qa = selected(authorEl), qr = selected(reviewerEl);
    var qaSet = {}, qrSet = {}, i;
    for (i = 0; i < qa.length; i++) qaSet[qa[i]] = true;
    for (i = 0; i < qr.length; i++) qrSet[qr[i]] = true;
    var filtered = qa.length > 0 || qr.length > 0;
    var showCol = [];
    var visibleReviewers = 0;
    for (i = 0; i < headCells.length; i++) showCol.push(true);
    for (var r = 0; r < reviewerCols.length; r++) {
      var c = reviewerCols[r];
      showCol[c] = qr.length === 0 || qrSet[headCells[c].getAttribute("data-reviewer")] === true;
      if (showCol[c]) visibleReviewers++;
    }
    var visibleAuthors = 0;
    for (var j = 0; j < bodyRows.length; j++) {
      var row = bodyRows[j];
      var cells = row.cells;
      var rowVisible = qa.length === 0 || qaSet[cells[0].getAttribute("data-author")] === true;
      row.style.display = rowVisible ? "" : "none";
      if (!rowVisible) continue;
      visibleAuthors++;
      for (c = 1; c < cells.length; c++) {
        cells[c].style.display = showCol[c] ? "" : "none";
      }
    }
    for (i = 0; i < headCells.length; i++) {
      headCells[i].style.display = showCol[i] ? "" : "none";
    }
    hint.textContent = filtered
      ? visibleAuthors + " of " + bodyRows.length + " authors · " + visibleReviewers + " of " + reviewerCols.length + " reviewers"
      : "";
  }
  authorEl.addEventListener("change", apply);
  reviewerEl.addEventListener("change", apply);
})();
(function() {
  var DATA = {{.ChartsJSON}};
  var charts = [];
  function cssVar(name) {
    return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  }
  function buildCharts() {
    charts.forEach(function(c) { c.destroy(); });
    charts = [];
    if (typeof Chart === "undefined") {
      document.querySelectorAll(".chart-box").forEach(function(el) {
        el.innerHTML = '<div class="chart-note">Charts unavailable: Chart.js could not be loaded from the CDN.</div>';
      });
      return;
    }
    var border = cssVar("--border");
    Chart.defaults.color = cssVar("--muted");
    Chart.defaults.borderColor = border;
    Chart.defaults.font.family = "-apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif";

    var trend = document.getElementById("trend-chart");
    if (trend && DATA.months && DATA.months.length) {
      charts.push(new Chart(trend, {
        type: "bar",
        data: {
          labels: DATA.months,
          datasets: [
            { label: "Opened", data: DATA.opened, backgroundColor: "rgba(76, 141, 255, 0.65)" },
            { label: "Merged", data: DATA.merged, backgroundColor: "rgba(63, 185, 80, 0.65)" },
            { type: "line", label: "Median hours to merge", data: DATA.medianHrs, yAxisID: "y2",
              borderColor: "#d29922", backgroundColor: "#d29922", tension: 0.3, pointRadius: 3 }
          ]
        },
        options: {
          responsive: true, maintainAspectRatio: false,
          scales: {
            y: { beginAtZero: true, title: { display: true, text: "PRs" }, grid: { color: border } },
            y2: { position: "right", beginAtZero: true, title: { display: true, text: "hours" }, grid: { drawOnChartArea: false } },
            x: { grid: { display: false } }
          }
        }
      }));
    }

    var size = document.getElementById("size-chart");
    if (size && DATA.sizes && DATA.sizes.length) {
      charts.push(new Chart(size, {
        type: "bar",
        data: {
          labels: ["xs", "s", "m", "l", "xl"],
          datasets: [{ label: "PRs", data: DATA.sizes, backgroundColor: "rgba(171, 125, 248, 0.65)" }]
        },
        options: {
          responsive: true, maintainAspectRatio: false,
          plugins: { legend: { display: false } },
          scales: { y: { beginAtZero: true, grid: { color: border } }, x: { grid: { display: false } } }
        }
      }));
    }

    var activity = document.getElementById("activity-chart");
    if (activity && DATA.hours && DATA.hours.length) {
      var hours = [];
      for (var h = 0; h < 24; h++) hours.push(String(h));
      charts.push(new Chart(activity, {
        type: "bar",
        data: {
          labels: hours,
          datasets: [{ label: "PRs opened", data: DATA.hours, backgroundColor: "rgba(76, 141, 255, 0.65)" }]
        },
        options: {
          responsive: true, maintainAspectRatio: false,
          plugins: { legend: { display: false } },
          scales: { y: { beginAtZero: true, grid: { color: border } }, x: { grid: { display: false } } }
        }
      }));
    }
  }
  buildCharts();
  document.addEventListener("ghx-theme", buildCharts);
})();
</script>
</body>
</html>
`

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
	payload := chartsPayload{Hours: r.ActivityHours}
	for _, t := range r.Trends {
		payload.Months = append(payload.Months, t.Month)
		payload.Opened = append(payload.Opened, t.Opened)
		payload.Merged = append(payload.Merged, t.Merged)
		payload.MedianHrs = append(payload.MedianHrs, t.medianHrs)
	}
	for _, s := range r.SizeRows {
		payload.Sizes = append(payload.Sizes, s.Count)
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
