// Package export provides functionality for exporting workload data from CockroachDB clusters.
// It exports statistics, schemas, and configurations into a portable zip file format.
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

// ExporterVersion is the current version of the exporter tool.
const ExporterVersion = "1.0.0"

var systemDatabases = []string{"system", "crdb_internal", "postgres"}

// TenantScope indicates which virtual cluster a table or query should be routed to.
// In non-virtualized clusters, all queries use the single connection regardless of scope.
type TenantScope string

const (
	// TenantScopeMain routes the query to the main (application) virtual cluster.
	// This is the default when no scope is specified.
	TenantScopeMain TenantScope = "main"
	// TenantScopeSystem routes the query to the system virtual cluster.
	// Used for cluster-wide data not available in application virtual clusters,
	// such as gossip_nodes. Auto-detection occurs on first failure.
	TenantScopeSystem TenantScope = "system"
	// TenantScopeBoth routes the query to both virtual clusters.
	// The main virtual cluster is always exported. In virtualized clusters, the system
	// virtual cluster is also exported with a ".system" filename suffix (e.g.,
	// crdb_internal.cluster_settings.system.csv). The system export is best-effort: if the
	// system connection cannot be established, it is skipped with a warning.
	TenantScopeBoth TenantScope = "both"
)

// Exporter handles the export of workload data from a CockroachDB cluster.
// It manages database connections and coordinates the export of statistics,
// schemas, and configurations into a zip file.
type Exporter struct {
	// Config contains the export configuration settings
	Config Config
	// Db is the active database connection to the CockroachDB cluster (main virtual cluster)
	Db *pgx.Conn
	// SystemDb is a connection to the system virtual cluster, established lazily when a
	// TenantScopeSystem query fails against Db with a virtual cluster error. Nil in
	// non-virtualized clusters.
	SystemDb *pgx.Conn
	// CleanConnectionString is the connection string with password redacted
	CleanConnectionString string
}

// Config defines the configuration for a workload export operation.
type Config struct {
	// ConnectionString is the PostgreSQL connection URL for the CockroachDB cluster
	ConnectionString string
	// OutputFile is the path to the output zip file (default: "workload-export.zip")
	OutputFile string
	// TimeRange specifies the time window for filtering exported statistics
	TimeRange TimeRange
}

// TimeRange defines a time window for filtering exported data.
type TimeRange struct {
	// Start is the beginning of the time range (inclusive)
	Start time.Time
	// End is the end of the time range (inclusive)
	End time.Time
}

// Metadata contains information about the exported data and cluster configuration.
// This is serialized to metadata.json in the export zip file.
type Metadata struct {
	Version                     string        `json:"version"`
	Timestamp                   time.Time     `json:"timestamp"`
	ExportConfig                Config        `json:"export_config"`
	ClusterVersion              string        `json:"cluster_version"`
	ClusterId                   string        `json:"cluster_id"`
	ClusterName                 string        `json:"cluster_name"`
	Organization                string        `json:"organization"`
	SqlStatsAggregationInterval time.Duration `json:"sql.stats.aggregation.interval"`
	SqlStatsFlushInterval       time.Duration `json:"sql.stats.flush.interval"`
	VirtualCluster              bool          `json:"virtual_cluster"`
}

// Table represents a CockroachDB table to be exported with optional time-based filtering.
type Table struct {
	// Database is the name of the database containing the table
	Database string
	// Name is the table name
	Name string
	// TimeColumn is the column name used for time-based filtering (empty if not applicable)
	TimeColumn string
	// Optional indicates that export failures should be logged as warnings rather than errors.
	// Use this for tables that may not be available in all cluster configurations (e.g. Cloud virtual clusters).
	Optional bool
	// Scope indicates which virtual cluster connection to use for this table.
	// Defaults to TenantScopeMain when unset.
	Scope TenantScope
	// RedactKeyColumn is the column used to identify rows whose sensitive column should be redacted.
	// Set together with RedactColumn and RedactedKeys.
	RedactKeyColumn string
	// RedactColumn is the column whose value is replaced with "<redacted>" for matching rows.
	RedactColumn string
	// RedactedKeys is the set of RedactKeyColumn values for which RedactColumn is redacted.
	RedactedKeys []string
	// Query overrides the default SELECT for this table. When set, the query is used as-is
	// for both column discovery and data export. TimeColumn, RedactKeyColumn, RedactColumn,
	// and RedactedKeys are ignored when Query is set. The output filename is still derived
	// from Database and Name.
	Query string
}

