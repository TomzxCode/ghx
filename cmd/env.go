package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// This file holds the shared resolution of settings that have both a flag and an
// environment variable. The order is always flag, then environment variable,
// then default, so a flag overrides an ambient setting for one invocation.
// Resolvers take the "was the flag given" answer rather than a command, which
// keeps them usable from the command functions tests call directly.

// flagWasSet reports whether the named flag of cmd was given explicitly.
func flagWasSet(cmd *cobra.Command, name string) bool {
	return cmd != nil && cmd.Flags().Changed(name)
}

// globalFlagWasSet reports whether a root persistent flag was given explicitly.
// It reads the root's own persistent flag set because a subcommand can declare a
// local flag with the same name (telemetry summary has its own --repo); such a
// shadow must not be mistaken for the global flag.
func globalFlagWasSet(cmd *cobra.Command, name string) bool {
	if cmd == nil {
		return false
	}
	return cmd.Root().PersistentFlags().Changed(name)
}

// resolveString resolves a string setting from the flag, then the environment
// variable. current is the flag's bound value, and it already holds the flag's
// default when the flag was not given, so it is also the result when neither
// applies. Callers that set the bound value without parsing arguments (tests)
// therefore keep their value.
func resolveString(wasSet bool, env, current string) string {
	if wasSet {
		return current
	}
	if v := os.Getenv(env); v != "" {
		return v
	}
	return current
}

// resolveInt resolves an integer setting. A value that is not a whole number is
// reported with an error naming the variable, so the fix is obvious.
func resolveInt(wasSet bool, env string, current int) (int, error) {
	if wasSet {
		return current, nil
	}
	raw := strings.TrimSpace(os.Getenv(env))
	if raw == "" {
		return current, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: use a whole number", env, raw)
	}
	return n, nil
}

// resolveBool resolves a boolean setting. An explicit --flag=false wins over a
// truthy variable, so a single run can override an ambient default.
func resolveBool(wasSet bool, env string, current bool) bool {
	if wasSet {
		return current
	}
	if raw, ok := os.LookupEnv(env); ok {
		return envTruthy(raw)
	}
	return current
}

// envTruthy parses an environment value as a boolean, treating the common deny
// values as off.
func envTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "no", "off", "":
		return false
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// applyEnvFallbacks fills the global flag variables from their environment
// variables when the corresponding flag was not given. It runs in the root
// PersistentPreRunE, before any command reads those variables.
func applyEnvFallbacks(cmd *cobra.Command) {
	logLevelFlag = resolveString(globalFlagWasSet(cmd, "log-level"), "GHX_LOG_LEVEL", logLevelFlag)
	cacheDir = resolveString(globalFlagWasSet(cmd, "cache-dir"), "GHX_CACHE_DIR", cacheDir)
	repoFlag = resolveString(globalFlagWasSet(cmd, "repo"), "GHX_REPO", repoFlag)
	apiURLFlag = resolveString(globalFlagWasSet(cmd, "api-url"), "GHX_API_URL", apiURLFlag)
	storageFlag = resolveString(globalFlagWasSet(cmd, "storage"), "GHX_STORAGE", storageFlag)
}
