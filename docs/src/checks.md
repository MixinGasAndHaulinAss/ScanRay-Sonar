# Synthetic checks

Sonar **checks** are additive reachability monitors (ICMP, TCP, HTTP, DNS, TLS). They do **not** replace SNMP OID packs, Meraki sync, or agent DEX host telemetry.

## Agent first, central fallback

Each check has `preferredRunner`: `auto` | `agent` | `collector` | `central`.

- **auto** — if an enrolled agent at the site is online (last seen &lt; 2 minutes), the agent runs the check; otherwise the central poller runs it.
- **agent** — prefer the assigned agent (or any online site agent); fall back to central if none are available.
- **central** / **collector** — run on the poller (collector path treated as central in phase 1).

## Check types

| Type | What it measures |
|------|------------------|
| `icmp` | Echo latency + packet loss |
| `tcp` | TCP connect time / up |
| `http` | Status code + response time |
| `dns` | Resolve success + time |
| `tls` | Handshake + days to cert expiry + CN match |

Types are embedded under `internal/checks/catalog/`. Regenerate from a local full-sensor catalog extract:

```bash
python scripts/build-checkpacks.py
```

The raw extract directory `prtg_full_sensor_catalog/` is gitignored (reference only). Runtime IDs and docs use Sonar names only.

## UI / API

- UI: **Checks** in the primary nav
- API: `GET/POST /api/v1/checks`, `GET /api/v1/check-types`, samples at `/checks/{id}/samples`
- Alarms: seeded rules with `target_kind=check` on flattened fields like `device.http_up`, `device.tls_days_to_expiration`

## Relation to other dumps

| Source | Role |
|--------|------|
| `oid_bundle/` | SNMP OID rows → OID packs |
| `prtg_full_sensor_catalog/` | Product catalog of sensors/channels/mechanisms → check type packs (phase 1: synthetic only) |
