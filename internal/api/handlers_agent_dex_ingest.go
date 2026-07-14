// Agent DEX ingest enrichment — historical indices, enriched NATS
// payload, compliance eval, online/offline system events.
// Best-effort: never fails the heartbeat path.

package api

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NCLGISA/ScanRay-Sonar/internal/agentevents"
	"github.com/NCLGISA/ScanRay-Sonar/internal/compliance"
)

// dexSnapshot is the subset of the probe Snapshot used for DEX history
// and compliance. Extra fields are ignored by encoding/json.
type dexSnapshot struct {
	Host struct {
		Hostname      string `json:"hostname"`
		OS            string `json:"os"`
		UptimeSeconds uint64 `json:"uptimeSeconds"`
		BootTime      string `json:"bootTime"`
	} `json:"host"`
	CPU struct {
		UsagePct float64 `json:"usagePct"`
	} `json:"cpu"`
	Memory struct {
		TotalBytes uint64 `json:"totalBytes"`
		UsedBytes  uint64 `json:"usedBytes"`
	} `json:"memory"`
	Disks           []parsedDisk `json:"disks"`
	PendingReboot   bool         `json:"pendingReboot"`
	TopByCPU        []dexProcess `json:"topByCpu"`
	ActiveProcesses []dexProcess `json:"activeProcesses"`
	Health          *dexHealth   `json:"health"`
	InstalledApps   []dexApp     `json:"installedApps"`
	MissingPatches  []dexPatch   `json:"missingPatches"`
	Win11Readiness  *struct {
		Eligible   *bool `json:"eligible"`
		SecureBoot *bool `json:"secureBoot"`
	} `json:"win11Readiness"`
	Hardware *struct {
		GPUs    []dexGPU  `json:"gpus"`
		Storage []dexDisk `json:"storage"`
	} `json:"hardware"`
	InstalledExtensions []dexExtension  `json:"installedExtensions"`
	Latency             []parsedLatency `json:"latency"`
}

type dexProcess struct {
	PID      int32   `json:"pid"`
	Name     string  `json:"name"`
	User     string  `json:"user"`
	CPUPct   float64 `json:"cpuPct"`
	RSSBytes uint64  `json:"rssBytes"`
}

type dexHealth struct {
	BatteryHealthPct  *float64 `json:"batteryHealthPct"`
	BatteryCycleCount *int     `json:"batteryCycleCount"`
	BatteryWearPct    *float64 `json:"batteryWearPct"`
	BSODCount24h      *int     `json:"bsodCount24h"`
	AppCrashCount24h  *int     `json:"appCrashCount24h"`
	MissingPatchCount *int     `json:"missingPatchCount"`
	WiFiRSSIdBm       *int     `json:"wifiRssiDbm"`
	LogonAvgMs        *float64 `json:"logonAvgMs"`
	BootDurationMs    *int64   `json:"bootDurationMs"`
	EDRProducts       []string `json:"edrProducts"`
}

type dexApp struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Publisher string `json:"publisher"`
}

type dexPatch struct {
	Title    string   `json:"title"`
	KB       string   `json:"kb"`
	Severity string   `json:"severity"`
	SizeMB   *float64 `json:"sizeMb"`
}

type dexGPU struct {
	Product string `json:"product"`
	Vendor  string `json:"vendor"`
	Driver  string `json:"driver"`
	VRAMMB  *int   `json:"vramMb"`
}

type dexDisk struct {
	TempC   *float64 `json:"tempC"`
	WearPct *float64 `json:"wearPct"`
}

type dexExtension struct {
	Browser string `json:"browser"`
	Name    string `json:"name"`
	Version string `json:"version"`
	ID      string `json:"id"`
}

type dexEnrichResult struct {
	Score             float64
	DiskUsedRatio     float64
	BSOD24h           int
	AppCrash24h       int
	MissingPatchCount int
	PendingReboot     float64 // 1 or 0 for alarm DSL
	BatteryHealthPct  float64
	BatteryWearPct    *float64
	BootDurationMs    *int64
	WiFiRSSIdBm       int
	UptimeSec         uint64
	GPUName           string
	GroupID           string
	Hostname          string
}

