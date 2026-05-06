// Package api — agent telemetry ingest + read paths.
//
// This file owns the WebSocket frame demuxer for /agent/ws and the
// REST endpoints the UI uses to render an agent's "system tab".
//
// Wire format (probe → API), one JSON object per text frame:
//
//	{
//	  "type": "hello"     | "heartbeat" | "metrics",
//	  "agentId": "<uuid>",
//	  "sentAt":  "<rfc3339nano>",
//	  ...kind-specific fields...
//	}
//
// Only `metrics` carries a payload today (the full host snapshot under
// `snapshot`); `hello` and `heartbeat` are header-only and just keep
// the connection warm + bump last_seen_at via the parallel touch
// goroutine.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// frameEnvelope is the minimal shape we need to dispatch a frame. The
// payload-bearing fields are decoded a second time into the kind-
// specific struct once we know what we're looking at — saves us
// allocating a 50 KB Snapshot for every heartbeat.
type frameEnvelope struct {
	Type    string          `json:"type"`
	AgentID string          `json:"agentId"`
	SentAt  string          `json:"sentAt"`
	Raw     json.RawMessage `json:"-"`
}

// metricsFrame is the full /metrics frame body. The Snapshot field is
// kept as RawMessage so we can store it verbatim in JSONB without a
// re-marshal round-trip, while still letting us pull denormalized
// columns out of the parallel `parsedSnapshot` decode.
type metricsFrame struct {
	Type     string          `json:"type"`
	AgentID  string          `json:"agentId"`
	SentAt   string          `json:"sentAt"`
	Snapshot json.RawMessage `json:"snapshot"`
}

// parsedDisk / parsedNIC / parsedLatency are lifted into named types
// so helper functions can accept them by reference without dragging
// the full Snapshot's anonymous struct literals into their
// signatures (Go matches anonymous structs by exact field equality;
// adding a new field to parsedSnapshot.NICs would otherwise require
// updating every helper call site).
type parsedDisk struct {
	Mountpoint string `json:"mountpoint"`
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}

type parsedNIC struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Up           bool     `json:"up"`
	Addresses    []string `json:"addresses"`
	BytesSentBps uint64   `json:"bytesSentBps"`
	BytesRecvBps uint64   `json:"bytesRecvBps"`
}

type parsedLatency struct {
	Target  string  `json:"target"`
	Address string  `json:"address"`
	AvgMs   float64 `json:"avgMs"`
	MinMs   float64 `json:"minMs"`
	MaxMs   float64 `json:"maxMs"`
	LossPct float64 `json:"lossPct"`
}

// parsedSnapshot mirrors just the few fields we lift into typed
// columns. It intentionally does not duplicate the full Snapshot
// struct from the probe package — keeping the shapes weakly coupled
// means a probe-side schema bump only needs an opt-in change here.
type parsedSnapshot struct {
	Host struct {
		UptimeSeconds uint64 `json:"uptimeSeconds"`
	} `json:"host"`
	PublicIP string `json:"publicIp"`
	CPU      struct {
		UsagePct float64 `json:"usagePct"`
	} `json:"cpu"`
	Memory struct {
		TotalBytes uint64 `json:"totalBytes"`
		UsedBytes  uint64 `json:"usedBytes"`
	} `json:"memory"`
	Disks []parsedDisk `json:"disks"`
	NICs  []parsedNIC  `json:"nics"`
	// Latency is the per-target ICMP RTT report (one row per target,
	// today: "8.8.8.8" + "gateway"). Schema v4+. Older probes omit it.
	Latency       []parsedLatency `json:"latency"`
	PendingReboot bool            `json:"pendingReboot"`
}

// runAgentIngestLoop upgrades the request to a websocket, runs the
// keepalive ticker, and reads frames until the agent disconnects.
// Any error from a single frame is logged but does not tear down the
// connection — a probe with a buggy collector should still be able to
// keep its `last_seen_at` fresh.
func (s *Server) runAgentIngestLoop(parent context.Context, w http.ResponseWriter, r *http.Request, agentID uuid.UUID) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Cloudflare-fronted; in-cluster is HTTP.
	})
	if err != nil {
		s.log.Warn("agent ws accept failed", "err", err, "agent_id", agentID)
		return
	}
	defer c.CloseNow()

	// 16 MB is generous: a worst-case snapshot is ~50 KB even with
	// large process tables, and we want a comfortable margin so we
	// never drop a frame because the agent had a chatty minute.
	c.SetReadLimit(16 * 1024 * 1024)

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	go agentKeepalive(ctx, c)

	log := s.log.With("agent_id", agentID)

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			// Normal close paths log at debug; everything else is a warning.
			if isNormalWSClose(err) || errors.Is(err, context.Canceled) {
				log.Debug("agent ws closed", "err", err)
			} else {
				log.Warn("agent ws read failed", "err", err)
			}
			return
		}
		s.handleAgentFrame(ctx, log.With("frame_bytes", len(data)), agentID, data)
	}
}

