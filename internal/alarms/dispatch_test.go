package alarms

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NCLGISA/ScanRay-Sonar/internal/notify"
)

type recAudit struct {
	actions []string
}

func (r *recAudit) Audit(ctx context.Context, actorKind, action string, actorID *uuid.UUID, ip string, metadata map[string]any) {
	r.actions = append(r.actions, action)
}

func TestExtractEmailRecipients(t *testing.T) {
	if got := extractEmailRecipients(nil); len(got) != 0 {
		t.Fatalf("%v", got)
	}
	if got := extractEmailRecipients(map[string]any{"to": "  x@y.com  "}); len(got) != 1 || got[0] != "x@y.com" {
		t.Fatalf("%v", got)
	}
	if got := extractEmailRecipients(map[string]any{"to": []any{"a@b", "", "c@d"}}); len(got) != 2 {
		t.Fatalf("%v", got)
	}
	if got := extractEmailRecipients(map[string]any{"to": []string{" u@v.net ", ""}}); len(got) != 1 || got[0] != "u@v.net" {
		t.Fatalf("%v", got)
	}
}

func TestDispatchWebhookSignedAndAudit(t *testing.T) {
	secret := []byte("sign-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts := r.Header.Get("X-Sonar-Timestamp")
		sig := strings.TrimPrefix(r.Header.Get("X-Sonar-Signature"), "sha256=")
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(500)
			return
		}
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(ts + "."))
		mac.Write(body)
		want := hex.EncodeToString(mac.Sum(nil))
		if sig != want {
			t.Errorf("signature mismatch got=%s want=%s", sig, want)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	rec := &recAudit{}
	ch := notify.Channel{
		ID:            uuid.New(),
		Kind:          "webhook",
		Name:          "hook",
		Config:        map[string]any{"url": srv.URL},
		SigningSecret: secret,
	}
	evt := AlarmNotifyEvent{
		AlarmID:        7,
		RuleID:         uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		RuleName:       "rule",
		RuleExpression: "device.cpu_pct > 1",
		Severity:       "warning",
		SiteID:         uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		TargetKind:     "agent",
		TargetID:       uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		Title:          "alarm title",
		DedupKey:       "dedup",
		LastValue:      json.RawMessage(`{"k":1}`),
	}

	client := srv.Client()
	dispatchAlarmChannels(context.Background(), rec, slog.Default(), client, evt, []notify.Channel{ch}, notify.SMTPConfig{})

	if len(rec.actions) != 1 || rec.actions[0] != "alarm.notified" {
		t.Fatalf("audit = %v", rec.actions)
	}
}

func TestDispatchEmailMissingSMTPAudit(t *testing.T) {
	rec := &recAudit{}
	ch := notify.Channel{
		ID:     uuid.New(),
		Kind:   "email",
		Name:   "mail",
		Config: map[string]any{"to": []any{"nobody@example.com"}},
	}
	evt := AlarmNotifyEvent{
		AlarmID:        1,
		RuleID:         uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		RuleName:       "r",
		RuleExpression: "true",
		Severity:       "info",
		SiteID:         uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		TargetKind:     "agent",
		TargetID:       uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		Title:          "t",
		DedupKey:       "dk",
	}

	dispatchAlarmChannels(context.Background(), rec, slog.Default(), nil, evt, []notify.Channel{ch}, notify.SMTPConfig{})
	if len(rec.actions) != 1 || rec.actions[0] != "alarm.notify_failed" {
		t.Fatalf("audit = %v", rec.actions)
	}
}
