package mockserver

import (
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

func TestGenerate_SmallConfig(t *testing.T) {
	cfg := SmallConfig()
	scenario := Generate(cfg)

	if len(scenario.Issues) != cfg.IssuesPerRepo*len(cfg.Repos) {
		t.Errorf("issues: got %d, want %d", len(scenario.Issues), cfg.IssuesPerRepo*len(cfg.Repos))
	}
	if len(scenario.PRs) != cfg.PRsPerRepo*len(cfg.Repos) {
		t.Errorf("PRs: got %d, want %d", len(scenario.PRs), cfg.PRsPerRepo*len(cfg.Repos))
	}
}

func TestGenerate_AllReposHaveData(t *testing.T) {
	cfg := SmallConfig()
	scenario := Generate(cfg)

	issuesByRepo := map[string]int{}
	prsByRepo := map[string]int{}
	for _, issue := range scenario.Issues {
		key := issue.Owner + "/" + issue.Repo
		issuesByRepo[key]++
	}
	for _, pr := range scenario.PRs {
		key := pr.Owner + "/" + pr.Repo
		prsByRepo[key]++
	}

	for _, r := range cfg.Repos {
		if issuesByRepo[r] == 0 {
			t.Errorf("no issues for repo %s", r)
		}
		if prsByRepo[r] == 0 {
			t.Errorf("no PRs for repo %s", r)
		}
	}
}

func TestGenerate_IssuesHaveComments(t *testing.T) {
	cfg := SmallConfig()
	cfg.CommentsPerIssue = 3
	scenario := Generate(cfg)

	totalComments := 0
	for _, issue := range scenario.Issues {
		totalComments += len(issue.Comments)
	}

	if totalComments == 0 {
		t.Error("expected at least some issue comments")
	}
}

func TestGenerate_PRsHaveComments(t *testing.T) {
	cfg := SmallConfig()
	cfg.CommentsPerPR = 3
	scenario := Generate(cfg)

	totalComments := 0
	for _, pr := range scenario.PRs {
		totalComments += len(pr.Comments)
	}

	if totalComments == 0 {
		t.Error("expected at least some PR comments")
	}
}

func TestGenerate_CloseRate(t *testing.T) {
	cfg := SmallConfig()
	cfg.CloseRate = 1.0
	scenario := Generate(cfg)

	closed := 0
	for _, issue := range scenario.Issues {
		if issue.State == "CLOSED" {
			closed++
		}
	}
	if closed != len(scenario.Issues) {
		t.Errorf("with CloseRate=1.0: got %d closed out of %d", closed, len(scenario.Issues))
	}
}

func TestGenerate_CloseRateZero(t *testing.T) {
	cfg := SmallConfig()
	cfg.CloseRate = 0.0
	scenario := Generate(cfg)

	for _, issue := range scenario.Issues {
		if issue.State != "OPEN" {
			t.Errorf("with CloseRate=0.0: issue #%d has state %q, want OPEN", issue.Number, issue.State)
		}
	}
}

func TestGenerate_MergeRate(t *testing.T) {
	cfg := SmallConfig()
	cfg.MergeRate = 1.0
	cfg.DraftRate = 0.0
	scenario := Generate(cfg)

	merged := 0
	for _, pr := range scenario.PRs {
		if pr.State == "MERGED" {
			merged++
		}
	}
	if merged != len(scenario.PRs) {
		t.Errorf("with MergeRate=1.0: got %d merged out of %d", merged, len(scenario.PRs))
	}
}

func TestGenerate_MergeRateZero(t *testing.T) {
	cfg := SmallConfig()
	cfg.MergeRate = 0.0
	scenario := Generate(cfg)

	for _, pr := range scenario.PRs {
		if pr.State == "MERGED" {
			t.Errorf("with MergeRate=0.0: PR #%d should not be MERGED", pr.Number)
		}
	}
}

func TestGenerate_MergeTimesDistinct(t *testing.T) {
	cfg := SmallConfig()
	cfg.MergeRate = 1.0
	cfg.History = 90 * 24 * time.Hour
	scenario := Generate(cfg)

	// Every merged PR must merge after creation, and merge times must not
	// all collapse onto the same instant (a loop-variable aliasing bug).
	distinct := map[string]bool{}
	for _, pr := range scenario.PRs {
		if pr.MergedAt == nil {
			continue
		}
		if pr.MergedAt.Before(pr.CreatedAt) {
			t.Errorf("PR #%d merged before it was created", pr.Number)
		}
		distinct[pr.MergedAt.String()] = true
	}
	if len(distinct) < 2 {
		t.Errorf("expected distinct merge times, got %d", len(distinct))
	}
}

func TestGenerate_DraftRate(t *testing.T) {
	cfg := SmallConfig()
	cfg.DraftRate = 1.0
	cfg.MergeRate = 0.0
	scenario := Generate(cfg)

	// Drafts either stay in draft or carry a ready-for-review event; some
	// of both should appear across the generated PRs.
	drafts, ready := 0, 0
	for _, pr := range scenario.PRs {
		if pr.IsDraft {
			drafts++
		}
		for _, ev := range pr.Timeline {
			if ev.Kind == github.TimelineReadyForReview {
				ready++
			}
		}
	}
	if drafts == 0 && ready == 0 {
		t.Error("with DraftRate=1.0: expected drafts or ready-for-review events")
	}
	if ready == 0 {
		t.Error("with DraftRate=1.0: expected some drafts to become ready for review")
	}
}

func TestGenerate_DraftRateZero(t *testing.T) {
	cfg := SmallConfig()
	cfg.DraftRate = 0.0
	cfg.MergeRate = 0.0
	scenario := Generate(cfg)

	for _, pr := range scenario.PRs {
		if pr.IsDraft {
			t.Errorf("with DraftRate=0.0: PR #%d should not be draft", pr.Number)
		}
	}
}

func TestGenerate_TimestampsAreConsistent(t *testing.T) {
	cfg := SmallConfig()
	scenario := Generate(cfg)

	for _, issue := range scenario.Issues {
		if issue.CreatedAt.After(issue.UpdatedAt) {
			t.Errorf("issue #%d: createdAt > updatedAt", issue.Number)
		}
		if issue.ClosedAt != nil && issue.ClosedAt.Before(issue.CreatedAt) {
			t.Errorf("issue #%d: closedAt < createdAt", issue.Number)
		}
		for _, c := range issue.Comments {
			if c.CreatedAt.Before(issue.CreatedAt) {
				t.Errorf("issue #%d: comment createdAt < issue createdAt", issue.Number)
			}
		}
	}

	for _, pr := range scenario.PRs {
		if pr.CreatedAt.After(pr.UpdatedAt) {
			t.Errorf("PR #%d: createdAt > updatedAt", pr.Number)
		}
		if pr.MergedAt != nil && pr.MergedAt.Before(pr.CreatedAt) {
			t.Errorf("PR #%d: mergedAt < createdAt", pr.Number)
		}
		if pr.ClosedAt != nil && pr.ClosedAt.Before(pr.CreatedAt) {
			t.Errorf("PR #%d: closedAt < createdAt", pr.Number)
		}
		for _, c := range pr.Comments {
			if c.CreatedAt.Before(pr.CreatedAt) {
				t.Errorf("PR #%d: comment createdAt < PR createdAt", pr.Number)
			}
		}
	}
}

func TestGenerate_TimestampsWithinHistory(t *testing.T) {
	cfg := SmallConfig()
	cfg.Now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cfg.History = 30 * 24 * time.Hour
	scenario := Generate(cfg)

	start := cfg.Now.Add(-cfg.History)
	end := cfg.Now

	for _, issue := range scenario.Issues {
		if issue.CreatedAt.Before(start) || issue.CreatedAt.After(end) {
			t.Errorf("issue #%d: createdAt %v outside [%v, %v]", issue.Number, issue.CreatedAt, start, end)
		}
	}
	for _, pr := range scenario.PRs {
		if pr.CreatedAt.Before(start) || pr.CreatedAt.After(end) {
			t.Errorf("PR #%d: createdAt %v outside [%v, %v]", pr.Number, pr.CreatedAt, start, end)
		}
	}
}

func TestGenerate_Deterministic(t *testing.T) {
	cfg := SmallConfig()

	s1 := Generate(cfg)
	s2 := Generate(cfg)

	if len(s1.Issues) != len(s2.Issues) {
		t.Fatalf("issue count differs: %d vs %d", len(s1.Issues), len(s2.Issues))
	}
	if len(s1.PRs) != len(s2.PRs) {
		t.Fatalf("PR count differs: %d vs %d", len(s1.PRs), len(s2.PRs))
	}

	issuesByNum1 := map[int]string{}
	issuesByNum2 := map[int]string{}
	for _, issue := range s1.Issues {
		issuesByNum1[issue.Number] = issue.Title
	}
	for _, issue := range s2.Issues {
		issuesByNum2[issue.Number] = issue.Title
	}

	for num, title1 := range issuesByNum1 {
		title2, ok := issuesByNum2[num]
		if !ok {
			t.Errorf("issue #%d in s1 but not s2", num)
		} else if title1 != title2 {
			t.Errorf("issue #%d title differs: %q vs %q", num, title1, title2)
		}
	}
}

func TestGenerate_DifferentSeedsProduceDifferentResults(t *testing.T) {
	cfg1 := SmallConfig()
	cfg1.Seed = 1
	cfg2 := SmallConfig()
	cfg2.Seed = 2

	s1 := Generate(cfg1)
	s2 := Generate(cfg2)

	different := false
	if len(s1.Issues) == len(s2.Issues) && len(s1.Issues) > 0 {
		for i := range s1.Issues {
			if s1.Issues[i].Title != s2.Issues[i].Title {
				different = true
				break
			}
		}
	} else {
		different = len(s1.Issues) != len(s2.Issues)
	}

	if !different {
		t.Error("different seeds should produce different results")
	}
}

func TestGenerate_MultipleRepos(t *testing.T) {
	cfg := SmallConfig()
	cfg.Repos = []string{"org/repo-a", "org/repo-b", "org/repo-c"}
	cfg.IssuesPerRepo = 3
	cfg.PRsPerRepo = 2
	scenario := Generate(cfg)

	if len(scenario.Issues) != 9 {
		t.Errorf("issues: got %d, want 9", len(scenario.Issues))
	}
	if len(scenario.PRs) != 6 {
		t.Errorf("PRs: got %d, want 6", len(scenario.PRs))
	}

	seen := map[string]bool{}
	for _, issue := range scenario.Issues {
		key := issue.Owner + "/" + issue.Repo
		seen[key] = true
	}
	if len(seen) != 3 {
		t.Errorf("expected 3 distinct repos, got %d", len(seen))
	}
}

func TestGenerate_ReviewDecisions(t *testing.T) {
	cfg := SmallConfig()
	cfg.ReviewRate = 1.0
	cfg.MergeRate = 0.0
	scenario := Generate(cfg)

	reviewed := 0
	for _, pr := range scenario.PRs {
		if pr.ReviewDecision != "" {
			reviewed++
		}
	}
	if reviewed != len(scenario.PRs) {
		t.Errorf("with ReviewRate=1.0: got %d reviewed out of %d", reviewed, len(scenario.PRs))
	}
}

func TestGenerate_Labels(t *testing.T) {
	cfg := SmallConfig()
	cfg.LabelsPerItem = 3
	scenario := Generate(cfg)

	hasLabels := false
	for _, issue := range scenario.Issues {
		if len(issue.Labels) > 0 {
			hasLabels = true
			break
		}
	}
	if !hasLabels {
		t.Error("expected at least some issues to have labels")
	}
}

func TestGenerate_Assignees(t *testing.T) {
	cfg := SmallConfig()
	cfg.AssigneesPerIssue = 3
	cfg.AssigneesPerPR = 3
	scenario := Generate(cfg)

	hasAssignees := false
	for _, issue := range scenario.Issues {
		if len(issue.Assignees) > 0 {
			hasAssignees = true
			break
		}
	}
	if !hasAssignees {
		t.Error("expected at least some issues to have assignees")
	}
}

func TestGenerate_Milestones(t *testing.T) {
	cfg := SmallConfig()
	cfg.MilestonesPerRepo = 3
	scenario := Generate(cfg)

	hasMilestone := false
	for _, issue := range scenario.Issues {
		if issue.Milestone != nil {
			hasMilestone = true
			break
		}
	}
	if !hasMilestone {
		t.Error("expected at least some issues to have milestones")
	}
}

func TestGenerate_URLsAreCorrect(t *testing.T) {
	cfg := SmallConfig()
	scenario := Generate(cfg)

	for _, issue := range scenario.Issues {
		expected := "https://github.com/" + issue.Owner + "/" + issue.Repo + "/issues/" + itoa(issue.Number)
		if issue.URL != expected {
			t.Errorf("issue URL: got %q, want %q", issue.URL, expected)
		}
	}
	for _, pr := range scenario.PRs {
		expected := "https://github.com/" + pr.Owner + "/" + pr.Repo + "/pull/" + itoa(pr.Number)
		if pr.URL != expected {
			t.Errorf("PR URL: got %q, want %q", pr.URL, expected)
		}
	}
}

func TestGenerate_MergedPRsNotDraft(t *testing.T) {
	cfg := SmallConfig()
	cfg.MergeRate = 1.0
	cfg.DraftRate = 1.0
	scenario := Generate(cfg)

	for _, pr := range scenario.PRs {
		if pr.State == "MERGED" && pr.IsDraft {
			t.Errorf("PR #%d is MERGED but still marked as draft", pr.Number)
		}
	}
}

func TestGenerate_MergedPRsHaveTimestamps(t *testing.T) {
	cfg := SmallConfig()
	cfg.MergeRate = 1.0
	cfg.DraftRate = 0.0
	scenario := Generate(cfg)

	for _, pr := range scenario.PRs {
		if pr.State == "MERGED" {
			if pr.MergedAt == nil {
				t.Errorf("merged PR #%d has nil mergedAt", pr.Number)
			}
			if pr.ClosedAt == nil {
				t.Errorf("merged PR #%d has nil closedAt", pr.Number)
			}
		}
	}
}

func TestGenerate_ClosedIssuesHaveTimestamps(t *testing.T) {
	cfg := SmallConfig()
	cfg.CloseRate = 1.0
	scenario := Generate(cfg)

	for _, issue := range scenario.Issues {
		if issue.State == "CLOSED" && issue.ClosedAt == nil {
			t.Errorf("closed issue #%d has nil closedAt", issue.Number)
		}
	}
}

func TestGenerate_CommentTimestampsOrdered(t *testing.T) {
	cfg := SmallConfig()
	cfg.CommentsPerIssue = 5
	cfg.CommentsPerPR = 5
	scenario := Generate(cfg)

	for _, issue := range scenario.Issues {
		for i := 1; i < len(issue.Comments); i++ {
			if issue.Comments[i].CreatedAt.Before(issue.Comments[i-1].CreatedAt) {
				t.Errorf("issue #%d: comments not in chronological order at index %d", issue.Number, i)
			}
		}
	}
	for _, pr := range scenario.PRs {
		for i := 1; i < len(pr.Comments); i++ {
			if pr.Comments[i].CreatedAt.Before(pr.Comments[i-1].CreatedAt) {
				t.Errorf("PR #%d: comments not in chronological order at index %d", pr.Number, i)
			}
		}
	}
}

func TestGenerate_WorksWithServer(t *testing.T) {
	cfg := SmallConfig()
	scenario := Generate(cfg)
	srv := NewServer(scenario)
	defer srv.Close()

	gql := postGQL(t, srv.URL(), `
	query($owner: String!, $repo: String!, $first: Int!, $states: [IssueState!]) {
	  repository(owner: $owner, name: $repo) {
	    issues(first: $first, states: $states, orderBy: {field: UPDATED_AT, direction: DESC}) {
	      pageInfo { hasNextPage }
	      nodes { number title state }
	    }
	  }
	}`, map[string]interface{}{
		"owner":  "acme",
		"repo":   "testrepo",
		"first":  100,
		"states": []string{"OPEN", "CLOSED"},
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("errors: %v", gql.Errors)
	}
}

func TestGenerate_ActivityBursts(t *testing.T) {
	cfg := SmallConfig()
	cfg.ActivityBursts = 3
	scenario := Generate(cfg)

	if len(scenario.Issues) == 0 {
		t.Error("expected issues to be generated with activity bursts")
	}
}

func TestGenerate_LargeConfig(t *testing.T) {
	cfg := SimulationConfig{
		NumUsers:          20,
		Repos:             []string{"bigcorp/monolith", "bigcorp/microservice", "bigcorp/infra", "bigcorp/docs"},
		History:           180 * 24 * time.Hour,
		IssuesPerRepo:     50,
		PRsPerRepo:        40,
		CommentsPerIssue:  5,
		CommentsPerPR:     4,
		AssigneesPerIssue: 2,
		AssigneesPerPR:    2,
		LabelsPerItem:     3,
		MilestonesPerRepo: 5,
		CloseRate:         0.6,
		MergeRate:         0.7,
		DraftRate:         0.15,
		ReviewRate:        0.8,
		Seed:              12345,
		ActivityBursts:    5,
	}

	start := time.Now()
	scenario := Generate(cfg)
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("generation took %v, expected < 5s", elapsed)
	}

	expectedIssues := cfg.IssuesPerRepo * len(cfg.Repos)
	expectedPRs := cfg.PRsPerRepo * len(cfg.Repos)
	if len(scenario.Issues) != expectedIssues {
		t.Errorf("issues: got %d, want %d", len(scenario.Issues), expectedIssues)
	}
	if len(scenario.PRs) != expectedPRs {
		t.Errorf("PRs: got %d, want %d", len(scenario.PRs), expectedPRs)
	}

	t.Logf("Generated %d issues, %d PRs in %v", len(scenario.Issues), len(scenario.PRs), elapsed)

	totalComments := 0
	for _, issue := range scenario.Issues {
		totalComments += len(issue.Comments)
	}
	for _, pr := range scenario.PRs {
		totalComments += len(pr.Comments)
	}
	t.Logf("Total comments: %d", totalComments)
}

func TestSimulationStats(t *testing.T) {
	cfg := DefaultConfig()
	stats := SimulationStats(cfg)

	if stats == "" {
		t.Error("SimulationStats should not be empty")
	}
}

func TestGenerate_ZeroComments(t *testing.T) {
	cfg := SmallConfig()
	cfg.CommentsPerIssue = 0
	cfg.CommentsPerPR = 0
	scenario := Generate(cfg)

	for _, issue := range scenario.Issues {
		if len(issue.Comments) != 0 {
			t.Errorf("issue #%d should have 0 comments, got %d", issue.Number, len(issue.Comments))
		}
	}
	for _, pr := range scenario.PRs {
		if len(pr.Comments) != 0 {
			t.Errorf("PR #%d should have 0 comments, got %d", pr.Number, len(pr.Comments))
		}
	}
}

func TestGenerate_PRHeadBranch(t *testing.T) {
	cfg := SmallConfig()
	scenario := Generate(cfg)

	for _, pr := range scenario.PRs {
		if pr.HeadRefName == "" {
			t.Errorf("PR #%d has empty HeadRefName", pr.Number)
		}
		if pr.BaseRefName != "main" {
			t.Errorf("PR #%d has BaseRefName %q, want 'main'", pr.Number, pr.BaseRefName)
		}
	}
}

func TestGenerate_CommentCountMatches(t *testing.T) {
	cfg := SmallConfig()
	cfg.CommentsPerIssue = 3
	cfg.CommentsPerPR = 2
	scenario := Generate(cfg)

	for _, issue := range scenario.Issues {
		if issue.CommentCount != len(issue.Comments) {
			t.Errorf("issue #%d: CommentCount=%d but len(Comments)=%d",
				issue.Number, issue.CommentCount, len(issue.Comments))
		}
	}
	for _, pr := range scenario.PRs {
		if pr.CommentCount != len(pr.Comments) {
			t.Errorf("PR #%d: CommentCount=%d but len(Comments)=%d",
				pr.Number, pr.CommentCount, len(pr.Comments))
		}
	}
}

func itoa(n int) string {
	return formatInt(n)
}

func formatInt(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
