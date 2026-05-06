package alarms

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

type ruleRow struct {
	ID         uuid.UUID
	SiteID     *uuid.UUID
	Name       string
	Severity   string
	Expression string
}

// Engine evaluates inbound NATS metric payloads against enabled alarm_rules.
type Engine struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	nc   *nats.Conn

	mu    sync.RWMutex
	rules []ruleRow
}

func NewEngine(pool *pgxpool.Pool, log *slog.Logger, nc *nats.Conn) *Engine {
	return &Engine{pool: pool, log: log, nc: nc}
}

func (e *Engine) Run(ctx context.Context) {
	if e.nc == nil || !e.nc.IsConnected() {
		return
	}
	e.refreshRules(ctx)
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				e.refreshRules(context.Background())
			}
		}
	}()

	_, _ = e.nc.Subscribe("metrics.appliance", e.onMetric)
	<-ctx.Done()
}

func (e *Engine) refreshRules(ctx context.Context) {
	rows, err := e.pool.Query(ctx, `
		SELECT id, COALESCE(site_id::text,''), name, severity, expression
		  FROM alarm_rules WHERE enabled ORDER BY created_at DESC`)
	if err != nil {
		e.log.Warn("alarm rules refresh failed", "err", err)
		return
	}
	defer rows.Close()
	var next []ruleRow
	for rows.Next() {
		var r ruleRow
		var siteTxt string
		if rows.Scan(&r.ID, &siteTxt, &r.Name, &r.Severity, &r.Expression) != nil {
			continue
		}
		if siteTxt != "" {
			if u, err := uuid.Parse(siteTxt); err == nil {
				r.SiteID = &u
			}
		}
		next = append(next, r)
	}
	e.mu.Lock()
	e.rules = next
	e.mu.Unlock()
}

func (e *Engine) onMetric(msg *nats.Msg) {
	var payload map[string]any
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return
	}
	applianceID, _ := payload["applianceId"].(string)
	siteIDStr, _ := payload["siteId"].(string)
	if applianceID == "" || siteIDStr == "" {
		return
	}
	siteUUID, err := uuid.Parse(siteIDStr)
	if err != nil {
		return
	}
	apUUID, err := uuid.Parse(applianceID)
	if err != nil {
		return
	}

	cpu, _ := toFloat64(payload["cpuPct"])
	memRatio, _ := toFloat64(payload["memUsedRatio"])
	crit, _ := payload["criticality"].(string)
	env := map[string]any{
		"cpu_pct":        cpu,
		"mem_used_ratio": memRatio,
		"criticality":    crit,
		"vendor":         payload["vendor"],
	}

	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()

	for _, rl := range rules {
		if rl.SiteID != nil && *rl.SiteID != siteUUID {
			continue
		}
		ok, err := EvalMini(rl.Expression, env)
		if err != nil || !ok {
			continue
		}
		dedup := rl.ID.String() + ":appliance:" + applianceID
		var n int64
		_ = e.pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM alarms WHERE dedup_key = $1 AND cleared_at IS NULL`, dedup).Scan(&n)
		if n > 0 {
			continue
		}
		short := applianceID
		if len(short) > 8 {
			short = short[:8]
		}
		title := rl.Name + " (" + short + ")"
		_, err = e.pool.Exec(context.Background(), `
			INSERT INTO alarms (rule_id, site_id, target_kind, target_id, severity, title, dedup_key, last_value)
			VALUES ($1, $2, 'appliance', $3, $4, $5, $6, $7::jsonb)`,
			rl.ID, siteUUID, apUUID, rl.Severity, title, dedup, msg.Data)
		if err != nil {
			e.log.Warn("alarm insert failed", "err", err, "rule", rl.ID)
			continue
		}
		_ = e.nc.Publish("alarm.opened", []byte(`{"ruleId":"`+rl.ID.String()+`","applianceId":"`+applianceID+`"}`))
	}
}
