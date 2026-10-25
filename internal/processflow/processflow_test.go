package processflow

import (
	"math"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

var now = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func at(day int) time.Time {
	return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC).Add(time.Duration(day) * 24 * time.Hour)
}

// day 0 is Aug 1 12:00 UTC; the window start (90 days before now) is day -85.
func hours(days float64) *float64 { return &days }

func issueItem(id string, created time.Time, station Station, exited *time.Time) Item {
	return Item{ID: id, Kind: KindIssue, Title: "Issue " + id, CreatedAt: created, Station: station, ExitedAt: exited}
}

func prItem(id string, created time.Time, station Station, exited *time.Time, moves []Move) Item {
	return Item{ID: id, Kind: KindPR, Title: "PR " + id, CreatedAt: created, Station: station, ExitedAt: exited, Moves: moves}
}

// scenarioItems covers every station on both lines and one completion per
// line inside the window.
func scenarioItems() []Item {
	return []Item{
		// Issue #1: opened Sep 1 (day 31), closed Sep 10 (day 40): a 9-day trip.
		issueItem("#1", at(31), Closed, tp(at(40))),
		issueItem("#2", at(80), Open, nil), // Oct 20: 5 days open
		issueItem("#3", at(61), Open, nil), // Oct 1: 24 days open
		issueItem("#4", at(75), Open, nil), // Oct 15: 10 days open
		// PR #10: drafted Sep 1, ready Sep 3, merged Sep 10: 2d draft, 7d review.
		prItem("#10", at(31), Merged, tp(at(40)), []Move{{At: at(33), To: Ready}}),
		prItem("#11", at(80), Draft, nil, nil),                             // Oct 20, still a draft
		prItem("#12", at(65), Ready, nil, []Move{{At: at(68), To: Ready}}), // Oct 8
		// PR #13: drafted Sep 15, discarded Sep 20 without merging.
		prItem("#13", at(45), Discarded, tp(at(50)), nil),
	}
}

func tp(t time.Time) *time.Time { return &t }

func lineOf(report LineReport, station Station) *StageStat {
	for i := range report.Stations {
		if report.Stations[i].Station == station {
			return &report.Stations[i]
		}
	}
	return nil
}

func TestQuantileInterpolatesBetweenOrderStatistics(t *testing.T) {
	values := []float64{10, 20, 30, 40}
	if got := *Quantile(values, 0.0); got != 10 {
		t.Errorf("q0 = %v, want 10", got)
	}
	if got := *Quantile(values, 0.5); got != 25 {
		t.Errorf("q0.5 = %v, want 25", got)
	}
	if got := *Quantile(values, 1.0); got != 40 {
		t.Errorf("q1 = %v, want 40", got)
	}
	if Quantile(nil, 0.5) != nil {
		t.Error("quantile of an empty sample should be nil")
	}
	if got := *Quantile([]float64{7}, 0.9); got != 7 {
		t.Errorf("single value quantile = %v, want 7", got)
	}
}

func TestTimelineWalksForwardAndCorrectsMissingHistory(t *testing.T) {
	// A PR drafted Sep 1, made ready Sep 3, still open: two entries.
	pr := prItem("#20", at(31), Ready, nil, []Move{{At: at(33), To: Ready}})
	timeline := pr.Timeline()
	if len(timeline) != 2 || timeline[0].Station != Draft || timeline[1].Station != Ready {
		t.Fatalf("timeline = %v, want draft then ready", timeline)
	}
	// A stale duplicate move (ready again) must not apply: the walk is
	// already in Ready.
	pr = prItem("#21", at(31), Ready, nil, []Move{{At: at(33), To: Ready}, {At: at(34), To: Ready}})
	if timeline = pr.Timeline(); len(timeline) != 2 {
		t.Fatalf("duplicate move applied: %v", timeline)
	}
	// No recorded history at all: the timeline collapses to the current
	// station, entered at creation.
	issue := issueItem("#22", at(31), Closed, nil)
	timeline = issue.Timeline()
	if len(timeline) != 1 || timeline[0].Station != Closed || !timeline[0].At.Equal(at(31)) {
		t.Fatalf("missing-history correction failed: %v", timeline)
	}
}

