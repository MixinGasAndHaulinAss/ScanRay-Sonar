# Installation

This page covers standing up **central Sonar** and enrolling **edge agents** (probes and collectors). For day-to-day use after login, see [Getting started](getting-started.md).

There are two central-host tracks:

1. **Greenfield** — clone the repo and build the stack with Docker Compose (source build).
2. **StrikeTeam lab** — pull pre-built images from GLCR with `scripts/deploy-registry.sh` (pull-only).

StrikeTeam publishes images to **GLCR** (`glcr.nclgisa.org`). Greenfield installs build from source via compose `build:`; there is no separate public GHCR customer packaging path.

## Prerequisites

| Requirement | Notes |
|-------------|--------|
| Linux host (or Docker Desktop) | Production target is a Linux VM with Docker Engine |
| Docker Compose v2 | `docker compose` plugin |
| `openssl` | Used by `scripts/dev-bootstrap.sh` to mint secrets |
| Outbound HTTPS | Pull base images; MaxMind refresh; collector/probe enroll |
| Disk | Mid-size fleets: **≥ 50 GB** Postgres volume; with NetFlow/growth: **≥ 100 GB**. See the README capacity table for detail |

Optional: a MaxMind GeoLite2 account (World map + ASN labels), Cloudflare Tunnel (or another reverse proxy) in front of the API.

## Central stack — greenfield

### 1. Clone and bootstrap secrets

```bash
git clone <repo-url> /opt/scanraysonar   # or any path you prefer
cd /opt/scanraysonar
bash scripts/dev-bootstrap.sh           # writes .env; prints bootstrap admin password
```

`dev-bootstrap.sh` refuses to overwrite an existing `.env`. Save the printed admin password — it is not stored elsewhere.

### 2. Set public URLs

Edit `.env` and set at least:

| Variable | Purpose |
|----------|---------|
| `SONAR_PUBLIC_URL` | UI hostname (CORS + links), e.g. `https://sonar.example.com` |
| `SONAR_INGEST_URL` | Agent ingest hostname, e.g. `https://ingest.example.com` |

For LAN-only first light you can use `http://<host-ip>:6969` for both.

### 3. MinIO (site documents)

Compose starts `sonar-minio`. Point the API at it so document uploads use object storage instead of falling back to the database:

```bash
SONAR_MINIO_ENDPOINT=sonar-minio:9000
SONAR_MINIO_USER=minio
SONAR_MINIO_PASSWORD=changeme_minio_root   # match compose / set a strong password
SONAR_MINIO_BUCKET=sonar-documents
SONAR_MINIO_SSL=false
```

If these are unset, Documents still works but stores blobs in Postgres.

### 4. Optional GeoIP

```bash
# .env must have MAXMIND_ACCOUNT_ID and MAXMIND_LICENSE_KEY
make refresh-geoip
```

Skip for first light; World map and network graphs still render, with "unknown" geo labels.

### 5. Start the stack

```bash
docker compose up -d --build
```

This builds `sonar-api` and `sonar-poller` from source and starts Postgres, NATS, MinIO, API, and poller.

### 6. Verify

```bash
curl -fsS "http://127.0.0.1:${SONAR_API_PORT:-6969}/api/v1/version"
curl -fsS "http://127.0.0.1:${SONAR_API_PORT:-6969}/api/v1/healthz"
```

Open the UI (default `http://<host>:6969`), sign in with the bootstrap admin from step 1, then change that password.

!!! tip "Bind address"
    Default publish is `0.0.0.0:6969`. For tunnel-only exposure set `SONAR_API_BIND=127.0.0.1` in `.env` and recreate `sonar-api`.

## Cloudflare Tunnel (optional)

Cloudflared runs **on the host**, not in this compose stack. Example ingress (host port **6969**):

```yaml
ingress:
  - hostname: sonar.<domain>
    service: http://127.0.0.1:6969
  - hostname: ingest.<domain>
    service: http://127.0.0.1:6969
    originRequest:
      noTLSVerify: true
  # ...existing catch-all 404
```

Put a Zero Trust Access policy on `sonar.<domain>`. Leave `ingest.<domain>` open — probes authenticate with signed JWTs, not Cloudflare Access.

Corporate TLS-inspecting proxies: run `bash scripts/inject-host-ca.sh` once before building images on that host.

## StrikeTeam lab — GLCR pull-only

Use this on hosts that already pull from `glcr.nclgisa.org` (for example `/opt/scanraysonar` on `dev`).

### One-time

