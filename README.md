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

## Installation

### From Binary Releases

Download the appropriate binary for your platform from the [releases page](https://github.com/cockroachlabs/workload-exporter/releases).

### From Source

```bash
# Clone the repository
git clone https://github.com/yourusername/workload-exporter.git
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
- `--start`, `-s`: start time (default: current time - 6 hours)
- `--end`, `-e`: End time (default: current time + 1 hour) 

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
# Export a specific time window
workload-exporter export -c "postgresql://user:password@host:26257/?sslmode=verify-full" 
  -s '2025-04-18T13:25:00Z'
  -e '2025-04-18T20:25:00Z'
```

### Custom output file

Export using a custom file:

```bash
workload-exporter export -c "postgresql://user:password@host:26257/?sslmode=verify-full"
   -o 'my-export.zip'
```

## File Format

The export zip file contains:

- `metadata.json`: Information about the export, including databases, tables, and configuration
- One file per exported table in the format `[database].[table]`

## Building from Source

Requirements:
- Go 1.18 or later

```bash
# Get dependencies
go mod tidy

# Build
go build -o workload-exporter
```

## License

[MIT License](LICENSE)