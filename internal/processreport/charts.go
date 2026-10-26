// Package processreport renders the production-line flow report as a
// self-contained HTML page: inline CSS and SVG charts, no external assets, so
// the file works offline and the web UI can serve the same bytes it renders.
package processreport

import (
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/processflow"
)

// Meta describes the report run beyond the numbers.
type Meta struct {
	// Repos are the display labels for the repositories in the report.
	Repos []string
	// RepoIDs holds the raw host/owner/repo identifiers of the request, for
	// the interactive window form's hidden inputs.
	RepoIDs []string
	// Interactive marks a report served by the web UI: the template renders
	// the window form that reloads /process with a new window. CLI output
	// stays a static document.
	Interactive bool
	// AllRepos reports whether the request covered every cached repository
	// (no explicit repo list), so the form can resubmit without pinning the
	// repository set.
	AllRepos bool
	// HasRange marks a report that is not the trailing days window (an
	// explicit from/to range or all available history): the meta line shows
	// the covered span.
	HasRange bool
	// FromValue, ToValue, and DaysValue echo the requested bounds for the
	// interactive form's inputs; empty means unset, which for the dates
	// means the entire available history.
	FromValue string
	ToValue   string
	DaysValue string
}

// stationColors assigns one hue per station, shared by every chart so a
// color always means the same station.
var stationColors = map[processflow.Station]string{
	processflow.Open:           "#64748b",
	processflow.Draft:          "#9333ea",
	processflow.AwaitingReview: "#2563eb",
	processflow.AwaitingAuthor: "#d97706",
	processflow.AwaitingMerge:  "#0d9488",
	processflow.Closed:         "#16a34a",
	processflow.Merged:         "#15803d",
	processflow.Discarded:      "#94a3b8",
	processflow.Unsplit:        "#6b7280",
}

func stationColor(station processflow.Station) string {
	if color, ok := stationColors[station]; ok {
		return color
	}
	return "#64748b"
}

