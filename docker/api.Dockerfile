# syntax=docker/dockerfile:1.7

# ---- Stage 1: build the React UI -------------------------------------------
FROM node:20-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- Stage 2: build the Go binary with the UI baked in ---------------------
FROM golang:1.23-alpine AS gobuild
RUN apk add --no-cache git ca-certificates
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# Copy the freshly-built UI in so go:embed picks it up.
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Version=${VERSION} \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Commit=${COMMIT} \
        -X github.com/NCLGISA/ScanRay-Sonar/internal/version.BuildTime=${BUILD_TIME}" \
      -o /out/sonar-api ./cmd/sonar-api

# ---- Stage 3: runtime ------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=gobuild /out/sonar-api /sonar-api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/sonar-api"]