// agentKeepalive pings the connection on a 25s cadence. Same interval
// as the UI hub — short enough to beat Cloudflare's idle reaper.
func agentKeepalive(ctx context.Context, c *websocket.Conn) {
	t := time.NewTicker(25 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}

// isNormalWSClose returns true for the variety of close errors the
// websocket library raises during a clean shutdown — none of which
// indicate a real problem.
func isNormalWSClose(err error) bool {
	if err == nil {
		return true
	}
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		switch ce.Code {
		case websocket.StatusNormalClosure,
			websocket.StatusGoingAway,
			websocket.StatusNoStatusRcvd:
			return true
		}
	}
	msg := err.Error()
	return strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "use of closed network connection")
}

// handleAgentFrame demuxes one inbound frame. Unknown `type`s are
// logged and dropped; the connection stays open so a forward-compat
// probe doesn't get hung up on for sending a frame we don't yet
// understand.
func (s *Server) handleAgentFrame(ctx context.Context, log loggerLike, agentID uuid.UUID, data []byte) {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		log.Warn("agent frame: bad json", "err", err)
		return
	}
	switch env.Type {
	case "hello", "heartbeat":
		// Header-only. last_seen_at is touched by the parallel
		// heartbeat goroutine on a 60s tick; nothing to do here.
		return
	case "metrics":
		var f metricsFrame
		if err := json.Unmarshal(data, &f); err != nil {
			log.Warn("metrics frame: bad json", "err", err)
			return
		}
		if len(f.Snapshot) == 0 {
			log.Warn("metrics frame: empty snapshot")
			return
		}
		if err := s.ingestMetrics(ctx, agentID, f); err != nil {
			log.Warn("metrics frame: store failed", "err", err)
		}
	default:
		log.Debug("agent frame: unknown type", "type", env.Type)
	}
}

// loggerLike is the slim slog.Logger surface the demuxer needs.
// Threading the real logger through this seam keeps the call sites
// unit-testable without spinning up a Server.
type loggerLike interface {
	Debug(msg string, args ...any)
	Warn(msg string, args ...any)
}

