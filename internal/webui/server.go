// Package webui serves a local web UI and JSON API over the ghx cache store.
// The UI lists and views cached issues and pull requests for all cached
// repositories and can trigger a cache refresh through an injected callback.
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/logging"
)

//go:embed assets
var embeddedFS embed.FS

// RefreshFunc fetches fresh issues and PRs for a repository into the store.
// It is injected by the caller (the serve command) so this package stays
// decoupled from the GitHub client. The returned counts are the numbers of
// issues and PRs written.
type RefreshFunc func(host, owner, repo string) (issues, prs int, err error)

// ItemRefreshFunc force-fetches one issue or pull request into the store,
// bypassing the cache's freshness checks. kind is "issue" or "pr". It backs the
// detail view's refresh button, which behaves like `ghx issue view --refresh`
// and `ghx pr view --refresh`.
type ItemRefreshFunc func(host, owner, repo, kind string, number int) error

// StatsQuery describes one stats report request. The string fields mirror the
// ghx stats flags, so the UI can pass the query string through unchanged.
// Repos holds "host/owner/repo" identifiers; an empty list means every cached
// repository.
type StatsQuery struct {
	Repos       []string
	From        string
	To          string
	State       string
	Authors     []string
	Reviewers   []string
	IncludeBots bool
	ListPRs     bool
	Top         int
}

// StatsFunc renders the stats report for a query. It is injected by the caller
// (the serve command) so this package stays decoupled from the report builder
// in the cmd package. It returns a complete HTML document.
type StatsFunc func(q StatsQuery) (string, error)

// ProcessQuery describes one process report request. Repos holds
// "host/owner/repo" identifiers; an empty list means every cached repository.
type ProcessQuery struct {
	Repos []string
	// Days is the optional trailing window; 0 means unset.
	Days int
	// From and To are optional date bounds (YYYY-MM-DD or RFC3339): when
	// either is set the report covers that explicit range instead of the
	// trailing Days window (an empty From starts at the first item, an empty
	// To ends now). With no bounds and no Days, the report covers all
	// available history.
	From string
	To   string
}

// ProcessFunc renders the process (production-line) report for a query. Like
// StatsFunc it is injected by the serve command and returns a complete HTML
// document.
type ProcessFunc func(q ProcessQuery) (string, error)

// Server serves the web UI and its JSON API over the cache store.
type Server struct {
	store       cache.Store
	refresh     RefreshFunc
	itemRefresh ItemRefreshFunc
	stats       StatsFunc
	process     ProcessFunc

	mu         sync.Mutex
	refreshing map[string]bool

	mounts []mount

	srv *http.Server
}

// mount is an extra handler served alongside the UI, such as the JSON-RPC API.
type mount struct {
	path    string
	handler http.Handler
}

// New creates a Server reading from store. refresh may be nil, in which case
// the refresh endpoint reports that refreshing is unavailable.
func New(store cache.Store, refresh RefreshFunc) *Server {
	return &Server{
		store:      store,
		refresh:    refresh,
		refreshing: map[string]bool{},
	}
}

// SetItemRefreshFunc sets the callback behind the single-item refresh endpoint.
// Without one, that endpoint reports that refreshing is unavailable.
func (s *Server) SetItemRefreshFunc(f ItemRefreshFunc) {
	s.itemRefresh = f
}