// sensitiveClusterSettings is the list of cluster setting names whose values are
// redacted in the export to avoid leaking secrets or credentials.
var sensitiveClusterSettings = []string{
	// Sensitive settings (contain credentials, keys, or PEM data)
	"server.host_based_authentication.configuration",
	"server.identity_map.configuration",
	"server.jwt_authentication.issuers.custom_ca",
	"server.ldap_authentication.domain.custom_ca",
	"server.ldap_authentication.client.tls_certificate",
	"server.ldap_authentication.client.tls_key",
	"server.oidc_authentication.client_id",
	"server.oidc_authentication.client_secret",
	"server.oidc_authentication.provider.custom_ca",
	"sql.override.allow_unsafe_internals.enabled",
	// Non-reportable settings (always redacted in telemetry)
	"cluster.secret",
	"cluster.label",
	"enterprise.license",
}

var exportTables = []Table{
	{Database: "crdb_internal", Name: "statement_statistics", TimeColumn: "aggregated_ts", Scope: TenantScopeMain},
	{Database: "crdb_internal", Name: "transaction_statistics", TimeColumn: "aggregated_ts", Scope: TenantScopeMain},
	{Database: "crdb_internal", Name: "transaction_contention_events", TimeColumn: "collection_ts", Scope: TenantScopeMain},
	{Database: "crdb_internal", Name: "gossip_nodes", TimeColumn: "", Optional: true, Scope: TenantScopeSystem},
	{
		Database: "crdb_internal",
		Name:     "node_cpu_mem",
		Optional: true,
		Scope:    TenantScopeSystem,
		Query: `SELECT node_id, address,` +
			` ROUND(` +
			`((metrics->>'sys.cpu.user.percent')::FLOAT + (metrics->>'sys.cpu.sys.percent')::FLOAT)` +
			` / NULLIF((metrics->>'sys.cpu.combined.percent-normalized')::FLOAT, 0)` +
			`)::INT AS num_vcpus,` +
			` ROUND((metrics->>'sys.totalmem')::FLOAT / 1073741824, 1) AS total_mem_gib` +
			` FROM crdb_internal.kv_node_status`,
	},
	{Database: "", Name: "crdb_internal.table_indexes", TimeColumn: "", Scope: TenantScopeMain},           // Use "" to query across all databases
	{Database: "", Name: "crdb_internal.index_usage_statistics", TimeColumn: "", Scope: TenantScopeMain}, // Use "" to query across all databases
	{Database: "system", Name: "table_statistics", TimeColumn: "", Scope: TenantScopeMain},
	{
		Database:        "crdb_internal",
		Name:            "cluster_settings",
		TimeColumn:      "",
		Scope:           TenantScopeBoth,
		RedactKeyColumn: "variable",
		RedactColumn:    "value",
		RedactedKeys:    sensitiveClusterSettings,
	},
	{
		Database:        "system",
		Name:            "settings",
		TimeColumn:      "",
		Scope:           TenantScopeBoth,
		RedactKeyColumn: "name",
		RedactColumn:    "value",
		RedactedKeys:    sensitiveClusterSettings,
	},
}