// Render builds the report page for one computed report.
func Render(report *processflow.Report, meta Meta) (string, error) {
	if len(meta.Repos) == 0 {
		meta.Repos = []string{"(no repository)"}
	}
	tmpl, err := template.New("process").Funcs(template.FuncMap{
		"hours": formatHours,
		"pct":   formatPct,
		"color": stationColor,
	}).Parse(reportTemplate)
	if err != nil {
		return "", err
	}
	view := buildView(report, meta)
	var out strings.Builder
	if err := tmpl.Execute(&out, view); err != nil {
		return "", err
	}
	return out.String(), nil
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

// formatHours renders a duration in human units: hours below two days, then
// days, then months.
func formatHours(hours *float64) string {
	if hours == nil {
		return "–"
	}
	h := *hours
	switch {
	case h < 48:
		return fmt.Sprintf("%.0fh", h)
	case h < 24*60:
		return fmt.Sprintf("%.1fd", h/24)
	default:
		return fmt.Sprintf("%.1fmo", h/(24*30))
	}
}

func formatPct(share float64) string {
	return fmt.Sprintf("%.0f%%", share*100)
}

func formatDate(t time.Time) string {
	return t.Format("Jan 2, 2006")
}

// ---------------------------------------------------------------------------
// Chart builders (server-side SVG)
// ---------------------------------------------------------------------------

// stackedArea renders the cumulative flow diagram: one band per active
// station (open issues, draft PRs, ready PRs) with cumulative completions
// stacked on top. A widening band is a queue forming; the top edge is total
// output to date.
func stackedArea(samples []processflow.FlowSample) string {
	if len(samples) < 2 {
		return "<div class=\"chart-empty\">Not enough data for a cumulative flow diagram.</div>"
	}
	width, height, pad := 720.0, 220.0, 28.0
	plotW, plotH := width-2*pad, height-2*pad

	// Stacked series, bottom to top.
	s0 := make([]float64, len(samples)) // open issues
	s1 := make([]float64, len(samples)) // + draft PRs
	s2 := make([]float64, len(samples)) // + PRs waiting on reviewer
	s3 := make([]float64, len(samples)) // + PRs waiting on author
	s4 := make([]float64, len(samples)) // + PRs waiting on merge
	s5 := make([]float64, len(samples)) // + completions
	maxY := 0.0
	for i, s := range samples {
		s0[i] = float64(s.OpenIssues)
		s1[i] = s0[i] + float64(s.DraftPRs)
		s2[i] = s1[i] + float64(s.ReviewPRs)
		s3[i] = s2[i] + float64(s.AuthorPRs)
		s4[i] = s3[i] + float64(s.MergePRs)
		s5[i] = s4[i] + float64(s.Completed)
		maxY = math.Max(maxY, s5[i])
	}
	if maxY == 0 {
		return "<div class=\"chart-empty\">No items in the window.</div>"
	}
	n := float64(len(samples) - 1)
	x := func(i int) float64 { return pad + plotW*float64(i)/n }
	y := func(v float64) float64 { return pad + plotH*(1-v/maxY) }

	polygon := func(lower, upper []float64, color string) string {
		var points strings.Builder
		for i := range samples {
			fmt.Fprintf(&points, "%.1f,%.1f ", x(i), y(lower[i]))
		}
		for i := len(samples) - 1; i >= 0; i-- {
			fmt.Fprintf(&points, "%.1f,%.1f ", x(i), y(upper[i]))
		}
		return fmt.Sprintf(`<polygon points="%s" fill="%s" opacity="0.85"/>`, strings.TrimSpace(points.String()), color)
	}
	zero := make([]float64, len(samples))

	var doneLine strings.Builder
	for i := range samples {
		if i > 0 {
			doneLine.WriteString(" ")
		}
		fmt.Fprintf(&doneLine, "%.1f,%.1f", x(i), y(s4[i]))
	}

	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg viewBox="0 0 %v %v" class="chart" role="img" aria-label="Cumulative flow diagram">`, width, height)
	for q := 1; q <= 3; q++ {
		yy := pad + plotH*float64(q)/4
		fmt.Fprintf(&svg, `<line x1="%v" y1="%.1f" x2="%v" y2="%.1f" class="grid"/><text x="%v" y="%.1f" class="tick" text-anchor="end">%.0f</text>`,
			pad-4, yy, width-pad, yy, pad-6, yy+3, maxY*float64(4-q)/4)
	}
	svg.WriteString(polygon(zero, s0, stationColor(processflow.Open)))
	svg.WriteString(polygon(s0, s1, stationColor(processflow.Draft)))
	svg.WriteString(polygon(s1, s2, stationColor(processflow.AwaitingReview)))
	svg.WriteString(polygon(s2, s3, stationColor(processflow.AwaitingAuthor)))
	svg.WriteString(polygon(s3, s4, stationColor(processflow.AwaitingMerge)))
	svg.WriteString(polygon(s4, s5, "#16a34a"))
	fmt.Fprintf(&svg, `<polyline points="%s" fill="none" stroke="#15803d" stroke-width="2"/>`, doneLine.String())
	for _, i := range []int{0, len(samples) / 2, len(samples) - 1} {
		fmt.Fprintf(&svg, `<text x="%.1f" y="%v" class="tick" text-anchor="middle">%s</text>`,
			x(i), height-6, samples[i].At.Format("Jan 2"))
	}
	svg.WriteString("</svg>")
	return svg.String()
}

// weeklyBars renders grouped bars: arrivals and completions per week.
func weeklyBars(weeks []processflow.FlowWeek, createdLabel, completedLabel string, createdColor, completedColor string) string {
	if len(weeks) == 0 {
		return ""
	}
	width, height, pad := 720.0, 160.0, 24.0
	plotW, plotH := width-2*pad, height-2*pad-14
	maxV := 1
	for _, w := range weeks {
		maxV = maxInt(maxV, w.Created, w.Completed)
	}
	n := float64(len(weeks))
	slot := plotW / n
	barW := slot * 0.38
	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg viewBox="0 0 %v %v" class="chart" role="img" aria-label="%s and %s per week">`, width, height, createdLabel, completedLabel)
	for i, w := range weeks {
		cx := pad + slot*float64(i) + slot/2
		ch := plotH * float64(w.Created) / float64(maxV)
		mh := plotH * float64(w.Completed) / float64(maxV)
		fmt.Fprintf(&svg, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"><title>%s: %d created</title></rect>`,
			cx-barW-1, pad+plotH-ch, barW, ch, createdColor, w.Start.Format("Jan 2"), w.Created)
		fmt.Fprintf(&svg, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"><title>%s: %d completed</title></rect>`,
			cx+1, pad+plotH-mh, barW, mh, completedColor, w.Start.Format("Jan 2"), w.Completed)
		if i%4 == 0 {
			fmt.Fprintf(&svg, `<text x="%.1f" y="%v" class="tick" text-anchor="middle">%s</text>`, cx, height-6, w.Start.Format("Jan 2"))
		}
	}
	fmt.Fprintf(&svg, `<line x1="%v" y1="%.1f" x2="%v" y2="%.1f" class="axis"/>`, pad, pad+plotH, width-pad, pad+plotH)
	svg.WriteString("</svg>")
	return svg.String()
}

