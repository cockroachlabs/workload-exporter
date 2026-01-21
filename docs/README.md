# Documentation Index

This directory contains detailed documentation for the CockroachDB Workload Export Tool.

## For Users

### [Troubleshooting Guide](TROUBLESHOOTING.md)
Solutions to common issues:
- Connection problems
- Authentication and permission errors
- Time format issues
- Empty exports
- Debug logging

### [Version Compatibility](COMPATIBILITY.md)
CockroachDB version support and version-specific behavior:
- Supported versions (24.1+)
- Version-specific features
- Upgrade considerations
- Permission requirements

## For Developers

### [Development Guide](DEVELOPMENT.md)
Building and contributing:
- Building from source
- Development workflow
- Using Make
- Code structure
- Contributing guidelines

### [Testing Guide](TESTING.md)
Running tests:
- Unit tests
- Integration tests (cross-version)
- CI/CD integration
- Performance expectations

### [Integration Test Summary](INTEGRATION_TEST_SUMMARY.md)
Quick reference for integration testing implementation

### [Library Usage](LIBRARY.md)
Using workload-exporter as a Go library:
- Installation
- API documentation
- Example code
- Advanced usage patterns

## Quick Links

- **Main README:** [../README.md](../README.md)
- **Report Issues:** [GitHub Issues](https://github.com/cockroachlabs/workload-exporter/issues)
- **Latest Releases:** [Releases Page](https://github.com/cockroachlabs/workload-exporter/releases)
- **API Reference:** [pkg.go.dev](https://pkg.go.dev/github.com/cockroachlabs/workload-exporter/pkg/export)

## Documentation Organization

```
docs/
├── README.md                      # This file - documentation index
├── COMPATIBILITY.md               # Version compatibility guide
├── TROUBLESHOOTING.md             # Solutions to common issues
├── DEVELOPMENT.md                 # Building and contributing
├── TESTING.md                     # Testing guide
├── INTEGRATION_TEST_SUMMARY.md    # Integration test reference
└── LIBRARY.md                     # Go library usage
```

## Getting Help

1. Check the [Troubleshooting Guide](TROUBLESHOOTING.md) for common issues
2. Review [Version Compatibility](COMPATIBILITY.md) for version-specific behavior
3. Search [GitHub Issues](https://github.com/cockroachlabs/workload-exporter/issues)
4. Open a new issue with details about your problem
