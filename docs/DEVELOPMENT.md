# Development Guide

This guide is for developers who want to build, test, or contribute to the workload-exporter tool.

## Prerequisites

- Go 1.18 or later
- Git

## Building from Source

```bash
# Clone the repository
git clone https://github.com/cockroachlabs/workload-exporter.git
cd workload-exporter

# Get dependencies
go mod tidy

# Build
go build -o workload-exporter
```

### Build with Version Information

To include version information in the binary:

```bash
go build -ldflags="-X main.Version=v1.4.0 -X main.Commit=abc12345 -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o workload-exporter
```

## Using Make

The project includes a Makefile for common tasks:

```bash
make help              # Show available commands
make build             # Build the binary
make test              # Run unit tests
make test-integration  # Run integration tests
make lint              # Run linter
make clean             # Clean build artifacts
```

## Testing

### Unit Tests

Run the standard test suite:
```bash
make test
# or
go test -v ./...
```

### Integration Tests

See [TESTING.md](TESTING.md) for comprehensive testing documentation.

## Code Structure

```
workload-exporter/
├── cmd/               # CLI commands
│   ├── root.go       # Root command
│   ├── export.go     # Export command
│   └── version.go    # Version command
├── pkg/
│   └── export/       # Core export functionality
│       ├── exporter.go       # Main exporter logic
│       └── exporter_test.go  # Unit tests
├── docs/             # Documentation
├── Makefile          # Build automation
└── go.mod            # Go dependencies
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Run tests: `make test`
6. Run linter: `make lint`
7. Submit a pull request

## Release Process

1. Update version in relevant files
2. Run integration tests: `make test-integration`
3. Create git tag: `git tag v1.x.x`
4. Push tag: `git push origin v1.x.x`
5. GitHub Actions will build and publish release binaries

## Linting

The project uses golangci-lint for code quality:

```bash
# Run linter
make lint

# Or directly
golangci-lint run --timeout=5m
```

## Debugging

Enable debug logging when running the exporter:

```bash
./workload-exporter export -c "connection-string" --debug
```

This will show detailed information about each step of the export process.
