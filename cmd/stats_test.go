package cmd

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
	"github.com/tomzxcode/ghx/internal/mockserver"
)

func statsTestPR(number int, author string, createdAt time.Time, state string, comments []github.Comment) *github.PullRequest {
	return &github.PullRequest{
		Number:    number,
		Title:     "PR title",
		State:     state,
		Author:    github.Actor{Login: author},
		CreatedAt: createdAt,
		UpdatedAt: createdAt.Add(time.Hour),
		Comments:  comments,
	}
}

func TestParseStatsTime(t *testing.T) {
	// Empty is unbounded.
	if got, err := parseStatsTime("", false); err != nil || !got.IsZero() {
		t.Errorf("empty: got %v, %v; want zero, nil", got, err)
	}

	// Date-only from is midnight.
	got, err := parseStatsTime("2026-01-15", false)
	if err != nil {
		t.Fatalf("date-only: %v", err)
	}
	if got.Month() != time.January || got.Day() != 15 || got.Hour() != 0 {
		t.Errorf("date-only from: got %v", got)
	}

	// Date-only to is the end of that day (inclusive).
	got, err = parseStatsTime("2026-01-15", true)
	if err != nil {
		t.Fatalf("date-only end: %v", err)
	}
	if got.Hour() != 23 || got.Minute() != 59 {
		t.Errorf("date-only to: got %v", got)
	}

	// RFC3339 is accepted as-is.
	got, err = parseStatsTime("2026-01-15T10:30:00Z", true)
	if err != nil {
		t.Fatalf("rfc3339: %v", err)
	}
	if got.UTC().Hour() != 10 || got.UTC().Minute() != 30 {
		t.Errorf("rfc3339: got %v", got)
	}

	if _, err := parseStatsTime("not-a-date", false); err == nil {
		t.Error("invalid date: want error, got nil")
	}
}

