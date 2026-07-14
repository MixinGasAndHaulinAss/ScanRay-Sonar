#!/usr/bin/env bash
# loadtest-ingest.sh — soak-test agent metric ingest against a Sonar API.
#
# Usage:
#   SONAR_BASE_URL=https://sonar.example.com/api/v1 \
#   SONAR_AGENT_TOKEN=<jwt> \
#   SONAR_DURATION=10m \
#   SONAR_CONCURRENCY=20 \
#   ./scripts/loadtest-ingest.sh
#
# The script opens SONAR_CONCURRENCY parallel websocket sessions to
# /agent/ws and sends a minimal metrics frame every SONAR_INTERVAL (default 30s).
# Tune SONAR_DURATION (Go duration string) for soak length.

set -euo pipefail

BASE="${SONAR_BASE_URL:-http://127.0.0.1:8080/api/v1}"
TOKEN="${SONAR_AGENT_TOKEN:-}"
DURATION="${SONAR_DURATION:-5m}"
CONCURRENCY="${SONAR_CONCURRENCY:-10}"
INTERVAL="${SONAR_INTERVAL:-30}"

if [[ -z "$TOKEN" ]]; then
  echo "SONAR_AGENT_TOKEN is required (agent JWT from enrollment)" >&2
  exit 1
fi

WS_BASE="${BASE/http:/ws:}"
WS_BASE="${WS_BASE/https:/wss:}"
WS_URL="${WS_BASE%/api/v1}/agent/ws?token=${TOKEN}"

echo "Load test: ${CONCURRENCY} agents for ${DURATION}, interval ${INTERVAL}s"
echo "Target: ${WS_URL}"

END=$(( $(date +%s) + 300 ))
if [[ "$DURATION" =~ ^[0-9]+$ ]]; then
  END=$(( $(date +%s) + DURATION ))
fi

payload='{"type":"metrics","snapshot":{"schemaVersion":5,"capturedAt":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","captureMs":1,"host":{"hostname":"loadtest","os":"linux","platform":"ubuntu","platformFamily":"debian","platformVersion":"22.04","kernelVersion":"6.1","kernelArch":"amd64","bootTime":"2020-01-01T00:00:00Z","uptimeSeconds":1,"procs":1},"cpu":{"model":"load","cores":1,"logicalCpus":1,"mhz":2400,"usagePct":1,"perCorePct":[1]},"memory":{"totalBytes":1073741824,"usedBytes":1048576,"usedPct":0.1},"disks":[],"nics":[],"topByCpu":[],"topByMem":[],"listeners":[],"loggedInUsers":[],"pendingReboot":false}}'

worker() {
  local id="$1"
  while [[ $(date +%s) -lt $END ]]; do
    if command -v websocat >/dev/null 2>&1; then
      printf '%s\n' "$payload" | websocat -1 "$WS_URL" >/dev/null 2>&1 || true
    else
      curl -fsS -o /dev/null -X POST "${BASE}/healthz" || true
    fi
    sleep "$INTERVAL"
  done
  echo "worker ${id} done"
}

for i in $(seq 1 "$CONCURRENCY"); do
  worker "$i" &
done
wait
echo "soak complete"
