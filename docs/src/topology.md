# Topology

**Topology** shows how appliances connect—LLDP/CDP neighbors, Meraki Dashboard switch topology, WAN uplinks to the Internet, and Auto VPN / third-party VPN peers.

## How to use it

1. Open **Topology** (fleet-wide) or a site’s **network map**.
2. Drag nodes to rearrange; use **+/- / Fit / Reset** and scroll-wheel zoom.
3. Click an appliance bubble to open its detail page (Internet and foreign peers are not clickable destinations).

## Filters

- **Show IP phones** — off by default so access-switch phone leaves do not drown the backbone.
- **Role chips** — quick toggles for common Meraki tags (`firewall`, `switch`, `wap`, `meraki`, …).
- **Tag filter** with **AND / OR**:
  - AND — appliance must have every selected tag.
  - OR — appliance must have any selected tag (use this when selecting multiple roles).
- Foreign neighbors and the Internet cloud stay visible when they connect to a kept appliance.

Preferences persist in the browser (tags, AND/OR, phones).

## What the edges mean

| Kind | Source | Appearance |
|------|--------|------------|
| L2 LLDP/CDP | SNMP snapshots; Meraki switch topology | Thicker stroke; util colors when IF-MIB rates exist |
| WAN uplink | Meraki MX/Z uplinks; SNMP oper-up tunnel interfaces (stub) | Amber line to **Internet** cloud; labeled `wan1` / tunnel name |
| Meraki Auto VPN | Meraki `vpn.merakiPeers` | Dashed purple; resolved to another managed MX when the peer network name matches |
| Third-party VPN | Meraki third-party peers | Dashed fuchsia to a foreign peer node |

OSPF/BGP adjacency collection is still future work; those legend entries are placeholders.

## Empty or sparse graphs

- SNMP switches need `lldp run` / `cdp run` (or equivalent) and a successful poll.
- Meraki appliances need Dashboard sync + telemetry so `last_snapshot` is `meraki-2` (neighbors, uplinks, VPN).
- Tag AND with conflicting role tags (`firewall` AND `switch`) yields an empty set—switch to OR or clear filters.

## Site network map

Same engine, limited to one site’s appliances. Filters and WAN/VPN edges work the same way.
