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
- First run downloads a CockroachDB binary per version: ~125-145 MB compressed each, about 1.1 GB for the current eight-version matrix
- Binaries extract to ~275-325 MB each, about 2.4 GB total, and are cached in the system temp directory (`$TMPDIR`, or `/tmp` on Linux)
- Subsequent runs reuse the cached binaries and skip the downloads entirely
- Subtests run in parallel, bounded by `-parallel` (default `GOMAXPROCS`)
- A 20-minute timeout is ample; see [Performance](#performance) for measured times

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

### GitHub Actions

Integration tests run nightly via `.github/workflows/integration.yaml`, at 07:37 UTC
against the default branch, plus on demand:

```bash
gh workflow run integration.yaml --ref main
```

They are deliberately *not* part of `ci.yaml`, which runs on every push and pull
request: a run starts a real CockroachDB node per version and pulls ~1.1 GB of binaries,
which is too slow and too bandwidth-heavy for per-push CI.

Two details of that workflow worth knowing if you change it:

- Binaries are cached on a key derived from the version list itself rather than from
  `hashFiles()` over `integration_test.go`, so editing an assertion does not discard the
  cache. Bumping a version does, by design.
- It runs with `-parallel 2`. The default is `GOMAXPROCS` (4 on a standard runner), and
  each cluster has a hard 60-second startup deadline that simultaneous bootstraps on
  4 vCPU could miss.

Note that `workflow_dispatch` only works once the workflow is on the default branch;
dispatching it from a feature branch returns a 404.

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

`testserver` caches each binary in the system temp directory, not under a dedicated
dotfile directory. If a download fails or leaves a truncated binary, remove the cached
copies and retry:

```bash
# Linux
rm -f /tmp/cockroach-v*
# macOS (TMPDIR is per-user, not /tmp)
rm -f "$TMPDIR"/cockroach-v*

make test-integration
```

A binary is only reused when it is present with mode `0555`, so a partial download is
re-fetched rather than used.

### Timeout Errors

If tests time out:
- Confirm the downloads are progressing; a cold cache pulls ~1.1 GB
- Increase the timeout: `go test -tags=integration -timeout=30m ./pkg/export/`
- Each cluster has its own 60-second deadline to start and publish its listening URL,
  independent of the `go test` timeout. On a CPU-constrained machine, lower
  `-parallel` rather than raising the timeout.

### Port Conflicts

Each test uses a random port, but if you see "address already in use" errors, reduce how
many clusters run at once:

```bash
go test -tags=integration -v -parallel 1 ./pkg/export/
```

## Performance

Measured, not estimated. On a GitHub-hosted `ubuntu-latest` runner (4 vCPU, 16 GB RAM)
with `-parallel 2`, the whole job including a cold-cache download of all eight binaries:

| | |
|---|---|
| Full job, cold cache (8 versions) | ~60 s |
| Per version | 6-8 s |

On a 12-core workstation with a warm cache, the same eight versions take ~45 s.

The downloads are a smaller share of the total than they look: the runner pulls ~1.1 GB
in well under a minute. Disk is not a constraint either — a standard runner has ~86 GB
free, against ~2.4 GB of extracted binaries.

## Future Enhancements

Potential improvements to integration tests:

- [ ] Test multi-node clusters
- [ ] Test version upgrade scenarios (export from v1, import to v2)
- [ ] Performance benchmarking across versions
- [ ] Test with large datasets
- [ ] Compatibility matrix generation
- [ ] Test failure scenarios (connection loss, disk full, etc.)