func TestBuildReport_MatrixCounts(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	prs := []*repoPR{
		// alice's PR reviewed by bob (2h) and carol (1d).
		{Repo: "org/repo1", PR: statsTestPR(1, "alice", base, "MERGED", []github.Comment{
			{Author: github.Actor{Login: "bob"}, CreatedAt: base.Add(2 * time.Hour)},
			{Author: github.Actor{Login: "carol"}, CreatedAt: base.Add(24 * time.Hour)},
			{Author: github.Actor{Login: "alice"}, CreatedAt: base.Add(time.Hour)}, // self, ignored
		})},
		// alice's second PR reviewed by bob (4h).
		{Repo: "org/repo1", PR: statsTestPR(2, "alice", base, "OPEN", []github.Comment{
			{Author: github.Actor{Login: "bob"}, CreatedAt: base.Add(4 * time.Hour)},
		})},
		// bob's PR with no reviewer comments.
		{Repo: "org/repo1", PR: statsTestPR(3, "bob", base, "OPEN", []github.Comment{
			{Author: github.Actor{Login: "bob"}, CreatedAt: base.Add(time.Hour)},
		})},
		// Out-of-period PR, excluded.
		{Repo: "org/repo1", PR: statsTestPR(4, "alice", base.Add(-720*time.Hour), "MERGED", []github.Comment{
			{Author: github.Actor{Login: "bob"}, CreatedAt: base.Add(-719 * time.Hour)},
		})},
	}

	filters := statsFilters{
		From:  base.Add(-24 * time.Hour),
		To:    base.Add(24 * time.Hour),
		State: "all",
	}

	report := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		prs, filters,
	)

	if report.TotalPRs != 3 {
		t.Errorf("TotalPRs: got %d, want 3", report.TotalPRs)
	}
	if report.Merged != 1 || report.Open != 2 {
		t.Errorf("states: got merged=%d open=%d, want 1/2", report.Merged, report.Open)
	}
	if report.AuthorCount != 2 {
		t.Errorf("AuthorCount: got %d, want 2", report.AuthorCount)
	}
	if report.ReviewerCount != 2 {
		t.Errorf("ReviewerCount: got %d, want 2", report.ReviewerCount)
	}

	// Find alice's row.
	var aliceRow *MatrixRow
	for _, row := range report.Matrix.Rows {
		if row.Author == "alice" {
			aliceRow = row
		}
	}
	if aliceRow == nil {
		t.Fatal("alice row missing from matrix")
	}
	if aliceRow.Total != 2 {
		t.Errorf("alice Total: got %d, want 2", aliceRow.Total)
	}

	// Column order: (no comments) first, then reviewers by total desc.
	cols := report.Matrix.Cols
	if cols[0].Reviewer != "(no comments)" {
		t.Errorf("first column: got %q", cols[0].Reviewer)
	}
	if cols[1].Reviewer != "bob" || cols[1].Total != 2 {
		t.Errorf("bob column: got %+v, want bob/2", cols[1])
	}
	if cols[2].Reviewer != "carol" || cols[2].Total != 1 {
		t.Errorf("carol column: got %+v, want carol/1", cols[2])
	}

	// alice x bob: 2 PRs, durations 2h and 4h => median 3h, avg 3h.
	// Cells are ordered: (no comments), bob, carol.
	bobIdx := 1
	cell := aliceRow.Cells[bobIdx]
	if cell.Count != 2 {
		t.Errorf("alice/bob count: got %d, want 2", cell.Count)
	}
	if cell.Median != "3h 0m" {
		t.Errorf("alice/bob median: got %q, want %q", cell.Median, "3h 0m")
	}
	if cell.Avg != "3h 0m" {
		t.Errorf("alice/bob avg: got %q, want %q", cell.Avg, "3h 0m")
	}
	if cell.Bg == "" {
		t.Error("alice/bob cell: want heat background")
	}

	// alice x carol: 1 PR, duration 24h.
	carolIdx := 2
	cell = aliceRow.Cells[carolIdx]
	if cell.Count != 1 || cell.Median != "1d 0h" {
		t.Errorf("alice/carol: got count=%d median=%q, want 1/1d 0h", cell.Count, cell.Median)
	}

	// bob has one PR without reviewer comments.
	var bobRow *MatrixRow
	for _, row := range report.Matrix.Rows {
		if row.Author == "bob" {
			bobRow = row
		}
	}
	if bobRow == nil {
		t.Fatal("bob row missing from matrix")
	}
	if bobRow.Cells[0].Count != 1 {
		t.Errorf("bob no-comment count: got %d, want 1", bobRow.Cells[0].Count)
	}
}

func TestBuildReport_Filters(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	prs := []*repoPR{
		{Repo: "org/repo1", PR: statsTestPR(1, "alice", base, "MERGED", []github.Comment{
			{Author: github.Actor{Login: "bob"}, CreatedAt: base.Add(time.Hour)},
		})},
		{Repo: "org/repo1", PR: statsTestPR(2, "carol", base, "OPEN", []github.Comment{
			{Author: github.Actor{Login: "dave"}, CreatedAt: base.Add(time.Hour)},
			{Author: github.Actor{Login: "renovate[bot]"}, CreatedAt: base.Add(30 * time.Minute)},
		})},
	}

	// Author filter keeps only alice's PRs.
	report := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		prs,
		statsFilters{State: "all", Authors: toLowerSet([]string{"alice"})},
	)
	if report.TotalPRs != 1 {
		t.Errorf("author filter: got %d PRs, want 1", report.TotalPRs)
	}

	// Reviewer filter restricts columns to dave (bot excluded by default).
	report = buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		prs,
		statsFilters{State: "all", Reviewers: toLowerSet([]string{"dave"})},
	)
	if report.ReviewerCount != 1 || report.Matrix.Cols[1].Reviewer != "dave" {
		t.Errorf("reviewer filter: got %d reviewers, cols %+v", report.ReviewerCount, report.Matrix.Cols)
	}

	// Bots are excluded by default and included with IncludeBots.
	report = buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		prs,
		statsFilters{State: "all"},
	)
	for _, col := range report.Matrix.Cols {
		if strings.Contains(col.Reviewer, "[bot]") {
			t.Errorf("bot excluded by default but found column %q", col.Reviewer)
		}
	}
	report = buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		prs,
		statsFilters{State: "all", IncludeBots: true},
	)
	found := false
	for _, col := range report.Matrix.Cols {
		if col.Reviewer == "renovate[bot]" {
			found = true
		}
	}
	if !found {
		t.Error("IncludeBots: want renovate[bot] column")
	}

	// State filter.
	report = buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		prs,
		statsFilters{State: "open"},
	)
	if report.TotalPRs != 1 {
		t.Errorf("state filter: got %d PRs, want 1", report.TotalPRs)
	}
}

