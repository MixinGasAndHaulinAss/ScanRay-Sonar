// Package retention loads platform retention settings and applies them
// to TimescaleDB policies plus alarm/audit prune jobs.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Settings is the singleton platform retention configuration.
type Settings struct {
	HotWindowDays       int       `json:"hotWindowDays"`
	CompressAfterDays   int       `json:"compressAfterDays"`
	RollupRetentionDays int       `json:"rollupRetentionDays"`
	FlowHotWindowDays   int       `json:"flowHotWindowDays"`
	VendorSamplesDays   int       `json:"vendorSamplesDays"`
	AlarmsClearedDays   int       `json:"alarmsClearedDays"`
	AuditLogDays        int       `json:"auditLogDays"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// Defaults matches migration 0026 / historical migration policies.
func Defaults() Settings {
	return Settings{
		HotWindowDays:       30,
		CompressAfterDays:   1,
		RollupRetentionDays: 365,
		FlowHotWindowDays:   14,
		VendorSamplesDays:   180,
		AlarmsClearedDays:   365,
		AuditLogDays:        365,
	}
}

// Clamp enforces API/UI bounds from the plan.
func (s *Settings) Clamp() {
	d := Defaults()
	clampInt := func(v, lo, hi, fallback int) int {
		if v < lo || v > hi {
			return fallback
		}
		return v
	}
	s.HotWindowDays = clampInt(s.HotWindowDays, 7, 90, d.HotWindowDays)
	s.CompressAfterDays = clampInt(s.CompressAfterDays, 0, 7, d.CompressAfterDays)
	s.RollupRetentionDays = clampInt(s.RollupRetentionDays, 30, 1825, d.RollupRetentionDays)
	s.FlowHotWindowDays = clampInt(s.FlowHotWindowDays, 3, 90, d.FlowHotWindowDays)
	s.VendorSamplesDays = clampInt(s.VendorSamplesDays, 30, 730, d.VendorSamplesDays)
	s.AlarmsClearedDays = clampInt(s.AlarmsClearedDays, 30, 1825, d.AlarmsClearedDays)
	s.AuditLogDays = clampInt(s.AuditLogDays, 30, 1825, d.AuditLogDays)
}

// HotWindow returns the raw-sample lookback window.
func (s Settings) HotWindow() time.Duration {
	return time.Duration(s.HotWindowDays) * 24 * time.Hour
}

// MaxChartRange is the farthest a metrics API may look back (rollup horizon).
func (s Settings) MaxChartRange() time.Duration {
	return time.Duration(s.RollupRetentionDays) * 24 * time.Hour
}

// UseRollup reports whether a requested chart range should hit continuous aggregates.
func (s Settings) UseRollup(rangeDur time.Duration) bool {
	return rangeDur > s.HotWindow()
}

// Load reads the singleton row; missing table/row returns Defaults.
func Load(ctx context.Context, pool *pgxpool.Pool) (Settings, error) {
	s := Defaults()
	err := pool.QueryRow(ctx, `
		SELECT hot_window_days, compress_after_days, rollup_retention_days,
		       flow_hot_window_days, vendor_samples_days,
		       alarms_cleared_days, audit_log_days, updated_at
		  FROM platform_retention_settings WHERE id = 1`).
		Scan(&s.HotWindowDays, &s.CompressAfterDays, &s.RollupRetentionDays,
			&s.FlowHotWindowDays, &s.VendorSamplesDays,
			&s.AlarmsClearedDays, &s.AuditLogDays, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Defaults(), nil
		}
		// Table may not exist yet during early migrate race.
		return Defaults(), fmt.Errorf("retention load: %w", err)
	}
	s.Clamp()
	return s, nil
}

// Save upserts settings and returns the stored row.
func Save(ctx context.Context, pool *pgxpool.Pool, s Settings) (Settings, error) {
	s.Clamp()
	err := pool.QueryRow(ctx, `
		INSERT INTO platform_retention_settings (
		  id, hot_window_days, compress_after_days, rollup_retention_days,
		  flow_hot_window_days, vendor_samples_days,
		  alarms_cleared_days, audit_log_days, updated_at)
		VALUES (1, $1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (id) DO UPDATE SET
		  hot_window_days = EXCLUDED.hot_window_days,
		  compress_after_days = EXCLUDED.compress_after_days,
		  rollup_retention_days = EXCLUDED.rollup_retention_days,
		  flow_hot_window_days = EXCLUDED.flow_hot_window_days,
		  vendor_samples_days = EXCLUDED.vendor_samples_days,
		  alarms_cleared_days = EXCLUDED.alarms_cleared_days,
		  audit_log_days = EXCLUDED.audit_log_days,
		  updated_at = NOW()
		RETURNING hot_window_days, compress_after_days, rollup_retention_days,
		          flow_hot_window_days, vendor_samples_days,
		          alarms_cleared_days, audit_log_days, updated_at`,
		s.HotWindowDays, s.CompressAfterDays, s.RollupRetentionDays,
		s.FlowHotWindowDays, s.VendorSamplesDays,
		s.AlarmsClearedDays, s.AuditLogDays,
	).Scan(&s.HotWindowDays, &s.CompressAfterDays, &s.RollupRetentionDays,
		&s.FlowHotWindowDays, &s.VendorSamplesDays,
		&s.AlarmsClearedDays, &s.AuditLogDays, &s.UpdatedAt)
	if err != nil {
		return s, fmt.Errorf("retention save: %w", err)
	}
	return s, nil
}

// Apply updates Timescale retention/compression policies to match settings.
// Safe on plain Postgres (no-ops when timescaledb is absent).
func Apply(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, s Settings) error {
	s.Clamp()
	var hasTS bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb')`).
		Scan(&hasTS); err != nil {
		return err
	}
	if !hasTS {
		log.Info("retention apply: timescaledb absent; policies skipped")
		return Prune(ctx, pool, log, s)
	}

	hot := fmt.Sprintf("%d days", s.HotWindowDays)
	flow := fmt.Sprintf("%d days", s.FlowHotWindowDays)
	vendor := fmt.Sprintf("%d days", s.VendorSamplesDays)
	rollup := fmt.Sprintf("%d days", s.RollupRetentionDays)
	compress := fmt.Sprintf("%d days", s.CompressAfterDays)
	if s.CompressAfterDays == 0 {
		compress = "1 hour"
	}

	core := []string{
		"agent_metric_samples",
		"agent_network_samples",
		"agent_latency_samples",
		"appliance_metric_samples",
		"appliance_iface_samples",
	}
	for _, ht := range core {
		if err := setRetention(ctx, pool, ht, hot); err != nil {
			log.Warn("retention policy update failed", "table", ht, "err", err)
		}
		if err := setCompression(ctx, pool, ht, compress); err != nil {
			log.Warn("compression policy update failed", "table", ht, "err", err)
		}
	}
	if err := setRetention(ctx, pool, "flow_summaries", flow); err != nil {
		log.Warn("retention policy update failed", "table", "flow_summaries", "err", err)
	}
	if err := setCompression(ctx, pool, "flow_summaries", compress); err != nil {
		log.Warn("compression policy update failed", "table", "flow_summaries", "err", err)
	}
	if err := setRetention(ctx, pool, "appliance_vendor_samples", vendor); err != nil {
		log.Warn("retention policy update failed", "table", "appliance_vendor_samples", "err", err)
	}
	if err := setCompression(ctx, pool, "appliance_vendor_samples", compress); err != nil {
		log.Warn("compression policy update failed", "table", "appliance_vendor_samples", "err", err)
	}

	caggs := []string{
		"agent_metric_samples_hourly",
		"agent_network_samples_hourly",
		"agent_latency_samples_hourly",
		"appliance_metric_samples_hourly",
		"appliance_iface_samples_hourly",
	}
	for _, ht := range caggs {
		if err := setRetention(ctx, pool, ht, rollup); err != nil {
			log.Warn("cagg retention update failed", "table", ht, "err", err)
		}
	}

	log.Info("retention policies applied",
		"hot_days", s.HotWindowDays,
		"rollup_days", s.RollupRetentionDays,
		"compress_after_days", s.CompressAfterDays)

	return Prune(ctx, pool, log, s)
}

