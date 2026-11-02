package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/logging"
)

// TestResolveString covers the flag > environment > default order shared by the
// global string settings.
func TestResolveString(t *testing.T) {
	cases := []struct {
		name     string
		wasSet   bool
		env      string
		envSet   bool
		current  string
		fallback string
		want     string
	}{
		{name: "flag wins", wasSet: true, env: "from-env", envSet: true, current: "from-flag", fallback: "d", want: "from-flag"},
		{name: "env fills in", env: "from-env", envSet: true, current: "", fallback: "d", want: "from-env"},
		{name: "fallback when unset", current: "", fallback: "d", want: "d"},
		{name: "empty env falls through", env: "", envSet: true, current: "", fallback: "d", want: "d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envSet {
				t.Setenv("GHX_TEST_STRING", tc.env)
			} else {
				os.Unsetenv("GHX_TEST_STRING")
			}
			got := resolveString(tc.wasSet, "GHX_TEST_STRING", tc.current, tc.fallback)
			if got != tc.want {
				t.Errorf("resolveString() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveInt covers precedence, the unset default, and the error raised for
// a value that is not a whole number.
func TestResolveInt(t *testing.T) {
	cases := []struct {
		name    string
		wasSet  bool
		env     string
		envSet  bool
		current int
		want    int
		wantErr bool
	}{
		{name: "flag wins", wasSet: true, env: "7", envSet: true, current: 3, want: 3},
		{name: "env fills in", env: "7", envSet: true, current: 3, want: 7},
		{name: "fallback when unset", current: 3, want: 2},
		{name: "env with spaces", env: " 7 ", envSet: true, current: 3, want: 7},
		{name: "empty env falls through", env: "", envSet: true, current: 3, want: 2},
		{name: "not a number", env: "many", envSet: true, current: 3, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envSet {
				t.Setenv("GHX_TEST_INT", tc.env)
			} else {
				os.Unsetenv("GHX_TEST_INT")
			}
			got, err := resolveInt(tc.wasSet, "GHX_TEST_INT", tc.current, 2)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveInt() = %d, want an error", got)
				}
				if !strings.Contains(err.Error(), "GHX_TEST_INT") {
					t.Errorf("error %q does not name the variable", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveInt: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveInt() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestResolveBool verifies an explicit flag beats a variable in either
// direction, which is what lets one run override an ambient default.
func TestResolveBool(t *testing.T) {
	cases := []struct {
		name     string
		wasSet   bool
		env      string
		envSet   bool
		current  bool
		fallback bool
		want     bool
	}{
		{name: "flag true beats false env", wasSet: true, env: "0", envSet: true, current: true, want: true},
		{name: "flag false beats true env", wasSet: true, env: "1", envSet: true, current: false, want: false},
		{name: "truthy env", env: "yes", envSet: true, current: false, fallback: false, want: true},
		{name: "deny env", env: "off", envSet: true, current: true, fallback: true, want: false},
		{name: "empty env is off", env: "", envSet: true, current: true, fallback: true, want: false},
		{name: "fallback when unset", current: false, fallback: true, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envSet {
				t.Setenv("GHX_TEST_BOOL", tc.env)
			} else {
				os.Unsetenv("GHX_TEST_BOOL")
			}
			got := resolveBool(tc.wasSet, "GHX_TEST_BOOL", tc.current, tc.fallback)
			if got != tc.want {
				t.Errorf("resolveBool() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestGlobalFlagWasSet_IgnoresLocalShadow verifies that a subcommand's own flag
// with a global name (telemetry summary has its own --repo) is not mistaken for
// the global flag, while an unshadowed global flag is still detected.
func TestGlobalFlagWasSet_IgnoresLocalShadow(t *testing.T) {
	newTree := func(t *testing.T, shadow bool) (root, child *cobra.Command, globalRepo *string) {
		t.Helper()
		root = &cobra.Command{Use: "root"}
		globalRepo = new(string)
		root.PersistentFlags().StringVar(globalRepo, "repo", "", "")
		child = &cobra.Command{Use: "child", RunE: func(*cobra.Command, []string) error { return nil }}
		if shadow {
			child.Flags().StringVar(new(string), "repo", "", "")
		}
		root.AddCommand(child)
		return root, child, globalRepo
	}

	t.Run("local flag does not count", func(t *testing.T) {
		root, child, globalRepo := newTree(t, true)
		root.SetArgs([]string{"child", "--repo", "local/repo"})
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if *globalRepo != "" {
			t.Fatalf("the local flag wrote the global variable: %q", *globalRepo)
		}
		if globalFlagWasSet(child, "repo") {
			t.Error("a subcommand-local --repo must not count as the global --repo")
		}
		if !flagWasSet(child, "repo") {
			t.Error("flagWasSet should see the subcommand-local --repo")
		}
	})

	t.Run("persistent flag counts", func(t *testing.T) {
		root, child, globalRepo := newTree(t, false)
		root.SetArgs([]string{"--repo", "global/repo", "child"})
		if err := root.Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if *globalRepo != "global/repo" {
			t.Fatalf("global repo = %q, want the parsed value", *globalRepo)
		}
		if !globalFlagWasSet(child, "repo") {
			t.Error("globalFlagWasSet should see the inherited --repo")
		}
	})
}

// TestApplyEnvFallbacks verifies the global settings pick up their environment
// variables when the flags are absent.
func TestApplyEnvFallbacks(t *testing.T) {
	saved := struct {
		repo, apiURL, cacheDir, storage, logLevel string
	}{repoFlag, apiURLFlag, cacheDir, storageFlag, logLevelFlag}
	t.Cleanup(func() {
		repoFlag, apiURLFlag, cacheDir, storageFlag, logLevelFlag =
			saved.repo, saved.apiURL, saved.cacheDir, saved.storage, saved.logLevel
	})
	repoFlag, apiURLFlag, cacheDir, storageFlag, logLevelFlag = "", "", "", "", "info"

	t.Setenv("GHX_REPO", "acme/widgets")
	t.Setenv("GHX_API_URL", "http://127.0.0.1:9999/graphql")
	t.Setenv("GHX_CACHE_DIR", "/tmp/ghx-cache")
	t.Setenv("GHX_STORAGE", "file")
	t.Setenv("GHX_LOG_LEVEL", "warn")

	applyEnvFallbacks(nil)

	if repoFlag != "acme/widgets" {
		t.Errorf("repoFlag = %q", repoFlag)
	}
	if apiURLFlag != "http://127.0.0.1:9999/graphql" {
		t.Errorf("apiURLFlag = %q", apiURLFlag)
	}
	if cacheDir != "/tmp/ghx-cache" {
		t.Errorf("cacheDir = %q", cacheDir)
	}
	if storageFlag != "file" {
		t.Errorf("storageFlag = %q", storageFlag)
	}
	if logLevelFlag != "warn" {
		t.Errorf("logLevelFlag = %q", logLevelFlag)
	}
}

// TestResolveCacheKnobs verifies the cache command's tuning settings come from
// their variables and that an unparseable value names the variable.
func TestResolveCacheKnobs(t *testing.T) {
	saved := struct{ duration, workers, retries int }{cacheDuration, cacheWorkers, cacheRetries}
	t.Cleanup(func() { cacheDuration, cacheWorkers, cacheRetries = saved.duration, saved.workers, saved.retries })

	t.Run("environment", func(t *testing.T) {
		t.Setenv("GHX_CACHE_DURATION", "15")
		t.Setenv("GHX_CACHE_WORKERS", "4")
		t.Setenv("GHX_CACHE_RETRIES", "0")
		knobs, err := resolveCacheKnobs(nil)
		if err != nil {
			t.Fatalf("resolveCacheKnobs: %v", err)
		}
		want := cacheKnobs{duration: 15, workers: 4, retries: 0}
		if knobs != want {
			t.Errorf("knobs = %+v, want %+v", knobs, want)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		cacheDuration, cacheWorkers, cacheRetries = defaultCacheDuration, defaultCacheWorkers, defaultCacheRetries
		os.Unsetenv("GHX_CACHE_DURATION")
		os.Unsetenv("GHX_CACHE_WORKERS")
		os.Unsetenv("GHX_CACHE_RETRIES")
		knobs, err := resolveCacheKnobs(nil)
		if err != nil {
			t.Fatalf("resolveCacheKnobs: %v", err)
		}
		want := cacheKnobs{duration: defaultCacheDuration, workers: defaultCacheWorkers, retries: defaultCacheRetries}
		if knobs != want {
			t.Errorf("knobs = %+v, want %+v", knobs, want)
		}
	})

	t.Run("flag wins over environment", func(t *testing.T) {
		t.Setenv("GHX_CACHE_WORKERS", "9")
		cacheWorkers = 3
		cmd := &cobra.Command{Use: "cache"}
		cmd.Flags().IntVar(new(int), "workers", 0, "")
		if err := cmd.Flags().Set("workers", "3"); err != nil {
			t.Fatalf("Set(workers): %v", err)
		}
		knobs, err := resolveCacheKnobs(cmd)
		if err != nil {
			t.Fatalf("resolveCacheKnobs: %v", err)
		}
		if knobs.workers != 3 {
			t.Errorf("workers = %d, want the flag value 3", knobs.workers)
		}
	})

	t.Run("invalid value", func(t *testing.T) {
		t.Setenv("GHX_CACHE_WORKERS", "lots")
		if _, err := resolveCacheKnobs(nil); err == nil {
			t.Fatal("expected an error for a non-numeric GHX_CACHE_WORKERS")
		} else if !strings.Contains(err.Error(), "GHX_CACHE_WORKERS") {
			t.Errorf("error %q does not name the variable", err)
		}
	})
}

// TestResolveRPCServe verifies the server bind settings come from their
// variables, including the boolean exposure switch.
func TestResolveRPCServe(t *testing.T) {
	saved := struct {
		host string
		port int
		exec bool
	}{rpcHost, rpcPort, rpcAllowExec}
	t.Cleanup(func() { rpcHost, rpcPort, rpcAllowExec = saved.host, saved.port, saved.exec })

	t.Setenv("GHX_RPC_HOST", "0.0.0.0")
	t.Setenv("GHX_RPC_PORT", "9000")
	t.Setenv("GHX_RPC_ALLOW_EXEC", "0")

	got, err := resolveRPCServe(nil)
	if err != nil {
		t.Fatalf("resolveRPCServe: %v", err)
	}
	want := rpcServeSettings{host: "0.0.0.0", port: 9000, allowExec: false}
	if got != want {
		t.Errorf("settings = %+v, want %+v", got, want)
	}

	t.Setenv("GHX_RPC_PORT", "not-a-port")
	if _, err := resolveRPCServe(nil); err == nil {
		t.Fatal("expected an error for a non-numeric GHX_RPC_PORT")
	}
}

// TestApplyEnvFallbacks_LogLevelApplies proves the resolved level reaches the
// logger, through the real command tree and PersistentPreRunE.
func TestApplyEnvFallbacks_LogLevelApplies(t *testing.T) {
	savedLevel := logLevelFlag
	t.Cleanup(func() {
		logLevelFlag = savedLevel
		logging.SetOutput(os.Stderr)
		_ = logging.SetLevel("info")
		rootCmd.SetArgs(nil)
	})

	t.Setenv("GHX_LOG_LEVEL", "debug")
	t.Setenv("GHX_TELEMETRY", "0")
	t.Setenv("GHX_TELEMETRY_DB", filepath.Join(t.TempDir(), "telemetry.db"))
	_ = logging.SetLevel("info")

	rootCmd.SetArgs([]string{"telemetry", "status"})
	captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("rootCmd.Execute: %v", err)
		}
	})

	if logLevelFlag != "debug" {
		t.Fatalf("logLevelFlag = %q, want debug", logLevelFlag)
	}
	// SetLevel kept the same level pointer, so redirecting the output here
	// shows whether the resolved level was applied.
	var buf bytes.Buffer
	logging.SetOutput(&buf)
	logging.Debug("level probe")
	if !strings.Contains(buf.String(), "level probe") {
		t.Errorf("GHX_LOG_LEVEL=debug did not enable debug logging; got %q", buf.String())
	}
}
