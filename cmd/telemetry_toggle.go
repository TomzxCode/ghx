package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tomzxcode/ghx/internal/telemetry"
)

var telemetryEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Turn on telemetry recording globally",
	Long: `Persists the global telemetry preference so future runs record API and cache
timings without passing --telemetry. Recording is local-only and never uploaded.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := telemetry.ConfigPath(resolveTelemetryDBPath())
		if err := telemetry.SaveConfig(path, telemetry.Config{Enabled: true}); err != nil {
			return fmt.Errorf("saving telemetry config: %w", err)
		}
		fmt.Printf("Telemetry enabled (%s).\n", path)
		return nil
	},
}

var telemetryDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Turn off telemetry recording globally",
	Long: `Persists the global telemetry preference so future runs do not record. Existing
recorded events are kept; use ` + "`ghx telemetry clear`" + ` to delete them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path := telemetry.ConfigPath(resolveTelemetryDBPath())
		if err := telemetry.SaveConfig(path, telemetry.Config{Enabled: false}); err != nil {
			return fmt.Errorf("saving telemetry config: %w", err)
		}
		fmt.Printf("Telemetry disabled (%s).\n", path)
		return nil
	},
}

var telemetryStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether telemetry is enabled and where it writes",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := telemetry.ConfigPath(resolveTelemetryDBPath())
		dbPath := resolveTelemetryDBPath()
		cfg, configured := telemetry.LoadConfig(cfgPath)

		state := "disabled"
		switch {
		case telemetryFlag != nil:
			if *telemetryFlag {
				state = "enabled (this run, via --telemetry)"
			} else {
				state = "disabled (this run, via --telemetry=false)"
			}
		case os.Getenv("GHX_TELEMETRY") != "":
			if envTruthy(os.Getenv("GHX_TELEMETRY")) {
				state = "enabled (via GHX_TELEMETRY)"
			} else {
				state = "disabled (via GHX_TELEMETRY)"
			}
		case configured:
			if cfg.Enabled {
				state = "enabled"
			} else {
				state = "disabled"
			}
		default:
			if telemetry.DefaultConfig().Enabled {
				state = "enabled (default)"
			}
		}

		fmt.Printf("Telemetry:   %s\n", state)
		fmt.Printf("Database:    %s\n", dbPath)
		fmt.Printf("Config:      %s\n", cfgPath)
		if n := recordedEvents(dbPath); n >= 0 {
			fmt.Printf("Events:      %d\n", n)
		}
		return nil
	},
}

// recordedEvents returns the number of stored events, or -1 when the database
// does not exist or cannot be read.
func recordedEvents(dbPath string) int {
	if _, err := os.Stat(dbPath); err != nil {
		return -1
	}
	store, err := telemetry.Open(dbPath)
	if err != nil {
		return -1
	}
	defer store.Close()
	n, err := store.Count(telemetry.Filter{})
	if err != nil {
		return -1
	}
	return n
}

func init() {
	telemetryCmd.AddCommand(telemetryEnableCmd)
	telemetryCmd.AddCommand(telemetryDisableCmd)
	telemetryCmd.AddCommand(telemetryStatusCmd)
}
