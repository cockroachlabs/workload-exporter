package export

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v4"
	"github.com/sirupsen/logrus"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const ExporterVersion = "1.0.0"

var systemDatabases = []string{"system", "crdb_internal", "postgres"}

type Exporter struct {
	Config                Config
	Db                    *pgx.Conn
	CleanConnectionString string
}

type Config struct {
	ConnectionString string
	OutputFile       string
	TimeRange        TimeRange
}

type TimeRange struct {
	Start time.Time
	End   time.Time
}

type Metadata struct {
	Version                     string        `json:"version"`
	Timestamp                   time.Time     `json:"timestamp"`
	ExportConfig                Config        `json:"export_config"`
	ClusterVersion              string        `json:"cluster_version"`
	SqlStatsAggregationInterval time.Duration `json:"sql.stats.aggregation.interval"`
	SqlStatsFlushInterval       time.Duration `json:"sql.stats.flush.interval"`
}

type Table struct {
	Database   string
	Name       string
	TimeColumn string
}

var exportTables = []Table{
	Table{Database: "crdb_internal", Name: "statement_statistics", TimeColumn: "aggregated_ts"},
	Table{Database: "crdb_internal", Name: "transaction_statistics", TimeColumn: "aggregated_ts"},
	Table{Database: "crdb_internal", Name: "transaction_contention_events", TimeColumn: "collection_ts"},
	Table{Database: "crdb_internal", Name: "gossip_nodes", TimeColumn: ""},
}

func NewExporter(config Config) (*Exporter, error) {
	ctx := context.Background()
	cleanConnStr, err := cleanConnectionString(config.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to clean connection string %w", err)
	}

	logrus.Infof("connecting to cluster at '%s'", cleanConnStr)
	conn, err := pgx.Connect(ctx, config.ConnectionString)
	if err != nil {
		return nil, err
	}
	exporter := Exporter{Config: config, Db: conn, CleanConnectionString: cleanConnStr}
	return &exporter, nil
}

func (exporter *Exporter) Export() error {

	logrus.Info("starting export")
	logrus.Infof("using time range: %s - %s", exporter.Config.TimeRange.Start, exporter.Config.TimeRange.End)
	ctx := context.Background()

	tempDir, err := os.MkdirTemp("", "crdb-export-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	logrus.Infof("created temp directory at '%s'", tempDir)
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			logrus.Debugf("failed to remove temp directory: %w", err)
		}
	}(tempDir)

	logrus.Info("collecting cluster metadata")
	clusterVersion, err := exporter.clusterVersion()
	if err != nil {
		return fmt.Errorf("failed to get cluster version: %w", err)
	}

	agg, err := exporter.sqlStatsAggregationInterval()
	if err != nil {
		return fmt.Errorf("failed to get aggregation interval: %w", err)
	}

	flush, err := exporter.sqlStatsFlushInterval()
	if err != nil {
		return fmt.Errorf("failed to get flush interval: %w", err)
	}

	metadata := Metadata{
		Version:   ExporterVersion,
		Timestamp: time.Now(),
		ExportConfig: Config{
			ConnectionString: exporter.CleanConnectionString, // make sure to use clean connection string
			OutputFile:       exporter.Config.OutputFile,
			TimeRange:        exporter.Config.TimeRange,
		},
		ClusterVersion:              clusterVersion,
		SqlStatsAggregationInterval: agg,
		SqlStatsFlushInterval:       flush,
	}

	logrus.Infof("exporting database schemas")

	dbs, err := exporter.userDatabases()
	if err != nil {
		return fmt.Errorf("failed to get user databases: %w", err)
	}
	for _, db := range dbs {
		logrus.Infof("  exporting database %s", db)
		err := exporter.exportCreateStatements(ctx, db, tempDir)
		if err != nil {
			return err
		}
	}

	logrus.Info("exporting all zone configurations")
	err = exporter.exportAllZoneConfigurations(ctx, tempDir)
	if err != nil {
		return fmt.Errorf("failed to export all zone configurations: %w", err)
	}

	logrus.Info("starting table export")
	for _, table := range exportTables {

		logrus.Infof(" exporting table '%s.%s'", table.Database, table.Name)
		if err := exporter.exportTable(ctx, tempDir, table, agg); err != nil { // exportTableData(ctx, conn, dbName, tableName, dataFile); err != nil {
			return fmt.Errorf("failed to export data for table %s.%s: %w", table.Database, table.Name, err)
		}
	}
	logrus.Info("finished table export")

	metadataFile := filepath.Join(tempDir, "metadata.json")
	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataFile, metadataJSON, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %w", err)
	}

	// Create zip file
	logrus.Infof("creating zip file at '%s'", exporter.Config.OutputFile)
	if err := exporter.createZipFile(tempDir); err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}

	logrus.Infof("Export completed successfully: %s\n", exporter.Config.OutputFile)
	return nil

}

