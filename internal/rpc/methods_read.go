package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/gh"
	"github.com/tomzxcode/ghx/internal/github"
)

// RepoRef is embedded in parameter structs that accept an optional
// [HOST/]OWNER/REPO reference.
type RepoRef struct {
	Repo string `json:"repo,omitempty"`
}

// IssueListParams mirrors the filters of `ghx issue list`.
type IssueListParams struct {
	Repo      string   `json:"repo,omitempty"`
	State     string   `json:"state,omitempty"`
	Author    string   `json:"author,omitempty"`
	Assignee  string   `json:"assignee,omitempty"`
	Labels    []string `json:"labels,omitempty"`
	Milestone string   `json:"milestone,omitempty"`
	Mention   string   `json:"mention,omitempty"`
	Search    string   `json:"search,omitempty"`
	App       string   `json:"app,omitempty"`
	Limit     *int     `json:"limit,omitempty"`
}

// IssueListResult is the issue.list response.
type IssueListResult struct {
	Items  []*github.Issue `json:"items"`
	Total  int             `json:"total"`
	Source string          `json:"source"` // "cache" or "api"
}

// IssueViewParams selects one issue.
type IssueViewParams struct {
	Repo    string `json:"repo,omitempty"`
	Number  int    `json:"number"`
	Refresh bool   `json:"refresh,omitempty"`
}

// PRListParams mirrors the filters of `ghx pr list`.
type PRListParams struct {
	Repo     string   `json:"repo,omitempty"`
	State    string   `json:"state,omitempty"`
	Author   string   `json:"author,omitempty"`
	Assignee string   `json:"assignee,omitempty"`
	Labels   []string `json:"labels,omitempty"`
	Base     string   `json:"base,omitempty"`
	Head     string   `json:"head,omitempty"`
	Draft    bool     `json:"draft,omitempty"`
	Search   string   `json:"search,omitempty"`
	App      string   `json:"app,omitempty"`
	Limit    *int     `json:"limit,omitempty"`
}

// PRListResult is the pr.list response.
type PRListResult struct {
	Items  []*github.PullRequest `json:"items"`
	Total  int                   `json:"total"`
	Source string                `json:"source"` // "cache" or "api"
}

// PRViewParams selects one pull request.
type PRViewParams struct {
	Repo    string `json:"repo,omitempty"`
	Number  int    `json:"number"`
	Refresh bool   `json:"refresh,omitempty"`
}

// RepoListResult is the repo.list response.
type RepoListResult struct {
	Repos []RepoInfo `json:"repos"`
}

// RepoInfo describes one cached repository.
type RepoInfo struct {
	Host       string     `json:"host"`
	Owner      string     `json:"owner"`
	Repo       string     `json:"repo"`
	Label      string     `json:"label"`
	IssueCount int        `json:"issueCount"`
	PRCount    int        `json:"prCount"`
	CachedAt   *time.Time `json:"cachedAt,omitempty"`
	Duration   int        `json:"durationMinutes,omitempty"`
	Complete   bool       `json:"complete"`
	Fresh      bool       `json:"fresh"`
}

// RepoDeleteParams names the repository whose cache should be removed.
type RepoDeleteParams struct {
	Repo string `json:"repo,omitempty"`
}

// RepoDeleteResult is the repo.delete response.
type RepoDeleteResult struct {
	Repo    string `json:"repo"`
	Deleted bool   `json:"deleted"`
}

// ThreadsParams filters the review threads of one pull request.
type ThreadsParams struct {
	Repo   string `json:"repo,omitempty"`
	Number int    `json:"number"`
	Thread string `json:"thread,omitempty"`
	State  string `json:"state,omitempty"` // open, resolved, or all (default open)
}

// ThreadsResult is the pr.threads response.
type ThreadsResult struct {
	Threads []gh.ReviewThread `json:"threads"`
}

