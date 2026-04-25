package api

import (
	"context"
	"net/http"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/version"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	type check struct {
		OK     bool   `json:"ok"`
		Detail string `json:"detail,omitempty"`
	}
	out := struct {
		Status string           `json:"status"`
		Checks map[string]check `json:"checks"`
	}{Status: "ok", Checks: map[string]check{}}

	dbOK := true
	if err := s.pool.Ping(ctx); err != nil {
		dbOK = false
		out.Checks["postgres"] = check{OK: false, Detail: err.Error()}
		out.Status = "degraded"
	} else {
		out.Checks["postgres"] = check{OK: true}
	}

	natsOK := true
	if s.nats == nil || !s.nats.IsConnected() {
		natsOK = false
		out.Checks["nats"] = check{OK: false, Detail: "not connected"}
		out.Status = "degraded"
	} else {
		out.Checks["nats"] = check{OK: true}
	}

	if !dbOK {
		writeJSON(w, http.StatusServiceUnavailable, out)
		return
	}
	_ = natsOK
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, version.Get())
}
