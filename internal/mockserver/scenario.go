package mockserver

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

type ScenarioBuilder struct {
	owner      string
	repo       string
	now        time.Time
	rng        *rand.Rand
	issues     []ScenarioIssue
	prs        []ScenarioPR
	nextNum    int
	users      []string
	labels     []github.Label
	milestones []github.Milestone
}

func NewScenarioBuilder(owner, repo string) *ScenarioBuilder {
	return &ScenarioBuilder{
		owner: owner,
		repo:  repo,
		now:   time.Now(),
		rng:   rand.New(rand.NewSource(42)),
		users: []string{"alice", "bob", "carol", "dave", "eve"},
		labels: []github.Label{
			{Name: "bug", Color: "d73a4a"},
			{Name: "enhancement", Color: "a2eeef"},
			{Name: "documentation", Color: "0075ca"},
			{Name: "good first issue", Color: "7057ff"},
			{Name: "help wanted", Color: "008672"},
			{Name: "p0", Color: "b60205"},
			{Name: "p1", Color: "d93f0b"},
			{Name: "p2", Color: "fbca04"},
		},
		milestones: []github.Milestone{
			{Number: 1, Title: "v1.0"},
			{Number: 2, Title: "v2.0"},
			{Number: 3, Title: "v3.0"},
		},
	}
}

func (b *ScenarioBuilder) WithSeed(seed int64) *ScenarioBuilder {
	b.rng = rand.New(rand.NewSource(seed))
	return b
}

func (b *ScenarioBuilder) WithUsers(users []string) *ScenarioBuilder {
	b.users = users
	return b
}

func (b *ScenarioBuilder) WithNow(t time.Time) *ScenarioBuilder {
	b.now = t
	return b
}

func (b *ScenarioBuilder) nextNumber() int {
	b.nextNum++
	return b.nextNum
}

func (b *ScenarioBuilder) randomUser() string {
	return b.users[b.rng.Intn(len(b.users))]
}

func (b *ScenarioBuilder) randomLabels(n int) []github.Label {
	perm := b.rng.Perm(len(b.labels))
	count := n
	if count > len(b.labels) {
		count = len(b.labels)
	}
	result := make([]github.Label, count)
	for i := 0; i < count; i++ {
		result[i] = b.labels[perm[i]]
	}
	return result
}

func (b *ScenarioBuilder) randomMilestone() *github.Milestone {
	if b.rng.Float64() < 0.4 {
		return nil
	}
	m := b.milestones[b.rng.Intn(len(b.milestones))]
	return &m
}

func (b *ScenarioBuilder) url(path string, number int) string {
	return fmt.Sprintf("https://github.com/%s/%s/%s/%d", b.owner, b.repo, path, number)
}

func (b *ScenarioBuilder) AddIssue(title, body string, age time.Duration, opts ...IssueOption) *ScenarioBuilder {
	createdAt := b.now.Add(-age)
	updatedAt := createdAt
	if age > 0 {
		updatedAt = createdAt.Add(time.Duration(b.rng.Intn(int(age/2))) * time.Second)
	}
	num := b.nextNumber()

	labelCount := b.rng.Intn(3)
	var labels []github.Label
	if labelCount > 0 {
		labels = b.randomLabels(labelCount)
	}

	issue := github.Issue{
		Number:    num,
		Title:     title,
		State:     "OPEN",
		Author:    github.Actor{Login: b.randomUser()},
		Labels:    labels,
		Milestone: b.randomMilestone(),
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		URL:       b.url("issues", num),
		Body:      body,
	}

	for _, opt := range opts {
		opt(&issue, b)
	}

	b.issues = append(b.issues, ScenarioIssue{
		Owner: b.owner,
		Repo:  b.repo,
		Issue: issue,
	})
	return b
}

