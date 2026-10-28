package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tomzxcode/ghx/internal/github"
)

// newTestProgress builds a multiProgress over a buffer with no rate limit
// status, mirroring what newMultiProgress sets up.
func newTestProgress(out *bytes.Buffer, phases ...string) *multiProgress {
	p := &multiProgress{
		status:   func() string { return "" },
		writer:   out,
		portions: map[string]*portionBar{},
		active:   len(phases),
	}
	for _, phase := range phases {
		p.portions[phase] = p.newPortion(phase)
		p.order = append(p.order, phase)
	}
	return p
}

// TestNewMultiProgress_DeclaresEveryRow verifies the stack knows all its rows
// before any page arrives, so they are painted immediately instead of waiting
// for the first data increment.
func TestNewMultiProgress_DeclaresEveryRow(t *testing.T) {
	client, err := github.NewClientWithURL("http://example.invalid/graphql", "token", "mock")
	if err != nil {
		t.Fatalf("NewClientWithURL: %v", err)
	}
	p := newMultiProgress(client, []string{"issues", "prs"})

	if got, want := strings.Join(p.order, ","), "issues,prs"; got != want {
		t.Errorf("rows = %q, want %q", got, want)
	}
	if p.active != 2 {
		t.Errorf("active portions = %d, want 2", p.active)
	}
	row := p.row("issues")
	if !strings.Contains(row, "starting") {
		t.Errorf("a row with no data yet should say it is starting, got %q", row)
	}
	if !strings.Contains(row, "(0/0)") {
		t.Errorf("a row with no data yet should be an empty 0/0 bar, got %q", row)
	}
}

// TestMultiProgress_ShowsScanProgress verifies the pull request row reports the
// delta window walk as a bar whose total grows with the scanned count.
func TestMultiProgress_ShowsScanProgress(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "prs")
	p.handle(FetchEvent{Phase: "prs-scan", Kind: "batch", Done: 312})
	if !strings.Contains(out.String(), "scanning") {
		t.Errorf("row should say it is scanning, got %q", out.String())
	}
	if !strings.Contains(out.String(), "(312/312") {
		t.Errorf("row should count the scanned items, got %q", out.String())
	}

	out.Reset()
	p.handle(FetchEvent{Phase: "prs-scan", Kind: "batch", Done: 730})
	if !strings.Contains(out.String(), "(730/730") {
		t.Errorf("the scan total should grow, got %q", out.String())
	}
}

// TestMultiProgress_ShowsCachingState verifies a row with a known total says it
// is caching rather than only showing numbers.
func TestMultiProgress_ShowsCachingState(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "issues")
	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 5, Total: 10})
	if !strings.Contains(out.String(), "caching") {
		t.Errorf("row should say it is caching, got %q", out.String())
	}
}

// TestMultiProgress_AlignsBars verifies rows in different states put their bar
// at the same column, so a refresh where one portion scans and the other caches
// still lines up.
func TestMultiProgress_AlignsBars(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "issues", "prs")
	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 5, Total: 10})
	p.handle(FetchEvent{Phase: "prs-scan", Kind: "batch", Done: 300})

	caching := p.row("issues")
	scanning := p.row("prs")
	cachingBar := strings.Index(caching, "|")
	scanningBar := strings.Index(scanning, "|")
	if cachingBar < 0 || scanningBar < 0 {
		t.Fatalf("both rows should have a bar, got %q and %q", caching, scanning)
	}
	if cachingBar != scanningBar {
		t.Errorf("bar columns differ: caching at %d (%q), scanning at %d (%q)",
			cachingBar, caching, scanningBar, scanning)
	}
}

// TestMultiProgress_CountsFromCache verifies a refresh starts from what is
// already cached and grows to the cache's final size.
func TestMultiProgress_CountsFromCache(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "issues")
	p.handle(FetchEvent{Phase: "issues", Kind: "start", Cached: 500})
	if row := p.row("issues"); !strings.Contains(row, "500 cached") {
		t.Errorf("starting row should show the cached count, got %q", row)
	}

	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 5, Total: 10})
	row := p.row("issues")
	if !strings.Contains(row, "(505/510") {
		t.Errorf("row should count from the cached items, got %q", row)
	}
	if !strings.Contains(row, " 99%") {
		t.Errorf("row should be 99%% of the final cache, got %q", row)
	}
}

