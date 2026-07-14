# Data storage and retention

Sonar stores telemetry in PostgreSQL / TimescaleDB. **Superadmins** control how long each class of data lives under **Settings → Data retention**. Those knobs drive automatic roll-off: raw samples age into hourly trends, then are deleted; cleared alarms and audit rows prune on their own schedules.

Non-superadmins do not see the retention form. SMTP and webhooks on the same Settings page are separate (siteadmin).

## How data ages (lifecycle)

Telemetry does not stay at full resolution forever. It moves through four stages:

1. **Hot** — raw samples at poll cadence (CPU, interface counters, latency, etc.). Charts for recent ranges read this directly.
2. **Compressed** — still the same hot samples, but Timescale compresses older chunks on disk. Queries still see the data; disk use drops.
3. **Trend** — after the hot window ends, raw samples are dropped and charts use **hourly rollups** (continuous aggregates) for longer lookbacks.
4. **Gone** — past the rollup (or alarm / audit) limit, the history is deleted and cannot be recovered from Sonar.

!!! warning "Shortening windows deletes history"
    Saving a shorter window updates database retention policies immediately. Coordinate with operators before aggressive cuts—old samples and trends will roll off permanently.

## Where to change it

1. Sign in as **superadmin**.
2. Open **Settings** in the sidebar.
3. Edit the **Data retention** card at the top.
4. Click **Save retention**. Sonar persists the values and applies Timescale retention / compression policies (and prunes cleared alarms / audit rows). A background worker also reconciles policies about once an hour.

## What each setting controls

Defaults match a typical mid-size deployment (≈30-day hot, ≈1-year trends). Bounds are enforced in the UI and API.

| Setting | Default | Allowed range | What it controls |
|---------|---------|---------------|------------------|
| **Hot data window (days)** | 30 | 7–90 | How long full-resolution samples are kept for agents and appliances (metrics, network, latency, interface counters). Also the point after which charts switch to hourly trends. |
| **Compress after (days)** | 1 | 0–7 | When Timescale starts compressing hot chunks. `0` means compress almost immediately (policy uses a 1-hour interval). Compression saves disk; it does not remove data. |
| **Trend / rollup retention (days)** | 365 | 30–1825 (~5 years) | How long **hourly** rollups remain after the hot window. This is the farthest metrics charts can look back. |
| **Flow hot window (days)** | 14 | 3–90 | Retention for NetFlow / flow summary detail (`flow_summaries`). Flows are denser than SNMP samples, so the default is shorter. |
| **Vendor samples (days)** | 180 | 30–730 | Meraki / vendor-enriched samples (for example Dashboard-backed appliance metrics) kept as vendor sample rows. |
| **Cleared alarms roll-off (days)** | 365 | 30–1825 | How long **cleared** alarm rows remain. Open / active alarms are not deleted by this setting. |
| **Audit log roll-off (days)** | 365 | 30–1825 | How long security / admin audit events remain in **Audit log**. |

## What is stored (by class)

| Data class | Examples in the product | Retention driven by |
|------------|-------------------------|---------------------|
| Agent telemetry | Device CPU/memory, network samples, latency probes | Hot window → then hourly rollups to trend retention |
| Appliance telemetry | SNMP / collector metrics, interface bps graphs | Same as agent telemetry |
| Flow / traffic | Traffic views fed by flow summaries | Flow hot window |
| Vendor / Meraki samples | Dashboard-backed Meraki switch/AP enrichment | Vendor samples days |
| Alarms | Cleared alarm history | Cleared alarms roll-off |
| Audit | User admin actions, security events | Audit log roll-off |

Inventory objects themselves (sites, devices, appliances, collectors, users, alarm **rules**, API keys, uploaded **Documents**) are configuration—not time-series—and are **not** deleted by these retention windows.

## Capacity guidance

Disk need scales with fleet size, poll cadence, and how long you keep hot + trend data.

As a rule of thumb from the Settings UI:

- Mid-size fleets (~50 servers / ~40 appliances): plan **≥50 GB** for ~30-day hot + ~365-day trends.
- With NetFlow or ~100 appliances: plan **≥100 GB**.

If Postgres volume growth is the concern, shorten **hot** and **flow** windows first (largest raw footprint), then trend / vendor / alarm / audit windows.

## How charts behave

- Ranges **inside** the hot window use raw samples (full resolution).
- Ranges **beyond** the hot window use hourly rollups, up to the trend / rollup retention limit.
- Asking for a chart range past rollup retention will not return older points—the data is gone.

## Related pages

- [Settings](settings.md) — SMTP, webhooks, and a short link to this page
- [Alarms](alarms.md) — active vs cleared alarms
- [Audit log](audit-log.md) — what audit events record
- [Roles and permissions](rbac.md) — only superadmin can change retention
