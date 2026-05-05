// Package api — topology view.
//
// GET /topology assembles a graph of every managed appliance plus the
// foreign neighbors discovered via LLDP and CDP. The poller already
// stores raw neighbor rows on appliances.last_snapshot; this handler
// just denormalizes them into a node + edge model the UI can render.
//
// Edges are deduped by an unordered (sysName-A, sysName-B) pair so a
// link reported by both sides of the wire collapses into one edge —
// otherwise a 24-port-trunk between two switches would show up as 48
// parallel lines on the map.
package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp"
)

// topologyNode is one device in the graph. Managed nodes have an id +
// rich metadata; foreign nodes (everything we only learned about via
// CDP/LLDP) carry just enough to render and to attribute edges.
type topologyNode struct {
	// ID is the appliance UUID for managed nodes, or
	// "foreign:<sysname>" for unmanaged neighbors. The UI uses it as
	// a stable key for layout caching across renders.
	ID       string `json:"id"`
	Kind     string `json:"kind"`  // "appliance" | "foreign"
	Name     string `json:"name"`  // sys_name (or fallback)
	Label    string `json:"label"` // user-friendly name (appliance.name for managed)
	Vendor   string `json:"vendor,omitempty"`
	Model    string `json:"model,omitempty"`
	Platform string `json:"platform,omitempty"` // CDP cdpCachePlatform for foreign
	MgmtIP   string `json:"mgmtIp,omitempty"`
	SiteID   string `json:"siteId,omitempty"`
	// Status: "up" | "degraded" (last poll errored) | "down" (no
	// recent poll) | "unknown" (foreign - we don't poll it).
	Status      string     `json:"status"`
	LastSeenAt  *time.Time `json:"lastSeenAt,omitempty"`
	PortsTotal  *int       `json:"portsTotal,omitempty"`
	PortsUp     *int       `json:"portsUp,omitempty"`
	UplinkCount *int       `json:"uplinkCount,omitempty"`
	// Tags carry through from the appliance row so the UI can offer
	// a tag-filter dropdown over the topology. Foreign nodes never
	// have tags (we don't manage them) — the field is omitted then.
	Tags []string `json:"tags,omitempty"`
}

// topologyEdge is one discovered link between two nodes. Direction is
// always "from local-side appliance to remote neighbor" because that's
// the side we have rich port info for.
type topologyEdge struct {
	From     string `json:"from"` // node ID
	To       string `json:"to"`   // node ID
	FromPort string `json:"fromPort,omitempty"`
	ToPort   string `json:"toPort,omitempty"`
	Protocol string `json:"protocol"` // "lldp" | "cdp" | "both"
	OperUp   bool   `json:"operUp"`   // local interface oper state
}

type topologyResp struct {
	Nodes       []topologyNode `json:"nodes"`
	Edges       []topologyEdge `json:"edges"`
	GeneratedAt time.Time      `json:"generatedAt"`
}

