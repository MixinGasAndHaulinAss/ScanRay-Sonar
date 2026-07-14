# Site discovery

Per-site discovery lives at **Sites → (site) → Discovery** (`/sites/:siteId/discovery`). Requires **siteadmin**.

There are three tabs: **Manage credentials**, **Settings**, and **Meraki**.

## Manage credentials

The credential vault stores secrets encrypted at rest. Collectors unseal them when jobs run. Prefer editing a credential to rotate secrets rather than deleting and recreating when possible.

| Kind | Use for |
|------|---------|
| **SNMP** | v2c community or v3 user for polling and discovery |
| **SSH login** | CLI access for discovery/backup style jobs |
| **Telnet login** | Legacy CLI (prefer SSH when available) |
| **WMI** | Windows WMI queries (prefer WinAgent where possible) |
| **WinAgent** | Privileged Windows agent credentials |
| **VMware** | vCenter / ESXi via HTTPS REST during discovery |
| **Device API** | Generic HTTPS device APIs |
| **Meraki** | Dashboard API key for org inventory and telemetry |

Never put long-lived secrets in [site documents](documents.md)—use this vault.

## Settings

Scan configuration for the site, including intervals such as:

- **Scan interval (seconds)** — how often discovery sweeps run (minimums enforced in UI)
- **Config backup interval (seconds)** — how often config backup jobs are scheduled when enabled

Tune intervals to balance freshness against load on gear and collectors.

## Meraki

Enable Meraki sync to pull org devices into Sonar as vendor `meraki` with automatic role tags. Live health (ports, uplinks, clients, and so on) comes from the Dashboard API on the sync/telemetry interval—not SNMP.

Typical controls:

- **Enable sync**
- **Organization filter** — limit which Meraki orgs are imported
- **Sync interval (seconds, minimum 300)** — how often inventory sync and related telemetry cadence apply
- **Poll now / Sync now** — force an immediate inventory sync

Create the Dashboard API key in Meraki, store it as a Meraki credential on the site, then enable sync.

!!! note "Management IP"
    Meraki management IP in Sonar stays on the LAN/appliance address. WAN uplink IPs in telemetry are health data, not the management target.
