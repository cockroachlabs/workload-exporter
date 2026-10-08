# Testing Guide

This document describes the testing strategy for the workload-exporter tool.

## Test Types

### Unit Tests

Standard Go unit tests that test individual functions and components in isolation.

**Run unit tests:**
```bash
make test
# or
go test -v ./...
```

### Integration Tests

Integration tests validate the exporter works correctly across multiple versions of CockroachDB. These tests:

- Download and cache CockroachDB binaries for each version
- Start a single-node test cluster for each version
- Seed test data (databases, tables, queries, zone configurations)
- Run a complete export
- Validate the exported zip file contains all expected content

**Run integration tests:**
```bash
make test-integration
# or
go test -tags=integration -v -timeout=20m ./pkg/export/
```

**⚠️ Important Notes:**
- First run will download CockroachDB binaries (~100MB per version) and may take 5-10 minutes
- Subsequent runs use cached binaries and are much faster (1-2 minutes)
- Tests run in parallel for faster execution
- Requires at least 500MB of free disk space for cached binaries
- Use a 20-minute timeout to account for binary downloads

## Cross-Version Compatibility

The integration tests verify compatibility with the versions listed in the `versions`
slice in `pkg/export/integration_test.go`, which is the single source of truth:

- CockroachDB v24.1.25
- CockroachDB v24.3.25
- CockroachDB v25.2.11
- CockroachDB v25.4.17
- CockroachDB v26.1.0-beta.3
- CockroachDB v26.2.7
- CockroachDB v26.3.2
- CockroachDB v26.4.0-alpha.1

**Note for CockroachDB 26.1+:** The exporter automatically detects the version and enables the `allow_unsafe_internals` setting (introduced in v26.1) to access `crdb_internal` tables. This is handled transparently in the `NewExporter` function.

To add or bump a version, edit that slice:

```go
versions := []string{
    "v24.1.25",
    "v24.3.25",
    "v25.2.11",
    "v25.4.17",
    "v26.1.0-beta.3",
    "v26.2.7",
    "v26.3.2",
    "v26.4.0-alpha.1", // Add new versions here
}
```

The version must have a published binary for the platform the tests run on; `testserver`
downloads from `binaries.cockroachdb.com`. Also update the lists in
[COMPATIBILITY.md](COMPATIBILITY.md) and `INTEGRATION_TEST_SUMMARY.md`, which copy this
set by hand.

## CI/CD Integration

### Manual Pre-Release Testing

Before cutting a new release, run the integration tests:

```bash
make test-integration
```

Ensure all versions pass before releasing.

### GitHub Actions (Optional)

To run integration tests in CI, add to `.github/workflows/ci.yaml`:

```yaml
integration-tests:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v3
    - uses: actions/setup-go@v4
      with:
        go-version: '1.21'
    - name: Cache CockroachDB binaries
      uses: actions/cache@v3
      with:
        path: ~/.cockroach-go
        key: ${{ runner.os }}-cockroach-binaries-${{ hashFiles('**/integration_test.go') }}
    - name: Run integration tests
      run: make test-integration
```

**Note:** Integration tests should be run on-demand (workflow_dispatch) or on a schedule, not on every PR, due to their runtime.

## Validation Details

Each integration test validates:

1. ✅ Export completes without error
2. ✅ Zip file is created and non-empty
3. ✅ All expected files are present:
   - `metadata.json`
   - `crdb_internal.statement_statistics.csv`
   - `crdb_internal.transaction_statistics.csv`
   - `crdb_internal.transaction_contention_events.csv`
   - `crdb_internal.gossip_nodes.csv`
   - `crdb_internal.table_indexes.csv`
   - `system.table_statistics.csv`
   - `zone_configurations.txt`
   - `testdb.schema.txt` (test database schema)
4. ✅ CSV files have valid headers
5. ✅ Metadata JSON is valid and contains required fields
6. ✅ Cluster version is correctly captured

## Troubleshooting

### Binary Download Failures

If binary downloads fail, the testserver package will retry automatically. If issues persist:

```bash
# Clear the cache and retry
rm -rf ~/.cockroach-go
make test-integration
```

### Timeout Errors

If tests timeout:
- First run with binary downloads may take 10+ minutes
- Increase timeout: `go test -tags=integration -timeout=30m ./pkg/export/`
- Check available disk space

### Port Conflicts

Each test uses a random port, but if you see "address already in use" errors:
- Tests run in parallel; reduce parallelism with: `GOMAXPROCS=1 make test-integration`

## Performance

Approximate test times:

- **First run** (with downloads): 5-10 minutes
- **Cached runs**: 1-2 minutes
- **Single version**: 20-40 seconds

## Future Enhancements

Potential improvements to integration tests:

- [ ] Test multi-node clusters
- [ ] Test version upgrade scenarios (export from v1, import to v2)
- [ ] Performance benchmarking across versions
- [ ] Test with large datasets
- [ ] Compatibility matrix generation
- [ ] Test failure scenarios (connection loss, disk full, etc.)