func TestTimelineIncludesRecordedExit(t *testing.T) {
	// The merge moment is part of the walk, so historical lookups put the PR
	// in Ready until the merge, not in Merged from its last flip.
	pr := prItem("#10", at(31), Merged, tp(at(40)), []Move{{At: at(33), To: Ready}})
	timeline := pr.Timeline()
	if len(timeline) != 3 {
		t.Fatalf("timeline = %v, want three entries", timeline)
	}
	last := timeline[2]
	if last.Station != Merged || !last.At.Equal(at(40)) {
		t.Fatalf("exit entry = %v, want merged at merge time", last)
	}
	if station, ok := stationAt(timeline, at(35)); !ok || station != Ready {
		t.Fatalf("station at day 35 = %v, want ready", station)
	}
}

func TestStageInventoryCountsAndAges(t *testing.T) {
	items := scenarioItems()
	report := Build(items, now, 90)

	open := lineOf(report.Issues, Open)
	if open.Count != 3 {
		t.Errorf("open issue count = %d, want 3", open.Count)
	}
	if *open.OldestHours != 24*24 {
		t.Errorf("oldest open issue = %vh, want 576h", *open.OldestHours)
	}
	if *open.NewestHours != 5*24 {
		t.Errorf("newest open issue = %vh, want 120h", *open.NewestHours)
	}
	if *open.MedianAgeHrs != 10*24 {
		t.Errorf("median open age = %vh, want 240h", *open.MedianAgeHrs)
	}
	closed := lineOf(report.Issues, Closed)
	if closed.Count != 1 {
		t.Errorf("closed issue count = %d, want 1", closed.Count)
	}
	draft := lineOf(report.PRs, Draft)
	if draft.Count != 1 {
		t.Errorf("draft PR count = %d, want 1", draft.Count)
	}
	ready := lineOf(report.PRs, Ready)
	if ready.Count != 1 || *ready.OldestHours != 17*24 {
		t.Errorf("ready PR count/age = %d/%vh, want 1/408h", ready.Count, *ready.OldestHours)
	}
	if lineOf(report.PRs, Merged).Count != 1 || lineOf(report.PRs, Discarded).Count != 1 {
		t.Error("merged and discarded PRs are missing from the inventory")
	}
}

func TestCycleTimeDistributionAndHistogram(t *testing.T) {
	report := Build(scenarioItems(), now, 90)
	if report.Issues.CycleTime.Count != 1 {
		t.Fatalf("issue completions = %d, want 1", report.Issues.CycleTime.Count)
	}
	if *report.Issues.CycleTime.MedianHours != 9*24 {
		t.Errorf("issue cycle time = %vh, want 216h", *report.Issues.CycleTime.MedianHours)
	}
	bucketCount := func(label string) int {
		for _, bucket := range report.Issues.Histogram {
			if bucket.Label == label {
				return bucket.Count
			}
		}
		return -1
	}
	if bucketCount("1-2w") != 1 {
		t.Errorf("9 days should land in the 1-2w bucket, got %d", bucketCount("1-2w"))
	}
	total := 0
	for _, bucket := range report.Issues.Histogram {
		total += bucket.Count
	}
	if total != 1 {
		t.Errorf("histogram total = %d, want 1", total)
	}
	if report.PRs.CycleTime.Count != 1 || *report.PRs.CycleTime.MedianHours != 9*24 {
		t.Errorf("PR cycle time = %+v, want one 216h completion", report.PRs.CycleTime)
	}
}

func TestWaitSharesRankTheBottleneck(t *testing.T) {
	report := Build(scenarioItems(), now, 90)
	total := 0.0
	for _, share := range report.PRs.WaitShares {
		total += share.Share
	}
	if math.Abs(total-1) > 1e-9 {
		t.Errorf("wait shares sum to %v, want 1", total)
	}
	byStation := map[Station]WaitShare{}
	for _, share := range report.PRs.WaitShares {
		byStation[share.Station] = share
	}
	if got := byStation[Ready].Share; math.Abs(got-7.0/9.0) > 1e-9 {
		t.Errorf("ready share = %v, want 7/9", got)
	}
	if got := byStation[Draft].Share; math.Abs(got-2.0/9.0) > 1e-9 {
		t.Errorf("draft share = %v, want 2/9", got)
	}
	if byStation[Merged].Share != 0 {
		t.Errorf("merged station should hold no wait, got %v", byStation[Merged].Share)
	}
}

