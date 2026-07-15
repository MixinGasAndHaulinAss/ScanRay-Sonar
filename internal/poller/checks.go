package poller

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/NCLGISA/ScanRay-Sonar/internal/checks"
)

// StartCheckScheduler launches the additive synthetic-check loop.
func (s *Scheduler) StartCheckScheduler(ctx context.Context) {
	go s.runChecks(ctx)
}

func (s *Scheduler) runChecks(ctx context.Context) {
	s.log.Info("check scheduler starting")
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	s.tickChecks(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.tickChecks(ctx)
		}
	}
}

func (s *Scheduler) tickChecks(ctx context.Context) {
	agents := listOnlineAgents(ctx, s.pool)
	rows, err := s.pool.Query(ctx, `
		SELECT id, site_id, name, type_id, params, interval_seconds, preferred_runner,
		       assigned_agent_id, assigned_collector_id, appliance_id,
		       COALESCE(last_run_at, TIMESTAMPTZ 'epoch')
		  FROM checks
		 WHERE enabled = TRUE`)
	if err != nil {
		s.log.Warn("checks list failed", "err", err)
		return
	}
	defer rows.Close()

	now := time.Now()
	for rows.Next() {
		var (
			c           checks.CheckRow
			paramsRaw   []byte
			lastRun     time.Time
			agentID     *uuid.UUID
			collectorID *uuid.UUID
			applianceID *uuid.UUID
		)
		if err := rows.Scan(&c.ID, &c.SiteID, &c.Name, &c.TypeID, &paramsRaw, &c.IntervalSeconds,
			&c.PreferredRunner, &agentID, &collectorID, &applianceID, &lastRun); err != nil {
			continue
		}
		c.AssignedAgentID = agentID
		c.AssignedCollectorID = collectorID
		c.ApplianceID = applianceID
		_ = json.Unmarshal(paramsRaw, &c.Params)
		if c.Params == nil {
			c.Params = map[string]any{}
		}
		interval := time.Duration(c.IntervalSeconds) * time.Second
		if interval < 15*time.Second {
			interval = 15 * time.Second
		}
		if now.Sub(lastRun) < interval {
			continue
		}
		if checks.SelectRunner(c, agents, now) == "agent" {
			continue
		}
		s.executeCheck(ctx, c, "central")
	}
}

func listOnlineAgents(ctx context.Context, pool *pgxpool.Pool) []checks.OnlineAgent {
	rows, err := pool.Query(ctx, `
		SELECT id, site_id, COALESCE(last_seen_at, TIMESTAMPTZ 'epoch')
		  FROM agents WHERE is_active = TRUE`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []checks.OnlineAgent
	for rows.Next() {
		var a checks.OnlineAgent
		if rows.Scan(&a.ID, &a.SiteID, &a.LastSeen) == nil {
			out = append(out, a)
		}
	}
	return out
}

func (s *Scheduler) executeCheck(ctx context.Context, c checks.CheckRow, runner string) {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return
	}
	rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res := checks.Run(rctx, c.TypeID, c.Params)
	if err := PersistCheckResult(ctx, s.pool, s.nc, c, res, runner); err != nil {
		s.log.Warn("check persist failed", "check_id", c.ID, "err", err)
	}
}

// PersistCheckResult writes samples, updates the check row, and publishes metrics.check.
func PersistCheckResult(ctx context.Context, pool *pgxpool.Pool, nc *nats.Conn, c checks.CheckRow, res checks.Result, runner string) error {
	now := time.Now().UTC()
	for _, sm := range res.Samples {
		var vd *float64
		var vt *string
		if sm.HasNum {
			v := sm.Value
			vd = &v
		}
		if sm.Text != "" {
			t := sm.Text
			vt = &t
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO check_samples (check_id, time, channel_key, value_double, value_text, runner, ok)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			c.ID, now, sm.Key, vd, vt, runner, res.OK); err != nil {
			return err
		}
	}
	errMsg := res.Error
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	if _, err := pool.Exec(ctx, `
		UPDATE checks SET last_run_at = $2, last_ok = $3, last_error = NULLIF($4,''), updated_at = NOW()
		 WHERE id = $1`, c.ID, now, res.OK, errMsg); err != nil {
		return err
	}
	if nc == nil || !nc.IsConnected() {
		return nil
	}
	payload := map[string]any{
		"checkId": c.ID.String(),
		"siteId":  c.SiteID.String(),
		"typeId":  c.TypeID,
		"name":    c.Name,
		"runner":  runner,
		"ok":      res.OK,
	}
	for k, v := range checks.FlatFields(c.TypeID, res) {
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return nc.Publish("metrics.check", b)
}