// ListenAndServe serves on addr until Shutdown is called. It returns nil on a
// clean shutdown (http.ErrServerClosed).
func (s *Server) ListenAndServe(addr string) error {
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully shuts the server down, waiting at most for ctx.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// Mount registers an extra handler at path, so a single server can expose the
// UI and another API (the JSON-RPC endpoint) on one port. Mount must be called
// before Handler.
func (s *Server) Mount(path string, handler http.Handler) {
	s.mounts = append(s.mounts, mount{path: path, handler: handler})
}

// SetStatsFunc installs the stats report renderer, served at /stats. Passing
// nil disables the endpoint. It must be called before Handler.
func (s *Server) SetStatsFunc(f StatsFunc) {
	s.stats = f
}

// SetProcessFunc installs the process report renderer, served at /process.
// Passing nil disables the endpoint. It must be called before Handler.
func (s *Server) SetProcessFunc(f ProcessFunc) {
	s.process = f
}

// Handler returns the root http.Handler, separately from ListenAndServe so
// tests can exercise it without binding a port.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, m := range s.mounts {
		mux.Handle(m.path, m.handler)
	}
	mux.HandleFunc("/api/repos/", s.handleRepoPath)
	mux.HandleFunc("/api/repos", s.handleRepos)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/api/process", s.handleAPIProcess)
	mux.HandleFunc("/stats", s.handleStats)
	mux.HandleFunc("/process", s.handleProcess)
	mux.HandleFunc("/assets/", s.handleAssets)
	mux.HandleFunc("/", s.handleIndex)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(rec, r)
		logging.Info("page request", "method", r.Method, "path", r.URL.RequestURI(), "status", rec.status, "duration", time.Since(start))
	})
}

// ---------------------------------------------------------------------------
// Response models
// ---------------------------------------------------------------------------

// LabelChip is a label as exposed to the UI, with the color normalized to a
// CSS value ("" when the stored color is not a safe hex value).
type LabelChip struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// ItemSummary is the list-view projection of an issue or PR. The search
// endpoint fills the repository fields; the list endpoints leave them empty.
type ItemSummary struct {
	Kind         string      `json:"kind"` // "issue" or "pr"
	Number       int         `json:"number"`
	Title        string      `json:"title"`
	State        string      `json:"state"` // normalized lowercase
	IsDraft      bool        `json:"isDraft"`
	Author       string      `json:"author"`
	Labels       []LabelChip `json:"labels"`
	CommentCount int         `json:"commentCount"`
	UpdatedAt    time.Time   `json:"updatedAt"`
	URL          string      `json:"url"`
	Host         string      `json:"host,omitempty"`
	Owner        string      `json:"owner,omitempty"`
	Repo         string      `json:"repo,omitempty"`
}

// CommentView is a comment with its markdown body rendered to HTML.
type CommentView struct {
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	BodyHTML  string    `json:"bodyHTML"`
	URL       string    `json:"url"`
}

// MilestoneInfo identifies a milestone in detail views.
type MilestoneInfo struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

// ReviewerView is one reviewer shown on a pull request's detail page. Login
// identifies a person or bot, Name identifies a team. State is the reviewer's
// latest submitted review state in lowercase ("approved",
// "changes_requested", "commented", "dismissed"), or "pending" when GitHub
// currently waits on them.
type ReviewerView struct {
	Login string `json:"login,omitempty"`
	Name  string `json:"name,omitempty"`
	State string `json:"state"`
}

// ItemDetail is the detail-view projection of an issue or PR. ItemSummary is
// embedded, so its fields are flattened into the JSON object. CachedAt is the
// time the item's cache row was last written.
type ItemDetail struct {
	ItemSummary
	BodyHTML    string         `json:"bodyHTML"`
	CreatedAt   time.Time      `json:"createdAt"`
	CachedAt    time.Time      `json:"cachedAt"`
	ClosedAt    *time.Time     `json:"closedAt"`
	MergedAt    *time.Time     `json:"mergedAt"`
	Assignees   []string       `json:"assignees"`
	Milestone   *MilestoneInfo `json:"milestone"`
	BaseRefName string         `json:"baseRefName,omitempty"`
	HeadRefName string         `json:"headRefName,omitempty"`
	Reviewers   []ReviewerView `json:"reviewers,omitempty"`
	Comments    []CommentView  `json:"comments"`
}

