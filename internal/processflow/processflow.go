// Package processflow reads cached issues and pull requests as a production
// line: stations hold work, items move between them, and the report answers
// how much work sits where, how long it takes to flow through, and whether
// the queues are growing.
//
// The model is the production-line view: the backlog is the raw-material
// queue, unfinished stations are work in process, and completed stations are
// finished goods. GitHub records only part of each item's history, so every
// item carries the moves that are known and the timeline walk corrects
// missing history: an item has sat in its current station since its last
// known move.
//
// Everything here is pure: it takes items (already mapped onto stations) plus
// the reporting window, and returns the report. The GitHub-specific mapping
// lives in github.go; the CLI command and the web UI only convert, build,
// and render.
package processflow

import (
	"math"
	"math/rand/v2"
	"sort"
	"time"
)

// Kind distinguishes the two production lines.
type Kind string

const (
	KindIssue Kind = "issue"
	KindPR    Kind = "pr"
)

// Station identifies where an item currently sits.
type Station string

const (
	// Active stations: unfinished work, the queues the flow metrics are about.
	Open Station = "open" // issue: open
	// The PR review pipeline: a PR is drafted, waits for review, then waits
	// alternately on the author (changes requested) and on merge (approved)
	// until it ships.
	Draft          Station = "draft"           // PR: open, still a draft
	AwaitingReview Station = "awaiting_review" // PR: open, waiting for a reviewer
	AwaitingAuthor Station = "awaiting_author" // PR: changes requested, waiting on the author
	AwaitingMerge  Station = "awaiting_merge"  // PR: approved, waiting for the merge

	// Completed stations: finished goods.
	Closed Station = "closed" // issue: closed
	Merged Station = "merged" // PR: merged

	// Discarded: left the line without shipping (a closed, unmerged PR).
	Discarded Station = "discarded"
)

// Line is one production line: the stations an item of a kind moves through.
type Line struct {
	Kind     Kind
	Label    string
	Stations []Station
	Labels   map[Station]string
}

// LineFor returns the production line an item of a kind belongs to.
func LineFor(kind Kind) Line {
	if kind == KindIssue {
		return IssuesLine
	}
	return PRsLine
}

// IssuesLine is the issue line: open work, closed work.
var IssuesLine = Line{
	Kind:     KindIssue,
	Label:    "Issues",
	Stations: []Station{Open, Closed},
	Labels:   map[Station]string{Open: "Open", Closed: "Closed"},
}

// PRsLine is the pull-request line: drafts, the review-wait stations,
// merged PRs, and discarded PRs.
var PRsLine = Line{
	Kind:     KindPR,
	Label:    "Pull requests",
	Stations: []Station{Draft, AwaitingReview, AwaitingAuthor, AwaitingMerge, Merged, Discarded},
	Labels: map[Station]string{
		Draft:          "Draft",
		AwaitingReview: "Waiting on reviewer",
		AwaitingAuthor: "Waiting on author",
		AwaitingMerge:  "Waiting on merge",
		Merged:         "Merged",
		Discarded:      "Discarded",
	},
}

// ActiveStations are the unfinished-work stations of a line, in flow order.
func (l Line) ActiveStations() []Station {
	var active []Station
	for _, station := range l.Stations {
		if station == Closed || station == Merged || station == Discarded {
			continue
		}
		active = append(active, station)
	}
	return active
}

// IsReviewWaitStation reports whether a station is one of the PR review-wait
// stages (as opposed to the draft stage).
func isReviewStation(station Station) bool {
	return station == AwaitingReview || station == AwaitingAuthor || station == AwaitingMerge
}

// DoneStation is the station that counts as a completion for a line.
func (l Line) DoneStation() Station {
	if l.Kind == KindIssue {
		return Closed
	}
	return Merged
}

// LabelOf names a station for display.
func (l Line) LabelOf(station Station) string {
	if label, ok := l.Labels[station]; ok {
		return label
	}
	return string(station)
}

// DefaultWindowDays is the reporting window when none is given.
const DefaultWindowDays = 90

// Forecast bounds: five hundred simulations, each at most 520 weeks.
const (
	ForecastSims     = 500
	ForecastWeeksCap = 520
)

// HistogramBucket is one cycle-time bucket: [FromHours, ToHours), open-ended
// when ToHours is nil.
type HistogramBucket struct {
	Label     string   `json:"label"`
	FromHours float64  `json:"fromHours"`
	ToHours   *float64 `json:"toHours,omitempty"`
	Count     int      `json:"count"`
}

// histogramBounds are the fixed cycle-time bucket edges, in hours.
var histogramBounds = []struct {
	label string
	low   float64
	high  float64 // math.Inf(1) for the open-ended top bucket
}{
	{"<1d", 0, 24},
	{"1-2d", 24, 48},
	{"2-4d", 48, 96},
	{"4-7d", 96, 168},
	{"1-2w", 168, 336},
	{"2-4w", 336, 720},
	{"1-2mo", 720, 1440},
	{">2mo", 1440, math.Inf(1)},
}