// handleTopology builds the graph in three passes:
//  1. Pull every appliance row + its last_snapshot in one query.
//  2. Index managed appliances by sys_name (lowercased) so neighbors
//     reported under that name resolve to the existing node instead
//     of becoming a duplicate "foreign" entry.
//  3. Walk each snapshot's lldp + cdp arrays, mapping neighbor names
//     either back to a managed node or to a synthesized foreign node.
//     Edges are accumulated into a map keyed by an unordered node-pair
//     so we don't double-count when both ends report the same link.
//
// Query params:
//   - siteId — restrict to a single site
//   - includePhones=1 — include IP phones (default suppressed because
//     a single 48-port access switch can produce a hairball of 30+ phone
//     leaves that drown out the inter-switch backbone)
func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("siteId")
	includePhones := r.URL.Query().Get("includePhones") == "1"

	q := `SELECT id, site_id, name, vendor, model, host(mgmt_ip),
	             sys_name, last_polled_at, last_error,
	             phys_total_count, phys_up_count, uplink_count,
	             tags, last_snapshot
	      FROM appliances`
	args := []any{}
	if siteID != "" {
		q += ` WHERE site_id = $1`
		args = append(args, siteID)
	}

	rows, err := s.pool.Query(r.Context(), q, args...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "server_error", "topology query failed")
		return
	}
	defer rows.Close()

	type appRow struct {
		node     topologyNode
		sysName  string
		snapshot *snmp.Snapshot
	}
	var apps []appRow
	// Index managed appliances by their sys_name (the value other
	// switches will report when describing this device). Some vendors
	// uppercase the hostname in CDP/LLDP, so we normalize on lower.
	sysIndex := map[string]string{} // lower(sysName) -> node ID

	for rows.Next() {
		var (
			id, sid, name, vendor, ip  string
			model, sysName, lastErr    *string
			lastPolled                 *time.Time
			physTotal, physUp, uplinks *int
			tags                       []string
			snapBytes                  []byte
		)
		if err := rows.Scan(&id, &sid, &name, &vendor, &model, &ip,
			&sysName, &lastPolled, &lastErr,
			&physTotal, &physUp, &uplinks,
			&tags, &snapBytes); err != nil {
			writeErr(w, http.StatusInternalServerError, "server_error", "topology scan failed")
			return
		}

		status := "unknown"
		if lastPolled != nil {
			age := time.Since(*lastPolled)
			switch {
			case lastErr != nil && *lastErr != "":
				status = "degraded"
			case age <= 5*time.Minute:
				status = "up"
			case age <= 30*time.Minute:
				status = "degraded"
			default:
				status = "down"
			}
		}

		n := topologyNode{
			ID:          id,
			Kind:        "appliance",
			Name:        deref(sysName, name),
			Label:       name,
			Vendor:      vendor,
			MgmtIP:      ip,
			SiteID:      sid,
			Status:      status,
			LastSeenAt:  lastPolled,
			PortsTotal:  physTotal,
			PortsUp:     physUp,
			UplinkCount: uplinks,
			Tags:        tags,
		}
		if model != nil {
			n.Model = *model
		}

		var snap *snmp.Snapshot
		if len(snapBytes) > 0 {
			snap = &snmp.Snapshot{}
			if err := json.Unmarshal(snapBytes, snap); err != nil {
				snap = nil
			}
		}
		apps = append(apps, appRow{node: n, sysName: deref(sysName, ""), snapshot: snap})
		if sysName != nil && *sysName != "" {
			sysIndex[strings.ToLower(*sysName)] = id
		}
		// Also index by appliance display name as a fallback for
		// cases where sys_name isn't set yet (first poll never
		// completed) but neighbors might still report the configured
		// hostname.
		sysIndex[strings.ToLower(name)] = id
	}

	// Build edges. The dedupe key is an unordered pair of node IDs so
	// a link reported from both sides folds into one edge — see
	// the package comment.
	type edgeKey struct{ a, b string }
	edges := map[edgeKey]*topologyEdge{}
	// foreignNodes is request-scoped because handlers run concurrently
	// — using a package-level map here would race under load.
	foreignNodes := map[string]topologyNode{}
	addEdge := func(localID, localPort string, operUp bool, remoteSys, remotePortID, remotePlatform, remoteAddr, proto string) {
		remoteSys = strings.TrimSpace(remoteSys)
		if remoteSys == "" && remoteAddr == "" {
			return
		}
		var remoteID string
		// Cisco CDP often appends ".local" or a serial suffix; strip
		// trailing "(serial)" pattern for matching.
		clean := strings.ToLower(remoteSys)
		if i := strings.Index(clean, "("); i > 0 {
			clean = strings.TrimSpace(clean[:i])
		}
		if id, ok := sysIndex[clean]; ok {
			remoteID = id
		} else {
			// Synthesize a stable foreign-node ID. Prefer the cleaned
			// name, fall back to address — both are stable enough to
			// dedupe across multiple switches that see the same
			// neighbor.
			key := clean
			if key == "" {
				key = remoteAddr
			}
			remoteID = "foreign:" + key
		}

		k := edgeKey{a: localID, b: remoteID}
		if remoteID < localID {
			k = edgeKey{a: remoteID, b: localID}
		}
		if existing := edges[k]; existing != nil {
			// Same link reported by both sides or both protocols —
			// upgrade the protocol marker to "both" when applicable.
			if existing.Protocol != proto {
				existing.Protocol = "both"
			}
			if existing.FromPort == "" && localID == existing.From {
				existing.FromPort = localPort
			}
			if existing.ToPort == "" && remoteID == existing.To {
				existing.ToPort = remotePortID
			}
			if operUp {
				existing.OperUp = true
			}
			return
		}
		edges[k] = &topologyEdge{
			From:     localID,
			To:       remoteID,
			FromPort: localPort,
			ToPort:   remotePortID,
			Protocol: proto,
			OperUp:   operUp,
		}

		// Make sure foreign nodes exist in the response. We collect
		// them here and merge after the loop so multiple appliances
		// reporting the same neighbor share one node.
		if strings.HasPrefix(remoteID, "foreign:") {
			foreignNodes[remoteID] = topologyNode{
				ID:       remoteID,
				Kind:     "foreign",
				Name:     remoteSys,
				Label:    remoteSys,
				Platform: remotePlatform,
				MgmtIP:   remoteAddr,
				Status:   "unknown",
			}
		}
	}

	for _, a := range apps {
		if a.snapshot == nil {
			continue
		}
		// Build local-port-name lookup: ifIndex -> (name, operUp).
		type portInfo struct {
			name   string
			operUp bool
		}
		ports := make(map[int32]portInfo, len(a.snapshot.Interfaces))
		for _, ifc := range a.snapshot.Interfaces {
			ports[ifc.Index] = portInfo{name: ifc.Name, operUp: ifc.OperUp}
		}

		for _, n := range a.snapshot.LLDP {
			if !includePhones && isLLDPPhone(n) {
				continue
			}
			p := ports[n.LocalIfIndex]
			addEdge(a.node.ID, fallback(n.LocalPort, p.name), p.operUp,
				n.RemoteSysName, fallback(n.RemotePortDescr, n.RemotePortID), "", "", "lldp")
		}
		for _, n := range a.snapshot.CDP {
			if !includePhones && isCDPPhone(n) {
				continue
			}
			p := ports[n.LocalIfIndex]
			addEdge(a.node.ID, p.name, p.operUp,
				n.RemoteSysName, n.RemotePortID, n.RemotePlatform, n.RemoteAddress, "cdp")
		}
	}

	resp := topologyResp{
		Nodes:       make([]topologyNode, 0, len(apps)+len(foreignNodes)),
		Edges:       make([]topologyEdge, 0, len(edges)),
		GeneratedAt: time.Now().UTC(),
	}
	for _, a := range apps {
		resp.Nodes = append(resp.Nodes, a.node)
	}
	for _, fn := range foreignNodes {
		resp.Nodes = append(resp.Nodes, fn)
	}
	for _, e := range edges {
		resp.Edges = append(resp.Edges, *e)
	}
	// Stable order so React keys don't shuffle between polls.
	sort.Slice(resp.Nodes, func(i, j int) bool { return resp.Nodes[i].ID < resp.Nodes[j].ID })
	sort.Slice(resp.Edges, func(i, j int) bool {
		if resp.Edges[i].From != resp.Edges[j].From {
			return resp.Edges[i].From < resp.Edges[j].From
		}
		return resp.Edges[i].To < resp.Edges[j].To
	})
	writeJSON(w, http.StatusOK, resp)
}

