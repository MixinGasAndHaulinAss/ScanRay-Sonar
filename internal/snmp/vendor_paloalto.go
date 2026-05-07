// Package snmp — Palo Alto VM/PAN-COMMON-MIB session-table collector.
//
// PAN-COMMON-MIB lives under .1.3.6.1.4.1.25461. The session table
// scalars are at .25461.2.1.2.3.{1,2,3,4}.0:
//
//	1 = panSessionActive       (current sessions)
//	2 = panSessionMax          (capacity)
//	3 = panSessionActiveTcp
//	4 = panSessionActiveUdp
//
// Utilization is computed locally; PAN-OS doesn't expose it directly.
package snmp

import "context"

const (
	oidPanSessionActive    = "1.3.6.1.4.1.25461.2.1.2.3.1.0"
	oidPanSessionMax       = "1.3.6.1.4.1.25461.2.1.2.3.2.0"
	oidPanSessionActiveTCP = "1.3.6.1.4.1.25461.2.1.2.3.3.0"
	oidPanSessionActiveUDP = "1.3.6.1.4.1.25461.2.1.2.3.4.0"
)

// CollectPaloAlto fetches the session-table scalars and computes the
// utilization percentage. Returns nil if all fields came back empty.
func CollectPaloAlto(_ context.Context, c *Client) *PaloAltoHealth {
	res, err := c.Get([]string{
		oidPanSessionActive,
		oidPanSessionMax,
		oidPanSessionActiveTCP,
		oidPanSessionActiveUDP,
	})
	if err != nil || len(res) == 0 {
		return nil
	}
	h := &PaloAltoHealth{}
	if v, ok := res[oidPanSessionActive]; ok {
		if n, ok2 := v.Int64(); ok2 {
			h.SessionActive = &n
		}
	}
	if v, ok := res[oidPanSessionMax]; ok {
		if n, ok2 := v.Int64(); ok2 {
			h.SessionMax = &n
		}
	}
	if v, ok := res[oidPanSessionActiveTCP]; ok {
		if n, ok2 := v.Int64(); ok2 {
			h.SessionActiveTcp = &n
		}
	}
	if v, ok := res[oidPanSessionActiveUDP]; ok {
		if n, ok2 := v.Int64(); ok2 {
			h.SessionActiveUdp = &n
		}
	}
	if h.SessionActive != nil && h.SessionMax != nil && *h.SessionMax > 0 {
		pct := 100.0 * float64(*h.SessionActive) / float64(*h.SessionMax)
		h.SessionUtilPct = &pct
	}
	return h
}