// histogramBars renders the cycle-time histogram.
func histogramBars(buckets []processflow.HistogramBucket, color string) string {
	if len(buckets) == 0 {
		return ""
	}
	width, height, pad := 340.0, 140.0, 22.0
	plotW, plotH := width-2*pad, height-2*pad-12
	maxV := 1
	for _, b := range buckets {
		maxV = maxInt(maxV, b.Count)
	}
	slot := plotW / float64(len(buckets))
	barW := slot * 0.7
	var svg strings.Builder
	fmt.Fprintf(&svg, `<svg viewBox="0 0 %v %v" class="chart" role="img" aria-label="Cycle time histogram">`, width, height)
	for i, b := range buckets {
		bh := plotH * float64(b.Count) / float64(maxV)
		x := pad + slot*float64(i) + (slot-barW)/2
		fmt.Fprintf(&svg, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"><title>%s: %d</title></rect>`,
			x, pad+plotH-bh, barW, bh, color, b.Label, b.Count)
		if b.Count > 0 {
			fmt.Fprintf(&svg, `<text x="%.1f" y="%.1f" class="tick" text-anchor="middle">%d</text>`, x+barW/2, pad+plotH-bh-3, b.Count)
		}
		fmt.Fprintf(&svg, `<text x="%.1f" y="%v" class="tick" text-anchor="middle">%s</text>`, x+barW/2, height-4, b.Label)
	}
	svg.WriteString("</svg>")
	return svg.String()
}

// sparkline renders a tiny inventory trend line for the queue table.
func sparkline(points []int, color string) string {
	if len(points) < 2 {
		return ""
	}
	width, height := 96.0, 24.0
	maxV := 1
	for _, p := range points {
		maxV = maxInt(maxV, p)
	}
	n := float64(len(points) - 1)
	var path strings.Builder
	for i, p := range points {
		if i > 0 {
			path.WriteString(" ")
		}
		fmt.Fprintf(&path, "%.1f,%.1f", width*float64(i)/n, height-2-(height-4)*float64(p)/float64(maxV))
	}
	return fmt.Sprintf(`<svg viewBox="0 0 %v %v" class="spark" role="img" aria-label="weekly inventory trend"><polyline points="%s" fill="none" stroke="%s" stroke-width="1.5"/></svg>`, width, height, path.String(), color)
}

// waitShareBar renders the horizontal stacked bar of where completed items'
// time went.
func waitShareBar(shares []processflow.WaitShare) string {
	var svg strings.Builder
	svg.WriteString(`<div class="share-bar" role="img" aria-label="share of completed cycle time per station">`)
	for _, share := range shares {
		if share.Share <= 0 {
			continue
		}
		fmt.Fprintf(&svg, `<span class="share-seg" style="width:%s;background:%s" title="%s: %s (%s)"></span>`,
			formatPct(share.Share), stationColor(share.Station), share.Label, formatHours(&share.TotalHrs), formatPct(share.Share))
	}
	svg.WriteString("</div>")
	return svg.String()
}