// TestMultiProgress_AlreadySyncedRow verifies a portion with nothing to fetch
// still reports 100% instead of leaving an empty or spinning row.
func TestMultiProgress_AlreadySyncedRow(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "issues", "prs")
	p.handle(FetchEvent{Phase: "issues", Kind: "start", Cached: 42})
	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 0, Total: 0})
	p.handle(FetchEvent{Phase: "issues", Kind: "done", Done: 0})

	row := p.row("issues")
	if !strings.Contains(row, "100%") || !strings.Contains(row, "up to date") {
		t.Errorf("a synced portion should show a full row, got %q", row)
	}
	if !strings.Contains(row, "(42/42") {
		t.Errorf("a synced portion should count the cached items, got %q", row)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("the painted stack should show the full row, got %q", out.String())
	}
	if !p.portions["issues"].finished {
		t.Error("the issue portion should be marked finished")
	}
}

// TestMultiProgress_KeepsFinishedRow verifies a portion that finished keeps its
// row at 100% while the other portion still runs.
func TestMultiProgress_KeepsFinishedRow(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "issues", "prs")
	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 5, Total: 5})
	p.handle(FetchEvent{Phase: "prs", Kind: "batch", Done: 2, Total: 4})
	p.handle(FetchEvent{Phase: "issues", Kind: "done", Done: 5})

	if p.rows != 2 {
		t.Errorf("painted rows = %d, want 2 (both portions keep a row)", p.rows)
	}
	if !p.portions["issues"].finished {
		t.Error("the issue portion should be marked finished")
	}
	if !strings.Contains(out.String(), "up to date") && !strings.Contains(out.String(), "100%") {
		t.Errorf("the finished row should still be painted, got %q", out.String())
	}

	p.handle(FetchEvent{Phase: "prs", Kind: "done", Done: 4})
	if p.rows != 0 {
		t.Errorf("painted rows after the run = %d, want 0", p.rows)
	}
}

// TestMultiProgress_RendersPointsLeftOnItsOwnLine verifies the rate limit
// figure gets a row of its own below the bars.
func TestMultiProgress_RendersPointsLeftOnItsOwnLine(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "issues", "prs")
	p.status = func() string { return "4321 points left" }
	out.Reset()
	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 5, Total: 10})

	painted := out.String()
	if got := strings.Count(painted, "points left"); got != 1 {
		t.Fatalf("points left should be painted once, got %d in %q", got, painted)
	}
	if !strings.HasSuffix(painted, "\r\n\x1b[2K4321 points left") {
		t.Errorf("points left should be the last row, got %q", painted)
	}
	if p.rows != 3 {
		t.Errorf("painted rows = %d, want 3 (two barriers and the rate limit line)", p.rows)
	}
}

// TestMultiProgressPaint_StacksAndShrinks pins the cursor arithmetic: a paint
// moves up over the rows it drew last time, erases them, and leaves the cursor
// on the last row still showing a bar.
func TestMultiProgressPaint_StacksAndShrinks(t *testing.T) {
	var out bytes.Buffer
	p := &multiProgress{writer: &out}

	p.paint([]string{"issues 5/10", "pull requests 2/4"})
	if got, want := out.String(), "\033[2Kissues 5/10\r\n\033[2Kpull requests 2/4"; got != want {
		t.Errorf("first paint = %q, want %q", got, want)
	}

	out.Reset()
	p.paint([]string{"pull requests 3/4"})
	if got, want := out.String(), "\r\033[1A\033[2Kpull requests 3/4\r\n\033[2K\033[1A\r"; got != want {
		t.Errorf("shrink paint = %q, want %q", got, want)
	}

	out.Reset()
	p.paint(nil)
	if got, want := out.String(), "\r\033[2K\r"; got != want {
		t.Errorf("clear paint = %q, want %q", got, want)
	}
	if p.rows != 0 {
		t.Errorf("painted rows = %d, want 0", p.rows)
	}
}