// NewExporter creates a new Exporter instance with the given configuration.
// It establishes a database connection to the CockroachDB cluster and prepares for data export.
// The connection string password is redacted in CleanConnectionString for logging purposes.
//
// Returns an error if the connection fails or the connection string is invalid.
//
// Example:
//
//	config := export.Config{
//	    ConnectionString: "postgresql://user:password@host:26257/?sslmode=verify-full",
//	    OutputFile:       "export.zip",
//	    TimeRange: export.TimeRange{
//	        Start: time.Now().Add(-2 * time.Hour),
//	        End:   time.Now(),
//	    },
//	}
//	exporter, err := export.NewExporter(config)
//	if err != nil {
//	    return err
//	}
//	defer exporter.Close()
func NewExporter(config Config) (*Exporter, error) {
	ctx := context.Background()
	cleanConnStr, err := cleanConnectionString(config.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to clean connection string: %w", err)
	}

	logrus.Infof("connecting to cluster at '%s'", cleanConnStr)
	conn, err := pgx.Connect(ctx, config.ConnectionString)
	if err != nil {
		return nil, err
	}

	// Enable access to crdb_internal for CockroachDB 26.1+
	// Starting in v26.1, the allow_unsafe_internals setting defaults to false
	// and must be explicitly enabled to access crdb_internal tables
	if err := enableUnsafeInternalsIfNeeded(ctx, conn); err != nil {
		if closeErr := conn.Close(ctx); closeErr != nil {
			logrus.WithError(closeErr).Debug("failed to close connection after configuration error")
		}
		return nil, fmt.Errorf("failed to configure database access: %w", err)
	}

	exporter := Exporter{Config: config, Db: conn, CleanConnectionString: cleanConnStr}
	return &exporter, nil
}

// Close closes the database connection.
// It should be called when the Exporter is no longer needed, typically using defer.
//
// Example:
//
//	exporter, err := export.NewExporter(config)
//	if err != nil {
//	    return err
//	}
//	defer exporter.Close()
func (exporter *Exporter) Close() error {
	if exporter.SystemDb != nil {
		_ = exporter.SystemDb.Close(context.Background())
	}
	if exporter.Db != nil {
		return exporter.Db.Close(context.Background())
	}
	return nil
}

// Export performs the complete workload export operation.
// It exports the following data into a zip file:
//   - Cluster metadata (version, ID, name, organization, settings)
//   - Database schemas (CREATE statements for all user databases)
//   - Zone configurations
//   - Statistics tables (statement_statistics, transaction_statistics, transaction_contention_events, gossip_nodes, node_cpu_mem, table_indexes across all databases, system.table_statistics)
//   - Cluster settings (crdb_internal.cluster_settings, system.settings) with sensitive values redacted
//
// The statistics tables are filtered by the TimeRange specified in Config.
// In virtualized clusters, tables with TenantScopeBoth are exported once per virtual cluster,
// with the system virtual cluster export using a ".system" filename suffix.
// All exported data is written to the OutputFile specified in Config.
//
// Returns an error if any step of the export process fails.
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
			logrus.WithError(err).Debug("failed to remove temp directory")
		}
	}(tempDir)

	logrus.Info("collecting cluster metadata")
	clusterVersion, err := exporter.clusterVersion()
	if err != nil {
		return fmt.Errorf("failed to get cluster version: %w", err)
	}

	clusterId, err := exporter.clusterId()
	if err != nil {
		return fmt.Errorf("failed to get cluster id: %w", err)
	}

	clusterName, err := exporter.clusterName()
	if err != nil {
		return fmt.Errorf("failed to get cluster name: %w", err)
	}

	organization, err := exporter.organization()
	if err != nil {
		return fmt.Errorf("failed to get organization: %w", err)
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
		ClusterId:                   clusterId,
		ClusterName:                 clusterName,
		Organization:                organization,
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
		if err := exporter.exportTable(ctx, tempDir, table, agg); err != nil {
			if table.Optional {
				logrus.WithError(err).Warnf("skipping optional table %s.%s (not available in this cluster configuration)", table.Database, table.Name)
				continue
			}
			return fmt.Errorf("failed to export data for table %s.%s: %w", table.Database, table.Name, err)
		}
	}
	logrus.Info("finished table export")

	metadata.VirtualCluster = exporter.SystemDb != nil

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

func (exporter *Exporter) clusterId() (string, error) {
	r := exporter.Db.QueryRow(context.Background(), "SELECT crdb_internal.cluster_id()")
	var clusterId string
	err := r.Scan(&clusterId)
	return clusterId, err

}

func (exporter *Exporter) clusterName() (string, error) {
	r := exporter.Db.QueryRow(context.Background(), "SELECT crdb_internal.cluster_name()")
	var name string
	err := r.Scan(&name)
	return name, err

}

