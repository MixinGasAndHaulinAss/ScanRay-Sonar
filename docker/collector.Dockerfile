# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS gobuild
COPY docker/local-ca.crt /tmp/local-ca.crt
RUN if grep -q "BEGIN CERTIFICATE" /tmp/local-ca.crt 2>/dev/null; then \
      cat /tmp/local-ca.crt >> /etc/ssl/certs/ca-certificates.crt; \
    fi && rm /tmp/local-ca.crt
WORKDIR /src
COPY go.mod go.sum* ./
COPY . .
RUN go mod tidy
ARG VERSION=
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN set -eux; \
    V="${VERSION:-$(tr -d "[:space:]" < VERSION 2>/dev/null)}"; \
    V="${V:-dev}"; \
    BT="${BUILD_TIME}"; \
    if [ "$BT" = "unknown" ]; then BT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; fi; \
    CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Version=${V} \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Commit=${COMMIT} \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.BuildTime=${BT}" \
      -o /out/sonar-collector ./cmd/sonar-collector

# Skeleton config dir owned by uid:gid 65532 (the distroless `nonroot`
# user). When operators mount a fresh named volume at /etc/sonar, Docker
# seeds it from this directory — which means the volume root inherits
# nonroot ownership and the collector can write collector.json without
# the operator having to pre-chown anything.
RUN mkdir -p /sonar-config-skel && chown -R 65532:65532 /sonar-config-skel

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=gobuild /out/sonar-collector /sonar-collector
COPY --from=gobuild --chown=nonroot:nonroot /sonar-config-skel /etc/sonar
USER nonroot:nonroot
ENTRYPOINT ["/sonar-collector"]
