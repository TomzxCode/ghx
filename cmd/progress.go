package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

// barRenderer is a progress display that log output must clear before writing
// and redraw afterwards. It is implemented by multiProgress.
type barRenderer interface {
	Clear() error
	RenderBlank() error
}

const (
	// progressBarWidth is the width of the drawn bar, in cells.
	progressBarWidth = 30
	// progressLabelWidth pads portion labels so stacked rows line up; it is
	// the width of the longest label.
	progressLabelWidth = len("pull requests")
	// progressStateWidth pads state words to the longest one, so a row's bar
	// starts at the same column in every state.
	progressStateWidth = len("starting")
)

// progressWriter returns os.Stderr when it is a terminal device, otherwise
// io.Discard so progress bars never clutter captured or piped output.
func progressWriter() io.Writer {
	if fi, err := os.Stderr.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return os.Stderr
	}
	return io.Discard
}

// cliFetchProgress renders fetch progress on the CLI: one stacked row per
// portion being fetched, plus a row for the remaining GitHub API points.
// phases lists the portions the run will fetch, in display order, so their
// rows appear before the first page arrives.
func cliFetchProgress(client *github.Client, phases ...string) FetchProgress {
	return newMultiProgress(client, phases).handle
}

// multiProgress renders one row per fetched portion, stacked in the order the
// phases were declared. Rows appear as soon as the run starts and stay in place
// until the last portion finishes: a portion that had nothing to fetch keeps its
// row showing the cache it already had.
//
// Each row is drawn from its portion's counters: unlike a single progress bar,
// the rows repaint as a block, moving the cursor over the rows painted last
// time. A single progress bar cannot do this because it clears only the line its
// cursor is on.
type multiProgress struct {
	status func() string
	writer io.Writer

	mu       sync.Mutex
	portions map[string]*portionBar
	order    []string // portions with a row, in display order
	active   int      // portions started and not yet finished
	rows     int      // rows painted by the last repaint
}

// portionBar is one portion's counters. The total shown on its row is the
// number of items the cache will hold at the end of the run: the items already
// cached plus the items this run fetches, so a refresh starts part filled
// instead of at zero.
type portionBar struct {
	cached     int       // items already in the cache when the run started
	cacheTotal int       // total items of this type in the repository (0 unknown)
	done       int       // items cached so far this run
	total      int       // items this run expects to fetch: 0 unknown, -1 nothing to fetch
	started    time.Time // when the current state began, for rate and elapsed
	finished   bool      // keeps its row until the last portion ends
	finishedAt time.Time // when the portion finished, so its clock stops
}

// newMultiProgress creates the stacked renderer for the given phases. It
// registers itself so log lines clear and redraw the stack instead of landing
// in the middle of it.
func newMultiProgress(client *github.Client, phases []string) *multiProgress {
	p := &multiProgress{
		status:   rateLimitStatus(client),
		writer:   progressWriter(),
		portions: map[string]*portionBar{},
		active:   len(phases),
	}
	for _, phase := range phases {
		p.portions[phase] = p.newPortion(phase)
		p.order = append(p.order, phase)
	}
	if p.writer == os.Stderr {
		setActiveBar(p)
	}
	p.repaint()
	return p
}

func (p *multiProgress) handle(ev FetchEvent) {
	switch ev.Kind {
	case "notice":
		p.println(ev.Message)
	case "start":
		p.stateStarted(ev.Phase, ev.Cached, ev.CacheTotal)
		if ev.Message != "" {
			p.println(ev.Message)
		}
	case "batch":
		p.record(ev.Phase, ev.Done, ev.Total, ev.CacheTotal)
	case "done":
		p.finish(ev.Phase, ev.Done)
	}
}

// stateStarted opens a portion's current state: it records how much is already
// cached and restarts the clock, so the caching rate and ETA describe this run.
func (p *multiProgress) stateStarted(phase string, cached, cacheTotal int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pb := p.portionLocked(phase)
	pb.started = time.Now()
	pb.cached = cached
	if cacheTotal > 0 {
		pb.cacheTotal = cacheTotal
	}
}

// portionLocked returns a portion's row, adding it to the stack if the phase
// was not declared up front.
func (p *multiProgress) portionLocked(phase string) *portionBar {
	if pb := p.portions[phase]; pb != nil {
		return pb
	}
	pb := p.newPortion(phase)
	p.portions[phase] = pb
	p.order = append(p.order, phase)
	p.active++
	return pb
}

// newPortion creates a portion whose clock is already running.
func (p *multiProgress) newPortion(phase string) *portionBar {
	return &portionBar{started: time.Now()}
}

