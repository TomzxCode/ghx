package processflow

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

// FromIssue maps a cached issue onto a work item. A closed issue whose
// closedAt GitHub did not record keeps a nil ExitedAt: it counts toward
// inventory and outcomes but not toward cycle times or throughput, which
// need a completion moment.
func FromIssue(issue *github.Issue) Item {
	if issue == nil {
		return Item{}
	}
	item := Item{
		ID:        issueRef(issue.Number),
		Kind:      KindIssue,
		Title:     issue.Title,
		URL:       issue.URL,
		CreatedAt: issue.CreatedAt,
		Station:   Open,
	}
	// The cached state is the GraphQL enum verbatim ("OPEN"/"CLOSED") in
	// older cache rows, so compare case-insensitively.
	if strings.EqualFold(issue.State, "closed") {
		item.Station = Closed
		if issue.ClosedAt != nil {
			exited := *issue.ClosedAt
			item.ExitedAt = &exited
		}
	}
	return item
}

// FromPR maps a cached pull request onto a work item. The intermediate moves
// come from two cached sources: the timeline (ready-for-review and
// converted-to-draft events) and the reviews (an approval moves the PR to
// waiting-on-merge, a changes-requested review to waiting-on-author; plain
// comments do not change the turn). A PR with no flip events was opened
// directly as ready for review and starts in the review-wait station.
func FromPR(pr *github.PullRequest) Item {
	if pr == nil {
		return Item{}
	}
	item := Item{
		ID:        issueRef(pr.Number),
		Kind:      KindPR,
		Title:     pr.Title,
		URL:       pr.URL,
		CreatedAt: pr.CreatedAt,
		Station:   Draft,
	}
	// Collect every transition candidate with its time, then walk them in
	// order from the initial station so each move records the station it
	// actually left.
	type candidate struct {
		at time.Time
		to Station
	}
	var candidates []candidate
	wasEverDraft := false
	for _, event := range pr.Timeline {
		switch event.Kind {
		case github.TimelineReadyForReview:
			candidates = append(candidates, candidate{at: event.CreatedAt, to: AwaitingReview})
			wasEverDraft = true
		case github.TimelineConvertedToDraft:
			candidates = append(candidates, candidate{at: event.CreatedAt, to: Draft})
			wasEverDraft = true
		}
	}
	for _, review := range pr.Reviews {
		switch strings.ToUpper(review.State) {
		case "APPROVED":
			candidates = append(candidates, candidate{at: review.SubmittedAt, to: AwaitingMerge})
		case "CHANGES_REQUESTED":
			candidates = append(candidates, candidate{at: review.SubmittedAt, to: AwaitingAuthor})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].at.Before(candidates[j].at) })
	if wasEverDraft {
		item.Station = Draft
	} else {
		// No flip events: the PR was opened directly as ready for review and
		// has never been in the draft station.
		item.Station = AwaitingReview
	}
	current := item.Station
	for _, c := range candidates {
		if c.at.Before(item.CreatedAt) {
			continue
		}
		if c.to == current {
			continue
		}
		item.Moves = append(item.Moves, Move{At: c.at, From: current, To: c.to})
		current = c.to
	}
	switch {
	case pr.MergedAt != nil:
		item.Station = Merged
		exited := *pr.MergedAt
		item.ExitedAt = &exited
	case strings.EqualFold(pr.State, "closed"):
		item.Station = Discarded
		if pr.ClosedAt != nil {
			exited := *pr.ClosedAt
			item.ExitedAt = &exited
		} else {
			// A discarded PR with no close time: fall back to its last known
			// activity, the same correction missing timeline history gets.
			exited := pr.UpdatedAt
			item.ExitedAt = &exited
		}
	case pr.IsDraft:
		item.Station = Draft
	default:
		item.Station = AwaitingReview
	}
	return item
}

// FromIssues converts a batch of cached issues, skipping nil entries.
func FromIssues(issues []*github.Issue) []Item {
	items := make([]Item, 0, len(issues))
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		items = append(items, FromIssue(issue))
	}
	return items
}

// FromPRs converts a batch of cached pull requests, skipping nil entries.
func FromPRs(prs []*github.PullRequest) []Item {
	items := make([]Item, 0, len(prs))
	for _, pr := range prs {
		if pr == nil {
			continue
		}
		items = append(items, FromPR(pr))
	}
	return items
}

func issueRef(number int) string {
	return "#" + strconv.Itoa(number)
}