func (exporter *Exporter) clusterVersion() (string, error) {
	r := exporter.Db.QueryRow(context.Background(), "SELECT version()")
	var version string
	err := r.Scan(&version)
	return version, err

}

// sql.stats.aggregation.interval
// sql.stats.flush.interval
func (exporter *Exporter) sqlStatsAggregationInterval() (time.Duration, error) {

	r := exporter.Db.QueryRow(context.Background(), "SHOW CLUSTER SETTING sql.stats.aggregation.interval")
	var d time.Duration
	if err := r.Scan(&d); err != nil {
		return d, fmt.Errorf("failed to get sql.stats.aggregation.interval: %w", err)
	}

	return d, nil

}

func (exporter *Exporter) sqlStatsFlushInterval() (time.Duration, error) {

	r := exporter.Db.QueryRow(context.Background(), "SHOW CLUSTER SETTING sql.stats.flush.interval")
	var d time.Duration
	if err := r.Scan(&d); err != nil {
		return d, fmt.Errorf("failed to get sql.stats.flush.interval: %w", err)
	}

	return d, nil

}

func (exporter *Exporter) exportAllZoneConfigurations(ctx context.Context, tempDir string) error {

	dataFile := filepath.Join(tempDir, "zone_configurations.txt")

	// Create output file
	file, err := os.Create(dataFile)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logrus.Errorf("failed to close file: %s", err)
		}
	}(file)

	rows, err := exporter.Db.Query(ctx, "with z AS (SHOW ALL ZONE CONFIGURATIONS) SELECT raw_config_sql FROM z WHERE raw_config_sql IS NOT NULL")

	if err != nil {
		return fmt.Errorf("failed to query z configurations: %w", err)
	}

	var configs []string
	for rows.Next() {
		var config string
		err := rows.Scan(&config)
		if err != nil {
			return err
		}
		configs = append(configs, config)
	}

	if err := os.WriteFile(dataFile, []byte(strings.Join(configs, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write zone configurations file: %w", err)
	}

	return nil

}

func (exporter *Exporter) exportCreateStatements(ctx context.Context, db string, tempDir string) error {

	filename := fmt.Sprintf("%s.schema.txt", db)
	dataFile := filepath.Join(tempDir, filename)

	// Create output file
	file, err := os.Create(dataFile)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logrus.Errorf("failed to close file: %s", err)
		}
	}(file)

	creates, err := exporter.createStatements(db)
	if err != nil {
		return err
	}

	if err := os.WriteFile(dataFile, []byte(strings.Join(creates, "\n")), 0644); err != nil {
		return fmt.Errorf("failed to write create statements file: %w", err)
	}

	return nil

}

func (exporter *Exporter) createStatements(db string) ([]string, error) {

	var creates []string

	_, err := exporter.Db.Exec(context.Background(), fmt.Sprintf("USE %s", db))
	if err != nil {
		return creates, err
	}

	rows, err := exporter.Db.Query(context.Background(), "SELECT create_statement FROM [SHOW CREATE ALL TABLES]")

	if err != nil {
		return creates, err
	}

	for rows.Next() {
		var create string
		err := rows.Scan(&create)
		if err != nil {
			return nil, err
		}
		creates = append(creates, create)
	}

	return creates, nil

}

