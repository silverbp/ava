# ava

A double-entry accounting system: a Postgres-backed ledger schema, a gRPC API server (`cmd/ava`),
and a CLI client (`cmd/avactl`). Covers core ledger accounting, parties, trading documents,
banking/reconciliation, period close, tax, and reporting, with passkey (WebAuthn)-based auth.

## Layout

- `migrations/` — the Postgres schema (single up migration, no down migrations by design)
- `proto/ava/v1/` — gRPC service and message definitions (`buf generate` → `gen/ava/v1/`)
- `sql/queries/` — sqlc query definitions (`sqlc generate` → `internal/db/sqlcgen/`)
- `internal/server/` — gRPC service implementations
- `internal/avactl/`, `cmd/avactl/` — CLI client
- `cmd/ava/` — API server entrypoint
- `docs/` — architecture notes and schema reference

## Development

```sh
make db-up        # start Postgres (+ SeaweedFS) via docker compose
make migrate-up    # apply the schema
make generate      # regenerate proto + sqlc code after touching proto/ or sql/queries/
make build         # go build ./...
make test          # go test ./...
make run           # go run ./cmd/ava
```

## License

MIT — see [LICENSE](LICENSE). All source files carry an SPDX `MIT` identifier.