// RepoSummary describes a cached repository.
type RepoSummary struct {
	Host       string     `json:"host"`
	Owner      string     `json:"owner"`
	Repo       string     `json:"repo"`
	IssueCount int        `json:"issueCount"`
	PRCount    int        `json:"prCount"`
	CachedAt   *time.Time `json:"cachedAt"`
	Complete   bool       `json:"complete"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	serveAsset(w, "index.html", "text/html; charset=utf-8")
}

func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/assets/")
	if name == "" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	switch {
	case strings.HasSuffix(name, ".css"):
		serveAsset(w, name, "text/css; charset=utf-8")
	case strings.HasSuffix(name, ".js"):
		serveAsset(w, name, "text/javascript; charset=utf-8")
	default:
		serveAsset(w, name, "application/octet-stream")
	}
}

func serveAsset(w http.ResponseWriter, name, contentType string) {
	data, err := fs.ReadFile(embeddedFS, "assets/"+name)
	if err != nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(data)
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.store.ListCachedRepos()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "%s", err.Error())
		return
	}
	out := make([]RepoSummary, 0, len(repos))
	for _, cr := range repos {
		rs := RepoSummary{
			Host:       cr.Host,
			Owner:      cr.Owner,
			Repo:       cr.Repo,
			IssueCount: cr.IssueCount,
			PRCount:    cr.PRCount,
		}
		if cr.Info != nil {
			cachedAt := cr.Info.CachedAt
			rs.CachedAt = &cachedAt
			rs.Complete = cr.Info.Complete
		}
		out = append(out, rs)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Owner != out[j].Owner {
			return out[i].Owner < out[j].Owner
		}
		return out[i].Repo < out[j].Repo
	})
	writeJSON(w, http.StatusOK, map[string]any{"repos": out})
}

// handleRepoPath routes /api/repos/{host}/{owner}/{repo}/... paths.
func (s *Server) handleRepoPath(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/repos/")
	parts := strings.Split(rest, "/")
	if len(parts) < 4 {
		httpError(w, http.StatusNotFound, "expected /api/repos/{host}/{owner}/{repo}/...")
		return
	}
	host, owner, repo := parts[0], parts[1], parts[2]
	kind, tail := parts[3], parts[4:]

	switch kind {
	case "issues", "prs":
		if len(tail) == 0 {
			s.handleList(w, r, host, owner, repo, kind)
			return
		}
		number, err := strconv.Atoi(tail[0])
		if err != nil {
			httpError(w, http.StatusNotFound, "invalid number %q", tail[0])
			return
		}
		if len(tail) == 2 && tail[1] == "refresh" {
			s.handleItemRefresh(w, r, host, owner, repo, kind, number)
			return
		}
		if len(tail) > 1 {
			httpError(w, http.StatusNotFound, "unknown resource %q", tail[1])
			return
		}
		s.handleDetail(w, r, host, owner, repo, kind, number)
	case "refresh":
		s.handleRefresh(w, r, host, owner, repo)
	default:
		httpError(w, http.StatusNotFound, "unknown resource %q", kind)
	}
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, host, owner, repo, kind string) {
	if !s.hasRepo(host, owner, repo) {
		httpError(w, http.StatusNotFound, "repository %s/%s/%s is not cached", host, owner, repo)
		return
	}
	query := r.URL.Query()
	state := strings.ToLower(query.Get("state"))
	if state == "" {
		state = "open"
	}
	q := strings.ToLower(query.Get("q"))

	if !validState(kind, state) {
		httpError(w, http.StatusBadRequest, "invalid state %q for %s (use open, closed, merged, or all)", state, kind)
		return
	}

	items, err := s.loadSummaries(host, owner, repo, kind)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "%s", err.Error())
		return
	}

	filtered := make([]ItemSummary, 0, len(items))
	for _, item := range items {
		if !stateMatches(item.State, state) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(item.Title), q) {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	writeJSON(w, http.StatusOK, map[string]any{"items": filtered})
}

func (s *Server) handleDetail(w http.ResponseWriter, r *http.Request, host, owner, repo, kind string, number int) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method %s not allowed", r.Method)
		return
	}
	if !s.hasRepo(host, owner, repo) {
		httpError(w, http.StatusNotFound, "repository %s/%s/%s is not cached", host, owner, repo)
		return
	}

	var detail *ItemDetail
	switch kind {
	case "issues":
		issue, cachedAt, err := s.store.LoadIssue(host, owner, repo, number)
		if err != nil {
			httpError(w, http.StatusNotFound, "issue #%d not found in cache", number)
			return
		}
		detail = issueDetail(issue)
		detail.CachedAt = cachedAt
	case "prs":
		pr, cachedAt, err := s.store.LoadPR(host, owner, repo, number)
		if err != nil {
			httpError(w, http.StatusNotFound, "pull request #%d not found in cache", number)
			return
		}
		detail = prDetail(pr)
		detail.CachedAt = cachedAt
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request, host, owner, repo string) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "refresh requires POST")
		return
	}
	if !s.hasRepo(host, owner, repo) {
		httpError(w, http.StatusNotFound, "repository %s/%s/%s is not cached", host, owner, repo)
		return
	}
	if s.refresh == nil {
		httpError(w, http.StatusInternalServerError, "refresh is not available")
		return
	}

	key := host + "/" + owner + "/" + repo
	s.mu.Lock()
	if s.refreshing[key] {
		s.mu.Unlock()
		httpError(w, http.StatusConflict, "a refresh is already running for %s", key)
		return
	}
	s.refreshing[key] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.refreshing, key)
		s.mu.Unlock()
	}()

	issues, prs, err := s.refresh(host, owner, repo)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "%s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"issues": issues, "prs": prs})
}

// handleItemRefresh re-fetches one cached issue or pull request from GitHub,
// bypassing the freshness checks the detail endpoint relies on. The UI calls it
// from the detail view, where the repo-level refresh would be wasted work.
func (s *Server) handleItemRefresh(w http.ResponseWriter, r *http.Request, host, owner, repo, kind string, number int) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "refresh requires POST")
		return
	}
	if !s.hasRepo(host, owner, repo) {
		httpError(w, http.StatusNotFound, "repository %s/%s/%s is not cached", host, owner, repo)
		return
	}
	if s.itemRefresh == nil {
		httpError(w, http.StatusNotImplemented, "item refresh is not available")
		return
	}

	itemKind := "issue"
	if kind == "prs" {
		itemKind = "pr"
	}
	if !s.hasItem(host, owner, repo, itemKind, number) {
		httpError(w, http.StatusNotFound, "%s #%d not found in cache", itemKind, number)
		return
	}

	key := host + "/" + owner + "/" + repo + "/" + itemKind + "/" + strconv.Itoa(number)
	s.mu.Lock()
	if s.refreshing[key] {
		s.mu.Unlock()
		httpError(w, http.StatusConflict, "a refresh is already running for %s", key)
		return
	}
	s.refreshing[key] = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.refreshing, key)
		s.mu.Unlock()
	}()

	if err := s.itemRefresh(host, owner, repo, itemKind, number); err != nil {
		httpError(w, http.StatusInternalServerError, "%s", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"refreshed": true})
}

// hasItem reports whether the store holds the named issue or pull request.
func (s *Server) hasItem(host, owner, repo, kind string, number int) bool {
	switch kind {
	case "issue":
		_, _, err := s.store.LoadIssue(host, owner, repo, number)
		return err == nil
	case "pr":
		_, _, err := s.store.LoadPR(host, owner, repo, number)
		return err == nil
	}
	return false
}

// handleStats serves the stats report at /stats. The report is rendered by the
// injected StatsFunc; the query string mirrors the ghx stats flags (repo,
// from, to, state, author, reviewer, include-bots, list-prs, top).
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "method %s not allowed", r.Method)
		return
	}
	if s.stats == nil {
		httpError(w, http.StatusNotImplemented, "stats report is not available")
		return
	}
	query := r.URL.Query()
	state := strings.ToLower(query.Get("state"))
	if state == "" {
		state = "all"
	}
	if !validState("prs", state) {
		httpError(w, http.StatusBadRequest, "invalid state %q (use open, closed, merged, or all)", state)
		return
	}
	top := 5
	if v := query.Get("top"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			httpError(w, http.StatusBadRequest, "invalid top %q", v)
			return
		}
		top = n
	}
	q := StatsQuery{
		Repos:       query["repo"],
		From:        query.Get("from"),
		To:          query.Get("to"),
		State:       state,
		Authors:     query["author"],
		Reviewers:   query["reviewer"],
		IncludeBots: isTruthyParam(query.Get("include-bots")),
		ListPRs:     isTruthyParam(query.Get("list-prs")),
		Top:         top,
	}
	html, err := s.stats(q)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "%s", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// isTruthyParam interprets a boolean query parameter ("1" and "true" are on).
func isTruthyParam(v string) bool {
	return v == "1" || strings.EqualFold(v, "true")
}

// handleSearch serves the palette search across one repository (scope=repo,
// the default, using the host/owner/repo query params) or all cached
// repositories (scope=all). The query matches item numbers exactly, title
// word prefixes, or title subsequences; an empty query returns the most
// recently updated items.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	q := query.Get("q")
	scope := query.Get("scope")
	if scope == "" {
		scope = "repo"
	}
	if scope != "repo" && scope != "all" {
		httpError(w, http.StatusBadRequest, "invalid scope %q (use repo or all)", scope)
		return
	}

	var targets [][3]string // host, owner, repo
	if scope == "repo" {
		host, owner, repo := query.Get("host"), query.Get("owner"), query.Get("repo")
		if host == "" || owner == "" || repo == "" {
			httpError(w, http.StatusBadRequest, "scope=repo requires host, owner, and repo parameters")
			return
		}
		targets = append(targets, [3]string{host, owner, repo})
	} else {
		repos, err := s.store.ListCachedRepos()
		if err != nil {
			httpError(w, http.StatusInternalServerError, "%s", err.Error())
			return
		}
		for _, cr := range repos {
			targets = append(targets, [3]string{cr.Host, cr.Owner, cr.Repo})
		}
	}

	type scored struct {
		item  ItemSummary
		score int
	}
	var matches []scored
	for _, t := range targets {
		for _, kind := range []string{"issues", "prs"} {
			items, err := s.loadSummaries(t[0], t[1], t[2], kind)
			if err != nil {
				continue
			}
			for _, item := range items {
				item.Host, item.Owner, item.Repo = t[0], t[1], t[2]
				score := 0
				if q != "" {
					score = paletteScore(q, item.Number, item.Title)
					if score == 0 {
						continue
					}
				}
				matches = append(matches, scored{item: item, score: score})
			}
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].item.UpdatedAt.After(matches[j].item.UpdatedAt)
	})
	const maxResults = 50
	out := make([]ItemSummary, 0, maxResults)
	for i, m := range matches {
		if i >= maxResults {
			break
		}
		out = append(out, m.item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// ---------------------------------------------------------------------------
// Projections
// ---------------------------------------------------------------------------

// loadSummaries reads every cached item of kind ("issues" or "prs") for a
// repository and projects it onto ItemSummary values.
func (s *Server) loadSummaries(host, owner, repo, kind string) ([]ItemSummary, error) {
	switch kind {
	case "issues":
		issues, err := s.store.LoadAllIssues(host, owner, repo)
		if err != nil {
			return nil, err
		}
		out := make([]ItemSummary, 0, len(issues))
		for _, issue := range issues {
			out = append(out, issueSummary(issue))
		}
		return out, nil
	case "prs":
		prs, err := s.store.LoadAllPRs(host, owner, repo)
		if err != nil {
			return nil, err
		}
		out := make([]ItemSummary, 0, len(prs))
		for _, pr := range prs {
			out = append(out, prSummary(pr))
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown kind %q", kind)
}

func issueSummary(issue *github.Issue) ItemSummary {
	return ItemSummary{
		Kind:         "issue",
		Number:       issue.Number,
		Title:        issue.Title,
		State:        strings.ToLower(issue.State),
		Author:       issue.Author.Login,
		Labels:       labelChips(issue.Labels),
		CommentCount: commentCount(issue.CommentCount, len(issue.Comments)),
		UpdatedAt:    issue.UpdatedAt,
		URL:          issue.URL,
	}
}

func prSummary(pr *github.PullRequest) ItemSummary {
	state := strings.ToLower(pr.State)
	if state != "merged" && pr.MergedAt != nil {
		state = "merged"
	}
	return ItemSummary{
		Kind:         "pr",
		Number:       pr.Number,
		Title:        pr.Title,
		State:        state,
		IsDraft:      pr.IsDraft,
		Author:       pr.Author.Login,
		Labels:       labelChips(pr.Labels),
		CommentCount: commentCount(pr.CommentCount, len(pr.Comments)),
		UpdatedAt:    pr.UpdatedAt,
		URL:          pr.URL,
	}
}

func issueDetail(issue *github.Issue) *ItemDetail {
	return &ItemDetail{
		ItemSummary: issueSummary(issue),
		BodyHTML:    RenderMarkdown(issue.Body),
		CreatedAt:   issue.CreatedAt,
		ClosedAt:    issue.ClosedAt,
		Assignees:   actorLogins(issue.Assignees),
		Milestone:   milestoneInfo(issue.Milestone),
		Comments:    commentViews(issue.Comments),
	}
}

func prDetail(pr *github.PullRequest) *ItemDetail {
	return &ItemDetail{
		ItemSummary: prSummary(pr),
		BodyHTML:    RenderMarkdown(pr.Body),
		CreatedAt:   pr.CreatedAt,
		ClosedAt:    pr.ClosedAt,
		MergedAt:    pr.MergedAt,
		Assignees:   actorLogins(pr.Assignees),
		Milestone:   milestoneInfo(pr.Milestone),
		BaseRefName: pr.BaseRefName,
		HeadRefName: pr.HeadRefName,
		Reviewers:   prReviewers(pr),
		Comments:    commentViews(pr.Comments),
	}
}

// prReviewers lists the pull request's reviewers for the detail sidebar. It
// combines the reviewers who submitted a review with GitHub's current review
// requests, including teams. The pull request author never counts as a
// reviewer. Review state is the latest non-comment review (so a later plain
// comment does not hide an approval), falling back to "commented" when the
// reviewer only left comment reviews; anyone in the current review requests is
// "pending", which covers re-requests.
//
// A nil ReviewRequests means the cached record predates the field (or came
// from a query that did not ask for it), so the timeline's review-request
// events are used as a user-only fallback. Teams and review removals are not
// visible there.
func prReviewers(pr *github.PullRequest) []ReviewerView {
	type reviewer struct {
		login        string
		name         string
		state        string
		stateAt      time.Time
		lastReviewAt time.Time
		requested    bool
		requestedAt  time.Time
	}
	byKey := map[string]*reviewer{}
	get := func(key, login, name string) *reviewer {
		r, ok := byKey[key]
		if !ok {
			r = &reviewer{login: login, name: name}
			byKey[key] = r
		}
		return r
	}
	userKey := func(login string) string { return "user:" + strings.ToLower(login) }

	for _, rev := range pr.Reviews {
		if rev.Author.Login == "" || strings.EqualFold(rev.Author.Login, pr.Author.Login) {
			continue
		}
		r := get(userKey(rev.Author.Login), rev.Author.Login, "")
		submitted := rev.SubmittedAt
		if submitted.After(r.lastReviewAt) {
			r.lastReviewAt = submitted
		}
		state := strings.ToLower(rev.State)
		switch state {
		case "commented":
			if r.state == "" {
				r.state = "commented"
			}
			continue
		case "approved", "changes_requested", "dismissed":
			// Later comment-only reviews do not replace a decision, but a
			// later decision does.
			if submitted.After(r.stateAt) || r.state == "commented" {
				r.state = state
				r.stateAt = submitted
			}
		}
	}

	if pr.ReviewRequests != nil {
		for _, req := range pr.ReviewRequests {
			if req.Name != "" {
				r := get("team:"+strings.ToLower(req.Name), "", req.Name)
				r.requested = true
				continue
			}
			if req.Login == "" || strings.EqualFold(req.Login, pr.Author.Login) {
				continue
			}
			r := get(userKey(req.Login), req.Login, "")
			r.requested = true
		}
	} else {
		for _, ev := range pr.Timeline {
			if ev.Kind != github.TimelineReviewRequested || ev.RequestedReviewer.Login == "" ||
				strings.EqualFold(ev.RequestedReviewer.Login, pr.Author.Login) {
				continue
			}
			r := get(userKey(ev.RequestedReviewer.Login), ev.RequestedReviewer.Login, "")
			if ev.CreatedAt.After(r.requestedAt) {
				r.requestedAt = ev.CreatedAt
			}
		}
	}

	out := make([]ReviewerView, 0, len(byKey))
	for _, r := range byKey {
		state := r.state
		if state == "" || r.requested {
			state = "pending"
		} else if pr.ReviewRequests == nil && r.requestedAt.After(r.lastReviewAt) {
			state = "pending"
		}
		out = append(out, ReviewerView{Login: r.login, Name: r.name, State: state})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Name, out[j].Name
		if a == "" {
			a = out[i].Login
		}
		if b == "" {
			b = out[j].Login
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	return out
}

func labelChips(labels []github.Label) []LabelChip {
	out := make([]LabelChip, 0, len(labels))
	for _, l := range labels {
		out = append(out, LabelChip{Name: l.Name, Color: SanitizeColor(l.Color)})
	}
	return out
}

func commentViews(comments []github.Comment) []CommentView {
	out := make([]CommentView, 0, len(comments))
	for _, c := range comments {
		out = append(out, CommentView{
			Author:    c.Author.Login,
			CreatedAt: c.CreatedAt,
			BodyHTML:  RenderMarkdown(c.Body),
			URL:       c.URL,
		})
	}
	return out
}

func actorLogins(actors []github.Actor) []string {
	out := make([]string, 0, len(actors))
	for _, a := range actors {
		out = append(out, a.Login)
	}
	return out
}

func milestoneInfo(m *github.Milestone) *MilestoneInfo {
	if m == nil {
		return nil
	}
	return &MilestoneInfo{Number: m.Number, Title: m.Title}
}

func commentCount(count, fallback int) int {
	if count > 0 {
		return count
	}
	return fallback
}

// ---------------------------------------------------------------------------
// Filtering and palette scoring
// ---------------------------------------------------------------------------

// validState reports whether state is acceptable for the item kind. PRs
// additionally accept "merged".
func validState(kind, state string) bool {
	switch state {
	case "open", "closed", "all":
		return true
	case "merged":
		return kind == "prs"
	}
	return false
}

// stateMatches reports whether the normalized item state passes the filter
// ("all" and empty pass everything).
func stateMatches(itemState, filter string) bool {
	if filter == "" || filter == "all" {
		return true
	}
	return itemState == filter
}

// paletteScore scores a palette query against an item: 3 for an exact number
// match ("123" or "#123"), 2 for a title word prefix match, 1 for a title
// subsequence match, 0 for no match.
func paletteScore(query string, number int, title string) int {
	q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(query), "#")))
	if q == "" {
		return 0
	}
	if q == strconv.Itoa(number) {
		return 3
	}
	t := strings.ToLower(title)
	for _, word := range strings.Fields(t) {
		if strings.HasPrefix(word, q) {
			return 2
		}
	}
	if isSubsequence(t, q) {
		return 1
	}
	return 0
}

// isSubsequence reports whether sub's characters appear in s in order.
func isSubsequence(s, sub string) bool {
	i := 0
	for j := 0; j < len(s) && i < len(sub); j++ {
		if s[j] == sub[i] {
			i++
		}
	}
	return i == len(sub)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// hasRepo reports whether the store holds anything for a repository: cache
// metadata from a cache run, or cached items written by the read-through
// caching of `ghx issue view` and `ghx pr view`. A repository that was only
// ever viewed has items but no metadata, and the UI still lists it, so it must
// be treatable as cached; a refresh then starts a cold fetch.
func (s *Server) hasRepo(host, owner, repo string) bool {
	if _, err := s.store.LoadCacheInfo(host, owner, repo); err == nil {
		return true
	}
	repos, err := s.store.ListCachedRepos()
	if err != nil {
		return false
	}
	for _, cr := range repos {
		if strings.EqualFold(cr.Host, host) && strings.EqualFold(cr.Owner, owner) && strings.EqualFold(cr.Repo, repo) {
			return true
		}
	}
	return false
}

// statusRecorder captures the response status code written by a handler so the
// request logger can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func httpError(w http.ResponseWriter, code int, format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	writeJSON(w, code, map[string]string{"error": msg})
}
