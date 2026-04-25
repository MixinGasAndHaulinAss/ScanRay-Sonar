package auth

// Role is a coarse-grained permission tier. Phase 1 keeps this simple
// and string-based; Phase 6 will introduce per-site role bindings via
// a relational table without changing this enum.
type Role string

const (
	RoleSuperAdmin Role = "superadmin" // global; can manage users, sites, billing
	RoleSiteAdmin  Role = "siteadmin"  // can manage agents, appliances, alerts within their sites
	RoleTech       Role = "tech"       // can view everything, ack alerts, run on-demand checks
	RoleReadOnly   Role = "readonly"   // can view dashboards only
)

// Valid reports whether r is a known role string.
func (r Role) Valid() bool {
	switch r {
	case RoleSuperAdmin, RoleSiteAdmin, RoleTech, RoleReadOnly:
		return true
	}
	return false
}

// rank gives each role a comparable level so AtLeast is O(1).
func (r Role) rank() int {
	switch r {
	case RoleSuperAdmin:
		return 4
	case RoleSiteAdmin:
		return 3
	case RoleTech:
		return 2
	case RoleReadOnly:
		return 1
	}
	return 0
}

// AtLeast reports whether r meets or exceeds the required tier.
// Use this in middleware/handlers; never compare role strings directly.
func (r Role) AtLeast(required Role) bool {
	return r.rank() >= required.rank()
}
