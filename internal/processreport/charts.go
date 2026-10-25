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
	Repos []string
}

// stationColors assigns one hue per station, shared by every chart so a
// color always means the same station.
var stationColors = map[processflow.Station]string{
	processflow.Open:      "#64748b",
	processflow.Draft:     "#d97706",
	processflow.Ready:     "#2563eb",
	processflow.Closed:    "#16a34a",
	processflow.Merged:    "#15803d",
	processflow.Discarded: "#94a3b8",
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
	s2 := make([]float64, len(samples)) // + ready PRs
	s3 := make([]float64, len(samples)) // + completions
	maxY := 0.0
	for i, s := range samples {
		s0[i] = float64(s.OpenIssues)
		s1[i] = s0[i] + float64(s.DraftPRs)
		s2[i] = s1[i] + float64(s.ReadyPRs)
		s3[i] = s2[i] + float64(s.Completed)
		maxY = math.Max(maxY, s3[i])
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
		fmt.Fprintf(&doneLine, "%.1f,%.1f", x(i), y(s2[i]))
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
	svg.WriteString(polygon(s1, s2, stationColor(processflow.Ready)))
	svg.WriteString(polygon(s2, s3, "#16a34a"))
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

// lineDiagram renders the station boxes with arrows carrying the window's
// move counts. The draft/ready arrow shows both directions when PRs flipped
// back to draft.
func lineDiagram(line *processflow.LineReport) string {
	count := func(from, to processflow.Station) int {
		for _, flow := range line.Flows {
			if flow.From == from && flow.To == to {
				return flow.Count
			}
		}
		return 0
	}
	var html strings.Builder
	html.WriteString(`<div class="line-diagram">`)
	for i, station := range line.Stations {
		if i > 0 {
			prev := line.Stations[i-1]
			forward := count(prev.Station, station.Station)
			back := count(station.Station, prev.Station)
			html.WriteString(`<div class="arrow"><span class="head">&rarr;</span>`)
			if forward > 0 || back > 0 {
				fmt.Fprintf(&html, `<span>%d &rarr;`, forward)
				if back > 0 {
					fmt.Fprintf(&html, ` %d &larr;`, back)
				}
				html.WriteString(`</span>`)
			}
			html.WriteString(`</div>`)
		}
		color := stationColor(station.Station)
		ages := ""
		if isActiveStation(station.Station) && station.Count > 0 && station.OldestHours != nil {
			ages = fmt.Sprintf(`<div class="ages">oldest %s &middot; median %s</div>`,
				template.HTMLEscapeString(formatHours(station.OldestHours)),
				template.HTMLEscapeString(formatHours(station.MedianAgeHrs)))
		}
		fmt.Fprintf(&html,
			`<div class="station" style="--station-color:%s"><div class="name">%s</div><div class="count">%d</div>%s</div>`,
			color, template.HTMLEscapeString(station.Label), station.Count, ages)
	}
	html.WriteString(`</div>`)
	return html.String()
}

// isActiveStation reports whether a station holds unfinished work.
func isActiveStation(station processflow.Station) bool {
	return station == processflow.Open || station == processflow.Draft || station == processflow.Ready
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