func TestWeeklyFlowSplitsArrivalsAndCompletions(t *testing.T) {
	report := Build(scenarioItems(), now, 90)
	// Ninety days: twelve whole weeks plus a six-day stub.
	if len(report.Issues.Weekly) != 13 {
		t.Fatalf("issue weeks = %d, want 13", len(report.Issues.Weekly))
	}
	last := report.Issues.Weekly[len(report.Issues.Weekly)-1]
	if last.End.Sub(last.Start) >= 7*24*time.Hour {
		t.Errorf("last week spans %v, want a short stub", last.End.Sub(last.Start))
	}
	created := 0
	completed := 0
	for _, week := range report.Issues.Weekly {
		created += week.Created
		completed += week.Completed
	}
	if created != 4 || completed != 1 {
		t.Errorf("issue weekly totals = %d created / %d completed, want 4/1", created, completed)
	}
	if report.Issues.Arrivals != 4 || report.Issues.Completions != 1 {
		t.Errorf("issue arrivals/completions = %d/%d, want 4/1", report.Issues.Arrivals, report.Issues.Completions)
	}
	if report.PRs.Completions != 1 || report.PRs.Discards != 1 {
		t.Errorf("PR completions/discards = %d/%d, want 1/1", report.PRs.Completions, report.PRs.Discards)
	}
}

func TestLittlesLawIntegratesWorkInProcess(t *testing.T) {
	report := Build(scenarioItems(), now, 90)
	windowHours := 90 * 24
	// Issue active time: 9d (closed one) + 5d + 24d + 10d (the open ones).
	wantWIP := (9*24 + 5*24 + 24*24 + 10*24) / float64(windowHours)
	if got := *report.Issues.Littles.AvgWIP; math.Abs(got-wantWIP) > 1e-9 {
		t.Errorf("issue avg WIP = %v, want %v", got, wantWIP)
	}
	if got := *report.Issues.Littles.ThroughputPerDay; math.Abs(got-1.0/90) > 1e-12 {
		t.Errorf("issue throughput = %v, want %v", got, 1.0/90)
	}
	wantCycle := wantWIP * float64(windowHours)
	if got := *report.Issues.Littles.ProjectedCycleHrs; math.Abs(got-wantCycle) > 1e-9 {
		t.Errorf("implied cycle = %v, want %v", got, wantCycle)
	}
}

func TestQueuePressureReadsNetFlow(t *testing.T) {
	report := Build(scenarioItems(), now, 90)
	if len(report.PRs.Queue) != 2 {
		t.Fatalf("PR queue stations = %d, want 2 (draft, ready)", len(report.PRs.Queue))
	}
	byStation := map[Station]QueuePressure{}
	for _, pressure := range report.PRs.Queue {
		byStation[pressure.Station] = pressure
	}
	draft := byStation[Draft]
	// Arrived: four PR creations. Departed: two ready flips (#10, #12) and
	// one discard (#13).
	if draft.Arrived != 4 || draft.Departed != 3 || draft.Net != 1 || draft.Trend != TrendFilling {
		t.Errorf("draft pressure = %+v, want arrived 4, departed 3, net +1 filling", draft)
	}
	if len(draft.WeeklyWIP) != 13 {
		t.Errorf("draft weekly WIP samples = %d, want 13", len(draft.WeeklyWIP))
	}
	ready := byStation[Ready]
	// Arrived: two flips. Departed: one merge.
	if ready.Arrived != 2 || ready.Departed != 1 || ready.Trend != TrendFilling {
		t.Errorf("ready pressure = %+v, want arrived 2, departed 1, filling", ready)
	}
	issueOpen := report.Issues.Queue[0]
	if issueOpen.Station != Open || issueOpen.Arrived != 4 || issueOpen.Departed != 1 || issueOpen.Net != 3 {
		t.Errorf("open-issue pressure = %+v, want arrived 4, departed 1, net 3", issueOpen)
	}
}

