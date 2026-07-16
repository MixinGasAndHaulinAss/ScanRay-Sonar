# Collectors

A **sonar-collector** is a small Go daemon you run on a **Linux** host inside a remote site. It connects **outbound** to the central Sonar API over HTTPS/WSS—no inbound firewall holes are required on the collector host for Sonar itself.

For central-stack setup and the full enroll checklist, see [Installation](installation.md).

## Prerequisites

| Requirement | Notes |
|-------------|--------|
| Linux + Docker (Compose v2 helpful) | Collector images are Linux containers |
| Outbound HTTPS/WSS to central Sonar | `--base` URL from the install command |
| Reachability to local gear | SNMP/UDP 161, SSH, ICMP, etc. as needed |
| Matching `SONAR_MASTER_KEY` | **Must match** central Sonar or site credentials cannot be decrypted |
| Host networking | Install commands use `--network host` / `network_mode: host` so SNMP/ICMP source IPs are not mangled by Docker NAT |
| `cap_add: NET_RAW` | Required for [passive SNMP discovery](passive-snmp.md); optional if that feature is off |

Default image published by StrikeTeam CI:

`glcr.nclgisa.org:443/striketeam/scanray-sonar/collector:latest`

Air-gapped or mirrored registries: set `SONAR_COLLECTOR_IMAGE` on the **central API** so the Collectors UI emits the correct pull reference.

## How collectors work

1. **Provision a token** — On **Collectors**, click **Add collector**, pick the site, submit. Sonar mints a single-use enrollment token (default TTL often 72h) and shows install commands.
2. **Run install commands** — On the collector host, set `SONAR_MASTER_KEY` to the same value as central Sonar, then run **Enroll** and **Run** in order (or use the supplied compose snippet + `.env`).
3. **Jobs over outbound websocket** — The collector authenticates with the JWT from enrollment, stays online, and pulls work (SNMP polling, discovery sweeps, passive SNMP where configured). Results stream back on the same connection.
4. **Operate from Collectors** — Watch **Last seen** (heartbeat about every minute). Rename, deactivate (stop claiming new jobs), or delete from inventory.

## Tokens vs collectors

An **enrollment token** is a short-lived secret that lets a host claim a collector identity. Once consumed, the collector exists with a permanent `collectorId` and its own JWT. Revoke unused tokens on the Collectors page.

## Dev / compose note

On hosts where compose already runs `sonar-collector` (for example StrikeTeam `dev` with `docker-compose.dev-collector.yml`), do not also start a long-running manual `docker run` with the same name—use enroll once to seed config, then let compose manage the process. See [Installation](installation.md#dev-host-test-collector).
