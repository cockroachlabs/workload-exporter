package export

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCleanConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "Basic connection string with password",
			input:    "postgresql://user:password@localhost:26257/defaultdb",
			expected: "postgresql://user@localhost:26257/defaultdb",
			wantErr:  false,
		},
		{
			name:     "Connection string without password",
			input:    "postgresql://user@localhost:26257/defaultdb",
			expected: "postgresql://user@localhost:26257/defaultdb",
			wantErr:  false,
		},
		{
			name:     "Connection string with query parameters",
			input:    "postgresql://user:password@localhost:26257/defaultdb?sslmode=verify-full",
			expected: "postgresql://user@localhost:26257/defaultdb?sslmode=verify-full",
			wantErr:  false,
		},
		{
			name:     "Invalid connection string",
			input:    "://invalid",
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cleanConnectionString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("cleanConnectionString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("cleanConnectionString() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestStartTime(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "Round down to hour",
			input:    time.Date(2025, 4, 18, 13, 45, 30, 0, time.UTC),
			expected: time.Date(2025, 4, 18, 13, 0, 0, 0, time.UTC),
		},
		{
			name:     "Already at hour boundary",
			input:    time.Date(2025, 4, 18, 13, 0, 0, 0, time.UTC),
			expected: time.Date(2025, 4, 18, 13, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := startTime(tt.input)
			if !got.Equal(tt.expected) {
				t.Errorf("startTime() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestEndTime(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Time
		expected time.Time
	}{
		{
			name:     "Round to end of hour",
			input:    time.Date(2025, 4, 18, 13, 45, 30, 0, time.UTC),
			expected: time.Date(2025, 4, 18, 13, 59, 59, 0, time.UTC),
		},
		{
			name:     "From hour boundary",
			input:    time.Date(2025, 4, 18, 13, 0, 0, 0, time.UTC),
			expected: time.Date(2025, 4, 18, 13, 59, 59, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endTime(tt.input)
			if !got.Equal(tt.expected) {
				t.Errorf("endTime() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConfig(t *testing.T) {
	config := Config{
		ConnectionString: "postgresql://user:pass@localhost:26257/db",
		OutputFile:       "test.zip",
		TimeRange: TimeRange{
			Start: time.Now().Add(-1 * time.Hour),
			End:   time.Now(),
		},
	}

	if config.ConnectionString == "" {
		t.Error("ConnectionString should not be empty")
	}
	if config.OutputFile == "" {
		t.Error("OutputFile should not be empty")
	}
	if config.TimeRange.Start.After(config.TimeRange.End) {
		t.Error("Start time should be before End time")
	}
}

func TestTimeRange(t *testing.T) {
	now := time.Now()
	tr := TimeRange{
		Start: now.Add(-6 * time.Hour),
		End:   now,
	}

	if tr.Start.After(tr.End) {
		t.Error("Start should be before End")
	}

	duration := tr.End.Sub(tr.Start)
	if duration != 6*time.Hour {
		t.Errorf("Duration should be 6 hours, got %v", duration)
	}
}

func TestExporterVersion(t *testing.T) {
	if ExporterVersion == "" {
		t.Error("ExporterVersion should not be empty")
	}
}

func TestTable(t *testing.T) {
	table := Table{
		Database:   "crdb_internal",
		Name:       "statement_statistics",
		TimeColumn: "aggregated_ts",
	}

	if table.Database == "" {
		t.Error("Database should not be empty")
	}
	if table.Name == "" {
		t.Error("Name should not be empty")
	}
	if table.TimeColumn == "" {
		t.Error("TimeColumn should not be empty")
	}
}

func TestExportTables(t *testing.T) {
	if len(exportTables) == 0 {
		t.Error("exportTables should not be empty")
	}

	validScopes := map[TenantScope]bool{
		TenantScopeMain:   true,
		TenantScopeSystem: true,
		TenantScopeBoth:   true,
	}

	for i, table := range exportTables {
		// Database can be empty for cross-database queries (e.g., "".crdb_internal.table_indexes)
		// but Name must always be present
		if table.Name == "" {
			t.Errorf("exportTables[%d].Name should not be empty", i)
		}
		if !validScopes[table.Scope] {
			t.Errorf("exportTables[%d] (%s.%s) has invalid or missing Scope %q", i, table.Database, table.Name, table.Scope)
		}
	}
}

func TestExportTablesIncludesIndexUsageStatistics(t *testing.T) {
	found := false
	for _, table := range exportTables {
		if table.Database == "" && table.Name == "crdb_internal.index_usage_statistics" {
			found = true
			if table.Scope != TenantScopeMain {
				t.Errorf("crdb_internal.index_usage_statistics should have Scope TenantScopeMain, got %q", table.Scope)
			}
			if table.TimeColumn != "" {
				t.Errorf("crdb_internal.index_usage_statistics should have no TimeColumn, got %q", table.TimeColumn)
			}
		}
	}
	if !found {
		t.Error("exportTables should contain crdb_internal.index_usage_statistics")
	}
}

func TestExportTablesIncludesNodeCPUMem(t *testing.T) {
	found := false
	for _, table := range exportTables {
		if table.Database == "crdb_internal" && table.Name == "node_cpu_mem" {
			found = true
			if table.Scope != TenantScopeSystem {
				t.Errorf("crdb_internal.node_cpu_mem should have Scope TenantScopeSystem, got %q", table.Scope)
			}
			if !table.Optional {
				t.Error("crdb_internal.node_cpu_mem should be Optional")
			}
			if table.Query == "" {
				t.Error("crdb_internal.node_cpu_mem should have a custom Query")
			}
			if !strings.Contains(table.Query, "num_vcpus") {
				t.Error("crdb_internal.node_cpu_mem Query should select num_vcpus")
			}
			if !strings.Contains(table.Query, "total_mem_gib") {
				t.Error("crdb_internal.node_cpu_mem Query should select total_mem_gib")
			}
		}
	}
	if !found {
		t.Error("exportTables should contain crdb_internal.node_cpu_mem")
	}
}

func TestExportTablesIncludesClusterSettings(t *testing.T) {
	found := false
	for _, table := range exportTables {
		if table.Database == "crdb_internal" && table.Name == "cluster_settings" {
			found = true
			if table.Scope != TenantScopeBoth {
				t.Errorf("crdb_internal.cluster_settings should have Scope TenantScopeBoth, got %q", table.Scope)
			}
			if table.TimeColumn != "" {
				t.Errorf("crdb_internal.cluster_settings should have no TimeColumn, got %q", table.TimeColumn)
			}
			if table.RedactKeyColumn != "variable" {
				t.Errorf("crdb_internal.cluster_settings RedactKeyColumn should be \"variable\", got %q", table.RedactKeyColumn)
			}
			if table.RedactColumn != "value" {
				t.Errorf("crdb_internal.cluster_settings RedactColumn should be \"value\", got %q", table.RedactColumn)
			}
			if len(table.RedactedKeys) == 0 {
				t.Error("crdb_internal.cluster_settings RedactedKeys should not be empty")
			}
		}
	}
	if !found {
		t.Error("exportTables should contain crdb_internal.cluster_settings")
	}
}

func TestExportTablesIncludesSystemSettings(t *testing.T) {
	found := false
	for _, table := range exportTables {
		if table.Database == "system" && table.Name == "settings" {
			found = true
			if table.Scope != TenantScopeBoth {
				t.Errorf("system.settings should have Scope TenantScopeBoth, got %q", table.Scope)
			}
			if table.RedactKeyColumn != "name" {
				t.Errorf("system.settings RedactKeyColumn should be \"name\", got %q", table.RedactKeyColumn)
			}
			if table.RedactColumn != "value" {
				t.Errorf("system.settings RedactColumn should be \"value\", got %q", table.RedactColumn)
			}
			if len(table.RedactedKeys) == 0 {
				t.Error("system.settings RedactedKeys should not be empty")
			}
		}
	}
	if !found {
		t.Error("exportTables should contain system.settings")
	}
}

func TestSensitiveClusterSettings(t *testing.T) {
	expected := []string{
		"server.host_based_authentication.configuration",
		"server.identity_map.configuration",
		"server.jwt_authentication.issuers.custom_ca",
		"server.ldap_authentication.domain.custom_ca",
		"server.ldap_authentication.client.tls_certificate",
		"server.ldap_authentication.client.tls_key",
		"server.oidc_authentication.client_id",
		"server.oidc_authentication.client_secret",
		"server.oidc_authentication.provider.custom_ca",
		"sql.override.allow_unsafe_internals.enabled",
		"cluster.secret",
		"cluster.label",
		"enterprise.license",
	}
	for _, want := range expected {
		found := false
		for _, got := range sensitiveClusterSettings {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("sensitiveClusterSettings is missing %q", want)
		}
	}
}

func TestBuildSelectExpr(t *testing.T) {
	columns := []string{"variable", "value", "type", "description"}

	t.Run("no redaction returns star", func(t *testing.T) {
		table := Table{RedactColumn: "", RedactKeyColumn: "", RedactedKeys: nil}
		got := buildSelectExpr(columns, table)
		if got != "*" {
			t.Errorf("expected \"*\", got %q", got)
		}
	})

	t.Run("empty RedactedKeys returns star", func(t *testing.T) {
		table := Table{RedactColumn: "value", RedactKeyColumn: "variable", RedactedKeys: []string{}}
		got := buildSelectExpr(columns, table)
		if got != "*" {
			t.Errorf("expected \"*\", got %q", got)
		}
	})

	t.Run("redacted column gets CASE expression", func(t *testing.T) {
		table := Table{
			RedactColumn:    "value",
			RedactKeyColumn: "variable",
			RedactedKeys:    []string{"cluster.secret", "enterprise.license"},
		}
		got := buildSelectExpr(columns, table)
		// Must contain a CASE expression for the value column
		if !contains(got, "CASE WHEN") {
			t.Errorf("expected CASE expression in SELECT, got %q", got)
		}
		// Must contain the redacted key literals
		if !contains(got, "'cluster.secret'") {
			t.Errorf("expected 'cluster.secret' in SELECT, got %q", got)
		}
		if !contains(got, "'enterprise.license'") {
			t.Errorf("expected 'enterprise.license' in SELECT, got %q", got)
		}
		// Must contain the redaction placeholder
		if !contains(got, "'<redacted>'") {
			t.Errorf("expected '<redacted>' in SELECT, got %q", got)
		}
		// Non-redacted columns must appear as plain identifiers
		if !contains(got, `"variable"`) {
			t.Errorf("expected \"variable\" column in SELECT, got %q", got)
		}
		if !contains(got, `"type"`) {
			t.Errorf("expected \"type\" column in SELECT, got %q", got)
		}
	})

	t.Run("single-quote in key is escaped", func(t *testing.T) {
		table := Table{
			RedactColumn:    "value",
			RedactKeyColumn: "variable",
			RedactedKeys:    []string{"it's.a.key"},
		}
		got := buildSelectExpr(columns, table)
		if !contains(got, "'it''s.a.key'") {
			t.Errorf("expected escaped single-quote in SELECT, got %q", got)
		}
	})
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestIsVirtualClusterError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "virtual cluster error",
			err:      fmt.Errorf("ERROR: operation is unsupported within a virtual cluster (SQLSTATE XXUUU)"),
			expected: true,
		},
		{
			name:     "wrapped virtual cluster error",
			err:      fmt.Errorf("failed to query gossip_nodes: operation is unsupported within a virtual cluster"),
			expected: true,
		},
		{
			name:     "unrelated error",
			err:      fmt.Errorf("connection refused"),
			expected: false,
		},
		{
			name:     "permission denied error",
			err:      fmt.Errorf("ERROR: permission denied for table gossip_nodes"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isVirtualClusterError(tt.err)
			if got != tt.expected {
				t.Errorf("isVirtualClusterError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestBuildSystemConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "basic URL",
			input:    "postgresql://user@localhost:26257/defaultdb",
			expected: "postgresql://user@localhost:26257/defaultdb?options=-ccluster%3Dsystem",
			wantErr:  false,
		},
		{
			name:     "URL with existing query params",
			input:    "postgresql://user@localhost:26257/defaultdb?sslmode=verify-full",
			expected: "postgresql://user@localhost:26257/defaultdb?options=-ccluster%3Dsystem&sslmode=verify-full",
			wantErr:  false,
		},
		{
			name:     "URL with existing options param",
			input:    "postgresql://user@localhost:26257/defaultdb?options=-csomething%3Dvalue",
			expected: "postgresql://user@localhost:26257/defaultdb?options=-csomething%3Dvalue+-ccluster%3Dsystem",
			wantErr:  false,
		},
		{
			name:     "URL with password is preserved",
			input:    "postgresql://user:secret@localhost:26257/defaultdb",
			expected: "postgresql://user:secret@localhost:26257/defaultdb?options=-ccluster%3Dsystem",
			wantErr:  false,
		},
		{
			name:    "invalid URL",
			input:   "://invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildSystemConnectionString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("buildSystemConnectionString() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("buildSystemConnectionString() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestParseMajorVersion(t *testing.T) {
	tests := []struct {
		name        string
		versionStr  string
		expected    int
		expectError bool
	}{
		{
			name:        "CockroachDB v26.1.0-beta.3",
			versionStr:  "CockroachDB CCL v26.1.0-beta.3 (x86_64-apple-darwin19, built 2024/01/01 00:00:00, go1.21.5)",
			expected:    26,
			expectError: false,
		},
		{
			name:        "CockroachDB v25.2.11",
			versionStr:  "CockroachDB CCL v25.2.11 (x86_64-unknown-linux-gnu, built 2024/01/01 00:00:00, go1.21.5)",
			expected:    25,
			expectError: false,
		},
		{
			name:        "CockroachDB v24.3.25",
			versionStr:  "CockroachDB CCL v24.3.25 (x86_64-unknown-linux-gnu, built 2024/01/01 00:00:00, go1.21.5)",
			expected:    24,
			expectError: false,
		},
		{
			name:        "CockroachDB v24.1.25",
			versionStr:  "CockroachDB CCL v24.1.25 (x86_64-unknown-linux-gnu, built 2024/01/01 00:00:00, go1.21.5)",
			expected:    24,
			expectError: false,
		},
		{
			name:        "Simple version",
			versionStr:  "v26.1.0",
			expected:    26,
			expectError: false,
		},
		{
			name:        "Invalid version string",
			versionStr:  "PostgreSQL 14.0",
			expected:    0,
			expectError: true,
		},
		{
			name:        "Empty string",
			versionStr:  "",
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, err := parseMajorVersion(tt.versionStr)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if major != tt.expected {
					t.Errorf("parseMajorVersion() = %d, want %d", major, tt.expected)
				}
			}
		})
	}
}