// enrichAfterMetricsIngest runs post-commit DEX writes. Failures are logged only.
func (s *Server) enrichAfterMetricsIngest(ctx context.Context, agentID, siteID uuid.UUID, snapshot json.RawMessage, sentAt time.Time, hostname, crit string, prevLastSeen *time.Time, groupID *uuid.UUID) {
	var ds dexSnapshot
	if err := json.Unmarshal(snapshot, &ds); err != nil {
		s.log.Debug("dex enrich: snapshot decode failed", "err", err, "agent_id", agentID)
		return
	}

	// Online transition: previously offline (>5m) or never seen.
	aid := agentID
	offlineThresh := 5 * time.Minute
	wasOffline := prevLastSeen == nil || time.Since(*prevLastSeen) > offlineThresh
	if wasOffline {
		_ = agentevents.Emit(ctx, s.pool, siteID, &aid, agentevents.KindAgentOnline, "info",
			"Agent online", hostname+" reported metrics", map[string]any{"hostname": hostname})
	}

	res := s.writeDEXHistory(ctx, agentID, siteID, ds, sentAt)
	if groupID != nil {
		res.GroupID = groupID.String()
	}
	res.Hostname = hostname

	// Lift hot inventory columns.
	_, _ = s.pool.Exec(ctx, `
		UPDATE agents SET
		  battery_wear_pct = COALESCE($2, battery_wear_pct),
		  boot_duration_ms = COALESCE($3, boot_duration_ms),
		  gpu_name = COALESCE(NULLIF($4, ''), gpu_name)
		WHERE id = $1`,
		agentID, res.BatteryWearPct, res.BootDurationMs, res.GPUName)

	// Enriched NATS payload for alarm engine.
	memRatio := 0.0
	if ds.Memory.TotalBytes > 0 {
		memRatio = float64(ds.Memory.UsedBytes) / float64(ds.Memory.TotalBytes)
	}
	if s.nats != nil && s.nats.IsConnected() {
		payload := map[string]any{
			"agentId":           agentID.String(),
			"siteId":            siteID.String(),
			"cpuPct":            ds.CPU.UsagePct,
			"memUsedRatio":      memRatio,
			"diskUsedRatio":     res.DiskUsedRatio,
			"score":             res.Score,
			"bsod24h":           res.BSOD24h,
			"appCrash24h":       res.AppCrash24h,
			"missingPatchCount": res.MissingPatchCount,
			"pendingReboot":     res.PendingReboot,
			"batteryHealthPct":  res.BatteryHealthPct,
			"wifiRssi":          res.WiFiRSSIdBm,
			"uptimeSec":         res.UptimeSec,
			"criticality":       crit,
			"vendor":            hostname,
			"hostname":          hostname,
		}
		if res.GroupID != "" {
			payload["groupId"] = res.GroupID
		}
		if b, err := json.Marshal(payload); err == nil {
			if err := s.nats.Publish("metrics.agent", b); err != nil {
				s.log.Debug("metrics.agent publish failed", "err", err, "agent_id", agentID)
			}
		}
	}

	// Compliance evaluation.
	patchCount := res.MissingPatchCount
	if len(ds.MissingPatches) > patchCount {
		patchCount = len(ds.MissingPatches)
	}
	sig := compliance.SnapshotSignals{
		PendingReboot:     ds.PendingReboot,
		MissingPatchCount: patchCount,
		OS:                ds.Host.OS,
	}
	for _, p := range ds.MissingPatches {
		sig.Patches = append(sig.Patches, compliance.PatchRef{Title: p.Title, KB: p.KB, Severity: p.Severity})
	}
	for _, a := range ds.InstalledApps {
		sig.Apps = append(sig.Apps, compliance.AppRef{Name: a.Name, Version: a.Version})
	}
	if ds.Win11Readiness != nil {
		sig.Win11Eligible = ds.Win11Readiness.Eligible
		sig.SecureBoot = ds.Win11Readiness.SecureBoot
	}
	if ds.Health != nil {
		sig.EDRProducts = ds.Health.EDRProducts
	}
	if prevLastSeen != nil {
		sig.LastSeenAge = time.Since(*prevLastSeen)
	}
	if err := compliance.EvaluateAndPersist(ctx, s.pool, agentID, siteID, sig); err != nil {
		s.log.Debug("compliance eval failed", "err", err, "agent_id", agentID)
	}
}