func setRetention(ctx context.Context, pool *pgxpool.Pool, relation, interval string) error {
	q := fmt.Sprintf(`
		DO $do$
		BEGIN
		  PERFORM remove_retention_policy('%s'::regclass, if_exists => TRUE);
		  PERFORM add_retention_policy('%s'::regclass, INTERVAL '%s', if_not_exists => TRUE);
		EXCEPTION WHEN OTHERS THEN
		  RAISE NOTICE 'setRetention skipped: %%', SQLERRM;
		END
		$do$`, relation, relation, interval)
	_, err := pool.Exec(ctx, q)
	return err
}

func setCompression(ctx context.Context, pool *pgxpool.Pool, relation, interval string) error {
	q := fmt.Sprintf(`
		DO $do$
		BEGIN
		  PERFORM remove_compression_policy('%s'::regclass, if_exists => TRUE);
		  PERFORM add_compression_policy('%s'::regclass, INTERVAL '%s', if_not_exists => TRUE);
		EXCEPTION WHEN OTHERS THEN
		  RAISE NOTICE 'setCompression skipped: %%', SQLERRM;
		END
		$do$`, relation, relation, interval)
	_, err := pool.Exec(ctx, q)
	return err
}

// Prune deletes aged cleared alarms and audit log rows in batches.
func Prune(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, s Settings) error {
	s.Clamp()
	alarmsInterval := fmt.Sprintf("%d days", s.AlarmsClearedDays)
	auditInterval := fmt.Sprintf("%d days", s.AuditLogDays)

	tag, err := pool.Exec(ctx, `
		DELETE FROM alarms
		 WHERE cleared_at IS NOT NULL
		   AND cleared_at < NOW() - $1::interval
		   AND id IN (
		     SELECT id FROM alarms
		      WHERE cleared_at IS NOT NULL
		        AND cleared_at < NOW() - $1::interval
		      ORDER BY cleared_at ASC
		      LIMIT 5000
		   )`, alarmsInterval)
	if err != nil {
		log.Warn("alarms prune failed", "err", err)
	} else if tag.RowsAffected() > 0 {
		log.Info("alarms pruned", "rows", tag.RowsAffected(), "older_than", alarmsInterval)
	}

	tag, err = pool.Exec(ctx, `
		DELETE FROM audit_log
		 WHERE occurred_at < NOW() - $1::interval
		   AND id IN (
		     SELECT id FROM audit_log
		      WHERE occurred_at < NOW() - $1::interval
		      ORDER BY occurred_at ASC
		      LIMIT 5000
		   )`, auditInterval)
	if err != nil {
		log.Warn("audit_log prune failed", "err", err)
	} else if tag.RowsAffected() > 0 {
		log.Info("audit_log pruned", "rows", tag.RowsAffected(), "older_than", auditInterval)
	}
	return nil
}

// Runner periodically reconciles policies and prunes unbounded tables.
func Runner(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	const every = 1 * time.Hour
	applyOnce := func() {
		s, err := Load(ctx, pool)
		if err != nil {
			log.Warn("retention runner load failed", "err", err)
			s = Defaults()
		}
		if err := Apply(ctx, pool, log, s); err != nil {
			log.Warn("retention runner apply failed", "err", err)
		}
	}
	applyOnce()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			applyOnce()
		}
	}
}
