# syntax=docker/dockerfile:1.7
# Optional Linux container packaging of the Probe — useful for testing
# inside the dev compose stack. Production deployment is bare-binary via
# the cross-compile matrix in scripts/build-probe.sh.

FROM golang:1.23-alpine AS gobuild
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Version=${VERSION} \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Commit=${COMMIT} \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/sonar-probe ./cmd/sonar-probe

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=gobuild /out/sonar-probe /sonar-probe
USER nonroot:nonroot
ENTRYPOINT ["/sonar-probe"]
