package cmd

import (
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

func TestRenderReport(t *testing.T) {
	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	pr := statsTestPR(1, "alice", base, "MERGED", []github.Comment{
		{Author: github.Actor{Login: "bob"}, CreatedAt: base.Add(time.Hour)},
	})
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