// histogramBuckets copies the empty bucket set, so counts never leak between
// reports.
func histogramBuckets() []HistogramBucket {
	buckets := make([]HistogramBucket, 0, len(histogramBounds))
	for _, b := range histogramBounds {
		bucket := HistogramBucket{Label: b.label, FromHours: b.low}
		if !math.IsInf(b.high, 1) {
			high := b.high
			bucket.ToHours = &high
		}
		buckets = append(buckets, bucket)
	}
	return buckets
}

// Move is one observed station change: PR draft/ready flips from the cached
// timeline, and review-driven turns (approved, changes requested) derived
// from the cached reviews.
type Move struct {
	At   time.Time `json:"at"`
	From Station   `json:"from"`
	To   Station   `json:"to"`
}

// Item is one work item reduced to what the flow computation needs.
type Item struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Title     string    `json:"title"`
	URL       string    `json:"url,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Station   Station   `json:"station"`
	// ExitedAt is when the item reached its final station (closed, merged, or
	// discarded). It is nil while the item is active, and nil for a finished
	// item whose finish time GitHub did not record.
	ExitedAt *time.Time `json:"exitedAt,omitempty"`
	// Moves are the observed intermediate moves, oldest first.
	Moves []Move `json:"moves,omitempty"`
}

// LineOf returns the production line the item belongs to.
func (it Item) LineOf() Line { return LineFor(it.Kind) }

// IsActive reports whether the item is unfinished work.
func (it Item) IsActive() bool {
	return it.Station == Open || it.Station == Draft || isReviewStation(it.Station)
}

// IsDone reports whether the item reached its line's completion station.
func (it Item) IsDone() bool {
	return it.Station == Closed || it.Station == Merged
}

// TimelineEntry is one (entered at, station) pair of an item's history.
type TimelineEntry struct {
	At      time.Time
	Station Station
}

// Timeline walks the item's history from creation to now, oldest first.
//
// Every item starts in its line's first station at creation (issues in Open,
// PRs in Draft unless they were opened directly as ready for review). A move
// applies only when it actually changes the station the walk is in and
// happens after the previous entry, so duplicate or stale moves cannot
// corrupt the walk. A recorded finish (exit time) is appended as the final
// move, so completed items' histories show the real merge or close moment.
// When the walk still ends somewhere other than the item's current station
// (history is missing), the last entry is corrected to the current station:
// the item has been there since its last known move.
func (it Item) Timeline() []TimelineEntry {
	first := Open
	if it.Kind == KindPR && it.startsAsDraft() {
		first = Draft
	} else if it.Kind == KindPR {
		first = AwaitingReview
	}
	timeline := []TimelineEntry{{At: it.CreatedAt, Station: first}}
	for _, move := range it.Moves {
		last := timeline[len(timeline)-1]
		if move.At.After(last.At) && move.To != last.Station {
			timeline = append(timeline, TimelineEntry{At: move.At, Station: move.To})
		}
	}
	if it.ExitedAt != nil {
		last := timeline[len(timeline)-1]
		if it.ExitedAt.After(last.At) && it.Station != last.Station {
			timeline = append(timeline, TimelineEntry{At: *it.ExitedAt, Station: it.Station})
		}
	}
	last := timeline[len(timeline)-1]
	if last.Station != it.Station {
		timeline[len(timeline)-1] = TimelineEntry{At: last.At, Station: it.Station}
	}
	return timeline
}

// startsAsDraft reports whether a PR's history says it went through the draft
// stage: either it is a draft now, or its moves record draft/ready flips. A
// PR with no flip events was opened directly as ready for review, so it has
// never been in the draft station.
func (it Item) startsAsDraft() bool {
	if it.Kind != KindPR {
		return false
	}
	if it.Station == Draft {
		return true
	}
	for _, move := range it.Moves {
		if move.From == Draft || move.To == Draft {
			return true
		}
	}
	return false
}

// EnteredCurrentAt returns when the item entered the station it sits in now.
func (it Item) EnteredCurrentAt() time.Time {
	timeline := it.Timeline()
	return timeline[len(timeline)-1].At
}

// ---------------------------------------------------------------------------
// Report structures
// ---------------------------------------------------------------------------

// Distribution summarizes a sample of durations, in hours.
type Distribution struct {
	Count       int      `json:"count"`
	MeanHours   *float64 `json:"meanHours,omitempty"`
	MedianHours *float64 `json:"medianHours,omitempty"`
	P10Hours    *float64 `json:"p10Hours,omitempty"`
	P90Hours    *float64 `json:"p90Hours,omitempty"`
	Fastest     *float64 `json:"fastestHours,omitempty"`
	Slowest     *float64 `json:"slowestHours,omitempty"`
}

// StageStat is one station's inventory: how many items, and how long the
// items currently there have been waiting.
type StageStat struct {
	Station      Station  `json:"station"`
	Label        string   `json:"label"`
	Count        int      `json:"count"`
	OldestHours  *float64 `json:"oldestHours,omitempty"`
	MedianAgeHrs *float64 `json:"medianAgeHours,omitempty"`
	NewestHours  *float64 `json:"newestHours,omitempty"`
}

// StationFlow counts the items that moved from one station to another in the
// window.
type StationFlow struct {
	From  Station `json:"from"`
	To    Station `json:"to"`
	Count int     `json:"count"`
}

// StageDwell says where completed items' time went, for one station. Count is
// how many completed items actually waited there.
type StageDwell struct {
	Station   Station  `json:"station"`
	Label     string   `json:"label"`
	Count     int      `json:"count"`
	MeanHours *float64 `json:"meanHours,omitempty"`
	MedianHrs *float64 `json:"medianHours,omitempty"`
}

// WaitShare is one station's slice of the completed items' total cycle time.
// This is the bottleneck ranking: the station holding the largest slice is
// where the line loses the most time.
type WaitShare struct {
	Station   Station  `json:"station"`
	Label     string   `json:"label"`
	Count     int      `json:"count"`
	MeanHours *float64 `json:"meanHours,omitempty"`
	TotalHrs  float64  `json:"totalHours"`
	Share     float64  `json:"share"`
}

// Trend verdicts for a station's net flow.
const (
	TrendFilling  = "filling"
	TrendDraining = "draining"
	TrendSteady   = "steady"
)

// QueuePressure says whether an active station's queue is growing, shrinking,
// or holding steady. WeeklyWIP samples the inventory at each week's end,
// oldest first, ending at the current count.
type QueuePressure struct {
	Station   Station `json:"station"`
	Label     string  `json:"label"`
	Count     int     `json:"count"`
	Arrived   int     `json:"arrived"`
	Departed  int     `json:"departed"`
	Net       int     `json:"net"`
	Trend     string  `json:"trend"`
	WeeklyWIP []int   `json:"weeklyWip"`
}

// FlowWeek is one week of the window: arrivals and completions.
type FlowWeek struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Created   int       `json:"created"`
	Completed int       `json:"completed"`
}

// LittlesLaw holds the average work in process, the throughput, and the cycle
// time the queue depth implies, in hours.
type LittlesLaw struct {
	AvgWIP            *float64 `json:"avgWip,omitempty"`
	ThroughputPerDay  *float64 `json:"throughputPerDay,omitempty"`
	ProjectedCycleHrs *float64 `json:"projectedCycleHours,omitempty"`
}

// Forecast is a Monte Carlo projection for clearing the items currently on
// the line: each simulation draws one week's completions from the observed
// weekly history until the active count is exhausted.
type Forecast struct {
	Items         int        `json:"items"`
	Sims          int        `json:"sims"`
	WeeksObserved int        `json:"weeksObserved"`
	P50Weeks      *float64   `json:"p50Weeks,omitempty"`
	P85Weeks      *float64   `json:"p85Weeks,omitempty"`
	P50Date       *time.Time `json:"p50Date,omitempty"`
	P85Date       *time.Time `json:"p85Date,omitempty"`
}

// Outcomes says where the window's arrivals stand now.
type Outcomes struct {
	Created   int `json:"created"`
	Done      int `json:"done"`
	Discarded int `json:"discarded"`
	Active    int `json:"active"`
}

// FlowSample is one day's snapshot of the whole shop for the cumulative flow
// diagram: the three active inventories plus cumulative completions, which
// only ever grow.
type FlowSample struct {
	At         time.Time `json:"at"`
	OpenIssues int       `json:"openIssues"`
	DraftPRs   int       `json:"draftPrs"`
	ReviewPRs  int       `json:"reviewPrs"`
	AuthorPRs  int       `json:"authorPrs"`
	MergePRs   int       `json:"mergePrs"`
	Completed  int       `json:"completed"`
}

// WipItem is one item currently on the line, for the aging list.
type WipItem struct {
	ID       string    `json:"id"`
	Kind     Kind      `json:"kind"`
	Title    string    `json:"title"`
	URL      string    `json:"url,omitempty"`
	Station  Station   `json:"station"`
	Entered  time.Time `json:"enteredAt"`
	AgeHours float64   `json:"ageHours"`
}

// LineReport is the whole flow report for one production line.
type LineReport struct {
	Kind        Kind              `json:"kind"`
	Label       string            `json:"label"`
	Stations    []StageStat       `json:"stations"`
	Flows       []StationFlow     `json:"flows"`
	Arrivals    int               `json:"arrivals"`
	Completions int               `json:"completions"`
	Discards    int               `json:"discards"`
	CycleTime   Distribution      `json:"cycleTime"`
	Histogram   []HistogramBucket `json:"histogram"`
	Dwell       []StageDwell      `json:"dwell"`
	WaitShares  []WaitShare       `json:"waitShares"`
	Weekly      []FlowWeek        `json:"weekly"`
	Littles     LittlesLaw        `json:"littles"`
	Queue       []QueuePressure   `json:"queuePressure"`
	Forecast    Forecast          `json:"forecast"`
	Outcomes    Outcomes          `json:"outcomes"`
}

// Report is the whole production-line report for one window.
type Report struct {
	GeneratedAt time.Time    `json:"generatedAt"`
	WindowStart time.Time    `json:"windowStart"`
	WindowEnd   time.Time    `json:"windowEnd"`
	WindowDays  int          `json:"windowDays"`
	Issues      LineReport   `json:"issues"`
	PRs         LineReport   `json:"prs"`
	Samples     []FlowSample `json:"samples"`
	WIP         []WipItem    `json:"wip"`
}

// ---------------------------------------------------------------------------
// Primitives
// ---------------------------------------------------------------------------

// HoursBetween returns the elapsed hours between two instants, never negative.
func HoursBetween(start, end time.Time) float64 {
	if end.Before(start) {
		return 0
	}
	return end.Sub(start).Hours()
}

// Quantile returns the linear-interpolated quantile of a sample, or nil when
// it is empty.
func Quantile(values []float64, q float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	if len(ordered) == 1 {
		v := ordered[0]
		return &v
	}
	position := float64(len(ordered)-1) * q
	lower := math.Floor(position)
	upper := math.Ceil(position)
	if lower == upper {
		v := ordered[int(lower)]
		return &v
	}
	v := ordered[int(lower)] + (ordered[int(upper)]-ordered[int(lower)])*(position-lower)
	return &v
}

// fmean returns the arithmetic mean, or nil for an empty sample.
func fmean(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	total := 0.0
	for _, v := range values {
		total += v
	}
	mean := total / float64(len(values))
	return &mean
}

// median returns the 0.5 quantile, or nil for an empty sample.
func median(values []float64) *float64 {
	return Quantile(values, 0.5)
}

func stationAt(timeline []TimelineEntry, at time.Time) (Station, bool) {
	var current Station
	found := false
	for _, entry := range timeline {
		if entry.At.After(at) {
			break
		}
		current = entry.Station
		found = true
	}
	return current, found
}

// ---------------------------------------------------------------------------
// Per-line computations
// ---------------------------------------------------------------------------

// stageInventory counts the items per station and how long the current
// members have been waiting.
func stageInventory(items []Item, line Line, now time.Time) []StageStat {
	stats := make([]StageStat, 0, len(line.Stations))
	for _, station := range line.Stations {
		var ages []float64
		count := 0
		for _, it := range items {
			if it.Kind != line.Kind || it.Station != station {
				continue
			}
			count++
			ages = append(ages, HoursBetween(it.EnteredCurrentAt(), now))
		}
		stats = append(stats, StageStat{
			Station:      station,
			Label:        line.LabelOf(station),
			Count:        count,
			OldestHours:  maxFloat(ages),
			MedianAgeHrs: median(ages),
			NewestHours:  minFloat(ages),
		})
	}
	return stats
}

func maxFloat(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	best := values[0]
	for _, v := range values[1:] {
		if v > best {
			best = v
		}
	}
	return &best
}

func minFloat(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	best := values[0]
	for _, v := range values[1:] {
		if v < best {
			best = v
		}
	}
	return &best
}

// completion pairs a finished item with the moment it finished.
type completion struct {
	item Item
	at   time.Time
}

// completions returns the line's items that finished inside the window, with
// the moment each finished, oldest first. A finished item whose finish time
// GitHub did not record has no completion moment and is skipped.
func completions(items []Item, line Line, start, end time.Time) []completion {
	done := line.DoneStation()
	var out []completion
	for _, it := range items {
		if it.Kind != line.Kind || it.Station != done || it.ExitedAt == nil {
			continue
		}
		at := *it.ExitedAt
		if at.Before(start) || at.After(end) {
			continue
		}
		out = append(out, completion{it, at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// discards counts the line's items that left through the discard station
// inside the window.
func discards(items []Item, line Line, start, end time.Time) int {
	if line.Kind != KindPR {
		return 0
	}
	count := 0
	for _, it := range items {
		if it.Kind != KindPR || it.Station != Discarded || it.ExitedAt == nil {
			continue
		}
		if !it.ExitedAt.Before(start) && !it.ExitedAt.After(end) {
			count++
		}
	}
	return count
}

// summarizeDurations reduces a duration sample to its distribution.
func summarizeDurations(durations []float64) Distribution {
	if len(durations) == 0 {
		return Distribution{}
	}
	return Distribution{
		Count:       len(durations),
		MeanHours:   fmean(durations),
		MedianHours: median(durations),
		P10Hours:    Quantile(durations, 0.10),
		P90Hours:    Quantile(durations, 0.90),
		Fastest:     minFloat(durations),
		Slowest:     maxFloat(durations),
	}
}

// histogramFor counts a duration sample into the fixed buckets.
func histogramFor(durations []float64) []HistogramBucket {
	buckets := histogramBuckets()
	for _, value := range durations {
		for i := range buckets {
			bucket := &buckets[i]
			upper := math.Inf(1)
			if bucket.ToHours != nil {
				upper = *bucket.ToHours
			}
			if value >= bucket.FromHours && value < upper {
				bucket.Count++
				break
			}
		}
	}
	return buckets
}

// Unsplit is not a station: it is the wait-share bucket for completed PRs
// whose draft/ready split is unknown (no recorded flip events), so their
// whole trip is reported as one unsplit duration instead of being attributed
// to a station by assumption.
const Unsplit Station = "unsplit"

// hasMoveHistory reports whether a completed PR's intermediate stations were
// recorded (draft/ready flips or review turns). A merged PR with no moves at
// all (automation merges into a cache without timeline and review data) has
// none, so its trip is reported as one unsplit duration instead of being
// attributed to a station by assumption.
func hasMoveHistory(it Item) bool {
	return it.Kind != KindPR || len(it.Moves) > 0
}

// stageWaits returns, per station, how long each completed item waited there,
// clamped at its completion. The segments of a completed item's timeline
// cover its whole trip, so the sums over stations add up to its cycle time.
// Completed items without flip history contribute nothing here; they are
// reported in the unsplit bucket instead.
func stageWaits(done []completion) map[Station][]float64 {
	waits := map[Station][]float64{}
	for _, entry := range done {
		if !hasMoveHistory(entry.item) {
			continue
		}
		timeline := entry.item.Timeline()
		for i, segment := range timeline {
			leftAt := entry.at
			if i+1 < len(timeline) && timeline[i+1].At.Before(leftAt) {
				leftAt = timeline[i+1].At
			}
			if !leftAt.After(segment.At) {
				continue
			}
			waits[segment.Station] = append(waits[segment.Station], HoursBetween(segment.At, leftAt))
		}
	}
	return waits
}

// stageDwell describes where the completed items' cycle time went, per
// station. A station's numbers cover only the items that actually waited
// there, so a median answers "when an item waited here, how long was it".
func stageDwell(line Line, waits map[Station][]float64) []StageDwell {
	dwell := make([]StageDwell, 0, len(line.Stations))
	for _, station := range line.Stations {
		samples := waits[station]
		dwell = append(dwell, StageDwell{
			Station:   station,
			Label:     line.LabelOf(station),
			Count:     len(samples),
			MeanHours: fmean(samples),
			MedianHrs: median(samples),
		})
	}
	return dwell
}

// stageWaitShares returns each station's slice of the completed items' total
// cycle time, plus, for the PR line, one unsplit bucket holding the
// completed PRs whose draft/ready split is not recorded. All shares,
// including the unsplit one, sum to one.
func stageWaitShares(line Line, waits map[Station][]float64, done []completion) []WaitShare {
	grandTotal := 0.0
	for _, samples := range waits {
		for _, v := range samples {
			grandTotal += v
		}
	}
	unsplitCount := 0
	unsplitTotal := 0.0
	if line.Kind == KindPR {
		for _, entry := range done {
			if !hasMoveHistory(entry.item) {
				unsplitCount++
				unsplitTotal += HoursBetween(entry.item.CreatedAt, entry.at)
			}
		}
		grandTotal += unsplitTotal
	}
	shares := make([]WaitShare, 0, len(line.Stations)+1)
	for _, station := range line.Stations {
		samples := waits[station]
		total := 0.0
		for _, v := range samples {
			total += v
		}
		share := 0.0
		if grandTotal > 0 {
			share = total / grandTotal
		}
		shares = append(shares, WaitShare{
			Station:   station,
			Label:     line.LabelOf(station),
			Count:     len(samples),
			MeanHours: fmean(samples),
			TotalHrs:  total,
			Share:     share,
		})
	}
	if line.Kind == KindPR && unsplitCount > 0 {
		mean := unsplitTotal / float64(unsplitCount)
		share := 0.0
		if grandTotal > 0 {
			share = unsplitTotal / grandTotal
		}
		shares = append(shares, WaitShare{
			Station:   Unsplit,
			Label:     "No draft/ready history",
			Count:     unsplitCount,
			MeanHours: &mean,
			TotalHrs:  unsplitTotal,
			Share:     share,
		})
	}
	return shares
}

// weekBoundaries returns consecutive seven-day spans (the last one possibly
// shorter) covering the window.
func weekBoundaries(start, end time.Time) [][2]time.Time {
	var boundaries [][2]time.Time
	weekStart := start
	for weekStart.Before(end) {
		weekEnd := weekStart.Add(7 * 24 * time.Hour)
		if weekEnd.After(end) {
			weekEnd = end
		}
		boundaries = append(boundaries, [2]time.Time{weekStart, weekEnd})
		weekStart = weekEnd
	}
	return boundaries
}

// weeklyFlow splits arrivals and completions into weeks anchored at the
// window start.
func weeklyFlow(items []Item, line Line, done []completion, start, end time.Time) []FlowWeek {
	weeks := make([]FlowWeek, 0, 16)
	for _, bounds := range weekBoundaries(start, end) {
		week := FlowWeek{Start: bounds[0], End: bounds[1]}
		for _, it := range items {
			if it.Kind != line.Kind {
				continue
			}
			if !it.CreatedAt.Before(week.Start) && it.CreatedAt.Before(week.End) {
				week.Created++
			}
		}
		for _, entry := range done {
			if !entry.at.Before(week.Start) && entry.at.Before(week.End) {
				week.Completed++
			}
		}
		weeks = append(weeks, week)
	}
	return weeks
}

// littlesLaw integrates the line's work in process over the window (the exact
// time active items spent in active stations, divided by the window length),
// divides completions by days for throughput, and reports the cycle time the
// queue depth implies.
func littlesLaw(items []Item, line Line, done []completion, start, end time.Time) LittlesLaw {
	windowHours := HoursBetween(start, end)
	if windowHours <= 0 {
		return LittlesLaw{}
	}
	active := map[Station]bool{}
	for _, station := range line.ActiveStations() {
		active[station] = true
	}
	totalActiveHours := 0.0
	for _, it := range items {
		if it.Kind != line.Kind {
			continue
		}
		timeline := it.Timeline()
		for i, segment := range timeline {
			leftAt := end
			if i+1 < len(timeline) {
				leftAt = timeline[i+1].At
			}
			overlapStart := maxTime(segment.At, start)
			overlapEnd := minTime(leftAt, end)
			if active[segment.Station] && overlapEnd.After(overlapStart) {
				totalActiveHours += HoursBetween(overlapStart, overlapEnd)
			}
		}
	}
	avgWIP := totalActiveHours / windowHours
	windowDays := windowHours / 24
	throughput := float64(len(done)) / windowDays
	law := LittlesLaw{AvgWIP: &avgWIP, ThroughputPerDay: &throughput}
	if throughput > 0 {
		projected := (avgWIP / throughput) * 24
		law.ProjectedCycleHrs = &projected
	}
	return law
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// lineFlows counts the moves between stations observed in the window: PR
// draft/ready flips, issue closures, PR merges, and PR discards. Creations
// are arrivals, not moves, and are counted separately.
func lineFlows(items []Item, line Line, start, end time.Time) []StationFlow {
	counts := map[[2]Station]int{}
	bump := func(at time.Time, from, to Station) {
		if at.Before(start) || at.After(end) || from == to {
			return
		}
		counts[[2]Station{from, to}]++
	}
	for _, it := range items {
		if it.Kind != line.Kind {
			continue
		}
		for _, move := range it.Moves {
			stillActive := it.ExitedAt == nil || move.At.Before(*it.ExitedAt)
			if stillActive {
				bump(move.At, move.From, move.To)
			}
		}
		if it.ExitedAt != nil {
			bump(*it.ExitedAt, activeStationAtExit(it), it.Station)
		}
	}
	flows := make([]StationFlow, 0, len(counts))
	for pair, count := range counts {
		if count <= 0 {
			continue
		}
		flows = append(flows, StationFlow{From: pair[0], To: pair[1], Count: count})
	}
	sort.Slice(flows, func(i, j int) bool {
		if flows[i].Count != flows[j].Count {
			return flows[i].Count > flows[j].Count
		}
		if flows[i].From != flows[j].From {
			return flows[i].From < flows[j].From
		}
		return flows[i].To < flows[j].To
	})
	return flows
}

func firstStation(kind Kind) Station {
	if kind == KindIssue {
		return Open
	}
	return Draft
}

// activeStationAtExit names the active station an item sat in immediately
// before it exited, from its timeline: issues leave Open, PRs leave the
// review-wait station they had reached.
func activeStationAtExit(it Item) Station {
	exit := firstStation(it.Kind)
	if it.ExitedAt == nil {
		return exit
	}
	station := exit
	for _, entry := range it.Timeline() {
		if !entry.At.Before(*it.ExitedAt) {
			break
		}
		if entry.Station == Open || entry.Station == Draft || isReviewStation(entry.Station) {
			station = entry.Station
		}
	}
	return station
}

// queuePressure reads, for each active station, whether the queue grew over
// the window: arrivals (creations into the first station, moves into the
// station) against departures (moves out), the trend that net implies, and a
// weekly inventory sparkline sampled at each week's end.
func queuePressure(items []Item, line Line, start, end time.Time) []QueuePressure {
	active := line.ActiveStations()
	activeSet := map[Station]bool{}
	for _, station := range active {
		activeSet[station] = true
	}
	weeks := weekBoundaries(start, end)
	weekly := map[Station][]int{}
	for _, bounds := range weeks {
		counts := map[Station]int{}
		for _, it := range items {
			if it.Kind != line.Kind {
				continue
			}
			if station, ok := stationAt(it.Timeline(), bounds[1]); ok && activeSet[station] {
				counts[station]++
			}
		}
		for _, station := range active {
			weekly[station] = append(weekly[station], counts[station])
		}
	}
	arrived := map[Station]int{}
	departed := map[Station]int{}
	for _, it := range items {
		if it.Kind != line.Kind {
			continue
		}
		if !it.CreatedAt.Before(start) && !it.CreatedAt.After(end) {
			arrived[firstStation(line.Kind)]++
		}
		consider := func(at time.Time, from, to Station) {
			if at.Before(start) || at.After(end) {
				return
			}
			if from != to {
				if activeSet[from] {
					departed[from]++
				}
				if activeSet[to] {
					arrived[to]++
				}
			}
		}
		for _, move := range it.Moves {
			consider(move.At, move.From, move.To)
		}
		if it.ExitedAt != nil {
			consider(*it.ExitedAt, activeStationAtExit(it), it.Station)
		}
	}
	pressure := make([]QueuePressure, 0, len(active))
	for _, station := range active {
		samples := weekly[station]
		count := 0
		if len(samples) > 0 {
			count = samples[len(samples)-1]
		}
		net := arrived[station] - departed[station]
		trend := TrendSteady
		switch {
		case net > 0:
			trend = TrendFilling
		case net < 0:
			trend = TrendDraining
		}
		pressure = append(pressure, QueuePressure{
			Station:   station,
			Label:     line.LabelOf(station),
			Count:     count,
			Arrived:   arrived[station],
			Departed:  departed[station],
			Net:       net,
			Trend:     trend,
			WeeklyWIP: samples,
		})
	}
	return pressure
}

// lineOutcomes splits the window's arrivals by where they stand now.
func lineOutcomes(items []Item, line Line, start, end time.Time) Outcomes {
	out := Outcomes{}
	for _, it := range items {
		if it.Kind != line.Kind {
			continue
		}
		if it.CreatedAt.Before(start) || it.CreatedAt.After(end) {
			continue
		}
		out.Created++
		switch {
		case it.IsDone():
			out.Done++
		case it.Station == Discarded:
			out.Discarded++
		default:
			out.Active++
		}
	}
	return out
}

// lineReport computes one line's whole report.
func lineReport(items []Item, line Line, start, end time.Time) LineReport {
	done := completions(items, line, start, end)
	durations := make([]float64, 0, len(done))
	for _, entry := range done {
		durations = append(durations, HoursBetween(entry.item.CreatedAt, entry.at))
	}
	waits := stageWaits(done)
	return LineReport{
		Kind:        line.Kind,
		Label:       line.Label,
		Stations:    stageInventory(items, line, end),
		Flows:       lineFlows(items, line, start, end),
		Arrivals:    countArrivals(items, line, start, end),
		Completions: len(done),
		Discards:    discards(items, line, start, end),
		CycleTime:   summarizeDurations(durations),
		Histogram:   histogramFor(durations),
		Dwell:       stageDwell(line, waits),
		WaitShares:  stageWaitShares(line, waits, done),
		Weekly:      weeklyFlow(items, line, done, start, end),
		Littles:     littlesLaw(items, line, done, start, end),
		Queue:       queuePressure(items, line, start, end),
		Forecast:    forecastBacklog(activeCount(items, line), weeklyCompletions(items, line, done, start, end), end),
		Outcomes:    lineOutcomes(items, line, start, end),
	}
}

func countArrivals(items []Item, line Line, start, end time.Time) int {
	count := 0
	for _, it := range items {
		if it.Kind == line.Kind && !it.CreatedAt.Before(start) && !it.CreatedAt.After(end) {
			count++
		}
	}
	return count
}

func activeCount(items []Item, line Line) int {
	count := 0
	for _, it := range items {
		if it.Kind == line.Kind && it.IsActive() {
			count++
		}
	}
	return count
}

func weeklyCompletions(items []Item, line Line, done []completion, start, end time.Time) []int {
	weeks := weeklyFlow(items, line, done, start, end)
	counts := make([]int, 0, len(weeks))
	for _, week := range weeks {
		counts = append(counts, week.Completed)
	}
	return counts
}

// ---------------------------------------------------------------------------
// Whole-shop computations
// ---------------------------------------------------------------------------

// flowSamples takes one snapshot per day of every active inventory plus
// cumulative completions, the raw material of the cumulative flow diagram:
// plotted as stacked bands, a widening band is a queue forming, and the top
// edge is the shop's total output to date.
func flowSamples(items []Item, start, end time.Time) []FlowSample {
	var samples []FlowSample
	for day := start; !day.After(end); day = day.Add(24 * time.Hour) {
		sample := FlowSample{At: day}
		for _, it := range items {
			station, ok := stationAt(it.Timeline(), day)
			if !ok {
				continue
			}
			switch station {
			case Open:
				sample.OpenIssues++
			case Draft:
				sample.DraftPRs++
			case AwaitingReview:
				sample.ReviewPRs++
			case AwaitingAuthor:
				sample.AuthorPRs++
			case AwaitingMerge:
				sample.MergePRs++
			}
			if (it.IsDone()) && it.ExitedAt != nil && !it.ExitedAt.After(day) {
				sample.Completed++
			}
		}
		samples = append(samples, sample)
	}
	return samples
}

// wipItems lists everything currently on the line, longest wait first: the
// aging report says which items wait, since when, and in which station.
func wipItems(items []Item, now time.Time) []WipItem {
	var out []WipItem
	for _, it := range items {
		if !it.IsActive() {
			continue
		}
		entered := it.EnteredCurrentAt()
		out = append(out, WipItem{
			ID:       it.ID,
			Kind:     it.Kind,
			Title:    it.Title,
			URL:      it.URL,
			Station:  it.Station,
			Entered:  entered,
			AgeHours: HoursBetween(entered, now),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].AgeHours > out[j].AgeHours })
	return out
}

// ForecastBacklog projects when the items currently on a line will be
// finished. Each simulation resamples the window's weekly completion counts
// (with replacement) until it has covered activeCount items, then reports the
// P50 and P85 of the simulated durations. A window with no completions at all
// supports no forecast, so the quantiles stay nil.
func ForecastBacklog(activeCount int, weeklyCompletions []int, now time.Time) Forecast {
	return forecastBacklog(activeCount, weeklyCompletions, now)
}

func forecastBacklog(activeCount int, weeklyCompletions []int, now time.Time) Forecast {
	history := append([]int(nil), weeklyCompletions...)
	if activeCount <= 0 {
		p50Date := now
		return Forecast{
			Items:         0,
			Sims:          ForecastSims,
			WeeksObserved: len(history),
			P50Weeks:      floatPtr(0),
			P85Weeks:      floatPtr(0),
			P50Date:       &p50Date,
			P85Date:       &p50Date,
		}
	}
	forecast := Forecast{Items: activeCount, Sims: ForecastSims, WeeksObserved: len(history)}
	total := 0
	for _, c := range history {
		total += c
	}
	if len(history) == 0 || total == 0 {
		return forecast
	}
	rng := rand.New(rand.NewPCG(0, 0))
	durations := make([]float64, 0, ForecastSims)
	for range ForecastSims {
		remaining := activeCount
		weeks := 0
		for remaining > 0 && weeks < ForecastWeeksCap {
			weeks++
			remaining -= history[rng.IntN(len(history))]
		}
		durations = append(durations, float64(weeks))
	}
	p50 := *Quantile(durations, 0.5)
	p85 := *Quantile(durations, 0.85)
	p50Date := now.Add(time.Duration(p50*7*24) * time.Hour)
	p85Date := now.Add(time.Duration(p85*7*24) * time.Hour)
	return Forecast{
		Items:         activeCount,
		Sims:          ForecastSims,
		WeeksObserved: len(history),
		P50Weeks:      &p50,
		P85Weeks:      &p85,
		P50Date:       &p50Date,
		P85Date:       &p85Date,
	}
}

func floatPtr(v float64) *float64 { return &v }

// ---------------------------------------------------------------------------
// Report
// ---------------------------------------------------------------------------

// Build computes the whole report for the window of windowDays ending at now.
// The window end doubles as "current time": station ages and forecasts are
// measured against it, so a report for a past window stays reproducible.
func Build(items []Item, now time.Time, windowDays int) Report {
	if windowDays <= 0 {
		windowDays = DefaultWindowDays
	}
	return BuildRange(items, now.Add(-time.Duration(windowDays)*24*time.Hour), now)
}

// itemAsOf projects an item to its state at instant end: the station it sat
// in then, the moves recorded up to then, and its exit only if it finished by
// then. An item created after end did not exist yet and returns false. This
// is what makes an explicit range describe the line as it was during the
// range instead of as it is now: items that finished later still count as
// open work, and items opened later do not count at all.
func itemAsOf(it Item, end time.Time) (Item, bool) {
	last := -1
	for i, entry := range it.Timeline() {
		if entry.At.After(end) {
			break
		}
		last = i
	}
	if last < 0 {
		return Item{}, false
	}
	proj := Item{
		ID:        it.ID,
		Kind:      it.Kind,
		Title:     it.Title,
		URL:       it.URL,
		CreatedAt: it.CreatedAt,
		Station:   it.Timeline()[last].Station,
	}
	for _, move := range it.Moves {
		if !move.At.After(end) {
			proj.Moves = append(proj.Moves, move)
		}
	}
	if it.ExitedAt != nil && !it.ExitedAt.After(end) {
		exited := *it.ExitedAt
		proj.ExitedAt = &exited
	}
	return proj, true
}

// BuildRange computes the report for an explicit window [start, end]; end
// doubles as "current time": items are projected to their state at end, so
// the report describes the line as it stood at the window end, with flow
// metrics measured inside the window. A zero start means all history: the
// window begins at the earliest item's creation, which keeps the daily
// sampling bounded. The end must not be zero.
func BuildRange(items []Item, start, end time.Time) Report {
	if start.IsZero() {
		start = end
		for _, it := range items {
			if it.CreatedAt.Before(start) {
				start = it.CreatedAt
			}
		}
	}
	considered := make([]Item, 0, len(items))
	for _, it := range items {
		if proj, ok := itemAsOf(it, end); ok {
			considered = append(considered, proj)
		}
	}
	windowDays := int(math.Ceil(HoursBetween(start, end) / 24))
	return Report{
		GeneratedAt: end,
		WindowStart: start,
		WindowEnd:   end,
		WindowDays:  windowDays,
		Issues:      lineReport(considered, IssuesLine, start, end),
		PRs:         lineReport(considered, PRsLine, start, end),
		Samples:     flowSamples(considered, start, end),
		WIP:         wipItems(considered, end),
	}
}
