package mockserver

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

// SimulationConfig controls how much data Generate produces and how it is
// distributed over time.
type SimulationConfig struct {
	// Number of GitHub users to simulate.
	NumUsers int
	// Repositories to create. Each entry is "owner/repo".
	Repos []string
	// Duration of simulated history (e.g. 90 days).
	History time.Duration
	// Per-repo counts.
	IssuesPerRepo    int
	PRsPerRepo       int
	CommentsPerIssue int
	CommentsPerPR    int
	// AssigneesPerIssue is the max number of assignees on a single issue (0-3).
	AssigneesPerIssue int
	// AssigneesPerPR is the max number of assignees on a single PR (0-3).
	AssigneesPerPR int
	// LabelsPerItem is the max number of labels on a single issue or PR (0-5).
	LabelsPerItem int
	// Milestones to create per repo.
	MilestonesPerRepo int
	// CloseRate is the fraction [0,1] of issues that get closed.
	CloseRate float64
	// MergeRate is the fraction [0,1] of PRs that get merged.
	MergeRate float64
	// DraftRate is the fraction [0,1] of PRs created as drafts.
	DraftRate float64
	// ReviewRate is the fraction [0,1] of PRs that receive a review decision.
	ReviewRate float64
	// RNG seed for deterministic output.
	Seed int64
	// Now is the reference time; history extends backwards from here.
	// Defaults to time.Now() if zero.
	Now time.Time
	// ActivityBursts controls non-uniform activity. If > 0, this many high-activity
	// bursts are sprinkled across the history. Each burst concentrates events within
	// a short window (e.g. hackathons, release sprints).
	ActivityBursts int
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() SimulationConfig {
	return SimulationConfig{
		NumUsers:          8,
		Repos:             []string{"acme/platform", "acme/frontend", "acme/infrastructure"},
		History:           90 * 24 * time.Hour,
		IssuesPerRepo:     40,
		PRsPerRepo:        30,
		CommentsPerIssue:  4,
		CommentsPerPR:     3,
		AssigneesPerIssue: 2,
		AssigneesPerPR:    2,
		LabelsPerItem:     3,
		MilestonesPerRepo: 4,
		CloseRate:         0.6,
		MergeRate:         0.7,
		DraftRate:         0.15,
		ReviewRate:        0.8,
		Seed:              42,
		ActivityBursts:    3,
	}
}

// SmallConfig returns a minimal config for fast tests.
func SmallConfig() SimulationConfig {
	return SimulationConfig{
		NumUsers:          3,
		Repos:             []string{"acme/testrepo"},
		History:           7 * 24 * time.Hour,
		IssuesPerRepo:     5,
		PRsPerRepo:        3,
		CommentsPerIssue:  2,
		CommentsPerPR:     2,
		AssigneesPerIssue: 1,
		AssigneesPerPR:    1,
		LabelsPerItem:     2,
		MilestonesPerRepo: 2,
		CloseRate:         0.5,
		MergeRate:         0.6,
		DraftRate:         0.1,
		ReviewRate:        0.8,
		Seed:              1,
		ActivityBursts:    0,
	}
}

// ---------------------------------------------------------------------------
// Internal types for the event-based timeline
// ---------------------------------------------------------------------------

type eventType int

const (
	eventCreateIssue eventType = iota
	eventCommentIssue
	eventCloseIssue
	eventCreatePR
	eventCommentPR
	eventReviewPR
	eventMergePR
	eventClosePR
	eventReadyPR
	eventRequestReview
)

type simEvent struct {
	t       time.Time
	typ     eventType
	user    string
	owner   string
	repo    string
	ref     int // issue/PR number
	payload string
	labels  []github.Label
}

type simUser struct {
	login string
}

type simRepo struct {
	owner string
	name  string
}

type simIssue struct {
	number    int
	author    string
	title     string
	body      string
	state     string
	labels    []github.Label
	milestone *github.Milestone
	assignees []github.Actor
	createdAt time.Time
	updatedAt time.Time
	closedAt  *time.Time
	comments  []github.Comment
}

type simPR struct {
	number         int
	author         string
	title          string
	body           string
	state          string
	isDraft        bool
	reviewDecision string
	labels         []github.Label
	milestone      *github.Milestone
	assignees      []github.Actor
	baseRefName    string
	headRefName    string
	createdAt      time.Time
	updatedAt      time.Time
	mergedAt       *time.Time
	closedAt       *time.Time
	comments       []github.Comment
	additions      int
	deletions      int
	reviews        []github.PRReview
	timeline       []github.TimelineEvent
}

// ---------------------------------------------------------------------------
// Generator
// ---------------------------------------------------------------------------

// Generate builds a Scenario from the given config. All events are placed on a
// timeline spanning [now - history, now] and processed in chronological order so
// that createdAt/updatedAt/comment timestamps are internally consistent.
func Generate(cfg SimulationConfig) *Scenario {
	if cfg.Now.IsZero() {
		cfg.Now = time.Now()
	}
	rng := rand.New(rand.NewSource(cfg.Seed))

	users := make([]simUser, cfg.NumUsers)
	for i := range users {
		users[i] = simUser{login: fmt.Sprintf("user-%03d", i+1)}
	}

	repos := make([]simRepo, len(cfg.Repos))
	for i, r := range cfg.Repos {
		parts := strings.SplitN(r, "/", 2)
		repos[i] = simRepo{owner: parts[0], name: parts[1]}
	}

	labelPool := []github.Label{
		{Name: "bug", Color: "d73a4a"},
		{Name: "enhancement", Color: "a2eeef"},
		{Name: "documentation", Color: "0075ca"},
		{Name: "good first issue", Color: "7057ff"},
		{Name: "help wanted", Color: "008672"},
		{Name: "question", Color: "d876e3"},
		{Name: "wontfix", Color: "ffffff"},
		{Name: "invalid", Color: "e4e669"},
		{Name: "duplicate", Color: "cfd3d7"},
		{Name: "p0", Color: "b60205"},
		{Name: "p1", Color: "d93f0b"},
		{Name: "p2", Color: "fbca04"},
		{Name: "p3", Color: "0e8a16"},
		{Name: "security", Color: "ee0701"},
		{Name: "performance", Color: "1d76db"},
		{Name: "refactor", Color: "bfdadc"},
		{Name: "testing", Color: "fef2c0"},
		{Name: "dependencies", Color: "0366d6"},
		{Name: "ci", Color: "e99695"},
		{Name: "breaking", Color: "b60205"},
	}

	issueTitles := []string{
		"Fix crash when %s is nil",
		"Add support for %s",
		"Memory leak in %s handler",
		"Improve error messages for %s",
		"Race condition in %s processing",
		"Update documentation for %s",
		"Deprecate legacy %s API",
		"Add logging to %s module",
		"Performance regression in %s",
		"Handle edge case in %s validation",
		"Refactor %s for readability",
		"Add integration tests for %s",
		"Timeout handling in %s client",
		"Missing input validation for %s",
		"Inconsistent behavior in %s across platforms",
		"Migrate %s to new API version",
		"Add caching layer for %s",
		"Fix flaky test in %s",
		"Implement retry logic for %s",
		"Remove deprecated %s usage",
		"Add health check for %s",
		"Fix pagination in %s endpoint",
		"Normalize %s response format",
		"Add rate limiting to %s",
		"Investigate %s latency spike",
	}
	issueBodies := []string{
		"When running under load, %s occasionally returns an unexpected error. This was observed in production logs from the last deployment.",
		"We need to extend the current implementation to support %s. This is a blocker for the upcoming release.",
		"After the last refactor, %s has been consuming increasing amounts of memory. Profiling points to unbounded growth in the internal buffer.",
		"The current error messages from %s are generic and unhelpful. Users report difficulty diagnosing issues.",
		"Two goroutines accessing %s concurrently can cause a data race, detected by the race detector.",
		"The docs for %s have not been updated since the v2 migration. Several code examples no longer compile.",
		"The legacy %s API is confusing and poorly tested. We should migrate consumers and deprecate it.",
		"Adding structured logging to %s would make debugging production issues much easier.",
		"Recent benchmarks show %s is 3x slower than the previous version. Bisect points to commit abc123.",
		"The %s validation function does not handle empty strings, unicode, or very long inputs correctly.",
		"The %s module has grown organically and needs a cleanup pass to improve readability and maintainability.",
		"%s currently lacks integration tests. We should add at least basic coverage before the next release.",
		"%s client does not respect context deadlines, leading to hung requests when the upstream is slow.",
		"%s accepts user input without proper validation, potentially leading to injection or crash.",
		"%s behaves differently on Linux and macOS, likely due to path handling assumptions.",
		"%s uses the v1 REST API which is being sunset. We need to migrate to the v2 GraphQL API.",
		"Repeated calls to %s are slow due to lack of caching. A simple LRU cache would help.",
		"The test for %s fails intermittently on CI, likely due to timing sensitivity or shared state.",
		"%s does not retry on transient failures. Adding exponential backoff would improve resilience.",
		"%s still imports the deprecated package. We should update to the recommended replacement.",
	}
	commentBodies := []string{
		"I can reproduce this locally. Happens every time I run the test suite.",
		"Working on a fix, should have a PR up by end of day.",
		"LGTM, merging.",
		"Could you add a test case for this?",
		"This is a duplicate of #%d, closing.",
		"Confirmed this fixes the issue on our staging environment.",
		" assigning to myself, will investigate.",
		"Bumped priority since this is affecting production.",
		"Adding to the v2.0 milestone since we need this before release.",
		"I've written a reproduction script, see attached.",
		"This was introduced in commit %s. Tagging author for context.",
		"Tested locally, no longer seeing the issue.",
		"Good catch! I didn't consider that edge case.",
		"Moving to the next milestone since we ran out of time.",
		"Can we get a regression test for this?",
		"Reviewed the fix, left a few comments.",
		"Pushed an update addressing review feedback.",
		"This is working as designed, but we should document the behavior.",
		"Is there a workaround we can share with users in the meantime?",
		"Adding the security label since this could be exploited.",
	}
	prTitles := []string{
		"Fix %s crash on nil input",
		"Add %s support",
		"Fix memory leak in %s",
		"Improve %s error handling",
		"Refactor %s module",
		"Add tests for %s",
		"Update %s dependencies",
		"Optimize %s performance",
		"Add retry logic to %s",
		"Migrate %s to v2 API",
		"Add caching for %s",
		"Fix %s flaky test",
		"Implement %s rate limiting",
		"Add health check for %s",
		"Fix %s pagination bug",
		"Normalize %s responses",
		"Add structured logging to %s",
		"Handle %s edge cases",
		"Deprecate old %s API",
		"Add input validation for %s",
	}
	prBodies := []string{
		"Fixes #%d.",
		"Closes #%d. See the issue for context.",
		"This PR addresses the %s performance regression by introducing a bounded buffer.",
		"Refactors %s to use the builder pattern for better readability.",
		"Adds comprehensive test coverage for %s, including edge cases.",
		"Bumps %s to the latest version to pick up security fixes.",
		"Implements exponential backoff with jitter for %s retries.",
		"Migrates %s from the deprecated v1 API to v2.",
		"Adds an LRU cache for %s lookups, reducing p99 latency by ~40%%.",
		"Fixes the flaky test by properly isolating shared state in %s.",
	}
	headBranchPrefixes := []string{
		"fix", "feat", "refactor", "chore", "docs", "test", "perf", "security",
	}
	moduleNames := []string{
		"auth", "cache", "config", "database", "handler", "middleware",
		"parser", "queue", "router", "scheduler", "serializer", "service",
		"storage", "validator", "worker", "logger", "metrics", "client",
	}

	pickString := func(pool []string) string {
		return pool[rng.Intn(len(pool))]
	}

	pickUser := func() string {
		return users[rng.Intn(len(users))].login
	}

	pickLabels := func() []github.Label {
		n := rng.Intn(cfg.LabelsPerItem + 1)
		if n == 0 || len(labelPool) == 0 {
			return nil
		}
		perm := rng.Perm(len(labelPool))
		result := make([]github.Label, min(n, len(labelPool)))
		for i := range result {
			result[i] = labelPool[perm[i]]
		}
		return result
	}

	pickAssignees := func(maxAssignees int) []github.Actor {
		n := rng.Intn(maxAssignees + 1)
		if n == 0 {
			return nil
		}
		seen := map[string]bool{}
		var result []github.Actor
		for len(result) < n {
			u := pickUser()
			if !seen[u] {
				seen[u] = true
				result = append(result, github.Actor{Login: u})
			}
		}
		return result
	}

	// Generate burst windows for concentrated activity.
	startTime := cfg.Now.Add(-cfg.History)
	totalDuration := cfg.History

	burstWindows := make([]time.Duration, cfg.ActivityBursts)
	for i := range burstWindows {
		burstWindows[i] = time.Duration(rng.Int63n(int64(totalDuration)))
	}
	sort.Slice(burstWindows, func(i, j int) bool { return burstWindows[i] < burstWindows[j] })

	// weightAt returns a relative activity weight for a point in history.
	// Points near a burst window get higher weight.
	weightAt := func(offset time.Duration) float64 {
		w := 1.0
		for _, bw := range burstWindows {
			dist := math.Abs(float64(offset - bw))
			if dist < float64(6*time.Hour) {
				w += 3.0 * (1.0 - dist/float64(6*time.Hour))
			}
		}
		return w
	}

	// sampleTime picks a random time in [startTime, cfg.Now) weighted by activity.
	sampleTime := func() time.Time {
		for {
			offset := time.Duration(rng.Int63n(int64(totalDuration)))
			if rng.Float64() < weightAt(offset)/4.0 {
				return startTime.Add(offset)
			}
		}
	}

	// Generate milestones per repo.
	type repoMilestones struct {
		milestones []github.Milestone
	}
	repoMS := make(map[string]repoMilestones)
	for _, repo := range repos {
		key := repo.owner + "/" + repo.name
		ms := make([]github.Milestone, cfg.MilestonesPerRepo)
		for i := range ms {
			ms[i] = github.Milestone{
				Number: i + 1,
				Title:  fmt.Sprintf("v%d.%d", (i+1)/2+1, (i+1)%2),
			}
		}
		repoMS[key] = repoMilestones{milestones: ms}
	}

	pickMilestone := func(repo simRepo) *github.Milestone {
		key := repo.owner + "/" + repo.name
		ms := repoMS[key].milestones
		if len(ms) == 0 || rng.Float64() < 0.35 {
			return nil
		}
		m := ms[rng.Intn(len(ms))]
		return &m
	}

	// ---- Build events per repo ----

	var events []simEvent
	nextNum := 0
	nextNumFn := func() int {
		nextNum++
		return nextNum
	}

	expandTemplate := func(tpl string, data ...string) string {
		s := tpl
		for _, d := range data {
			s = strings.Replace(s, "%s", d, 1)
		}
		return s
	}

	expandTemplateInt := func(tpl string, n int) string {
		s := tpl
		return strings.Replace(s, "%d", fmt.Sprintf("%d", n), 1)
	}

	for _, repo := range repos {
		// Create issues.
		issues := make([]int, cfg.IssuesPerRepo)
		for i := 0; i < cfg.IssuesPerRepo; i++ {
			num := nextNumFn()
			issues[i] = num
			mod := pickString(moduleNames)
			title := fmt.Sprintf(pickString(issueTitles), mod)
			body := expandTemplate(pickString(issueBodies), mod)

			created := sampleTime()

			events = append(events, simEvent{
				t:       created,
				typ:     eventCreateIssue,
				user:    pickUser(),
				owner:   repo.owner,
				repo:    repo.name,
				ref:     num,
				payload: title + "\n" + body,
				labels:  pickLabels(),
			})

			for j := 0; j < cfg.CommentsPerIssue; j++ {
				commentDelay := time.Duration(rng.Int63n(int64(48 * time.Hour)))
				events = append(events, simEvent{
					t:       created.Add(commentDelay + time.Minute),
					typ:     eventCommentIssue,
					user:    pickUser(),
					owner:   repo.owner,
					repo:    repo.name,
					ref:     num,
					payload: expandTemplateInt(pickString(commentBodies), issues[rng.Intn(len(issues))]),
				})
			}

			if rng.Float64() < cfg.CloseRate {
				closeDelay := time.Duration(rng.Int63n(int64(7 * 24 * time.Hour)))
				events = append(events, simEvent{
					t:     created.Add(closeDelay + time.Hour),
					typ:   eventCloseIssue,
					user:  pickUser(),
					owner: repo.owner,
					repo:  repo.name,
					ref:   num,
				})
			}
		}

		// Create PRs.
		for i := 0; i < cfg.PRsPerRepo; i++ {
			num := nextNumFn()
			mod := pickString(moduleNames)
			title := fmt.Sprintf(pickString(prTitles), mod)
			body := expandTemplate(pickString(prBodies), mod)
			if strings.Contains(body, "%d") {
				body = expandTemplateInt(body, issues[rng.Intn(len(issues))])
			}

			created := sampleTime()

			isDraft := rng.Float64() < cfg.DraftRate
			author := pickUser()

			events = append(events, simEvent{
				t:       created,
				typ:     eventCreatePR,
				user:    author,
				owner:   repo.owner,
				repo:    repo.name,
				ref:     num,
				payload: title + "\n" + body,
				labels:  pickLabels(),
			})

			if isDraft && rng.Float64() < 0.7 {
				events[len(events)-1].payload += "\n[draft]"
				// Most drafts become ready for review within a couple of
				// days; the rest stay in draft.
				readyDelay := time.Duration(1+rng.Int63n(47)) * time.Hour
				events = append(events, simEvent{
					t:     created.Add(readyDelay),
					typ:   eventReadyPR,
					user:  author,
					owner: repo.owner,
					repo:  repo.name,
					ref:   num,
				})
			}

			for j := 0; j < cfg.CommentsPerPR; j++ {
				commentDelay := time.Duration(rng.Int63n(int64(24 * time.Hour)))
				events = append(events, simEvent{
					t:       created.Add(commentDelay + time.Minute),
					typ:     eventCommentPR,
					user:    pickUser(),
					owner:   repo.owner,
					repo:    repo.name,
					ref:     num,
					payload: expandTemplateInt(pickString(commentBodies), issues[rng.Intn(len(issues))]),
				})
			}

			if rng.Float64() < cfg.ReviewRate {
				// A review request precedes the review itself, and the
				// requested reviewer is the one who reviews.
				reviewer := pickUser()
				requestDelay := time.Duration(rng.Int63n(int64(12*time.Hour))) + 30*time.Minute
				events = append(events, simEvent{
					t:     created.Add(requestDelay),
					typ:   eventRequestReview,
					user:  reviewer,
					owner: repo.owner,
					repo:  repo.name,
					ref:   num,
				})
				reviewDelay := time.Duration(rng.Int63n(int64(48 * time.Hour)))
				decisions := []string{"APPROVED", "CHANGES_REQUESTED", "APPROVED", "APPROVED"}
				events = append(events, simEvent{
					t:       created.Add(requestDelay + reviewDelay + 30*time.Minute),
					typ:     eventReviewPR,
					user:    reviewer,
					owner:   repo.owner,
					repo:    repo.name,
					ref:     num,
					payload: decisions[rng.Intn(len(decisions))],
				})
			}

			if rng.Float64() < cfg.MergeRate {
				mergeDelay := time.Duration(rng.Int63n(int64(72 * time.Hour)))
				events = append(events, simEvent{
					t:     created.Add(mergeDelay + 2*time.Hour),
					typ:   eventMergePR,
					user:  pickUser(),
					owner: repo.owner,
					repo:  repo.name,
					ref:   num,
				})
			} else if rng.Float64() < 0.2 {
				closeDelay := time.Duration(rng.Int63n(int64(72 * time.Hour)))
				events = append(events, simEvent{
					t:     created.Add(closeDelay + 2*time.Hour),
					typ:   eventClosePR,
					user:  pickUser(),
					owner: repo.owner,
					repo:  repo.name,
					ref:   num,
				})
			}
		}
	}

	// Sort all events chronologically.
	sort.Slice(events, func(i, j int) bool { return events[i].t.Before(events[j].t) })

	// ---- Process events into scenario data ----

	issueMap := map[string]*simIssue{} // key: "owner/repo/number"
	prMap := map[string]*simPR{}
	commentCounter := map[string]int{}

	issueKey := func(owner, repo string, num int) string {
		return fmt.Sprintf("%s/%s/%d", owner, repo, num)
	}
	prKey := issueKey

	for _, ev := range events {
		switch ev.typ {
		case eventCreateIssue:
			parts := strings.SplitN(ev.payload, "\n", 2)
			title := parts[0]
			body := ""
			if len(parts) > 1 {
				body = parts[1]
			}
			si := &simIssue{
				number:    ev.ref,
				author:    ev.user,
				title:     title,
				body:      body,
				state:     "OPEN",
				labels:    ev.labels,
				milestone: pickMilestone(simRepo{owner: ev.owner, name: ev.repo}),
				assignees: pickAssignees(cfg.AssigneesPerIssue),
				createdAt: ev.t,
				updatedAt: ev.t,
			}
			issueMap[issueKey(ev.owner, ev.repo, ev.ref)] = si

		case eventCommentIssue:
			k := issueKey(ev.owner, ev.repo, ev.ref)
			si, ok := issueMap[k]
			if !ok {
				continue
			}
			commentCounter[k]++
			si.comments = append(si.comments, github.Comment{
				ID:        fmt.Sprintf("IC_%d_%d", ev.ref, commentCounter[k]),
				Author:    github.Actor{Login: ev.user},
				Body:      ev.payload,
				CreatedAt: ev.t,
				UpdatedAt: ev.t,
				URL:       fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-%d", ev.owner, ev.repo, ev.ref, commentCounter[k]),
			})
			si.updatedAt = ev.t

		case eventCloseIssue:
			k := issueKey(ev.owner, ev.repo, ev.ref)
			si, ok := issueMap[k]
			if !ok {
				continue
			}
			si.state = "CLOSED"
			closedAt := ev.t
			si.closedAt = &closedAt
			si.updatedAt = ev.t

		case eventCreatePR:
			parts := strings.SplitN(ev.payload, "\n", 2)
			title := parts[0]
			body := ""
			if len(parts) > 1 {
				body = parts[1]
			}
			isDraft := strings.Contains(ev.payload, "[draft]")
			mod := pickString(moduleNames)

			sp := &simPR{
				number:         ev.ref,
				author:         ev.user,
				title:          title,
				body:           body,
				state:          "OPEN",
				isDraft:        isDraft,
				reviewDecision: "",
				labels:         ev.labels,
				milestone:      pickMilestone(simRepo{owner: ev.owner, name: ev.repo}),
				assignees:      pickAssignees(cfg.AssigneesPerPR),
				baseRefName:    "main",
				headRefName:    fmt.Sprintf("%s/%s-%d", pickString(headBranchPrefixes), mod, ev.ref),
				createdAt:      ev.t,
				updatedAt:      ev.t,
				additions:      10 + rng.Intn(800),
				deletions:      rng.Intn(300),
			}
			prMap[prKey(ev.owner, ev.repo, ev.ref)] = sp

		case eventCommentPR:
			k := prKey(ev.owner, ev.repo, ev.ref)
			sp, ok := prMap[k]
			if !ok {
				continue
			}
			commentCounter[k]++
			sp.comments = append(sp.comments, github.Comment{
				ID:        fmt.Sprintf("PC_%d_%d", ev.ref, commentCounter[k]),
				Author:    github.Actor{Login: ev.user},
				Body:      ev.payload,
				CreatedAt: ev.t,
				UpdatedAt: ev.t,
				URL:       fmt.Sprintf("https://github.com/%s/%s/pull/%d#discussion_r%d", ev.owner, ev.repo, ev.ref, commentCounter[k]),
			})
			sp.updatedAt = ev.t

		case eventReviewPR:
			k := prKey(ev.owner, ev.repo, ev.ref)
			sp, ok := prMap[k]
			if !ok {
				continue
			}
			sp.reviewDecision = ev.payload
			sp.reviews = append(sp.reviews, github.PRReview{
				Author:      github.Actor{Login: ev.user},
				State:       ev.payload,
				SubmittedAt: ev.t,
			})
			// A changes-requested review is followed by a re-request, which
			// GitHub records as another review-requested event.
			if ev.payload == "CHANGES_REQUESTED" {
				sp.timeline = append(sp.timeline, github.TimelineEvent{
					Kind:              github.TimelineReviewRequested,
					Actor:             github.Actor{Login: ev.user},
					RequestedReviewer: github.Actor{Login: ev.user},
					CreatedAt:         ev.t.Add(time.Hour),
				})
			}
			sp.updatedAt = ev.t

		case eventReadyPR:
			k := prKey(ev.owner, ev.repo, ev.ref)
			sp, ok := prMap[k]
			if !ok {
				continue
			}
			if sp.state != "OPEN" {
				continue // merged or closed while still draft
			}
			sp.isDraft = false
			sp.timeline = append(sp.timeline, github.TimelineEvent{
				Kind:      github.TimelineReadyForReview,
				Actor:     github.Actor{Login: ev.user},
				CreatedAt: ev.t,
			})
			sp.updatedAt = ev.t

		case eventRequestReview:
			k := prKey(ev.owner, ev.repo, ev.ref)
			sp, ok := prMap[k]
			if !ok {
				continue
			}
			sp.timeline = append(sp.timeline, github.TimelineEvent{
				Kind:              github.TimelineReviewRequested,
				Actor:             github.Actor{Login: ev.user},
				RequestedReviewer: github.Actor{Login: ev.user},
				CreatedAt:         ev.t,
			})
			sp.updatedAt = ev.t

		case eventMergePR:
			k := prKey(ev.owner, ev.repo, ev.ref)
			sp, ok := prMap[k]
			if !ok {
				continue
			}
			sp.state = "MERGED"
			mergedAt := ev.t
			sp.mergedAt = &mergedAt
			sp.closedAt = &mergedAt
			sp.updatedAt = ev.t
			sp.isDraft = false

		case eventClosePR:
			k := prKey(ev.owner, ev.repo, ev.ref)
			sp, ok := prMap[k]
			if !ok {
				continue
			}
			sp.state = "CLOSED"
			closedAt := ev.t
			sp.closedAt = &closedAt
			sp.updatedAt = ev.t
			sp.isDraft = false
		}
	}

	// ---- Convert to Scenario ----

	scenario := &Scenario{}

	for k, si := range issueMap {
		parts := strings.SplitN(k, "/", 3)
		if len(parts) != 3 {
			continue
		}
		issue := github.Issue{
			Number:       si.number,
			Title:        si.title,
			State:        si.state,
			Author:       github.Actor{Login: si.author},
			Labels:       si.labels,
			Milestone:    si.milestone,
			Assignees:    si.assignees,
			CreatedAt:    si.createdAt,
			UpdatedAt:    si.updatedAt,
			ClosedAt:     si.closedAt,
			URL:          fmt.Sprintf("https://github.com/%s/%s/issues/%d", parts[0], parts[1], si.number),
			Body:         si.body,
			CommentCount: len(si.comments),
			Comments:     si.comments,
		}
		scenario.Issues = append(scenario.Issues, ScenarioIssue{
			Owner: parts[0],
			Repo:  parts[1],
			Issue: issue,
		})
	}

	for k, sp := range prMap {
		parts := strings.SplitN(k, "/", 3)
		if len(parts) != 3 {
			continue
		}
		pr := github.PullRequest{
			Number:         sp.number,
			Title:          sp.title,
			State:          sp.state,
			IsDraft:        sp.isDraft,
			ReviewDecision: sp.reviewDecision,
			Author:         github.Actor{Login: sp.author},
			Labels:         sp.labels,
			Milestone:      sp.milestone,
			Assignees:      sp.assignees,
			BaseRefName:    sp.baseRefName,
			HeadRefName:    sp.headRefName,
			CreatedAt:      sp.createdAt,
			UpdatedAt:      sp.updatedAt,
			MergedAt:       sp.mergedAt,
			ClosedAt:       sp.closedAt,
			URL:            fmt.Sprintf("https://github.com/%s/%s/pull/%d", parts[0], parts[1], sp.number),
			Body:           sp.body,
			CommentCount:   len(sp.comments),
			Comments:       sp.comments,
			Additions:      sp.additions,
			Deletions:      sp.deletions,
			Reviews:        sp.reviews,
			Timeline:       sp.timeline,
		}
		scenario.PRs = append(scenario.PRs, ScenarioPR{
			Owner:       parts[0],
			Repo:        parts[1],
			PullRequest: pr,
		})
	}

	return scenario
}

// SimulationStats returns a human-readable summary of what Generate would produce
// for the given config, without actually generating the data.
func SimulationStats(cfg SimulationConfig) string {
	totalIssues := cfg.IssuesPerRepo * len(cfg.Repos)
	totalPRs := cfg.PRsPerRepo * len(cfg.Repos)
	totalIssueComments := totalIssues * cfg.CommentsPerIssue
	totalPRComments := totalPRs * cfg.CommentsPerPR
	totalClosed := int(float64(totalIssues) * cfg.CloseRate)
	totalMerged := int(float64(totalPRs) * cfg.MergeRate)
	totalDrafts := int(float64(totalPRs) * cfg.DraftRate)
	totalReviewed := int(float64(totalPRs) * cfg.ReviewRate)
	totalEvents := totalIssues + totalIssueComments + totalClosed + totalPRs + totalPRComments + totalReviewed + totalMerged

	var b strings.Builder
	fmt.Fprintf(&b, "Simulation Config:\n")
	fmt.Fprintf(&b, "  Users:              %d\n", cfg.NumUsers)
	fmt.Fprintf(&b, "  Repositories:       %d (%s)\n", len(cfg.Repos), strings.Join(cfg.Repos, ", "))
	fmt.Fprintf(&b, "  History:            %s\n", FormatAge(cfg.History))
	fmt.Fprintf(&b, "  Activity bursts:    %d\n", cfg.ActivityBursts)
	fmt.Fprintf(&b, "Per-repo:\n")
	fmt.Fprintf(&b, "  Issues:             %d\n", cfg.IssuesPerRepo)
	fmt.Fprintf(&b, "  PRs:                %d\n", cfg.PRsPerRepo)
	fmt.Fprintf(&b, "  Comments/issue:     %d\n", cfg.CommentsPerIssue)
	fmt.Fprintf(&b, "  Comments/PR:        %d\n", cfg.CommentsPerPR)
	fmt.Fprintf(&b, "  Milestones:         %d\n", cfg.MilestonesPerRepo)
	fmt.Fprintf(&b, "Totals:\n")
	fmt.Fprintf(&b, "  Issues:             %d (%d closed)\n", totalIssues, totalClosed)
	fmt.Fprintf(&b, "  PRs:                %d (%d merged, %d draft, %d reviewed)\n", totalPRs, totalMerged, totalDrafts, totalReviewed)
	fmt.Fprintf(&b, "  Issue comments:     %d\n", totalIssueComments)
	fmt.Fprintf(&b, "  PR comments:        %d\n", totalPRComments)
	fmt.Fprintf(&b, "  Total events:       %d\n", totalEvents)
	return b.String()
}