func (exporter *Exporter) organization() (string, error) {
	r := exporter.Db.QueryRow(context.Background(), "SHOW CLUSTER SETTING cluster.organization")
	var organization string
	err := r.Scan(&organization)
	return organization, err

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

	rows, err := exporter.Db.Query(ctx, "with z AS (SHOW ALL ZONE CONFIGURATIONS) SELECT raw_config_sql FROM z WHERE raw_config_sql IS NOT NULL")

	if err != nil {
		return fmt.Errorf("failed to query z configurations: %w", err)
	}
	defer rows.Close()

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

	_, err := exporter.Db.Exec(context.Background(), fmt.Sprintf("USE %s", pgx.Identifier{db}.Sanitize()))
	if err != nil {
		return creates, err
	}

	// Run in dependency order so the output can be replayed as-is.
	queries := []string{
		"SELECT create_statement FROM [SHOW CREATE ALL SCHEMAS]",
		"SELECT create_statement FROM [SHOW CREATE ALL TYPES]",
		"SELECT create_statement FROM [SHOW CREATE ALL TABLES]",
		"SELECT create_statement FROM [SHOW CREATE ALL ROUTINES]",
		"SELECT create_statement FROM [SHOW CREATE ALL TRIGGERS]",
	}

	for _, query := range queries {
		rows, err := exporter.Db.Query(context.Background(), query)
		if err != nil {
			return creates, err
		}
		for rows.Next() {
			var create string
			if err := rows.Scan(&create); err != nil {
				rows.Close()
				return nil, err
			}
			creates = append(creates, create)
		}
		rows.Close()
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
	defer rows.Close()

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

// exportTable routes the table export to the appropriate virtual cluster connection
// based on the table's Scope. For TenantScopeSystem tables, it first attempts the
// export using the main connection; if CockroachDB returns a virtual cluster error,
// it establishes a system connection and retries automatically. For TenantScopeBoth
// tables, the main virtual cluster is always exported, and the system virtual cluster
// is exported with a ".system" filename suffix when in virtualized cluster mode.
func (exporter *Exporter) exportTable(ctx context.Context, dir string, table Table, aggregationInterval time.Duration) error {
	scope := table.Scope
	if scope == "" {
		scope = TenantScopeMain
	}

	if scope == TenantScopeBoth {
		// Always export from the main virtual cluster.
		if err := exporter.doExportTable(ctx, dir, table, aggregationInterval, exporter.Db, ""); err != nil {
			return err
		}
		// Also export from the system virtual cluster (best-effort).
		systemConn, err := exporter.ensureSystemConn(ctx)
		if err != nil {
			logrus.WithError(err).Warnf("skipping system virtual cluster export for %s.%s (could not connect to system virtual cluster)", table.Database, table.Name)
			return nil
		}
		return exporter.doExportTable(ctx, dir, table, aggregationInterval, systemConn, ".system")
	}

	conn := exporter.Db
	if scope == TenantScopeSystem && exporter.SystemDb != nil {
		conn = exporter.SystemDb
	}

	err := exporter.doExportTable(ctx, dir, table, aggregationInterval, conn, "")
	if err != nil && scope == TenantScopeSystem && isVirtualClusterError(err) {
		systemConn, connErr := exporter.ensureSystemConn(ctx)
		if connErr != nil {
			return fmt.Errorf("failed to connect to system virtual cluster: %w", connErr)
		}
		return exporter.doExportTable(ctx, dir, table, aggregationInterval, systemConn, "")
	}
	return err
}

// doExportTable performs the actual table export using the provided connection.
// filenameSuffix is appended before the ".csv" extension (e.g. ".system" produces
// "crdb_internal.cluster_settings.system.csv"). Pass an empty string for no suffix.
func (exporter *Exporter) doExportTable(ctx context.Context, dir string, table Table, aggregationInterval time.Duration, conn *pgx.Conn, filenameSuffix string) error {
	// Create filename - if database is empty, just use table name
	var filename string
	if table.Database == "" {
		filename = fmt.Sprintf("%s%s.csv", table.Name, filenameSuffix)
	} else {
		filename = fmt.Sprintf("%s.%s%s.csv", table.Database, table.Name, filenameSuffix)
	}
	dataFile := filepath.Join(dir, filename)

	// Create output file
	file, err := os.Create(dataFile)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logrus.WithError(err).Debug("failed to close file")
		}
	}(file)

	// Construct table reference - handle empty database for cross-database queries
	var tableRef string
	if table.Database == "" {
		// Empty database means query across all databases using "" prefix
		tableRef = fmt.Sprintf(`"".%s`, table.Name)
	} else {
		tableRef = fmt.Sprintf("%s.%s", pgx.Identifier{table.Database}.Sanitize(), pgx.Identifier{table.Name}.Sanitize())
	}

	// Get column names
	var colProbeSQL string
	if table.Query != "" {
		colProbeSQL = fmt.Sprintf("SELECT * FROM (%s) AS q LIMIT 0", table.Query)
	} else {
		colProbeSQL = fmt.Sprintf("SELECT * FROM %s LIMIT 0", tableRef)
	}
	rows, err := conn.Query(ctx, colProbeSQL)
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

	// Build and run the COPY query.
	var copyQuery string
	if table.Query != "" {
		copyQuery = fmt.Sprintf("COPY (%s) TO STDOUT WITH CSV", table.Query)
	} else {
		// Use a SQL query to export data in CSV format
		var where string
		if table.TimeColumn != "" {
			where = fmt.Sprintf("WHERE %s BETWEEN '%s' and '%s'",
				pgx.Identifier{table.TimeColumn}.Sanitize(),
				startTime(exporter.Config.TimeRange.Start).Format("2006-01-02 15:04:05"), // offset for aggregation interval -- TODO
				endTime(exporter.Config.TimeRange.End).Format("2006-01-02 15:04:05"),
			)
		}

		// Build SELECT expression, applying column-level redaction when configured.
		selectExpr := buildSelectExpr(headers, table)

		copyQuery = fmt.Sprintf(
			"COPY (SELECT %s FROM %s %s) TO STDOUT WITH CSV",
			selectExpr, tableRef, where)
	}
	logrus.Info(copyQuery)
	_, err = conn.PgConn().CopyTo(ctx, file, copyQuery)
	if err != nil {
		return err
	}

	return nil
}