func TestBuildReport_SummaryTablesAndPRList(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	prs := []*repoPR{
		{Repo: "org/repo1", PR: statsTestPR(1, "alice", base, "MERGED", []github.Comment{
			{Author: github.Actor{Login: "bob"}, CreatedAt: base.Add(time.Hour)},
		})},
		{Repo: "org/repo2", PR: statsTestPR(2, "carol", base, "OPEN", nil)},
	}

	report := buildReport(
		[]*gitremote.Repo{
			{Host: "github.com", Owner: "org", Name: "repo1"},
			{Host: "github.com", Owner: "org", Name: "repo2"},
		},
		prs,
		statsFilters{State: "all"},
	)

	if len(report.AuthorRows) != 2 {
		t.Fatalf("AuthorRows: got %d, want 2", len(report.AuthorRows))
	}
	if report.AuthorRows[0].Login != "alice" || report.AuthorRows[0].Merged != 1 {
		t.Errorf("alice summary: got %+v", report.AuthorRows[0])
	}
	if report.AuthorRows[0].MedianFirstComment != "1h 0m" {
		t.Errorf("alice median: got %q", report.AuthorRows[0].MedianFirstComment)
	}
	if len(report.ReviewerRows) != 1 || report.ReviewerRows[0].Login != "bob" || report.ReviewerRows[0].PRsReviewed != 1 {
		t.Errorf("reviewer rows: got %+v", report.ReviewerRows)
	}

	if report.ShowPRs || report.PRs != nil {
		t.Errorf("PR list should be omitted by default: ShowPRs=%v PRs=%v", report.ShowPRs, report.PRs)
	}

	report = buildReport(
		[]*gitremote.Repo{
			{Host: "github.com", Owner: "org", Name: "repo1"},
			{Host: "github.com", Owner: "org", Name: "repo2"},
		},
		prs,
		statsFilters{State: "all", ListPRs: true},
	)
	if !report.ShowPRs {
		t.Error("ShowPRs: got false, want true with ListPRs")
	}
	if len(report.PRs) != 2 {
		t.Fatalf("PR list: got %d, want 2", len(report.PRs))
	}
	if report.PRs[0].Repo != "org/repo1" || report.PRs[1].Repo != "org/repo2" {
		t.Errorf("PR list order: got %s then %s", report.PRs[0].Repo, report.PRs[1].Repo)
	}
	if report.PRs[1].State != "open" {
		t.Errorf("PR list state: got %q", report.PRs[1].State)
	}

	if report.Period != "all time" {
		t.Errorf("Period: got %q", report.Period)
	}
}

func TestFormatStatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0m"},
		{35 * time.Minute, "35m"},
		{4*time.Hour + 12*time.Minute, "4h 12m"},
		{50*time.Hour + 3*time.Minute, "2d 2h"},
	}
	for _, c := range cases {
		if got := formatStatDuration(c.d); got != c.want {
			t.Errorf("formatStatDuration(%v): got %q, want %q", c.d, got, c.want)
		}
	}
}