// ingestMetrics writes the snapshot in two places:
//
//  1. agents.last_metrics + denormalized headline columns — the row
//     the detail page reads.
//  2. agent_metric_samples — one append-only row per snapshot,
//     pruned by the timescale retention policy.
//
// Both writes share a transaction so the list view's sparkline counts
// and the detail page never disagree about which snapshot was most
// recent. We also tolerate a bit of clock skew between probe and API:
// if `sentAt` is nonsense, we fall back to NOW().
func (s *Server) ingestMetrics(ctx context.Context, agentID uuid.UUID, f metricsFrame) error {
	var ps parsedSnapshot
	if err := json.Unmarshal(f.Snapshot, &ps); err != nil {
		return fmt.Errorf("decode snapshot fields: %w", err)
	}

	sentAt := parseRFC3339OrNow(f.SentAt)

	rootUsed, rootTotal := pickRootDisk(ps.Disks)
	primaryIP := pickPrimaryIP(ps.NICs)

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		UPDATE agents
		   SET last_metrics          = $2::jsonb,
		       last_metrics_at       = $3,
		       cpu_pct               = $4,
		       mem_used_bytes        = $5,
		       mem_total_bytes       = $6,
		       root_disk_used_bytes  = $7,
		       root_disk_total_bytes = $8,
		       uptime_seconds        = $9,
		       pending_reboot        = $10,
		       primary_ip            = NULLIF($11, '')::inet,
		       last_seen_at          = NOW()
		 WHERE id = $1
	`,
		agentID,
		string(f.Snapshot),
		sentAt,
		ps.CPU.UsagePct,
		ps.Memory.UsedBytes,
		ps.Memory.TotalBytes,
		nullableUint64(rootUsed),
		nullableUint64(rootTotal),
		ps.Host.UptimeSeconds,
		ps.PendingReboot,
		primaryIP,
	)
	if err != nil {
		return fmt.Errorf("update agents: %w", err)
	}

	// Public IP + GeoIP enrichment is conditional: skip the write
	// entirely when the probe didn't report one (e.g. air-gapped
	// LAN). When it did, only re-resolve geo if the IP changed
	// since last snapshot or the cached lookup is older than 7
	// days — keeps ingest cheap on the steady-state path.
	if ps.PublicIP != "" {
		if err := s.maybeUpdateAgentGeo(ctx, tx, agentID, ps.PublicIP, sentAt); err != nil {
			s.log.Warn("agent geo update failed", "err", err, "agent_id", agentID)
		}
	}

	// ON CONFLICT keeps duplicate-second collisions from breaking
	// ingest — under normal operation a collision is impossible
	// (60s cadence, monotonic clock), but a probe restart loop or a
	// test harness can produce them.
	_, err = tx.Exec(ctx, `
		INSERT INTO agent_metric_samples
		  (agent_id, time, cpu_pct, mem_used_bytes, mem_total_bytes,
		   root_disk_used_bytes, root_disk_total_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (agent_id, time) DO UPDATE SET
		  cpu_pct               = EXCLUDED.cpu_pct,
		  mem_used_bytes        = EXCLUDED.mem_used_bytes,
		  mem_total_bytes       = EXCLUDED.mem_total_bytes,
		  root_disk_used_bytes  = EXCLUDED.root_disk_used_bytes,
		  root_disk_total_bytes = EXCLUDED.root_disk_total_bytes
	`,
		agentID,
		sentAt,
		ps.CPU.UsagePct,
		ps.Memory.UsedBytes,
		ps.Memory.TotalBytes,
		nullableUint64(rootUsed),
		nullableUint64(rootTotal),
	)
	if err != nil {
		return fmt.Errorf("insert sample: %w", err)
	}

	// Aggregate physical-NIC throughput for the network sample. We
	// deliberately exclude virtual / loopback adapters so the chart
	// reflects what actually crossed the wire — Hyper-V / Docker
	// veth bytes would otherwise double-count host traffic.
	if inBps, outBps, ok := sumPhysicalNICBps(ps.NICs); ok {
		_, err = tx.Exec(ctx, `
			INSERT INTO agent_network_samples
			  (agent_id, time, in_bps, out_bps)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (agent_id, time) DO UPDATE SET
			  in_bps  = EXCLUDED.in_bps,
			  out_bps = EXCLUDED.out_bps
		`,
			agentID,
			sentAt,
			int64(inBps),
			int64(outBps),
		)
		if err != nil {
			return fmt.Errorf("insert network sample: %w", err)
		}
	}

	// Per-target latency rows. ON CONFLICT keeps a probe restart
	// from causing duplicate-key failures during a "double snapshot
	// at the same instant" race.
	for _, lr := range ps.Latency {
		if lr.Target == "" {
			continue
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO agent_latency_samples
			  (agent_id, time, target, address, avg_ms, min_ms, max_ms, loss_pct)
			VALUES ($1, $2, $3, NULLIF($4, '')::inet, $5, $6, $7, $8)
			ON CONFLICT (agent_id, time, target) DO UPDATE SET
			  address  = EXCLUDED.address,
			  avg_ms   = EXCLUDED.avg_ms,
			  min_ms   = EXCLUDED.min_ms,
			  max_ms   = EXCLUDED.max_ms,
			  loss_pct = EXCLUDED.loss_pct
		`,
			agentID,
			sentAt,
			lr.Target,
			lr.Address,
			lr.AvgMs,
			lr.MinMs,
			lr.MaxMs,
			lr.LossPct,
		)
		if err != nil {
			return fmt.Errorf("insert latency sample: %w", err)
		}
	}

	var siteID uuid.UUID
	var crit string
	var hostname string
	if err := tx.QueryRow(ctx, `
		SELECT site_id, COALESCE(criticality, 'normal'), hostname FROM agents WHERE id = $1`,
		agentID).Scan(&siteID, &crit, &hostname); err != nil {
		return fmt.Errorf("read agent row: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	memRatio := 0.0
	if ps.Memory.TotalBytes > 0 {
		memRatio = float64(ps.Memory.UsedBytes) / float64(ps.Memory.TotalBytes)
	}
	if s.nats != nil && s.nats.IsConnected() {
		payload := map[string]any{
			"agentId":       agentID.String(),
			"siteId":        siteID.String(),
			"cpuPct":        ps.CPU.UsagePct,
			"memUsedRatio":  memRatio,
			"criticality":   crit,
			"vendor":        hostname,
		}
		if b, err := json.Marshal(payload); err == nil {
			if err := s.nats.Publish("metrics.agent", b); err != nil {
				s.log.Debug("metrics.agent publish failed", "err", err, "agent_id", agentID)
			}
		}
	}
	return nil
}

// sumPhysicalNICBps adds up bytesSentBps / bytesRecvBps across NICs
// the probe classified as "wired" or "wireless". Loopback (lo) and
// virtual (Hyper-V switch, docker0, vEthernet, tunnel) interfaces
// are excluded so the aggregate matches what crossed the host's
// physical link. Returns ok=false when there's no usable NIC at all
// — older probes (schema v3) didn't populate Kind, so we fall back
// to "any NIC that's up and has an address" in that case.
func sumPhysicalNICBps(nics []parsedNIC) (in uint64, out uint64, ok bool) {
	var hasKind bool
	for _, n := range nics {
		if n.Kind != "" {
			hasKind = true
			break
		}
	}
	for _, n := range nics {
		if !n.Up {
			continue
		}
		if hasKind {
			if n.Kind != "wired" && n.Kind != "wireless" {
				continue
			}
		} else {
			// v3-fallback: skip loopback by name, count everything else.
			if strings.EqualFold(n.Name, "lo") || strings.HasPrefix(strings.ToLower(n.Name), "loopback") {
				continue
			}
		}
		in += n.BytesRecvBps
		out += n.BytesSentBps
		ok = true
	}
	return in, out, ok
}

func parseRFC3339OrNow(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	return time.Now().UTC()
}

// pickRootDisk chooses the volume the operator most likely thinks of
// as "the system disk". On Linux that's `/`; on Windows it's `C:` /
// `C:\`. Falls back to the largest volume so we always surface
// something on exotic layouts.
func pickRootDisk(disks []parsedDisk) (used, total uint64) {
	for _, d := range disks {
		mp := strings.ToLower(strings.TrimSpace(d.Mountpoint))
		if mp == "/" || mp == "c:" || mp == "c:\\" || mp == `c:\` {
			return d.UsedBytes, d.TotalBytes
		}
	}
	var best uint64
	for _, d := range disks {
		if d.TotalBytes > best {
			best = d.TotalBytes
			used, total = d.UsedBytes, d.TotalBytes
		}
	}
	return used, total
}

// pickPrimaryIP returns the first non-loopback, non-link-local IPv4
// address attached to an "up" interface — the value most useful in a
// list view ("which box is at 10.20.30.40?"). Returns "" when nothing
// matches; the caller turns that into a SQL NULL.
func pickPrimaryIP(nics []parsedNIC) string {
	for _, n := range nics {
		if !n.Up {
			continue
		}
		for _, a := range n.Addresses {
			ip := stripCIDR(a)
			if ip == "" {
				continue
			}
			if isLoopbackOrLinkLocal(ip) {
				continue
			}
			if strings.Contains(ip, ":") { // skip IPv6 for the headline column
				continue
			}
			return ip
		}
	}
	return ""
}

func stripCIDR(addr string) string {
	if i := strings.Index(addr, "/"); i >= 0 {
		return addr[:i]
	}
	return addr
}

func isLoopbackOrLinkLocal(ip string) bool {
	switch {
	case strings.HasPrefix(ip, "127."), strings.HasPrefix(ip, "169.254."), ip == "::1":
		return true
	}
	return false
}

// nullableUint64 returns nil when v is zero so the column gets a SQL
// NULL instead of a misleading "0 bytes". Disk sizing is the obvious
// case: a missing root volume is "we don't know", not "no space".
func nullableUint64(v uint64) any {
	if v == 0 {
		return nil
	}
	return int64(v)
}

// geoTTL is how long a cached MaxMind lookup is considered fresh.
// Longer than the per-snapshot interval but short enough to pick up
// real-world IP renumbering inside a single billing cycle.
const geoTTL = 7 * 24 * time.Hour

// maybeUpdateAgentGeo writes public_ip + (optionally) the geo_*
// columns. Geo lookup is skipped when:
//   - the public IP didn't change since last snapshot AND geo was
//     resolved within geoTTL, OR
//   - no GeoIP databases are loaded.
//
// Runs inside the same transaction as the headline metrics update so
// a partial failure rolls the whole snapshot back rather than leaving
// inconsistent rows.
func (s *Server) maybeUpdateAgentGeo(ctx context.Context, tx pgx.Tx, agentID uuid.UUID, publicIP string, sentAt time.Time) error {
	var (
		curIP       *string
		curResolved *time.Time
	)
	if err := tx.QueryRow(ctx,
		`SELECT host(public_ip), geo_resolved_at FROM agents WHERE id = $1`,
		agentID,
	).Scan(&curIP, &curResolved); err != nil {
		return fmt.Errorf("read current geo: %w", err)
	}

	ipChanged := curIP == nil || *curIP != publicIP
	stale := curResolved == nil || time.Since(*curResolved) > geoTTL
	needsLookup := s.geo != nil && s.geo.Has() && (ipChanged || stale)

	if !needsLookup {
		// Cheap path: just bump public_ip + seen_at.
		_, err := tx.Exec(ctx, `
			UPDATE agents
			   SET public_ip         = NULLIF($2, '')::inet,
			       public_ip_seen_at = $3
			 WHERE id = $1
		`, agentID, publicIP, sentAt)
		return err
	}

	res, ok := s.geo.Lookup(publicIP)
	if !ok {
		// IP didn't resolve (rare for public IPs but happens for
		// fresh Cloudflare/AWS allocations). Still record the IP
		// itself so the operator can see the value.
		_, err := tx.Exec(ctx, `
			UPDATE agents
			   SET public_ip         = NULLIF($2, '')::inet,
			       public_ip_seen_at = $3,
			       geo_resolved_at   = $3
			 WHERE id = $1
		`, agentID, publicIP, sentAt)
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE agents
		   SET public_ip         = NULLIF($2, '')::inet,
		       public_ip_seen_at = $3,
		       geo_country_iso   = NULLIF($4, ''),
		       geo_country_name  = NULLIF($5, ''),
		       geo_subdivision   = NULLIF($6, ''),
		       geo_city          = NULLIF($7, ''),
		       geo_lat           = $8,
		       geo_lon           = $9,
		       geo_asn           = NULLIF($10, 0),
		       geo_org           = NULLIF($11, ''),
		       geo_resolved_at   = $3
		 WHERE id = $1
	`,
		agentID, publicIP, sentAt,
		res.CountryISO, res.CountryName, res.Subdivision, res.City,
		nullableFloat64(res.Lat), nullableFloat64(res.Lon),
		int(res.ASN), res.Organization,
	)
	return err
}

// nullableFloat64 mirrors nullableUint64 for the geo coordinate
// columns: emit SQL NULL on the zero value rather than 0.0, so a
// missing-coordinates row reads as "unknown location" on the map
// instead of "(0,0) in the Atlantic".
func nullableFloat64(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

// ============================================================================
// REST endpoints — agent detail + metrics history
// ============================================================================

// agentDetailView is what GET /agents/{id} returns: every column on
// the row plus the parsed `last_metrics` so the UI can render the
// detail page from a single fetch.
type agentDetailView struct {
	ID                 string          `json:"id"`
	SiteID             string          `json:"siteId"`
	Hostname           string          `json:"hostname"`
	Fingerprint        *string         `json:"fingerprint,omitempty"`
	OS                 string          `json:"os"`
	OSVersion          string          `json:"osVersion"`
	AgentVersion       string          `json:"agentVersion"`
	EnrolledAt         *time.Time      `json:"enrolledAt,omitempty"`
	LastSeenAt         *time.Time      `json:"lastSeenAt,omitempty"`
	IsActive           bool            `json:"isActive"`
	Tags               []string        `json:"tags"`
	CreatedAt          time.Time       `json:"createdAt"`
	CPUPct             *float64        `json:"cpuPct,omitempty"`
	MemUsedBytes       *int64          `json:"memUsedBytes,omitempty"`
	MemTotalBytes      *int64          `json:"memTotalBytes,omitempty"`
	RootDiskUsedBytes  *int64          `json:"rootDiskUsedBytes,omitempty"`
	RootDiskTotalBytes *int64          `json:"rootDiskTotalBytes,omitempty"`
	UptimeSeconds      *int64          `json:"uptimeSeconds,omitempty"`
	PendingReboot      bool            `json:"pendingReboot"`
	PrimaryIP          *string         `json:"primaryIp,omitempty"`
	PublicIP           *string         `json:"publicIp,omitempty"`
	GeoCountryISO      *string         `json:"geoCountryIso,omitempty"`
	GeoCountryName     *string         `json:"geoCountryName,omitempty"`
	GeoSubdivision     *string         `json:"geoSubdivision,omitempty"`
	GeoCity            *string         `json:"geoCity,omitempty"`
	GeoLat             *float64        `json:"geoLat,omitempty"`
	GeoLon             *float64        `json:"geoLon,omitempty"`
	GeoASN             *int            `json:"geoAsn,omitempty"`
	GeoOrg             *string         `json:"geoOrg,omitempty"`
	LastMetricsAt      *time.Time      `json:"lastMetricsAt,omitempty"`
	LastMetrics        json.RawMessage `json:"lastMetrics,omitempty"`
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}

	const q = `
		SELECT id, site_id, hostname, fingerprint, os, os_version, agent_version,
		       enrolled_at, last_seen_at, is_active, tags, created_at,
		       cpu_pct, mem_used_bytes, mem_total_bytes,
		       root_disk_used_bytes, root_disk_total_bytes,
		       uptime_seconds, pending_reboot, host(primary_ip), host(public_ip),
		       geo_country_iso, geo_country_name, geo_subdivision, geo_city,
		       geo_lat, geo_lon, geo_asn, geo_org,
		       last_metrics_at, last_metrics
		  FROM agents
		 WHERE id = $1
	`
	var v agentDetailView
	var lastMetrics []byte
	var primaryIP, publicIP *string
	err = s.pool.QueryRow(r.Context(), q, id).Scan(
		&v.ID, &v.SiteID, &v.Hostname, &v.Fingerprint, &v.OS, &v.OSVersion, &v.AgentVersion,
		&v.EnrolledAt, &v.LastSeenAt, &v.IsActive, &v.Tags, &v.CreatedAt,
		&v.CPUPct, &v.MemUsedBytes, &v.MemTotalBytes,
		&v.RootDiskUsedBytes, &v.RootDiskTotalBytes,
		&v.UptimeSeconds, &v.PendingReboot, &primaryIP, &publicIP,
		&v.GeoCountryISO, &v.GeoCountryName, &v.GeoSubdivision, &v.GeoCity,
		&v.GeoLat, &v.GeoLon, &v.GeoASN, &v.GeoOrg,
		&v.LastMetricsAt, &lastMetrics,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusNotFound, "not_found", "agent not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "load agent failed")
		return
	}
	v.PrimaryIP = primaryIP
	v.PublicIP = publicIP
	if len(lastMetrics) > 0 {
		v.LastMetrics = json.RawMessage(lastMetrics)
	}
	writeJSON(w, http.StatusOK, v)
}

// metricSample is the shape sent to sparklines / line charts. Time
// goes first so the JSON is naturally sorted oldest → newest.
type metricSample struct {
	Time               time.Time `json:"time"`
	CPUPct             *float64  `json:"cpuPct,omitempty"`
	MemUsedBytes       *int64    `json:"memUsedBytes,omitempty"`
	MemTotalBytes      *int64    `json:"memTotalBytes,omitempty"`
	RootDiskUsedBytes  *int64    `json:"rootDiskUsedBytes,omitempty"`
	RootDiskTotalBytes *int64    `json:"rootDiskTotalBytes,omitempty"`
}

// handleAgentMetrics returns the time-series samples in [now-range, now].
// `range` accepts plain Go durations ("1h", "24h", "7d") with a 30d
// cap so a hostile or buggy client can't ask for the entire table.
func (s *Server) handleAgentMetrics(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}

	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "24h"
	}
	dur, err := parseRangeDuration(rng)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	const maxRange = 30 * 24 * time.Hour
	if dur > maxRange {
		dur = maxRange
	}

	rows, err := s.pool.Query(r.Context(), `
		SELECT time, cpu_pct, mem_used_bytes, mem_total_bytes,
		       root_disk_used_bytes, root_disk_total_bytes
		  FROM agent_metric_samples
		 WHERE agent_id = $1
		   AND time >= NOW() - $2::interval
		 ORDER BY time ASC
	`, id, fmt.Sprintf("%d seconds", int64(dur.Seconds())))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query metrics failed")
		return
	}
	defer rows.Close()

	out := []metricSample{}
	for rows.Next() {
		var m metricSample
		if err := rows.Scan(&m.Time, &m.CPUPct, &m.MemUsedBytes, &m.MemTotalBytes,
			&m.RootDiskUsedBytes, &m.RootDiskTotalBytes); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan metric failed")
			return
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "iter metrics failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agentId":      id.String(),
		"range":        dur.String(),
		"samples":      out,
		"capturedAtTo": time.Now().UTC(),
	})
}

