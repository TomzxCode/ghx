package processflow

import (
	"strconv"

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
	if issue.State == "closed" {
		item.Station = Closed
		if issue.ClosedAt != nil {
			exited := *issue.ClosedAt
			item.ExitedAt = &exited
		}
	}
	return item
}

// FromPR maps a cached pull request onto a work item. The draft/ready moves
// come from the cached timeline (ready-for-review and converted-to-draft
// events); a PR is created in Draft, and the timeline walk corrects that
// start when the history says otherwise.
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
	for _, event := range pr.Timeline {
		switch event.Kind {
		case github.TimelineReadyForReview:
			item.Moves = append(item.Moves, Move{At: event.CreatedAt, To: Ready})
		case github.TimelineConvertedToDraft:
			item.Moves = append(item.Moves, Move{At: event.CreatedAt, To: Draft})
		}
	}
	switch {
	case pr.MergedAt != nil:
		item.Station = Merged
		exited := *pr.MergedAt
		item.ExitedAt = &exited
	case pr.State == "closed":
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
		item.Station = Ready
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
