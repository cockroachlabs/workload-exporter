package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version information - set via ldflags in main package
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// SetVersionInfo sets the version information from main package
func SetVersionInfo(version, commit, buildDate string) {
	Version = version
	Commit = commit
	BuildDate = buildDate
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("workload-exporter version %s\n", Version)
		fmt.Printf("Commit: %s\n", Commit)
		fmt.Printf("Built:  %s\n", BuildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
