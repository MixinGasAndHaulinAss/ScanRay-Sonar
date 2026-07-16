# ScanRay Sonar

Single-pane-of-visibility platform for the enterprise network: servers, switches, network health, EDR alerts, and traffic flows — all in one dark-themed web console.

Sonar is the third member of the **ScanRay** product line (alongside ScanRay Console and ScanRay Pupp). It is **standalone** — it does not depend on Console or Pupp — and is built Go-first with a small embedded React/TypeScript UI.

> **Status:** Phases 1–3 substantially shipped. The platform polls SNMP appliances, ingests endpoint telemetry from a cross-compiled Go probe, renders an LLDP/CDP topology, draws a per-agent network graph (host → process → ISP) with offline GeoIP, and pins every agent on a zoomable world map. Phases 4–6 (vendor/EDR integrations, full Traffic UI, hardening) are still ahead. See [Roadmap](#roadmap).

---

## Features

### Shipped today

- **Endpoint agent (`sonar-probe`)** — single Go binary cross-compiled for Linux/Windows/macOS (amd64 + arm64). Self-enrolls over WebSocket, then streams snapshots every 30 s with: CPU / memory / disk / NIC counters; **expanded top-process stats** (cpu %, mem %, disk-r/w bps, net-up/down bps, open conns); listeners + remote conversations (per-process, per-peer, with port set); DNS resolution telemetry; **hardware specs** (CPU model, RAM modules, motherboard / BIOS, disk model+serial, NICs, GPU, chassis — collected via DMI/SMBIOS on Linux and WMI on Windows); **public-IP discovery** via `icanhazip.com`; agent **tags** for GUI filtering.
- **Network appliances** — SNMP **v1, v2c, v3** poller covering IF-MIB (port + interface counters), ENTITY-MIB / ENTITY-SENSOR-MIB (chassis + transceiver DDM), LLDP and Cisco CDP (auto-topology). SNMP credentials encrypted at rest with the master key. Per-appliance polling goroutines compute deltas + bps rates in-memory.
- **Vendor health polls** — UPS (RFC1628 + APC enterprise), Synology NAS (system + per-disk + RAID), Palo Alto firewalls (session table utilization), HPE Alletra / Nimble SAN (per-volume capacity), Cisco extras (per-window CPU averages, VLAN inventory). Time-series-stored in `appliance_vendor_samples`; flat-keyed on the `metrics.appliance` NATS subject so threshold rules like `device.battery_charge_pct < 50` reuse the existing alarm engine. Sixteen default vendor-health alarm rules ship preconfigured.
- **Passive SNMP discovery** — collector listens on its NIC for outbound UDP/161 packets emitted by upstream pollers (LibreNMS, Observium, vendor cloud appliances, MSP collectors), classifies each destination IP with one public-OID SNMP GET, and reports an additive per-site inventory plus an add/retire/change feed visible in the **Discovered** UI page. Linux-only (raw AF_PACKET socket + BPF filter); requires `cap_add: NET_RAW` and `network_mode: host` on the collector container.
- **Reports** — server-side rendered Markdown reports from the data Sonar already collects: per-site summary, UPS fleet health, Synology fleet, switch fleet. Templates live in Postgres, render via Go `text/template`, and download as `text/markdown` (use the browser's print → save-as-PDF for an offline copy). The UI previews Markdown via `react-markdown` with GitHub-flavored markdown tables.
- **Switch topology** — interactive force-directed graph of all managed appliances plus foreign neighbors discovered via LLDP/CDP, with Cisco IP-phone suppression, **physical-vs-virtual port** distinction, **uplink highlighting**, and last-seen timestamps. Pan + scroll-to-zoom; click any node to focus. A searchable **tag-filter dropdown** narrows the view (and is shared with the Agents and World pages).
- **Per-agent network graph** — two-tab view of every conversation the agent has open. The **Graph** tab is a deterministic radial layout (host center → processes inner ring → ISP / providers outer ring) with optional endpoints tier and a node-detail panel that answers "which providers does this process talk to" / "which processes touch this ISP." The **Map** tab plots the same peers on a zoomable world map, with great-circle lines from the agent to each peer (when the agent's own GeoIP is known), inbound vs. outbound colouring, automatic clustering at low zoom, and a side panel listing peers without a fix (private networks, anycast). All filters (Direction, Scope, Process) are shared between the two tabs. Built from the snapshot's conversation list, enriched offline with MaxMind GeoLite2 (City + ASN).
- **World map** — zoomable, clickable world map (`react-simple-maps` + bundled `world-110m.json`) plotting every agent at its public-IP location, with country / city / ASN labels.
- **Multi-site** + **RBAC** (`superadmin`/`siteadmin`/`tech`/`readonly`) + Argon2id local accounts + TOTP MFA; full **CRUD UI for users, sites, and appliances** (delete, not just disable).
- **Encrypted secrets at rest** via AES-256-GCM envelope encryption — per-row data keys wrapped by `SONAR_MASTER_KEY`.
- **OpenAPI 3.1** source-of-truth at [`/api/v1/openapi.yaml`](internal/api/openapi.yaml); **WebSocket-first** for agent ingest and UI live updates.
- **CalVer** versioning (`YYYY.M.D.patch`), shown on the login screen and inside the app.
- **Cloudflare Tunnel**-friendly: every container binds `127.0.0.1` only; the host's cloudflared exposes `sonar.<domain>` and `ingest.<domain>`.

### Planned (see [Roadmap](#roadmap))

- IPMI / Redfish, RAID + SMART deep dives, EDR/Sysmon event ingest, certificate inventory, synthetic reachability, time drift, backup last-success, reboot / crash history.
- Vendor integrations: Meraki Dashboard API (sync shipped), Cisco / Aruba / Ubiquiti / MikroTik plugin interface, SNMP trap receiver on UDP 162 (shipped).
- Traffic visualization: live per-host ribbon flow, site sankey/chord with LLDP-overlay link utilization (topology overlay shipped), NetFlow / IPFIX collector on UDP 2055 (shipped), optional eBPF (Linux) and ETW (Windows) byte-accurate flow capture.
- Universal Ctrl/Cmd-K search across hosts/IPs/CIDRs/ports/processes/DNS/ASNs.
- Alert rule editor (shipped), email + Slack/Teams channels (shipped), OIDC / Azure AD (stubs shipped), signed agent self-update channel (partial).

---

## Architecture

```mermaid
flowchart LR
  subgraph endpoints [Endpoints]
    win[Windows Probe]
    lin[Linux Probe]
    mac[macOS Probe]
  end

  subgraph appliances [Network]
    sw[Switches / Routers / APs]
    meraki[Meraki Cloud]
  end

  subgraph host [dev host VM]
    cf[cloudflared on host]
    subgraph stack [Sonar Docker Stack]
      api[sonar-api Go + embedded React UI]
      poller[sonar-poller Go]
      pg[(Postgres + Timescale)]
      nats[NATS JetStream]
    end
  end

  win -- wss --> cf
  lin -- wss --> cf
  mac -- wss --> cf
  cf --> api
  poller -- "SNMP v1/v2c/v3 + LLDP" --> sw
  poller -- HTTPS --> meraki
  api <--> nats
  poller <--> nats
  api <--> pg
```

### Components

| Service          | Image / build                                          | Purpose                                                    |
| ---------------- | ------------------------------------------------------ | ---------------------------------------------------------- |
| `sonar-api`      | **Build:** `docker/api.Dockerfile` (distroless). **Lab pull:** `glcr.nclgisa.org:443/striketeam/scanray-sonar/api` | HTTP/WS API + embedded React UI; agent ingest; GeoIP from read-only volume |
| `sonar-poller`   | **Build:** `docker/poller.Dockerfile`. **Lab pull:** `glcr.nclgisa.org:443/striketeam/scanray-sonar/poller` | Network appliance polling (SNMP v1/v2c/v3, LLDP/CDP)       |
| `sonar-collector`| **Lab pull:** `glcr.nclgisa.org:443/striketeam/scanray-sonar/collector` | Site-side SNMP/discovery daemon (remote hosts + optional co-resident on lab) |
| `sonar-postgres` | `timescale/timescaledb:2.17.2-pg16`                    | Relational + time-series storage                           |
| `sonar-nats`     | `nats:2.10-alpine`                                     | JetStream message bus for fan-out                          |
| `sonar-minio`    | `minio/minio`                                          | Optional object store for site Documents                   |
| `sonar-probe`    | bare binary (Win/Linux/Mac, amd64+arm64), no container | Endpoint telemetry agent (built by `make probe-all`)       |

Greenfield installs **build from source** via compose. StrikeTeam lab hosts **pull** GLCR images with [`scripts/deploy-registry.sh`](scripts/deploy-registry.sh). Step-by-step: [`docs/src/installation.md`](docs/src/installation.md) (also served in-app under **Documentation**).

Named volumes:

| Volume          | Mounted by   | Purpose                                                                                        |
| --------------- | ------------ | ---------------------------------------------------------------------------------------------- |
| `sonar-pgdata`  | `sonar-postgres` | Postgres data dir                                                                          |
| `sonar-natsdata` | `sonar-nats`    | JetStream stream storage                                                                   |
| `sonar-geoip`   | `sonar-api`  | MaxMind `GeoLite2-City.mmdb` + `GeoLite2-ASN.mmdb` (read-only mount). Populated by `make refresh-geoip`. |
| `sonar-minio-data` | `sonar-minio` | Object store for site Documents (when `SONAR_MINIO_*` is configured on the API) |

The web UI is **built once** with Vite and **embedded into `sonar-api`** via `go:embed` — no separate web container, no nginx. The API container is `gcr.io/distroless/static-debian12:nonroot` (no shell, no curl); use the `/api/v1/healthz` endpoint for upstream probes.

---

## Storage & Retention

Time-series data is the dominant storage cost in Sonar. The schema follows TimescaleDB best practice: dual-shape per source (latest-snapshot JSONB on the parent row + narrow append-only hypertable for history), one-day chunks, automated drop policies, and native columnar compression on cold chunks.

### Hypertables

| Hypertable                 | Source         | Cadence           | `segmentby`               | Retention | Compress after |
| -------------------------- | -------------- | ----------------- | ------------------------- | --------- | -------------- |
| `agent_metric_samples`     | probe          | 60 s              | `agent_id`                | 30 days   | 1 day          |
| `agent_network_samples`    | probe          | 60 s              | `agent_id`                | 30 days   | 1 day          |
| `agent_latency_samples`    | probe (ICMP)   | 60 s × 2 targets  | `agent_id, target`        | 30 days   | 1 day          |
| `appliance_metric_samples` | poller         | poll-interval     | `appliance_id`            | 30 days   | 1 day          |
| `appliance_iface_samples`  | poller         | poll-interval × N ports | `appliance_id, if_index` | 30 days   | 1 day          |

The `segmentby` columns are picked from the actual `WHERE` clauses in `internal/api/handlers_*.go` so each query touches only the columnar segments it needs — no decompression of unrelated rows.

### Compression

Migration [0008_compression](internal/db/migrations/0008_compression.up.sql) turns on **TimescaleDB native columnar compression** for every hypertable. Chunks older than 1 day get rewritten from row-oriented heap into per-column compressed arrays (delta-of-delta for timestamps, Gorilla for floats, dictionary for low-cardinality text). Reads are transparent — the planner decompresses on the fly. Typical ratio on this kind of repetitive numeric data is **8–12×**, with the largest table (`appliance_iface_samples`) reliably hitting the high end.

The 1-day delay matches the 24 h hot-read window every UI page uses, so the most recent chunk stays uncompressed (fast inserts, mutable) and the most common queries never pay decompression cost.

A one-shot backfill in the same migration walks every existing chunk older than 1 day and compresses inline, so the savings appear the moment `migrate up` returns rather than waiting for the daily background policy job.

### Capacity planning

Per-unit storage at 30-day **hot** retention, with compression enabled:

| Source                                | ~30-day footprint |
| ------------------------------------- | ----------------- |
| One agent (metrics + network + latency) | ~3 MB           |
| One 24-port appliance                 | ~30 MB            |
| One 48-port appliance                 | ~60 MB            |
| One 128-port chassis                  | ~150 MB           |

Fleet projections (steady state). Default product model is **30-day hot raw + 365-day hourly rollups** (~5% the cost of keeping raw for a year). Disk scales near-linearly with the admin **hot window**; rollup retention is far cheaper.

| Fleet                          | ~30d hot only | ~30d hot + 365d hourly | Naive 365d raw |
| ------------------------------ | ------------- | ---------------------- | -------------- |
| 6 agents / 3 appliances        | ~250 MB       | ~300–400 MB            | ~3 GB          |
| **50 agents / 40 appliances**  | **~2–4 GB**   | **~3–6 GB**            | ~25–50 GB      |
| 250 agents / 10 appliances     | ~3 GB         | ~4–5 GB                | ~36 GB         |
| **250 agents / 100 appliances**| **~8–15 GB**  | **~12–25 GB**          | ~100–180 GB    |
| 500 agents / 20 appliances     | ~7 GB         | ~9–12 GB               | ~84 GB         |

Without compression these numbers are roughly 10× larger. Recommended Postgres volume for a mid-size customer (50/40): **≥ 50 GB**; with NetFlow or growth toward 100 appliances: **≥ 100 GB**.

Superadmins configure hot window, compress-after, trend/rollup retention, and alarm/audit roll-off under **Settings → Data retention** (`GET/PUT /settings/retention`). Timescale policies are applied live when those values change.

### Verifying compression after deploy

```sql
-- per-hypertable ratio
SELECT hypertable_name,
       pg_size_pretty(before_compression_total_bytes) AS before,
       pg_size_pretty(after_compression_total_bytes)  AS after,
       round((before_compression_total_bytes::numeric
              / NULLIF(after_compression_total_bytes,0)), 1) AS ratio
  FROM hypertable_compression_stats('appliance_iface_samples');

-- which chunks are compressed
SELECT hypertable_name, chunk_name, is_compressed
  FROM timescaledb_information.chunks
 ORDER BY hypertable_name, range_start DESC;
```

### Long-range trends

Hourly **continuous aggregates** retain chart history past the hot window (default 365 days). Chart APIs use raw samples when `range ≤ hot_window_days`, and hourly aggregates when the requested range is longer. Optional later: Timescale `move_chunk` to park cold chunks on a slower tablespace.

---

## Repository Layout

```
.
├── cmd/
│   ├── sonar-api/                # API + UI server entrypoint (distroless)
│   ├── sonar-poller/             # SNMP polling service
│   └── sonar-probe/              # Endpoint agent (cross-compiled, OS-specific runners)
├── docker/
│   ├── api.Dockerfile            # multi-stage: web build → go build → distroless
│   ├── poller.Dockerfile
│   ├── probe.Dockerfile          # optional Linux container build for the probe
│   └── local-ca.crt              # corporate CA bundle (skip-worktree, never committed populated)
├── internal/
│   ├── api/                      # HTTP + WS handlers, OpenAPI spec, embedded UI
│   │                             #   handlers_{agents,agent_telemetry,appliances,
│   │                             #   appliance_telemetry,auth,meta,probe,resources,topology}.go
│   ├── auth/                     # Argon2id, JWT, TOTP, RBAC
│   ├── config/                   # SONAR_* environment loader
│   ├── crypto/                   # AES-256-GCM envelope encryption + tests
│   ├── db/
│   │   └── migrations/           # versioned SQL migrations (0001 … 0008)
│   ├── geoip/                    # MaxMind .mmdb readers (City + ASN), offline lookups
│   ├── logging/                  # slog JSON setup
│   ├── poller/                   # per-appliance scheduler, rate calc, persistence
│   ├── probe/                    # probe-side collection: snapshot, hardware,
│   │                             #   process tracker, public IP, DNS cache, enrollment
│   ├── probebins/                # embedded probe binaries served from /api/v1/probe/...
│   ├── snmp/                     # SNMP client, IF-MIB / ENTITY / LLDP / CDP collection
│   └── version/                  # ldflag-injected build info
├── scripts/
│   ├── build-probe.sh            # cross-compile matrix → web/dist embed via probebins
│   ├── dev-bootstrap.sh          # generate fresh secrets into .env
│   ├── deploy.sh                 # wrapper for git pull + compose up --build on dev host
│   ├── inject-host-ca.sh         # bake corporate CA into docker/local-ca.crt
│   └── refresh-geoip.sh          # download MaxMind .mmdb into the sonar-geoip volume
├── web/                          # Vite + React + TS + Tailwind UI
│   ├── src/api/                  # typed REST + WS client, response types
│   ├── src/components/           # ForceGraph, AgentNetworkGraph, Layout, Sparkline, ErrorBoundary
│   ├── src/pages/                # Dashboard, Login, Agents, AgentDetail, Appliances,
│   │                             #   ApplianceDetail, Sites, Topology, Users, World
│   ├── src/lib/                  # format helpers
│   └── dist/                     # built artifacts — embed target for sonar-api
├── ci/
│   └── smart_build.sh            # per-service smart-rebuild helper for the build stage
├── .gitlab-ci.yml                # canonical CI/CD pipeline (gitlab.nclgisa.org)
├── .hadolint.yaml                # Dockerfile lint allowlist (justified)
├── .gitleaks.toml                # secrets-scan allowlist (false positives only)
├── docker-compose.yml
├── docker-compose.registry.yml   # Phase 2 pull-only overlay (uses GLCR images)
├── Makefile
├── VERSION                       # CalVer source of truth
└── README.md
```

---

## Quick Start (Local Development)

Operator install (greenfield + lab + edges): **[`docs/src/installation.md`](docs/src/installation.md)**.

Prerequisites: Go 1.23+, Node 20+, Docker (for Postgres + NATS), `openssl` for the bootstrap script.

### 1. Generate `.env` and start dependencies

```bash
bash scripts/dev-bootstrap.sh        # writes .env with random secrets
docker compose up -d sonar-postgres sonar-nats
```

### 2. Build the UI once and run the API

```bash
cd web && npm install && npm run build && cd ..

set -a; source .env; set +a
SONAR_DB_HOST=127.0.0.1 SONAR_NATS_URL=nats://127.0.0.1:4222 \
  go run ./cmd/sonar-api
```

Open <http://localhost:6969> (or `http://<dev-host>:6969` from anywhere on the LAN) and sign in as the bootstrap admin (the script printed the password).

> The API is published on the host as `${SONAR_API_BIND}:${SONAR_API_PORT}` — defaults `0.0.0.0:6969`. Inside the container it still listens on `0.0.0.0:8080`. Port 8080 is intentionally avoided because it's taken on the `dev` host by an unrelated service. To restrict the UI to the host (cloudflared-only), set `SONAR_API_BIND=127.0.0.1` in `.env` and recreate the container.

### 3. (Optional) Populate the MaxMind GeoIP databases

Skip this for first-light dev work; without it, the World map and the agent network graph still render but ASN / city / country labels show "unknown".

```bash
# .env must already have MAXMIND_ACCOUNT_ID and MAXMIND_LICENSE_KEY
make refresh-geoip
docker compose restart sonar-api      # API loads .mmdb files at startup
```

`make refresh-geoip` downloads `GeoLite2-City.mmdb` and `GeoLite2-ASN.mmdb` into the `sonar-geoip` named volume. Re-run it weekly (MaxMind updates the free databases on Tuesdays) — easiest via cron:

```cron
17 4 * * 2 cd /opt/scanraysonar && /usr/bin/make refresh-geoip && /usr/bin/docker compose restart sonar-api
```

### 4. Build the probe binaries

```bash
make probe-all                        # cross-compiles linux/windows/darwin × amd64/arm64
```

The output drops into `internal/probebins/`, which is `go:embed`-ed into the API. The login screen's "Add an agent" panel then serves install one-liners that pull the right binary directly from `/api/v1/probe/install/...`.

### 5. UI hot-reload (optional, parallel terminal)

```bash
cd web && npm run dev   # http://127.0.0.1:5173 with /api proxy to :6969
```

### Run tests

```bash
go test ./... -race -count=1
```

---

## Production / lab deployment

Full checklist (greenfield compose **and** StrikeTeam GLCR pull-only, plus probe/collector enroll): **[`docs/src/installation.md`](docs/src/installation.md)**.

### StrikeTeam lab (`dev` via Tendril) — pull-only (preferred)

Canonical path after GitLab CI publishes images:

```bash
cd /opt/scanraysonar
./scripts/deploy-registry.sh
curl -fsS http://127.0.0.1:6969/api/v1/version
```

One-time: clone to `/opt/scanraysonar`, `bash scripts/dev-bootstrap.sh` (set `SONAR_PUBLIC_URL` / `SONAR_INGEST_URL`), `docker login` to GLCR with `sonar-read`, optional `make refresh-geoip`, optional Cloudflare Tunnel on port **6969** (Access on UI hostname only). Details and collector caveats are in the install guide.

`IMAGE_TAG` defaults to **`latest`**. Pin with `IMAGE_TAG=2026.7.16.1` (or a SHA) for rollback. Set `SKIP_DEV_COLLECTOR=1` on hosts that must not run the co-resident test collector.

### Build-on-host fallback

When GLCR is unavailable or you are iterating from source:

```bash
cd /opt/scanraysonar
./scripts/deploy.sh    # git pull + docker compose up -d --build
```

Corporate TLS-inspecting proxies: run `bash scripts/inject-host-ca.sh` once before building.

---

## Versioning (CalVer)

Format: `YYYY.M.D.patch` — e.g. `2026.4.26.12`. The single source of truth is the [`VERSION`](VERSION) file. CI reads it and injects it into all binaries via `-ldflags`. The same string is also rendered on the login screen and inside the app shell so operators can see at a glance what's deployed.

To bump, update **four** files in lockstep:

```powershell
# Windows / PowerShell
$v = "2026.4.27.1"
$v | Set-Content -NoNewline VERSION

# Then update the matching strings in:
#   web/package.json                    ("version": "$v")
#   web/src/components/Layout.tsx       (const APP_VERSION = "$v")
#   internal/api/openapi.yaml           (info.version: "$v")

git tag "v$v" && git push --tags
```

The tag push triggers the GitLab `release` flow described in [CI/CD Pipeline](#cicd-pipeline) below.

---

## CI/CD Pipeline

ScanRay Sonar's canonical source of truth is **`gitlab.nclgisa.org/StrikeTeam/Scanray-Sonar`**. GitHub at `MixinGasAndHaulinAss/ScanRay-Sonar` is a one-way mirror, refreshed by the `mirror:github` job on every successful master/tag pipeline. Day-to-day pushes target GitLab.

### Stages

```mermaid
flowchart LR
  validate --> test --> build --> scan --> package --> sync
```

| Stage | Jobs | Blocks pipeline? |
|---|---|---|
| `validate` | `lint:go` (gofmt + go vet), `lint:web` (`tsc --noEmit`), `lint:dockerfiles` (hadolint), `secrets-scan` (gitleaks full-history, `--redact`'d log + 30-day artifact), `vuln-scan-fs` (trivy fs, HIGH/CRITICAL fixed CVEs) | yes |
| `test` | `test:go` (`go test ./... -race -count=1`), `test:web` (`npm run build`), `test:probe-windows` (`GOOS=windows go vet ./...`), `test:probe-darwin` (`GOOS=darwin go vet ./...`), `vuln-scan:govulncheck` (reachable-CVE) | yes |
| `build` | `build:api`, `build:poller` — both delegate to [`ci/smart_build.sh`](ci/smart_build.sh) which decides per-service whether to do a real `docker build` or just retag the previous SHA's image as the current SHA based on a watched-paths diff | yes |
| `scan` | `scan:trivy-images` rescans the `:$CI_COMMIT_SHORT_SHA` images that the build stage just pushed, blocks on HIGH/CRITICAL fixed CVEs | yes |
| `package` | `package:api`, `package:poller` — alias the SHA tag as `:latest`, `:$VERSION`, and (on tag pipelines) `:$CI_COMMIT_TAG` | master + tag only |
| `sync` | `mirror:github` — push master/tag to `MixinGasAndHaulinAss/ScanRay-Sonar` | master + tag only |

No `allow_failure: true` anywhere. Every check is a hard gate.

### `FORCE_RUN_ALL=true` switch

To force every per-component job to fire regardless of changed paths (e.g. after a base-image bump or security advisory), trigger a manual pipeline with the variable `FORCE_RUN_ALL=true`. This is the "secure baseline" lever — every check ran, every image rebuilt fresh.

### Image registry (GLCR)

Three repositories are published per pipeline:

| Repository | Purpose |
|---|---|
| `glcr.nclgisa.org:443/striketeam/scanray-sonar/api` | sonar-api + embedded UI + cross-compiled probe binaries |
| `glcr.nclgisa.org:443/striketeam/scanray-sonar/poller` | sonar-poller |
| `glcr.nclgisa.org:443/striketeam/scanray-sonar/collector` | sonar-collector (remote sites + optional lab co-resident) |

Tag flavors on each:

| Tag | Source | When |
|---|---|---|
| `:$CI_COMMIT_SHORT_SHA` | git short SHA | every master / tag pipeline |
| `:latest` | aliased to current SHA | master only |
| `:$VERSION` | top-level [`VERSION`](VERSION) file | master only |
| `:$CI_COMMIT_TAG` | git tag (e.g. `v2026.5.5.8`) | tag pipelines only |

Probe binaries stay embedded in the API image via the `probebuild` stage in [`docker/api.Dockerfile`](docker/api.Dockerfile); no separate distribution path. Operators download per-OS probes from `https://<sonar>/api/v1/probe/download/{os}/{arch}` regardless of how they install.

### Security findings policy

When CI flags a finding, fix the underlying cause — never silently allowlist a real issue:

- **Trivy fs / image CVE in a Go dep** — bump the dep (`go get module@vX.Y.Z && go mod tidy`); bump `GO_IMAGE` in [.gitlab-ci.yml](.gitlab-ci.yml) and the `go` directive in `go.mod` if a newer compiler is required.
- **Trivy CVE in a base image** — bump the base in the Dockerfile + matching `*_IMAGE` variable in `.gitlab-ci.yml`.
- **gitleaks finding that's a real example/doc** — add a path or regex allowlist in [`.gitleaks.toml`](.gitleaks.toml) with a justification comment. Don't add open-ended allowlists.
- **gitleaks finding that's a real secret** — rotate the secret first, `git filter-repo` the commit out, then force-push. Never allowlist real secrets.
- **govulncheck "no vulnerabilities found"** is the win condition; if it flags reachable code, fix the call site or bump the dep.
- **hadolint warning for a documented exception** — add the rule code to [`.hadolint.yaml`](.hadolint.yaml) `ignored:` with a comment explaining the call site.

### Pull-only deploy on lab hosts (GLCR)

Lab hosts cut over from build-on-host to pull-only via [`docker-compose.registry.yml`](docker-compose.registry.yml):

```bash
echo "$DEPLOY_TOKEN" | docker login glcr.nclgisa.org:443 -u sonar-read --password-stdin
git remote set-url origin "https://sonar-read:$DEPLOY_TOKEN@gitlab.nclgisa.org/StrikeTeam/Scanray-Sonar.git"

# Preferred — wraps pull + force-recreate for api + poller + optional dev-collector:
./scripts/deploy-registry.sh
```

Manual equivalent:

```bash
git pull origin main
docker compose -f docker-compose.yml -f docker-compose.registry.yml pull
docker compose -f docker-compose.yml -f docker-compose.registry.yml up -d --pull always --force-recreate
```

This shrinks deploys from ~3 min to ~10 seconds and removes the build toolchain from the deploy host. **[`scripts/deploy-registry.sh`](scripts/deploy-registry.sh)** pulls **`:latest`** from GLCR for `sonar-api`, `sonar-poller`, and `sonar-collector` by default, exports **`SCANRAY_STACK_VERSION`** (read from the repo `VERSION` file) so every stack container gets label **`com.scanraysonar.release`**, and runs **`docker compose up -d --pull always --force-recreate`**. CI publishes `:latest`, `:$VERSION`, and `:$CI_COMMIT_SHORT_SHA` for every green main pipeline, so `:latest` always points at the most recent packaged digest. Override with **`IMAGE_TAG=2026.7.16.1`** (or any CalVer / SHA) when you want a deliberate, reproducible pin — typically for rollback or external customer hand-off. `/api/v1/version` stays honest regardless: each binary embeds its build-time CalVer.

#### After each change on `main` (operators + automation)

1. Wait for the GitLab pipeline on `main` to finish **green** (images published to GLCR — `:latest`, `:short-sha`, and **`:$VERSION`** CalVer).
2. On lab: `cd /opt/scanraysonar` and `git pull` so the checkout matches `main`. If you mirror only to GitHub, wait until `mirror:github` completes or pull from GitLab directly — a stale mirror leaves old `VERSION` and old compose refs even after CI publishes new images.
3. Run **`./scripts/deploy-registry.sh`** — pulls matching images from GLCR and recreates containers (uniform binaries + uniform **`com.scanraysonar.release`** label across Postgres/NATS/MinIO/API/poller).

Skipping step 3 after a code change means the host can keep running an older GLCR digest even when `git pull` advanced.

### Deploy token rotation (`sonar-read`)

The dev host pulls GLCR images using a project-scoped GitLab deploy token named `sonar-read` (scopes `read_repository` + `read_registry`). The same token authenticates `git pull` from GitLab on dev.

To rotate (compromise, expiry, scope change):

1. GitLab UI → `StrikeTeam/Scanray-Sonar` → **Settings → Repository → Deploy tokens**. Revoke the current token; create a new one with the same name + same scopes.
2. On dev: `docker logout glcr.nclgisa.org:443 && echo "<new>" | docker login glcr.nclgisa.org:443 -u sonar-read --password-stdin`
3. On dev: `git remote set-url origin "https://sonar-read:<new>@gitlab.nclgisa.org/StrikeTeam/Scanray-Sonar.git"`
4. Run a deploy to confirm the new token works before revoking the old one in step 1 (out-of-order if needed; the order above assumes a no-overlap rotation window).

### Required CI/CD variables (configure once on the GitLab project)

| Variable | Scope | Purpose |
|---|---|---|
| `GITHUB_MIRROR_TOKEN` | masked + protected | Fine-grained PAT for `MixinGasAndHaulinAss/ScanRay-Sonar` with `contents:write`. Used by `mirror:github`. |
| `DOCKERHUB_USER` / `DOCKERHUB_TOKEN` | masked | Docker Hub credentials for raised pull rate limits during runner image pulls. |

The runner-side dependency proxy (`${CI_DEPENDENCY_PROXY_GROUP_IMAGE_PREFIX}`) and the per-job container registry credentials (`$CI_REGISTRY_USER` / `$CI_REGISTRY_PASSWORD`) are populated automatically by GitLab.

---

## Security Model

- **Secrets at rest** — every `enc_*` column is sealed with AES-256-GCM. Per-row data keys wrap the actual ciphertext; the data keys themselves are wrapped by `SONAR_MASTER_KEY` (32 random bytes, base64). Loss of the master key = loss of every secret. Back it up offline.
- **User auth** — Argon2id (PHC string, parameters embedded so cost can be raised over time) + mandatory TOTP MFA for admins (Phase 2 ships the enrollment UI).
- **Tokens** — short-lived HS256 access JWT (15m) + refresh JWT (30d) with `kind` claim so a refresh can never be used as an access token.
- **RBAC** — coarse roles (`superadmin`/`siteadmin`/`tech`/`readonly`) compared via numeric rank; never compare role strings directly.
- **Audit log** — append-only `audit_log` table records every login, role grant, secret read, alert ack, and admin action.
- **Network exposure** — no compose port is published outside `127.0.0.1`. Public access is exclusively via the host's Cloudflare Tunnel.
- **Agent ingest** — `ingest.<domain>` is unauthenticated at the CF Access layer (machines can't sign in) but every websocket upgrade requires a signed agent JWT bound to the host fingerprint.

---

## Configuration

All settings come from environment variables prefixed `SONAR_`. See [`.env.example`](.env.example) for the full list.

### Required

| Variable                | Purpose                                                                |
| ----------------------- | ---------------------------------------------------------------------- |
| `SONAR_MASTER_KEY`      | 32 bytes base64 — wraps every database secret                          |
| `SONAR_JWT_SECRET`      | 64 bytes base64 — signs access + refresh tokens                        |
| `SONAR_DB_PASSWORD`     | Postgres password                                                      |
| `SONAR_PUBLIC_URL`      | Public hostname for the UI (e.g. `https://sonar.example.com`) — used in CORS |
| `SONAR_INGEST_URL`      | Public hostname for agent ingest (e.g. `https://ingest.example.com`) — embedded in install one-liners |

### Common optional

| Variable                                     | Purpose                                                                |
| -------------------------------------------- | ---------------------------------------------------------------------- |
| `SONAR_API_BIND` / `SONAR_API_PORT`          | Host interface + port for the published UI (defaults `0.0.0.0:6969`; set bind to `127.0.0.1` for cloudflared-only) |
| `SONAR_BOOTSTRAP_ADMIN_EMAIL` / `_PASSWORD`  | First-run admin (only used if no users exist)                          |
| `SONAR_JWT_ACCESS_TTL` / `_REFRESH_TTL`      | Token lifetimes (default 15 m / 30 d)                                  |
| `SONAR_SMTP_*`                               | Outbound email for alerts                                              |
| `SONAR_COLLECTOR_IMAGE`                      | Image ref shown in Collectors UI install commands (default GLCR collector `:latest`) |
| `SONAR_MINIO_ENDPOINT` / `_USER` / `_PASSWORD` / `_BUCKET` / `_SSL` | Documents object store (compose service `sonar-minio`; unset = Postgres fallback) |
| `SONAR_SNMP_TRAP_LISTEN`                     | Poller SNMP trap listen addr (e.g. `:162`)                             |
| `SONAR_FLOW_LISTEN`                          | Poller NetFlow/IPFIX listen addr (e.g. `:2055`)                        |
| `SONAR_MERAKI_API_KEY`                       | Optional env fallback for Meraki sync (prefer sealed key in site discovery UI) |
| `SONAR_OIDC_*`                               | OIDC login stubs (`ISSUER`, `CLIENT_ID`, `CLIENT_SECRET`, `REDIRECT`)  |

### MaxMind / GeoIP

The API enriches agent conversations with country / city / ASN / org using two MaxMind GeoLite2 `.mmdb` files mounted read-only at `/var/lib/sonar/geoip` from the `sonar-geoip` volume. Lookups are offline, in-process, and cached on the agent row — no per-request HTTP calls. Without these files, the World map and agent network graph still work but pin everything at "unknown."

| Variable                | Purpose                                                                |
| ----------------------- | ---------------------------------------------------------------------- |
| `MAXMIND_ACCOUNT_ID`    | Account ID from your MaxMind GeoIP2 account — read by `scripts/refresh-geoip.sh` |
| `MAXMIND_LICENSE_KEY`   | License key from the same account                                      |
| `SONAR_GEOIP_CITY_DB`   | Path inside the API container (default `/var/lib/sonar/geoip/GeoLite2-City.mmdb`) |
| `SONAR_GEOIP_ASN_DB`    | Path inside the API container (default `/var/lib/sonar/geoip/GeoLite2-ASN.mmdb`)  |

Refresh the databases with `make refresh-geoip` and restart `sonar-api` to pick them up.

---

## Roadmap

- **Phase 1 — Foundation** ✅
  Repo, compose stack, Postgres + Timescale, OpenAPI 3.1, JWT / MFA / RBAC, multi-site, AES-256-GCM envelope encryption, host-side Cloudflare Tunnel wiring, CalVer.
- **Phase 2 — Probe v1** ✅ (core shipped)
  Cross-compiled Go agent with self-enrollment + WebSocket ingest. Collects: system / disk / NIC (incl. per-NIC `Kind=wired|wireless|virtual|loopback` and `bytesSent/RecvBps` deltas), expanded top-process stats (cpu %, mem %, disk-r/w, net-up/down, open conns), listeners + remote conversations (per-process, per-peer, port set), DNS resolution telemetry, hardware specs (DMI/SMBIOS/WMI), public-IP discovery via `icanhazip.com`, **interactive logon sessions** (Windows via `WTSEnumerateSessionsExW` with state/source; Linux/macOS via `gopsutil`), **ICMP latency** to `8.8.8.8` and the discovered default gateway (60 s cadence, raw socket), and **HealthSignals** at 5 min cadence (battery health %, BSOD count, user-reboot count, app-crash count, event-log error count, missing-patch count, CPU + disk queue length, WiFi SSID/RSSI/signal %). Agent **tagging** + GUI filtering. Per-agent radial network graph (host → process → ISP) with offline GeoIP. World map of agents.

  **HealthSignals per-OS coverage** (✓ collected, — not yet, ✗ N/A):

  | Signal                    | Windows | Linux | macOS |
  |---------------------------|:-------:|:-----:|:-----:|
  | `batteryHealthPct`        |   ✓     |   ✓   |   —   |
  | `bsodCount24h`            |   ✓     |   ✗   |   ✗   |
  | `userRebootCount24h`      |   ✓     |   —   |   —   |
  | `appCrashCount24h`        |   ✓     |   —   |   —   |
  | `eventLogErrorCount24h`   |   ✓     |   ✗   |   ✗   |
  | `missingPatchCount`       |   ✓     |   —   |   —   |
  | `cpuQueueLength`          |   ✓     |   ✓   |   —   |
  | `diskQueueLength`         |   ✓     |   ✓   |   —   |
  | `highloadCpuIncidents24h` |   —     |   —   |   —   |
  | `wifiSsid` / `wifiRssiDbm`/`wifiSignalPct` | ✓ | ✓ |   —   |
  | `ispName`                 | mirrors `agents.geo_org`             |||

  Linux Windows-only patch counts and Linux package-manager scraping are gated behind `SONAR_PROBE_HEALTH_PKG=true` because they shell out to `apt`/`dnf`. Windows uses an embedded PowerShell batch + `typeperf.exe` for queue length to avoid pulling in `go-ole`.

  *Still TODO for Phase 2:* IPMI / Redfish, RAID + SMART, failed services, pending reboots, backup-last-success, time drift, cert inventory, security posture, EDR event ingest (presence detection shipped), GPU/UPS, containers/VMs, synthetic checks, traceroute hops, ETW UserInputDelay (high user-input delay).
- **Phase 3 — Network module** ✅ (core shipped)
  SNMP v1/v2c/v3 poller, IF-MIB (port stats), ENTITY-MIB / ENTITY-SENSOR-MIB (chassis + transceiver DDM), LLDP + CDP auto-topology with Cisco IP-phone suppression, **physical-vs-virtual port** distinction, **uplink highlighting**, last-seen tracking, full appliance + site CRUD, encrypted SNMP credentials, **link-utilization overlay on topology** (IF-MIB bps), **SNMP trap receiver** (UDP 162 via `SONAR_SNMP_TRAP_LISTEN`), **vCenter REST inventory** during discovery.
- **Phase 4 — Vendor + EDR** (partial)
  **Meraki Dashboard sync** (GUI under Sites → Discovery → Meraki sync; sealed API key + Sync now; poller also honors `SONAR_MERAKI_API_KEY`), **EDR/Sysmon presence** on Windows probes, **alarm rule editor** (channel multi-select, PATCH, forSeconds/clearForSeconds), **Slack/Teams webhook channels**.
  *Still TODO:* full EDR event ingest, Cisco / Aruba / Ubiquiti / MikroTik plugin interface.
- **Phase 5 — Traffic visualization** (partial)
  **NetFlow v5 / IPFIX stub parser**, **flow_summaries hypertable**, **sonar-flowd** / poller `SONAR_FLOW_LISTEN`, **Traffic UI** with IP search.
  *Still TODO:* universal Ctrl/Cmd-K search, per-host ribbon flow, site sankey/chord, sFlow, eBPF/ETW byte-accurate capture.
- **Phase 6 — Polish** (partial)
  **OIDC login stubs** (`SONAR_OIDC_*`), **load-test script** (`scripts/loadtest-ingest.sh`), **MinIO document offload** (`SONAR_MINIO_*`), **signed probe self-update** channel.
  *Still TODO:* full OIDC user provisioning, OpenAPI polish, production hardening pass.

---

## Contributing

This is a small internal project. PRs welcome from authorized contributors. Conventions:

- `gofmt -s` + `go vet` clean before pushing.
- `go test ./... -race` green.
- **Bump CalVer on every change to `main`.** Update `VERSION`,
  `web/package.json`, `web/package-lock.json`, `internal/api/openapi.yaml`,
  and `web/src/components/Layout.tsx` together — the operator-facing
  version on the dev host should match the commit they last deployed.
  This replaces the older "one bump per release" rule; we now run the
  pull-only deploy on every merge.
- Comments explain *why*, not *what*. The code says what.

## License

MIT — see [LICENSE](LICENSE).
