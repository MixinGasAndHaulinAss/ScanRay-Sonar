// Package api — overview aggregation endpoints.
//
// handlers_overview.go owns the seven /agents/overview/* endpoints
// that power the Agents-page dropdown switcher (Devices, Devices -
// Averages, Devices - Performance, Network - Latency, Network -
// Performance, Applications - Performance, User Experience).
//
// Design notes:
//
//   - Each endpoint reads the smallest set of columns + a few aggregate
//     rollups that fit in one round-trip. The dashboards refresh on
//     a 30–60 s poll so we deliberately favour query simplicity over
//     micro-optimisation. Operators can layer caching later if needed.
//
//   - Per-host signals (battery health, BSOD count, missing patches,
//     etc.) live in the JSONB `last_metrics` column under
//     `health.<key>`. We extract via `last_metrics->'health'->'<key>'`
//     so adding a new health field doesn't require a migration.
//
//   - All counts are scoped to "online" agents (last_seen_at within
//     the last 5 minutes) unless otherwise documented. That mirrors
//     the Dashboard's AGENT_ONLINE_MS = 5 minutes and gives operators
//     the same answer regardless of which tile they're looking at.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// overviewAgent is the slim row the seven endpoints share. Loaded
// once per request (one query) and then folded into per-view
// aggregates.
type overviewAgent struct {
	ID            string
	Hostname      string
	SiteID        string
	OS            string
	Tags          []string
	LastSeenAt    *time.Time
	IsActive      bool
	UptimeSeconds *int64
	CPUPct        *float64
	MemUsedBytes  *int64
	MemTotalBytes *int64
	DiskUsedBytes *int64
	DiskTotalBytes *int64
	GeoCity       *string
	GeoCountry    *string
	GeoLat        *float64
	GeoLon        *float64
	GeoOrg        *string
	GeoASN        *int

	// Pulled from last_metrics JSONB — the probe's HealthSignals.
	BatteryHealthPct        *float64
	BSODCount24h            *int
	UserRebootCount24h      *int
	AppCrashCount24h        *int
	EventLogErrorCount24h   *int
	MissingPatchCount       *int
	CPUQueueLength          *float64
	DiskQueueLength         *float64
	HighloadCPUIncidents24h *int
	WiFiSignalPct           *int
	WiFiSSID                string

	// Pulled from the hardware sub-blob.
	HardwareModel string
	// Adapter-type indicator from NICs[*].kind. Set to "wifi" when
	// the host has any "up" wireless NIC, else "wired", else "".
	AdapterType string

	// Aggregates (populated by helper queries below).
	LatencyTargetAvgMs   *float64 // 8.8.8.8 (or first non-gateway target)
	LatencyTargetLossPct *float64
	LatencyGatewayAvgMs  *float64
	NetIn24hBytes        *int64
	NetOut24hBytes       *int64
}

