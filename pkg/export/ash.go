package export

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/sirupsen/logrus"
)

// Active Session History (ASH) periodically samples the work that active sessions are
// doing, along with the event each sample was waiting on or consuming time in. It is
// exposed through stable views in the information_schema catalog:
//
//   - crdb_cluster_active_session_history   (v26.2+) cluster-wide in-memory samples,
//     bounded by obs.ash.buffer_size and obs.ash.response_limit
//   - crdb_persisted_active_session_history (v26.3+) samples flushed to
//     system.active_session_history every obs.ash.flush.interval and retained for
//     obs.ash.compaction.retention_period (7 days by default)
//
// Clusters older than v26.2 have no ASH at all; the views are absent and the ASH export
// is skipped. Availability is determined by probing the catalog rather than parsing the
// cluster version, so the export adapts to clusters where ASH is backported or removed.
//
// The information_schema views are preferred over their crdb_internal counterparts
// because they are the supported interface and remain readable without
// allow_unsafe_internals in v26.1+.
const ashSchema = "information_schema"

const (
	// ashClusterView is the in-memory, cluster-wide ASH view (v26.2+).
	ashClusterView = "crdb_cluster_active_session_history"
	// ashPersistedView is the view over persisted ASH samples (v26.3+).
	ashPersistedView = "crdb_persisted_active_session_history"
)

// ashTables lists the ASH relations the exporter knows about, in preference order:
// persisted history first (covers the full time range), then the in-memory cluster view
// (covers the most recent samples that have not been flushed yet). Both are filtered on
// sample_time and are Optional so that a cluster which exposes a view but denies access
// to it degrades to a warning rather than failing the export.
var ashTables = []Table{
	{Database: ashSchema, Name: ashPersistedView, TimeColumn: "sample_time", Optional: true, Scope: TenantScopeMain},
	{Database: ashSchema, Name: ashClusterView, TimeColumn: "sample_time", Optional: true, Scope: TenantScopeMain},
}

// ASHInfo records Active Session History availability and configuration for the exported
// cluster. It is serialized into metadata.json so that consumers know whether ASH data is
// present and how to interpret it (for example, database time is approximated by the
// sample count multiplied by SampleInterval).
type ASHInfo struct {
	// Available reports whether the cluster exposes any ASH view.
	Available bool `json:"available"`
	// Enabled is the value of obs.ash.enabled. When false, ASH exports may be empty.
	Enabled bool `json:"enabled"`
	// EnrichmentEnabled is the value of obs.ash.enrichment.enabled. When false, the
	// per-execution columns (user, plan_gist, canary_stats, txn_id, session_id) are NULL.
	EnrichmentEnabled bool `json:"enrichment_enabled"`
	// SampleInterval is the value of obs.ash.sample_interval, the time each sample represents.
	SampleInterval time.Duration `json:"sample_interval,omitempty"`
	// RetentionPeriod is the value of obs.ash.compaction.retention_period, the age beyond
	// which persisted samples are pruned. Zero when the cluster has no persisted ASH.
	RetentionPeriod time.Duration `json:"retention_period,omitempty"`
	// Views lists the ASH views that were found and exported, in export order.
	Views []string `json:"views,omitempty"`
}

// detectASH probes the cluster for ASH views and reads the ASH cluster settings.
// It never fails the export: a cluster without ASH simply reports Available false, and
// settings that cannot be read are left at their zero value.
func (exporter *Exporter) detectASH(ctx context.Context) ASHInfo {
	var info ASHInfo

	views, err := exporter.ashViews(ctx)
	if err != nil {
		logrus.WithError(err).Warn("failed to probe for active session history views; skipping ASH export")
		return info
	}
	if len(views) == 0 {
		logrus.Info("cluster does not provide active session history (requires v26.2 or later); skipping ASH export")
		return info
	}

	info.Available = true
	info.Views = views
	logrus.Infof("detected active session history views: %v", views)

	if enabled, err := exporter.boolClusterSetting(ctx, "obs.ash.enabled"); err != nil {
		logrus.WithError(err).Debug("failed to read obs.ash.enabled")
	} else {
		info.Enabled = enabled
		if !enabled {
			logrus.Warn("obs.ash.enabled is false; exported active session history data may be empty")
		}
	}

	if enriched, err := exporter.boolClusterSetting(ctx, "obs.ash.enrichment.enabled"); err != nil {
		logrus.WithError(err).Debug("failed to read obs.ash.enrichment.enabled")
	} else {
		info.EnrichmentEnabled = enriched
	}

	if interval, err := exporter.durationClusterSetting(ctx, "obs.ash.sample_interval"); err != nil {
		logrus.WithError(err).Debug("failed to read obs.ash.sample_interval")
	} else {
		info.SampleInterval = interval
	}

	if slices.Contains(views, ashPersistedView) {
		if retention, err := exporter.durationClusterSetting(ctx, "obs.ash.compaction.retention_period"); err != nil {
			logrus.WithError(err).Debug("failed to read obs.ash.compaction.retention_period")
		} else {
			info.RetentionPeriod = retention
		}
	}

	return info
}

// ashViews returns the ASH views present in the cluster, in the order they should be
// exported. An empty result means the cluster has no ASH.
func (exporter *Exporter) ashViews(ctx context.Context) ([]string, error) {
	rows, err := exporter.Db.Query(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_schema = $1 AND table_name IN ($2, $3)",
		ashSchema, ashPersistedView, ashClusterView)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var present []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		present = append(present, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return orderASHViews(present), nil
}

// orderASHViews returns the subset of present view names that the exporter knows how to
// export, ordered by the preference expressed in ashTables.
func orderASHViews(present []string) []string {
	var ordered []string
	for _, table := range ashTables {
		if slices.Contains(present, table.Name) {
			ordered = append(ordered, table.Name)
		}
	}
	return ordered
}

// ashTablesFor returns the export table definitions for the given ASH view names,
// preserving the preference order of ashTables.
func ashTablesFor(views []string) []Table {
	var tables []Table
	for _, table := range ashTables {
		if slices.Contains(views, table.Name) {
			tables = append(tables, table)
		}
	}
	return tables
}

// boolClusterSetting reads a boolean cluster setting. The setting name is interpolated
// into the statement because SHOW CLUSTER SETTING does not accept placeholders; callers
// must pass a hard-coded name.
func (exporter *Exporter) boolClusterSetting(ctx context.Context, name string) (bool, error) {
	var value bool
	if err := exporter.Db.QueryRow(ctx, fmt.Sprintf("SHOW CLUSTER SETTING %s", name)).Scan(&value); err != nil {
		return false, fmt.Errorf("failed to get %s: %w", name, err)
	}
	return value, nil
}

// durationClusterSetting reads a duration cluster setting. As with boolClusterSetting,
// the name must be hard-coded.
func (exporter *Exporter) durationClusterSetting(ctx context.Context, name string) (time.Duration, error) {
	var value time.Duration
	if err := exporter.Db.QueryRow(ctx, fmt.Sprintf("SHOW CLUSTER SETTING %s", name)).Scan(&value); err != nil {
		return 0, fmt.Errorf("failed to get %s: %w", name, err)
	}
	return value, nil
}