// networkSample is one point on the network usage line chart.
type networkSample struct {
	Time   time.Time `json:"time"`
	InBps  *int64    `json:"inBps,omitempty"`
	OutBps *int64    `json:"outBps,omitempty"`
}

// networkHourly is one bar on the hourly bytes-sent / bytes-received
// chart. Hour is the start of the bucket (UTC); InBytes / OutBytes are
// totals across the whole hour computed via SUM(bps) * sample_interval
// / 1 (seconds per second). Since samples are emitted on the same 60s
// cadence as the snapshot loop, we approximate "bytes in this hour"
// as `avg(bps) * 3600`.
type networkHourly struct {
	Hour     time.Time `json:"hour"`
	InBytes  *int64    `json:"inBytes,omitempty"`
	OutBytes *int64    `json:"outBytes,omitempty"`
}

// handleAgentNetwork returns network bps samples + hourly byte totals.
// Same range parsing as handleAgentMetrics. The hourly bucket is
// computed via date_trunc('hour', ...) which works on plain Postgres
// and TimescaleDB alike (we deliberately don't use time_bucket so the
// query stays portable for CI without the extension loaded).
func (s *Server) handleAgentNetwork(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}

	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "24h"
	}
	dur, err := parseRangeDuration(rng)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	const maxRange = 30 * 24 * time.Hour
	if dur > maxRange {
		dur = maxRange
	}

	intervalSecs := fmt.Sprintf("%d seconds", int64(dur.Seconds()))

	// Per-sample series.
	rows, err := s.pool.Query(r.Context(), `
		SELECT time, in_bps, out_bps
		  FROM agent_network_samples
		 WHERE agent_id = $1
		   AND time >= NOW() - $2::interval
		 ORDER BY time ASC
	`, id, intervalSecs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query network samples failed")
		return
	}
	defer rows.Close()
	samples := []networkSample{}
	for rows.Next() {
		var m networkSample
		if err := rows.Scan(&m.Time, &m.InBps, &m.OutBps); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan network sample failed")
			return
		}
		samples = append(samples, m)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "iter network samples failed")
		return
	}

	// Hourly aggregate. AVG(bps) * 3600 ≈ bytes-this-hour. We allow
	// nulls through so the frontend can render gaps as gaps.
	hRows, err := s.pool.Query(r.Context(), `
		SELECT date_trunc('hour', time) AS hour,
		       AVG(in_bps)::BIGINT  * 3600 AS in_bytes,
		       AVG(out_bps)::BIGINT * 3600 AS out_bytes
		  FROM agent_network_samples
		 WHERE agent_id = $1
		   AND time >= NOW() - $2::interval
		 GROUP BY hour
		 ORDER BY hour ASC
	`, id, intervalSecs)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query hourly network failed")
		return
	}
	defer hRows.Close()
	hourly := []networkHourly{}
	for hRows.Next() {
		var h networkHourly
		if err := hRows.Scan(&h.Hour, &h.InBytes, &h.OutBytes); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan hourly network failed")
			return
		}
		hourly = append(hourly, h)
	}
	if err := hRows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "iter hourly network failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agentId":      id.String(),
		"range":        dur.String(),
		"samples":      samples,
		"hourly":       hourly,
		"capturedAtTo": time.Now().UTC(),
	})
}