func TestLineFlowsCountObservedMoves(t *testing.T) {
	report := Build(scenarioItems(), now, 90)
	count := func(from, to Station) int {
		for _, flow := range report.PRs.Flows {
			if flow.From == from && flow.To == to {
				return flow.Count
			}
		}
		return 0
	}
	if got := count(Draft, Ready); got != 2 {
		t.Errorf("draft->ready moves = %d, want 2", got)
	}
	if got := count(Ready, Merged); got != 1 {
		t.Errorf("ready->merged moves = %d, want 1", got)
	}
	if got := count(Draft, Discarded); got != 1 {
		t.Errorf("draft->discarded moves = %d, want 1", got)
	}
	if got := len(report.Issues.Flows); got != 1 {
		t.Fatalf("issue flows = %d, want 1", got)
	}
	if report.Issues.Flows[0].From != Open || report.Issues.Flows[0].To != Closed || report.Issues.Flows[0].Count != 1 {
		t.Errorf("issue flow = %+v, want open->closed x1", report.Issues.Flows[0])
	}
}

func TestFlowSamplesSnapshotEveryDay(t *testing.T) {
	report := Build(scenarioItems(), now, 90)
	// One sample per day, inclusive of both ends.
	if len(report.Samples) != 91 {
		t.Fatalf("samples = %d, want 91", len(report.Samples))
	}
	completed := make([]int, 0, len(report.Samples))
	for _, sample := range report.Samples {
		completed = append(completed, sample.Completed)
	}
	for i := 1; i < len(completed); i++ {
		if completed[i] < completed[i-1] {
			t.Fatalf("cumulative completions decreased at sample %d", i)
		}
	}
	if completed[0] != 0 {
		t.Errorf("first sample has %d completed, want 0", completed[0])
	}
	if completed[len(completed)-1] != 2 {
		t.Errorf("last sample has %d completed, want 2", completed[len(completed)-1])
	}
	final := report.Samples[len(report.Samples)-1]
	if final.OpenIssues != 3 || final.DraftPRs != 1 || final.ReadyPRs != 1 {
		t.Errorf("final sample = %+v, want 3 open issues, 1 draft, 1 ready", final)
	}
}

func TestWipItemsSortLongestWaitFirst(t *testing.T) {
	report := Build(scenarioItems(), now, 90)
	if len(report.WIP) != 5 {
		t.Fatalf("WIP list = %d items, want 5", len(report.WIP))
	}
	for i := 1; i < len(report.WIP); i++ {
		if report.WIP[i].AgeHours > report.WIP[i-1].AgeHours {
			t.Fatalf("WIP list is not sorted by age: %v before %v", report.WIP[i-1].AgeHours, report.WIP[i].AgeHours)
		}
	}
	if report.WIP[0].ID != "#3" {
		t.Errorf("oldest WIP item = %s, want #3", report.WIP[0].ID)
	}
}

func TestForecastResamplesWeeklyHistory(t *testing.T) {
	done := ForecastBacklog(2, []int{1, 1, 1, 1}, now)
	if *done.P50Weeks != 2.0 || *done.P85Weeks != 2.0 {
		t.Errorf("forecast = %v/%v weeks, want 2/2", *done.P50Weeks, *done.P85Weeks)
	}
	if !done.P50Date.Equal(now.Add(14 * 24 * time.Hour)) {
		t.Errorf("p50 date = %v, want two weeks out", done.P50Date)
	}
	if done.WeeksObserved != 4 || done.Sims != ForecastSims {
		t.Errorf("forecast meta = %+v", done)
	}

	empty := ForecastBacklog(2, []int{0, 0, 0}, now)
	if empty.P50Weeks != nil || empty.P85Weeks != nil {
		t.Error("no completion history should support no forecast")
	}

	clear := ForecastBacklog(0, []int{1, 2}, now)
	if *clear.P50Weeks != 0 || !clear.P50Date.Equal(now) {
		t.Errorf("empty line forecast = %+v, want zero weeks, dated now", clear)
	}
}

func TestMergedPRWithoutFlipHistoryCountsAsReady(t *testing.T) {
	// Automation merges PRs without anyone flipping them to ready, so the
	// cache has no draft/ready events: the trip must still read as Ready
	// immediately before the merge, not as a full lifetime in Draft.
	pr := prItem("#30", at(31), Merged, tp(at(40)), nil)
	report := Build([]Item{pr}, now, 90)

	if got := activeStationAtExit(pr); got != Ready {
		t.Errorf("exit station = %v, want ready", got)
	}
	count := func(from, to Station) int {
		for _, flow := range report.PRs.Flows {
			if flow.From == from && flow.To == to {
				return flow.Count
			}
		}
		return 0
	}
	if count(Ready, Merged) != 1 || count(Draft, Merged) != 0 {
		t.Errorf("flows = %+v, want ready->merged x1", report.PRs.Flows)
	}
	for _, share := range report.PRs.WaitShares {
		if share.Station == Ready && share.Share != 1 {
			t.Errorf("ready share = %v, want 1", share.Share)
		}
		if share.Station == Draft && share.Share != 0 {
			t.Errorf("draft share = %v, want 0", share.Share)
		}
	}
	// Before the merge, the PR sits in the ready band of the cumulative flow.
	if station, _ := stationAt(pr.Timeline(), at(35)); station != Ready {
		t.Errorf("station mid-trip = %v, want ready", station)
	}
}