func (s *Server) writeDEXHistory(ctx context.Context, agentID, siteID uuid.UUID, ds dexSnapshot, sentAt time.Time) dexEnrichResult {
	res := dexEnrichResult{UptimeSec: ds.Host.UptimeSeconds}
	if ds.PendingReboot {
		res.PendingReboot = 1
	}

	rootUsed, rootTotal := pickRootDisk(ds.Disks)
	var diskPct *float64
	if rootTotal > 0 {
		v := float64(rootUsed) / float64(rootTotal)
		res.DiskUsedRatio = v
		pct := v * 100
		diskPct = &pct
	}
	var cpuPct, memPct *float64
	c := ds.CPU.UsagePct
	cpuPct = &c
	if ds.Memory.TotalBytes > 0 {
		m := float64(ds.Memory.UsedBytes) / float64(ds.Memory.TotalBytes) * 100
		memPct = &m
	}
	var bat, latAvg, loss *float64
	var bsod, crash *int
	if ds.Health != nil {
		bat = ds.Health.BatteryHealthPct
		bsod = ds.Health.BSODCount24h
		crash = ds.Health.AppCrashCount24h
		if ds.Health.MissingPatchCount != nil {
			res.MissingPatchCount = *ds.Health.MissingPatchCount
		}
		if ds.Health.WiFiRSSIdBm != nil {
			res.WiFiRSSIdBm = *ds.Health.WiFiRSSIdBm
		}
		if ds.Health.BatteryWearPct != nil {
			res.BatteryWearPct = ds.Health.BatteryWearPct
		} else if bat != nil {
			w := math.Max(0, 100-*bat)
			res.BatteryWearPct = &w
		}
		if ds.Health.BootDurationMs != nil {
			res.BootDurationMs = ds.Health.BootDurationMs
		}
		if bat != nil {
			res.BatteryHealthPct = *bat
		}
		if bsod != nil {
			res.BSOD24h = *bsod
		}
		if crash != nil {
			res.AppCrash24h = *crash
		}
	}
	if len(ds.MissingPatches) > res.MissingPatchCount {
		res.MissingPatchCount = len(ds.MissingPatches)
	}
	for _, lr := range ds.Latency {
		if lr.Target == "8.8.8.8" || latAvg == nil {
			v := lr.AvgMs
			latAvg = &v
			l := lr.LossPct
			loss = &l
		}
	}
	res.Score = ComputeScore(ScoreInputs{
		CPUPct: cpuPct, MemPct: memPct, DiskPct: diskPct,
		BatteryHealthPct: bat, BSODCount24h: bsod, AppCrashCount24h: crash,
		LatencyAvgMs: latAvg, LossPct: loss,
	})

	if ds.Hardware != nil && len(ds.Hardware.GPUs) > 0 {
		g := ds.Hardware.GPUs[0]
		res.GPUName = g.Product
		if res.GPUName == "" {
			res.GPUName = g.Vendor
		}
	}

	uxInputs, _ := json.Marshal(map[string]any{
		"cpuPct": cpuPct, "memPct": memPct, "diskPct": diskPct,
		"battery": bat, "bsod": bsod, "crash": crash,
	})
	_, err := s.pool.Exec(ctx, `
		INSERT INTO agent_score_samples (time, agent_id, site_id, score, ux_inputs)
		VALUES ($1, $2, $3, $4, $5::jsonb)
		ON CONFLICT (agent_id, time) DO UPDATE SET score = EXCLUDED.score, ux_inputs = EXCLUDED.ux_inputs`,
		sentAt, agentID, siteID, res.Score, string(uxInputs))
	if err != nil {
		s.log.Debug("dex score sample failed", "err", err)
	}

	var patchCount any
	if res.MissingPatchCount > 0 || (ds.Health != nil && ds.Health.MissingPatchCount != nil) {
		patchCount = res.MissingPatchCount
	}
	var wifiRssi any
	var logonMs any
	if ds.Health != nil {
		if ds.Health.WiFiRSSIdBm != nil {
			wifiRssi = *ds.Health.WiFiRSSIdBm
		}
		if ds.Health.LogonAvgMs != nil {
			logonMs = *ds.Health.LogonAvgMs
		}
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO agent_health_samples
		  (time, agent_id, site_id, battery_pct, battery_wear_pct, bsod_24h, crash_24h,
		   patch_count, wifi_rssi, logon_ms, boot_duration_ms, pending_reboot)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (agent_id, time) DO UPDATE SET
		  battery_pct = EXCLUDED.battery_pct,
		  battery_wear_pct = EXCLUDED.battery_wear_pct,
		  bsod_24h = EXCLUDED.bsod_24h,
		  crash_24h = EXCLUDED.crash_24h,
		  patch_count = EXCLUDED.patch_count,
		  wifi_rssi = EXCLUDED.wifi_rssi,
		  logon_ms = EXCLUDED.logon_ms,
		  boot_duration_ms = EXCLUDED.boot_duration_ms,
		  pending_reboot = EXCLUDED.pending_reboot`,
		sentAt, agentID, siteID,
		nullableFloatPtr(bat), res.BatteryWearPct,
		bsod, crash, patchCount, wifiRssi, logonMs,
		res.BootDurationMs, ds.PendingReboot)
	if err != nil {
		s.log.Debug("dex health sample failed", "err", err)
	}

	procs := ds.ActiveProcesses
	if len(procs) == 0 {
		procs = ds.TopByCPU
	}
	sort.Slice(procs, func(i, j int) bool { return procs[i].CPUPct > procs[j].CPUPct })
	if len(procs) > 25 {
		procs = procs[:25]
	}
	batch := &pgx.Batch{}
	for _, p := range procs {
		if p.Name == "" || p.PID == 0 {
			continue
		}
		batch.Queue(`
			INSERT INTO agent_process_samples
			  (time, agent_id, site_id, pid, name, user_name, cpu_pct, rss_bytes)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (agent_id, time, pid) DO UPDATE SET
			  name = EXCLUDED.name, cpu_pct = EXCLUDED.cpu_pct, rss_bytes = EXCLUDED.rss_bytes`,
			sentAt, agentID, siteID, p.PID, p.Name, p.User, p.CPUPct, int64(p.RSSBytes))
	}
	if batch.Len() > 0 {
		br := s.pool.SendBatch(ctx, batch)
		_ = br.Close()
	}

	day := sentAt.UTC().Truncate(24 * time.Hour)
	appBatch := &pgx.Batch{}
	seenApp := map[string]struct{}{}
	for _, a := range ds.InstalledApps {
		if a.Name == "" {
			continue
		}
		key := a.Name + "|" + a.Version
		if _, ok := seenApp[key]; ok {
			continue
		}
		seenApp[key] = struct{}{}
		appBatch.Queue(`
			INSERT INTO agent_app_inventory_daily (day, agent_id, site_id, name, version, publisher)
			VALUES ($1::date,$2,$3,$4,$5,$6)
			ON CONFLICT (agent_id, day, name, version) DO UPDATE SET publisher = EXCLUDED.publisher`,
			day, agentID, siteID, a.Name, a.Version, a.Publisher)
	}
	for _, e := range ds.InstalledExtensions {
		name := e.Browser + " ext: " + e.Name
		if e.Name == "" {
			continue
		}
		appBatch.Queue(`
			INSERT INTO agent_app_inventory_daily (day, agent_id, site_id, name, version, publisher)
			VALUES ($1::date,$2,$3,$4,$5,$6)
			ON CONFLICT (agent_id, day, name, version) DO NOTHING`,
			day, agentID, siteID, name, e.Version, e.Browser)
	}
	if appBatch.Len() > 0 {
		br := s.pool.SendBatch(ctx, appBatch)
		_ = br.Close()
	}

	patchBatch := &pgx.Batch{}
	for _, p := range ds.MissingPatches {
		if p.Title == "" {
			continue
		}
		patchBatch.Queue(`
			INSERT INTO agent_patch_samples (time, agent_id, site_id, title, kb, severity, size_mb)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (agent_id, time, title, kb) DO UPDATE SET severity = EXCLUDED.severity`,
			sentAt, agentID, siteID, p.Title, p.KB, p.Severity, p.SizeMB)
	}
	if patchBatch.Len() > 0 {
		br := s.pool.SendBatch(ctx, patchBatch)
		_ = br.Close()
	}

	return res
}

func nullableFloatPtr(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
