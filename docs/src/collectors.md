# Collectors

A **sonar-collector** is a small Go daemon you run on a Linux host inside a remote site. It connects **outbound** to the central Sonar API over HTTPS/WSS—no inbound firewall holes are required on the collector host for Sonar itself.

## How collectors work

1. **Provision a token** — On **Collectors**, click **Add collector**, pick the site, submit. Sonar mints a single-use enrollment token (default TTL often 72h) and shows install commands.
2. **Run install commands** — On the collector host (Docker installed), set `SONAR_MASTER_KEY` to the same value as central Sonar, then run **Enroll** and **Run** in order (or use the supplied `docker-compose.yml` + `.env`).
3. **Jobs over outbound websocket** — The collector authenticates with the JWT from enrollment, stays online, and pulls work (SNMP polling, discovery sweeps, passive SNMP where configured). Results stream back on the same connection.
4. **Operate from Collectors** — Watch **Last seen** (heartbeat about every minute). Rename, deactivate (stop claiming new jobs), or delete from inventory.

## Network requirements

- Outbound HTTPS to central Sonar (`--base` URL in the install command)
- Outbound websocket (`wss://…/collector/ws`) for jobs and results
- From the collector to the gear it polls (SNMP/UDP 161, SSH, ICMP, etc.)
- **`SONAR_MASTER_KEY` must match** central Sonar so the collector can decrypt site credentials
- Install commands typically use `--network host` / `network_mode: host` so SNMP/ICMP source IPs are not mangled by Docker NAT

## Tokens vs collectors

An **enrollment token** is a short-lived secret that lets a host claim a collector identity. Once consumed, the collector exists with a permanent `collectorId` and its own JWT. Revoke unused tokens on the Collectors page.

## Dev / compose note

On hosts where compose already runs `sonar-collector`, do not also start a long-running manual `docker run` with the same name—use enroll once to seed config, then let compose manage the process.