// record updates one portion's counters once a page lands. The bar adopts the
// server-reported fetch total, or the portion is marked as having nothing to
// fetch when that total is zero.
func (p *multiProgress) record(phase string, done, total, cacheTotal int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pb := p.portionLocked(phase)
	if pb.finished {
		return
	}
	if cacheTotal > 0 {
		pb.cacheTotal = cacheTotal
	}
	switch {
	case total > 0:
		pb.total = total
	case done == 0 && pb.total == 0:
		pb.total = -1
	}
	pb.done = done
	p.repaint()
}

// finish completes one portion: its row keeps showing the cache it now holds
// while the other portions run, its summary is printed, and the whole stack is
// cleared once no portion is left running.
func (p *multiProgress) finish(phase string, count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pb := p.portionLocked(phase)
	pb.finished = true
	pb.finishedAt = time.Now()
	p.active--
	last := p.active <= 0
	p.clear()
	switch phase {
	case "issues":
		fmt.Printf("Cached %d issue(s).\n", count)
	case "prs":
		fmt.Printf("Cached %d pull request(s).\n", count)
	}
	if last {
		if p.writer == os.Stderr {
			setActiveBar(nil)
		}
		return
	}
	p.repaint()
}

// println prints a message above the stack, so it never lands inside a bar.
func (p *multiProgress) println(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
	fmt.Println(msg)
	p.repaint()
}

// repaint redraws the stack: one row per portion, then the rate limit status on
// a row of its own.
func (p *multiProgress) repaint() {
	if p.writer == io.Discard {
		return
	}
	lines := make([]string, 0, len(p.order)+1)
	for _, phase := range p.order {
		lines = append(lines, p.row(phase))
	}
	if status := p.status(); status != "" && len(p.order) > 0 {
		lines = append(lines, status)
	}
	p.paint(lines)
}

// row renders one portion's line, according to the state it is in.
func (p *multiProgress) row(phase string) string {
	pb := p.portions[phase]
	if pb == nil {
		return ""
	}
	switch {
	case pb.total < 0 || (pb.finished && pb.total == 0 && pb.done == 0):
		return p.cachingRow(phase, pb, true)
	case pb.total > 0 || pb.done > 0:
		return p.cachingRow(phase, pb, false)
	default:
		return p.startingRow(phase, pb)
	}
}

// startingRow renders a portion's row before its first page: an empty bar with
// zero updated so far, plus how much is already cached out of the repository
// total.
func (p *multiProgress) startingRow(phase string, pb *portionBar) string {
	stats := "0/0 updated"
	switch {
	case pb.cacheTotal > 0:
		stats += fmt.Sprintf(", %d/%d cached", pb.cached, pb.cacheTotal)
	case pb.cached > 0:
		stats += fmt.Sprintf(", %d cached", pb.cached)
	}
	return fmt.Sprintf("%s%4d%% |%s| (%s)",
		p.stateLabel(phase, "starting"), 0, progressCells(0), stats)
}

// cachingRow renders a portion counting the items fetched by this run. The bar
// and ETA describe the current update, while the trailing figures keep the
// cache's overall fill visible: "done/window updated, cached/cache total
// cached". A zero window means the update has no known size yet; the row then
// shows motion and rate without a percentage.
func (p *multiProgress) cachingRow(phase string, pb *portionBar, upToDate bool) string {
	window := pb.total
	if window < 0 {
		window = 0
	}
	// The search count used to size a seeded walk can lag the live connection
	// by a few items. Let the denominator grow rather than showing 148/147.
	if pb.done > window {
		window = pb.done
	}
	percent := 0
	if upToDate {
		percent = 100
	} else if window > 0 {
		percent = pb.done * 100 / window
		if percent > 100 {
			percent = 100
		}
	} else if pb.done > 0 {
		percent = 100
	}

	cacheTotal := pb.cacheTotal
	if cacheTotal <= 0 {
		cacheTotal = pb.cached + window
	}
	cacheDone := pb.cached + pb.done
	if cacheDone > cacheTotal {
		cacheDone = cacheTotal
	}

	desc := p.stateLabel(phase, "caching")
	var stats string
	switch {
	case upToDate:
		stats = fmt.Sprintf("0/0 updated, %d/%d cached, up to date", cacheDone, cacheTotal)
	case window > 0:
		stats = fmt.Sprintf("%d/%d updated, %d/%d cached", pb.done, window, cacheDone, cacheTotal)
	default:
		stats = fmt.Sprintf("%d updated, %d/%d cached", pb.done, cacheDone, cacheTotal)
	}

	since := time.Since(pb.started)
	if pb.finished && !pb.finishedAt.IsZero() {
		// A finished portion keeps its row while the other one runs, but its
		// clock must not keep ticking.
		since = pb.finishedAt.Sub(pb.started)
	}
	elapsed := since.Round(time.Second)
	if upToDate {
		return fmt.Sprintf("%s%4d%% |%s| (%s)", desc, percent, progressCells(percent), stats)
	}
	if pb.done <= 0 || since <= 0 {
		return fmt.Sprintf("%s%4d%% |%s| (%s) [%s]", desc, percent, progressCells(percent), stats, elapsed)
	}

	rate := float64(pb.done) / since.Seconds()
	stats += ", " + itemRate(rate)
	if window <= 0 {
		return fmt.Sprintf("%s%4d%% |%s| (%s) [%s]",
			desc, percent, progressCells(percent), stats, elapsed)
	}
	remaining := window - pb.done
	if remaining < 0 {
		remaining = 0
	}
	left := time.Duration(float64(remaining) / rate * float64(time.Second)).Round(time.Second)
	if pb.finished {
		left = 0
	}
	return fmt.Sprintf("%s%4d%% |%s| (%s) [%s:%s]",
		desc, percent, progressCells(percent), stats, elapsed, left)
}