func (s *Server) registerReadMethods() {
	s.add("repo.list", "List repositories in the local cache.", "", s.repoList)
	s.add("repo.delete", "Delete every cached issue, pull request, and cache entry for one repository.", `{"repo":"OWNER/REPO"}`, s.repoDelete)
	s.add("issue.list", "List issues with the same filters as `ghx issue list`.", `{"repo":"OWNER/REPO","state":"open","author":"","assignee":"","labels":[],"milestone":"","mention":"","search":"","limit":1000}`, s.issueList)
	s.add("issue.view", "View one issue, serving a fresh cached record when possible.", `{"repo":"OWNER/REPO","number":42,"refresh":false}`, s.issueView)
	s.add("pr.list", "List pull requests with the same filters as `ghx pr list`.", `{"repo":"OWNER/REPO","state":"open","author":"","assignee":"","labels":[],"base":"","head":"","draft":false,"search":"","limit":1000}`, s.prList)
	s.add("pr.view", "View one pull request, serving a fresh cached record when possible.", `{"repo":"OWNER/REPO","number":10,"refresh":false}`, s.prView)
	s.add("pr.threads", "List the review threads of one pull request.", `{"repo":"OWNER/REPO","number":10,"thread":"","state":"open"}`, s.prThreads)
}

func (s *Server) repoList(context.Context, json.RawMessage) (any, error) {
	if s.cfg.Store == nil {
		return nil, fmt.Errorf("cache store is not configured on this server")
	}
	repos, err := s.cfg.Store.ListCachedRepos()
	if err != nil {
		return nil, err
	}
	out := make([]RepoInfo, 0, len(repos))
	now := time.Now()
	for _, cr := range repos {
		info := RepoInfo{
			Host:       cr.Host,
			Owner:      cr.Owner,
			Repo:       cr.Repo,
			Label:      formatRepo(cr.Host, cr.Owner, cr.Repo),
			IssueCount: cr.IssueCount,
			PRCount:    cr.PRCount,
		}
		if cr.Info != nil {
			cachedAt := cr.Info.CachedAt
			info.CachedAt = &cachedAt
			info.Duration = cr.Info.Duration
			info.Complete = cr.Info.Complete
			info.Fresh = now.Sub(cachedAt) < time.Duration(cr.Info.Duration)*time.Minute
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Repo < out[j].Repo
	})
	return RepoListResult{Repos: out}, nil
}

func (s *Server) repoDelete(ctx context.Context, raw json.RawMessage) (any, error) {
	var p RepoDeleteParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	repo, err := s.resolveRepo(p.Repo)
	if err != nil {
		return nil, err
	}
	if s.cfg.Store == nil {
		return nil, fmt.Errorf("cache store is not configured on this server")
	}
	deleted, err := s.cfg.Store.DeleteRepo(repo.Host, repo.Owner, repo.Name)
	if err != nil {
		return nil, err
	}
	return RepoDeleteResult{Repo: repoLabel(repo), Deleted: deleted}, nil
}

func (s *Server) issueList(ctx context.Context, raw json.RawMessage) (any, error) {
	var p IssueListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	repo, err := s.resolveRepo(p.Repo)
	if err != nil {
		return nil, err
	}
	state := strings.ToLower(strings.TrimSpace(p.State))
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "closed" && state != "all" {
		return nil, Errorf(CodeInvalidParams, "invalid state %q: use open, closed, or all", p.State)
	}
	limit := 1000
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 0 {
		return nil, Errorf(CodeInvalidParams, "limit must be 0 or greater")
	}

	store := s.cfg.Store
	if store == nil {
		return nil, fmt.Errorf("cache store is not configured on this server")
	}
	if fresh, _ := store.IsIssuesCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
		issues, err := store.QueryIssues(repo.Host, repo.Owner, repo.Name, cache.IssueQuery{
			State:     state,
			Assignee:  p.Assignee,
			Author:    p.Author,
			Labels:    p.Labels,
			Milestone: p.Milestone,
			Search:    p.Search,
		})
		if err == nil {
			sort.Slice(issues, func(i, j int) bool {
				return issues[i].UpdatedAt.After(issues[j].UpdatedAt)
			})
			total := len(issues)
			if limit > 0 && len(issues) > limit {
				issues = issues[:limit]
			}
			if issues == nil {
				issues = []*github.Issue{}
			}
			return IssueListResult{Items: issues, Total: total, Source: "cache"}, nil
		}
	}

	client, err := s.client(repo.Host)
	if err != nil {
		return nil, err
	}
	issues, err := client.ListIssues(repo.Owner, repo.Name, github.IssueListOptions{
		Limit:     limit,
		State:     state,
		Assignee:  p.Assignee,
		Author:    p.Author,
		Labels:    p.Labels,
		Milestone: p.Milestone,
		Mention:   p.Mention,
		Search:    p.Search,
		App:       p.App,
	})
	if err != nil {
		return nil, err
	}
	if s.cfg.BackfillIssues != nil {
		s.cfg.BackfillIssues(store, client, repo, issues)
	}
	if issues == nil {
		issues = []*github.Issue{}
	}
	return IssueListResult{Items: issues, Total: len(issues), Source: "api"}, nil
}

