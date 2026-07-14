package poller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gosnmp/gosnmp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

// TrapReceiver listens for SNMPv2c traps on UDP and opens alarms best-effort.
type TrapReceiver struct {
	addr string
	pool *pgxpool.Pool
	nc   *nats.Conn
	log  *slog.Logger
}

// NewTrapReceiver binds addr (e.g. ":162").
func NewTrapReceiver(addr string, pool *pgxpool.Pool, nc *nats.Conn, log *slog.Logger) *TrapReceiver {
	return &TrapReceiver{addr: addr, pool: pool, nc: nc, log: log}
}

// Run blocks until ctx is cancelled.
func (t *TrapReceiver) Run(ctx context.Context) error {
	tl := gosnmp.NewTrapListener()
	tl.Params = gosnmp.Default
	tl.OnNewTrap = func(pkt *gosnmp.SnmpPacket, addr *net.UDPAddr) {
		t.handleTrap(ctx, pkt, addr)
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- tl.Listen(t.addr)
	}()
	t.log.Info("SNMP trap receiver started", "addr", t.addr)
	select {
	case <-ctx.Done():
		tl.Close()
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (t *TrapReceiver) handleTrap(ctx context.Context, pkt *gosnmp.SnmpPacket, addr *net.UDPAddr) {
	if pkt == nil || addr == nil {
		return
	}
	if pkt.Version != gosnmp.Version2c && pkt.Version != gosnmp.Version1 {
		t.log.Debug("trap ignored (unsupported SNMP version)", "version", pkt.Version, "from", addr.IP)
		return
	}
	pktSrc := addr.IP.String()
	oid := ""
	if len(pkt.Variables) > 0 {
		oid = pkt.Variables[0].Name
	}
	title := fmt.Sprintf("SNMP trap from %s", pktSrc)
	if oid != "" {
		title = fmt.Sprintf("SNMP trap %s from %s", oid, pktSrc)
	}
	body := summarizeTrapVars(pkt.Variables)
	dedup := "snmp-trap:" + pktSrc + ":" + oid

	siteID, targetID := t.resolveTarget(ctx, pktSrc)
	lastVal, _ := json.Marshal(map[string]any{
		"sourceIp": pktSrc, "oid": oid, "variables": body,
	})

	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var alarmID int64
	err := t.pool.QueryRow(fctx, `
		INSERT INTO alarms (site_id, target_kind, target_id, severity, title, body, dedup_key, last_value)
		SELECT $1, 'appliance', $2, 'warning', $3, $4, $5, $6::jsonb
		WHERE NOT EXISTS (SELECT 1 FROM alarms WHERE dedup_key = $5 AND cleared_at IS NULL)
		RETURNING id`,
		siteID, targetID, title, body, dedup, string(lastVal)).Scan(&alarmID)
	if err != nil {
		t.log.Debug("trap alarm insert skipped", "err", err, "from", pktSrc)
		return
	}
	t.log.Info("trap opened alarm", "alarm_id", alarmID, "from", pktSrc)
	if t.nc != nil && t.nc.IsConnected() {
		_ = t.nc.Publish("alarm.opened", []byte(fmt.Sprintf(`{"alarmId":%d,"source":"snmp-trap"}`, alarmID)))
	}
}

func (t *TrapReceiver) resolveTarget(ctx context.Context, srcIP string) (*uuid.UUID, uuid.UUID) {
	var id, siteID uuid.UUID
	err := t.pool.QueryRow(ctx, `
		SELECT id, site_id FROM appliances WHERE host(mgmt_ip) = $1 LIMIT 1`, srcIP).
		Scan(&id, &siteID)
	if err == nil {
		return &siteID, id
	}
	_ = t.pool.QueryRow(ctx, `SELECT id FROM sites ORDER BY created_at LIMIT 1`).Scan(&siteID)
	return &siteID, uuid.Nil
}

func summarizeTrapVars(vars []gosnmp.SnmpPDU) string {
	var b strings.Builder
	for i, v := range vars {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s=%v", v.Name, v.Value)
		if b.Len() > 2000 {
			break
		}
	}
	return b.String()
}

// StartTrapReceiverIfConfigured launches the UDP trap listener when
// SONAR_SNMP_TRAP_LISTEN is set (e.g. ":162").
func StartTrapReceiverIfConfigured(ctx context.Context, pool *pgxpool.Pool, nc *nats.Conn, log *slog.Logger) {
	addr := strings.TrimSpace(os.Getenv("SONAR_SNMP_TRAP_LISTEN"))
	if addr == "" {
		return
	}
	go func() {
		r := NewTrapReceiver(addr, pool, nc, log)
		if err := r.Run(ctx); err != nil && ctx.Err() == nil {
			log.Warn("trap receiver stopped", "err", err)
		}
	}()
}