func TestMedianDuration(t *testing.T) {
	even := []time.Duration{4 * time.Hour, 2 * time.Hour}
	if got := medianDuration(even); got != 3*time.Hour {
		t.Errorf("even median: got %v, want 3h", got)
	}
	odd := []time.Duration{time.Hour, 5 * time.Hour, 10 * time.Hour}
	if got := medianDuration(odd); got != 5*time.Hour {
		t.Errorf("odd median: got %v, want 5h", got)
	}
	if got := medianDuration(nil); got != 0 {
		t.Errorf("empty median: got %v, want 0", got)
	}
}

func TestLoadStatsPRs_FallbackFetchesComments(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	scenario := mockserver.NewScenario(
		mockserver.WithScenarioPR("acme", "widget", github.PullRequest{
			Number:    1,
			Title:     "Fix bug",
			State:     "MERGED",
			Author:    github.Actor{Login: "alice"},
			CreatedAt: base,
			UpdatedAt: base.Add(time.Hour),
			Comments: []github.Comment{{
				ID:        "c1",
				Author:    github.Actor{Login: "bob"},
				Body:      "thoughts",
				CreatedAt: base.Add(2 * time.Hour),
				UpdatedAt: base.Add(2 * time.Hour),
			}},
		}),
	)
	srv := mockserver.NewServer(scenario)
	defer srv.Close()

	savedURL, savedDir := apiURLFlag, cacheDir
	apiURLFlag, cacheDir = srv.URL(), t.TempDir()
	defer func() { apiURLFlag, cacheDir = savedURL, savedDir }()

	repo := &gitremote.Repo{Host: "mock", Owner: "acme", Name: "widget"}
	prs, err := loadStatsPRs(newStore(), repo, time.Time{})
	if err != nil {
		t.Fatalf("loadStatsPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d prs, want 1", len(prs))
	}
	if len(prs[0].Comments) == 0 {
		t.Fatal("fallback PR has no comments; reviewer metrics would be unavailable")
	}
	if got := prs[0].Comments[0].Author.Login; got != "bob" {
		t.Errorf("comment author: got %q, want bob", got)
	}
}

func TestBuildReport_LeadTimeAndContribution(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	mergedAt := base.Add(48 * time.Hour)

	pr := statsTestPR(1, "alice", base, "MERGED", []github.Comment{
		{Author: github.Actor{Login: "bob"}, CreatedAt: base.Add(time.Hour)},
	})
	pr.MergedAt = &mergedAt
	pr.Additions = 400
	pr.Deletions = 100

	// bob's PR, closed without merge, never reviewed.
	closedAt := base.Add(24 * time.Hour)
	closed := statsTestPR(2, "bob", base, "CLOSED", nil)
	closed.ClosedAt = &closedAt

	report := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: pr}, {Repo: "org/repo1", PR: closed}},
		statsFilters{State: "all"},
	)

	if report.MedianMerge != "2d 0h" {
		t.Errorf("MedianMerge: got %q, want 2d 0h", report.MedianMerge)
	}
	if len(report.LeadRows) != 1 || report.LeadRows[0].Login != "alice" {
		t.Fatalf("LeadRows: got %+v, want only alice", report.LeadRows)
	}
	lead := report.LeadRows[0]
	if lead.MergedCount != 1 || lead.MedianMerge != "2d 0h" {
		t.Errorf("alice lead row: got %+v", lead)
	}

	var aliceRow, bobRow *ContributionRow
	for _, row := range report.ContributionRows {
		switch row.Login {
		case "alice":
			aliceRow = row
		case "bob":
			bobRow = row
		}
	}
	if aliceRow == nil || bobRow == nil {
		t.Fatalf("contribution rows missing: %+v", report.ContributionRows)
	}
	if aliceRow.Opened != 1 || aliceRow.Merged != 1 || aliceRow.MergeRate != "100%" {
		t.Errorf("alice contribution: got %+v", aliceRow)
	}
	if aliceRow.Additions != 400 || aliceRow.Deletions != 100 || !aliceRow.HasSizeData {
		t.Errorf("alice size data: got %+v", aliceRow)
	}
	if aliceRow.SizeCounts[2] != 1 { // 500 changed lines -> m bucket
		t.Errorf("alice size bucket: got %+v, want m bucket", aliceRow.SizeCounts)
	}
	if bobRow.NoReview != 1 || bobRow.Closed != 1 {
		t.Errorf("bob contribution: got %+v", bobRow)
	}
	if report.TotalAdditions != 400 || report.TotalDeletions != 100 {
		t.Errorf("totals: got +%d/-%d, want +400/-100", report.TotalAdditions, report.TotalDeletions)
	}
}

