package alarms

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/NCLGISA/ScanRay-Sonar/internal/crypto"
	"github.com/NCLGISA/ScanRay-Sonar/internal/db"
	"github.com/NCLGISA/ScanRay-Sonar/internal/notify"
)

type ruleRow struct {
	ID              uuid.UUID
	SiteID          *uuid.UUID
	Name            string
	Severity        string
	Expression      string
	ChannelIDs      []uuid.UUID
	ForSeconds      int
	ClearForSeconds int
}

// metricEval bundles NATS metric payload fields shared by appliance + agent streams.
type metricEval struct {
	siteID      uuid.UUID
	targetKind  string // appliance | agent
	targetUUID  uuid.UUID
	targetKey   string // UUID string for dedup suffix
	env         map[string]any
	payloadJSON []byte
}

// pendingState tracks per-(rule,target) hysteresis: when did the predicate
// first turn truthy/falsy continuously? The engine uses this to delay opening
// or closing an alarm until the for_seconds / clear_for_seconds window is met.
type pendingState struct {
	truthySince time.Time // zero when predicate is currently falsy
	falsySince  time.Time // zero when predicate is currently truthy (and an alarm is open)
}

// Engine evaluates inbound NATS metric payloads against enabled alarm_rules.
type Engine struct {
	pool         *pgxpool.Pool
	log          *slog.Logger
	nc           *nats.Conn
	sealer       *crypto.Sealer
	store        *db.Store
	httpCli      *http.Client
	smtpFallback *notify.SMTPConfig

	mu      sync.RWMutex
	rules   []ruleRow
	pending map[string]*pendingState // key = dedup_key
}

func NewEngine(pool *pgxpool.Pool, log *slog.Logger, nc *nats.Conn, sealer *crypto.Sealer, store *db.Store, smtpFallback *notify.SMTPConfig) *Engine {
	cli := &http.Client{Timeout: 30 * time.Second}
	return &Engine{
		pool:         pool,
		log:          log,
		nc:           nc,
		sealer:       sealer,
		store:        store,
		httpCli:      cli,
		smtpFallback: smtpFallback,
		pending:      make(map[string]*pendingState),
	}
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

	_, _ = e.nc.Subscribe("metrics.appliance", e.onApplianceMetric)
	_, _ = e.nc.Subscribe("metrics.agent", e.onAgentMetric)
	<-ctx.Done()
}

