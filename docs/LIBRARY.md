# Using workload-exporter as a Go Library

You can import and use workload-exporter in your Go projects as a library.

## Installation

```bash
go get github.com/cockroachlabs/workload-exporter@latest
```

Or specify a specific version:

```bash
go get github.com/cockroachlabs/workload-exporter@v1.4.0
```

## Example Usage

```go
package main

import (
    "log"
    "time"

    "github.com/cockroachlabs/workload-exporter/pkg/export"
)

func main() {
    // Create exporter configuration
    config := export.Config{
        ConnectionString: "postgresql://user:password@host:26257/?sslmode=verify-full",
        OutputFile:       "my-export.zip",
        TimeRange: export.TimeRange{
            Start: time.Now().Add(-2 * time.Hour),
            End:   time.Now(),
        },
    }

    // Initialize exporter
    exporter, err := export.NewExporter(config)
    if err != nil {
        log.Fatalf("Failed to create exporter: %v", err)
    }
    defer exporter.Close()

    // Perform export
    if err := exporter.Export(); err != nil {
        log.Fatalf("Export failed: %v", err)
    }

    log.Println("Export completed successfully")
}
```

## API Documentation

### Types

#### `export.Config`
Configuration for the export operation.

**Fields:**
- `ConnectionString` (string): PostgreSQL connection URL for CockroachDB
- `OutputFile` (string): Path to output zip file
- `TimeRange` (TimeRange): Time window for filtering statistics data

#### `export.TimeRange`
Defines the time window for filtering exported data.

**Fields:**
- `Start` (time.Time): Beginning of time range (inclusive)
- `End` (time.Time): End of time range (inclusive)

#### `export.Exporter`
Handles the export of workload data from a CockroachDB cluster.

**Fields:**
- `Config` (Config): Export configuration settings
- `Db` (*pgx.Conn): Active database connection
- `CleanConnectionString` (string): Connection string with password redacted

### Functions

#### `export.NewExporter(config Config) (*Exporter, error)`
Creates a new Exporter instance with the given configuration.

**Parameters:**
- `config`: Export configuration

**Returns:**
- `*Exporter`: Exporter instance
- `error`: Error if connection fails or configuration is invalid

**Example:**
```go
exporter, err := export.NewExporter(config)
if err != nil {
    return err
}
defer exporter.Close()
```

#### `exporter.Export() error`
Performs the complete export operation.

**Returns:**
- `error`: Error if any step of the export process fails

**Example:**
```go
if err := exporter.Export(); err != nil {
    log.Fatalf("Export failed: %v", err)
}
```

#### `exporter.Close() error`
Closes the database connection.

**Returns:**
- `error`: Error if closing the connection fails

**Example:**
```go
defer exporter.Close()
```

## Advanced Usage

### Custom Time Ranges

```go
// Export last 24 hours
config := export.Config{
    ConnectionString: connStr,
    OutputFile:       "daily-export.zip",
    TimeRange: export.TimeRange{
        Start: time.Now().Add(-24 * time.Hour),
        End:   time.Now(),
    },
}
```

### Specific Date Range

```go
// Export specific date range
start, _ := time.Parse(time.RFC3339, "2025-01-01T00:00:00Z")
end, _ := time.Parse(time.RFC3339, "2025-01-31T23:59:59Z")

config := export.Config{
    ConnectionString: connStr,
    OutputFile:       "january-export.zip",
    TimeRange: export.TimeRange{
        Start: start,
        End:   end,
    },
}
```

### Error Handling

```go
exporter, err := export.NewExporter(config)
if err != nil {
    if strings.Contains(err.Error(), "connection refused") {
        log.Fatal("Cannot connect to database - is the cluster running?")
    }
    log.Fatalf("Failed to create exporter: %v", err)
}
defer exporter.Close()

if err := exporter.Export(); err != nil {
    if strings.Contains(err.Error(), "permission denied") {
        log.Fatal("User lacks required permissions")
    }
    log.Fatalf("Export failed: %v", err)
}
```

## Full API Reference

For complete API documentation, see [pkg.go.dev](https://pkg.go.dev/github.com/cockroachlabs/workload-exporter/pkg/export).

## Version Compatibility

The library supports CockroachDB versions 24.1 and later. For CockroachDB 26.1+, the library automatically enables the `allow_unsafe_internals` setting required to access `crdb_internal` tables.