func deref(p *string, fallbackS string) string {
	if p != nil && *p != "" {
		return *p
	}
	return fallbackS
}

func fallback(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// isLLDPPhone returns true when an LLDP neighbor row looks like an IP
// phone (or other VoIP endpoint). The reliable signal is bit 5 of
// lldpRemSysCapEnabled — the standard "telephone" capability — but
// older firmware sometimes leaves it unset, so we also pattern-match
// on the system description string. We err on the side of
// suppression because a single switch's worth of phones can easily
// dominate the topology view, and operators who really want them
// can pass ?includePhones=1.
func isLLDPPhone(n snmp.LLDP) bool {
	const telephoneCap = 0x20 // bit 5 of LldpSystemCapabilitiesMap
	if n.RemoteCaps&telephoneCap != 0 {
		return true
	}
	return looksLikePhoneString(n.RemoteSysName + " " + n.RemoteSysDescr)
}

// isCDPPhone uses cdpCacheCapabilities + the platform/version strings.
// The CDP capabilities bitmap encodes "telephone" as bit 7 (0x80) per
// CISCO-CDP-MIB, but in practice Cisco IP phones are most reliably
// identified by their platform string ("Cisco IP Phone …") so we
// check that first.
func isCDPPhone(n snmp.CDP) bool {
	const cdpTelephone = 0x80
	if n.RemoteCaps&cdpTelephone != 0 {
		return true
	}
	return looksLikePhoneString(n.RemotePlatform + " " + n.RemoteSysName + " " + n.RemoteVersion)
}

// looksLikePhoneString is the string-matching fallback for devices
// that don't advertise the telephone capability bit. We match on
// substrings rather than exact equality because vendors are wildly
// inconsistent ("Cisco IP Phone 8841", "CP-8865", "Cisco DX80",
// "Polycom VVX 411", etc.).
func looksLikePhoneString(s string) bool {
	s = strings.ToLower(s)
	switch {
	case strings.Contains(s, "ip phone"),
		strings.Contains(s, "ip-phone"),
		strings.Contains(s, "voip phone"),
		strings.Contains(s, "polycom"),
		strings.Contains(s, "yealink"),
		strings.Contains(s, "grandstream"),
		strings.Contains(s, "snom"),
		// Cisco endpoint product code prefixes seen in CDP platform
		strings.Contains(s, "cp-7"),
		strings.Contains(s, "cp-8"),
		strings.Contains(s, "cp-9"),
		strings.Contains(s, "cisco dx"):
		return true
	}
	return false
}
