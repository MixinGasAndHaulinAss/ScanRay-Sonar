package poller

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NCLGISA/ScanRay-Sonar/internal/flows"
)

// StartFlowListenerIfConfigured launches NetFlow/IPFIX listener when
// SONAR_FLOW_LISTEN is set (e.g. ":2055").
func StartFlowListenerIfConfigured(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	addr := strings.TrimSpace(os.Getenv("SONAR_FLOW_LISTEN"))
	if addr == "" {
		return
	}
	go func() {
		l := flows.NewListener(addr, pool, log)
		if err := l.Run(ctx); err != nil && ctx.Err() == nil {
			log.Warn("flow listener stopped", "err", err)
		}
	}()
}
