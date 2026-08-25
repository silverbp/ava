GOBIN := $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

# avactl's version is MAJOR.MINOR (from the VERSION file) plus a PATCH
# that's just the repo's total commit count, so it advances on its own as
# commits land instead of needing to be hand-bumped.
VERSION_PKG := github.com/silverbp/ava/internal/avactl/version
VERSION     := $(shell cat VERSION).$(shell git rev-list --count HEAD 2>/dev/null || echo 0)
GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -X '$(VERSION_PKG).Version=$(VERSION)' -X '$(VERSION_PKG).GitCommit=$(GIT_COMMIT)' -X '$(VERSION_PKG).BuildDate=$(BUILD_DATE)'

.PHONY: generate proto sqlc migrate-up db-up db-down run build avactl install test

generate: proto sqlc

proto:
	buf generate proto

sqlc:
	sqlc generate

db-up:
	docker compose up -d db seaweedfs

db-down:
	docker compose down

migrate-up: db-up
	@until docker compose exec -T db pg_isready -U ava >/dev/null 2>&1; do sleep 1; done
	AVA_POSTGRES_DSN=postgres://ava:ava@localhost:5432/ava?sslmode=disable go run ./cmd/ava migrate

build:
	go build ./...

avactl:
	go build -ldflags "$(LDFLAGS)" -o bin/avactl ./cmd/avactl

# Same as `avactl`, but installs to $GOBIN (plain `go install ./cmd/avactl`
# skips the Makefile entirely, so it can't stamp version info - always
# build avactl through this target, not a bare `go install`, unless you
# don't care which version string it reports).
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/avactl

run:
	go run ./cmd/ava

test:
	go test ./...