func (s *Server) issueView(ctx context.Context, raw json.RawMessage) (any, error) {
	var p IssueViewParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Number <= 0 {
		return nil, Errorf(CodeInvalidParams, "number must be a positive issue number")
	}
	repo, err := s.resolveRepo(p.Repo)
	if err != nil {
		return nil, err
	}
	store := s.cfg.Store
	if store == nil {
		return nil, fmt.Errorf("cache store is not configured on this server")
	}

	if !p.Refresh {
		if fresh, _ := store.IsIssuesCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
			if issue, _, err := store.LoadIssue(repo.Host, repo.Owner, repo.Name, p.Number); err == nil {
				return issue, nil
			}
		} else if issue, mtime, err := store.LoadIssue(repo.Host, repo.Owner, repo.Name, p.Number); err == nil && time.Since(mtime) < 60*time.Minute {
			return issue, nil
		}
	}

	// Issues and pull requests share one number sequence, so a cached pull
	// request proves no issue with this number exists. Report the type instead
	// of asking GitHub for an issue it cannot return.
	if _, _, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, p.Number); err == nil {
		return nil, Errorf(CodeApplication, "#%d is a pull request, not an issue; use \"ghx pr view %d\"", p.Number, p.Number)
	}

	client, err := s.client(repo.Host)
	if err != nil {
		return nil, err
	}
	issue, err := client.GetIssue(repo.Owner, repo.Name, p.Number)
	if err != nil {
		return nil, err
	}
	if err := store.SaveIssue(repo.Host, repo.Owner, repo.Name, issue); err != nil {
		return nil, fmt.Errorf("caching issue: %w", err)
	}
	return issue, nil
}

func (s *Server) prList(ctx context.Context, raw json.RawMessage) (any, error) {
	var p PRListParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	repo, err := s.resolveRepo(p.Repo)
	if err != nil {
		return nil, err
	}
	state := strings.ToLower(strings.TrimSpace(p.State))
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "closed" && state != "merged" && state != "all" {
		return nil, Errorf(CodeInvalidParams, "invalid state %q: use open, closed, merged, or all", p.State)
	}
	limit := 1000
	if p.Limit != nil {
		limit = *p.Limit
	}
	if limit < 0 {
		return nil, Errorf(CodeInvalidParams, "limit must be 0 or greater")
	}

	store := s.cfg.Store
	if store == nil {
		return nil, fmt.Errorf("cache store is not configured on this server")
	}
	if fresh, _ := store.IsPRsCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
		prs, err := store.QueryPRs(repo.Host, repo.Owner, repo.Name, cache.PRQuery{
			State:    state,
			Assignee: p.Assignee,
			Author:   p.Author,
			Labels:   p.Labels,
			BaseRef:  p.Base,
			HeadRef:  p.Head,
			Draft:    p.Draft,
			Search:   p.Search,
		})
		if err == nil {
			sort.Slice(prs, func(i, j int) bool {
				return prs[i].UpdatedAt.After(prs[j].UpdatedAt)
			})
			total := len(prs)
			if limit > 0 && len(prs) > limit {
				prs = prs[:limit]
			}
			if prs == nil {
				prs = []*github.PullRequest{}
			}
			return PRListResult{Items: prs, Total: total, Source: "cache"}, nil
		}
	}

	client, err := s.client(repo.Host)
	if err != nil {
		return nil, err
	}
	prs, err := client.ListPRs(repo.Owner, repo.Name, github.PRListOptions{
		Limit:    limit,
		State:    state,
		Assignee: p.Assignee,
		Author:   p.Author,
		Labels:   p.Labels,
		Base:     p.Base,
		Head:     p.Head,
		Draft:    p.Draft,
		Search:   p.Search,
		App:      p.App,
	})
	if err != nil {
		return nil, err
	}
	if s.cfg.BackfillPRs != nil {
		s.cfg.BackfillPRs(store, client, repo, prs)
	}
	if prs == nil {
		prs = []*github.PullRequest{}
	}
	return PRListResult{Items: prs, Total: len(prs), Source: "api"}, nil
}