// latencySample is one (target, time) row on the latency chart.
type latencySample struct {
	Time    time.Time `json:"time"`
	Target  string    `json:"target"`
	Address *string   `json:"address,omitempty"`
	AvgMs   *float64  `json:"avgMs,omitempty"`
	MinMs   *float64  `json:"minMs,omitempty"`
	MaxMs   *float64  `json:"maxMs,omitempty"`
	LossPct *float64  `json:"lossPct,omitempty"`
}

// handleAgentLatency returns latency samples filtered by an optional
// `target=` query parameter ("8.8.8.8", "gateway", or omitted/"both"
// for everything we have).
func (s *Server) handleAgentLatency(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}

	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "24h"
	}
	dur, err := parseRangeDuration(rng)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	const maxRange = 30 * 24 * time.Hour
	if dur > maxRange {
		dur = maxRange
	}

	target := strings.TrimSpace(r.URL.Query().Get("target"))
	args := []any{id, fmt.Sprintf("%d seconds", int64(dur.Seconds()))}
	filter := ""
	if target != "" && target != "both" && target != "all" {
		filter = "AND target = $3"
		args = append(args, target)
	}

	rows, err := s.pool.Query(r.Context(), `
		SELECT time, target, address::text, avg_ms, min_ms, max_ms, loss_pct
		  FROM agent_latency_samples
		 WHERE agent_id = $1
		   AND time >= NOW() - $2::interval
		   `+filter+`
		 ORDER BY time ASC, target ASC
	`, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "query latency failed")
		return
	}
	defer rows.Close()

	out := []latencySample{}
	for rows.Next() {
		var m latencySample
		if err := rows.Scan(&m.Time, &m.Target, &m.Address, &m.AvgMs, &m.MinMs, &m.MaxMs, &m.LossPct); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "scan latency failed")
			return
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "iter latency failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"agentId":      id.String(),
		"range":        dur.String(),
		"target":       target,
		"samples":      out,
		"capturedAtTo": time.Now().UTC(),
	})
}

