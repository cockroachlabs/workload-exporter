# Troubleshooting Guide

Common issues and solutions when using the workload-exporter tool.

## Connection Issues

### Problem: "connection refused"

**Cause:** Cannot reach the CockroachDB cluster.

**Solutions:**
1. Verify the cluster is running
2. Check the host and port in your connection string
3. Ensure network connectivity (firewall rules, security groups)
4. Verify you're using the correct port (default: 26257)

**Example:**
```bash
# Verify connection with psql or cockroach sql
cockroach sql --url "postgresql://user:password@host:26257/database?sslmode=verify-full"
```

### Problem: "SSL/TLS errors"

**Cause:** SSL mode mismatch or certificate issues.

**Solutions:**
1. For secure clusters, use `sslmode=verify-full` or `sslmode=require`
2. For local/dev clusters, you might use `sslmode=disable` (not recommended for production)
3. Ensure CA certificates are properly configured

**Examples:**
```bash
# Secure cluster with certificate verification
workload-exporter export -c "postgresql://user:password@host:26257/?sslmode=verify-full&sslrootcert=/path/to/ca.crt"

# Local dev cluster (insecure)
workload-exporter export -c "postgresql://user:password@localhost:26257/?sslmode=disable"
```

## Authentication Issues

### Problem: "authentication failed"

**Cause:** Incorrect username or password.

**Solutions:**
1. Verify credentials
2. Check if user exists in the cluster
3. Ensure password is properly URL-encoded in connection string

**Example:**
```bash
# If password contains special characters, URL-encode them
# Password with @ symbol: p@ssword -> p%40ssword
workload-exporter export -c "postgresql://user:p%40ssword@host:26257/?sslmode=verify-full"
```

## Permission Issues

### Problem: "permission denied" or "access restricted"

**Cause:** User lacks required permissions to read system tables.

**Required Permissions:**
- Read access to `crdb_internal` tables
- Read access to system settings
- Read access to all user databases (for schema export)

**Solutions:**
1. Grant admin role: `GRANT admin TO username;`
2. Or grant specific permissions:
   ```sql
   GRANT SELECT ON crdb_internal.* TO username;
   GRANT SELECT ON system.* TO username;
   ```

### Problem: "Access to crdb_internal is restricted" (CockroachDB 26.1+)

**Cause:** The `allow_unsafe_internals` setting is required in CockroachDB 26.1+.

**Solution:** This is handled automatically by the exporter. If you still see this error:
1. Ensure you're using the latest version of the exporter
2. Manually set the session variable before exporting:
   ```sql
   SET allow_unsafe_internals = true;
   ```

## Time Range Issues

### Problem: "Time Format Errors"

**Cause:** Incorrect time format for `--start` or `--end` flags.

**Required Format:** RFC3339 format

**Examples:**
```bash
# Correct formats
--start "2025-04-18T13:25:00Z"                    # UTC
--start "2025-04-18T13:25:00-05:00"               # With timezone offset
--start "2025-04-18T13:25:00.000Z"                # With milliseconds

# Incorrect formats
--start "2025-04-18 13:25:00"                     # Missing 'T' and timezone
--start "04/18/2025 13:25:00"                     # Wrong format
```

### Problem: "Empty Exports" or "No Data in CSV Files"

**Cause:** Time range doesn't contain any data.

**Solutions:**
1. Check the time range covers when your cluster had activity
2. Verify statistics are being collected (check cluster settings)
3. Adjust `--start` and `--end` flags to capture the desired period
4. Default is last 2 hours - adjust if needed

**Example:**
```bash
# Export last 24 hours
workload-exporter export -c "connection-string" \
  -s "$(date -u -d '24 hours ago' +%Y-%m-%dT%H:%M:%SZ)" \
  -e "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

## Export File Issues

### Problem: "Cannot create output file"

**Cause:** Insufficient permissions or disk space.

**Solutions:**
1. Check write permissions in the output directory
2. Verify sufficient disk space
3. Ensure the output path is valid
4. Try specifying a different output location

**Example:**
```bash
# Specify custom output location
workload-exporter export -c "connection-string" -o "/tmp/my-export.zip"
```

### Problem: "Zip file is corrupted"

**Cause:** Export was interrupted or failed.

**Solutions:**
1. Re-run the export
2. Check logs for errors during export
3. Enable debug logging to see detailed progress:
   ```bash
   workload-exporter export -c "connection-string" --debug
   ```

## Performance Issues

### Problem: "Export is very slow"

**Cause:** Large dataset or slow network.

**Solutions:**
1. Reduce the time range to export less data
2. Export during off-peak hours
3. Check network bandwidth to the cluster
4. Large exports may take several minutes - this is normal

### Problem: "Export uses too much memory"

**Cause:** Very large datasets being processed.

**Solutions:**
1. Reduce the time range
2. Run on a machine with more available memory
3. Close other applications

## Debugging

### Enable Debug Logging

For detailed information about what the exporter is doing:

```bash
workload-exporter export -c "connection-string" --debug
```

This will show:
- Connection attempts
- Each query being executed
- Files being created
- Progress of the export

### Check Export Contents

To verify what was exported:

```bash
# List files in the export
unzip -l workload-export.zip

# Extract and examine
unzip workload-export.zip -d export-contents
cd export-contents
cat metadata.json | jq .
head crdb_internal.statement_statistics.csv
```

## Version-Specific Issues

### CockroachDB 24.1 - 24.3

No known version-specific issues.

### CockroachDB 25.x

No known version-specific issues.

### CockroachDB 26.1+

The `allow_unsafe_internals` setting must be enabled. The exporter handles this automatically, but ensure you're using the latest version of the tool.

## Getting Help

If you continue to experience issues:

1. Run with `--debug` flag and save the output
2. Check the [GitHub Issues](https://github.com/cockroachlabs/workload-exporter/issues)
3. Open a new issue with:
   - CockroachDB version
   - Exporter version
   - Full command you ran (redact sensitive info)
   - Error message or unexpected behavior
   - Debug output (if available)

## Common Command Patterns

### Test Your Connection

Before running a full export, test your connection:

```bash
cockroach sql --url "postgresql://user:password@host:26257/?sslmode=verify-full" -e "SELECT version();"
```

### Quick Export for Recent Activity

```bash
# Last 2 hours (default)
workload-exporter export -c "connection-string"

# Last hour
workload-exporter export -c "connection-string" \
  -s "$(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ)"
```

### Full Day Export

```bash
# Export a full day
workload-exporter export -c "connection-string" \
  -s "2025-04-18T00:00:00Z" \
  -e "2025-04-18T23:59:59Z" \
  -o "april-18-export.zip"
```
