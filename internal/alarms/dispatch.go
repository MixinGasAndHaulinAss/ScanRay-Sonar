package alarms

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NCLGISA/ScanRay-Sonar/internal/crypto"
	"github.com/NCLGISA/ScanRay-Sonar/internal/notify"
)

// AuditSink is satisfied by *db.Store; tests may substitute a recorder.
type AuditSink interface {
	Audit(ctx context.Context, actorKind, action string, actorID *uuid.UUID, ip string, metadata map[string]any)
}

// AlarmNotifyEvent carries everything needed to notify channels after an alarm row exists.
type AlarmNotifyEvent struct {
	AlarmID        int64
	RuleID         uuid.UUID
	RuleName       string
	RuleExpression string
	Severity       string
	SiteID         uuid.UUID
	TargetKind     string
	TargetID       uuid.UUID
	Title          string
	DedupKey       string
	LastValue      json.RawMessage
	ChannelIDs     []uuid.UUID
	SMTPFallback   *notify.SMTPConfig // optional env/.ini fallback when DB SMTP empty
	// Event is "alarm.opened" (default) or "alarm.cleared" — drives subject prefix
	// and webhook payload `event` field so downstream automations can react.
	Event string
}

// Fire resolves channels and sends email + webhook deliveries (best-effort).
func Fire(ctx context.Context, pool *pgxpool.Pool, store AuditSink, sealer *crypto.Sealer, log *slog.Logger, httpCli *http.Client, evt AlarmNotifyEvent) {
	if len(evt.ChannelIDs) == 0 {
		return
	}
	if httpCli == nil {
		httpCli = &http.Client{Timeout: 25 * time.Second}
	}

	channels, err := notify.LoadChannels(ctx, pool, sealer, evt.ChannelIDs)
	if err != nil {
		log.Warn("alarm notify: load channels failed", "err", err, "alarm_id", evt.AlarmID)
		return
	}
	smtpCfg, smtpErr := notify.LoadSMTP(ctx, pool, sealer)
	if smtpErr != nil {
		log.Debug("alarm notify: smtp_settings load", "err", smtpErr)
	}
	if evt.SMTPFallback != nil && !smtpCfg.Valid() {
		smtpCfg = *evt.SMTPFallback
	}

	dispatchAlarmChannels(ctx, store, log, httpCli, evt, channels, smtpCfg)
}

func dispatchAlarmChannels(ctx context.Context, store AuditSink, log *slog.Logger, httpCli *http.Client, evt AlarmNotifyEvent, channels []notify.Channel, smtpCfg notify.SMTPConfig) {
	if httpCli == nil {
		httpCli = &http.Client{Timeout: 25 * time.Second}
	}
	for _, ch := range channels {
		switch strings.ToLower(strings.TrimSpace(ch.Kind)) {
		case "email":
			sendEmailNotify(ctx, store, log, smtpCfg, ch, evt)
		case "webhook":
			sendWebhookNotify(ctx, store, httpCli, log, ch, evt)
		default:
			store.Audit(ctx, "system", "alarm.notify_failed", nil, "",
				map[string]any{"alarmId": evt.AlarmID, "channelId": ch.ID.String(), "kind": ch.Kind, "error": "unknown channel kind"})
		}
	}
}

func sendEmailNotify(ctx context.Context, store AuditSink, log *slog.Logger, smtpCfg notify.SMTPConfig, ch notify.Channel, evt AlarmNotifyEvent) {
	recipients := extractEmailRecipients(ch.Config)
	if len(recipients) == 0 {
		store.Audit(ctx, "system", "alarm.notify_failed", nil, "",
			map[string]any{"alarmId": evt.AlarmID, "channelId": ch.ID.String(), "kind": "email", "error": "no recipients in config.to"})
		return
	}
	if !smtpCfg.Valid() {
		store.Audit(ctx, "system", "alarm.notify_failed", nil, "",
			map[string]any{"alarmId": evt.AlarmID, "channelId": ch.ID.String(), "kind": "email", "error": "SMTP not configured"})
		log.Warn("alarm notify: skip email — SMTP incomplete", "channel", ch.ID)
		return
	}
	prefix := evt.Severity
	if evt.Event == "alarm.cleared" {
		prefix = "RESOLVED " + evt.Severity
	}
	subject := fmt.Sprintf("[%s] %s", prefix, evt.Title)
	body := buildAlarmEmailBody(evt)
	sendCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := notify.SendMailMsg(sendCtx, smtpCfg, recipients, subject, body); err != nil {
		log.Warn("alarm notify: email failed", "channel", ch.ID, "err", err)
		store.Audit(ctx, "system", "alarm.notify_failed", nil, "",
			map[string]any{"alarmId": evt.AlarmID, "channelId": ch.ID.String(), "kind": "email", "error": err.Error()})
		return
	}
	store.Audit(ctx, "system", "alarm.notified", nil, "",
		map[string]any{"alarmId": evt.AlarmID, "channelId": ch.ID.String(), "kind": "email", "channelName": ch.Name})
}

