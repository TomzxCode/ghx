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
	// starts at the same column whether it says "caching" or "scanning".
	progressStateWidth = len("scanning")
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
	cached   int       // items already in the cache when the run started
	done     int       // items cached so far this run
	total    int       // 0 while unknown, -1 for nothing to fetch, else items to fetch
	scanned  int       // items walked by the prs-scan phase
	started  time.Time // when the current state began, for rate and elapsed
	finished bool      // keeps its row until the last portion ends
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
	case "start":
		p.stateStarted(ev.Phase, ev.Cached)
		if ev.Message != "" {
			p.println(ev.Message)
		}
	case "batch":
		// The newest-first walk that locates the delta window reports how many
		// pull requests it has scanned, not how many it will fetch, so it gets
		// a different kind of row (see scanRow).
		if ev.Phase == "prs-scan" {
			p.scan(ev.Done)
			return
		}
		p.record(ev.Phase, ev.Done, ev.Total)
	case "done":
		if ev.Phase == "prs-scan" {
			return
		}
		p.finish(ev.Phase, ev.Done)
	}
}

// stateStarted opens a portion's current state: it records how much is already
// cached and restarts the clock, so the caching rate and ETA do not include the
// time the delta window walk spent scanning.
func (p *multiProgress) stateStarted(phase string, cached int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pb := p.portionLocked(phase)
	pb.started = time.Now()
	pb.cached = cached
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

// scan records how many pull requests the delta window walk has looked at, so
// the pull request row can show the walk as a bar whose total keeps growing.
func (p *multiProgress) scan(scanned int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pb := p.portionLocked("prs")
	pb.scanned = scanned
	p.repaint()
}

// record updates one portion's counters once a page lands. The bar adopts the
// server-reported fetch total, or the portion is marked as having nothing to
// fetch when that total is zero.
func (p *multiProgress) record(phase string, done, total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pb := p.portionLocked(phase)
	if pb.finished {
		return
	}
	switch {
	case total > 0:
		pb.total = total
	case pb.total == 0:
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
	case pb.total < 0 || (pb.finished && pb.total == 0):
		return p.cachingRow(phase, pb, true)
	case pb.total > 0:
		return p.cachingRow(phase, pb, false)
	case pb.scanned > 0:
		return p.scanRow(phase, pb)
	default:
		return p.startingRow(phase, pb)
	}
}

// startingRow renders a portion's row before its first page: an empty bar with
// zero of zero items, plus how much is already cached.
func (p *multiProgress) startingRow(phase string, pb *portionBar) string {
	stats := "0/0"
	if pb.cached > 0 {
		stats += fmt.Sprintf(", %d cached", pb.cached)
	}
	return fmt.Sprintf("%s%4d%% |%s| (%s)",
		p.stateLabel(phase, "starting"), 0, progressCells(0), stats)
}

// cachingRow renders a portion that is counting items into the cache. The
// counts run from what is already cached to what the cache will hold at the
// end, and upToDate marks a portion with nothing left to fetch.
func (p *multiProgress) cachingRow(phase string, pb *portionBar, upToDate bool) string {
	toFetch := pb.total
	if toFetch < 0 {
		toFetch = 0
	}
	total := pb.cached + toFetch
	done := pb.cached + pb.done
	percent := 100
	if total > 0 {
		percent = done * 100 / total
	}
	if percent > 100 {
		percent = 100
	}

	desc := p.stateLabel(phase, "caching")
	stats := fmt.Sprintf("%d/%d", done, total)
	if upToDate {
		return fmt.Sprintf("%s%4d%% |%s| (%s, up to date)",
			desc, percent, progressCells(percent), stats)
	}

	since := time.Since(pb.started)
	elapsed := since.Round(time.Second)
	if pb.done <= 0 || since <= 0 {
		return fmt.Sprintf("%s%4d%% |%s| (%s) [%s]",
			desc, percent, progressCells(percent), stats, elapsed)
	}

	rate := float64(pb.done) / since.Seconds()
	stats += ", " + itemRate(rate)
	remaining := total - done
	if remaining < 0 {
		remaining = 0
	}
	left := time.Duration(float64(remaining) / rate * float64(time.Second)).Round(time.Second)
	return fmt.Sprintf("%s%4d%% |%s| (%s) [%s:%s]",
		desc, percent, progressCells(percent), stats, elapsed, left)
}

// scanRow renders the pull request row while the delta window walk runs. The
// walk has no total to count against, so the bar stays full and its total grows
// with the number of items scanned: the row shows motion and rate, not
// completion.
func (p *multiProgress) scanRow(phase string, pb *portionBar) string {
	since := time.Since(pb.started)
	stats := fmt.Sprintf("%d/%d", pb.scanned, pb.scanned)
	if since >= time.Second {
		stats += ", " + itemRate(float64(pb.scanned)/since.Seconds())
	}
	return fmt.Sprintf("%s%4d%% |%s| (%s) [%s]",
		p.stateLabel(phase, "scanning"), 100, progressCells(100), stats,
		since.Round(time.Second))
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
