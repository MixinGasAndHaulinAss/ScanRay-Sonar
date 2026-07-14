// Package compliance evaluates endpoint posture from agent snapshots
// and maintains agent_compliance_issues / agent_vulnerabilities.
//
// CVE-lite: heuristic matches against a static in-repo map — not a
// full vulnerability scanner and not live NVD.
package compliance

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NCLGISA/ScanRay-Sonar/internal/agentevents"
)

//go:embed cve_map.json
var cveMapJSON []byte

type cveMapFile struct {
	Products []struct {
		Match string `json:"match"`
		CVEs  []struct {
			ID         string `json:"id"`
			Severity   string `json:"severity"`
			MinVersion string `json:"minVersion"`
			MaxVersion string `json:"maxVersion"`
		} `json:"cves"`
	} `json:"products"`
	KBs []struct {
		KB       string `json:"kb"`
		CVE      string `json:"cve"`
		Severity string `json:"severity"`
		Product  string `json:"product"`
	} `json:"kbs"`
}

var staticCVEMap cveMapFile

func init() {
	_ = json.Unmarshal(cveMapJSON, &staticCVEMap)
}

// SnapshotSignals are the fields the evaluator needs from last_metrics.
type SnapshotSignals struct {
	PendingReboot     bool
	MissingPatchCount int
	Patches           []PatchRef
	Apps              []AppRef
	Win11Eligible     *bool
	SecureBoot        *bool
	EDRProducts       []string
	LastSeenAge       time.Duration
	OS                string
}

type PatchRef struct {
	Title    string
	KB       string
	Severity string
}

type AppRef struct {
	Name    string
	Version string
}

type Issue struct {
	Category string
	Code     string
	Severity string
	Title    string
	Detail   string
}

type Vuln struct {
	CVEID    string
	Severity string
	Product  string
}

// Evaluate builds open issues + CVEs and a 0–100 compliance score.
func Evaluate(sig SnapshotSignals) (score float64, severity string, issues []Issue, vulns []Vuln) {
	issues = make([]Issue, 0, 8)
	vulns = make([]Vuln, 0, 4)

	if sig.PendingReboot {
		issues = append(issues, Issue{
			Category: "policy", Code: "pending_reboot", Severity: "medium",
			Title: "Pending reboot", Detail: "Host has a pending reboot flag set",
		})
	}
	if sig.MissingPatchCount > 0 {
		sev := "medium"
		if sig.MissingPatchCount >= 10 {
			sev = "high"
		}
		if hasCriticalPatch(sig.Patches) {
			sev = "critical"
		}
		issues = append(issues, Issue{
			Category: "patch", Code: "missing_patches", Severity: sev,
			Title:  "Missing patches",
			Detail: strconv.Itoa(sig.MissingPatchCount) + " outstanding update(s)",
		})
	}
	for _, p := range sig.Patches {
		if p.KB == "" {
			continue
		}
		kb := strings.ToUpper(strings.TrimSpace(p.KB))
		if !strings.HasPrefix(kb, "KB") {
			kb = "KB" + kb
		}
		for _, m := range staticCVEMap.KBs {
			if strings.EqualFold(m.KB, kb) {
				vulns = append(vulns, Vuln{CVEID: m.CVE, Severity: m.Severity, Product: m.Product})
				issues = append(issues, Issue{
					Category: "vulnerability", Code: "cve:" + m.CVE, Severity: m.Severity,
					Title: m.CVE, Detail: "Linked to missing " + kb,
				})
			}
		}
	}
	for _, app := range sig.Apps {
		for _, prod := range staticCVEMap.Products {
			if !strings.Contains(strings.ToLower(app.Name), strings.ToLower(prod.Match)) {
				continue
			}
			for _, c := range prod.CVEs {
				if versionLTE(app.Version, c.MaxVersion) {
					vulns = append(vulns, Vuln{CVEID: c.ID, Severity: c.Severity, Product: app.Name})
					issues = append(issues, Issue{
						Category: "vulnerability", Code: "cve:" + c.ID, Severity: c.Severity,
						Title: c.ID, Detail: app.Name + " " + app.Version,
					})
				}
			}
		}
	}
	if sig.Win11Eligible != nil && !*sig.Win11Eligible {
		issues = append(issues, Issue{
			Category: "misconfig", Code: "win11_not_ready", Severity: "low",
			Title: "Windows 11 readiness failed", Detail: "Host did not pass Win11 readiness checks",
		})
	}
	if sig.SecureBoot != nil && !*sig.SecureBoot {
		issues = append(issues, Issue{
			Category: "misconfig", Code: "secure_boot_off", Severity: "medium",
			Title: "Secure Boot disabled", Detail: "Secure Boot reported as not enabled",
		})
	}
	if strings.EqualFold(sig.OS, "windows") && len(sig.EDRProducts) == 0 {
		issues = append(issues, Issue{
			Category: "policy", Code: "edr_missing", Severity: "high",
			Title: "EDR not detected", Detail: "No endpoint protection product reported",
		})
	}
	if sig.LastSeenAge > 24*time.Hour {
		issues = append(issues, Issue{
			Category: "policy", Code: "agent_stale", Severity: "medium",
			Title: "Stale agent", Detail: "No heartbeat in over 24 hours",
		})
	}

	score = 100
	for _, iss := range issues {
		switch iss.Severity {
		case "critical":
			score -= 25
		case "high":
			score -= 15
		case "medium":
			score -= 8
		case "low":
			score -= 3
		default:
			score -= 1
		}
	}
	if score < 0 {
		score = 0
	}
	severity = "info"
	for _, iss := range issues {
		if sevRank(iss.Severity) > sevRank(severity) {
			severity = iss.Severity
		}
	}
	if len(issues) == 0 {
		severity = "info"
	}
	return score, severity, issues, dedupeVulns(vulns)
}

