package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cockroachlabs/workload-exporter/internal/update"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newUpdateCmd())
}

func newUpdateCmd() *cobra.Command {
	var checkOnly bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update workload-exporter to the latest release",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("workload-exporter version %s\n", Version)

			if checkOnly {
				return runUpdateCheck(cmd.Context())
			}

			return update.PerformUpdate(cmd.Context(), os.Stdout, Version)
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false, "check for updates without installing")

	return cmd
}

func runUpdateCheck(ctx context.Context) error {
	if Version == "" || Version == "dev" {
		fmt.Println("dev build, skipping update check")
		fmt.Println("hint: use 'make build' to embed the version")
		return nil
	}

	fmt.Println("checking for updates...")

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := update.Check(ctx, Version)
	if err != nil {
		return fmt.Errorf("update check failed: %w", err)
	}

	if result.TagName == Version {
		fmt.Println("up to date")
	} else {
		fmt.Printf("\nnew version available: %s (current: %s)\n", result.TagName, Version)
		fmt.Printf("release: %s\n", result.HTMLURL)
		fmt.Println("\nTo update: workload-exporter update")
	}

	return nil
}
