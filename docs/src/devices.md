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

## Enroll a new device

Requires **siteadmin** (to mint enrollment tokens).

1. Open **Devices** → enrollment / tokens section (or the enrollment UI under Devices).
2. Create an enrollment token for the target **site**.
3. Copy the install command for the OS (shell or PowerShell).
4. Run it on the endpoint. The probe enrolls once, then uses its own long-lived agent JWT.

Enrollment tokens are single-use (or short-lived) secrets. Revoke unused tokens.

## Install notes

- The endpoint needs outbound HTTPS to Sonar (and websocket if used for live updates).
- Prefer the official install scripts from Sonar (`/api/v1/probe/install.sh` / `.ps1`) so the correct probe binary is fetched.
- After enroll, confirm the device appears online on **Devices**.

## Rename, update, delete

Siteadmins can patch metadata or remove an agent from inventory. Deleting removes Sonar’s record; uninstall the probe on the host separately if you no longer want it reporting.
