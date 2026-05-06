package poller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp"
)

// PersistSnapshot updates appliances.last_snapshot + samples (same as central poller).
func PersistSnapshot(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, snap snmp.Snapshot) error {
	jsonBlob, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	var (
		upCount        int
		physTotalCount int
		physUpCount    int
		uplinkCount    int
	)
	for _, ifc := range snap.Interfaces {
		if ifc.OperUp {
			upCount++
		}
		if ifc.Kind == "physical" {
			physTotalCount++
			if ifc.OperUp {
				physUpCount++
			}
		}
		if ifc.IsUplink {
			uplinkCount++
		}
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		UPDATE appliances
		   SET last_snapshot     = $2::jsonb,
		       last_snapshot_at  = $3,
		       sys_descr         = NULLIF($4,''),
		       sys_name          = NULLIF($5,''),
		       uptime_seconds    = $6,
		       cpu_pct           = $7,
		       mem_used_bytes    = $8,
		       mem_total_bytes   = $9,
		       if_up_count       = $10,
		       if_total_count    = $11,
		       phys_total_count  = $12,
		       phys_up_count     = $13,
		       uplink_count      = $14,
		       last_polled_at    = $3,
		       last_error        = NULL
		 WHERE id = $1
	`,
		id,
		string(jsonBlob),
		snap.CapturedAt,
		snap.System.Description,
		snap.System.Name,
		snap.System.UptimeSecs,
		floatSQL(snap.Chassis.CPUPct),
		uint64SQL(snap.Chassis.MemUsedBytes),
		uint64SQL(snap.Chassis.MemTotalBytes),
		upCount,
		len(snap.Interfaces),
		physTotalCount,
		physUpCount,
		uplinkCount,
	)
	if err != nil {
		return fmt.Errorf("update appliances: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO appliance_metric_samples
		  (appliance_id, time, cpu_pct, mem_used_bytes, mem_total_bytes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (appliance_id, time) DO UPDATE SET
		  cpu_pct         = EXCLUDED.cpu_pct,
		  mem_used_bytes  = EXCLUDED.mem_used_bytes,
		  mem_total_bytes = EXCLUDED.mem_total_bytes
	`,
		id, snap.CapturedAt,
		floatSQL(snap.Chassis.CPUPct),
		uint64SQL(snap.Chassis.MemUsedBytes),
		uint64SQL(snap.Chassis.MemTotalBytes),
	)
	if err != nil {
		return fmt.Errorf("insert chassis sample: %w", err)
	}

	batch := &pgx.Batch{}
	for _, ifc := range snap.Interfaces {
		if ifc.InBps == nil && ifc.OutBps == nil {
			continue
		}
		batch.Queue(`
			INSERT INTO appliance_iface_samples
			  (appliance_id, if_index, time, in_bps, out_bps,
			   in_errors, out_errors, in_discards, out_discards)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (appliance_id, if_index, time) DO NOTHING
		`,
			id, ifc.Index, snap.CapturedAt,
			uint64SQL(ifc.InBps), uint64SQL(ifc.OutBps),
			int64(ifc.InErrors), int64(ifc.OutErrors),
			int64(ifc.InDiscards), int64(ifc.OutDiscards),
		)
	}
	if batch.Len() > 0 {
		br := tx.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err := br.Exec(); err != nil {
				_ = br.Close()
				return fmt.Errorf("iface sample %d: %w", i, err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("close iface batch: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// RecordPollError writes poll failure to appliances.last_error.
func RecordPollError(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger, id uuid.UUID, pollErr error) {
	if pollErr == nil {
		return
	}
	log.Warn("appliance poll failed", "appliance_id", id, "err", pollErr)
	_, err := pool.Exec(ctx, `
		UPDATE appliances
		   SET last_error     = $2,
		       last_polled_at = NOW()
		 WHERE id = $1
	`, id, truncateErr(pollErr.Error(), 500))
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Warn("recordPollError: db update failed", "appliance_id", id, "err", err)
	}
}

func floatSQL(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func uint64SQL(p *uint64) any {
	if p == nil {
		return nil
	}
	return int64(*p)
}

func truncateErr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
