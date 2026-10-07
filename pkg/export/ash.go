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
//
// The two views behave very differently under a time-range filter:
//
//   - The persisted view reads a stored table, so a sample_time predicate filters it
//     normally and any window within the retention period is served correctly.
//   - The in-memory cluster view is populated by an RPC fan-out that caps each node's
//     contribution at obs.ash.response_limit samples, keeping the newest ones, before
//     the sample_time predicate is applied by the SQL layer above it. The cap is a hard
//     horizon rather than a limit within the requested window: on a busy cluster the
//     newest response_limit samples may span only seconds, so a window ending earlier
//     than that horizon exports zero rows with no error. See ashRangeWarning.
const ashSchema = "information_schema"

const (
	// ashClusterView is the in-memory, cluster-wide ASH view (v26.2+).
	ashClusterView = "crdb_cluster_active_session_history"
	// ashPersistedView is the view over persisted ASH samples (v26.3+).
	ashPersistedView = "crdb_persisted_active_session_history"
)

// ashInMemoryGrace is how far before "now" a requested time range may end before the
// in-memory cluster view is treated as unable to serve it. The real horizon depends on
// how many sessions are concurrently active on each node and cannot be computed here,
// so this is deliberately generous: it suppresses the warning for the common "export
// what just happened" case without hiding plainly historical ranges.
const ashInMemoryGrace = 5 * time.Minute

// ashTables lists the ASH relations the exporter knows about, in preference order:
// persisted history first (covers the full time range), then the in-memory cluster view
// (covers the most recent samples that have not been flushed yet). Both are filtered on
// sample_time and are Optional so that a cluster which exposes a view but denies access
// to it degrades to a warning rather than failing the export.
//
// ExactTimeRange is set on both: the aggregation-interval rounding applied to the SQL
// statistics tables would widen a request by up to two hours, which on a relation
// sampled once per second per active session is a large amount of unasked-for data.
var ashTables = []Table{
	{Database: ashSchema, Name: ashPersistedView, TimeColumn: "sample_time", ExactTimeRange: true, Optional: true, Scope: TenantScopeMain},
	{Database: ashSchema, Name: ashClusterView, TimeColumn: "sample_time", ExactTimeRange: true, Optional: true, Scope: TenantScopeMain},
}

// ASHInfo records Active Session History availability and configuration for the exported
// cluster. It is serialized into metadata.json so that consumers know whether ASH data is
// present and how to interpret it (for example, database time is approximated by the
// sample count multiplied by SampleInterval).
//
// Settings that could not be read are left nil and serialize as JSON null, so a consumer
// can tell "the cluster reported false" from "the exporter could not find out".
type ASHInfo struct {
	// Available reports whether the cluster exposes any ASH view.
	Available bool `json:"available"`
	// Enabled is the value of obs.ash.enabled. When false, ASH exports may be empty.
	// Nil when the setting could not be read.
	Enabled *bool `json:"enabled"`
	// EnrichmentEnabled is the value of obs.ash.enrichment.enabled. When false, the
	// per-execution columns (user, plan_gist, canary_stats, txn_id, session_id) are NULL.
	// Nil when the setting could not be read.
	EnrichmentEnabled *bool `json:"enrichment_enabled"`
	// SampleInterval is the value of obs.ash.sample_interval, the time each sample represents.
	SampleInterval time.Duration `json:"sample_interval,omitempty"`
	// RetentionPeriod is the value of obs.ash.compaction.retention_period, the age beyond
	// which persisted samples are pruned. Zero when the cluster has no persisted ASH.
	RetentionPeriod time.Duration `json:"retention_period,omitempty"`
	// ResponseLimit is the value of obs.ash.response_limit: the maximum number of samples
	// each node contributes to the in-memory cluster view, newest first, before the
	// time-range predicate is applied. Rows in the cluster view CSV may be truncated to
	// this many per node. Nil when the cluster has no in-memory view or it could not be read.
	ResponseLimit *int64 `json:"response_limit,omitempty"`
	// BufferSize is the value of obs.ash.buffer_size: the per-node in-memory sample ring
	// capacity, bounding how far back the cluster view can reach at all. Zero means the
	// cluster auto-sizes it from the Go soft memory limit. Nil when the cluster has no
	// in-memory view or it could not be read.
	BufferSize *int64 `json:"buffer_size,omitempty"`
	// Views lists the ASH views detected in the cluster catalog, in export order.
	Views []string `json:"views,omitempty"`
	// ExportedViews lists the subset of Views whose CSV was written successfully. A view
	// in Views but not here exists in the cluster but could not be read, and has no file
	// in the export.
	ExportedViews []string `json:"exported_views,omitempty"`
}