func TestOutcomesSplitWindowArrivalsByDisposition(t *testing.T) {
	report := Build(scenarioItems(), now, 90)
	if report.Issues.Outcomes.Created != 4 || report.Issues.Outcomes.Done != 1 || report.Issues.Outcomes.Active != 3 {
		t.Errorf("issue outcomes = %+v, want 4 created, 1 done, 3 active", report.Issues.Outcomes)
	}
	if report.PRs.Outcomes.Created != 4 || report.PRs.Outcomes.Done != 1 || report.PRs.Outcomes.Discarded != 1 || report.PRs.Outcomes.Active != 2 {
		t.Errorf("PR outcomes = %+v, want 4 created, 1 done, 1 discarded, 2 active", report.PRs.Outcomes)
	}
	// Arrivals older than the window do not count: only #4 (Oct 15) and #2
	// (Oct 20) fall inside the last fourteen days.
	narrow := Build(scenarioItems(), now, 14)
	if narrow.Issues.Outcomes.Created != 2 {
		t.Errorf("narrow-window issue outcomes = %+v, want 2 created", narrow.Issues.Outcomes)
	}
}

func TestBuildDefaultsTheWindow(t *testing.T) {
	report := Build(scenarioItems(), now, 0)
	if report.WindowDays != DefaultWindowDays {
		t.Errorf("window = %d, want default %d", report.WindowDays, DefaultWindowDays)
	}
}

func TestBuildRangeCoversAnExplicitWindow(t *testing.T) {
	// A four-day window, Oct 15 to Oct 19: arrivals after that date (#2,
	// Oct 20) must not count, and ages are measured against the window end.
	start := at(75) // Oct 15 12:00
	end := at(79)   // Oct 19 12:00
	items := []Item{
		issueItem("#1", at(31), Closed, tp(at(40))),    // closed Sep 10, before the range
		issueItem("#4", at(75), Open, nil),             // created at the range start
		issueItem("#2", at(80), Open, nil),             // Oct 20, after the range end
		prItem("#10", at(31), Merged, tp(at(78)), nil), // merged inside the range
	}
	report := BuildRange(items, start, end)
	if !report.WindowStart.Equal(start) || !report.WindowEnd.Equal(end) {
		t.Errorf("window = %v..%v, want %v..%v", report.WindowStart, report.WindowEnd, start, end)
	}
	if report.WindowDays != 4 {
		t.Errorf("window days = %d, want 4", report.WindowDays)
	}
	if report.Issues.Arrivals != 1 || report.Issues.Outcomes.Created != 1 {
		t.Errorf("issue arrivals/outcomes = %d/%d, want 1/1 (only #4)", report.Issues.Arrivals, report.Issues.Outcomes.Created)
	}
	if report.PRs.Completions != 1 {
		t.Errorf("PR completions = %d, want 1", report.PRs.Completions)
	}
	// The range filters the items considered: #2 did not exist yet at the
	// window end, so the line holds only #4.
	if open := lineOf(report.Issues, Open); open.Count != 1 {
		t.Errorf("open issues = %d, want 1 (the item created after the range is excluded)", open.Count)
	}
	if len(report.WIP) != 1 {
		t.Errorf("WIP list = %d items, want 1", len(report.WIP))
	}
	// The open issue's age counts from creation to the window end: 4 days.
	open := lineOf(report.Issues, Open)
	if *open.OldestHours != 4*24 {
		t.Errorf("oldest open issue = %vh, want 96h", *open.OldestHours)
	}
}

