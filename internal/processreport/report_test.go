package processreport

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/processflow"
)

func reportItem(id string, kind processflow.Kind, created time.Time, station processflow.Station, exited *time.Time) processflow.Item {
	return processflow.Item{ID: id, Kind: kind, Title: "Item " + id, URL: "https://example.com/" + id, CreatedAt: created, Station: station, ExitedAt: exited}
}

// TestRenderProducesTheSections verifies the report renders every section
// from the computed numbers, and that the static (CLI) document carries no
// interactive window form.
func TestRenderProducesTheSections(t *testing.T) {
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	closed := created.Add(9 * 24 * time.Hour)
	merged := created.Add(5 * 24 * time.Hour)
	report := processflow.Build([]processflow.Item{
		reportItem("#1", processflow.KindIssue, created, processflow.Closed, &closed),
		reportItem("#2", processflow.KindIssue, created.Add(48*time.Hour), processflow.Open, nil),
		reportItem("#10", processflow.KindPR, created, processflow.Merged, &merged),
		reportItem("#11", processflow.KindPR, created.Add(48*time.Hour), processflow.AwaitingReview, nil),
	}, created.Add(30*24*time.Hour), 30)

	html, err := Render(&report, Meta{Repos: []string{"acme/repo1"}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"Process report",
		"acme/repo1",
		"Production line",
		"Cumulative flow",
		"Bottlenecks",
		"On the line now",
		"Cycle time",
		"Flow",
		"Item #2",          // the aging list links the WIP items
		"Item #11",         // by title
		"svg",              // the charts are inline SVG
		"filling",          // the queue-pressure trend badges
		"Open issues",      // the cumulative flow legend
		"overflow-x: auto", // wide tables scroll instead of breaking mobile layout
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered report missing %q", want)
		}
	}
	if strings.Contains(html, "<form") {
		t.Error("a static report should not render the interactive window form")
	}
}

// TestRenderInteractiveWindowForm verifies the served report renders the
// window form that reloads /process, preserving the requested repositories
// and pre-filling the bounds with the covered window.
func TestRenderInteractiveWindowForm(t *testing.T) {
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	report := processflow.Build([]processflow.Item{
		reportItem("#1", processflow.KindIssue, created, processflow.Open, nil),
	}, created.Add(48*time.Hour), 30)
	html, err := Render(&report, Meta{
		Repos:       []string{"acme/repo1"},
		RepoIDs:     []string{"github.com/acme/repo1"},
		Interactive: true,
		DaysValue:   "30",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`<form class="window-form" method="get" action="/process">`,
		`<input type="hidden" name="repo" value="github.com/acme/repo1">`,
		`name="days" min="1" max="730" value="30"`,
		`name="from" value=""`,
		`name="to" value=""`,
		"Apply",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("interactive report missing %q", want)
		}
	}

	// Covering every cached repository, the form resubmits without pinning a
	// repository set.
	html, err = Render(&report, Meta{Repos: []string{"acme/repo1"}, Interactive: true, AllRepos: true})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(html, `type="hidden" name="repo"`) {
		t.Error("an all-repositories report should not pin repositories in the form")
	}
}

