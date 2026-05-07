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

	if err := persistVendorSamples(ctx, tx, id, snap); err != nil {
		return fmt.Errorf("vendor samples: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// persistVendorSamples writes a row to appliance_vendor_samples for
// each metric the snapshot's VendorHealth carries. The keying scheme
// matches the migration's example list. Missing fields silently skip.
func persistVendorSamples(ctx context.Context, tx pgx.Tx, id uuid.UUID, snap snmp.Snapshot) error {
	if snap.Vendor == nil {
		return nil
	}
	batch := &pgx.Batch{}
	q := `INSERT INTO appliance_vendor_samples (appliance_id, time, metric_key, value_double, value_text)
	      VALUES ($1, $2, $3, $4, NULLIF($5,''))
	      ON CONFLICT (appliance_id, metric_key, time) DO UPDATE SET
	        value_double = EXCLUDED.value_double,
	        value_text   = EXCLUDED.value_text`
	add := func(key string, num *float64, txt string) {
		var n any
		if num != nil {
			n = *num
		}
		batch.Queue(q, id, snap.CapturedAt, key, n, txt)
	}
	addInt := func(key string, num *int32) {
		if num == nil {
			return
		}
		f := float64(*num)
		add(key, &f, "")
	}
	addInt64 := func(key string, num *int64) {
		if num == nil {
			return
		}
		f := float64(*num)
		add(key, &f, "")
	}
	addBool := func(key string, b *bool) {
		if b == nil {
			return
		}
		var f float64
		if *b {
			f = 1
		}
		add(key, &f, "")
	}

	// UPS
	if u := snap.Vendor.UPS; u != nil {
		addInt("ups.battery.charge_pct", u.EstChargePct)
		addInt("ups.battery.runtime_min", u.EstRuntimeMin)
		addInt("ups.output.load_pct", u.OutputLoadPct)
		add("ups.battery.temp_c", u.BatteryTempC, "")
		add("ups.input.voltage_v", u.InputVoltage, "")
		add("ups.output.voltage_v", u.OutputVoltage, "")
		addInt("ups.battery.status", u.BatteryStatus)
		addInt("ups.output.status", u.OutputStatus)
		addInt("ups.input.line_fail_cause", u.InputLineFailCause)
		addBool("ups.battery.replace_needed", u.BatteryReplaceNeeded)
	}
	// Synology
	if s := snap.Vendor.Synology; s != nil {
		addInt("synology.system.status", s.SystemStatus)
		addInt("synology.power.status", s.PowerStatus)
		add("synology.system.temp_c", s.TempC, "")
		for _, d := range s.Disks {
			pre := "synology.disk." + itoa32(d.Index)
			fStatus := float64(d.Status)
			add(pre+".status", &fStatus, "")
			if d.TempC > 0 {
				t := d.TempC
				add(pre+".temp_c", &t, "")
			}
		}
		for _, v := range s.Volumes {
			pre := "synology.volume." + itoa32(v.Index)
			fStatus := float64(v.Status)
			add(pre+".status", &fStatus, "")
		}
	}
	// Palo Alto
	if p := snap.Vendor.PaloAlto; p != nil {
		addInt64("paloalto.session.active", p.SessionActive)
		addInt64("paloalto.session.max", p.SessionMax)
		addInt64("paloalto.session.active_tcp", p.SessionActiveTcp)
		addInt64("paloalto.session.active_udp", p.SessionActiveUdp)
		add("paloalto.session.util_pct", p.SessionUtilPct, "")
	}
	// Alletra
	if a := snap.Vendor.Alletra; a != nil {
		addInt64("alletra.global.vol_count", a.GlobalVolCount)
		addInt64("alletra.global.snap_count", a.GlobalSnapCount)
		for _, v := range a.Volumes {
			pre := "alletra.volume." + itoa32(v.Index)
			used := v.UsedPct
			add(pre+".used_pct", &used, v.Name)
			usedB := float64(v.UsageBytes)
			add(pre+".used_bytes", &usedB, "")
			sizeB := float64(v.SizeBytes)
			add(pre+".size_bytes", &sizeB, "")
			online := 0.0
			if v.Online {
				online = 1
			}
			add(pre+".online", &online, "")
		}
	}
	// Cisco extras
	if c := snap.Vendor.Cisco; c != nil {
		add("cisco.cpu.5sec_pct", c.CPU5sec, "")
		add("cisco.cpu.1min_pct", c.CPU1min, "")
		add("cisco.cpu.5min_pct", c.CPU5min, "")
	}

	if batch.Len() == 0 {
		return nil
	}
	br := tx.SendBatch(ctx, batch)
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return err
		}
	}
	return br.Close()
}

func itoa32(n int32) string {
	// Cheap stdlib-only int-to-string, avoiding strconv import bloat
	// in this otherwise-stdlib-light file. Negative n is impossible
	// for our index domain.
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
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
