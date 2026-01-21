//go:build integration
// +build integration

package export

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		// Create a test database
		"CREATE DATABASE IF NOT EXISTS testdb",
		// Create a test table
		"CREATE TABLE IF NOT EXISTS testdb.test_table (id INT PRIMARY KEY, name STRING)",
		// Insert some data
		"INSERT INTO testdb.test_table VALUES (1, 'test1'), (2, 'test2')",
		// Run some queries to generate statistics
		"SELECT * FROM testdb.test_table WHERE id = 1",
		"SELECT * FROM testdb.test_table WHERE name = 'test2'",
		// Create a zone configuration
		"ALTER TABLE testdb.test_table CONFIGURE ZONE USING num_replicas = 1",
	}

	for _, query := range queries {
		_, err := conn.Exec(ctx, query)
		require.NoError(t, err, "Failed to execute seed query: %s", query)
	}

	// Wait a bit for statistics to be collected
	time.Sleep(2 * time.Second)

	t.Log("Test data seeded successfully")
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
		"testdb.schema.txt":                               false, // Our test database
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