func (b *ScenarioBuilder) AddPR(title, body, headBranch string, age time.Duration, opts ...PROption) *ScenarioBuilder {
	createdAt := b.now.Add(-age)
	updatedAt := createdAt
	if age > 0 {
		updatedAt = createdAt.Add(time.Duration(b.rng.Intn(int(age/2))) * time.Second)
	}
	num := b.nextNumber()

	labelCount := b.rng.Intn(3)
	var labels []github.Label
	if labelCount > 0 {
		labels = b.randomLabels(labelCount)
	}

	pr := github.PullRequest{
		Number:         num,
		Title:          title,
		State:          "OPEN",
		IsDraft:        false,
		ReviewDecision: "REVIEW_REQUIRED",
		Author:         github.Actor{Login: b.randomUser()},
		Labels:         labels,
		Milestone:      b.randomMilestone(),
		BaseRefName:    "main",
		HeadRefName:    headBranch,
		HeadRefOid:     fakeOID(num),
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		URL:            b.url("pull", num),
		Body:           body,
	}

	for _, opt := range opts {
		opt(&pr, b)
	}

	b.prs = append(b.prs, ScenarioPR{
		Owner:       b.owner,
		Repo:        b.repo,
		PullRequest: pr,
	})
	return b
}

func (b *ScenarioBuilder) Build() *Scenario {
	return &Scenario{
		Issues: b.issues,
		PRs:    b.prs,
	}
}

func (b *ScenarioBuilder) GenerateRealistic() *Scenario {
	return b.
		AddIssue("Fix memory leak in worker pool", "The worker pool does not release memory after tasks complete. This causes steady memory growth under load.", 30*24*time.Hour, WithIssueState("CLOSED"), WithIssueAssignee("bob"), WithIssueComment("alice", "Confirmed, I see this in production too.", 29*24*time.Hour), WithIssueComment("bob", "Fixed in #5.", 28*24*time.Hour)).
		AddIssue("Add dark mode support", "Users have requested a dark theme. Should use CSS custom properties for theming.", 25*24*time.Hour, WithIssueLabels("enhancement", "p2"), WithIssueComment("carol", "Happy to work on this!", 24*24*time.Hour)).
		AddIssue("Crash on invalid JSON input", "The API server panics when receiving malformed JSON. Should return a 400 error instead.", 20*24*time.Hour, WithIssueState("CLOSED"), WithIssueLabels("bug", "p0"), WithIssueComment("dave", "Stack trace: panic at parser.go:42", 20*24*time.Hour), WithIssueComment("alice", "Fixed in #8.", 19*24*time.Hour)).
		AddIssue("Improve documentation for API endpoints", "The API docs are outdated and missing several new endpoints.", 15*24*time.Hour, WithIssueLabels("documentation"), WithIssueMilestone("v2.0"), WithIssueComment("eve", "I can help with this.", 14*24*time.Hour), WithIssueComment("alice", "Assigned to you, Eve.", 14*24*time.Hour)).
		AddIssue("Support pagination in list endpoints", "All list endpoints should support cursor-based pagination.", 10*24*time.Hour, WithIssueLabels("enhancement", "p1"), WithIssueMilestone("v2.0"), WithIssueComment("bob", "Working on this.", 9*24*time.Hour), WithIssueComment("bob", "PR up: #12.", 8*24*time.Hour)).
		AddIssue("Add health check endpoint", "Need a /healthz endpoint for Kubernetes readiness probes.", 5*24*time.Hour, WithIssueLabels("enhancement", "good first issue")).
		AddIssue("Race condition in concurrent cache writes", "Multiple goroutines writing to the cache simultaneously can cause data corruption.", 3*24*time.Hour, WithIssueLabels("bug", "p0"), WithIssueAssignee("alice"), WithIssueComment("dave", "Reproduced with `-race` flag.", 3*24*time.Hour)).
		AddIssue("Migrate to structured logging", "Replace fmt.Println calls with structured logging using slog.", 2*24*time.Hour, WithIssueLabels("enhancement", "help wanted"), WithIssueMilestone("v3.0")).
		AddPR("Fix memory leak in worker pool", "Releases idle workers after timeout. Fixes #1.", "fix/memory-leak", 28*24*time.Hour, WithPRState("MERGED"), WithPRReview("APPROVED"), WithPRComment("alice", "Looks good, merging.", 28*24*time.Hour)).
		AddPR("Fix crash on invalid JSON input", "Adds proper error handling for JSON parsing. Fixes #3.", "fix/json-crash", 19*24*time.Hour, WithPRState("MERGED"), WithPRReview("APPROVED"), WithPRComment("dave", "Tested locally, works great.", 19*24*time.Hour)).
		AddPR("Add dark mode support", "Implements dark mode using CSS custom properties and prefers-color-scheme.", "feat/dark-mode", 23*24*time.Hour, WithPRLabels("enhancement"), WithPRComment("carol", "Still need to add the toggle button.", 22*24*time.Hour), WithPRComment("bob", "Looking forward to this!", 21*24*time.Hour)).
		AddPR("Support pagination in list endpoints", "Adds cursor-based pagination to all list endpoints. Closes #5.", "feat/pagination", 8*24*time.Hour, WithPRLabels("enhancement"), WithPRMilestone("v2.0"), WithPRReview("CHANGES_REQUESTED"), WithPRComment("alice", "Please add integration tests.", 7*24*time.Hour)).
		AddPR("WIP: Refactor authentication middleware", "Major refactor of auth middleware to support multiple providers.", "refactor/auth", 6*24*time.Hour, WithPRDraft(true), WithPRComment("alice", "Still a work in progress.", 6*24*time.Hour)).
		AddPR("Fix race condition in cache writes", "Adds sync.RWMutex to cache write operations. Fixes #7.", "fix/cache-race", 2*24*time.Hour, WithPRLabels("bug"), WithPRReview("APPROVED"), WithPRComment("dave", "LGTM, but please add a benchmark.", 2*24*time.Hour), WithPRComment("alice", "Benchmark added.", 1*24*time.Hour)).
		Build()
}