// Production-line diagram geometry, in the SVG's user units.
const (
	lineBoxW  = 164.0
	lineBoxH  = 96.0
	lineLeft  = 8.0
	lineMid1  = 268.0 // reviewer and author column
	lineMid2  = 528.0 // merge column
	lineRight = 788.0
)

// lineDiagram renders the production line as an inline SVG graph: station
// cards carry the current inventory and wait, arrows carry the window's move
// counts. The review stations are placed as a loop (reviewer above, author
// below, merge to the right) so approvals that skip a station stay visible,
// which a single row of stations cannot show.
func lineDiagram(line *processflow.LineReport) string {
	if line.Kind == processflow.KindIssue {
		return issueLineDiagram(line)
	}
	return prLineDiagram(line)
}

func issueLineDiagram(line *processflow.LineReport) string {
	var b strings.Builder
	b.WriteString(`<div class="line-scroll">`)
	fmt.Fprintf(&b, `<svg class="line-svg" viewBox="0 0 960 112" role="img" aria-label="%s production line">`,
		template.HTMLEscapeString(line.Label))
	writeArrowMarker(&b, "issues")
	horizontalMoves(&b, 172, 788, 56, flowCount(line, processflow.Open, processflow.Closed), 0, "line-arrow-issues")
	b.WriteString(stationBox(statFor(line, processflow.Open), lineLeft, 8))
	b.WriteString(stationBox(statFor(line, processflow.Closed), lineRight, 8))
	b.WriteString(`</svg></div>`)
	return b.String()
}

func prLineDiagram(line *processflow.LineReport) string {
	// Every observed move is attributed to exactly one drawn edge, so the
	// arrow counts still add up to the window's moves. A move that skips a
	// station (an approval from review, or a close from any station) lands on
	// the edge for the turn it represents.
	draft := processflow.Draft
	review := processflow.AwaitingReview
	author := processflow.AwaitingAuthor
	merge := processflow.AwaitingMerge
	merged := processflow.Merged
	discarded := processflow.Discarded

	readyForward := flowCount(line, draft, review)
	readyBack := flowCount(line, review, draft) + flowCount(line, author, draft) + flowCount(line, merge, draft)
	changesForward := flowCount(line, review, author) + flowCount(line, draft, author)
	changesBack := flowCount(line, author, review)
	approveForward := flowCount(line, review, merge) + flowCount(line, draft, merge)
	approveBack := flowCount(line, merge, review)
	authorApprove := flowCount(line, author, merge)
	authorApproveBack := flowCount(line, merge, author)
	mergeCount := 0
	discardCount := 0
	for _, from := range []processflow.Station{draft, review, author, merge} {
		mergeCount += flowCount(line, from, merged)
		discardCount += flowCount(line, from, discarded)
	}

	var b strings.Builder
	b.WriteString(`<div class="line-scroll">`)
	fmt.Fprintf(&b, `<svg class="line-svg" viewBox="0 0 960 280" role="img" aria-label="%s production line">`,
		template.HTMLEscapeString(line.Label))
	writeArrowMarker(&b, "prs")

	// Main row, left to right: draft, reviewer, merge, merged.
	horizontalMoves(&b, 172, 268, 56, readyForward, readyBack, "line-arrow-prs")
	horizontalMoves(&b, 432, 528, 56, approveForward, approveBack, "line-arrow-prs")
	horizontalMoves(&b, 692, 788, 56, mergeCount, 0, "line-arrow-prs")
	// Reviewer to author (and back), then the author/merge turns.
	verticalMoves(&b, 350, 104, 176, changesForward, changesBack, "line-arrow-prs")
	authorMergeMoves(&b, authorApprove, authorApproveBack, "line-arrow-prs")
	// Merge to discarded, which any open station can reach.
	b.WriteString(`<path class="edge" d="M 660 109 V 152 H 870 V 171" marker-end="url(#line-arrow-prs)"/>`)
	if discardCount > 0 {
		writeMoveLabel(&b, 765, 144, fmt.Sprintf("%d", discardCount))
	}

	b.WriteString(stationBox(statFor(line, draft), lineLeft, 8))
	b.WriteString(stationBox(statFor(line, review), lineMid1, 8))
	b.WriteString(stationBox(statFor(line, merge), lineMid2, 8))
	b.WriteString(stationBox(statFor(line, merged), lineRight, 8))
	b.WriteString(stationBox(statFor(line, author), lineMid1, 176))
	b.WriteString(stationBox(statFor(line, discarded), lineRight, 176))
	b.WriteString(`</svg></div>`)
	return b.String()
}

