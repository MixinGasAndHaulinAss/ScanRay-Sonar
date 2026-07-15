# Appliances

**Appliances** are network devices Sonar monitors: switches, firewalls, access points, sensors, and similar gear.

## Two data sources

| Source | How it works | Typical vendors |
|--------|----------------|-----------------|
| **SNMP** | Poller or site collector walks SNMP; stores interface counters, CPU/memory when available, LLDP/CDP neighbors | Cisco IOS/XE, other SNMP gear |
| **Meraki Dashboard** | Org API sync + telemetry loop; no SNMP required on Meraki | Meraki MX/MS/MR/MT |

Management IP for Meraki stays on the LAN/appliance address—not the WAN uplink shown in telemetry.

## Inventory

Open **Appliances** to filter by site, vendor, and status. Check **last polled** / snapshot age. Meraki health often refreshes on the site Meraki sync interval (commonly ~15 minutes). SNMP uses each appliance’s poll interval.

## Appliance detail

On a device page you typically see:

- Status and identity (name, model, management IP)
- Chassis charts (CPU/memory) when the source provides them
- Interface or switch-port tables with rates
- Expandable port graphs (iface samples) when history exists
- Neighbors (LLDP/CDP or Meraki topology discovery)
- **OID metrics** from the embedded SNMP OID pack catalog (vendor-selected packs; see below)
- Meraki-only blocks: uplinks, VPN, wireless loss, sensors, firmware, alerts
- **Add check** — jump to [Checks](checks.md) with site and management IP pre-filled (ICMP/HTTP/TLS against the appliance, separate from SNMP)

### Meraki switch ports

For Meraki MS, Sonar enriches ports with name, VLAN, client counts, In/Out bps, and neighbors when Dashboard APIs allow. Soft-failures (404/400 on optional APIs) are logged and do not wipe the rest of the snapshot.

### SNMP ports

Physical and logical interfaces show admin/oper state, speed, In/Out bps, errors/discards, and uplink heuristics where configured.

### OID pack catalog

In addition to typed collectors (UPS, Cisco chassis, Synology, Palo Alto, Alletra), Sonar embeds **OID packs** under `internal/snmp/oidpack/data/`. On each poll, packs matching `appliances.vendor` and/or `sysObjectID` are collected into `last_snapshot.vendor.oidMetrics` and `appliance_vendor_samples`.

- Packs are Sonar-native JSON (no third-party product names in runtime IDs).
- Selection is selective: a Cisco switch does not pull Linux/host kitchen-sink packs.
- Table walks are capped (rows + time) so a large MIB never stalls the poller.
- Seeded alarm rules cover high-value pack fields (printer toner/paper/jam, Dell overall health, APC env alarm, host load).

To regenerate packs from a local analysis extract:

```bash
# Place the extract at ./oid_bundle/ (gitignored), then:
python scripts/build-oidpacks.py
```

## Add or edit an appliance

Siteadmins can create SNMP appliances (IP, community/v3, interval) or rely on Meraki sync / discovery to invent inventory. Prefer discovery or Meraki sync for large fleets so credentials stay centralized under the site. Vendor values such as `dell`, `linux`, `printer`, `windows`, `hp-procurve`, and `apc-env` select the matching OID packs.
