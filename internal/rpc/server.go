package rpc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tomzxcode/ghx/internal/cache"
	"github.com/tomzxcode/ghx/internal/github"
	"github.com/tomzxcode/ghx/internal/gitremote"
)

// RefreshRequest describes a cache refresh requested over RPC.
type RefreshRequest struct {
	Repo     *gitremote.Repo
	Type     string // "issues", "prs", or "both"
	Force    bool
	Since    *time.Time
	Duration int
	Retries  int
}

// RefreshResult reports how many records a cache refresh wrote.
type RefreshResult struct {
	Issues int `json:"issues"`
	PRs    int `json:"prs"`
}

// RefreshFunc fetches issues and pull requests for one repository into the
// cache. It is injected by the command layer so this package stays independent
// of the cache fetch implementation.
type RefreshFunc func(ctx context.Context, req RefreshRequest) (RefreshResult, error)

// BackfillIssuesFunc caches the full records for issues returned by a list
// call. It may be nil, in which case the list payload is returned as-is.
type BackfillIssuesFunc func(store cache.Store, client *github.Client, repo *gitremote.Repo, issues []*github.Issue)

// BackfillPRsFunc caches the full records for pull requests returned by a list
// call. It may be nil.
type BackfillPRsFunc func(store cache.Store, client *github.Client, repo *gitremote.Repo, prs []*github.PullRequest)

// ExecConfig configures the optional cli.run method.
type ExecConfig struct {
	// Binary is the ghx executable to run. When empty, os.Executable is used.
	Binary string
	// BaseArgs are global flags prepended to every executed command so child
	// processes use the same cache and API configuration as the server.
	BaseArgs []string
	// Env is the child environment. When nil, os.Environ is used.
	Env []string
	// Dir is the child working directory. When empty, the server's working
	// directory is inherited.
	Dir string
}

// Config wires a Server to the CLI's cache, GitHub client, and helper
// functions.
type Config struct {
	// Store is the cache store used by the typed methods. Required.
	Store cache.Store
	// ClientFactory creates a GitHub client for a host. Required for methods
	// that fall back to the API.
	ClientFactory func(host string) (*github.Client, error)
	// DefaultRepo resolves the repository for requests that omit one. It is
	// usually the CLI's --repo flag or the git remote of the server's working
	// directory.
	DefaultRepo func() (*gitremote.Repo, error)
	// Refresh performs a cache refresh. Required for cache.refresh.
	Refresh RefreshFunc
	// ParseSince parses a cache.refresh since value. Required when since is
	// used.
	ParseSince func(string) (time.Time, error)
	// BackfillIssues and BackfillPRs warm the cache after a list falls back to
	// the GitHub API.
	BackfillIssues BackfillIssuesFunc
	BackfillPRs    BackfillPRsFunc
	// Version is reported by server.info and rpc.discover.
	Version string
	// Token, when set, requires clients to send it as a bearer token.
	Token string
	// AllowExec enables the cli.run method.
	AllowExec bool
	// Exec configures cli.run.
	Exec ExecConfig
}

type method struct {
	name        string
	description string
	params      string
	handler     func(ctx context.Context, params json.RawMessage) (any, error)
}

// Server is a JSON-RPC 2.0 server. It implements http.Handler.
type Server struct {
	cfg     Config
	methods map[string]*method
	order   []string
	started time.Time

	// refreshMu serialises cache refreshes: a refresh writes many records and
	// the file backend has no cross-goroutine write coordination.
	refreshMu sync.Mutex
}

// NewServer builds a server with every method registered.
func NewServer(cfg Config) *Server {
	s := &Server{
		cfg:     cfg,
		methods: map[string]*method{},
		started: time.Now(),
	}
	s.registerCore()
	s.registerReadMethods()
	s.registerCacheMethods()
	if cfg.AllowExec {
		s.registerExec()
	}
	return s
}

func (s *Server) add(name, description, params string, handler func(context.Context, json.RawMessage) (any, error)) {
	s.methods[name] = &method{name: name, description: description, params: params, handler: handler}
	s.order = append(s.order, name)
}

// authorized checks the bearer token when the server was configured with one.
func (s *Server) authorized(r *http.Request) bool {
	if s.cfg.Token == "" {
		return true
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if got == "" {
		got = r.Header.Get("X-GHX-Token")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.Token)) == 1
}

// MethodInfo describes one method for rpc.discover.
type MethodInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Params      string `json:"params,omitempty"`
}

// DiscoverResult is the rpc.discover response.
type DiscoverResult struct {
	Name    string       `json:"name"`
	Version string       `json:"version"`
	Methods []MethodInfo `json:"methods"`
}

// ServerInfo is the server.info response.
type ServerInfo struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	StoreKind     string `json:"storeKind,omitempty"`
	StoreLocation string `json:"storeLocation,omitempty"`
	DefaultRepo   string `json:"defaultRepo,omitempty"`
	ExecEnabled   bool   `json:"execEnabled"`
	Methods       int    `json:"methods"`
	StartedAt     string `json:"startedAt"`
}