1. Clone (or pull) the repo; ensure `.env` exists (`dev-bootstrap.sh` or copy `.env.example`).
2. Log in to GLCR with a deploy token that has `read_registry` (and `read_repository` if you `git pull` with the same token):

   ```bash
   echo "$DEPLOY_TOKEN" | docker login glcr.nclgisa.org:443 -u sonar-read --password-stdin
   git remote set-url origin "https://sonar-read:$DEPLOY_TOKEN@gitlab.nclgisa.org/StrikeTeam/Scanray-Sonar.git"
   ```

3. Optional GeoIP + Cloudflare as above.

### Recurring deploy

After a **green** GitLab pipeline on `main`:

```bash
cd /opt/scanraysonar
./scripts/deploy-registry.sh
```

That script:

- `git pull`s `main`
- Sets `SCANRAY_STACK_VERSION` from `VERSION`
- Defaults **`IMAGE_TAG=latest`** (override with `IMAGE_TAG=2026.7.16.1` to pin a CalVer or SHA)
- Runs compose with `docker-compose.yml` + `docker-compose.registry.yml` + (unless skipped) `docker-compose.dev-collector.yml`
- Pulls and force-recreates API, poller, and the co-resident test collector

```bash
# Hosts that must not run a self-collector:
SKIP_DEV_COLLECTOR=1 ./scripts/deploy-registry.sh

# Verify
curl -fsS http://127.0.0.1:6969/api/v1/version
```

Build-on-host fallback (no GLCR): `./scripts/deploy.sh` — slower; prefer registry mode when images exist.

CI variables, token rotation, and smart-build levers stay in the repository README.

### Dev-host test collector

When `docker-compose.dev-collector.yml` is active:

1. In the UI: **Collectors → Add collector → Issue token**.
2. Run **only** the **Enroll** one-shot from the install panel (seeds `sonar-collector-config`).
3. Do **not** also run the long-running `docker run -d … run` command — compose owns `sonar-collector`.
4. Later deploys rotate the collector image with `deploy-registry.sh`. Re-enroll only if the volume was wiped or the collector was deleted from inventory.

## Edge: devices (probes)

1. Create a [site](sites.md).
2. **Devices → Enrollment** — issue a token; copy the **Linux** or **Windows** install command.
3. Run it on the endpoint (outbound HTTPS/WSS to `SONAR_INGEST_URL` / public Sonar).
4. Confirm the device appears online.

Linux installs as a systemd unit; Windows as the `SonarProbe` service. Cross-compiled macOS binaries exist in the API image, but the Enrollment UI currently surfaces Linux and Windows one-liners. Details: [Devices](devices.md).

## Edge: collectors

A collector is a Linux Docker host at a remote site that polls gear Sonar cannot reach directly.

1. Issue a token on **Collectors**.
2. On the collector host, set `SONAR_MASTER_KEY` to the **same value as central Sonar**.
3. Run **Enroll**, then **Run** (or use the generated compose snippet).
4. Confirm **Last seen** updates on Collectors.

Default image: `glcr.nclgisa.org:443/striketeam/scanray-sonar/collector:latest`. Override with `SONAR_COLLECTOR_IMAGE` on the API if you mirror elsewhere. Host networking (and `NET_RAW` for passive SNMP) is required for correct SNMP/ICMP source IPs. Details: [Collectors](collectors.md).

## Post-install checklist

1. Create at least one [site](sites.md).
2. Enroll a [collector](collectors.md) and/or [devices](devices.md) as needed.
3. Configure [site discovery](site-discovery.md) (SNMP / Meraki).
4. Confirm **Appliances** and **Devices**, then set [alarms](alarms.md).
5. Add [checks](checks.md) for critical services.
6. Optionally schedule weekly GeoIP refresh:

   ```cron
   17 4 * * 2 cd /opt/scanraysonar && /usr/bin/make refresh-geoip && /usr/bin/docker compose restart sonar-api
   ```

## Troubleshooting

| Symptom | Likely fix |
|---------|------------|
| Compose exits: `.env not found` | Run `scripts/dev-bootstrap.sh` or copy `.env.example` → `.env` |
| API won't start: missing master key / JWT / DB password | Fill required keys in `.env` (see `.env.example`) |
| Collector enroll fails / can't decrypt credentials | `SONAR_MASTER_KEY` on the collector must match central Sonar |
| World map shows "unknown" | Populate MaxMind keys and run `make refresh-geoip`, then restart `sonar-api` |
| `/api/v1/version` stale after `git pull` | Pull alone does not recreate containers — run `./scripts/deploy-registry.sh` (or `compose up --build`) |
| `docker pull` from GLCR fails | Re-login with `sonar-read` deploy token; confirm network to `glcr.nclgisa.org:443` |
| Two collectors fighting on `dev` | Enroll only; let compose manage `run` when the dev-collector overlay is enabled |