func TestBuildRangeCountsItemsAsOfTheEnd(t *testing.T) {
	// A PR merged after the range end was still open work during the range:
	// it must sit in Ready as of the end, not in Merged, and its exit must
	// not count as a completion.
	pr := prItem("#40", at(31), Merged, tp(at(80)), []Move{{At: at(33), To: Ready}})
	report := BuildRange([]Item{pr}, at(40), at(50))
	if got := lineOf(report.PRs, Merged).Count; got != 0 {
		t.Errorf("merged count = %d, want 0 (the merge happened after the range)", got)
	}
	if got := lineOf(report.PRs, Ready).Count; got != 1 {
		t.Errorf("ready count = %d, want 1 (as of the range end the PR was in review)", got)
	}
	if report.PRs.Completions != 0 {
		t.Errorf("completions = %d, want 0", report.PRs.Completions)
	}
	if len(report.WIP) != 1 || report.WIP[0].Station != Ready {
		t.Errorf("WIP = %+v, want the PR on the line in Ready", report.WIP)
	}
}

func TestBuildRangeZeroStartStartsAtTheFirstItem(t *testing.T) {
	items := []Item{
		issueItem("#1", at(31), Closed, tp(at(40))),
		issueItem("#2", at(80), Open, nil),
	}
	report := BuildRange(items, time.Time{}, now)
	if !report.WindowStart.Equal(at(31)) {
		t.Errorf("window start = %v, want the earliest item's creation %v", report.WindowStart, at(31))
	}
	if len(report.Samples) != 55 { // Sep 1 (earliest item) .. Oct 25 inclusive
		t.Errorf("samples = %d, want 55 daily samples from the first item", len(report.Samples))
	}
	// An empty line with a zero start covers a zero-length window at the end.
	empty := BuildRange(nil, time.Time{}, now)
	if !empty.WindowStart.Equal(now) || len(empty.Samples) != 1 {
		t.Errorf("empty line = start %v, %d samples, want the end instant and one sample", empty.WindowStart, len(empty.Samples))
	}
}

func TestFromIssueMapsStates(t *testing.T) {
	open := FromIssue(&github.Issue{Number: 1, State: "open", CreatedAt: at(31), UpdatedAt: at(31)})
	if open.Station != Open || open.Kind != KindIssue || open.ExitedAt != nil {
		t.Errorf("open issue = %+v", open)
	}
	closedAt := at(40)
	closed := FromIssue(&github.Issue{Number: 2, State: "closed", CreatedAt: at(31), ClosedAt: &closedAt})
	if closed.Station != Closed || closed.ExitedAt == nil || !closed.ExitedAt.Equal(closedAt) {
		t.Errorf("closed issue = %+v", closed)
	}
	// A closed issue without a close time keeps a nil exit: inventory yes,
	// cycle time no.
	orphan := FromIssue(&github.Issue{Number: 3, State: "closed", CreatedAt: at(31)})
	if orphan.Station != Closed || orphan.ExitedAt != nil {
		t.Errorf("closed issue without close time = %+v", orphan)
	}
	if got := FromIssue(nil); got.ID != "" || got.Kind != "" {
		t.Error("nil issue should map to the zero item")
	}
}

func TestFromPRMapsStatesAndMoves(t *testing.T) {
	mergedAt := at(40)
	merged := &github.PullRequest{Number: 10, State: "closed", CreatedAt: at(31), MergedAt: &mergedAt}
	merged.Timeline = []github.TimelineEvent{{Kind: github.TimelineReadyForReview, CreatedAt: at(33)}}
	item := FromPR(merged)
	if item.Station != Merged || item.ExitedAt == nil || len(item.Moves) != 1 || item.Moves[0].To != Ready {
		t.Errorf("merged PR = %+v", item)
	}

	closedAt := at(50)
	discarded := &github.PullRequest{Number: 13, State: "closed", CreatedAt: at(45), ClosedAt: &closedAt}
	if item = FromPR(discarded); item.Station != Discarded || item.ExitedAt == nil {
		t.Errorf("discarded PR = %+v", item)
	}

	draft := &github.PullRequest{Number: 11, State: "open", IsDraft: true, CreatedAt: at(80)}
	if item = FromPR(draft); item.Station != Draft {
		t.Errorf("draft PR = %+v", item)
	}

	ready := &github.PullRequest{Number: 12, State: "open", CreatedAt: at(65)}
	if item = FromPR(ready); item.Station != Ready {
		t.Errorf("ready PR = %+v", item)
	}

	if got := FromPR(nil); got.ID != "" || got.Kind != "" {
		t.Error("nil PR should map to the zero item")
	}
}