func (s *Server) registerCore() {
	s.add("rpc.discover", "List the methods this server exposes.", "", func(context.Context, json.RawMessage) (any, error) {
		names := append([]string(nil), s.order...)
		sort.Strings(names)
		methods := make([]MethodInfo, 0, len(names))
		for _, name := range names {
			m := s.methods[name]
			methods = append(methods, MethodInfo{Name: m.name, Description: m.description, Params: m.params})
		}
		return DiscoverResult{Name: "ghx", Version: s.cfg.Version, Methods: methods}, nil
	})

	s.add("server.info", "Report the server version, cache backend, and resolved default repository.", "", func(context.Context, json.RawMessage) (any, error) {
		info := ServerInfo{
			Name:        "ghx",
			Version:     s.cfg.Version,
			ExecEnabled: s.cfg.AllowExec,
			Methods:     len(s.methods),
			StartedAt:   s.started.UTC().Format(time.RFC3339),
		}
		if s.cfg.Store != nil {
			info.StoreKind = s.cfg.Store.Kind()
			info.StoreLocation = s.cfg.Store.Location()
		}
		if s.cfg.DefaultRepo != nil {
			if repo, err := s.cfg.DefaultRepo(); err == nil && repo != nil {
				info.DefaultRepo = repoLabel(repo)
			}
		}
		return info, nil
	})
}

// ---------------------------------------------------------------------------
// Parameter helpers
// ---------------------------------------------------------------------------

// decodeParams decodes a params object. Missing or null params decode into the
// zero value.
func decodeParams(raw json.RawMessage, v any) error {
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] != '{' {
		return Errorf(CodeInvalidParams, "params must be a JSON object")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return Errorf(CodeInvalidParams, "invalid params: %v", err)
	}
	return nil
}

// resolveRepo parses an explicit repository reference, falling back to the
// server's default repository.
func (s *Server) resolveRepo(raw string) (*gitremote.Repo, error) {
	if strings.TrimSpace(raw) != "" {
		return gitremote.ParseRepo(raw)
	}
	if s.cfg.DefaultRepo == nil {
		return nil, Errorf(CodeInvalidParams, "repo is required: the request did not name one and the server has no default repository")
	}
	repo, err := s.cfg.DefaultRepo()
	if err != nil {
		return nil, Errorf(CodeInvalidParams, "repo is required (server default: %v)", err)
	}
	if repo == nil {
		return nil, Errorf(CodeInvalidParams, "repo is required")
	}
	return repo, nil
}

func (s *Server) client(host string) (*github.Client, error) {
	if s.cfg.ClientFactory == nil {
		return nil, fmt.Errorf("GitHub client is not configured on this server")
	}
	return s.cfg.ClientFactory(host)
}

func repoLabel(repo *gitremote.Repo) string {
	if repo == nil {
		return ""
	}
	return formatRepo(repo.Host, repo.Owner, repo.Name)
}

// formatRepo renders [HOST/]OWNER/REPO, omitting github.com the way the CLI
// does.
func formatRepo(host, owner, name string) string {
	if host == "" || host == "github.com" {
		return owner + "/" + name
	}
	return host + "/" + owner + "/" + name
}

// ---------------------------------------------------------------------------
// cli.run
// ---------------------------------------------------------------------------

// ExecParams selects the command to run and supplies its standard input.
type ExecParams struct {
	Args  []string `json:"args"`
	Stdin string   `json:"stdin,omitempty"`
}

// ExecResult is the captured output and exit status of a CLI command.
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
}

func (s *Server) registerExec() {
	s.add("cli.run", "Run a ghx CLI command on the server and return its output and exit code.", `{"args":["issue","list"],"stdin":""}`, s.execCLI)
}

func (s *Server) execCLI(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ExecParams
	if err := decodeParams(raw, &p); err != nil {
		return nil, err
	}
	if len(p.Args) == 0 {
		return nil, Errorf(CodeInvalidParams, "args must contain at least one CLI argument")
	}

	binary := s.cfg.Exec.Binary
	if binary == "" {
		var err error
		binary, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolving ghx executable: %w", err)
		}
	}
	argv := make([]string, 0, len(s.cfg.Exec.BaseArgs)+len(p.Args))
	argv = append(argv, s.cfg.Exec.BaseArgs...)
	argv = append(argv, p.Args...)

	cmd := exec.CommandContext(ctx, binary, argv...)
	if s.cfg.Exec.Dir != "" {
		cmd.Dir = s.cfg.Exec.Dir
	}
	if s.cfg.Exec.Env != nil {
		cmd.Env = s.cfg.Exec.Env
	} else {
		cmd.Env = os.Environ()
	}
	cmd.Stdin = strings.NewReader(p.Stdin)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if ok := asExitError(err, &exitErr); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return nil, fmt.Errorf("running %s: %w", binary, err)
}

// asExitError is errors.As specialised for exec.ExitError.
func asExitError(err error, target **exec.ExitError) bool {
	exitErr, ok := err.(*exec.ExitError)
	if ok {
		*target = exitErr
	}
	return ok
}
