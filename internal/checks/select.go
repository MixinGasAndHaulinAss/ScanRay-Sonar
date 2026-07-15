package checks

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// CheckRow is the scheduling view of a checks table row.
type CheckRow struct {
	ID                  uuid.UUID
	SiteID              uuid.UUID
	Name                string
	TypeID              string
	Params              map[string]any
	IntervalSeconds     int
	PreferredRunner     string
	AssignedAgentID     *uuid.UUID
	AssignedCollectorID *uuid.UUID
	ApplianceID         *uuid.UUID
	CredentialID        *uuid.UUID
}

// OnlineAgent is a minimal agent presence signal for runner selection.
type OnlineAgent struct {
	ID       uuid.UUID
	SiteID   uuid.UUID
	LastSeen time.Time
}

// SelectRunner picks who should execute the check.
// auto → online assigned agent, else any online agent at site, else central.
// Vault-backed types are always central.
func SelectRunner(c CheckRow, agents []OnlineAgent, now time.Time) string {
	if IsCentralOnly(c.TypeID) {
		return "central"
	}
	pref := strings.ToLower(strings.TrimSpace(c.PreferredRunner))
	if pref == "" {
		pref = "auto"
	}
	online := func(id uuid.UUID) bool {
		for _, a := range agents {
			if a.ID == id && now.Sub(a.LastSeen) < 2*time.Minute {
				return true
			}
		}
		return false
	}
	siteOnline := func() *uuid.UUID {
		for _, a := range agents {
			if a.SiteID == c.SiteID && now.Sub(a.LastSeen) < 2*time.Minute {
				id := a.ID
				return &id
			}
		}
		return nil
	}

	switch pref {
	case "agent":
		if c.AssignedAgentID != nil && online(*c.AssignedAgentID) {
			return "agent"
		}
		if id := siteOnline(); id != nil {
			return "agent"
		}
		return "central" // fall back so the check still runs
	case "collector":
		return "collector"
	case "central":
		return "central"
	default: // auto
		if c.AssignedAgentID != nil && online(*c.AssignedAgentID) {
			return "agent"
		}
		if id := siteOnline(); id != nil {
			return "agent"
		}
		return "central"
	}
}
