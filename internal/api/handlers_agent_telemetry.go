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

// parsedSnapshot mirrors just the few fields we lift into typed
// columns. It intentionally does not duplicate the full Snapshot
// struct from the probe package — keeping the shapes weakly coupled
// means a probe-side schema bump only needs an opt-in change here.
type parsedSnapshot struct {
	Host struct {
		UptimeSeconds uint64 `json:"uptimeSeconds"`
	} `json:"host"`
	CPU struct {
		UsagePct float64 `json:"usagePct"`
	} `json:"cpu"`
	Memory struct {
		TotalBytes uint64 `json:"totalBytes"`
		UsedBytes  uint64 `json:"usedBytes"`
	} `json:"memory"`
	Disks []struct {
		Mountpoint string `json:"mountpoint"`
		TotalBytes uint64 `json:"totalBytes"`
		UsedBytes  uint64 `json:"usedBytes"`
	} `json:"disks"`
	NICs []struct {
		Name      string   `json:"name"`
		Up        bool     `json:"up"`
		Addresses []string `json:"addresses"`
	} `json:"nics"`
	PendingReboot bool `json:"pendingReboot"`
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

	// $1=agent_id, $2=last_metrics(jsonb as string), $3=sentAt,
	// $4=cpu_pct, $5=mem_used, $6=mem_total, $7=root_used, $8=root_total,
	// $9=uptime_s, $10=pending_reboot, $11=primary_ip (text or NULL).
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

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
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
func pickRootDisk(disks []struct {
	Mountpoint string `json:"mountpoint"`
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}) (used, total uint64) {
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
func pickPrimaryIP(nics []struct {
	Name      string   `json:"name"`
	Up        bool     `json:"up"`
	Addresses []string `json:"addresses"`
}) string {
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
		       uptime_seconds, pending_reboot, host(primary_ip),
		       last_metrics_at, last_metrics
		  FROM agents
		 WHERE id = $1
	`
	var v agentDetailView
	var lastMetrics []byte
	var primaryIP *string
	err = s.pool.QueryRow(r.Context(), q, id).Scan(
		&v.ID, &v.SiteID, &v.Hostname, &v.Fingerprint, &v.OS, &v.OSVersion, &v.AgentVersion,
		&v.EnrolledAt, &v.LastSeenAt, &v.IsActive, &v.Tags, &v.CreatedAt,
		&v.CPUPct, &v.MemUsedBytes, &v.MemTotalBytes,
		&v.RootDiskUsedBytes, &v.RootDiskTotalBytes,
		&v.UptimeSeconds, &v.PendingReboot, &primaryIP,
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
		"agentId":  id.String(),
		"range":    dur.String(),
		"samples":  out,
		"capturedAtTo": time.Now().UTC(),
	})
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