// fakeOID returns a deterministic 40-hex-character head commit id for a PR, so
// cached fixtures look like real GitHub SHAs without shipping a commit graph.
func fakeOID(seed int) string {
	return fmt.Sprintf("%040x", uint64(seed)*0x9E3779B97F4A7C15)
}

// ---------------------------------------------------------------------------
// Issue options
// ---------------------------------------------------------------------------

type IssueOption func(*github.Issue, *ScenarioBuilder)

func WithIssueState(state string) IssueOption {
	return func(issue *github.Issue, b *ScenarioBuilder) {
		issue.State = state
		if state == "CLOSED" {
			closedAt := issue.UpdatedAt
			issue.ClosedAt = &closedAt
		}
	}
}

func WithIssueAssignee(login string) IssueOption {
	return func(issue *github.Issue, b *ScenarioBuilder) {
		issue.Assignees = append(issue.Assignees, github.Actor{Login: login})
	}
}

func WithIssueLabels(names ...string) IssueOption {
	return func(issue *github.Issue, b *ScenarioBuilder) {
		issue.Labels = nil
		for _, name := range names {
			color := "ffffff"
			for _, l := range b.labels {
				if l.Name == name {
					color = l.Color
					break
				}
			}
			issue.Labels = append(issue.Labels, github.Label{Name: name, Color: color})
		}
	}
}

func WithIssueMilestone(title string) IssueOption {
	return func(issue *github.Issue, b *ScenarioBuilder) {
		for _, m := range b.milestones {
			if m.Title == title {
				issue.Milestone = &github.Milestone{Number: m.Number, Title: m.Title}
				return
			}
		}
	}
}

func WithIssueComment(author, body string, age time.Duration) IssueOption {
	return func(issue *github.Issue, b *ScenarioBuilder) {
		createdAt := b.now.Add(-age)
		updatedAt := createdAt
		issue.Comments = append(issue.Comments, github.Comment{
			ID:        fmt.Sprintf("ic_%d_%d", issue.Number, len(issue.Comments)+1),
			Author:    github.Actor{Login: author},
			Body:      body,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			URL:       fmt.Sprintf("%s#issuecomment-%d", issue.URL, len(issue.Comments)+1),
		})
		issue.CommentCount = len(issue.Comments)
		if createdAt.After(issue.UpdatedAt) {
			issue.UpdatedAt = createdAt
		}
	}
}

// ---------------------------------------------------------------------------
// PR options
// ---------------------------------------------------------------------------

