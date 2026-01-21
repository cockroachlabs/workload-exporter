# Version Compatibility

This document details CockroachDB version compatibility for the workload-exporter tool.

## Supported Versions

The workload-exporter tool supports **CockroachDB 24.1 and later**.

| CockroachDB Version | Support Status | Notes |
|---------------------|----------------|-------|
| 26.1.x | ✅ Supported | Requires automatic `allow_unsafe_internals` enablement |
| 25.4.x | ✅ Supported | Fully tested |
| 25.2.x | ✅ Supported | Fully tested |
| 24.3.x | ✅ Supported | Fully tested |
| 24.2.x | ✅ Supported | Fully tested |
| 24.1.x | ✅ Supported | Fully tested |
| < 24.1 | ⚠️  May work | Not tested, not officially supported |

## Version-Specific Behavior

### CockroachDB 26.1+

**Change:** Introduction of `allow_unsafe_internals` session variable

In CockroachDB 26.1, a new security feature requires the `allow_unsafe_internals` session variable to be explicitly set to `true` to access `crdb_internal` tables.

**Impact:** The workload-exporter automatically detects CockroachDB 26.1+ and enables this setting. No user action is required.

**Technical Details:**
- The exporter queries the cluster version after connecting
- If major version >= 26, it executes: `SET allow_unsafe_internals = true`
- This happens transparently during the `NewExporter` initialization

### CockroachDB 24.1 - 25.4

No version-specific handling required. The exporter works with default settings.

## Testing

The workload-exporter includes comprehensive integration tests that validate functionality across all supported versions:

- v24.1.25
- v24.3.25
- v25.2.11
- v25.4.3
- v26.1.0-beta.3

See [TESTING.md](TESTING.md) for details on running integration tests.

## Exported Data Compatibility

All exported data uses standard formats that are compatible across versions:

- **CSV files**: Standard CSV format with headers
- **metadata.json**: Standard JSON format
- **Schema files**: SQL CREATE statements
- **Zone configurations**: SQL statements

### Format Stability

The export format has been stable since v1.0.0. Future versions will maintain backward compatibility:

- New files may be added to exports
- Existing files will maintain the same structure
- Column additions to CSV files will be appended at the end

## Required Permissions

Permissions required are consistent across all supported versions:

1. **Read access to `crdb_internal` tables**
   - For CockroachDB 26.1+: Also requires `allow_unsafe_internals = true` (automatic)

2. **Read access to system settings**
   - `SHOW CLUSTER SETTING` permissions

3. **Read access to user databases**
   - For schema export: `SHOW CREATE ALL TABLES` permissions

**Recommended:** Grant `admin` role for simplest setup
```sql
GRANT admin TO your_user;
```

## Deprecated Versions

### CockroachDB < 24.1

While the exporter may work with versions prior to 24.1, these are not tested or officially supported. We recommend:

1. Upgrading to CockroachDB 24.1 or later
2. Testing the exporter in a non-production environment first
3. Reporting any issues via [GitHub Issues](https://github.com/cockroachlabs/workload-exporter/issues)

## Future Compatibility

The exporter is designed to be forward-compatible with future CockroachDB versions:

- **Version detection**: Automatically adapts to new versions
- **Graceful degradation**: If new features are unavailable, continues with available data
- **Error handling**: Logs warnings for unsupported features without failing the export

## Checking Compatibility

### Check Your CockroachDB Version

```sql
SELECT version();
```

Or via CLI:
```bash
cockroach version
```

### Check Exporter Version

```bash
workload-exporter version
```

### Verify Compatibility

The exporter will log the detected CockroachDB version and any version-specific handling:

```bash
workload-exporter export -c "connection-string" --debug
```

Look for log messages like:
```
INFO connecting to cluster at 'postgresql://user@host:26257/'
INFO detected CockroachDB v26.x, enabling allow_unsafe_internals
```

## Upgrading CockroachDB

When upgrading your CockroachDB cluster:

1. **Before upgrade:** Create a workload export for baseline
2. **After upgrade:** Test the exporter against the new version
3. **Verify:** Compare export contents to ensure all data is captured

The exporter should work seamlessly across upgrades without changes.

## Reporting Compatibility Issues

If you encounter compatibility issues:

1. Note the CockroachDB version: `SELECT version();`
2. Note the exporter version: `workload-exporter version`
3. Run with debug logging: `--debug`
4. [Open an issue](https://github.com/cockroachlabs/workload-exporter/issues) with:
   - Both versions
   - Error message or unexpected behavior
   - Debug output

## Compatibility Testing

We continuously test against multiple CockroachDB versions. See our [CI configuration](.github/workflows/ci.yaml) for the current test matrix.

Integration tests run against:
- Latest patch release of each minor version
- Current beta/RC versions for upcoming releases