func TestBuildReport_SizeBuckets(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	mkPR := func(number int, additions, deletions int) *github.PullRequest {
		pr := statsTestPR(number, "alice", base, "MERGED", nil)
		mergedAt := base.Add(time.Duration(number) * time.Hour)
		pr.MergedAt = &mergedAt
		pr.Additions = additions
		pr.Deletions = deletions
		return pr
	}
	prs := []*repoPR{
		{Repo: "org/repo1", PR: mkPR(1, 50, 49)},     // 99 -> xs
		{Repo: "org/repo1", PR: mkPR(2, 300, 100)},   // 400 -> s
		{Repo: "org/repo1", PR: mkPR(3, 500, 500)},   // 1000 -> l? 1000 is not < 1000 -> l
		{Repo: "org/repo1", PR: mkPR(4, 1000, 1000)}, // 2000 -> xl
	}
	report := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		prs,
		statsFilters{State: "all"},
	)
	want := [sizeBucketCount]int{1, 1, 0, 1, 1}
	for i, row := range report.SizeRows {
		if row.Count != want[i] {
			t.Errorf("size row %s: got %d, want %d", row.Bucket, row.Count, want[i])
		}
	}
}

func TestBuildReport_ReviewerEngagement(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	mergedAt := base.Add(72 * time.Hour)

	pr := statsTestPR(1, "alice", base, "MERGED", []github.Comment{
		{Author: github.Actor{Login: "carol"}, CreatedAt: base.Add(2 * time.Hour)},
	})
	pr.MergedAt = &mergedAt
	pr.Reviews = []github.PRReview{
		{Author: github.Actor{Login: "bob"}, State: "CHANGES_REQUESTED", SubmittedAt: base.Add(3 * time.Hour)},
		{Author: github.Actor{Login: "bob"}, State: "APPROVED", SubmittedAt: base.Add(8 * time.Hour)},
		{Author: github.Actor{Login: "carol"}, State: "COMMENTED", SubmittedAt: base.Add(4 * time.Hour)},
	}
	pr.Timeline = []github.TimelineEvent{
		{Kind: github.TimelineReviewRequested, RequestedReviewer: github.Actor{Login: "bob"}, CreatedAt: base.Add(time.Hour)},
		// GitHub records a re-request as another review-requested event.
		{Kind: github.TimelineReviewRequested, RequestedReviewer: github.Actor{Login: "bob"}, CreatedAt: base.Add(6 * time.Hour)},
	}

	report := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: pr}},
		statsFilters{State: "all"},
	)

	if report.TotalReviews != 3 {
		t.Errorf("TotalReviews: got %d, want 3", report.TotalReviews)
	}
	if len(report.EngagementRows) != 2 {
		t.Fatalf("EngagementRows: got %d, want 2", len(report.EngagementRows))
	}
	// bob: 2 reviews, sorted first (2 reviews vs carol's 1 comment+1 review = 2? carol has 1 comment + 1 review = 2 total; tie broken by login).
	var bobRow, carolRow *ReviewerEngagementRow
	for _, row := range report.EngagementRows {
		switch row.Login {
		case "bob":
			bobRow = row
		case "carol":
			carolRow = row
		}
	}
	if bobRow == nil || carolRow == nil {
		t.Fatalf("engagement rows missing: %+v", report.EngagementRows)
	}
	if bobRow.Reviews != 2 || bobRow.Approvals != 1 || bobRow.ChangesRequested != 1 || bobRow.CommentOnly != 0 {
		t.Errorf("bob engagement: got %+v", bobRow)
	}
	// bob's first review came 3h after opening; his re-request at +6h was
	// answered by the +8h approval (2h later).
	if bobRow.MedianOpenResponse != "3h 0m" {
		t.Errorf("bob open response: got %q, want 3h 0m", bobRow.MedianOpenResponse)
	}
	if bobRow.MedianRerequestResponse != "2h 0m" {
		t.Errorf("bob re-request response: got %q, want 2h 0m", bobRow.MedianRerequestResponse)
	}
	// Request at +1h answered at +3h.
	if bobRow.MedianRequestResponse != "2h 0m" {
		t.Errorf("bob request response: got %q, want 2h 0m", bobRow.MedianRequestResponse)
	}
	if carolRow.PRsCommented != 1 || carolRow.Comments != 1 || carolRow.CommentOnly != 1 {
		t.Errorf("carol engagement: got %+v", carolRow)
	}

	// Author-side lead time: first review at +3h, first approval at +8h.
	if len(report.LeadRows) != 1 {
		t.Fatalf("LeadRows: got %d, want 1", len(report.LeadRows))
	}
	if report.LeadRows[0].MedianFirstReview != "3h 0m" {
		t.Errorf("median first review: got %q, want 3h 0m", report.LeadRows[0].MedianFirstReview)
	}
	if report.LeadRows[0].MedianApprove != "8h 0m" {
		t.Errorf("median approve: got %q, want 8h 0m", report.LeadRows[0].MedianApprove)
	}
}