// ============================================================================
// Per-host network graph — feeds the "Network connections" section on
// the agent detail page.
// ============================================================================

// netGraphSnapshot is the slim shape we extract from the JSONB blob to
// drive the per-host topology. We only need conversations (the active
// peer list) and listeners (so we can label inbound peers' local
// ports) — everything else (CPU, disks, processes) is irrelevant
// here.
type netGraphSnapshot struct {
	CapturedAt    string `json:"capturedAt"`
	Conversations []struct {
		Proto       string `json:"proto"`
		Direction   string `json:"direction"`
		RemoteIP    string `json:"remoteIp"`
		RemoteHost  string `json:"remoteHost"`
		RemotePort  int    `json:"remotePort"`
		LocalPort   int    `json:"localPort"`
		State       string `json:"state"`
		PID         int32  `json:"pid"`
		ProcessName string `json:"processName"`
		Count       int    `json:"count"`
	} `json:"conversations"`
}

// netPeer is one remote endpoint after GeoIP enrichment, with the
// list of local processes that talked to it folded in.
type netPeer struct {
	IP          string  `json:"ip"`
	Host        string  `json:"host,omitempty"`
	ASN         int     `json:"asn,omitempty"`
	Org         string  `json:"org,omitempty"`
	CountryISO  string  `json:"countryIso,omitempty"`
	CountryName string  `json:"countryName,omitempty"`
	City        string  `json:"city,omitempty"`
	Lat         float64 `json:"lat,omitempty"`
	Lon         float64 `json:"lon,omitempty"`
	Direction   string  `json:"direction"` // "inbound" | "outbound"
	IsPrivate   bool    `json:"isPrivate,omitempty"`
	// Processes that hold a socket to this peer, with the aggregate
	// connection count. Sorted by count desc.
	Processes  []netPeerProc `json:"processes"`
	TotalConns int           `json:"totalConns"`
	Ports      []int         `json:"ports,omitempty"` // unique remote ports observed
}

