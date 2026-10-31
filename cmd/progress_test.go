package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
	if !strings.Contains(row, "(0/0 updated)") {
		t.Errorf("a row with no data yet should be an empty 0/0 bar, got %q", row)
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
// at the same column, so a refresh where one portion is starting and the other
// is caching still lines up.
func TestMultiProgress_AlignsBars(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "issues", "prs")
	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 5, Total: 10})
	p.handle(FetchEvent{Phase: "prs", Kind: "start", Cached: 3})

	caching := p.row("issues")
	starting := p.row("prs")
	cachingBar := strings.Index(caching, "|")
	startingBar := strings.Index(starting, "|")
	if cachingBar < 0 || startingBar < 0 {
		t.Fatalf("both rows should have a bar, got %q and %q", caching, starting)
	}
	if cachingBar != startingBar {
		t.Errorf("bar columns differ: caching at %d (%q), other at %d (%q)",
			cachingBar, caching, startingBar, starting)
	}
}

// TestMultiProgress_CacheDenominatorIsRepositoryTotal verifies the cached
// figure uses the repository total, not the size the cache will reach when the
// operation ends.
func TestMultiProgress_CacheDenominatorIsRepositoryTotal(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "issues")
	p.handle(FetchEvent{Phase: "issues", Kind: "start", Cached: 500, CacheTotal: 1000})
	if row := p.row("issues"); !strings.Contains(row, "500/1000 cached") {
		t.Errorf("starting row should show the repository total, got %q", row)
	}

	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 5, Total: 10, CacheTotal: 1000})
	row := p.row("issues")
	if !strings.Contains(row, "(5/10 updated, 505/1000 cached") {
		t.Errorf("cached figure should be a share of the repository, got %q", row)
	}
}

// TestMultiProgress_CountsFromCache verifies the bar tracks this run's update
// while the trailing figures keep the cache's overall fill visible.
func TestMultiProgress_CountsFromCache(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "issues")
	p.handle(FetchEvent{Phase: "issues", Kind: "start", Cached: 500})
	if row := p.row("issues"); !strings.Contains(row, "500 cached") {
		t.Errorf("starting row should show the cached count, got %q", row)
	}

	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 5, Total: 10})
	row := p.row("issues")
	if !strings.Contains(row, "(5/10 updated, 505/510 cached") {
		t.Errorf("row should separate update progress from cache fill, got %q", row)
	}
	if !strings.Contains(row, " 50%") {
		t.Errorf("row should be 50%% of this run's window, got %q", row)
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
	if !strings.Contains(row, "0/0 updated, 42/42 cached, up to date") {
		t.Errorf("a synced portion should show its cache fill, got %q", row)
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

// TestMultiProgress_FinishedRowFreezesTime verifies a portion that finished
// keeps its final elapsed time while the other portion is still running.
func TestMultiProgress_FinishedRowFreezesTime(t *testing.T) {
	var out bytes.Buffer
	p := newTestProgress(&out, "issues", "prs")
	p.handle(FetchEvent{Phase: "issues", Kind: "batch", Done: 10, Total: 10})
	p.handle(FetchEvent{Phase: "issues", Kind: "done", Done: 10})

	pb := p.portions["issues"]
	pb.started = time.Now().Add(-10 * time.Second)
	pb.finishedAt = pb.started.Add(8 * time.Second)

	row := p.row("issues")
	if !strings.Contains(row, "[8s:0s]") {
		t.Errorf("finished row should freeze at 8s with 0s left, got %q", row)
	}
	if !strings.Contains(row, "10/10 updated") {
		t.Errorf("finished row should keep its final counts, got %q", row)
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
