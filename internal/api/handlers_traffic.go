package api

import (
	"net/http"
	"strconv"
	"time"
)

// handleTrafficFlows returns top talkers, optionally filtered by IP.
// GET /traffic/flows?ip=10.0.0.5&limit=50
func (s *Server) handleTrafficFlows(w http.ResponseWriter, r *http.Request) {
	ip := r.URL.Query().Get("ip")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	since := time.Now().UTC().Add(-1 * time.Hour)

	var rows any
	if ip != "" {
		rows = s.queryFlowsByIP(r, ip, since, limit)
	} else {
		rows = s.queryTopTalkers(r, since, limit)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"generatedAt": time.Now().UTC(),
		"flows":       rows,
	})
}

func (s *Server) queryTopTalkers(r *http.Request, since time.Time, limit int) []map[string]any {
	q := `
		SELECT src_addr::text, dst_addr::text, SUM(bytes) AS bytes, SUM(packets) AS packets
		  FROM flow_summaries
		 WHERE time >= $1
		 GROUP BY src_addr, dst_addr
		 ORDER BY bytes DESC
		 LIMIT $2`
	dbRows, err := s.pool.Query(r.Context(), q, since, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer dbRows.Close()
	return scanFlowRows(dbRows)
}

func (s *Server) queryFlowsByIP(r *http.Request, ip string, since time.Time, limit int) []map[string]any {
	q := `
		SELECT src_addr::text, dst_addr::text, SUM(bytes) AS bytes, SUM(packets) AS packets
		  FROM flow_summaries
		 WHERE time >= $1 AND (src_addr = $2::inet OR dst_addr = $2::inet)
		 GROUP BY src_addr, dst_addr
		 ORDER BY bytes DESC
		 LIMIT $3`
	dbRows, err := s.pool.Query(r.Context(), q, since, ip, limit)
	if err != nil {
		return []map[string]any{}
	}
	defer dbRows.Close()
	return scanFlowRows(dbRows)
}

type flowScanner interface {
	Next() bool
	Scan(dest ...any) error
}

func scanFlowRows(rows flowScanner) []map[string]any {
	out := []map[string]any{}
	for rows.Next() {
		var src, dst string
		var bytes, packets int64
		if rows.Scan(&src, &dst, &bytes, &packets) != nil {
			continue
		}
		out = append(out, map[string]any{
			"srcAddr": src, "dstAddr": dst, "bytes": bytes, "packets": packets,
		})
	}
	return out
}
