package cmd

import (
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
}

// reviewerComment is a comment left on a PR by someone other than its author.
type reviewerComment struct {
	Login string
	At    time.Time
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

		e := &prEntry{item: rp, reviewers: map[string]time.Time{}}
		for _, c := range pr.Comments {
			login := c.Author.Login
			lower := strings.ToLower(login)
			if lower == strings.ToLower(pr.Author.Login) {
				continue // the author's own comments are not reviews
			}
			if !f.IncludeBots && isBotLogin(login) {
				continue
			}
			if len(f.Reviewers) > 0 && !f.Reviewers[lower] {
				continue
			}
			if _, ok := e.reviewers[lower]; !ok || c.CreatedAt.Before(e.reviewers[lower]) {
				e.reviewers[lower] = c.CreatedAt
			}
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
</div>

<h2>Author × reviewer matrix</h2>
<p class="meta">Each cell counts the PRs authored by the row author that the column reviewer commented on,
with the average and median time from PR creation to that reviewer's first comment.
Reviewers exclude the PR author{{if not .IncludeBots}} and bot accounts{{end}}.</p>
{{if .Matrix.Rows}}
<div class="matrix-filters">
  <label for="author-filter">Authors</label><input type="search" id="author-filter" placeholder="filter authors" autocomplete="off">
  <label for="reviewer-filter">Reviewers</label><input type="search" id="reviewer-filter" placeholder="filter reviewers" autocomplete="off">
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

<footer>Generated by <code>ghx stats</code>. Reviewers are derived from PR comments cached by <code>ghx cache</code>.</footer>
<script>
(function() {
  var root = document.documentElement;
  var btn = document.getElementById("theme-toggle");
  function apply(theme) {
    root.classList.toggle("dark", theme === "dark");
    btn.textContent = theme === "dark" ? "Light theme" : "Dark theme";
    try { localStorage.setItem("ghx-theme", theme); } catch (e) {}
  }
  apply(root.classList.contains("dark") ? "dark" : "light");
  btn.addEventListener("click", function() {
    apply(root.classList.contains("dark") ? "light" : "dark");
  });
})();
(function() {
  var table = document.getElementById("matrix-table");
  if (!table) return;
  var authorInput = document.getElementById("author-filter");
  var reviewerInput = document.getElementById("reviewer-filter");
  var hint = document.getElementById("matrix-filter-hint");
  var headCells = table.tHead.rows[0].cells;
  var bodyRows = table.tBodies[0].rows;
  var reviewerCols = [];
  for (var i = 0; i < headCells.length; i++) {
    if (headCells[i].getAttribute("data-reviewer") !== null) reviewerCols.push(i);
  }
  function apply() {
    var qa = authorInput.value.trim().toLowerCase();
    var qr = reviewerInput.value.trim().toLowerCase();
    var filtered = qa !== "" || qr !== "";
    var showCol = [];
    var visibleReviewers = 0;
    for (var i = 0; i < headCells.length; i++) showCol.push(true);
    for (var r = 0; r < reviewerCols.length; r++) {
      var c = reviewerCols[r];
      showCol[c] = qr === "" || headCells[c].getAttribute("data-reviewer").toLowerCase().indexOf(qr) !== -1;
      if (showCol[c]) visibleReviewers++;
    }
    var visibleAuthors = 0;
    for (var j = 0; j < bodyRows.length; j++) {
      var row = bodyRows[j];
      var cells = row.cells;
      var rowVisible = qa === "" || cells[0].getAttribute("data-author").toLowerCase().indexOf(qa) !== -1;
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
  authorInput.addEventListener("input", apply);
  reviewerInput.addEventListener("input", apply);
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
}

// renderReport renders the report to a complete HTML document.
func renderReport(r *Report) (string, error) {
	tmpl, err := template.New("report").Parse(reportTemplate)
	if err != nil {
		return "", err
	}
	view := &reportView{Report: r, ReposLabel: strings.Join(r.Repos, ", ")}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, view); err != nil {
		return "", err
	}
	return sb.String(), nil
}
