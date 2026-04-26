# syntax=docker/dockerfile:1.7

# ---- Stage 1: build the React UI -------------------------------------------
FROM node:20-alpine AS web
# Optional: inject a corporate root CA for hosts behind TLS inspection.
# `docker/local-ca.crt` ships as an empty placeholder; populate it via
# `scripts/inject-host-ca.sh` on hosts where outbound HTTPS is intercepted.
COPY docker/local-ca.crt /tmp/local-ca.crt
RUN if grep -q "BEGIN CERTIFICATE" /tmp/local-ca.crt 2>/dev/null; then \
      cat /tmp/local-ca.crt >> /etc/ssl/certs/ca-certificates.crt; \
    fi && rm /tmp/local-ca.crt
ENV NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt
WORKDIR /src/web
COPY web/package*.json ./
# Prefer `npm ci` (deterministic) but fall back to `npm install` when
# package.json adds dependencies before the lockfile catches up. This
# keeps the build green when the dev workstation has no Node locally
# (Windows hosts behind corporate IT often don't).
RUN npm ci --no-audit --no-fund \
  || npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- Stage 2: cross-compile the Sonar Probe for every endpoint OS/arch ----
# These binaries get embedded into the API binary in the next stage so
# /api/v1/probe/download/{os}/{arch} can serve them without a separate
# release artifact pipeline.
FROM golang:1.24-alpine AS probebuild
COPY docker/local-ca.crt /tmp/local-ca.crt
RUN if grep -q "BEGIN CERTIFICATE" /tmp/local-ca.crt 2>/dev/null; then \
      cat /tmp/local-ca.crt >> /etc/ssl/certs/ca-certificates.crt; \
    fi && rm /tmp/local-ca.crt
WORKDIR /src
COPY go.mod go.sum* ./
COPY . .
# `go mod tidy` regenerates go.sum from current go.mod + sources, so a
# fresh dependency (added during dev without a local Go toolchain) is
# fully resolved at build time without us having to hand-maintain
# go.sum on Windows.
RUN go mod tidy
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN set -eux; \
    LDFLAGS="-s -w \
      -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Version=${VERSION} \
      -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Commit=${COMMIT} \
      -X github.com/NCLGISA/ScanRay-Sonar/internal/version.BuildTime=${BUILD_TIME}"; \
    mkdir -p /probe/linux/amd64 /probe/linux/arm64 /probe/windows/amd64; \
    GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" \
      -o /probe/linux/amd64/sonar-probe       ./cmd/sonar-probe; \
    GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" \
      -o /probe/linux/arm64/sonar-probe       ./cmd/sonar-probe; \
    GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" \
      -o /probe/windows/amd64/sonar-probe.exe ./cmd/sonar-probe

# ---- Stage 3: build the Go binary with the UI + probes baked in -----------
FROM golang:1.24-alpine AS gobuild
COPY docker/local-ca.crt /tmp/local-ca.crt
RUN if grep -q "BEGIN CERTIFICATE" /tmp/local-ca.crt 2>/dev/null; then \
      cat /tmp/local-ca.crt >> /etc/ssl/certs/ca-certificates.crt; \
    fi && rm /tmp/local-ca.crt
WORKDIR /src
COPY go.mod go.sum* ./
COPY . .
RUN go mod tidy
# Copy the freshly-built UI in so go:embed picks it up.
COPY --from=web /src/web/dist ./web/dist
# Drop the cross-compiled probe binaries into the embed directory so the
# API binary serves them via /api/v1/probe/download/{os}/{arch}.
COPY --from=probebuild /probe ./internal/probebins/bin
ARG VERSION=
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
# Single source of truth for the CalVer string is the top-level VERSION
# file. The build arg can override (CI passes a pinned value); when it's
# empty (local docker compose build with no --build-arg) we fall back
# to the file so the resulting binary always reports the real version
# instead of "dev".
RUN set -eux; \
    V="${VERSION:-$(cat VERSION 2>/dev/null | tr -d '[:space:]')}"; \
    V="${V:-dev}"; \
    BT="${BUILD_TIME}"; \
    if [ "$BT" = "unknown" ]; then BT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; fi; \
    CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Version=${V} \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Commit=${COMMIT} \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.BuildTime=${BT}" \
      -o /out/sonar-api ./cmd/sonar-api

# ---- Stage 4: runtime ------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=gobuild /out/sonar-api /sonar-api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/sonar-api"]
