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
)

//go:embed assets
var embeddedFS embed.FS

// RefreshFunc fetches fresh issues and PRs for a repository into the store.
// It is injected by the caller (the serve command) so this package stays
// decoupled from the GitHub client. The returned counts are the numbers of
// issues and PRs written.
type RefreshFunc func(host, owner, repo string) (issues, prs int, err error)

// Server serves the web UI and its JSON API over the cache store.
type Server struct {
	store   cache.Store
	refresh RefreshFunc

	mu         sync.Mutex
	refreshing map[string]bool

	srv *http.Server
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

// Handler returns the root http.Handler, separately from ListenAndServe so
// tests can exercise it without binding a port.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/repos/", s.handleRepoPath)
	mux.HandleFunc("/api/repos", s.handleRepos)
	mux.HandleFunc("/api/search", s.handleSearch)
	mux.HandleFunc("/assets/", s.handleAssets)
	mux.HandleFunc("/", s.handleIndex)
	return mux
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

// ItemDetail is the detail-view projection of an issue or PR. ItemSummary is
// embedded, so its fields are flattened into the JSON object.
type ItemDetail struct {
	ItemSummary
	BodyHTML    string         `json:"bodyHTML"`
	CreatedAt   time.Time      `json:"createdAt"`
	ClosedAt    *time.Time     `json:"closedAt"`
	MergedAt    *time.Time     `json:"mergedAt"`
	Assignees   []string       `json:"assignees"`
	Milestone   *MilestoneInfo `json:"milestone"`
	BaseRefName string         `json:"baseRefName,omitempty"`
	HeadRefName string         `json:"headRefName,omitempty"`
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
		issue, _, err := s.store.LoadIssue(host, owner, repo, number)
		if err != nil {
			httpError(w, http.StatusNotFound, "issue #%d not found in cache", number)
			return
		}
		detail = issueDetail(issue)
	case "prs":
		pr, _, err := s.store.LoadPR(host, owner, repo, number)
		if err != nil {
			httpError(w, http.StatusNotFound, "pull request #%d not found in cache", number)
			return
		}
		detail = prDetail(pr)
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
		Comments:    commentViews(pr.Comments),
	}
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

// hasRepo reports whether the store holds cache metadata for a repository.
// It works across backends (the file and SQLite stores both persist a
// CacheInfo entry per repository).
func (s *Server) hasRepo(host, owner, repo string) bool {
	_, err := s.store.LoadCacheInfo(host, owner, repo)
	return err == nil
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