// progressCells renders the bar cells between its two borders for a percentage.
func progressCells(percent int) string {
	filled := percent * progressBarWidth / 100
	if filled < 0 {
		filled = 0
	}
	if filled > progressBarWidth {
		filled = progressBarWidth
	}
	return strings.Repeat("█", filled) + strings.Repeat(" ", progressBarWidth-filled)
}

// itemRate formats a rate with the unit that keeps its number readable.
func itemRate(perSecond float64) string {
	switch {
	case perSecond >= 1:
		return fmt.Sprintf("%.0f items/s", perSecond)
	case perSecond*60 >= 1:
		return fmt.Sprintf("%.0f items/min", perSecond*60)
	default:
		return fmt.Sprintf("%.0f items/hr", perSecond*3600)
	}
}

// stateLabel builds a row description: the padded phase label plus the padded
// state, so every row's bar starts at the same column and a row can say what it
// is doing instead of only showing numbers.
func (p *multiProgress) stateLabel(phase, state string) string {
	return fmt.Sprintf("%-*s %-*s", progressLabelWidth, fetchPhaseLabel(phase), progressStateWidth, state)
}

// paint writes lines as a stack of rows, replacing the rows painted last time.
// It assumes the cursor sits at the end of the last painted row and leaves it
// at the end of the new last row.
func (p *multiProgress) paint(lines []string) {
	var b strings.Builder
	if p.rows > 0 {
		b.WriteString("\r")
		if p.rows > 1 {
			fmt.Fprintf(&b, "\033[%dA", p.rows-1)
		}
	}
	rows := p.rows
	if len(lines) > rows {
		rows = len(lines)
	}
	for i := 0; i < rows; i++ {
		b.WriteString("\033[2K") // erase the row before drawing over it
		if i < len(lines) {
			b.WriteString(lines[i])
		}
		if i < rows-1 {
			b.WriteString("\r\n")
		}
	}
	if p.rows > len(lines) {
		// Rows below the stack stay cleared, and the cursor returns to the
		// last line still showing a row.
		up := p.rows - len(lines)
		if len(lines) == 0 {
			up--
		}
		if up > 0 {
			fmt.Fprintf(&b, "\033[%dA", up)
		}
		b.WriteString("\r")
	}
	p.rows = len(lines)
	_, _ = io.WriteString(p.writer, b.String())
}

// clear erases the painted stack, leaving the cursor where its first row was.
func (p *multiProgress) clear() {
	p.paint(nil)
}

// Clear erases the stack so a log line can take its place.
func (p *multiProgress) Clear() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
	return nil
}

// RenderBlank repaints the stack after a log line was written.
func (p *multiProgress) RenderBlank() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.repaint()
	return nil
}

// fetchPhaseLabel maps a fetch phase onto its progress bar label.
func fetchPhaseLabel(phase string) string {
	switch phase {
	case "prs":
		return "pull requests"
	default:
		return phase
	}
}

// rateLimitStatus returns a status closure for the progress stack that surfaces
// the live x-ratelimit-remaining value from the last API response, or an empty
// string before the first response arrives.
func rateLimitStatus(client *github.Client) func() string {
	return func() string {
		if n := client.RateLimitRemaining(); n >= 0 {
			return fmt.Sprintf("%d points left", n)
		}
		return ""
	}
}