func sendWebhookNotify(ctx context.Context, store AuditSink, httpCli *http.Client, log *slog.Logger, ch notify.Channel, evt AlarmNotifyEvent) {
	rawURL, _ := ch.Config["url"].(string)
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		store.Audit(ctx, "system", "alarm.notify_failed", nil, "",
			map[string]any{"alarmId": evt.AlarmID, "channelId": ch.ID.String(), "kind": "webhook", "error": "missing config.url"})
		return
	}
	event := evt.Event
	if event == "" {
		event = "alarm.opened"
	}
	payload := map[string]any{
		"event":      event,
		"alarmId":    evt.AlarmID,
		"ruleId":     evt.RuleID.String(),
		"ruleName":   evt.RuleName,
		"severity":   evt.Severity,
		"siteId":     evt.SiteID.String(),
		"targetKind": evt.TargetKind,
		"targetId":   evt.TargetID.String(),
		"title":      evt.Title,
		"dedupKey":   evt.DedupKey,
		"openedAt":   time.Now().UTC().Format(time.RFC3339),
	}
	if len(evt.LastValue) > 0 {
		var lv any
		if json.Unmarshal(evt.LastValue, &lv) == nil {
			payload["lastValue"] = lv
		} else {
			payload["lastValue"] = json.RawMessage(evt.LastValue)
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		store.Audit(ctx, "system", "alarm.notify_failed", nil, "",
			map[string]any{"alarmId": evt.AlarmID, "channelId": ch.ID.String(), "kind": "webhook", "error": err.Error()})
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := notify.PostSignedJSON(sendCtx, httpCli, rawURL, ch.SigningSecret, raw); err != nil {
		log.Warn("alarm notify: webhook failed", "channel", ch.ID, "err", err)
		store.Audit(ctx, "system", "alarm.notify_failed", nil, "",
			map[string]any{"alarmId": evt.AlarmID, "channelId": ch.ID.String(), "kind": "webhook", "error": err.Error()})
		return
	}
	store.Audit(ctx, "system", "alarm.notified", nil, "",
		map[string]any{"alarmId": evt.AlarmID, "channelId": ch.ID.String(), "kind": "webhook", "channelName": ch.Name})
}

func extractEmailRecipients(cfg map[string]any) []string {
	v, ok := cfg["to"]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return nil
		}
		return []string{s}
	case []any:
		var out []string
		for _, x := range t {
			s, ok := x.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		var out []string
		for _, s := range t {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func buildAlarmEmailBody(evt AlarmNotifyEvent) string {
	var b strings.Builder
	if evt.Event == "alarm.cleared" {
		fmt.Fprintf(&b, "Sonar alarm RESOLVED\r\n\r\n")
	} else {
		fmt.Fprintf(&b, "Sonar alarm\r\n\r\n")
	}
	fmt.Fprintf(&b, "Rule: %s\r\n", evt.RuleName)
	fmt.Fprintf(&b, "Expression: %s\r\n", evt.RuleExpression)
	fmt.Fprintf(&b, "Severity: %s\r\n", evt.Severity)
	fmt.Fprintf(&b, "Site: %s\r\n", evt.SiteID)
	fmt.Fprintf(&b, "Target: %s %s\r\n", evt.TargetKind, evt.TargetID)
	fmt.Fprintf(&b, "Title: %s\r\n", evt.Title)
	fmt.Fprintf(&b, "Dedup: %s\r\n\r\n", evt.DedupKey)
	if len(evt.LastValue) > 0 {
		fmt.Fprintf(&b, "Last metric payload (JSON):\r\n%s\r\n", string(evt.LastValue))
	}
	return b.String()
}