// buildSelectExpr constructs the SELECT expression for the COPY query.
// When the table has redaction configured, it returns an explicit column list
// with a CASE expression that replaces the sensitive column value with "<redacted>"
// for rows whose key column matches any entry in RedactedKeys.
// When no redaction is configured, it returns "*".
func buildSelectExpr(columns []string, table Table) string {
	if table.RedactColumn == "" || len(table.RedactedKeys) == 0 {
		return "*"
	}

	// Build the SQL IN list from the hard-coded redacted key names.
	quotedKeys := make([]string, len(table.RedactedKeys))
	for i, k := range table.RedactedKeys {
		quotedKeys[i] = "'" + strings.ReplaceAll(k, "'", "''") + "'"
	}
	inClause := strings.Join(quotedKeys, ", ")

	keyCol := pgx.Identifier{table.RedactKeyColumn}.Sanitize()
	redactCol := pgx.Identifier{table.RedactColumn}.Sanitize()

	cols := make([]string, len(columns))
	for i, col := range columns {
		quotedCol := pgx.Identifier{col}.Sanitize()
		if col == table.RedactColumn {
			cols[i] = fmt.Sprintf(
				"CASE WHEN %s IN (%s) THEN '<redacted>' ELSE %s END AS %s",
				keyCol, inClause, redactCol, redactCol,
			)
		} else {
			cols[i] = quotedCol
		}
	}
	return strings.Join(cols, ", ")
}

