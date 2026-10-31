package mockserver

import "github.com/tomzxcode/ghx/internal/github"

type ScenarioIssue struct {
	Owner string
	Repo  string
	github.Issue
}

type ScenarioPR struct {
	Owner string
	Repo  string
	github.PullRequest
	// Checks are the individual check contexts the head commit reports. The
	// cached record carries only their count (PullRequest.Checks); this is
	// what a checks detail read lists.
	Checks []github.PRCheck
}

type Scenario struct {
	Issues []ScenarioIssue
	PRs    []ScenarioPR

	// RejectSyntheticCursors makes the server refuse keyset cursors that do not
	// point at an existing test row, so tests can exercise the fallback used
	// when GitHub will not accept a synthesized date seek.
	RejectSyntheticCursors bool
}

type ScenarioOption func(*Scenario)

func WithScenarioIssue(owner, repo string, issue github.Issue) ScenarioOption {
	return func(s *Scenario) {
		s.Issues = append(s.Issues, ScenarioIssue{Owner: owner, Repo: repo, Issue: issue})
	}
}

func WithScenarioPR(owner, repo string, pr github.PullRequest) ScenarioOption {
	return func(s *Scenario) {
		s.PRs = append(s.PRs, ScenarioPR{Owner: owner, Repo: repo, PullRequest: pr})
	}
}

// WithScenarioPRChecks records the individual checks the head commit reports
// for one scenario pull request, and points the rollup's total at them. Apply
// it after the WithScenarioPR that adds that pull request.
func WithScenarioPRChecks(owner, repo string, number int, checks []github.PRCheck) ScenarioOption {
	return func(s *Scenario) {
		for i := range s.PRs {
			pr := &s.PRs[i]
			if pr.Owner != owner || pr.Repo != repo || pr.Number != number {
				continue
			}
			pr.Checks = checks
			if pr.PullRequest.Checks != nil {
				pr.PullRequest.Checks.Total = len(checks)
			}
			return
		}
	}
}

func NewScenario(opts ...ScenarioOption) *Scenario {
	s := &Scenario{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}