func (s *Server) prView(ctx context.Context, raw json.RawMessage) (any, error) {
	var p PRViewParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Number <= 0 {
		return nil, Errorf(CodeInvalidParams, "number must be a positive pull request number")
	}
	repo, err := s.resolveRepo(p.Repo)
	if err != nil {
		return nil, err
	}
	store := s.cfg.Store
	if store == nil {
		return nil, fmt.Errorf("cache store is not configured on this server")
	}

	if !p.Refresh {
		if fresh, _ := store.IsPRsCacheFresh(repo.Host, repo.Owner, repo.Name); fresh {
			if pr, _, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, p.Number); err == nil {
				return pr, nil
			}
		} else if pr, mtime, err := store.LoadPR(repo.Host, repo.Owner, repo.Name, p.Number); err == nil && time.Since(mtime) < 60*time.Minute {
			return pr, nil
		}
	}

	// Issues and pull requests share one number sequence, so a cached issue
	// proves no pull request with this number exists. Report the type instead
	// of asking GitHub for a pull request it cannot return.
	if _, _, err := store.LoadIssue(repo.Host, repo.Owner, repo.Name, p.Number); err == nil {
		return nil, Errorf(CodeApplication, "#%d is an issue, not a pull request; use \"ghx issue view %d\"", p.Number, p.Number)
	}

	client, err := s.client(repo.Host)
	if err != nil {
		return nil, err
	}
	pr, err := client.GetPR(repo.Owner, repo.Name, p.Number)
	if err != nil {
		return nil, err
	}
	if err := store.SavePR(repo.Host, repo.Owner, repo.Name, pr); err != nil {
		return nil, fmt.Errorf("caching pull request: %w", err)
	}
	return pr, nil
}

func (s *Server) prThreads(_ context.Context, raw json.RawMessage) (any, error) {
	var p ThreadsParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if p.Number <= 0 {
		return nil, Errorf(CodeInvalidParams, "number must be a positive pull request number")
	}
	repo, err := s.resolveRepo(p.Repo)
	if err != nil {
		return nil, err
	}
	state := strings.ToLower(strings.TrimSpace(p.State))
	if state == "" {
		state = "open"
	}
	if state != "open" && state != "resolved" && state != "all" {
		return nil, Errorf(CodeInvalidParams, "invalid state %q: use open, resolved, or all", p.State)
	}

	threads, err := gh.ListThreads(repo.Owner, repo.Name, p.Number)
	if err != nil {
		return nil, err
	}
	filtered := make([]gh.ReviewThread, 0, len(threads))
	for _, t := range threads {
		if p.Thread != "" && t.ID != p.Thread {
			continue
		}
		switch state {
		case "open":
			if t.IsResolved {
				continue
			}
		case "resolved":
			if !t.IsResolved {
				continue
			}
		}
		filtered = append(filtered, t)
	}
	return ThreadsResult{Threads: filtered}, nil
}