func (exporter *Exporter) createZipFile(sourceDir string) error {
	zipFile, err := os.Create(exporter.Config.OutputFile)
	if err != nil {
		return err
	}
	defer func(zipFile *os.File) {
		err := zipFile.Close()
		if err != nil {
			logrus.WithError(err).Debug("failed to close zip file")
		}
	}(zipFile)

	zipWriter := zip.NewWriter(zipFile)
	defer func(zipWriter *zip.Writer) {
		err := zipWriter.Close()
		if err != nil {
			logrus.WithError(err).Debug("failed to close zip writer")
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

		zf, err := zipWriter.Create(relPath)
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
				logrus.WithError(err).Debug("failed to close zip file")
			}
		}(file)

		_, err = io.Copy(zf, file)
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

// enableUnsafeInternalsIfNeeded checks the CockroachDB version and enables
// allow_unsafe_internals if the version is >= 26.1.
// This is required for accessing crdb_internal tables in CockroachDB 26.1+.
func enableUnsafeInternalsIfNeeded(ctx context.Context, conn *pgx.Conn) error {
	// Get the version string
	var versionStr string
	err := conn.QueryRow(ctx, "SELECT version()").Scan(&versionStr)
	if err != nil {
		return fmt.Errorf("failed to query version: %w", err)
	}

	// Parse the version to extract major version number
	// Version string format: "CockroachDB CCL v26.1.0-beta.3 ..."
	majorVersion, err := parseMajorVersion(versionStr)
	if err != nil {
		logrus.WithError(err).Warn("unable to parse CockroachDB version, skipping allow_unsafe_internals check")
		return nil // Don't fail, just skip the check
	}

	// Enable allow_unsafe_internals for versions >= 26
	if majorVersion >= 26 {
		logrus.Infof("detected CockroachDB v%d.x, enabling allow_unsafe_internals", majorVersion)
		_, err := conn.Exec(ctx, "SET allow_unsafe_internals = true")
		if err != nil {
			return fmt.Errorf("failed to set allow_unsafe_internals: %w", err)
		}
	}

	return nil
}

// isVirtualClusterError returns true if the error indicates an operation is unsupported
// within an application virtual cluster. When this occurs for a TenantScopeSystem table,
// the exporter will retry the query against the system virtual cluster.
//
// The error string "operation is unsupported within a virtual cluster" is produced by
// CockroachDB when an application tenant attempts to access system-only resources.
func isVirtualClusterError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "operation is unsupported within a virtual cluster")
}

// buildSystemConnectionString derives a system virtual cluster connection string from
// an existing connection string by appending options=-ccluster=system. If the connection
// string already contains an options parameter, the cluster option is appended to it.
func buildSystemConnectionString(connStr string) (string, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse connection string: %w", err)
	}
	q := u.Query()
	if existing := q.Get("options"); existing != "" {
		q.Set("options", existing+" -ccluster=system")
	} else {
		q.Set("options", "-ccluster=system")
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ensureSystemConn returns the system virtual cluster connection, creating it if needed.
// It is called lazily when a TenantScopeSystem query fails with a virtual cluster error.
func (exporter *Exporter) ensureSystemConn(ctx context.Context) (*pgx.Conn, error) {
	if exporter.SystemDb != nil {
		return exporter.SystemDb, nil
	}
	systemConnStr, err := buildSystemConnectionString(exporter.Config.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to build system connection string: %w", err)
	}
	logrus.Info("detected virtual cluster, connecting to system virtual cluster")
	conn, err := pgx.Connect(ctx, systemConnStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to system virtual cluster: %w", err)
	}
	if err := enableUnsafeInternalsIfNeeded(ctx, conn); err != nil {
		_ = conn.Close(ctx)
		return nil, err
	}
	exporter.SystemDb = conn
	return conn, nil
}

// parseMajorVersion extracts the major version number from a CockroachDB version string.
// Example: "CockroachDB CCL v26.1.0-beta.3 ..." -> 26
func parseMajorVersion(versionStr string) (int, error) {
	// Look for pattern like "v26.1" or "v25.2"
	// Version string format: "CockroachDB CCL v26.1.0-beta.3 ..."
	parts := strings.Fields(versionStr)
	for _, part := range parts {
		if strings.HasPrefix(part, "v") {
			// Remove the 'v' prefix
			versionPart := strings.TrimPrefix(part, "v")
			// Split by '.' to get major.minor.patch
			versionComponents := strings.Split(versionPart, ".")
			if len(versionComponents) > 0 {
				// Parse the major version
				var major int
				_, err := fmt.Sscanf(versionComponents[0], "%d", &major)
				if err != nil {
					return 0, fmt.Errorf("failed to parse major version from %s: %w", versionComponents[0], err)
				}
				return major, nil
			}
		}
	}
	return 0, fmt.Errorf("version number not found in string: %s", versionStr)
}
