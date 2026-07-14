// Package agentevents writes rows to agent_system_events.
package agentevents

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event kinds (keep in sync with migration comments / OpenAPI).
const (
	KindAlarmOpened       = "alarm.opened"
	KindAlarmCleared      = "alarm.cleared"
	KindAlarmAcked        = "alarm.acked"
	KindGroupChanged      = "group.changed"
	KindAgentOffline      = "agent.offline"
	KindAgentOnline       = "agent.online"
	KindComplianceChanged = "compliance.changed"
	KindConfigChanged     = "config.changed"
)

// Emit inserts one system-event row. Best-effort: returns error for callers
// that care; ingest paths typically log and continue.
func Emit(ctx context.Context, pool *pgxpool.Pool, siteID uuid.UUID, agentID *uuid.UUID, kind, severity, title, body string, metadata map[string]any) error {
	if severity == "" {
		severity = "info"
	}
	meta := []byte("{}")
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			meta = b
		}
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO agent_system_events (site_id, agent_id, kind, severity, title, body, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)`,
		siteID, agentID, kind, severity, title, body, string(meta))
	return err
}