// statFor returns the inventory entry for one station. Every station of a
// line is always present, so the zero value only guards a future change.
func statFor(line *processflow.LineReport, station processflow.Station) processflow.StageStat {
	for _, stat := range line.Stations {
		if stat.Station == station {
			return stat
		}
	}
	return processflow.StageStat{Station: station, Label: string(station)}
}

// flowCount is the number of moves between two stations in the window.
func flowCount(line *processflow.LineReport, from, to processflow.Station) int {
	for _, flow := range line.Flows {
		if flow.From == from && flow.To == to {
			return flow.Count
		}
	}
	return 0
}

// moveLabel formats an edge's count.
func moveLabel(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("%d", count)
}

// edgeGap is how far each arrow sits from an edge's centerline when both
// directions had moves.
const edgeGap = 5.0

// horizontalMoves draws one arrow per direction that had moves, each with its
// own count, so the arrow carries the direction and the number stays readable.
// With no moves it still draws the forward arrow, keeping the topology visible.
func horizontalMoves(b *strings.Builder, left, right, center float64, forward, back int, marker string) {
	both := forward > 0 && back > 0
	if forward > 0 || back == 0 {
		y := center
		if both {
			y = center - edgeGap
		}
		fmt.Fprintf(b, `<path class="edge" d="M %.0f %.0f H %.0f" marker-end="url(#%s)"/>`,
			left+5, y, right-5, marker)
		writeMoveLabel(b, (left+right)/2, y-9, moveLabel(forward))
	}
	if back > 0 {
		y := center
		if both {
			y = center + edgeGap
		}
		fmt.Fprintf(b, `<path class="edge" d="M %.0f %.0f H %.0f" marker-end="url(#%s)"/>`,
			right-5, y, left+5, marker)
		writeMoveLabel(b, (left+right)/2, y+14, moveLabel(back))
	}
}

// verticalMoves is horizontalMoves for a top-to-bottom edge: the forward
// arrow points down, the back arrow points up, and each label sits outside
// the pair.
func verticalMoves(b *strings.Builder, centerX, top, bottom float64, forward, back int, marker string) {
	both := forward > 0 && back > 0
	if forward > 0 || back == 0 {
		x := centerX
		if both {
			x = centerX - edgeGap
		}
		fmt.Fprintf(b, `<path class="edge" d="M %.0f %.0f V %.0f" marker-end="url(#%s)"/>`,
			x, top+5, bottom-5, marker)
		if forward > 0 {
			labelX, anchor := x+12, "start"
			if both {
				labelX, anchor = x-8, "end"
			}
			writeMoveLabelAt(b, labelX, (top+bottom)/2+4, anchor, moveLabel(forward))
		}
	}
	if back > 0 {
		x := centerX
		if both {
			x = centerX + edgeGap
		}
		fmt.Fprintf(b, `<path class="edge" d="M %.0f %.0f V %.0f" marker-end="url(#%s)"/>`,
			x, bottom-5, top+5, marker)
		writeMoveLabelAt(b, x+12, (top+bottom)/2+4, "start", moveLabel(back))
	}
}

// authorMergeMoves draws the turn between waiting on author and waiting on
// merge as one elbow per direction that had moves.
func authorMergeMoves(b *strings.Builder, forward, back int, marker string) {
	both := forward > 0 && back > 0
	if forward > 0 || back == 0 {
		y, x := 224.0, 610.0
		if both {
			y, x = 219, 605
		}
		fmt.Fprintf(b, `<path class="edge" d="M 437 %.0f H %.0f V 109" marker-end="url(#%s)"/>`, y, x, marker)
		writeMoveLabel(b, 520, y-8, moveLabel(forward))
	}
	if back > 0 {
		y, x := 224.0, 610.0
		if both {
			y, x = 229, 615
		}
		fmt.Fprintf(b, `<path class="edge" d="M %.0f 109 V %.0f H 437" marker-end="url(#%s)"/>`, x, y, marker)
		writeMoveLabel(b, 520, y+14, moveLabel(back))
	}
}