func TestBuildReport_DraftTime(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	mergedAt := base.Add(48 * time.Hour)

	pr := statsTestPR(1, "alice", base, "MERGED", nil)
	pr.MergedAt = &mergedAt
	pr.Timeline = []github.TimelineEvent{
		{Kind: github.TimelineReadyForReview, CreatedAt: base.Add(2 * time.Hour)},
	}

	report := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: pr}},
		statsFilters{State: "all"},
	)

	if len(report.LeadRows) != 1 || report.LeadRows[0].MedianDraft != "2h 0m" {
		t.Errorf("draft time: got %+v, want median draft 2h 0m", report.LeadRows)
	}

	// A review request recorded before the ready-for-review event must not
	// hide the draft period.
	pr.Timeline = []github.TimelineEvent{
		{Kind: github.TimelineReviewRequested, RequestedReviewer: github.Actor{Login: "bob"}, CreatedAt: base.Add(time.Hour)},
		{Kind: github.TimelineReadyForReview, CreatedAt: base.Add(2 * time.Hour)},
	}
	report = buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: pr}},
		statsFilters{State: "all"},
	)
	if len(report.LeadRows) != 1 || report.LeadRows[0].MedianDraft != "2h 0m" {
		t.Errorf("draft time with earlier request: got %+v, want median draft 2h 0m", report.LeadRows)
	}

	// A convert-to-draft cycle is measured from the conversion, not creation.
	pr.Timeline = []github.TimelineEvent{
		{Kind: github.TimelineConvertedToDraft, CreatedAt: base.Add(8 * time.Hour)},
		{Kind: github.TimelineReadyForReview, CreatedAt: base.Add(10 * time.Hour)},
	}
	report = buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: pr}},
		statsFilters{State: "all"},
	)
	if len(report.LeadRows) != 1 || report.LeadRows[0].MedianDraft != "2h 0m" {
		t.Errorf("draft time after conversion: got %+v, want median draft 2h 0m", report.LeadRows)
	}
}