func hasCriticalPatch(patches []PatchRef) bool {
	for _, p := range patches {
		s := strings.ToLower(p.Severity)
		if s == "critical" || s == "important" {
			return true
		}
	}
	return false
}

func sevRank(s string) int {
	switch strings.ToLower(s) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func dedupeVulns(in []Vuln) []Vuln {
	seen := map[string]struct{}{}
	out := make([]Vuln, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v.CVEID]; ok {
			continue
		}
		seen[v.CVEID] = struct{}{}
		out = append(out, v)
	}
	return out
}

// versionLTE is a best-effort dotted-numeric compare (a <= b).
func versionLTE(a, b string) bool {
	as := parseVer(a)
	bs := parseVer(b)
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av < bv {
			return true
		}
		if av > bv {
			return false
		}
	}
	return true
}

func parseVer(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out = append(out, n)
	}
	return out
}

// Persist upserts open issues/CVEs, clears resolved ones, updates agent columns.
// Returns whether the open-issue fingerprint changed (for system events).
func Persist(ctx context.Context, pool *pgxpool.Pool, agentID, siteID uuid.UUID, score float64, severity string, issues []Issue, vulns []Vuln) (changed bool, err error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var prevCount int
	_ = tx.QueryRow(ctx, `SELECT compliance_issues_count FROM agents WHERE id = $1`, agentID).Scan(&prevCount)

	wantCodes := make(map[string]Issue, len(issues))
	for _, iss := range issues {
		wantCodes[iss.Code] = iss
	}
	rows, err := tx.Query(ctx, `
		SELECT id, code FROM agent_compliance_issues
		 WHERE agent_id = $1 AND cleared_at IS NULL`, agentID)
	if err != nil {
		return false, err
	}
	type openRow struct {
		id   uuid.UUID
		code string
	}
	var open []openRow
	for rows.Next() {
		var o openRow
		if rows.Scan(&o.id, &o.code) == nil {
			open = append(open, o)
		}
	}
	rows.Close()

	for _, o := range open {
		if _, keep := wantCodes[o.code]; !keep {
			_, _ = tx.Exec(ctx, `UPDATE agent_compliance_issues SET cleared_at = NOW() WHERE id = $1`, o.id)
			changed = true
		} else {
			delete(wantCodes, o.code)
		}
	}
	for _, iss := range wantCodes {
		_, err = tx.Exec(ctx, `
			INSERT INTO agent_compliance_issues
			  (agent_id, site_id, category, code, severity, title, detail)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			agentID, siteID, iss.Category, iss.Code, normalizeSev(iss.Severity), iss.Title, iss.Detail)
		if err != nil {
			return false, err
		}
		changed = true
	}

	wantCVE := make(map[string]Vuln, len(vulns))
	for _, v := range vulns {
		wantCVE[v.CVEID] = v
	}
	vrows, err := tx.Query(ctx, `
		SELECT id, cve_id FROM agent_vulnerabilities
		 WHERE agent_id = $1 AND cleared_at IS NULL`, agentID)
	if err != nil {
		return false, err
	}
	type openV struct {
		id  uuid.UUID
		cve string
	}
	var ov []openV
	for vrows.Next() {
		var o openV
		if vrows.Scan(&o.id, &o.cve) == nil {
			ov = append(ov, o)
		}
	}
	vrows.Close()
	for _, o := range ov {
		if _, keep := wantCVE[o.cve]; !keep {
			_, _ = tx.Exec(ctx, `UPDATE agent_vulnerabilities SET cleared_at = NOW() WHERE id = $1`, o.id)
			changed = true
		} else {
			delete(wantCVE, o.cve)
		}
	}
	for _, v := range wantCVE {
		_, err = tx.Exec(ctx, `
			INSERT INTO agent_vulnerabilities (agent_id, cve_id, severity, product)
			VALUES ($1, $2, $3, $4)`,
			agentID, v.CVEID, normalizeSev(v.Severity), v.Product)
		if err != nil {
			return false, err
		}
		changed = true
	}

	_, err = tx.Exec(ctx, `
		UPDATE agents
		   SET compliance_score = $2,
		       compliance_severity = $3,
		       compliance_issues_count = $4,
		       last_compliance_at = NOW()
		 WHERE id = $1`,
		agentID, score, severity, len(issues))
	if err != nil {
		return false, err
	}
	if prevCount != len(issues) {
		changed = true
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return changed, nil
}

func normalizeSev(s string) string {
	switch strings.ToLower(s) {
	case "critical", "high", "medium", "low", "info":
		return strings.ToLower(s)
	case "important":
		return "high"
	default:
		return "info"
	}
}

// EvaluateAndPersist runs Evaluate + Persist and optionally emits a system event.
func EvaluateAndPersist(ctx context.Context, pool *pgxpool.Pool, agentID, siteID uuid.UUID, sig SnapshotSignals) error {
	score, severity, issues, vulns := Evaluate(sig)
	changed, err := Persist(ctx, pool, agentID, siteID, score, severity, issues, vulns)
	if err != nil {
		return err
	}
	if changed {
		aid := agentID
		_ = agentevents.Emit(ctx, pool, siteID, &aid, agentevents.KindComplianceChanged, mapSev(severity),
			"Compliance posture changed",
			strconv.Itoa(len(issues))+" open issue(s); score "+formatScore(score),
			map[string]any{"score": score, "severity": severity, "issues": len(issues)})
	}
	return nil
}

func mapSev(s string) string {
	switch s {
	case "critical":
		return "critical"
	case "high", "medium":
		return "warning"
	default:
		return "info"
	}
}

func formatScore(s float64) string {
	return strconv.FormatFloat(s, 'f', 1, 64)
}
