package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/retention"
)

func (s *Server) handleGetRetentionSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := retention.Load(r.Context(), s.pool)
	if err != nil {
		s.log.Warn("load retention settings failed", "err", err)
		writeJSON(w, http.StatusOK, retention.Defaults())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handlePutRetentionSettings(w http.ResponseWriter, r *http.Request) {
	var req retention.Settings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	req.Clamp()
	saved, err := retention.Save(r.Context(), s.pool, req)
	if err != nil {
		s.log.Warn("save retention settings failed", "err", err)
		writeErr(w, http.StatusBadRequest, "bad_request", "save failed: "+err.Error())
		return
	}
	if err := retention.Apply(r.Context(), s.pool, s.log, saved); err != nil {
		s.log.Warn("apply retention settings failed", "err", err)
		// Settings are saved; policies may catch up on the hourly runner.
	}
	uid := userIDFromCtx(r.Context())
	s.store.Audit(r.Context(), "user", "settings.retention.update", &uid, clientIP(r), map[string]any{
		"hotWindowDays":       saved.HotWindowDays,
		"rollupRetentionDays": saved.RollupRetentionDays,
		"compressAfterDays":   saved.CompressAfterDays,
		"flowHotWindowDays":   saved.FlowHotWindowDays,
		"vendorSamplesDays":   saved.VendorSamplesDays,
		"alarmsClearedDays":   saved.AlarmsClearedDays,
		"auditLogDays":        saved.AuditLogDays,
	})
	writeJSON(w, http.StatusOK, saved)
}

// loadRetention returns platform retention settings, falling back to defaults.
func (s *Server) loadRetention(r *http.Request) retention.Settings {
	cfg, err := retention.Load(r.Context(), s.pool)
	if err != nil {
		return retention.Defaults()
	}
	return cfg
}

// clampMetricRange parses range and caps it to the configured rollup horizon.
// useRollup is true when the request exceeds the hot raw window.
func (s *Server) clampMetricRange(r *http.Request, rangeStr string) (time.Duration, bool, retention.Settings, error) {
	cfg := s.loadRetention(r)
	dur, err := parseRangeDuration(rangeStr)
	if err != nil {
		return 0, false, cfg, err
	}
	max := cfg.MaxChartRange()
	if dur > max {
		dur = max
	}
	return dur, cfg.UseRollup(dur), cfg, nil
}