type PROption func(*github.PullRequest, *ScenarioBuilder)

func WithPRState(state string) PROption {
	return func(pr *github.PullRequest, b *ScenarioBuilder) {
		pr.State = state
		switch state {
		case "CLOSED":
			closedAt := pr.UpdatedAt
			pr.ClosedAt = &closedAt
		case "MERGED":
			mergedAt := pr.UpdatedAt
			pr.MergedAt = &mergedAt
			pr.ClosedAt = &mergedAt
		}
	}
}

func WithPRDraft(draft bool) PROption {
	return func(pr *github.PullRequest, b *ScenarioBuilder) {
		pr.IsDraft = draft
	}
}

func WithPRReview(decision string) PROption {
	return func(pr *github.PullRequest, b *ScenarioBuilder) {
		pr.ReviewDecision = decision
	}
}

func WithPRLabels(names ...string) PROption {
	return func(pr *github.PullRequest, b *ScenarioBuilder) {
		pr.Labels = nil
		for _, name := range names {
			color := "ffffff"
			for _, l := range b.labels {
				if l.Name == name {
					color = l.Color
					break
				}
			}
			pr.Labels = append(pr.Labels, github.Label{Name: name, Color: color})
		}
	}
}

func WithPRMilestone(title string) PROption {
	return func(pr *github.PullRequest, b *ScenarioBuilder) {
		for _, m := range b.milestones {
			if m.Title == title {
				pr.Milestone = &github.Milestone{Number: m.Number, Title: m.Title}
				return
			}
		}
	}
}

func WithPRComment(author, body string, age time.Duration) PROption {
	return func(pr *github.PullRequest, b *ScenarioBuilder) {
		createdAt := b.now.Add(-age)
		updatedAt := createdAt
		pr.Comments = append(pr.Comments, github.Comment{
			ID:        fmt.Sprintf("pc_%d_%d", pr.Number, len(pr.Comments)+1),
			Author:    github.Actor{Login: author},
			Body:      body,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
			URL:       fmt.Sprintf("%s#discussion_r%d", pr.URL, len(pr.Comments)+1),
		})
		pr.CommentCount = len(pr.Comments)
		if createdAt.After(pr.UpdatedAt) {
			pr.UpdatedAt = createdAt
		}
	}
}

// ---------------------------------------------------------------------------
// Time evolution: advance the scenario to simulate new activity
// ---------------------------------------------------------------------------

type TimeAdvance struct {
	builder *ScenarioBuilder
	delta   time.Duration
}

func (b *ScenarioBuilder) AdvanceTime(delta time.Duration) *TimeAdvance {
	return &TimeAdvance{builder: b, delta: delta}
}

func (ta *TimeAdvance) NewIssue(title, body string, opts ...IssueOption) *TimeAdvance {
	ta.builder.AddIssue(title, body, 0, opts...)
	issue := &ta.builder.issues[len(ta.builder.issues)-1].Issue
	issue.CreatedAt = ta.builder.now
	issue.UpdatedAt = ta.builder.now
	ta.builder.now = ta.builder.now.Add(ta.delta)
	return ta
}

func (ta *TimeAdvance) NewPR(title, body, headBranch string, opts ...PROption) *TimeAdvance {
	ta.builder.AddPR(title, body, headBranch, 0, opts...)
	pr := &ta.builder.prs[len(ta.builder.prs)-1].PullRequest
	pr.CreatedAt = ta.builder.now
	pr.UpdatedAt = ta.builder.now
	ta.builder.now = ta.builder.now.Add(ta.delta)
	return ta
}

func (ta *TimeAdvance) CommentOnIssue(issueNumber int, author, body string) *TimeAdvance {
	for i := range ta.builder.issues {
		if ta.builder.issues[i].Number == issueNumber {
			c := github.Comment{
				ID:        fmt.Sprintf("ic_%d_%d", issueNumber, len(ta.builder.issues[i].Comments)+1),
				Author:    github.Actor{Login: author},
				Body:      body,
				CreatedAt: ta.builder.now,
				UpdatedAt: ta.builder.now,
				URL:       fmt.Sprintf("%s#issuecomment-%d", ta.builder.issues[i].URL, len(ta.builder.issues[i].Comments)+1),
			}
			ta.builder.issues[i].Comments = append(ta.builder.issues[i].Comments, c)
			ta.builder.issues[i].CommentCount = len(ta.builder.issues[i].Comments)
			ta.builder.issues[i].UpdatedAt = ta.builder.now
			break
		}
	}
	ta.builder.now = ta.builder.now.Add(ta.delta)
	return ta
}

