package mockserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/tomzxcode/ghx/internal/github"
)

func postGQL(t *testing.T, url string, query string, variables map[string]interface{}) gqlResponse {
	t.Helper()
	body, _ := json.Marshal(gqlRequest{Query: query, Variables: variables})
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Authorization", "bearer test-token")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("executing request: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result gqlResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parsing response: %v\nbody: %s", err, string(raw))
	}
	return result
}

func sampleScenario() *Scenario {
	return NewScenarioBuilder("acme", "myproject").
		WithSeed(42).
		AddIssue("Bug: crash on start", "App crashes on startup", 10*24*time.Hour,
			WithIssueState("CLOSED"),
			WithIssueAssignee("bob"),
			WithIssueLabels("bug", "p0"),
			WithIssueComment("alice", "Reproduced.", 9*24*time.Hour),
			WithIssueComment("bob", "Fixed.", 8*24*time.Hour),
		).
		AddIssue("Feature: dark mode", "Add dark mode", 5*24*time.Hour,
			WithIssueLabels("enhancement"),
			WithIssueComment("carol", "Working on it.", 4*24*time.Hour),
		).
		AddIssue("Docs: update README", "README is outdated", 2*24*time.Hour,
			WithIssueLabels("documentation"),
		).
		AddPR("Fix crash on start", "Fixes the crash.", "fix/crash", 8*24*time.Hour,
			WithPRState("MERGED"),
			WithPRReview("APPROVED"),
			WithPRComment("alice", "LGTM.", 8*24*time.Hour),
		).
		AddPR("Add dark mode", "Adds dark theme.", "feat/dark-mode", 3*24*time.Hour,
			WithPRLabels("enhancement"),
			WithPRComment("bob", "Looking good!", 2*24*time.Hour),
		).
		AddPR("WIP: refactor auth", "Refactoring.", "refactor/auth", 1*24*time.Hour,
			WithPRDraft(true),
		).
		Build()
}

func TestGetIssue(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($owner: String!, $repo: String!, $number: Int!) {
	  repository(owner: $owner, name: $repo) {
	    issue(number: $number) {
	      number title state
	      author { login }
	      comments(first: 100) {
	        nodes { id author { login } body createdAt updatedAt url }
	      }
	    }
	  }
	}`, map[string]interface{}{
		"owner":  "acme",
		"repo":   "myproject",
		"number": 1,
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", gql.Errors)
	}

	raw, _ := json.Marshal(gql.Data)
	var result struct {
		Repository struct {
			Issue struct {
				Number   int                    `json:"number"`
				Title    string                 `json:"title"`
				State    string                 `json:"state"`
				Author   struct{ Login string } `json:"author"`
				Comments struct {
					Nodes []struct {
						Body string `json:"body"`
					} `json:"nodes"`
				} `json:"comments"`
			} `json:"issue"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	issue := result.Repository.Issue
	if issue.Number != 1 {
		t.Errorf("Number = %d, want 1", issue.Number)
	}
	if issue.Title != "Bug: crash on start" {
		t.Errorf("Title = %q, want 'Bug: crash on start'", issue.Title)
	}
	if issue.State != "CLOSED" {
		t.Errorf("State = %q, want CLOSED", issue.State)
	}
	if issue.Author.Login == "" {
		t.Errorf("Author is empty, expected a user")
	}
	if len(issue.Comments.Nodes) != 2 {
		t.Errorf("Comments = %d, want 2", len(issue.Comments.Nodes))
	}
}