func TestBuildReport_NotablePRs(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	mkMerged := func(number int, hours float64) *github.PullRequest {
		pr := statsTestPR(number, "alice", base, "MERGED", nil)
		mergedAt := base.Add(time.Duration(hours * float64(time.Hour)))
		pr.MergedAt = &mergedAt
		pr.Title = fmt.Sprintf("PR %d", number)
		return pr
	}
	openStale := statsTestPR(10, "alice", base.Add(-240*time.Hour), "OPEN", nil)
	openStale.Title = "Stale open PR"

	prs := []*repoPR{
		{Repo: "org/repo1", PR: mkMerged(1, 24)},
		{Repo: "org/repo1", PR: mkMerged(2, 72)},
		{Repo: "org/repo1", PR: openStale},
	}
	report := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		prs,
		statsFilters{State: "all", Top: 1},
	)

	if report.TopN != 1 {
		t.Errorf("TopN: got %d, want 1", report.TopN)
	}
	if len(report.NotableMerge) != 1 || report.NotableMerge[0].Number != 2 {
		t.Errorf("notable merge: got %+v, want PR 2 first", report.NotableMerge)
	}
	if len(report.NotableAwaiting) != 1 || report.NotableAwaiting[0].Number != 10 {
		t.Errorf("notable awaiting: got %+v, want PR 10", report.NotableAwaiting)
	}
	if !strings.Contains(report.NotableAwaiting[0].Value, "awaiting") {
		t.Errorf("awaiting value: got %q", report.NotableAwaiting[0].Value)
	}
}

func TestLoadStatsPRs_FetchesReviewData(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	scenario := mockserver.NewScenario(
		mockserver.WithScenarioPR("acme", "widget", github.PullRequest{
			Number:    1,
			Title:     "Fix bug",
			State:     "MERGED",
			Author:    github.Actor{Login: "alice"},
			CreatedAt: base,
			UpdatedAt: base.Add(time.Hour),
			Additions: 120,
			Deletions: 30,
			Reviews: []github.PRReview{{
				Author:      github.Actor{Login: "bob"},
				State:       "APPROVED",
				SubmittedAt: base.Add(2 * time.Hour),
			}},
			Timeline: []github.TimelineEvent{{
				Kind:              github.TimelineReviewRequested,
				RequestedReviewer: github.Actor{Login: "bob"},
				CreatedAt:         base.Add(30 * time.Minute),
			}},
		}),
	)
	srv := mockserver.NewServer(scenario)
	defer srv.Close()

	savedURL, savedDir := apiURLFlag, cacheDir
	apiURLFlag, cacheDir = srv.URL(), t.TempDir()
	defer func() { apiURLFlag, cacheDir = savedURL, savedDir }()

	repo := &gitremote.Repo{Host: "mock", Owner: "acme", Name: "widget"}
	prs, err := loadStatsPRs(newStore(), repo, time.Time{})
	if err != nil {
		t.Fatalf("loadStatsPRs: %v", err)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d prs, want 1", len(prs))
	}
	pr := prs[0]
	if pr.Additions != 120 || pr.Deletions != 30 {
		t.Errorf("size data: got +%d/-%d, want +120/-30", pr.Additions, pr.Deletions)
	}
	if len(pr.Reviews) != 1 || pr.Reviews[0].Author.Login != "bob" || pr.Reviews[0].State != "APPROVED" {
		t.Errorf("reviews: got %+v", pr.Reviews)
	}
	if len(pr.Timeline) != 1 || pr.Timeline[0].Kind != github.TimelineReviewRequested || pr.Timeline[0].RequestedReviewer.Login != "bob" {
		t.Errorf("timeline: got %+v", pr.Timeline)
	}
}