// loadOverviewAgents loads one row per agent enriched with the
// JSONB-derived health signals. We deliberately skip extracting
// the entire last_metrics blob (it's ~50 KB per row and the
// overview pages only need a handful of fields).
func loadOverviewAgents(ctx context.Context, pool *pgxpool.Pool) ([]*overviewAgent, error) {
	rows, err := pool.Query(ctx, `
		SELECT a.id::text, a.hostname, a.site_id::text, a.os, a.tags,
		       a.last_seen_at, a.is_active, a.uptime_seconds,
		       a.cpu_pct, a.mem_used_bytes, a.mem_total_bytes,
		       a.root_disk_used_bytes, a.root_disk_total_bytes,
		       a.geo_city, a.geo_country_name, a.geo_lat, a.geo_lon, a.geo_org, a.geo_asn,
		       (a.last_metrics->'health'->>'batteryHealthPct')::float8,
		       (a.last_metrics->'health'->>'bsodCount24h')::int,
		       (a.last_metrics->'health'->>'userRebootCount24h')::int,
		       (a.last_metrics->'health'->>'appCrashCount24h')::int,
		       (a.last_metrics->'health'->>'eventLogErrorCount24h')::int,
		       (a.last_metrics->'health'->>'missingPatchCount')::int,
		       (a.last_metrics->'health'->>'cpuQueueLength')::float8,
		       (a.last_metrics->'health'->>'diskQueueLength')::float8,
		       (a.last_metrics->'health'->>'highloadCpuIncidents24h')::int,
		       (a.last_metrics->'health'->>'wifiSignalPct')::int,
		        a.last_metrics->'health'->>'wifiSsid',
		        coalesce(
		          a.last_metrics->'hardware'->'system'->>'model',
		          a.last_metrics->'hardware'->>'model',
		          ''),
		        coalesce(
		          (SELECT 'wifi' FROM jsonb_array_elements(a.last_metrics->'nics') n
		             WHERE (n->>'kind') = 'wireless' AND (n->>'up')::bool LIMIT 1),
		          (SELECT 'wired' FROM jsonb_array_elements(a.last_metrics->'nics') n
		             WHERE (n->>'kind') = 'wired' AND (n->>'up')::bool LIMIT 1),
		          '')
		  FROM agents a
		 WHERE a.is_active = TRUE
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []*overviewAgent{}
	for rows.Next() {
		var a overviewAgent
		if err := rows.Scan(
			&a.ID, &a.Hostname, &a.SiteID, &a.OS, &a.Tags,
			&a.LastSeenAt, &a.IsActive, &a.UptimeSeconds,
			&a.CPUPct, &a.MemUsedBytes, &a.MemTotalBytes,
			&a.DiskUsedBytes, &a.DiskTotalBytes,
			&a.GeoCity, &a.GeoCountry, &a.GeoLat, &a.GeoLon, &a.GeoOrg, &a.GeoASN,
			&a.BatteryHealthPct,
			&a.BSODCount24h,
			&a.UserRebootCount24h,
			&a.AppCrashCount24h,
			&a.EventLogErrorCount24h,
			&a.MissingPatchCount,
			&a.CPUQueueLength,
			&a.DiskQueueLength,
			&a.HighloadCPUIncidents24h,
			&a.WiFiSignalPct,
			&a.WiFiSSID,
			&a.HardwareModel,
			&a.AdapterType,
		); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// loadLatencyAggregates fills LatencyTargetAvgMs / Loss / GatewayAvgMs
// from agent_latency_samples averaged over the last 24h.
func loadLatencyAggregates(ctx context.Context, pool *pgxpool.Pool, agents []*overviewAgent) error {
	if len(agents) == 0 {
		return nil
	}
	rows, err := pool.Query(ctx, `
		SELECT agent_id::text, target,
		       AVG(avg_ms)::float8 AS avg_ms,
		       AVG(loss_pct)::float8 AS loss_pct
		  FROM agent_latency_samples
		 WHERE time >= NOW() - INTERVAL '24 hours'
		 GROUP BY agent_id, target
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	idx := make(map[string]*overviewAgent, len(agents))
	for _, a := range agents {
		idx[a.ID] = a
	}

	for rows.Next() {
		var aid, target string
		var avg, loss float64
		if err := rows.Scan(&aid, &target, &avg, &loss); err != nil {
			return err
		}
		a, ok := idx[aid]
		if !ok {
			continue
		}
		switch target {
		case "gateway":
			v := avg
			a.LatencyGatewayAvgMs = &v
		default:
			v := avg
			a.LatencyTargetAvgMs = &v
			lp := loss
			a.LatencyTargetLossPct = &lp
		}
	}
	return rows.Err()
}

// loadNetwork24hAggregates fills NetIn24hBytes / NetOut24hBytes —
// AVG(bps) * 86400 ≈ bytes-this-day.
func loadNetwork24hAggregates(ctx context.Context, pool *pgxpool.Pool, agents []*overviewAgent) error {
	if len(agents) == 0 {
		return nil
	}
	rows, err := pool.Query(ctx, `
		SELECT agent_id::text,
		       (AVG(in_bps)  * 86400)::bigint AS in_bytes,
		       (AVG(out_bps) * 86400)::bigint AS out_bytes
		  FROM agent_network_samples
		 WHERE time >= NOW() - INTERVAL '24 hours'
		 GROUP BY agent_id
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	idx := make(map[string]*overviewAgent, len(agents))
	for _, a := range agents {
		idx[a.ID] = a
	}
	for rows.Next() {
		var aid string
		var in, out int64
		if err := rows.Scan(&aid, &in, &out); err != nil {
			return err
		}
		if a, ok := idx[aid]; ok {
			a.NetIn24hBytes = &in
			a.NetOut24hBytes = &out
		}
	}
	return rows.Err()
}

// memPct / diskPct convert the raw (used, total) pairs to percentages.
func memPct(a *overviewAgent) *float64 {
	if a.MemTotalBytes == nil || *a.MemTotalBytes == 0 || a.MemUsedBytes == nil {
		return nil
	}
	v := float64(*a.MemUsedBytes) / float64(*a.MemTotalBytes) * 100
	return &v
}

func diskPct(a *overviewAgent) *float64 {
	if a.DiskTotalBytes == nil || *a.DiskTotalBytes == 0 || a.DiskUsedBytes == nil {
		return nil
	}
	v := float64(*a.DiskUsedBytes) / float64(*a.DiskTotalBytes) * 100
	return &v
}

// freeDiskPct returns 100 - usedPct so "least free disk" sorts
// naturally (smallest free % first).
func freeDiskPct(a *overviewAgent) *float64 {
	if p := diskPct(a); p != nil {
		v := 100 - *p
		return &v
	}
	return nil
}

// score is a thin shim over ComputeScore that pulls the inputs from
// an overviewAgent.
func (a *overviewAgent) score() float64 {
	return ComputeScore(ScoreInputs{
		CPUPct:           a.CPUPct,
		MemPct:           memPct(a),
		DiskPct:          diskPct(a),
		BatteryHealthPct: a.BatteryHealthPct,
		BSODCount24h:     a.BSODCount24h,
		AppCrashCount24h: a.AppCrashCount24h,
		LatencyAvgMs:     a.LatencyTargetAvgMs,
		LossPct:          a.LatencyTargetLossPct,
	})
}

// pickTopN returns the top n entries by `key`, with desc=true for
// "highest first" (most BSODs) and desc=false for "lowest first"
// (worst battery, least free disk).
func pickTopN(agents []*overviewAgent, n int, desc bool, key func(*overviewAgent) *float64) []map[string]any {
	type kv struct {
		a *overviewAgent
		v float64
	}
	rows := make([]kv, 0, len(agents))
	for _, a := range agents {
		k := key(a)
		if k == nil {
			continue
		}
		rows = append(rows, kv{a, *k})
	}
	sort.Slice(rows, func(i, j int) bool {
		if desc {
			return rows[i].v > rows[j].v
		}
		return rows[i].v < rows[j].v
	})
	if len(rows) > n {
		rows = rows[:n]
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]any{
			"id":       r.a.ID,
			"hostname": r.a.Hostname,
			"value":    round1(r.v),
		})
	}
	return out
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

// ============================================================================
// Endpoint handlers
// ============================================================================

// handleOverviewDevicesAverages — the "Devices - Averages" view.
// Top-5 cards plus aggregated 24h trends.
func (s *Server) handleOverviewDevicesAverages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agents, err := loadOverviewAgents(ctx, s.pool)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load agents failed")
		return
	}

	// Top-5 cards
	worstBattery := pickTopN(agents, 5, false, func(a *overviewAgent) *float64 { return a.BatteryHealthPct })
	mostBSOD := pickTopN(agents, 5, true, func(a *overviewAgent) *float64 {
		if a.BSODCount24h == nil {
			return nil
		}
		v := float64(*a.BSODCount24h)
		return &v
	})
	mostCrashes := pickTopN(agents, 5, true, func(a *overviewAgent) *float64 {
		if a.AppCrashCount24h == nil {
			return nil
		}
		v := float64(*a.AppCrashCount24h)
		return &v
	})
	leastFreeDisk := pickTopN(agents, 5, false, freeDiskPct)
	mostPatches := pickTopN(agents, 5, true, func(a *overviewAgent) *float64 {
		if a.MissingPatchCount == nil {
			return nil
		}
		v := float64(*a.MissingPatchCount)
		return &v
	})
	mostEventErrors := pickTopN(agents, 5, true, func(a *overviewAgent) *float64 {
		if a.EventLogErrorCount24h == nil {
			return nil
		}
		v := float64(*a.EventLogErrorCount24h)
		return &v
	})

	// Hourly aggregates: shape is [{hour, avg}] suitable for a
	// 24-bucket area chart. The frontend stitches to a Date axis.
	hourly := func(query string, valExpr string) []map[string]any {
		rows, err := s.pool.Query(ctx, fmt.Sprintf(`
			SELECT date_trunc('hour', time) AS hour, %s
			  FROM %s
			 WHERE time >= NOW() - INTERVAL '24 hours'
			 GROUP BY hour
			 ORDER BY hour ASC
		`, valExpr, query))
		if err != nil {
			return nil
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var h time.Time
			var v float64
			if err := rows.Scan(&h, &v); err != nil {
				continue
			}
			out = append(out, map[string]any{"hour": h, "value": round1(v)})
		}
		return out
	}

	cpuTrend := hourly("agent_metric_samples", "AVG(cpu_pct)::float8 AS v")
	memTrend := hourly("agent_metric_samples",
		"(AVG(mem_used_bytes::float8 / NULLIF(mem_total_bytes,0)) * 100)::float8 AS v")
	diskQueue := hourlyHealth(ctx, s.pool, "diskQueueLength")
	cpuQueue := hourlyHealth(ctx, s.pool, "cpuQueueLength")
	netMBps := hourly("agent_network_samples",
		"(AVG(in_bps + out_bps) / 1024.0 / 1024.0)::float8 AS v")
	netHourly := hourly("agent_network_samples",
		"(AVG(in_bps + out_bps) * 3600 / 1024.0 / 1024.0)::float8 AS v")

	writeJSON(w, http.StatusOK, map[string]any{
		"top": map[string]any{
			"worstBatteryHealth": worstBattery,
			"mostBSODs":          mostBSOD,
			"mostAppCrashes":     mostCrashes,
			"leastFreeDiskPct":   leastFreeDisk,
			"mostMissingPatches": mostPatches,
			"mostEventLogErrors": mostEventErrors,
		},
		"trends": map[string]any{
			"cpuPct":         cpuTrend,
			"cpuQueueLength": cpuQueue,
			"memPct":         memTrend,
			"diskQueueLength": diskQueue,
			"networkMBps":    netMBps,
			"networkHourly":  netHourly,
		},
		"asOf": time.Now().UTC(),
	})
}

// hourlyHealth pulls a numeric scalar out of last_metrics->health
// across every snapshot in the last 24h. It's a separate helper
// because the JSONB extraction can't be parameterised by the same
// fmt.Sprintf path used for the dedicated sample tables.
func hourlyHealth(ctx context.Context, pool *pgxpool.Pool, key string) []map[string]any {
	rows, err := pool.Query(ctx, `
		SELECT date_trunc('hour', last_metrics_at) AS hour,
		       AVG((last_metrics->'health'->>$1)::float8)::float8 AS v
		  FROM agents
		 WHERE last_metrics_at >= NOW() - INTERVAL '24 hours'
		   AND last_metrics ? 'health'
		 GROUP BY hour
		 ORDER BY hour ASC
	`, key)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var h time.Time
		var v *float64
		if err := rows.Scan(&h, &v); err != nil {
			continue
		}
		val := 0.0
		if v != nil {
			val = round1(*v)
		}
		out = append(out, map[string]any{"hour": h, "value": val})
	}
	return out
}

// handleOverviewDevicesPerformance — the "Devices - Performance" view.
// Counts + KPI tiles + top/bottom 5 models + map data.
func (s *Server) handleOverviewDevicesPerformance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agents, err := loadOverviewAgents(ctx, s.pool)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load agents failed")
		return
	}
	if err := loadLatencyAggregates(ctx, s.pool, agents); err != nil {
		// Non-fatal — score still computes without latency.
		s.log.Warn("overview: latency aggregates failed", "err", err)
	}

	// Per-OS counts.
	byOS := map[string]int{}
	for _, a := range agents {
		byOS[a.OS]++
	}

	// KPI tiles.
	now := time.Now()
	kpi := map[string]int{}
	for _, a := range agents {
		kpi["total"]++
		if a.BSODCount24h != nil {
			kpi["bsodCount"] += *a.BSODCount24h
		}
		if a.MissingPatchCount != nil {
			kpi["missingPatchCount"] += *a.MissingPatchCount
		}
		if a.UserRebootCount24h != nil {
			kpi["userRebootCount"] += *a.UserRebootCount24h
		}
		if a.AppCrashCount24h != nil {
			kpi["appCrashCount"] += *a.AppCrashCount24h
		}
		if a.HighloadCPUIncidents24h != nil {
			kpi["highloadCpuIncidents"] += *a.HighloadCPUIncidents24h
		}
		if a.BatteryHealthPct != nil && *a.BatteryHealthPct < 50 {
			kpi["lowBatteryHealth"]++
		}
		if a.UptimeSeconds != nil && *a.UptimeSeconds > 30*24*3600 && a.OS == "windows" {
			kpi["winUptime30d"]++
		}
		if mp := memPct(a); mp != nil && *mp > 90 {
			kpi["lowRAM"]++
		}
		if fd := freeDiskPct(a); fd != nil && *fd < 5 {
			kpi["lowFreeDisk"]++
		}
		if a.score() < 5 {
			kpi["lowDeviceScore"]++
		}
	}

	// Top/bottom 5 models by score.
	type modelAgg struct {
		model    string
		count    int
		scoreSum float64
	}
	models := map[string]*modelAgg{}
	for _, a := range agents {
		if a.HardwareModel == "" {
			continue
		}
		m := models[a.HardwareModel]
		if m == nil {
			m = &modelAgg{model: a.HardwareModel}
			models[a.HardwareModel] = m
		}
		m.count++
		m.scoreSum += a.score()
	}
	type modelRow struct {
		Model string  `json:"model"`
		Count int     `json:"count"`
		Score float64 `json:"score"`
	}
	rows := make([]modelRow, 0, len(models))
	for _, m := range models {
		rows = append(rows, modelRow{Model: m.model, Count: m.count, Score: round1(m.scoreSum / float64(m.count))})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Score > rows[j].Score })
	top5 := rows
	if len(top5) > 5 {
		top5 = top5[:5]
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Score < rows[j].Score })
	bottom5 := rows
	if len(bottom5) > 5 {
		bottom5 = bottom5[:5]
	}

	// Map data — every host with a known geo. The UI clusters at
	// low zoom levels so we don't need to pre-aggregate here.
	mapData := []map[string]any{}
	for _, a := range agents {
		if a.GeoLat == nil || a.GeoLon == nil {
			continue
		}
		mapData = append(mapData, map[string]any{
			"id":       a.ID,
			"hostname": a.Hostname,
			"lat":      *a.GeoLat,
			"lon":      *a.GeoLon,
			"score":    a.score(),
		})
	}

	// Score-over-time: we don't have per-snapshot historical scores
	// in the DB. We approximate by pulling 24 hourly buckets of avg
	// CPU/Mem and applying the same formula. Latency / battery are
	// pinned at the latest value because we don't have hourly
	// aggregates of those.
	scoreTrend := []map[string]any{}
	tRows, err := s.pool.Query(ctx, `
		SELECT date_trunc('hour', time) AS hour,
		       AVG(cpu_pct)::float8,
		       (AVG(mem_used_bytes::float8 / NULLIF(mem_total_bytes,0)) * 100)::float8,
		       (AVG(root_disk_used_bytes::float8 / NULLIF(root_disk_total_bytes,0)) * 100)::float8
		  FROM agent_metric_samples
		 WHERE time >= NOW() - INTERVAL '24 hours'
		 GROUP BY hour
		 ORDER BY hour ASC
	`)
	if err == nil {
		defer tRows.Close()
		for tRows.Next() {
			var h time.Time
			var cpu, mem, disk *float64
			if err := tRows.Scan(&h, &cpu, &mem, &disk); err != nil {
				continue
			}
			s := ComputeScore(ScoreInputs{
				CPUPct:  cpu,
				MemPct:  mem,
				DiskPct: disk,
			})
			scoreTrend = append(scoreTrend, map[string]any{"hour": h, "score": s})
		}
	}

	_ = now

	writeJSON(w, http.StatusOK, map[string]any{
		"managedDevicesByOS": byOS,
		"keyDeviceInsights":  kpi,
		"top5Models":         top5,
		"bottom5Models":      bottom5,
		"map":                mapData,
		"scoreTrend":         scoreTrend,
		"asOf":               time.Now().UTC(),
	})
}

// handleOverviewNetworkLatency — the "Network - Latency" view.
func (s *Server) handleOverviewNetworkLatency(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agents, err := loadOverviewAgents(ctx, s.pool)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load agents failed")
		return
	}
	if err := loadLatencyAggregates(ctx, s.pool, agents); err != nil {
		s.log.Warn("overview: latency aggregates failed", "err", err)
	}

	// Latency by device — top 5 highest avg.
	latencyByDevice := pickTopN(agents, 5, true, func(a *overviewAgent) *float64 { return a.LatencyTargetAvgMs })

	// Latency by ISP (geo_org). Also bucket WiFi vs Wired so the
	// dashboard can paint two overlapping bars.
	type ispAgg struct {
		isp    string
		count  int
		latSum float64
	}
	isps := map[string]*ispAgg{}
	totalRSSI := 0
	rssiCount := 0
	for _, a := range agents {
		if a.GeoOrg != nil && a.LatencyTargetAvgMs != nil {
			key := *a.GeoOrg
			m := isps[key]
			if m == nil {
				m = &ispAgg{isp: key}
				isps[key] = m
			}
			m.count++
			m.latSum += *a.LatencyTargetAvgMs
		}
		if a.WiFiSignalPct != nil {
			totalRSSI += *a.WiFiSignalPct
			rssiCount++
		}
	}
	type ispRow struct {
		ISP   string  `json:"isp"`
		Count int     `json:"count"`
		AvgMs float64 `json:"avgMs"`
	}
	rows := make([]ispRow, 0, len(isps))
	for _, m := range isps {
		rows = append(rows, ispRow{ISP: m.isp, Count: m.count, AvgMs: round1(m.latSum / float64(m.count))})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].AvgMs > rows[j].AvgMs })
	latencyByISP := rows
	sort.Slice(rows, func(i, j int) bool { return rows[i].Count > rows[j].Count })
	topISPs := rows
	if len(topISPs) > 5 {
		topISPs = topISPs[:5]
	}

	wifiSignal := 0
	if rssiCount > 0 {
		wifiSignal = totalRSSI / rssiCount
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"latencyByDevice": latencyByDevice,
		"latencyByISP":    latencyByISP,
		"topISPs":         topISPs,
		"wifiSignalAvgPct": wifiSignal,
		// Traceroute longest hops is deferred (see plan); the UI
		// renders a stub when this is empty.
		"longestTracerouteHops": []map[string]any{},
		"asOf":                  time.Now().UTC(),
	})
}

// handleOverviewNetworkPerformance — the "Network - Performance" view.
func (s *Server) handleOverviewNetworkPerformance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agents, err := loadOverviewAgents(ctx, s.pool)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load agents failed")
		return
	}
	if err := loadLatencyAggregates(ctx, s.pool, agents); err != nil {
		s.log.Warn("overview: latency aggregates failed", "err", err)
	}
	if err := loadNetwork24hAggregates(ctx, s.pool, agents); err != nil {
		s.log.Warn("overview: network aggregates failed", "err", err)
	}

	// Adapter type split by traffic.
	wifiBytes := int64(0)
	wiredBytes := int64(0)
	wifiLatSum := 0.0
	wifiLatCount := 0
	wiredLatSum := 0.0
	wiredLatCount := 0
	wifiSignalSum := 0
	wifiSignalCount := 0
	highLatencyDevices := 0

	for _, a := range agents {
		bytes := int64(0)
		if a.NetIn24hBytes != nil {
			bytes += *a.NetIn24hBytes
		}
		if a.NetOut24hBytes != nil {
			bytes += *a.NetOut24hBytes
		}
		switch a.AdapterType {
		case "wifi":
			wifiBytes += bytes
			if a.LatencyTargetAvgMs != nil {
				wifiLatSum += *a.LatencyTargetAvgMs
				wifiLatCount++
			}
			if a.WiFiSignalPct != nil {
				wifiSignalSum += *a.WiFiSignalPct
				wifiSignalCount++
			}
		case "wired":
			wiredBytes += bytes
			if a.LatencyTargetAvgMs != nil {
				wiredLatSum += *a.LatencyTargetAvgMs
				wiredLatCount++
			}
		}
		// "High latency" thresholds taken from the screenshot:
		// >350ms WiFi, >201ms wired.
		if a.LatencyTargetAvgMs != nil {
			if a.AdapterType == "wifi" && *a.LatencyTargetAvgMs > 350 {
				highLatencyDevices++
			}
			if a.AdapterType == "wired" && *a.LatencyTargetAvgMs > 201 {
				highLatencyDevices++
			}
		}
	}

	avgWiFiLat := 0.0
	if wifiLatCount > 0 {
		avgWiFiLat = wifiLatSum / float64(wifiLatCount)
	}
	avgWiredLat := 0.0
	if wiredLatCount > 0 {
		avgWiredLat = wiredLatSum / float64(wiredLatCount)
	}
	avgWiFiSignal := 0
	if wifiSignalCount > 0 {
		avgWiFiSignal = wifiSignalSum / wifiSignalCount
	}

	// Hourly MB sent/received (sum across all agents).
	hourly := []map[string]any{}
	if rows, err := s.pool.Query(ctx, `
		SELECT date_trunc('hour', time) AS hour,
		       (SUM(in_bps)  * 3600 / 1024.0 / 1024.0)::float8 AS in_mb,
		       (SUM(out_bps) * 3600 / 1024.0 / 1024.0)::float8 AS out_mb
		  FROM agent_network_samples
		 WHERE time >= NOW() - INTERVAL '24 hours'
		 GROUP BY hour
		 ORDER BY hour ASC
	`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var h time.Time
			var inMB, outMB float64
			if err := rows.Scan(&h, &inMB, &outMB); err != nil {
				continue
			}
			hourly = append(hourly, map[string]any{
				"hour":   h,
				"inMB":   round1(inMB),
				"outMB":  round1(outMB),
			})
		}
	}

	// Latency by adapter — hourly average split.
	latByAdapter := []map[string]any{}
	if rows, err := s.pool.Query(ctx, `
		SELECT date_trunc('hour', l.time) AS hour,
		       AVG(CASE WHEN a.last_metrics->'nics' IS NOT NULL
		                THEN (
		                   SELECT CASE
		                     WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(a.last_metrics->'nics') n
		                                   WHERE (n->>'kind')='wireless' AND (n->>'up')::bool)
		                     THEN l.avg_ms ELSE NULL END
		                ) END)::float8 AS wifi_avg,
		       AVG(CASE WHEN a.last_metrics->'nics' IS NOT NULL
		                THEN (
		                   SELECT CASE
		                     WHEN NOT EXISTS (SELECT 1 FROM jsonb_array_elements(a.last_metrics->'nics') n
		                                       WHERE (n->>'kind')='wireless' AND (n->>'up')::bool)
		                     THEN l.avg_ms ELSE NULL END
		                ) END)::float8 AS wired_avg
		  FROM agent_latency_samples l
		  JOIN agents a ON a.id = l.agent_id
		 WHERE l.time >= NOW() - INTERVAL '24 hours'
		   AND l.target = '8.8.8.8'
		 GROUP BY hour
		 ORDER BY hour ASC
	`); err == nil {
		defer rows.Close()
		for rows.Next() {
			var h time.Time
			var wifi, wired *float64
			if err := rows.Scan(&h, &wifi, &wired); err != nil {
				continue
			}
			row := map[string]any{"hour": h}
			if wifi != nil {
				row["wifi"] = round1(*wifi)
			}
			if wired != nil {
				row["wired"] = round1(*wired)
			}
			latByAdapter = append(latByAdapter, row)
		}
	}

	// Top/bottom ISPs by latency.
	type ispAgg struct {
		isp    string
		count  int
		latSum float64
	}
	isps := map[string]*ispAgg{}
	for _, a := range agents {
		if a.GeoOrg == nil || a.LatencyTargetAvgMs == nil {
			continue
		}
		m := isps[*a.GeoOrg]
		if m == nil {
			m = &ispAgg{isp: *a.GeoOrg}
			isps[*a.GeoOrg] = m
		}
		m.count++
		m.latSum += *a.LatencyTargetAvgMs
	}
	type ispRow struct {
		ISP   string  `json:"isp"`
		Count int     `json:"count"`
		AvgMs float64 `json:"avgMs"`
	}
	ispRows := make([]ispRow, 0, len(isps))
	for _, m := range isps {
		ispRows = append(ispRows, ispRow{ISP: m.isp, Count: m.count, AvgMs: round1(m.latSum / float64(m.count))})
	}
	sort.Slice(ispRows, func(i, j int) bool { return ispRows[i].AvgMs < ispRows[j].AvgMs })
	topISPs := append([]ispRow(nil), ispRows...)
	if len(topISPs) > 5 {
		topISPs = topISPs[:5]
	}
	sort.Slice(ispRows, func(i, j int) bool { return ispRows[i].AvgMs > ispRows[j].AvgMs })
	bottomISPs := append([]ispRow(nil), ispRows...)
	if len(bottomISPs) > 5 {
		bottomISPs = bottomISPs[:5]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"adapterSplit": map[string]any{
			"wifiBytes24h":  wifiBytes,
			"wiredBytes24h": wiredBytes,
			"deviceCount":   len(agents),
		},
		"hourlyMB":             hourly,
		"highLatencyDevices":   highLatencyDevices,
		"avgWiredLatencyMs":    round1(avgWiredLat),
		"avgWiFiLatencyMs":     round1(avgWiFiLat),
		"avgWiFiSignalPct":     avgWiFiSignal,
		"latencyByAdapter":     latByAdapter,
		"topISPsByLatency":     topISPs,
		"bottomISPsByLatency":  bottomISPs,
		"asOf":                 time.Now().UTC(),
	})
}

// handleOverviewApplicationsPerformance — placeholder. We surface the
// aggregate AppCrashCount24h so the operator at least sees "the
// platform is collecting this" but there's no per-application detail
// until the probe gains WerFault report parsing (see plan's Open
// scope notes).
func (s *Server) handleOverviewApplicationsPerformance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agents, err := loadOverviewAgents(ctx, s.pool)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load agents failed")
		return
	}

	totalCrashes := 0
	mostCrashes := pickTopN(agents, 5, true, func(a *overviewAgent) *float64 {
		if a.AppCrashCount24h == nil {
			return nil
		}
		v := float64(*a.AppCrashCount24h)
		return &v
	})
	for _, a := range agents {
		if a.AppCrashCount24h != nil {
			totalCrashes += *a.AppCrashCount24h
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"coverage": map[string]any{
			"appCrashes":      true,
			"perAppBreakdown": false,
			"appLaunches":     false,
		},
		"summary": map[string]any{
			"totalCrashes24h": totalCrashes,
			"deviceCount":     len(agents),
		},
		"mostCrashes": mostCrashes,
		"asOf":        time.Now().UTC(),
	})
}

// handleOverviewUserExperience — composite scores per-host plus a
// distribution histogram (10 buckets, 0–10 score).
func (s *Server) handleOverviewUserExperience(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agents, err := loadOverviewAgents(ctx, s.pool)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load agents failed")
		return
	}
	if err := loadLatencyAggregates(ctx, s.pool, agents); err != nil {
		s.log.Warn("overview: latency aggregates failed", "err", err)
	}

	type scoredHost struct {
		ID       string  `json:"id"`
		Hostname string  `json:"hostname"`
		Score    float64 `json:"score"`
	}
	rows := make([]scoredHost, 0, len(agents))
	hist := make([]int, 11) // 0..10 inclusive
	totalScore := 0.0
	for _, a := range agents {
		s := a.score()
		rows = append(rows, scoredHost{ID: a.ID, Hostname: a.Hostname, Score: s})
		bucket := int(s + 0.5)
		if bucket < 0 {
			bucket = 0
		}
		if bucket > 10 {
			bucket = 10
		}
		hist[bucket]++
		totalScore += s
	}
	avg := 0.0
	if len(agents) > 0 {
		avg = totalScore / float64(len(agents))
	}

	sort.Slice(rows, func(i, j int) bool { return rows[i].Score < rows[j].Score })
	worst := rows
	if len(worst) > 10 {
		worst = worst[:10]
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Score > rows[j].Score })
	best := rows
	if len(best) > 10 {
		best = best[:10]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"averageScore": round1(avg),
		"histogram":    hist,
		"worst":        worst,
		"best":         best,
		"deviceCount":  len(agents),
		"asOf":         time.Now().UTC(),
	})
}

// suppress unused import warning for json — we re-use it indirectly
// through pgx Scan into JSONB.
var _ = json.Marshal
