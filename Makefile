# ScanRay Sonar — convenience targets. Equivalent PowerShell incantations
# are documented in README.md for Windows-native development.

VERSION    := $(shell cat VERSION)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
              -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Version=$(VERSION) \
              -X github.com/NCLGISA/ScanRay-Sonar/internal/version.Commit=$(COMMIT) \
              -X github.com/NCLGISA/ScanRay-Sonar/internal/version.BuildTime=$(BUILD_TIME)

.PHONY: all build api poller probe web test fmt vet tidy compose-up compose-down clean refresh-geoip

all: build

build: api poller probe

api:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/sonar-api ./cmd/sonar-api

poller:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/sonar-poller ./cmd/sonar-poller

probe:
	go build -trimpath -ldflags '$(LDFLAGS)' -o bin/sonar-probe ./cmd/sonar-probe

probe-all:
	bash scripts/build-probe.sh

web:
	cd web && npm install && npm run build

test:
	go test ./... -race -count=1

fmt:
	gofmt -s -w .

vet:
	go vet ./...

tidy:
	go mod tidy

compose-up:
	docker compose up -d --build

compose-down:
	docker compose down

clean:
	rm -rf bin/ dist/ web/dist/assets web/dist/index-*.js web/dist/index-*.css

refresh-geoip:
	bash scripts/refresh-geoip.sh
