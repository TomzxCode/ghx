package processreport

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/processflow"
)

// wipListCap bounds the rendered aging list; the JSON report keeps every
// item, the HTML page shows the ten oldest and a count of the rest.
const wipListCap = 10

// buildView flattens the report into template-friendly rows and pre-rendered
// charts. Charts and the station diagrams are HTML/SVG strings produced
// server-side, so the template only places them.
func buildView(report *processflow.Report, meta Meta) *view {
	v := &view{
		Repos:       strings.Join(meta.Repos, ", "),
		RepoIDs:     meta.RepoIDs,
		Interactive: meta.Interactive,
		AllRepos:    meta.AllRepos,
		FromValue:   meta.FromValue,
		ToValue:     meta.ToValue,
		DaysValue:   meta.DaysValue,
		GeneratedAt: report.GeneratedAt,
		WindowDays:  report.WindowDays,
		CFD:         template.HTML(stackedArea(report.Samples)),
		WIP:         wipRows(report.WIP),
	}
	if meta.HasRange {
		v.Range = formatRange(report.WindowStart, report.WindowEnd)
	}
	if len(v.WIP) > wipListCap {
		v.WIPHidden = len(v.WIP) - wipListCap
		v.WIP = v.WIP[:wipListCap]
	}
	v.Issues = buildLineView(&report.Issues)
	v.PRs = buildLineView(&report.PRs)
	for _, line := range []*processflow.LineReport{&report.Issues, &report.PRs} {
		for _, pressure := range line.Queue {
			v.Queue = append(v.Queue, queueRow{
				Line:     line.Label,
				Label:    pressure.Label,
				Count:    pressure.Count,
				Arrived:  pressure.Arrived,
				Departed: pressure.Departed,
				Net:      pressure.Net,
				Trend:    pressure.Trend,
				Spark:    template.HTML(sparkline(pressure.WeeklyWIP, stationColor(pressure.Station))),
			})
		}
	}
	if len(report.Samples) > 0 {
		last := report.Samples[len(report.Samples)-1]
		v.TotalOpen = last.OpenIssues + last.DraftPRs + last.ReadyPRs
	}
	return v
}

func buildLineView(line *processflow.LineReport) lineView {
	view := lineView{
		Label:       line.Label,
		Diagram:     template.HTML(lineDiagram(line)),
		Arrivals:    line.Arrivals,
		Completions: line.Completions,
		Discards:    line.Discards,
		Cycle:       newCycleCards(line),
		Histogram:   template.HTML(histogramBars(line.Histogram, "#2563eb")),
		Weekly:      template.HTML(weeklyBars(line.Weekly, "Created", "Completed", "#64748b", "#16a34a")),
		ShareBar:    template.HTML(waitShareBar(line.WaitShares)),
		WaitShares:  line.WaitShares,
		Littles:     newLittlesCards(line.Littles),
		Forecast:    newForecastCards(line.Forecast),
		Outcomes:    outcomesCards{HasData: line.Outcomes.Created > 0, Created: line.Outcomes.Created, Done: line.Outcomes.Done, Discarded: line.Outcomes.Discarded, Active: line.Outcomes.Active},
	}
	return view
}

// formatRange renders the covered span for the meta line: "Jan 3 – Feb 9,
// 2006" within one year, with both years across a year boundary.
func formatRange(start, end time.Time) string {
	if start.Year() == end.Year() {
		return start.Format("Jan 2") + " – " + end.Format("Jan 2, 2006")
	}
	return start.Format("Jan 2, 2006") + " – " + end.Format("Jan 2, 2006")
}

func newCycleCards(line *processflow.LineReport) cycleCards {
	return cycleCards{
		HasData:     line.CycleTime.Count > 0,
		Median:      formatHours(line.CycleTime.MedianHours),
		P90:         formatHours(line.CycleTime.P90Hours),
		Mean:        formatHours(line.CycleTime.MeanHours),
		Completions: line.CycleTime.Count,
	}
}

func newLittlesCards(l processflow.LittlesLaw) littlesCards {
	return littlesCards{
		HasData:    l.AvgWIP != nil,
		AvgWIP:     formatFloat(l.AvgWIP, 2),
		Throughput: formatPerDay(l.ThroughputPerDay),
		Implied:    formatHours(l.ProjectedCycleHrs),
	}
}

func newForecastCards(f processflow.Forecast) forecastCards {
	if f.P50Weeks == nil {
		return forecastCards{HasData: false, Items: f.Items, Sims: f.Sims, P50: "–", P85: "–"}
	}
	return forecastCards{
		HasData: true,
		Items:   f.Items,
		Sims:    f.Sims,
		P50:     fmt.Sprintf("%s (~%s)", formatDate(*f.P50Date), formatWeeks(*f.P50Weeks)),
		P85:     fmt.Sprintf("%s (~%s)", formatDate(*f.P85Date), formatWeeks(*f.P85Weeks)),
	}
}

func formatWeeks(weeks float64) string {
	if weeks < 1 {
		return "<1w"
	}
	return fmt.Sprintf("%.0fw", weeks)
}

func formatFloat(value *float64, digits int) string {
	if value == nil {
		return "–"
	}
	return fmt.Sprintf("%.*f", digits, *value)
}

func formatPerDay(value *float64) string {
	if value == nil {
		return "–"
	}
	return fmt.Sprintf("%.2f/day", *value)
}

func wipRows(items []processflow.WipItem) []wipRow {
	rows := make([]wipRow, 0, len(items))
	for _, item := range items {
		age := item.AgeHours
		rows = append(rows, wipRow{
			ID:      item.ID,
			Kind:    string(item.Kind),
			Title:   item.Title,
			URL:     item.URL,
			Station: string(item.Station),
			Color:   stationColor(item.Station),
			Age:     formatHours(&age),
		})
	}
	return rows
}

// ---------------------------------------------------------------------------
// View structures
// ---------------------------------------------------------------------------

type view struct {
	Repos       string
	RepoIDs     []string
	Interactive bool
	AllRepos    bool
	Range       string
	FromValue   string
	ToValue     string
	DaysValue   string
	GeneratedAt time.Time
	WindowDays  int
	TotalOpen   int
	Issues      lineView
	PRs         lineView
	CFD         template.HTML
	Queue       []queueRow
	WIP         []wipRow
	WIPHidden   int
}

type cycleCards struct {
	HasData     bool
	Median      string
	P90         string
	Mean        string
	Completions int
}

type queueRow struct {
	Line     string
	Label    string
	Count    int
	Arrived  int
	Departed int
	Net      int
	Trend    string
	Spark    template.HTML
}

type littlesCards struct {
	HasData    bool
	AvgWIP     string
	Throughput string
	Implied    string
}

type forecastCards struct {
	HasData bool
	Items   int
	Sims    int
	P50     string
	P85     string
}

type outcomesCards struct {
	HasData   bool
	Created   int
	Done      int
	Discarded int
	Active    int
}

type lineView struct {
	Label       string
	Diagram     template.HTML
	Arrivals    int
	Completions int
	Discards    int
	Cycle       cycleCards
	Histogram   template.HTML
	Weekly      template.HTML
	ShareBar    template.HTML
	WaitShares  []processflow.WaitShare
	Littles     littlesCards
	Forecast    forecastCards
	Outcomes    outcomesCards
}

type wipRow struct {
	ID      string
	Kind    string
	Title   string
	URL     string
	Station string
	Color   string
	Age     string
}
