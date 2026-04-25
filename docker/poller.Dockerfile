# syntax=docker/dockerfile:1.7
FROM golang:1.23-alpine AS gobuild
# Optional: inject a corporate root CA for hosts behind TLS inspection.
# `docker/local-ca.crt` ships as an empty placeholder; populate it via
# `scripts/inject-host-ca.sh` on hosts where outbound HTTPS is intercepted.
COPY docker/local-ca.crt /tmp/local-ca.crt
RUN if grep -q "BEGIN CERTIFICATE" /tmp/local-ca.crt 2>/dev/null; then \
      cat /tmp/local-ca.crt >> /etc/ssl/certs/ca-certificates.crt; \
    fi && rm /tmp/local-ca.crt
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Version=${VERSION} \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Commit=${COMMIT} \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/sonar-poller ./cmd/sonar-poller

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=gobuild /out/sonar-poller /sonar-poller
USER nonroot:nonroot
ENTRYPOINT ["/sonar-poller"]
