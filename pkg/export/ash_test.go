package export

import (
	"reflect"
	"testing"
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
