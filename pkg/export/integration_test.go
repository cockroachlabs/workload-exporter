//go:build integration
// +build integration

package export

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/cockroach-go/v2/testserver"
	"github.com/jackc/pgx/v4"
	"github.com/stretchr/testify/require"
)

// TestCrossVersionCompatibility tests the exporter against multiple CockroachDB versions.
// Run with: go test -tags=integration -v ./pkg/export/
//
// This test downloads CockroachDB binaries (cached after first run) and may take several minutes.
func TestCrossVersionCompatibility(t *testing.T) {
	// Versions to test - update this list as new versions are released
	versions := []string{
		"v24.1.25",
		"v24.3.25",
		"v25.2.11",
		"v25.4.3",
		"v26.1.0-beta.3",
	}

	for _, version := range versions {
		version := version // capture range variable
		t.Run(version, func(t *testing.T) {
			t.Parallel() // Run versions in parallel for speed
			testExportWithVersion(t, version)
		})
	}
}

func testExportWithVersion(t *testing.T, version string) {
	t.Logf("Starting test for CockroachDB %s", version)

	// Start CockroachDB test server with specific version
	ts, err := testserver.NewTestServer(
		testserver.CustomVersionOpt(version),
	)
	require.NoError(t, err, "Failed to start test server for version %s", version)
	defer ts.Stop()

	// Get connection URL
	connURL := ts.PGURL()
	require.NotEmpty(t, connURL, "Connection URL should not be empty")

	t.Logf("Test server running at: %s", connURL.String())

	// Connect to the database
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connURL.String())
	require.NoError(t, err, "Failed to connect to test server")
	defer conn.Close(ctx)

	// Seed test data
	seedTestData(t, conn)

	// Configure and run the export
	outputFile := filepath.Join(t.TempDir(), fmt.Sprintf("export-%s.zip", version))
	config := Config{
		ConnectionString: connURL.String(),
		OutputFile:       outputFile,
		TimeRange: TimeRange{
			Start: time.Now().Add(-1 * time.Hour),
			End:   time.Now().Add(1 * time.Hour),
		},
	}

	exporter, err := NewExporter(config)
	require.NoError(t, err, "Failed to create exporter")
	defer exporter.Close()

	err = exporter.Export()
	require.NoError(t, err, "Export failed for version %s", version)

	// Validate the export
	validateExport(t, outputFile, version)

	t.Logf("✓ Successfully tested version %s", version)
}

// seedTestData creates test data in the cluster to ensure exports have content
func seedTestData(t *testing.T, conn *pgx.Conn) {
	ctx := context.Background()

	queries := []string{
		// Create multiple test databases to verify cross-database table_indexes export
		"CREATE DATABASE IF NOT EXISTS testdb1",
		"CREATE DATABASE IF NOT EXISTS testdb2",
		"CREATE DATABASE IF NOT EXISTS testdb3",

		// Create tables in testdb1
		"CREATE TABLE IF NOT EXISTS testdb1.users (id INT PRIMARY KEY, username STRING)",
		"CREATE TABLE IF NOT EXISTS testdb1.orders (id INT PRIMARY KEY, user_id INT, total DECIMAL)",

		// Create tables in testdb2
		"CREATE TABLE IF NOT EXISTS testdb2.products (id INT PRIMARY KEY, name STRING, price DECIMAL)",
		"CREATE TABLE IF NOT EXISTS testdb2.inventory (product_id INT PRIMARY KEY, quantity INT)",

		// Create tables in testdb3
		"CREATE TABLE IF NOT EXISTS testdb3.logs (id INT PRIMARY KEY, message STRING, created_at TIMESTAMP)",

		// Insert some data
		"INSERT INTO testdb1.users VALUES (1, 'alice'), (2, 'bob')",
		"INSERT INTO testdb2.products VALUES (1, 'widget', 9.99), (2, 'gadget', 19.99)",
		"INSERT INTO testdb3.logs VALUES (1, 'test log', now())",

		// Run some queries to generate statistics
		"SELECT * FROM testdb1.users WHERE id = 1",
		"SELECT * FROM testdb2.products WHERE price > 10",
		"SELECT * FROM testdb3.logs LIMIT 1",

		// Create zone configurations
		"ALTER TABLE testdb1.users CONFIGURE ZONE USING num_replicas = 1",
		"ALTER TABLE testdb2.products CONFIGURE ZONE USING num_replicas = 1",
	}

	for _, query := range queries {
		_, err := conn.Exec(ctx, query)
		require.NoError(t, err, "Failed to execute seed query: %s", query)
	}

	// Wait a bit for statistics to be collected
	time.Sleep(2 * time.Second)

	t.Log("Test data seeded successfully with multiple databases")
}

