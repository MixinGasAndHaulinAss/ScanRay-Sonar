package api

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NCLGISA/ScanRay-Sonar/internal/agentevents"
)

const agentOfflineAfter = 5 * time.Minute

// runAgentPresenceWatcher emits agent.offline system events when last_seen
// goes stale. Dedupes via metadata so we don't spam every tick.
func (s *Server) runAgentPresenceWatcher(ctx context.Context) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.emitStaleAgentOffline(ctx)
		}
	}
}

func (s *Server) emitStaleAgentOffline(ctx context.Context) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.site_id, a.hostname
		  FROM agents a
		 WHERE a.is_active
		   AND a.last_seen_at IS NOT NULL
		   AND a.last_seen_at < NOW() - $1::interval
		   AND NOT EXISTS (
		     SELECT 1 FROM agent_system_events e
		      WHERE e.agent_id = a.id
		        AND e.kind = $2
		        AND e.time > a.last_seen_at
		   )
		 LIMIT 200`,
		"5 minutes", agentevents.KindAgentOffline)
	if err != nil {
		s.log.Debug("agent offline sweep failed", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id, siteID uuid.UUID
		var host string
		if rows.Scan(&id, &siteID, &host) != nil {
			continue
		}
		aid := id
		_ = agentevents.Emit(ctx, s.pool, siteID, &aid, agentevents.KindAgentOffline, "warning",
			"Agent offline", host+" has not reported in "+agentOfflineAfter.String(),
			map[string]any{"hostname": host})
	}
}