type netPeerProc struct {
	Name  string `json:"name"`
	PID   int32  `json:"pid,omitempty"`
	Count int    `json:"count"`
}

// handleAgentNetworkGraph returns the per-host network graph. Shape:
//
//	{
//	  "agent": {...},               // hostname / public IP / location
//	  "capturedAt": "...",
//	  "peers": [...netPeer...],
//	  "processes": ["app", "nginx", ...]   // distinct names talking out
//	}
//
// We deliberately do NOT page or filter on the server side: the
// underlying conversations list is already capped at 200 entries by
// the probe, and the front-end ForceGraph wants the whole set so it
// can lay them out in one pass.
func (s *Server) handleAgentNetworkGraph(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "id must be a UUID")
		return
	}

	const q = `
		SELECT hostname, host(public_ip), host(primary_ip),
		       geo_country_iso, geo_city, geo_lat, geo_lon, geo_asn, geo_org,
		       last_metrics, last_metrics_at
		  FROM agents
		 WHERE id = $1
	`
	var hostname string
	var publicIP, primaryIP, countryISO, city, org *string
	var lat, lon *float64
	var asn *int
	var lastMetricsAt *time.Time
	var lastMetrics []byte
	if err := s.pool.QueryRow(r.Context(), q, id).Scan(
		&hostname, &publicIP, &primaryIP,
		&countryISO, &city, &lat, &lon, &asn, &org,
		&lastMetrics, &lastMetricsAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, http.StatusNotFound, "not_found", "agent not found")
			return
		}
		writeErr(w, http.StatusInternalServerError, "server_error", "load agent failed")
		return
	}

	if len(lastMetrics) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"agent": map[string]any{
				"id":         id.String(),
				"hostname":   hostname,
				"publicIp":   publicIP,
				"primaryIp":  primaryIP,
				"countryIso": countryISO,
				"city":       city,
				"lat":        lat,
				"lon":        lon,
				"asn":        asn,
				"org":        org,
			},
			"capturedAt": nil,
			"peers":      []netPeer{},
			"processes":  []string{},
		})
		return
	}

	var snap netGraphSnapshot
	if err := json.Unmarshal(lastMetrics, &snap); err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "parse snapshot failed")
		return
	}

	// Aggregate by remote IP. We produce one netPeer per unique IP
	// even if it shows up under multiple processes / ports — the UI
	// renders one node per peer and unfolds the per-process detail
	// in the side panel.
	type peerKey struct {
		ip        string
		direction string
	}
	peers := map[peerKey]*netPeer{}
	procSet := map[string]struct{}{}
	for _, c := range snap.Conversations {
		if c.RemoteIP == "" {
			continue
		}
		key := peerKey{ip: c.RemoteIP, direction: c.Direction}
		p, ok := peers[key]
		if !ok {
			p = &netPeer{
				IP:        c.RemoteIP,
				Host:      c.RemoteHost,
				Direction: c.Direction,
				IsPrivate: isPrivateIP(c.RemoteIP),
			}
			// Enrich via GeoIP only for non-RFC1918 addresses; the
			// MaxMind DB returns all-zero rows for private ranges
			// and we don't want to render those as "ASN 0 in
			// (0,0)".
			if !p.IsPrivate && s.geo != nil && s.geo.Has() {
				if g, ok := s.geo.Lookup(c.RemoteIP); ok {
					p.ASN = int(g.ASN)
					p.Org = g.Organization
					p.CountryISO = g.CountryISO
					p.CountryName = g.CountryName
					p.City = g.City
					p.Lat = g.Lat
					p.Lon = g.Lon
				}
			}
			peers[key] = p
		}
		// Aggregate per-process counts; the same process can talk
		// to the same peer over many sockets/ports.
		merged := false
		for i := range p.Processes {
			if p.Processes[i].Name == c.ProcessName && p.Processes[i].PID == c.PID {
				p.Processes[i].Count += c.Count
				merged = true
				break
			}
		}
		if !merged {
			p.Processes = append(p.Processes, netPeerProc{
				Name:  c.ProcessName,
				PID:   c.PID,
				Count: c.Count,
			})
		}
		p.TotalConns += c.Count
		// Track distinct remote ports for the tooltip.
		if c.RemotePort > 0 {
			seen := false
			for _, pp := range p.Ports {
				if pp == c.RemotePort {
					seen = true
					break
				}
			}
			if !seen {
				p.Ports = append(p.Ports, c.RemotePort)
			}
		}
		if c.ProcessName != "" {
			procSet[c.ProcessName] = struct{}{}
		}
	}

	out := make([]netPeer, 0, len(peers))
	for _, p := range peers {
		// Sort processes by descending count so the most-active
		// process is first in the side-panel listing.
		sortPeerProcs(p.Processes)
		out = append(out, *p)
	}
	// Stable order for the response: highest connection count first,
	// IP alphabetically as tiebreaker.
	sortPeers(out)

	procs := make([]string, 0, len(procSet))
	for p := range procSet {
		procs = append(procs, p)
	}
	stringsSort(procs)

	writeJSON(w, http.StatusOK, map[string]any{
		"agent": map[string]any{
			"id":         id.String(),
			"hostname":   hostname,
			"publicIp":   publicIP,
			"primaryIp":  primaryIP,
			"countryIso": countryISO,
			"city":       city,
			"lat":        lat,
			"lon":        lon,
			"asn":        asn,
			"org":        org,
		},
		"capturedAt": lastMetricsAt,
		"peers":      out,
		"processes":  procs,
	})
}

