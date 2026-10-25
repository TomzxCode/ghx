package processreport

import (
	"strings"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/processflow"
)

func reportItem(id string, kind processflow.Kind, created time.Time, station processflow.Station, exited *time.Time) processflow.Item {
	return processflow.Item{ID: id, Kind: kind, Title: "Item " + id, URL: "https://example.com/" + id, CreatedAt: created, Station: station, ExitedAt: exited}
}

// TestRenderProducesTheSections verifies the report renders every section
// from the computed numbers.
func TestRenderProducesTheSections(t *testing.T) {
	created := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	closed := created.Add(9 * 24 * time.Hour)
	merged := created.Add(5 * 24 * time.Hour)
	report := processflow.Build([]processflow.Item{
		reportItem("#1", processflow.KindIssue, created, processflow.Closed, &closed),
		reportItem("#2", processflow.KindIssue, created.Add(48*time.Hour), processflow.Open, nil),
		reportItem("#10", processflow.KindPR, created, processflow.Merged, &merged),
		reportItem("#11", processflow.KindPR, created.Add(48*time.Hour), processflow.Ready, nil),
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
		"Item #2",     // the aging list links the WIP items
		"Item #11",    // by title
		"svg",         // the charts are inline SVG
		"filling",     // the queue-pressure trend badges
		"Open issues", // the cumulative flow legend
	} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered report missing %q", want)
		}
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
