package api

import (
	"encoding/json"
	"strings"

	"github.com/NCLGISA/ScanRay-Sonar/internal/poller"
	"github.com/NCLGISA/ScanRay-Sonar/internal/snmp"
)

const topologyInternetID = "cloud:internet"

func topologyWANKind(iface string) map[string]any {
	return map[string]any{
		"layer":     float64(3),
		"protocol":  "uplink",
		"medium":    "wan",
		"interface": iface,
	}
}

func topologyVPNKind(proto string) map[string]any {
	return map[string]any{
		"layer":    float64(3),
		"protocol": strings.ToLower(proto),
		"medium":   "vpn",
	}
}

func isMerakiSnapshot(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var peek struct {
		SchemaVersion string `json:"schemaVersion"`
		Source        string `json:"source"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return false
	}
	if strings.EqualFold(peek.Source, "meraki-dashboard") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(peek.SchemaVersion), "meraki")
}

func parseMerakiSnapshot(raw []byte) *poller.MerakiTelemetrySnapshot {
	if len(raw) == 0 {
		return nil
	}
	var snap poller.MerakiTelemetrySnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil
	}
	return &snap
}

// merakiNeighborRemote extracts a usable remote sysname from a Meraki
// topology discovery summary (often "hostname - port" or freeform).
func merakiNeighborRemote(summary string) string {
	s := strings.TrimSpace(summary)
	if s == "" {
		return ""
	}
	// Prefer the token before " - " / " — " when present.
	for _, sep := range []string{" — ", " - ", " | "} {
		if i := strings.Index(s, sep); i > 0 {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

func uplinkOperUp(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))
	switch s {
	case "active", "ready", "connected", "online", "up":
		return true
	case "failed", "not connected", "offline", "down", "disabled":
		return false
	default:
		// Meraki sometimes uses "active" / "failed"; treat unknown non-empty as up
		// only when it clearly isn't failed.
		return s != "" && !strings.Contains(s, "fail") && !strings.Contains(s, "down")
	}
}

func vpnPeerReachable(reachability string) bool {
	return strings.EqualFold(strings.TrimSpace(reachability), "reachable")
}

// ensureInternetCloud adds the synthetic Internet node when missing.
func ensureInternetCloud(foreignNodes map[string]topologyNode) {
	if _, ok := foreignNodes[topologyInternetID]; ok {
		return
	}
	foreignNodes[topologyInternetID] = topologyNode{
		ID:     topologyInternetID,
		Kind:   "cloud",
		Name:   "Internet",
		Label:  "Internet",
		Status: "up",
	}
}

// addMerakiL2Neighbors walks Meraki Dashboard neighbor rows into the same
// L2 edge path used for SNMP LLDP/CDP.
func addMerakiL2Neighbors(
	localID string,
	snap *poller.MerakiTelemetrySnapshot,
	includePhones bool,
	addEdge func(localID, localPort string, operUp bool, inBps, outBps *uint64, speedBps uint64, remoteSys, remotePortID, remotePlatform, remoteAddr, proto string),
) {
	if snap == nil {
		return
	}
	seen := map[string]bool{}
	for _, n := range snap.Neighbors {
		remote := merakiNeighborRemote(n.Summary)
		if remote == "" {
			continue
		}
		if !includePhones && looksLikePhoneString(remote+" "+n.Summary) {
			continue
		}
		proto := strings.ToLower(n.Protocol)
		if proto != "cdp" {
			proto = "lldp"
		}
		key := proto + "|" + n.PortID + "|" + strings.ToLower(remote)
		if seen[key] {
			continue
		}
		seen[key] = true
		addEdge(localID, n.PortID, true, nil, nil, 0, remote, "", "", "", proto)
	}
	// Port-embedded neighbor strings as a fallback when Neighbors[] is empty.
	for _, p := range snap.Ports {
		if p.Neighbor == "" {
			continue
		}
		remote := merakiNeighborRemote(p.Neighbor)
		if remote == "" {
			continue
		}
		if !includePhones && looksLikePhoneString(remote+" "+p.Neighbor) {
			continue
		}
		proto := "lldp"
		if len(p.LLDP) == 0 && len(p.CDP) > 0 {
			proto = "cdp"
		}
		key := proto + "|" + p.PortID + "|" + strings.ToLower(remote)
		if seen[key] {
			continue
		}
		seen[key] = true
		var inBps, outBps *uint64
		inBps, outBps = p.InBps, p.OutBps
		addEdge(localID, p.PortID, strings.EqualFold(p.Status, "Connected") || p.Enabled, inBps, outBps, 0, remote, "", "", "", proto)
	}
}

// addMerakiWANAndVPN emits Internet/WAN and Auto VPN / third-party VPN edges.
func addMerakiWANAndVPN(
	localID string,
	snap *poller.MerakiTelemetrySnapshot,
	sysIndex map[string]string,
	edges map[edgeKey]*topologyEdge,
	foreignNodes map[string]topologyNode,
) {
	if snap == nil {
		return
	}
	for _, u := range snap.Uplinks {
		iface := strings.TrimSpace(u.Interface)
		if iface == "" {
			continue
		}
		ensureInternetCloud(foreignNodes)
		family := "wan:" + strings.ToLower(iface)
		a, b := localID, topologyInternetID
		if b < a {
			a, b = b, a
		}
		k := edgeKey{a: a, b: b, family: family}
		edges[k] = &topologyEdge{
			From:     localID,
			To:       topologyInternetID,
			FromPort: iface,
			Protocol: "uplink",
			OperUp:   uplinkOperUp(u.Status),
			LinkKind: topologyWANKind(iface),
		}
	}
	if snap.VPN == nil {
		return
	}
	for _, p := range snap.VPN.MerakiPeers {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		remoteID := resolveVPNPeerID(name, "", sysIndex, foreignNodes, false)
		family := "vpn:meraki:" + strings.ToLower(name)
		putVPNEdge(edges, localID, remoteID, name, "meraki-autovpn", family, vpnPeerReachable(p.Reachability))
	}
	for _, p := range snap.VPN.ThirdPartyPeers {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			name = strings.TrimSpace(p.PublicIP)
		}
		if name == "" {
			continue
		}
		remoteID := resolveVPNPeerID(name, p.PublicIP, sysIndex, foreignNodes, true)
		family := "vpn:3p:" + strings.ToLower(name)
		putVPNEdge(edges, localID, remoteID, name, "third-party-vpn", family, vpnPeerReachable(p.Reachability))
	}
}

func resolveVPNPeerID(name, publicIP string, sysIndex map[string]string, foreignNodes map[string]topologyNode, thirdParty bool) string {
	clean := strings.ToLower(strings.TrimSpace(name))
	if id, ok := sysIndex[clean]; ok {
		return id
	}
	// Soft match: peer network name contained in indexed names or vice versa.
	for k, id := range sysIndex {
		if k == "" {
			continue
		}
		if strings.Contains(k, clean) || strings.Contains(clean, k) {
			return id
		}
	}
	key := clean
	if key == "" {
		key = strings.ToLower(publicIP)
	}
	prefix := "foreign:vpn:"
	if thirdParty {
		prefix = "foreign:vpn3p:"
	}
	remoteID := prefix + key
	if _, ok := foreignNodes[remoteID]; !ok {
		label := name
		if label == "" {
			label = publicIP
		}
		foreignNodes[remoteID] = topologyNode{
			ID:     remoteID,
			Kind:   "foreign",
			Name:   label,
			Label:  label,
			MgmtIP: publicIP,
			Status: "unknown",
		}
	}
	return remoteID
}

func putVPNEdge(edges map[edgeKey]*topologyEdge, localID, remoteID, fromPort, proto, family string, operUp bool) {
	if localID == remoteID {
		return
	}
	a, b := localID, remoteID
	if b < a {
		a, b = b, a
	}
	k := edgeKey{a: a, b: b, family: family}
	if existing := edges[k]; existing != nil {
		if operUp {
			existing.OperUp = true
		}
		return
	}
	edges[k] = &topologyEdge{
		From:     localID,
		To:       remoteID,
		FromPort: fromPort,
		Protocol: proto,
		OperUp:   operUp,
		LinkKind: topologyVPNKind(proto),
	}
}

// addSNMPTunnelStubs links appliances with oper-up tunnel interfaces to Internet.
func addSNMPTunnelStubs(
	localID string,
	snap *snmp.Snapshot,
	edges map[edgeKey]*topologyEdge,
	foreignNodes map[string]topologyNode,
) {
	if snap == nil {
		return
	}
	hasTunnel := false
	var tunnelName string
	for _, ifc := range snap.Interfaces {
		if !strings.EqualFold(ifc.Kind, "tunnel") || !ifc.OperUp {
			continue
		}
		hasTunnel = true
		tunnelName = ifc.Name
		break
	}
	if !hasTunnel {
		return
	}
	ensureInternetCloud(foreignNodes)
	family := "wan:snmp-tunnel"
	a, b := localID, topologyInternetID
	if b < a {
		a, b = b, a
	}
	k := edgeKey{a: a, b: b, family: family}
	if edges[k] != nil {
		return
	}
	edges[k] = &topologyEdge{
		From:     localID,
		To:       topologyInternetID,
		FromPort: tunnelName,
		Protocol: "uplink",
		OperUp:   true,
		LinkKind: topologyWANKind(fallback(tunnelName, "tunnel")),
	}
}
