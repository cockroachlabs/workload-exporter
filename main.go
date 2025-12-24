// Package main is the entry point for the workload-exporter CLI tool.
package main

import "github.com/cockroachlabs/workload-exporter/cmd"

// Version information set via ldflags during build
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	// Pass version info to cmd package
	cmd.SetVersionInfo(Version, Commit, BuildDate)
	cmd.Execute()
}