func TestGetIssue_NotFound(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($owner: String!, $repo: String!, $number: Int!) {
	  repository(owner: $owner, name: $repo) {
	    issue(number: $number) {
	      number
	    }
	  }
	}`, map[string]interface{}{
		"owner":  "acme",
		"repo":   "myproject",
		"number": 999,
	})

	if len(gql.Errors) == 0 {
		t.Fatal("expected error for missing issue")
	}
}

func TestGetPR(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($owner: String!, $repo: String!, $number: Int!) {
	  repository(owner: $owner, name: $repo) {
	    pullRequest(number: $number) {
	      number title state isDraft reviewDecision
	      baseRefName headRefName
	      comments(first: 100) {
	        nodes { id author { login } body }
	      }
	    }
	  }
	}`, map[string]interface{}{
		"owner":  "acme",
		"repo":   "myproject",
		"number": 4,
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", gql.Errors)
	}

	raw, _ := json.Marshal(gql.Data)
	var result struct {
		Repository struct {
			PullRequest struct {
				Number         int    `json:"number"`
				Title          string `json:"title"`
				State          string `json:"state"`
				IsDraft        bool   `json:"isDraft"`
				ReviewDecision string `json:"reviewDecision"`
				BaseRefName    string `json:"baseRefName"`
				HeadRefName    string `json:"headRefName"`
				Comments       struct {
					Nodes []struct{ Body string } `json:"nodes"`
				} `json:"comments"`
			} `json:"pullRequest"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse: %v", err)
	}

	pr := result.Repository.PullRequest
	if pr.Number != 4 {
		t.Errorf("Number = %d, want 4", pr.Number)
	}
	if pr.State != "MERGED" {
		t.Errorf("State = %q, want MERGED", pr.State)
	}
	if pr.IsDraft {
		t.Error("IsDraft should be false")
	}
	if pr.ReviewDecision != "APPROVED" {
		t.Errorf("ReviewDecision = %q, want APPROVED", pr.ReviewDecision)
	}
	if pr.BaseRefName != "main" {
		t.Errorf("BaseRefName = %q, want main", pr.BaseRefName)
	}
	if pr.HeadRefName != "fix/crash" {
		t.Errorf("HeadRefName = %q, want fix/crash", pr.HeadRefName)
	}
	if len(pr.Comments.Nodes) != 1 {
		t.Errorf("Comments = %d, want 1", len(pr.Comments.Nodes))
	}
}

func TestListIssues(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($owner: String!, $repo: String!, $first: Int!, $states: [IssueState!]) {
	  repository(owner: $owner, name: $repo) {
	    issues(first: $first, states: $states, orderBy: {field: UPDATED_AT, direction: DESC}) {
	      pageInfo { hasNextPage endCursor }
	      nodes { number title state }
	    }
	  }
	}`, map[string]interface{}{
		"owner":  "acme",
		"repo":   "myproject",
		"first":  10,
		"states": []string{"OPEN", "CLOSED"},
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", gql.Errors)
	}

	raw, _ := json.Marshal(gql.Data)
	var result struct {
		Repository struct {
			Issues struct {
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
				Nodes []struct {
					Number int    `json:"number"`
					Title  string `json:"title"`
					State  string `json:"state"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(result.Repository.Issues.Nodes) != 3 {
		t.Errorf("got %d issues, want 3", len(result.Repository.Issues.Nodes))
	}
	if result.Repository.Issues.PageInfo.HasNextPage {
		t.Error("should not have next page")
	}
}

func TestListIssues_FilterState(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($owner: String!, $repo: String!, $first: Int!, $states: [IssueState!]) {
	  repository(owner: $owner, name: $repo) {
	    issues(first: $first, states: $states, orderBy: {field: UPDATED_AT, direction: DESC}) {
	      nodes { number state }
	    }
	  }
	}`, map[string]interface{}{
		"owner":  "acme",
		"repo":   "myproject",
		"first":  10,
		"states": []string{"OPEN"},
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", gql.Errors)
	}

	raw, _ := json.Marshal(gql.Data)
	var result struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					Number int    `json:"number"`
					State  string `json:"state"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(result.Repository.Issues.Nodes) != 2 {
		t.Errorf("got %d open issues, want 2", len(result.Repository.Issues.Nodes))
	}
	for _, n := range result.Repository.Issues.Nodes {
		if n.State != "OPEN" {
			t.Errorf("issue #%d has state %q, want OPEN", n.Number, n.State)
		}
	}
}

func TestFetchAllIssues(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($owner: String!, $repo: String!, $after: String, $since: DateTime) {
	  repository(owner: $owner, name: $repo) {
	    issues(first: 100, states: [OPEN, CLOSED], filterBy: {since: $since}, after: $after, orderBy: {field: UPDATED_AT, direction: DESC}) {
	      totalCount
	      pageInfo { hasNextPage endCursor }
	      nodes {
	        number title state
	        comments(first: 100) {
	          totalCount
	          nodes { id author { login } body createdAt updatedAt url }
	        }
	      }
	    }
	  }
	}`, map[string]interface{}{
		"owner": "acme",
		"repo":  "myproject",
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", gql.Errors)
	}

	raw, _ := json.Marshal(gql.Data)
	var result struct {
		Repository struct {
			Issues struct {
				TotalCount int `json:"totalCount"`
				Nodes      []struct {
					Number   int `json:"number"`
					Comments struct {
						TotalCount int `json:"totalCount"`
					} `json:"comments"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if result.Repository.Issues.TotalCount != 3 {
		t.Errorf("issues totalCount = %d, want 3", result.Repository.Issues.TotalCount)
	}

	if len(result.Repository.Issues.Nodes) != 3 {
		t.Errorf("got %d issues, want 3", len(result.Repository.Issues.Nodes))
	}

	for _, n := range result.Repository.Issues.Nodes {
		if n.Number == 1 && n.Comments.TotalCount != 2 {
			t.Errorf("issue #1 comments = %d, want 2", n.Comments.TotalCount)
		}
	}
}

func TestSearchIssues(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($query: String!, $first: Int!) {
	  search(query: $query, type: ISSUE, first: $first) {
	    pageInfo { hasNextPage }
	    nodes {
	      __typename
	      ... on Issue {
	        number title state
	      }
	    }
	  }
	}`, map[string]interface{}{
		"query": "repo:acme/myproject is:issue is:open",
		"first": 10,
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", gql.Errors)
	}

	raw, _ := json.Marshal(gql.Data)
	var result struct {
		Search struct {
			Nodes []struct {
				Typename string `json:"__typename"`
				Number   int    `json:"number"`
				State    string `json:"state"`
			} `json:"nodes"`
		} `json:"search"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(result.Search.Nodes) != 2 {
		t.Errorf("got %d search results, want 2", len(result.Search.Nodes))
	}
	for _, n := range result.Search.Nodes {
		if n.Typename != "Issue" {
			t.Errorf("typename = %q, want Issue", n.Typename)
		}
	}
}

func TestSearchPRs(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($query: String!, $first: Int!) {
	  search(query: $query, type: ISSUE, first: $first) {
	    nodes {
	      __typename
	      ... on PullRequest {
	        number title state isDraft
	      }
	    }
	  }
	}`, map[string]interface{}{
		"query": "repo:acme/myproject is:pr is:open",
		"first": 10,
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", gql.Errors)
	}

	raw, _ := json.Marshal(gql.Data)
	var result struct {
		Search struct {
			Nodes []struct {
				Typename string `json:"__typename"`
				Number   int    `json:"number"`
				State    string `json:"state"`
				IsDraft  bool   `json:"isDraft"`
			} `json:"nodes"`
		} `json:"search"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(result.Search.Nodes) != 2 {
		t.Errorf("got %d PR results, want 2 (OPEN + DRAFT are both state=OPEN)", len(result.Search.Nodes))
	}
}

func TestListPRs(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($owner: String!, $repo: String!, $first: Int!, $states: [PullRequestState!]) {
	  repository(owner: $owner, name: $repo) {
	    pullRequests(first: $first, states: $states, orderBy: {field: UPDATED_AT, direction: DESC}) {
	      pageInfo { hasNextPage }
	      nodes { number title state isDraft reviewDecision }
	    }
	  }
	}`, map[string]interface{}{
		"owner":  "acme",
		"repo":   "myproject",
		"first":  10,
		"states": []string{"OPEN", "CLOSED", "MERGED"},
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", gql.Errors)
	}

	raw, _ := json.Marshal(gql.Data)
	var result struct {
		Repository struct {
			PullRequests struct {
				Nodes []struct {
					Number  int    `json:"number"`
					State   string `json:"state"`
					IsDraft bool   `json:"isDraft"`
				} `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(result.Repository.PullRequests.Nodes) != 3 {
		t.Errorf("got %d PRs, want 3", len(result.Repository.PullRequests.Nodes))
	}
}

func TestUnauthorized(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	body, _ := json.Marshal(gqlRequest{Query: "query { viewer { login } }", Variables: map[string]interface{}{}})
	req, _ := http.NewRequest("POST", s.URL(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var result gqlResponse
	json.Unmarshal(raw, &result)

	if len(result.Errors) == 0 {
		t.Fatal("expected unauthorized error")
	}
	if result.Errors[0].Message != "unauthorized" {
		t.Errorf("error = %q, want 'unauthorized'", result.Errors[0].Message)
	}
}

func TestScenarioBuilder_Realistic(t *testing.T) {
	scenario := NewScenarioBuilder("acme", "myproject").
		WithSeed(99).
		GenerateRealistic()

	s := NewServer(scenario)
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($owner: String!, $repo: String!, $first: Int!) {
	  repository(owner: $owner, name: $repo) {
	    issues(first: $first, states: [OPEN, CLOSED], orderBy: {field: UPDATED_AT, direction: DESC}) {
	      nodes { number title state }
	    }
	  }
	}`, map[string]interface{}{
		"owner": "acme",
		"repo":  "myproject",
		"first": 50,
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("errors: %v", gql.Errors)
	}

	raw, _ := json.Marshal(gql.Data)
	var result struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					Number int    `json:"number"`
					State  string `json:"state"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	}
	json.Unmarshal(raw, &result)

	if len(result.Repository.Issues.Nodes) != 8 {
		t.Errorf("got %d issues, want 8", len(result.Repository.Issues.Nodes))
	}
}

func TestTimeAdvance(t *testing.T) {
	now := time.Now()
	b := NewScenarioBuilder("acme", "myproject").WithNow(now)

	scenario := b.
		AdvanceTime(time.Hour).
		NewIssue("First issue", "Body text").
		NewIssue("Second issue", "Another body").
		CommentOnIssue(1, "bob", "A comment on first issue").
		NewPR("First PR", "PR body", "feat/first").
		CommentOnPR(3, "alice", "Reviewing soon").
		MergePR(3).
		Build()

	s := NewServer(scenario)
	defer s.Close()

	issue := s.scenario.Issues[0]
	if issue.Number != 1 || issue.Title != "First issue" {
		t.Errorf("issue 1 = %+v", issue)
	}
	if len(issue.Comments) != 1 || issue.Comments[0].Body != "A comment on first issue" {
		t.Errorf("issue 1 comments = %v", issue.Comments)
	}

	pr := s.scenario.PRs[0]
	if pr.Number != 3 || pr.State != "MERGED" {
		t.Errorf("PR 3 = state=%q, want MERGED", pr.State)
	}
	if len(pr.Comments) != 1 {
		t.Errorf("PR 3 comments = %d, want 1", len(pr.Comments))
	}

	summary := scenario.Summary()
	if summary == "" {
		t.Error("Summary should not be empty")
	}
}

func TestFetchAllPRs(t *testing.T) {
	s := NewServer(sampleScenario())
	defer s.Close()

	gql := postGQL(t, s.URL(), `
	query($owner: String!, $repo: String!, $after: String) {
	  repository(owner: $owner, name: $repo) {
	    pullRequests(first: 100, states: [OPEN, CLOSED, MERGED], after: $after, orderBy: {field: UPDATED_AT, direction: DESC}) {
	      totalCount
	      pageInfo { hasNextPage }
	      nodes {
	        number title state isDraft
	        comments(first: 100) {
	          totalCount
	          nodes { id author { login } body createdAt updatedAt url }
	        }
	      }
	    }
	  }
	}`, map[string]interface{}{
		"owner": "acme",
		"repo":  "myproject",
	})

	if len(gql.Errors) > 0 {
		t.Fatalf("errors: %v", gql.Errors)
	}

	raw, _ := json.Marshal(gql.Data)
	var result struct {
		Repository struct {
			PullRequests struct {
				TotalCount int `json:"totalCount"`
				Nodes      []struct {
					Number   int    `json:"number"`
					State    string `json:"state"`
					Comments struct {
						TotalCount int `json:"totalCount"`
					} `json:"comments"`
				} `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"repository"`
	}
	json.Unmarshal(raw, &result)

	if result.Repository.PullRequests.TotalCount != 3 {
		t.Errorf("pullRequests totalCount = %d, want 3", result.Repository.PullRequests.TotalCount)
	}

	if len(result.Repository.PullRequests.Nodes) != 3 {
		t.Errorf("got %d PRs, want 3", len(result.Repository.PullRequests.Nodes))
	}

	for _, n := range result.Repository.PullRequests.Nodes {
		if n.Number == 4 && n.Comments.TotalCount != 1 {
			t.Errorf("PR #4 comments = %d, want 1", n.Comments.TotalCount)
		}
	}
}

func TestIntegration_WithRealClient(t *testing.T) {
	scenario := sampleScenario()
	srv := NewServer(scenario)
	defer srv.Close()

	_ = &github.Client{}
	endpoint := srv.URL()

	testQuery := func(query string, vars map[string]interface{}) (json.RawMessage, error) {
		body, _ := json.Marshal(gqlRequest{Query: query, Variables: vars})
		req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(body))
		req.Header.Set("Authorization", "bearer test-token")
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return raw, nil
	}

	raw, err := testQuery(fmt.Sprintf(`
	query($owner: String!, $repo: String!, $number: Int!) {
	  repository(owner: $owner, name: $repo) {
	    issue(number: $number) {
	      number title state
	      author { login }
	      assignees(first: 10) { nodes { login } }
	      labels(first: 20) { nodes { name color } }
	      milestone { number title }
	      createdAt updatedAt closedAt url body
	      comments(first: 100) {
	        totalCount
	        nodes { id author { login } body createdAt updatedAt url }
	      }
	    }
	  }
	}`), map[string]interface{}{
		"owner":  "acme",
		"repo":   "myproject",
		"number": 1,
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	var parsed struct {
		Data struct {
			Repository struct {
				Issue *struct {
					Number   int    `json:"number"`
					Title    string `json:"title"`
					State    string `json:"state"`
					Comments struct {
						TotalCount int `json:"totalCount"`
					} `json:"comments"`
				} `json:"issue"`
			} `json:"repository"`
		} `json:"data"`
	}
	json.Unmarshal(raw, &parsed)

	issue := parsed.Data.Repository.Issue
	if issue == nil {
		t.Fatal("issue should not be nil")
	}
	if issue.Number != 1 {
		t.Errorf("Number = %d, want 1", issue.Number)
	}
	if issue.State != "CLOSED" {
		t.Errorf("State = %q, want CLOSED", issue.State)
	}
	if issue.Comments.TotalCount != 2 {
		t.Errorf("Comments.TotalCount = %d, want 2", issue.Comments.TotalCount)
	}
}
