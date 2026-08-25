GOBIN := $(shell go env GOPATH)/bin
export PATH := $(GOBIN):$(PATH)

.PHONY: generate proto sqlc migrate-up db-up db-down run build test

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

run:
	go run ./cmd/ava

test:
	go test ./...
