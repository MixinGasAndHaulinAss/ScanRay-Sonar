# Devices (agents)

**Devices** are endpoints running the Sonar **probe** (agent). They report telemetry to the central API: system health, network path, latency, and experience-style metrics.

## Inventory

Open **Devices** to list enrolled agents. Filter by site and status. Stale “last seen” usually means the probe stopped, lost outbound HTTPS/WSS, or the host is offline.

## Device detail

Open a device for:

- Overview panels (user experience, applications, network latency/performance, device performance and averages)
- Metric history
- Network / path views
- Per-device reports where available
- **Add check (agent runner)** — create a [synthetic check](checks.md) that prefers this probe (useful for LAN ping/HTTP from the site)

## Enroll a new device

Requires **siteadmin** (to mint enrollment tokens). Central Sonar must already be running — see [Installation](installation.md).

1. Open **Devices** → Enrollment.
2. Create an enrollment token for the target **site**.
3. Copy the install command for the OS tab (**Linux** or **Windows**).
4. Run it on the endpoint. The probe enrolls once, then uses its own long-lived agent JWT.

Enrollment tokens are single-use (or short-lived) secrets. Revoke unused tokens.

## Install notes

- The endpoint needs outbound HTTPS (and WSS) to Sonar / `SONAR_INGEST_URL`.
- Prefer the official install scripts from Sonar (`/api/v1/probe/install.sh` / `.ps1`) so the correct probe binary is fetched.
- **Linux** installs as a systemd unit (commands typically need `sudo`).
- **Windows** installs as the `SonarProbe` Windows service.
- Cross-compiled **macOS** binaries are built into the API image (`make probe-all` / probe download API), but the Enrollment UI currently surfaces Linux and Windows one-liners only.
- After enroll, confirm the device appears online on **Devices**.

## Rename, update, delete

Siteadmins can patch metadata or remove an agent from inventory. Deleting removes Sonar’s record; uninstall the probe on the host separately if you no longer want it reporting.

## Device groups

**Devices → Groups** manages named groups per site. Membership is **1:1** (each agent has at most one `groupId`). Assign/remove members from the Groups tab or the group picker on a device detail page. Details grid has a Group column and filter chip.

## Data explorer

**Devices → Data** queries historical DEX indices (not full snapshot blobs): `devices`, `scores`, `health`, `processes`, `apps`, `patches`, `compliance_issues`, `vulnerabilities`. Filter by site/group/time and export CSV.

## System events

**Devices → Events** is a timeline of `alarm.*`, `group.changed`, `agent.online`/`offline`, and `compliance.changed` signals. Also available via `GET /agents/events`.

## Compliance

**Devices → Compliance** shows fleet posture (0–100 score distinct from UX 0–10). Issues cover missing patches, pending reboot, Win11 readiness, Secure Boot, EDR absence, stale agents, and **CVE-lite** heuristics from a static in-repo map (`internal/compliance/cve_map.json`) — not a live NVD scanner. Per-device **Compliance** tab lists open issues and CVEs.

## Agent alerts

Alarm rules can set `targetKind` to `agent`, `appliance`, or `any`. Seeded agent rules cover high CPU/memory, low UX score, missing patches, BSOD, and pending reboot. Follow-ups are **notification channels only** (no remote scripts).

## Agent reports

**Devices → Reports** can generate Markdown downloads for `agent-fleet-summary`, `agent-compliance`, and `agent-patches` (same `/report-templates` + `/reports` API as appliance reports).