// validateExport checks that the export file contains all expected content
func validateExport(t *testing.T, zipPath string, version string) {
	// Verify zip file exists
	info, err := os.Stat(zipPath)
	require.NoError(t, err, "Export file should exist")
	require.Greater(t, info.Size(), int64(0), "Export file should not be empty")

	t.Logf("Export file size: %d bytes", info.Size())

	// Open and validate zip contents
	zipReader, err := zip.OpenReader(zipPath)
	require.NoError(t, err, "Should be able to open zip file")
	defer zipReader.Close()

	// Expected files that should always be present
	expectedFiles := map[string]bool{
		"metadata.json":                                   false,
		"crdb_internal.statement_statistics.csv":          false,
		"crdb_internal.transaction_statistics.csv":        false,
		"crdb_internal.transaction_contention_events.csv": false,
		"crdb_internal.gossip_nodes.csv":                  false,
		"crdb_internal.table_indexes.csv":                 false,
		"zone_configurations.txt":                         false,
		"testdb1.schema.txt":                              false, // Our test databases
		"testdb2.schema.txt":                              false,
		"testdb3.schema.txt":                              false,
	}

	// Check all files in zip
	for _, file := range zipReader.File {
		t.Logf("  Found file: %s (%d bytes)", file.Name, file.UncompressedSize64)

		if _, expected := expectedFiles[file.Name]; expected {
			expectedFiles[file.Name] = true
		}

		// Validate CSV files have headers
		if filepath.Ext(file.Name) == ".csv" {
			validateCSVFile(t, file)
		}

		// Validate metadata.json structure
		if file.Name == "metadata.json" {
			validateMetadataFile(t, file, version)
		}

		// Validate table_indexes contains data from multiple databases
		if file.Name == "crdb_internal.table_indexes.csv" {
			validateTableIndexesFile(t, file, version)
		}
	}

	// Ensure all expected files were found
	for filename, found := range expectedFiles {
		require.True(t, found, "Expected file not found in export: %s (version %s)", filename, version)
	}

	t.Logf("✓ All expected files validated for version %s", version)
}

// validateCSVFile ensures a CSV file has a header row
func validateCSVFile(t *testing.T, file *zip.File) {
	rc, err := file.Open()
	require.NoError(t, err, "Should be able to open CSV file %s", file.Name)
	defer rc.Close()

	// Read first line (header)
	buf := make([]byte, 1024)
	n, err := rc.Read(buf)
	if err != nil && err.Error() != "EOF" {
		require.NoError(t, err, "Should be able to read from CSV file %s", file.Name)
	}

	header := string(buf[:n])
	require.NotEmpty(t, header, "CSV file %s should have a header", file.Name)
	require.Contains(t, header, ",", "CSV file %s header should contain commas", file.Name)
}

// validateMetadataFile ensures metadata.json has required structure
func validateMetadataFile(t *testing.T, file *zip.File, version string) {
	rc, err := file.Open()
	require.NoError(t, err, "Should be able to open metadata.json")
	defer rc.Close()

	var metadata Metadata
	err = json.NewDecoder(rc).Decode(&metadata)
	require.NoError(t, err, "metadata.json should be valid JSON")

	// Validate key fields
	require.NotEmpty(t, metadata.Version, "Metadata should have version")
	require.NotEmpty(t, metadata.ClusterVersion, "Metadata should have cluster version")
	require.NotEmpty(t, metadata.ClusterId, "Metadata should have cluster ID")

	t.Logf("  Cluster version from metadata: %s", metadata.ClusterVersion)
}

// validateTableIndexesFile ensures table_indexes CSV contains data from multiple databases
func validateTableIndexesFile(t *testing.T, file *zip.File, version string) {
	rc, err := file.Open()
	require.NoError(t, err, "Should be able to open table_indexes CSV")
	defer rc.Close()

	// Parse CSV
	reader := csv.NewReader(rc)

	// Read header
	header, err := reader.Read()
	require.NoError(t, err, "Should be able to read CSV header")

	// Find the descriptor_name column index (contains database.schema.table)
	descriptorNameIdx := -1
	for i, col := range header {
		if col == "descriptor_name" {
			descriptorNameIdx = i
			break
		}
	}
	require.NotEqual(t, -1, descriptorNameIdx, "CSV should have descriptor_name column")

	// Track which databases we've seen
	databasesSeen := make(map[string]bool)

	// Read all rows and extract database names
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		require.NoError(t, err, "Should be able to read CSV row")

		if len(record) > descriptorNameIdx {
			descriptorName := record[descriptorNameIdx]
			// descriptor_name format is typically "database.schema.table" or "database.public.table"
			parts := strings.Split(descriptorName, ".")
			if len(parts) >= 1 {
				database := parts[0]
				// Track non-system databases
				if database != "system" && database != "postgres" && database != "" {
					databasesSeen[database] = true
				}
			}
		}
	}

	// Verify we have entries from our test databases
	expectedDatabases := []string{"testdb1", "testdb2", "testdb3"}
	foundCount := 0
	for _, db := range expectedDatabases {
		if databasesSeen[db] {
			foundCount++
			t.Logf("  ✓ Found table indexes for database: %s", db)
		}
	}

	// We should see at least 2 of our test databases to prove cross-database querying works
	require.GreaterOrEqual(t, foundCount, 2,
		"table_indexes CSV should contain entries from multiple test databases (found %d, expected at least 2)",
		foundCount)

	t.Logf("  ✓ table_indexes contains data from %d databases (version %s)", len(databasesSeen), version)
}