func (e *Engine) refreshRules(ctx context.Context) {
	rows, err := e.pool.Query(ctx, `
		SELECT id, COALESCE(site_id::text,''), name, severity, expression,
		       COALESCE(channel_ids, '{}'),
		       COALESCE(for_seconds, 0), COALESCE(clear_for_seconds, 0)
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
		if rows.Scan(&r.ID, &siteTxt, &r.Name, &r.Severity, &r.Expression, &r.ChannelIDs, &r.ForSeconds, &r.ClearForSeconds) != nil {
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

func (e *Engine) onApplianceMetric(msg *nats.Msg) {
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
		"cpuPct":         cpu,
		"memUsedRatio":   memRatio,
		"criticality":    crit,
		"vendor":         payload["vendor"],
	}
	e.evaluate(metricEval{
		siteID:      siteUUID,
		targetKind:  "appliance",
		targetUUID:  apUUID,
		targetKey:   applianceID,
		env:         env,
		payloadJSON: append(json.RawMessage(nil), msg.Data...),
	})
}

func (e *Engine) onAgentMetric(msg *nats.Msg) {
	var payload map[string]any
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return
	}
	agentIDStr, _ := payload["agentId"].(string)
	siteIDStr, _ := payload["siteId"].(string)
	if agentIDStr == "" || siteIDStr == "" {
		return
	}
	siteUUID, err := uuid.Parse(siteIDStr)
	if err != nil {
		return
	}
	agentUUID, err := uuid.Parse(agentIDStr)
	if err != nil {
		return
	}
	cpu, _ := toFloat64(payload["cpuPct"])
	memRatio, _ := toFloat64(payload["memUsedRatio"])
	crit, _ := payload["criticality"].(string)
	env := map[string]any{
		"cpu_pct":        cpu,
		"mem_used_ratio": memRatio,
		"cpuPct":         cpu,
		"memUsedRatio":   memRatio,
		"criticality":    crit,
		"vendor":         payload["vendor"],
	}
	e.evaluate(metricEval{
		siteID:      siteUUID,
		targetKind:  "agent",
		targetUUID:  agentUUID,
		targetKey:   agentIDStr,
		env:         env,
		payloadJSON: append(json.RawMessage(nil), msg.Data...),
	})
}

// evaluate is the unified open/close path. Predicate truthy beyond for_seconds
// → open new alarm + dispatch. Predicate falsy beyond clear_for_seconds →
// auto-close any open alarm + dispatch resolved notification.
func (e *Engine) evaluate(m metricEval) {
	e.mu.RLock()
	rules := e.rules
	e.mu.RUnlock()

	now := time.Now()
	for _, rl := range rules {
		if rl.SiteID != nil && *rl.SiteID != m.siteID {
			continue
		}
		truthy, err := EvalMini(rl.Expression, m.env)
		if err != nil {
			continue
		}
		dedup := rl.ID.String() + ":" + m.targetKind + ":" + m.targetKey
		st := e.pendingFor(dedup)
		if truthy {
			st.falsySince = time.Time{}
			if st.truthySince.IsZero() {
				st.truthySince = now
			}
			if now.Sub(st.truthySince) < time.Duration(rl.ForSeconds)*time.Second {
				continue // still inside for-window
			}
			e.maybeOpen(m, rl, dedup)
		} else {
			st.truthySince = time.Time{}
			if st.falsySince.IsZero() {
				st.falsySince = now
			}
			if now.Sub(st.falsySince) < time.Duration(rl.ClearForSeconds)*time.Second {
				continue
			}
			e.maybeAutoClear(m, rl, dedup)
		}
	}
}

func (e *Engine) pendingFor(key string) *pendingState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s, ok := e.pending[key]; ok {
		return s
	}
	s := &pendingState{}
	e.pending[key] = s
	return s
}

func (e *Engine) maybeOpen(m metricEval, rl ruleRow, dedup string) {
	var n int64
	_ = e.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM alarms WHERE dedup_key = $1 AND cleared_at IS NULL`, dedup).Scan(&n)
	if n > 0 {
		return
	}
	short := m.targetKey
	if len(short) > 8 {
		short = short[:8]
	}
	title := rl.Name + " (" + short + ")"

	var alarmID int64
	err := e.pool.QueryRow(context.Background(), `
		INSERT INTO alarms (rule_id, site_id, target_kind, target_id, severity, title, dedup_key, last_value)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb) RETURNING id`,
		rl.ID, m.siteID, m.targetKind, m.targetUUID, rl.Severity, title, dedup, m.payloadJSON).
		Scan(&alarmID)
	if err != nil {
		e.log.Warn("alarm insert failed", "err", err, "rule", rl.ID)
		return
	}

	pub := map[string]any{"ruleId": rl.ID.String(), "alarmId": alarmID}
	switch m.targetKind {
	case "appliance":
		pub["applianceId"] = m.targetKey
	case "agent":
		pub["agentId"] = m.targetKey
	}
	if b, err := json.Marshal(pub); err == nil {
		_ = e.nc.Publish("alarm.opened", b)
	}

	go e.dispatchAlarm(alarmID, rl, m.siteID, m.targetKind, m.targetUUID, title, dedup, m.payloadJSON, "alarm.opened")
}

func (e *Engine) maybeAutoClear(m metricEval, rl ruleRow, dedup string) {
	var alarmID int64
	err := e.pool.QueryRow(context.Background(), `
		UPDATE alarms
		   SET cleared_at = NOW(), auto_cleared = TRUE
		 WHERE dedup_key = $1 AND cleared_at IS NULL
		 RETURNING id`, dedup).Scan(&alarmID)
	if err != nil {
		// No row to clear (most common path) — silent.
		return
	}
	if e.nc != nil && e.nc.IsConnected() {
		_ = e.nc.Publish("alarm.cleared", []byte(`{"alarmId":`+strconv.FormatInt(alarmID, 10)+`,"auto":true}`))
	}
	short := m.targetKey
	if len(short) > 8 {
		short = short[:8]
	}
	title := rl.Name + " (" + short + ")"
	go e.dispatchAlarm(alarmID, rl, m.siteID, m.targetKind, m.targetUUID, title, dedup, m.payloadJSON, "alarm.cleared")
}

func (e *Engine) dispatchAlarm(alarmID int64, rl ruleRow, siteID uuid.UUID, targetKind string, targetID uuid.UUID, title, dedup string, lastValue json.RawMessage, event string) {
	bg, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	Fire(bg, e.pool, e.store, e.sealer, e.log, e.httpCli, AlarmNotifyEvent{
		AlarmID:        alarmID,
		RuleID:         rl.ID,
		RuleName:       rl.Name,
		RuleExpression: rl.Expression,
		Severity:       rl.Severity,
		SiteID:         siteID,
		TargetKind:     targetKind,
		TargetID:       targetID,
		Title:          title,
		DedupKey:       dedup,
		LastValue:      lastValue,
		ChannelIDs:     rl.ChannelIDs,
		SMTPFallback:   e.smtpFallback,
		Event:          event,
	})
}