// detectASH probes the cluster for ASH views and reads the ASH cluster settings.
// It never fails the export: a cluster without ASH simply reports Available false, and
// settings that cannot be read are recorded as unknown (nil) rather than as their zero
// value, so metadata never claims sampling was off when the exporter simply could not
// tell. Views records what the catalog exposes; the caller fills in ExportedViews once
// the export loop has run.
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
		logrus.WithError(err).Warn("failed to read obs.ash.enabled; recording it as unknown in metadata")
	} else {
		info.Enabled = &enabled
		if !enabled {
			logrus.Warn("obs.ash.enabled is false; exported active session history data may be empty")
		}
	}

	if enriched, err := exporter.boolClusterSetting(ctx, "obs.ash.enrichment.enabled"); err != nil {
		logrus.WithError(err).Warn("failed to read obs.ash.enrichment.enabled; recording it as unknown in metadata")
	} else {
		info.EnrichmentEnabled = &enriched
	}

	if interval, err := exporter.durationClusterSetting(ctx, "obs.ash.sample_interval"); err != nil {
		logrus.WithError(err).Warn("failed to read obs.ash.sample_interval; recording it as unknown in metadata")
	} else {
		info.SampleInterval = interval
	}

	if slices.Contains(views, ashPersistedView) {
		if retention, err := exporter.durationClusterSetting(ctx, "obs.ash.compaction.retention_period"); err != nil {
			logrus.WithError(err).Warn("failed to read obs.ash.compaction.retention_period; recording it as unknown in metadata")
		} else {
			info.RetentionPeriod = retention
		}
	}

	// The bounds on the in-memory view are recorded so a consumer can tell a truncated
	// CSV from a cluster that genuinely had no activity in the window.
	if slices.Contains(views, ashClusterView) {
		if limit, err := exporter.intClusterSetting(ctx, "obs.ash.response_limit"); err != nil {
			logrus.WithError(err).Warn("failed to read obs.ash.response_limit; recording it as unknown in metadata")
		} else {
			info.ResponseLimit = &limit
		}

		if size, err := exporter.intClusterSetting(ctx, "obs.ash.buffer_size"); err != nil {
			logrus.WithError(err).Warn("failed to read obs.ash.buffer_size; recording it as unknown in metadata")
		} else {
			info.BufferSize = &size
		}
	}

	if msg := ashRangeWarning(views, info.ResponseLimit, exporter.Config.TimeRange, time.Now()); msg != "" {
		logrus.Warn(msg)
	}

	return info
}

// ashRangeWarning returns a warning for a requested time range the cluster's ASH views
// cannot serve, or "" when the range is satisfiable.
//
// A cluster with the persisted view filters normally and is never warned about. A
// cluster with only the in-memory view cannot serve a historical window at all: each
// node contributes its newest obs.ash.response_limit samples and the sample_time
// predicate is applied afterwards, so a window that ends before that horizon yields an
// empty CSV rather than an error. The horizon shrinks as concurrency rises — at one
// sample per second per active session, the default 10,000 per node is minutes on a
// quiet cluster and seconds on a busy one — so the exporter cannot compute it and warns
// on elapsed time instead.
func ashRangeWarning(
	views []string, responseLimit *int64, timeRange TimeRange, now time.Time,
) string {
	if slices.Contains(views, ashPersistedView) || !slices.Contains(views, ashClusterView) {
		return ""
	}
	if !timeRange.End.Before(now.Add(-ashInMemoryGrace)) {
		return ""
	}

	limit := "obs.ash.response_limit samples"
	if responseLimit != nil {
		limit = fmt.Sprintf("%d samples", *responseLimit)
	}
	return fmt.Sprintf(
		"this cluster has no persisted active session history, so ASH can only be read from the "+
			"in-memory cluster view; it returns the newest %s per node before the requested time range "+
			"is applied, so the range ending %s is likely to export zero ASH rows. Export closer to the "+
			"window of interest, or upgrade to v26.3 or later for persisted ASH.",
		limit, timeRange.End.Format(time.RFC3339))
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
func (exporter *Exporter) durationClusterSetting(
	ctx context.Context, name string,
) (time.Duration, error) {
	var value time.Duration
	if err := exporter.Db.QueryRow(ctx, fmt.Sprintf("SHOW CLUSTER SETTING %s", name)).Scan(&value); err != nil {
		return 0, fmt.Errorf("failed to get %s: %w", name, err)
	}
	return value, nil
}

// intClusterSetting reads an integer cluster setting. As with boolClusterSetting, the
// name must be hard-coded.
func (exporter *Exporter) intClusterSetting(ctx context.Context, name string) (int64, error) {
	var value int64
	if err := exporter.Db.QueryRow(ctx, fmt.Sprintf("SHOW CLUSTER SETTING %s", name)).Scan(&value); err != nil {
		return 0, fmt.Errorf("failed to get %s: %w", name, err)
	}
	return value, nil
}
