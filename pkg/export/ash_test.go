package export

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestASHTables(t *testing.T) {
	if len(ashTables) != 2 {
		t.Fatalf("expected 2 ASH tables, got %d", len(ashTables))
	}

	// Persisted history must come first so the longest history is exported before the
	// in-memory buffer.
	if ashTables[0].Name != ashPersistedView {
		t.Errorf("ashTables[0].Name = %q, want %q", ashTables[0].Name, ashPersistedView)
	}
	if ashTables[1].Name != ashClusterView {
		t.Errorf("ashTables[1].Name = %q, want %q", ashTables[1].Name, ashClusterView)
	}

	for i, table := range ashTables {
		if table.Database != ashSchema {
			t.Errorf("ashTables[%d].Database = %q, want %q", i, table.Database, ashSchema)
		}
		if table.TimeColumn != "sample_time" {
			t.Errorf("ashTables[%d].TimeColumn = %q, want \"sample_time\"", i, table.TimeColumn)
		}
		if !table.Optional {
			t.Errorf("ashTables[%d] (%s) should be Optional", i, table.Name)
		}
		// ASH is sampled once per second per active session, so the whole-hour widening
		// applied to the aggregated SQL statistics tables would pull in a large amount
		// of data the user did not ask for.
		if !table.ExactTimeRange {
			t.Errorf("ashTables[%d] (%s) should set ExactTimeRange", i, table.Name)
		}
		if table.Scope != TenantScopeMain {
			t.Errorf("ashTables[%d] (%s) Scope = %q, want %q", i, table.Name, table.Scope, TenantScopeMain)
		}
	}
}

// ASH tables are appended per cluster after capability detection, so they must not be
// part of the unconditional export list.
func TestExportTablesExcludesASH(t *testing.T) {
	for _, table := range exportTables {
		if table.Name == ashPersistedView || table.Name == ashClusterView {
			t.Errorf("exportTables should not contain ASH view %q; it is added only when the cluster supports ASH", table.Name)
		}
	}
}

func TestOrderASHViews(t *testing.T) {
	tests := []struct {
		name     string
		present  []string
		expected []string
	}{
		{
			name:     "no ASH views (pre-v26.2)",
			present:  nil,
			expected: nil,
		},
		{
			name:     "in-memory view only (v26.2)",
			present:  []string{ashClusterView},
			expected: []string{ashClusterView},
		},
		{
			name:     "both views, persisted first (v26.3+)",
			present:  []string{ashClusterView, ashPersistedView},
			expected: []string{ashPersistedView, ashClusterView},
		},
		{
			name:     "unknown views are ignored",
			present:  []string{"crdb_node_active_session_history", ashClusterView},
			expected: []string{ashClusterView},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orderASHViews(tt.present)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("orderASHViews(%v) = %v, want %v", tt.present, got, tt.expected)
			}
		})
	}
}

func TestASHTablesFor(t *testing.T) {
	t.Run("no views yields no tables", func(t *testing.T) {
		if got := ashTablesFor(nil); len(got) != 0 {
			t.Errorf("ashTablesFor(nil) = %v, want empty", got)
		}
	})

	t.Run("in-memory view only", func(t *testing.T) {
		got := ashTablesFor([]string{ashClusterView})
		if len(got) != 1 || got[0].Name != ashClusterView {
			t.Errorf("ashTablesFor([%s]) = %v, want the in-memory view only", ashClusterView, got)
		}
	})

	t.Run("both views in preference order", func(t *testing.T) {
		got := ashTablesFor([]string{ashClusterView, ashPersistedView})
		if len(got) != 2 {
			t.Fatalf("expected 2 tables, got %d", len(got))
		}
		if got[0].Name != ashPersistedView || got[1].Name != ashClusterView {
			t.Errorf("ashTablesFor returned %q, %q; want %q, %q", got[0].Name, got[1].Name, ashPersistedView, ashClusterView)
		}
	})
}

// A cluster with only the in-memory view applies obs.ash.response_limit per node, newest
// samples first, before the sample_time predicate is applied. A historical window
// therefore exports an empty CSV rather than failing, so the exporter warns instead of
// leaving the gap silent.
func TestASHRangeWarning(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	limit := int64(10000)

	rangeEnding := func(end time.Time) TimeRange {
		return TimeRange{Start: end.Add(-time.Hour), End: end}
	}

	tests := []struct {
		name          string
		views         []string
		responseLimit *int64
		timeRange     TimeRange
		wantWarning   bool
	}{
		{
			name:        "no ASH at all",
			views:       nil,
			timeRange:   rangeEnding(now.Add(-24 * time.Hour)),
			wantWarning: false,
		},
		{
			name:        "persisted view serves historical ranges",
			views:       []string{ashPersistedView, ashClusterView},
			timeRange:   rangeEnding(now.Add(-24 * time.Hour)),
			wantWarning: false,
		},
		{
			name:        "persisted view only",
			views:       []string{ashPersistedView},
			timeRange:   rangeEnding(now.Add(-24 * time.Hour)),
			wantWarning: false,
		},
		{
			name:        "in-memory only, range ends now",
			views:       []string{ashClusterView},
			timeRange:   rangeEnding(now),
			wantWarning: false,
		},
		{
			name:        "in-memory only, range ends within the grace period",
			views:       []string{ashClusterView},
			timeRange:   rangeEnding(now.Add(-time.Minute)),
			wantWarning: false,
		},
		{
			name:        "in-memory only, historical range",
			views:       []string{ashClusterView},
			timeRange:   rangeEnding(now.Add(-24 * time.Hour)),
			wantWarning: true,
		},
		{
			name:          "in-memory only, historical range, limit known",
			views:         []string{ashClusterView},
			responseLimit: &limit,
			timeRange:     rangeEnding(now.Add(-24 * time.Hour)),
			wantWarning:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ashRangeWarning(tt.views, tt.responseLimit, tt.timeRange, now)
			if tt.wantWarning && got == "" {
				t.Fatalf("ashRangeWarning(%v) = \"\", want a warning", tt.views)
			}
			if !tt.wantWarning && got != "" {
				t.Fatalf("ashRangeWarning(%v) = %q, want no warning", tt.views, got)
			}
			if !tt.wantWarning {
				return
			}
			// The limit is the actionable part of the message: substituted when the
			// setting was read, named when it was not.
			if tt.responseLimit != nil && !strings.Contains(got, "10000 samples") {
				t.Errorf("warning should name the response limit value, got %q", got)
			}
			if tt.responseLimit == nil && !strings.Contains(got, "obs.ash.response_limit") {
				t.Errorf("warning should name the response limit setting, got %q", got)
			}
		})
	}
}
