// Package cmd implements the CLI commands for the workload-exporter tool.
package cmd

import (
	"time"

	"github.com/cockroachlabs/workload-exporter/pkg/connect"
	"github.com/cockroachlabs/workload-exporter/pkg/export"
	"github.com/spf13/cobra"
)

var connectionUrlFlag string
var urlFlag string
var outputFileFlag string
var startFlag string
var endFlag string
var hostFlag string
var portFlag int
var userFlag string
var databaseFlag string
var insecureFlag bool
var certsDirFlag string

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export cluster workload",
	RunE: func(cmd *cobra.Command, args []string) error {

		start, err := time.Parse(time.RFC3339, startFlag)
		if err != nil {
			return err
		}
		end, err := time.Parse(time.RFC3339, endFlag)
		if err != nil {
			return err
		}

		connURL, err := connect.ResolveConnectionURL(connect.ConnectionConfig{
			URL:          urlFlag,
			URLSet:       cmd.Flags().Changed("url"),
			LegacyURL:    connectionUrlFlag,
			LegacyURLSet: cmd.Flags().Changed("connection-url"),
			Host:         hostFlag,
			Port:         portFlag,
			User:         userFlag,
			UserSet:      cmd.Flags().Changed("user"),
			Database:     databaseFlag,
			DatabaseSet:  cmd.Flags().Changed("database"),
			Insecure:     insecureFlag,
			InsecureSet:  cmd.Flags().Changed("insecure"),
			CertsDir:     certsDirFlag,
			CertsDirSet:  cmd.Flags().Changed("certs-dir"),
		})
		if err != nil {
			return err
		}

		exporter, err := export.NewExporter(export.Config{
			ConnectionString: connURL,
			OutputFile:       outputFileFlag,
			TimeRange: export.TimeRange{
				Start: start,
				End:   end,
			},
		})

		if err != nil {
			return err
		}
		defer exporter.Close() //nolint:errcheck // Error from Close in defer is intentionally ignored

		return exporter.Export()
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)

	exportCmd.Flags().StringVarP(&connectionUrlFlag, "connection-url", "c", "", "connection URL (deprecated, use --url)")
	_ = exportCmd.Flags().MarkDeprecated("connection-url", "use --url instead")
	exportCmd.Flags().StringVar(&urlFlag, "url", "", "connection URL, e.g. postgresql://user@host:26257/db?sslmode=verify-full\n(env: COCKROACH_URL)")
	exportCmd.Flags().StringVarP(&outputFileFlag, "output-file", "o", "workload-export.zip", "output file")
	exportCmd.Flags().StringVarP(&startFlag, "start", "s", defaultStartFlag(), "start time")
	exportCmd.Flags().StringVarP(&endFlag, "end", "e", defaultEndFlag(), "end time")

	exportCmd.Flags().StringVar(&hostFlag, "host", "localhost", "database host")
	exportCmd.Flags().IntVar(&portFlag, "port", 26257, "database port")
	exportCmd.Flags().StringVarP(&userFlag, "user", "u", "root", "database user\n(env: COCKROACH_USER)")
	exportCmd.Flags().StringVarP(&databaseFlag, "database", "d", "", "database name\n(env: COCKROACH_DATABASE)")
	exportCmd.Flags().BoolVar(&insecureFlag, "insecure", false, "connect without TLS (env: COCKROACH_INSECURE)")
	exportCmd.Flags().StringVar(&certsDirFlag, "certs-dir", connect.DefaultCertsDir(), "path to certificate directory\n(env: COCKROACH_CERTS_DIR)")
}

func defaultStartFlag() string {
	return time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
}

func defaultEndFlag() string {
	return time.Now().UTC().Add(+1 * time.Hour).Format(time.RFC3339)
}