// TestRenderRangeMeta verifies an explicit from/to report names the covered
// range in the meta line instead of a trailing window, and pre-fills the
// date inputs with the range bounds.
func TestRenderRangeMeta(t *testing.T) {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	report := processflow.BuildRange([]processflow.Item{
		reportItem("#1", processflow.KindIssue, created, processflow.Open, nil),
	}, created, created.Add(30*24*time.Hour))
	html, err := Render(&report, Meta{Repos: []string{"acme/repo1"}, Interactive: true, HasRange: true,
		FromValue: "2026-03-01", ToValue: "2026-03-31"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, "Mar 1 – Mar 31, 2026") {
		t.Errorf("meta line should name the covered range, got page without it")
	}
	for _, want := range []string{`name="from" value="2026-03-01"`, `name="to" value="2026-03-31"`} {
		if !strings.Contains(html, want) {
			t.Errorf("date inputs should carry the range bounds, missing %q", want)
		}
	}
	if strings.Contains(html, "last 31 days") {
		t.Error("a ranged report should not describe itself as a trailing window")
	}
}

// TestLineDiagramShowsEveryMove verifies the diagram draws counts for moves
// that skip a station (approvals straight to waiting on merge, and closes into
// discarded from any open station), which a one-row strip of adjacent stations
// could not show, and that a two-way edge gets a separate arrow and number per
// direction instead of a single arrow with both numbers.
func TestLineDiagramShowsEveryMove(t *testing.T) {
	stat := func(station processflow.Station, label string, count int) processflow.StageStat {
		return processflow.StageStat{Station: station, Label: label, Count: count}
	}
	line := &processflow.LineReport{
		Kind:  processflow.KindPR,
		Label: "Pull requests",
		Stations: []processflow.StageStat{
			stat(processflow.Draft, "Draft", 1),
			stat(processflow.AwaitingReview, "Waiting on reviewer", 2),
			stat(processflow.AwaitingAuthor, "Waiting on author", 3),
			stat(processflow.AwaitingMerge, "Waiting on merge", 4),
			stat(processflow.Merged, "Merged", 5),
			stat(processflow.Discarded, "Discarded", 6),
		},
		Flows: []processflow.StationFlow{
			{From: processflow.Draft, To: processflow.AwaitingReview, Count: 11},
			{From: processflow.AwaitingReview, To: processflow.Draft, Count: 2},
			{From: processflow.AwaitingReview, To: processflow.AwaitingMerge, Count: 7},
			{From: processflow.AwaitingAuthor, To: processflow.AwaitingMerge, Count: 8},
			{From: processflow.AwaitingAuthor, To: processflow.Discarded, Count: 9},
			{From: processflow.AwaitingMerge, To: processflow.Merged, Count: 10},
		},
	}
	html := lineDiagram(line)
	for _, want := range []string{
		`class="line-svg"`,
		// Two-way edge: one arrow each way, each with its own number.
		`d="M 177 51 H 263"`,
		`d="M 263 61 H 177"`,
		`x="220" y="42" text-anchor="middle">11</text>`,
		`x="220" y="75" text-anchor="middle">2</text>`,
		// One-way edges carry their number beside the arrow.
		`x="480" y="47" text-anchor="middle">7</text>`,
		`x="520" y="216" text-anchor="middle">8</text>`,
		`x="765" y="144" text-anchor="middle">9</text>`,
		`x="740" y="47" text-anchor="middle">10</text>`,
		"Waiting on merge",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("diagram missing %q", want)
		}
	}
	if strings.Contains(html, "&#8594;") || strings.Contains(html, "&#8592;") {
		t.Error("arrow glyphs belong on the paths, not in the labels")
	}
}

// TestIssueLineDiagram verifies the issue line carries the close count.
func TestIssueLineDiagram(t *testing.T) {
	line := &processflow.LineReport{
		Kind:  processflow.KindIssue,
		Label: "Issues",
		Stations: []processflow.StageStat{
			{Station: processflow.Open, Label: "Open", Count: 4},
			{Station: processflow.Closed, Label: "Closed", Count: 12},
		},
		Flows: []processflow.StationFlow{{From: processflow.Open, To: processflow.Closed, Count: 12}},
	}
	html := lineDiagram(line)
	for _, want := range []string{
		`class="line-svg"`,
		`x="480" y="47" text-anchor="middle">12</text>`,
		"Open",
		"Closed",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("issue diagram missing %q", want)
		}
	}
}

// TestRenderCapsTheAgingList verifies the rendered page shows at most ten
// aging items and notes the remainder.
func TestRenderCapsTheAgingList(t *testing.T) {
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	items := make([]processflow.Item, 0, wipListCap+3)
	for i := range wipListCap + 3 {
		items = append(items, reportItem(fmt.Sprintf("#%d", i+1), processflow.KindIssue, created.Add(time.Duration(i)*time.Hour), processflow.Open, nil))
	}
	report := processflow.Build(items, created.Add(48*time.Hour), 30)
	if len(report.WIP) != wipListCap+3 {
		t.Fatalf("report WIP = %d items, want %d", len(report.WIP), wipListCap+3)
	}
	html, err := Render(&report, Meta{Repos: []string{"acme/repo1"}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got := strings.Count(html, `class="badge">issue`); got != wipListCap {
		t.Errorf("rendered aging rows = %d, want %d", got, wipListCap)
	}
	if !strings.Contains(html, "and 3 more items on the line") {
		t.Error("rendered report should note the hidden remainder")
	}
	// The oldest items win the visible rows: #1 is the oldest, #13 the newest.
	if !strings.Contains(html, ">#1</a>") {
		t.Error("the oldest item should be visible in the capped table")
	}
	if strings.Contains(html, ">#13</a>") {
		t.Error("the newest item should be hidden by the cap")
	}
}

// TestRenderEmptyReport verifies an empty window still renders (empty-state
// notes instead of charts) rather than failing.
func TestRenderEmptyReport(t *testing.T) {
	report := processflow.Build(nil, time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC), 90)
	html, err := Render(&report, Meta{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(html, "(no repository)") {
		t.Error("empty repo list should render a placeholder label")
	}
	if !strings.Contains(html, "No items in the window.") {
		t.Error("empty report should say the diagram has no data")
	}
}
