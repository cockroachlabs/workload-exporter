# CockroachDB Workload Export Tool

[![CI](https://github.com/cockroachlabs/workload-exporter/actions/workflows/ci.yaml/badge.svg)](https://github.com/cockroachlabs/workload-exporter/actions/workflows/ci.yaml)

A command-line tool that exports workload data from a CockroachDB cluster into a portable zip file for analysis and troubleshooting.

## What It Does

The workload-exporter creates a complete snapshot of your cluster's workload characteristics, including:

- **Statement and transaction statistics** - Query performance and execution patterns
- **Contention events** - Lock contention and blocking queries
- **Cluster metadata** - Version, configuration, and node topology
- **Database schemas** - Table structures and definitions
- **Zone configurations** - Replication and placement settings
- **Table indexes** - Index definitions and descriptor IDs

This data can be analyzed locally or shared with Cockroach Labs support for troubleshooting.

## Installation

### Download Pre-Built Binary

Download the latest release for your platform from the [**Releases Page**](https://github.com/cockroachlabs/workload-exporter/releases), or use the commands below (replace `VERSION` with the desired release tag, e.g. `v1.7.1`):

#### macOS (Apple Silicon)
```bash
curl -L https://github.com/cockroachlabs/workload-exporter/releases/download/VERSION/workload-exporter-VERSION-darwin-arm64.tar.gz | tar xz
mv workload-exporter-VERSION-darwin-arm64 workload-exporter
```

#### macOS (Intel)
```bash
curl -L https://github.com/cockroachlabs/workload-exporter/releases/download/VERSION/workload-exporter-VERSION-darwin-amd64.tar.gz | tar xz
mv workload-exporter-VERSION-darwin-amd64 workload-exporter
```

#### Linux (amd64)
```bash
curl -L https://github.com/cockroachlabs/workload-exporter/releases/download/VERSION/workload-exporter-VERSION-linux-amd64.tar.gz | tar xz
mv workload-exporter-VERSION-linux-amd64 workload-exporter
```

#### Linux (arm64)
```bash
curl -L https://github.com/cockroachlabs/workload-exporter/releases/download/VERSION/workload-exporter-VERSION-linux-arm64.tar.gz | tar xz
mv workload-exporter-VERSION-linux-arm64 workload-exporter
```

#### Windows
Download `workload-exporter-VERSION-windows-amd64.zip` from the [releases page](https://github.com/cockroachlabs/workload-exporter/releases) and extract it.

### Verify Installation

```bash
./workload-exporter version
```

### Updating

Update to the latest release in-place:

```bash
./workload-exporter update
```

Check if a newer version is available without installing:

```bash
./workload-exporter update --check
```

## Quick Start

### Basic Export

Export the last 2 hours of workload data (default):

```bash
./workload-exporter export \
  -c "postgresql://user:password@host:26257/?sslmode=verify-full"
```

This creates `workload-export.zip` in the current directory.

### Export Specific Time Range

Export data for a specific time period:

```bash
./workload-exporter export \
  -c "postgresql://user:password@host:26257/?sslmode=verify-full" \
  -s "2025-04-18T13:00:00Z" \
  -e "2025-04-18T20:00:00Z" \
  -o "incident-export.zip"
```

## Command Options

```
Flags:
  -c, --connection-url string   CockroachDB connection string (required)
  -o, --output-file string      Output zip file (default: "workload-export.zip")
  -s, --start string            Start time in RFC3339 format (default: 2 hours ago)
  -e, --end string              End time in RFC3339 format (default: 1 hour from now)
      --debug                   Enable debug logging
```

### Connection String Format

```
postgresql://[user]:[password]@[host]:[port]/[database]?sslmode=[mode]
```

**Example:**
```
postgresql://admin:mypassword@my-cluster.example.com:26257/defaultdb?sslmode=verify-full
```

## What Data is Collected

The export creates a **zip file** containing the following files:

### Metadata
- **`metadata.json`** - Cluster version, ID, name, organization, and export configuration
  - ⚠️ Note: Connection string password is automatically redacted

### Statistics (CSV format, time-filtered)
- **`crdb_internal.statement_statistics.csv`** - SQL statement execution stats
- **`crdb_internal.transaction_statistics.csv`** - Transaction execution stats
- **`crdb_internal.transaction_contention_events.csv`** - Lock contention events
- **`crdb_internal.gossip_nodes.csv`** - Node information and topology
- **`crdb_internal.table_indexes.csv`** - Table and index descriptor IDs across all databases

*Statistics files only include data within the specified time range*

### Schema Information
- **`[database_name].schema.txt`** - CREATE statements for all tables in each database
  - One file per user database (system databases excluded)

### Configuration
- **`zone_configurations.txt`** - All zone configuration SQL statements

## Inspecting the Export

The export is a standard **zip file** that you can inspect before sharing:

```bash
# List all files in the export
unzip -l workload-export.zip

# Extract to a directory
unzip workload-export.zip -d export-contents

# View the metadata
cat export-contents/metadata.json | jq .

# Preview statistics (first 10 lines)
head export-contents/crdb_internal.statement_statistics.csv

# Check what schemas were exported
ls export-contents/*.schema.txt
```

**All data is in plain text format** (JSON, CSV, SQL) and can be reviewed before sharing with Cockroach Labs or others.

## Privacy and Security

- **Passwords are redacted** - Connection string passwords are automatically removed from metadata
- **No query parameters** - Statement statistics include query fingerprints, not actual parameter values
- **Schema only** - Table schemas are exported, but **no actual table data** is included
- **Read-only** - The tool only reads data and makes no modifications to your cluster
- **Local export** - All data is written to a local zip file under your control

## Requirements

- **CockroachDB version:** 24.1 or later
- **Network access** to the CockroachDB cluster
- **User permissions:**
  - Read access to `crdb_internal` tables
  - Read access to system settings
  - Read access to user databases (for schema export)
  - *Recommended:* Admin role for simplest setup

### Grant Permissions

For simplest setup, grant admin role:
```sql
GRANT admin TO your_user;
```

For more restrictive permissions, see [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md#permission-issues).

## Common Use Cases

### Troubleshooting Performance Issues

```bash
# Export data from when the issue occurred
./workload-exporter export \
  -c "connection-string" \
  -s "2025-04-18T14:00:00Z" \
  -e "2025-04-18T16:00:00Z" \
  -o "performance-issue.zip"
```

### Daily Workload Snapshot

```bash
# Export the last 24 hours
./workload-exporter export \
  -c "connection-string" \
  -s "$(date -u -d '24 hours ago' +%Y-%m-%dT%H:%M:%SZ)" \
  -e "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o "daily-$(date +%Y%m%d).zip"
```

### Pre-Migration Baseline

```bash
# Capture workload before a migration
./workload-exporter export \
  -c "connection-string" \
  -o "pre-migration-baseline.zip"
```

## Getting Help

### Troubleshooting

See [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) for solutions to common issues:
- Connection problems
- Permission errors
- Time format issues
- Empty exports

### Enable Debug Logging

For detailed information about what the tool is doing:

```bash
./workload-exporter export -c "connection-string" --debug
```

### Support

- **Issues:** [GitHub Issues](https://github.com/cockroachlabs/workload-exporter/issues)
- **Cockroach Labs Support:** Share the generated zip file with your support ticket

## Additional Documentation

- **[Version Compatibility](docs/COMPATIBILITY.md)** - Supported CockroachDB versions and version-specific behavior
- **[Troubleshooting Guide](docs/TROUBLESHOOTING.md)** - Solutions to common issues
- **[Development Guide](docs/DEVELOPMENT.md)** - Building from source and contributing
- **[Library Usage](docs/LIBRARY.md)** - Using workload-exporter as a Go library
- **[Testing Guide](docs/TESTING.md)** - Running tests and integration tests

## License

[MIT License](LICENSE)

---

**Note:** This tool is designed for CockroachDB clusters. For CockroachDB 26.1+, the tool automatically handles the required `allow_unsafe_internals` setting.