func (exporter *Exporter) userDatabases() ([]string, error) {
	var databases []string
	sql := "SELECT database_name FROM [SHOW DATABASES]"

	rows, err := exporter.Db.Query(context.Background(), sql)
	if err != nil {
		return nil, err
	}

	var db string
	for rows.Next() {
		err := rows.Scan(&db)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(systemDatabases, db) {
			databases = append(databases, db)
		}
	}
	return databases, nil
}

func (exporter *Exporter) exportTable(ctx context.Context, dir string, table Table, aggregationInterval time.Duration) error {
	filename := fmt.Sprintf("%s.%s.csv", table.Database, table.Name)
	dataFile := filepath.Join(dir, filename)

	// Create output file
	file, err := os.Create(dataFile)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logrus.Errorf("failed to close file: %w", err)
		}
	}(file)

	// Get column names
	rows, err := exporter.Db.Query(ctx, fmt.Sprintf("SELECT * FROM %s.%s LIMIT 0", table.Database, table.Name))
	if err != nil {
		return err
	}

	fieldDescriptions := rows.FieldDescriptions()
	rows.Close()

	// Write CSV header
	var headers []string
	for _, fd := range fieldDescriptions {
		headers = append(headers, string(fd.Name))
	}

	_, err = file.WriteString(strings.Join(headers, ",") + "\n")
	if err != nil {
		return err
	}

	// Use a SQL query to export data in CSV format
	var where string
	if table.TimeColumn != "" {
		where = fmt.Sprintf("WHERE %s BETWEEN '%s' and '%s'",
			table.TimeColumn,
			startTime(exporter.Config.TimeRange.Start).Format("2006-01-02 15:04:05"), // offset for aggregation interval -- TODO
			endTime(exporter.Config.TimeRange.End).Format("2006-01-02 15:04:05"),
		)
	}
	copyQuery := fmt.Sprintf("COPY (SELECT * FROM %s.%s %s) TO STDOUT WITH CSV", table.Database, table.Name, where)
	logrus.Info(copyQuery)
	_, err = exporter.Db.PgConn().CopyTo(ctx, file, copyQuery)
	if err != nil {
		return err
	}

	return nil
}

func (exporter *Exporter) createZipFile(sourceDir string) error {
	zipFile, err := os.Create(exporter.Config.OutputFile)
	if err != nil {
		return err
	}
	defer func(zipFile *os.File) {
		err := zipFile.Close()
		if err != nil {
			logrus.Debugf("failed to close zip file: %w", err)
		}
	}(zipFile)

	zipWriter := zip.NewWriter(zipFile)
	defer func(zipWriter *zip.Writer) {
		err := zipWriter.Close()
		if err != nil {
			logrus.Debugf("failed to close zip writer: %w", err)
		}
	}(zipWriter)

	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		zipFile, err := zipWriter.Create(relPath)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func(file *os.File) {
			err := file.Close()
			if err != nil {
				logrus.Debugf("failed to close zip file: %w", err)
			}
		}(file)

		_, err = io.Copy(zipFile, file)
		return err
	})

	return err
}

func startTime(t time.Time) time.Time { // TODO - consider aggregation interval
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

func endTime(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 59, 59, 0, t.Location())
}

func cleanConnectionString(connStr string) (string, error) {
	/*
		if !strings.HasPrefix(connStr, "postgresql://") {
			return "", fmt.Errorf("invalid connection string: must start with postgresql://")
		}

	*/

	// Parse the connection string as a URL
	u, err := url.Parse(connStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Remove the password, keep the username
	if u.User != nil {
		username := u.User.Username()
		u.User = url.User(username)
	}

	return u.String(), nil
}
