# CockroachDB Workload Export Tool

A command-line utility for exporting workload data from a CockroachDB cluster into a portable zip file for analysis.

## Purpose

This tool simplifies the process of exporting workload data for analysis, including:

1. Statement and transaction statistics
2. Contention events
3. Cluster metadata, including node topology and configuration
4. Table schemas
5. Zone configurations
6. System settings

## Prerequisites

- CockroachDB cluster (v21.1 or later recommended)
- Network access to the CockroachDB cluster
- User with appropriate read permissions on system tables

## Installation

### From Binary Releases

Download the appropriate binary for your platform from the [releases page](https://github.com/cockroachlabs/workload-exporter/releases).

### From Source

```bash
# Clone the repository
git clone https://github.com/cockroachlabs/workload-exporter.git
cd workload-exporter

# Build the binary
go build -o workload-exporter
```

## Usage

### Export Command

Export workload data from a CockroachDB cluster to a zip file:

```bash
workload-exporter export \
  -c "postgresql://user:password@host:26257/database?sslmode=verify-full" \
  [options]
```

#### Export Options:

- `--connection-url`, `-c`: Connection string for CockroachDB (required)
- `--output-file`, `-o`: Output zip file name (default: "workload-export.zip")
- `--start`, `-s`: Start time in RFC3339 format (default: current time - 6 hours)
- `--end`, `-e`: End time in RFC3339 format (default: current time + 1 hour)
- `--debug`: Enable debug logging output

## Examples

### Basic Export

Export workload data:

```bash
# Export
workload-exporter export -c "postgresql://user:password@source-host:26257/?sslmode=verify-full"
```

### Specific time period

Export for a specific time period:

```bash
# Export a specific time window (times must be in RFC3339 format)
workload-exporter export \
  -c "postgresql://user:password@host:26257/?sslmode=verify-full" \
  -s "2025-04-18T13:25:00Z" \
  -e "2025-04-18T20:25:00Z"
```

### Custom output file

Export using a custom file:

```bash
workload-exporter export \
  -c "postgresql://user:password@host:26257/?sslmode=verify-full" \
  -o "my-export.zip"
```

### Enable debug logging

Export with verbose debug output:

```bash
workload-exporter export \
  -c "postgresql://user:password@host:26257/?sslmode=verify-full" \
  --debug
```

## File Format

The export zip file contains:

### Metadata
- `metadata.json`: Export metadata including:
  - Cluster version, ID, name, and organization
  - SQL statistics aggregation and flush intervals
  - Export configuration (connection string with password redacted, time range, output file)
  - Timestamp of export

### Statistics Data (CSV format with headers)
- `crdb_internal.statement_statistics.csv`: Statement execution statistics
- `crdb_internal.transaction_statistics.csv`: Transaction execution statistics
- `crdb_internal.transaction_contention_events.csv`: Lock contention events
- `crdb_internal.gossip_nodes.csv`: Cluster node information

**Note:** Statistics tables are filtered by the specified time range using their timestamp columns.

### Database Schemas
- `[database_name].schema.txt`: CREATE statements for all tables in each user database
  - One file per database (excludes system databases: `system`, `crdb_internal`, `postgres`)

### Configuration
- `zone_configurations.txt`: All zone configuration SQL statements from the cluster

## Building from Source

Requirements:
- Go 1.18 or later

```bash
# Get dependencies
go mod tidy

# Build
go build -o workload-exporter
```

## Troubleshooting

### Connection Issues
Ensure your connection string includes the proper SSL mode and authentication credentials:
```bash
postgresql://user:password@host:26257/database?sslmode=verify-full
```

### Time Format Errors
Start and end times must be in RFC3339 format:
- Correct: `2025-04-18T13:25:00Z`
- Correct: `2025-04-18T13:25:00-05:00`
- Incorrect: `2025-04-18 13:25:00`

### Permission Errors
The database user must have read access to:
- `crdb_internal` tables
- System settings (for cluster metadata)
- All user databases (for schema export)

### Empty Exports
If the time range doesn't contain any data, the CSV files will only contain headers. Adjust your `--start` and `--end` flags to capture the desired time period.

## License

[MIT License](LICENSE)