func (ta *TimeAdvance) CommentOnPR(prNumber int, author, body string) *TimeAdvance {
	for i := range ta.builder.prs {
		if ta.builder.prs[i].Number == prNumber {
			c := github.Comment{
				ID:        fmt.Sprintf("pc_%d_%d", prNumber, len(ta.builder.prs[i].Comments)+1),
				Author:    github.Actor{Login: author},
				Body:      body,
				CreatedAt: ta.builder.now,
				UpdatedAt: ta.builder.now,
				URL:       fmt.Sprintf("%s#discussion_r%d", ta.builder.prs[i].URL, len(ta.builder.prs[i].Comments)+1),
			}
			ta.builder.prs[i].Comments = append(ta.builder.prs[i].Comments, c)
			ta.builder.prs[i].CommentCount = len(ta.builder.prs[i].Comments)
			ta.builder.prs[i].UpdatedAt = ta.builder.now
			break
		}
	}
	ta.builder.now = ta.builder.now.Add(ta.delta)
	return ta
}

func (ta *TimeAdvance) CloseIssue(issueNumber int) *TimeAdvance {
	for i := range ta.builder.issues {
		if ta.builder.issues[i].Number == issueNumber {
			ta.builder.issues[i].State = "CLOSED"
			ta.builder.issues[i].ClosedAt = &ta.builder.now
			ta.builder.issues[i].UpdatedAt = ta.builder.now
			break
		}
	}
	ta.builder.now = ta.builder.now.Add(ta.delta)
	return ta
}

func (ta *TimeAdvance) MergePR(prNumber int) *TimeAdvance {
	for i := range ta.builder.prs {
		if ta.builder.prs[i].Number == prNumber {
			ta.builder.prs[i].State = "MERGED"
			ta.builder.prs[i].MergedAt = &ta.builder.now
			ta.builder.prs[i].ClosedAt = &ta.builder.now
			ta.builder.prs[i].UpdatedAt = ta.builder.now
			ta.builder.prs[i].IsDraft = false
			break
		}
	}
	ta.builder.now = ta.builder.now.Add(ta.delta)
	return ta
}

func (ta *TimeAdvance) ClosePR(prNumber int) *TimeAdvance {
	for i := range ta.builder.prs {
		if ta.builder.prs[i].Number == prNumber {
			ta.builder.prs[i].State = "CLOSED"
			ta.builder.prs[i].ClosedAt = &ta.builder.now
			ta.builder.prs[i].UpdatedAt = ta.builder.now
			ta.builder.prs[i].IsDraft = false
			break
		}
	}
	ta.builder.now = ta.builder.now.Add(ta.delta)
	return ta
}

func (ta *TimeAdvance) Build() *Scenario {
	return ta.builder.Build()
}

// FormatAge is a helper for printing human-readable durations.
func FormatAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
}

// Summary returns a human-readable summary of the scenario.
func (s *Scenario) Summary() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Scenario: %d issue(s), %d PR(s)\n", len(s.Issues), len(s.PRs)))
	for _, issue := range s.Issues {
		b.WriteString(fmt.Sprintf("  Issue #%d [%s] %s (%d comment(s))\n",
			issue.Number, issue.State, issue.Title, len(issue.Comments)))
	}
	for _, pr := range s.PRs {
		draft := ""
		if pr.IsDraft {
			draft = " [DRAFT]"
		}
		b.WriteString(fmt.Sprintf("  PR #%d [%s]%s %s (%s → %s, %d comment(s))\n",
			pr.Number, pr.State, draft, pr.Title, pr.HeadRefName, pr.BaseRefName, len(pr.Comments)))
	}
	return b.String()
}