func TestRenderReport(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	pr := statsTestPR(1, "alice", base, "MERGED", []github.Comment{
		{Author: github.Actor{Login: "bob"}, CreatedAt: base.Add(time.Hour)},
	})
	mergedAt := base.Add(24 * time.Hour)
	pr.MergedAt = &mergedAt
	pr.Title = "Fix <script>alert(1)</script>"

	report := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: pr}},
		statsFilters{State: "all", ListPRs: true},
	)

	html, err := renderReport(report)
	if err != nil {
		t.Fatalf("renderReport: %v", err)
	}

	for _, want := range []string{
		"Author × reviewer matrix",
		"alice",
		"bob",
		"Fix &lt;script&gt;alert(1)&lt;/script&gt;",
		"Pull requests in period",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing %q", want)
		}
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("rendered HTML contains unescaped title")
	}

	for _, want := range []string{
		`id="theme-toggle"`,
		"localStorage.getItem",
		"localStorage.setItem",
		"prefers-color-scheme: dark",
		"html.dark",
		"--state-open: #3fb950",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing theme support marker %q", want)
		}
	}

	for _, want := range []string{
		`id="author-filter"`,
		`id="reviewer-filter"`,
		`id="matrix-table"`,
		`data-author="alice"`,
		`data-reviewer="bob"`,
		`data-role="nocomments"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing matrix filter marker %q", want)
		}
	}

	for _, want := range []string{
		"Lead time",
		"Contribution",
		"Reviewer engagement",
		"PR size vs merge time",
		"Peak activity",
		"Notable pull requests",
		`id="trend-chart"`,
		`id="activity-chart"`,
		`id="size-chart"`,
		"cdn.jsdelivr.net/npm/chart.js",
		"Median time to merge",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered HTML missing analytics marker %q", want)
		}
	}
}

func TestRenderReport_LegacyNotice(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	// A PR with no review, size or timeline data looks like it came from a
	// cache created before analytics data was collected.
	pr := statsTestPR(1, "alice", base, "OPEN", nil)

	report := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: pr}},
		statsFilters{State: "all"},
	)
	if !report.LegacyCache {
		t.Error("LegacyCache: got false, want true for PR without analytics data")
	}
	html, err := renderReport(report)
	if err != nil {
		t.Fatalf("renderReport: %v", err)
	}
	if !strings.Contains(html, "ghx cache --force") {
		t.Error("rendered HTML missing legacy cache notice")
	}

	// With review data present the notice must disappear.
	pr.Reviews = []github.PRReview{{Author: github.Actor{Login: "bob"}, State: "APPROVED", SubmittedAt: base.Add(time.Hour)}}
	report = buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: pr}},
		statsFilters{State: "all"},
	)
	if report.LegacyCache {
		t.Error("LegacyCache: got true, want false for PR with review data")
	}

	// A small share of PRs without data (genuinely empty diffs) must not
	// trigger the notice.
	withData := statsTestPR(2, "alice", base, "OPEN", nil)
	withData.Additions = 10
	report = buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: withData}, {Repo: "org/repo1", PR: pr}},
		statsFilters{State: "all"},
	)
	if report.LegacyCache {
		t.Error("LegacyCache: got true, want false when most PRs carry analytics data")
	}
}

func TestRenderReport_PRsOmittedByDefault(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	pr := statsTestPR(1, "alice", base, "MERGED", nil)

	omitted := buildReport(
		[]*gitremote.Repo{{Host: "github.com", Owner: "org", Name: "repo1"}},
		[]*repoPR{{Repo: "org/repo1", PR: pr}},
		statsFilters{State: "all"},
	)
	omittedHTML, err := renderReport(omitted)
	if err != nil {
		t.Fatalf("renderReport without PR list: %v", err)
	}
	if strings.Contains(omittedHTML, "Pull requests in period") {
		t.Error("PR list section rendered without --list-prs")
	}
}
