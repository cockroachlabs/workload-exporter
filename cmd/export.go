package cmd

import (
	"github.com/cockroachlabs/workload-exporter/pkg/export"
	"github.com/spf13/cobra"
	"time"
)

var connectionUrlFlag string
var outputFileFlag string
var startFlag string
var endFlag string

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

		exporter, err := export.NewExporter(export.Config{
			ConnectionString: connectionUrlFlag,
			OutputFile:       outputFileFlag,
			TimeRange: export.TimeRange{
				Start: start,
				End:   end,
			},
		})

		if err != nil {
			return err
		}
		defer exporter.Close()

		err = exporter.Export()

		if err != nil {
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)

	exportCmd.Flags().StringVarP(&connectionUrlFlag, "connection-url", "c", "", "connection url")
	exportCmd.Flags().StringVarP(&outputFileFlag, "output-file", "o", "workload-export.zip", "output file")
	exportCmd.Flags().StringVarP(&startFlag, "start", "s", defaultStartFlag(), "start time")
	exportCmd.Flags().StringVarP(&endFlag, "end", "e", defaultEndFlag(), "end time")
}

func defaultStartFlag() string {
	return time.Now().UTC().Add(-6 * time.Hour).Format(time.RFC3339)
}

func defaultEndFlag() string {
	return time.Now().UTC().Add(+1 * time.Hour).Format(time.RFC3339)
}