func writeMoveLabel(b *strings.Builder, x, y float64, label string) {
	writeMoveLabelAt(b, x, y, "middle", label)
}

func writeMoveLabelAt(b *strings.Builder, x, y float64, anchor, label string) {
	if label == "" {
		return
	}
	fmt.Fprintf(b, `<text class="moves" x="%.0f" y="%.0f" text-anchor="%s">%s</text>`, x, y, anchor, label)
}

// writeArrowMarker defines the edge arrowhead once per diagram, so the tip
// lands at the path end at a fixed size.
func writeArrowMarker(b *strings.Builder, id string) {
	fmt.Fprintf(b, `<defs><marker id="line-arrow-%s" viewBox="0 0 8 8" refX="8" refY="4" markerWidth="8" markerHeight="8" markerUnits="userSpaceOnUse" orient="auto"><path class="head" d="M 0 0 L 8 4 L 0 8 Z"/></marker></defs>`, id)
}

// stationBox renders one station card: name, current inventory, and either
// the wait of the items there or what the station's count means.
func stationBox(stat processflow.StageStat, x, y float64) string {
	class := "box"
	switch stat.Station {
	case processflow.Closed, processflow.Merged:
		class = "box done"
	case processflow.Discarded:
		class = "box scrap"
	}
	center := x + lineBoxW/2
	var b strings.Builder
	fmt.Fprintf(&b, `<g><rect class="%s" x="%.0f" y="%.0f" width="%.0f" height="%.0f" rx="8"/>`, class, x, y, lineBoxW, lineBoxH)
	fmt.Fprintf(&b, `<rect class="bar" x="%.0f" y="%.0f" width="%.0f" height="3" rx="1.5" fill="%s"/>`,
		x+12, y+8, lineBoxW-24, stationColor(stat.Station))
	fmt.Fprintf(&b, `<text class="name" x="%.0f" y="%.0f" text-anchor="middle">%s</text>`,
		center, y+28, template.HTMLEscapeString(stat.Label))
	fmt.Fprintf(&b, `<text class="count" x="%.0f" y="%.0f" text-anchor="middle">%d</text>`, center, y+58, stat.Count)
	switch {
	case stat.Station == processflow.Closed || stat.Station == processflow.Merged:
		fmt.Fprintf(&b, `<text class="ages" x="%.0f" y="%.0f" text-anchor="middle">completed</text>`, center, y+82)
	case stat.Station == processflow.Discarded:
		fmt.Fprintf(&b, `<text class="ages" x="%.0f" y="%.0f" text-anchor="middle">scrap, not completed</text>`, center, y+82)
	case stat.Count > 0 && stat.OldestHours != nil:
		fmt.Fprintf(&b, `<text class="ages" x="%.0f" y="%.0f" text-anchor="middle">oldest %s</text>`,
			center, y+74, formatHours(stat.OldestHours))
		fmt.Fprintf(&b, `<text class="ages" x="%.0f" y="%.0f" text-anchor="middle">median %s</text>`,
			center, y+88, formatHours(stat.MedianAgeHrs))
	case isActiveStation(stat.Station):
		fmt.Fprintf(&b, `<text class="ages" x="%.0f" y="%.0f" text-anchor="middle">no items waiting</text>`, center, y+82)
	}
	b.WriteString(`</g>`)
	return b.String()
}

// isActiveStation reports whether a station holds unfinished work.
func isActiveStation(station processflow.Station) bool {
	return station == processflow.Open || station == processflow.Draft ||
		station == processflow.AwaitingReview || station == processflow.AwaitingAuthor ||
		station == processflow.AwaitingMerge
}

func maxInt(values ...int) int {
	best := values[0]
	for _, v := range values[1:] {
		if v > best {
			best = v
		}
	}
	return best
}
