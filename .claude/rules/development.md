# Development Tasks & Workflows

## Common Development Tasks

### Adding a New Export Table

1. Add a `Table` entry to `exportTables` in `pkg/export/exporter.go`:
   ```go
   Table{Database: "system", Name: "my_table", TimeColumn: "created_at", Scope: TenantScopeMain},
   ```
   - Set `TimeColumn` to the timestamp column if time-range filtering is needed, or `""` for no filtering.
   - Set `Optional: true` if the table may not exist in all cluster configurations (e.g. Cloud virtual clusters).
   - Use `Database: ""` with a dotted `Name` (e.g. `"crdb_internal.table_indexes"`) to query across all databases.
   - Set `Scope` to indicate which virtual cluster connection to use:
     - `TenantScopeMain` — application virtual cluster (default for most tables)
     - `TenantScopeSystem` — system virtual cluster only (e.g. `gossip_nodes`); auto-detects virtual cluster mode on first failure
     - `TenantScopeBoth` — reserved for future use

2. Add or update a test in `pkg/export/exporter_test.go`.

3. Build and verify:
   ```bash
   go build -o workload-exporter .
   ./workload-exporter export --help
   ```

### Adding a New CLI Command

1. Create a new file in `cmd/` (e.g. `cmd/mycommand.go`).
2. Register the command on the root command in `cmd/root.go`.
3. Implement any supporting logic in `pkg/`.
4. Add tests.
5. Update `--help` text and docs as needed.

### Adding a New Dependency

```bash
go get github.com/some/package
go mod tidy
```

## Build & Verification

```bash
# Standard build
go build -o workload-exporter .

# Build with version info (matches CI/release process)
go build -ldflags="-X main.Version=vX.Y.Z" -o workload-exporter .

# Verify binary works
./workload-exporter --version
./workload-exporter export --help
./workload-exporter update --help
```

## Testing

```bash
# Run all unit tests
go test ./...

# Run with race detector
go test -race ./...

# Run specific package
go test ./pkg/export/...
go test ./pkg/update/...

# Verbose output
go test -v ./pkg/export/...

# Short flag (skips long-running tests if tagged)
go test ./... -short
```

Integration tests require a live CockroachDB cluster. See `docs/TESTING.md`.

## Code Quality

```bash
# Static analysis
go vet ./...

# Format code
go fmt ./...

# Linter (project config: .golangci.yml)
golangci-lint run --timeout=5m
```

## Making Changes

### Standard Workflow

1. **Create feature branch** (optional but recommended):
   ```bash
   git checkout -b feat/my-feature
   ```

2. **Make changes** in `pkg/` and/or `cmd/`.

3. **Check for errors**:
   ```bash
   go vet ./...
   go test ./...
   ```

4. **Build and verify**:
   ```bash
   go build -o workload-exporter .
   ./workload-exporter --version
   ```

5. **Commit**:
   ```bash
   git add .
   git commit -m "feat: description of changes"
   ```

## Feature Completion Checklist

Before marking a feature as complete, verify all items below.

### 1. Code Quality ✅

```bash
go vet ./...
golangci-lint run --timeout=5m
go fmt ./...
```

### 2. Testing ✅

```bash
# Unit tests
go test ./...

# Race detector
go test -race ./...

# Integration tests (downloads CockroachDB binaries on first run — takes several minutes)
go test -tags=integration -v ./pkg/export/
```

### 3. Permissions Update ✅

**IMPORTANT**: Update `.claude/settings.local.json` if you:
- Added new bash commands Claude needs to run
- Added new tool invocations
- Added new file paths that need reading

**After updating, always prompt the user to test**:
```
I've updated .claude/settings.local.json with new permissions.
Please test the following command to verify it works:
  [example command here]
```

**Common permission patterns**:
```json
"Bash(go test ./pkg/mynewpkg/...)"   // New package tests
"Bash(./workload-exporter mycommand:*)"  // New CLI command
"WebFetch(domain:example.com)"       // New web domain
```

### 4. Documentation ✅

- `docs/DEVELOPMENT.md` — update if workflows changed
- `docs/TESTING.md` — update if testing approach changed
- `.claude/rules/development.md` — update if development patterns changed
- Code comments — document complex or non-obvious logic
- CLI `--help` text — update if commands or flags changed
- `README.md` — update if user-facing behaviour changed

### 5. Build Verification ✅

```bash
go build -o workload-exporter .
./workload-exporter --version
./workload-exporter export --help
```

### 6. Git Hygiene ✅

```bash
# Review what's changing
git status
git diff

# Check for debug leftovers
grep -r "fmt.Println\|log.Print" pkg/ cmd/

# Check for sensitive data
grep -r "PASSWORD\|SECRET\|API_KEY" .
```

- No commented-out code blocks
- Commit message follows `type: description` format (`feat:`, `fix:`, `docs:`, `refactor:`)

### 7. User Communication ✅

After completing a feature, provide:
1. **Summary** of what was implemented
2. **Example commands** showing how to use it
3. **Testing results** from your verification
4. **Documentation locations** that were updated
5. **Any caveats or limitations** to be aware of

## Feature Completion Template

```markdown
## [Feature Name] - Completion Report

### ✅ Code Quality
- [ ] go vet passes
- [ ] golangci-lint passes
- [ ] go fmt applied

### ✅ Testing
- [ ] Unit tests pass (`go test ./...`)
- [ ] Race detector clean (`go test -race ./...`)
- [ ] Integration tests pass (`go test -tags=integration -v ./pkg/export/`)

### ✅ Permissions
- [ ] .claude/settings.local.json updated (if needed)
- [ ] User prompted to test new permissions

### ✅ Documentation
- [ ] docs/ updated (if workflows changed)
- [ ] .claude/rules/ updated (if patterns changed)
- [ ] Code comments added
- [ ] --help text updated

### ✅ Build
- [ ] Clean build successful
- [ ] Binary verified working

### ✅ Git
- [ ] No debug code
- [ ] No sensitive data
- [ ] Meaningful commit message

### 📊 Summary
[Brief summary of what was implemented]

### 🧪 Testing Results
[What you tested and results]

### 📚 Documentation
[List of files updated]

### ⚠️ Limitations
[Any known limitations or future improvements]
```

## Debugging

```bash
# Enable debug logging
./workload-exporter export -c "connection-string" --debug

# Check CI pipeline config
cat .github/workflows/ci.yaml

# Inspect linter config
cat .golangci.yml
```

## Getting Help

When stuck:
1. Check `.claude/rules/` documentation
2. Look at similar existing code in `pkg/export/exporter.go`
3. Read test files for usage examples (`pkg/export/exporter_test.go`)
4. Use `--help` flag on CLI commands
5. Check recent git commits for similar changes