// isPrivateIP returns true for RFC1918 / link-local / unique-local IPs
// — the addresses MaxMind doesn't (and shouldn't) resolve.
func isPrivateIP(ip string) bool {
	switch {
	case strings.HasPrefix(ip, "10."),
		strings.HasPrefix(ip, "192.168."),
		strings.HasPrefix(ip, "169.254."),
		strings.HasPrefix(ip, "127."),
		ip == "::1":
		return true
	}
	if strings.HasPrefix(ip, "172.") {
		// 172.16.0.0/12
		var second int
		if _, err := fmt.Sscanf(ip, "172.%d.", &second); err == nil && second >= 16 && second <= 31 {
			return true
		}
	}
	if strings.HasPrefix(ip, "fd") || strings.HasPrefix(ip, "fc") {
		return true
	}
	return false
}

// sortPeerProcs / sortPeers / stringsSort are tiny wrappers around
// sort.Slice to keep the ordering rules co-located with the code that
// produces them. (Tucked away here rather than at the top of the
// file because they're noise for anyone reading the ingest path.)
func sortPeerProcs(ps []netPeerProc) {
	sortFunc(len(ps), func(i, j int) bool { return ps[i].Count > ps[j].Count }, func(i, j int) {
		ps[i], ps[j] = ps[j], ps[i]
	})
}
func sortPeers(ps []netPeer) {
	sortFunc(len(ps), func(i, j int) bool {
		if ps[i].TotalConns != ps[j].TotalConns {
			return ps[i].TotalConns > ps[j].TotalConns
		}
		return ps[i].IP < ps[j].IP
	}, func(i, j int) { ps[i], ps[j] = ps[j], ps[i] })
}
func stringsSort(ss []string) {
	sortFunc(len(ss), func(i, j int) bool { return ss[i] < ss[j] }, func(i, j int) {
		ss[i], ss[j] = ss[j], ss[i]
	})
}

// sortFunc is a hand-rolled insertion sort to avoid pulling in
// "sort" as a top-of-file import. With <=200 conversations this is
// fast enough not to register on profiles.
func sortFunc(n int, less func(i, j int) bool, swap func(i, j int)) {
	for i := 1; i < n; i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			swap(j, j-1)
		}
	}
}

// parseRangeDuration accepts standard Go durations and the operator-
// friendly "<N>d" shorthand (which time.ParseDuration doesn't speak).
func parseRangeDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, errors.New("range required")
	}
	if strings.HasSuffix(s, "d") {
		num := strings.TrimSuffix(s, "d")
		var days int
		if _, err := fmt.Sscanf(num, "%d", &days); err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid range %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid range %q", s)
	}
	return d, nil
}
