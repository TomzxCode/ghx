package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// CacheRefreshParams mirrors the flags of `ghx cache`.
type CacheRefreshParams struct {
	Repo     string `json:"repo,omitempty"`
	Type     string `json:"type,omitempty"` // issues, prs, or both (default both)
	Force    bool   `json:"force,omitempty"`
	Since    string `json:"since,omitempty"`
	Duration *int   `json:"durationMinutes,omitempty"` // default 60
	Retries  *int   `json:"retries,omitempty"`         // default 2
}

// CacheRefreshResult reports the records written by cache.refresh.
type CacheRefreshResult struct {
	Repo   string `json:"repo"`
	Issues int    `json:"issues"`
	PRs    int    `json:"prs"`
}

func (s *Server) registerCacheMethods() {
	s.add("cache.refresh", "Fetch issues and/or pull requests into the cache, like `ghx cache`.", `{"repo":"OWNER/REPO","type":"both","force":false,"since":"2026-09-01","durationMinutes":60,"retries":2}`, s.cacheRefresh)
}

func (s *Server) cacheRefresh(ctx context.Context, raw json.RawMessage) (any, error) {
	var p CacheRefreshParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if s.cfg.Refresh == nil {
		return nil, fmt.Errorf("cache refresh is not configured on this server")
	}
	repo, err := s.resolveRepo(p.Repo)
	if err != nil {
		return nil, err
	}

	kind := strings.ToLower(strings.TrimSpace(p.Type))
	if kind == "" {
		kind = "both"
	}
	if kind != "issues" && kind != "prs" && kind != "both" {
		return nil, Errorf(CodeInvalidParams, "invalid type %q: use issues, prs, or both", p.Type)
	}
	duration := 60
	if p.Duration != nil {
		duration = *p.Duration
	}
	if duration < 0 {
		return nil, Errorf(CodeInvalidParams, "durationMinutes must be 0 or greater")
	}
	retries := 2
	if p.Retries != nil {
		retries = *p.Retries
	}
	if retries < 0 {
		return nil, Errorf(CodeInvalidParams, "retries must be 0 or greater")
	}

	var since *time.Time
	if strings.TrimSpace(p.Since) != "" {
		if s.cfg.ParseSince == nil {
			return nil, Errorf(CodeInvalidParams, "since is not supported by this server")
		}
		t, err := s.cfg.ParseSince(p.Since)
		if err != nil {
			return nil, Errorf(CodeInvalidParams, "%s", err.Error())
		}
		since = &t
	}
	if p.Force && since != nil {
		return nil, Errorf(CodeInvalidParams, "force and since cannot be combined")
	}

	// Serialise refreshes: they write many records and the file backend has no
	// cross-goroutine coordination.
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	// A refresh queued behind another one may have outlived its client.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	req := RefreshRequest{
		Repo:     repo,
		Type:     kind,
		Force:    p.Force,
		Duration: duration,
		Retries:  retries,
	}
	req.Since = since
	res, err := s.cfg.Refresh(ctx, req)
	if err != nil {
		return nil, err
	}
	return CacheRefreshResult{Repo: repoLabel(repo), Issues: res.Issues, PRs: res.PRs}, nil
}
