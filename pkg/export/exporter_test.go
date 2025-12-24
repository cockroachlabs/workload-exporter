package export

import (
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

	for i, table := range exportTables {
		if table.Database == "" {
			t.Errorf("exportTables[%d].Database should not be empty", i)
		}
		if table.Name == "" {
			t.Errorf("exportTables[%d].Name should not be empty", i)
		}
	}
}
