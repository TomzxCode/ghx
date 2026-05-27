package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tomzxcode/ghx/internal/version"
)

var rootCmd = &cobra.Command{
	Use:          "ghx",
	Short:        "Extended GitHub CLI",
	Version:      version.Version,
	SilenceUsage: true,
	SilenceErrors: true,
	Long: `ghx is an extended GitHub CLI that provides additional functionality
beyond what the regular gh CLI offers.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}
