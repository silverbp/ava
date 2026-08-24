# Ava gRPC API + avactl CLI

## Current status (resume here)

**All 9 phases are done.** The original plan below this point is complete;
this section stays as the living record of what shipped and how it was
verified. Phases 1–8 were tested live end-to-end via a real Postgres +
the built `avactl` binary; **Phase 9 (real passkey auth) was verified as
far as this environment allows without a real browser + platform
authenticator** — see its own note below for exactly what was and wasn't
exercised.

**What exists right now:**
- Full schema in `migrations/00001_initial.up.sql` (one file, no down
  migrations — deliberate, see Phase 1 notes below), including all the
  users/auth and Phase 6 additions.
- `docs/schema.md` is kept in sync with the schema (regenerate its prose/
  diagrams after further schema edits, per its own header note).
- Codegen tooling: `buf generate proto` (writes `gen/ava/v1/`), `sqlc
  generate` (writes `internal/db/sqlcgen/`) — both required after touching
  `proto/` or `sql/queries/`. Tool binaries live in `$(go env GOPATH)/bin`
  (installed via `go install` — not assumed to be on `$PATH` by default;
  prepend `export PATH="$PATH:$(go env GOPATH)/bin"` if a fresh shell can't
  find `buf`/`sqlc`/`grpcurl`).
- Working gRPC services (all registered in `internal/server/server.go`):
  `BusinessService`, `LedgerAccountService`, `LedgerTransactionService`,
  `ReportingService` (trial balance/balance sheet/income statement/general
  ledger/customer statement, each with a `Get*Pdf` counterpart),
  `PeriodCloseService`, `ContactService`, `ServiceCatalogService`,
  `TaxRateService`, `EstimateService`, `InvoiceService` (incl.
  `GetInvoicePdf`), `PaymentService`, `BankStatementService` (Phase 7 —
  `CreateBankStatement`, `ReconcileLedgerTransactions`,
  `ListUnreconciledLedgerTransactions`; `BankStatement.reconciled_balance`
  is computed live — opening_balance + net reconciled activity in the
  account's own normal-balance direction — for comparison against
  `closing_balance`), `EntityContextService`, `AttachmentService` (Phase 8
  — polymorphic `entity_type`/`entity_id` CRUD, validated against a fixed
  allowlist plus a per-type existence+tenant check in
  `internal/server/entity_ref.go`, since the schema deliberately carries no
  FK for these references; `attachment.storage_url` is caller-supplied —
  the API registers a reference to a file already hosted elsewhere rather
  than handling upload/storage itself, since no object-storage integration
  exists in this project; a presigned-upload flow is a natural addition
  once one does).
- `internal/pdf` (pure-Go, `github.com/go-pdf/fpdf`): a shared
  `Document`/`Table` layout type used by every PDF renderer
  (`internal/pdf/reports.go`, `internal/pdf/invoice.go`). All text goes
  through `Document.tr` (`UnicodeTranslatorFromDescriptor("")`) before
  hitting fpdf — fpdf's core Helvetica font expects cp1252, not raw UTF-8,
  and skipping this turns any accented character/curly quote/em dash into
  mojibake. Found and fixed live (rendered a PDF, read it back, saw
  "â€"" where an em dash should be).
- Working `avactl` CLI: `get`/`delete` (generic, via the resource registry
  in `internal/avactl/resource/`), `create <resource>` (bespoke subcommand
  per resource, in `internal/avactl/cmd/create_*.go`), `report` (incl.
  `customer-statement`), `close`, `reconcile` (Phase 7 — links ledger
  transactions to a bank statement), `context list` (Phase 8 — entity-
  context + attachments for one entity_type/entity_id, since both are
  always scoped to an entity rather than business-wide), `config`, `login`
  (stub — real auth is Phase 9). `-o pdf` on `get invoice <id>` and every
  `report` subcommand writes raw PDF bytes to stdout for shell redirection
  (`avactl report trial-balance -o pdf > tb.pdf`).
- **Auth**: `AuthModeDev` (fixed dev user, no token needed — unchanged) and
  `AuthModePasskey` (real WebAuthn), selected via `AVA_AUTH_MODE`.
  Passkey mode adds: `internal/auth/jwt.go` (HS256 access tokens, 15 min
  TTL), `internal/auth/session.go` (refresh tokens — `user_session`,
  rotate-on-use so a stolen-and-reused old token is detectable),
  `internal/auth/authcode.go` (in-memory, single-use, 2 min TTL — the
  hand-off between the browser ceremony and `avactl login`'s loopback
  listener), `internal/auth/webauthn.go` (the actual ceremony logic on
  `github.com/go-webauthn/webauthn`, usernameless/discoverable login via
  resident keys — no "enter your email" step before signing in),
  `internal/server/httpauth.go` (the HTTP endpoints + a single
  self-contained HTML+JS page — cross-device QR/BLE sign-in is the
  browser's own built-in passkey UI, nothing here implements that
  transport), `auth.proto`/`AuthService` (`ExchangeCode`/`RefreshToken`/
  `Logout` — allowlisted in the interceptor to work without a token),
  real `avactl login` (loopback browser flow), `deploy/Caddyfile` for
  `ava.silverblueprints.net` (path-routes `/auth/*` to the HTTP listener,
  everything else via h2c to the gRPC listener). `cmd/ava` runs both
  listeners unconditionally (`internal/server/server.go`); the production
  guard barring `AVA_AUTH_MODE=dev` with `AVA_ENV=production` was already
  in place since Phase 2. No Apple Sign-In (a mid-project decision —
  passkeys cover Apple-device users too via iCloud Keychain, without a
  separate OAuth integration). `~/.avactl/config` gained a `users:` section
  (refresh/access token + expiry per login) and `insecure:` per cluster
  (TLS on by default; `--insecure`/`avactl config set-context --insecure`
  for a local dev server with no certificate).
- **What Phase 9 verification actually covered**: this environment can't
  drive a real browser + platform authenticator (Touch ID, a security key,
  or cross-device BLE), so the `navigator.credentials.create()`/`.get()`
  ceremony itself was never completed end-to-end. Everything reachable
  without that was verified live: the server starts and validates its
  WebAuthn RP config correctly in passkey mode; `/auth/start` renders;
  `/auth/webauthn/register/begin` creates/reuses the `app_user` row and
  returns spec-correct `PublicKeyCredentialCreationOptions` JSON
  (confirmed idempotent — calling it twice for the same email doesn't
  duplicate the user); `/auth/webauthn/login/begin` returns spec-correct
  discoverable-login options; malformed requests and expired/unknown
  ceremony session ids fail cleanly; `AuthService.ExchangeCode` rejects a
  bogus code; a business-scoped RPC with no token correctly fails
  `Unauthenticated` in passkey mode, while `AuthService` itself doesn't
  (confirms the interceptor's public-methods allowlist); dev mode was
  regression-tested unaffected, including the new `--insecure` CLI flag.
  The full JWT + refresh-token lifecycle was verified directly (mint,
  verify, wrong-secret rejection, issue, rotate, **reusing a
  just-rotated/revoked refresh token is correctly rejected**, the new
  token after rotation still works, revoking an unknown token is a safe
  no-op) via a throwaway script exercising `internal/auth` against the
  live Postgres — same code paths `ExchangeCode`/`RefreshToken` use, just
  called directly instead of over gRPC+HTTP. **Not verified**: an actual
  passkey being created and used to sign in (needs real browser +
  authenticator hardware — recommend testing this by hand once there's a
  real client to try it from).
- Three real bugs were found and fixed by testing live rather than trusting
  code review: (1) `AccountBalancesAsOf`'s date filter lived in a `LEFT
  JOIN ... ON` clause, which doesn't exclude out-of-range entries from a
  `SUM` — fixed by moving the range check into the `SUM`'s `CASE`
  (`sql/queries/reporting.sql`). (2) `postZeroingTransaction`'s
  `incomeSummaryDelta` had its subtraction backwards, computing the
  negative of what was actually posted to Income Summary, which made
  period close's sweep step post to the wrong side
  (`internal/periodclose/close.go`). (3) PDF text needed cp1252
  translation, not raw UTF-8 (`internal/pdf/document.go`, above).
- Also built as part of Phase 6: `internal/reporting/statement.go`
  (customer statement — invoice activity, payment activity, running
  balance, AR aging in Current/1-30/31-60/61-90/90+ buckets), with new
  sqlc queries `ListInvoicesForContact`/`ListPaymentsForContact`
  (`sql/queries/trading.sql`).

**What's left:** nothing from the original 9-phase plan. Real next steps,
in rough priority order:
1. **Manually test a real passkey end-to-end** from an actual browser
   against a locally-running passkey-mode server (`AVA_RP_ID=localhost`,
   `AVA_PUBLIC_BASE_URL=http://localhost:9091` — WebAuthn treats
   `localhost` as a secure context even over plain HTTP) — the one thing
   this environment couldn't drive itself. See the Phase 9 verification
   note above for exactly what's already been checked.
2. **Register `ava.silverblueprints.net`** and point it at wherever
   `cmd/ava` + `deploy/Caddyfile` actually get deployed, set real
   `AVA_JWT_SECRET`/`AVA_RP_ID`/`AVA_PUBLIC_BASE_URL` values, and confirm
   Caddy's automatic TLS issuance succeeds against the live domain.
3. Everything under "Known deferred/simplified items" below — none of it
   blocks what's already built, but each is a real gap if the business
   need for it shows up (discounts, PURCHASE-side tax handling review,
   invoice re-posting/correction, estimate→invoice conversion, purchase
   orders, presigned attachment uploads once real object storage exists).

**Known deferred/simplified items** (each called out where relevant below,
collected here for visibility):
- Discounts (`estimate.discount_amount`/`invoice.discount_amount`) exist as
  columns but aren't exposed by the API — posting a discounted invoice
  needs an allocation decision (contra-revenue account?) not yet designed.
- PURCHASE-side tax is rolled into the expense line rather than split to a
  liability account — deliberate (see `docs/schema.md`), not a gap.
- No `UpdateInvoice`/re-posting — correcting a posted invoice needs a
  genuine reversing entry, same pattern as period-close reversal.
- No estimate→invoice conversion RPC (`invoice.estimate_id` can be set
  manually, but nothing auto-copies line items).
- No purchase_order table.

## Context

`ava` (module `github.com/silverbp/ava`) currently has only a Postgres schema
(`migrations/00001_initial.up.sql`) for a double-entry accounting system —
ledger, chart of accounts, period close, contacts/estimates/invoices/payments,
banking reconciliation, and polymorphic AI-context/attachment tables. `docs/schema.md`
and `docs/architecture.md` document the schema and a fully-specified (but
unimplemented) period-close algorithm. `cmd/ava/main.go` is an empty stub;
`internal/`, `pkg/` are empty; there is no proto/gRPC/CLI/auth code at all yet.

The goal: build a Go gRPC API over this schema, covering every table plus a
reporting surface (trial balance, balance sheet, income statement, general
ledger) and the period-close service, together with a kubectl-styled CLI
(`avactl`). Decisions made with the user:

- **V1 scope**: full CRUD for every table in the schema, plus reporting and
  period close.
- **DB layer**: sqlc (hand-written SQL, matches the existing migration style)
  over pgx.
- **Auth**: passkeys/WebAuthn, served from `ava.silverblueprints.net`, driven
  through a loopback-browser flow (`avactl login` opens a browser to the
  server, like `gh`/`gcloud`). Passkeys were chosen over Sign in with Apple:
  an iPhone/Mac passkey backed by iCloud Keychain already authenticates
  through the same standard WebAuthn flow, so Apple-device users are covered
  without a separate OAuth integration, Apple Developer portal Services-ID
  setup, or domain verification — one auth code path instead of two. The
  server hosts a plain WebAuthn registration/login page; cross-device
  sign-in (scan a QR code from your phone) is handled natively by the
  browser's built-in passkey UI, not by anything avactl or the server has to
  implement. A user can belong to multiple businesses with a role each (new
  `app_user` + `business_user` tables) — avactl gets a kubectl-context-like
  concept for "which business". A dev-mode auth bypass is required so the
  API/CLI work end-to-end on localhost before real passkey auth is wired up.
- **User attribution**: ledger transactions and other user-initiated records
  (invoices, payments, estimates, period closes, etc.) get a nullable
  `created_by_user_id`, not just business scoping.
- **Build order** (explicit user instruction): (1) schema for users/auth
  first, (2) then the core API + CLI built against dev-bypass auth, (3) real
  Apple auth implementation last.
- **CLI**: `avactl`, modeled on kubectl — verb-first resource commands
  (`get`/`describe`/`create`/`apply`/`delete`/`edit`), `-o json|yaml|table`,
  a kubeconfig-shaped `~/.avactl/config` with clusters/users/contexts, current
  context bundling server + credentials + current business (the `--namespace`
  analog).

This is a large, multi-session build. The plan is organized as 9 sequential
phases, each independently buildable and testable, so work can be picked back
up cleanly between sessions.

One schema bug found during design: `ledger_account.id` is a plain
`INTEGER PRIMARY KEY` with no identity generation (unlike every other table),
which blocks both the `CreateLedgerAccount` RPC and period-close's
system-account provisioning. Fixed in Phase 1.

## Schema additions (Phase 1)

Not deployed anywhere yet, so no migration-fragmentation discipline is
needed: the users/auth tables and column additions below are added directly
into `migrations/00001_initial.up.sql`, keeping the whole schema in one file.
No `.down.sql` either — matches the current convention (none exists today).

- **`app_user`**: `id` (identity PK), `apple_sub` (unique, the true identity
  key — not email, since Apple private-relay emails can change), `email`
  (nullable), `display_name`, `is_active`, `created_at`/`updated_at`/`deleted_at`
  (matches existing soft-delete convention).
- **`business_user`**: `id`, `business_id` FK, `user_id` FK, `role` (`VARCHAR`
  + `CHECK IN ('OWNER','ADMIN','MEMBER','VIEWER')` — matches the schema's
  existing string-enum convention, e.g. `invoice.status`), unique on
  `(business_id, user_id)`.
- **`user_session`**: server-side refresh-token storage — `user_id` FK,
  `refresh_token_hash` (sha256, raw token never stored), `issued_at`,
  `expires_at`, `revoked_at`, `replaced_by_session_id` (rotation chain),
  `client_name`, `last_used_at`. Access tokens are short-lived signed JWTs
  (HMAC-SHA256 for V1), verified in-process by the gRPC interceptor with no
  DB hit per call; only the long-lived refresh token is server-side and
  revocable.
- **`created_by_user_id`**: nullable `BIGINT REFERENCES app_user(id)`, added
  to header/parent tables only (not child rows like `ledger_entry`,
  `*_line_item`, `period_close_entry`, `bank_statement_line`, which inherit
  attribution from their parent): `business`, `ledger_account`,
  `ledger_transaction`, `period_close`, `contact`, `service`, `tax_rate`,
  `estimate`, `invoice`, `payment`, `bank_statement`, `entity_context`,
  `attachment`. Stays nullable everywhere — period-close postings and any
  future legacy import have no acting user.
- **Fix `ledger_account.id`**: declare it `GENERATED BY DEFAULT AS IDENTITY`
  directly (no need for an `ALTER`/`setval` guard, since this edits the
  original `CREATE TABLE` in place rather than patching a deployed schema).

Exit criteria: `migrations/00001_initial.up.sql` applies clean against a
fresh docker-compose Postgres.

## Proto/gRPC structure (Phase 2+)

- `proto/ava/v1/*.proto`, package `ava.v1`, `buf.yaml`/`buf.gen.yaml`/`buf.lock`
  at repo root (buf v2, standard lint, `protoc-gen-go` + `protoc-gen-go-grpc`,
  plus the `googleapis` buf dependency for `google.type.Date`). Generated code
  committed to `gen/ava/v1/*.pb.go` (not under `internal/`, since both the
  server and the CLI import it) — `make generate` regenerates it, but
  `go build ./...` never requires the buf toolchain.
- One proto file per schema domain: `common.proto` (shared types only),
  `auth.proto`, `business.proto` (+ membership RPCs), `ledger.proto`
  (`LedgerAccountService`, `LedgerTransactionService`), `period_close.proto`,
  `reporting.proto`, `party.proto` (`ContactService`/`ServiceService`/`TaxRateService`),
  `trading.proto` (`EstimateService`/`InvoiceService`/`PaymentService`),
  `banking.proto`, `context.proto` (`EntityContextService`/`AttachmentService`).
- **Money**: a shared `message Decimal { string value = 1; }` used uniformly
  for every `NUMERIC` column (amounts at scale 2, quantities at scale 4, rates
  at scale 4) — round-trips exactly through JSON/gRPC, no float precision
  loss. Backed by `github.com/shopspring/decimal` server-side, with a
  `internal/moneypb` conversion helper. Rejected int64-cents: this schema has
  three different decimal scales in play, so a single "cents" convention
  doesn't cover it cleanly.
- **Dates**: `google.type.Date` for every `DATE` column (not
  `google.protobuf.Timestamp`, which implies a time-of-day these columns
  don't have) — `internal/datepb` conversion helper.
- **Pagination**: cursor-style (`page_size`/`page_token` request,
  `next_page_token` response) — not raw offset, since `ledger_entry`/
  `ledger_transaction` will grow large.
- CRUD services follow one repeating shape (`Get`/`List`/`Create`/`Update`/
  delete-or-deactivate); non-CRUD services (`PeriodCloseService`,
  `ReportingService`) get bespoke RPCs matching the operations they expose
  (`TriggerClose`/`ReverseClose`/`ListPeriodCloses`;
  `GetTrialBalance`/`GetBalanceSheet`/`GetIncomeStatement`/`GetGeneralLedger`).

## Go server architecture

```
internal/
  config/       env-driven Config (DSN, ports, AVA_AUTH_MODE, JWT key, Apple secrets)
  db/
    sqlcgen/    sqlc-generated code (do not hand-edit)
    store.go    pgxpool + ExecTx(ctx, fn) transaction helper
  auth/
    jwt.go            access-token mint/verify
    apple.go          Sign-in-with-Apple OAuth client (Phase 9)
    session.go        refresh-token issue/rotate/revoke
    interceptor.go    gRPC interceptor: resolves ctx user (dev-bypass or JWT)
    devbypass.go       startup-provisions a fixed dev app_user/business/OWNER row
    authz.go           RequireBusinessRole(ctx, store, businessID, minRole) helper
  server/
    server.go                  wiring: pgxpool, Store, grpc.Server, interceptors
    ledger_service.go, period_close_service.go, reporting_service.go, ...  (one per proto service)
    httpauth.go                 /auth/apple/start + /auth/apple/callback (Phase 9)
  periodclose/    provision.go, close.go, reverse.go — the docs/architecture.md algorithm, gRPC-agnostic
  reporting/      trial_balance.go, balance_sheet.go, income_statement.go, general_ledger.go — gRPC-agnostic
  moneypb/, datepb/
gen/ava/v1/       committed generated proto/grpc code
cmd/
  ava/main.go      server binary
  avactl/main.go    CLI binary
sqlc.yaml, buf.yaml, buf.gen.yaml, buf.lock, Makefile
sql/queries/*.sql   hand-written sqlc input, grouped like the proto files
```

`internal/periodclose` and `internal/reporting` are plain Go packages
(no gRPC dependency), unit-testable against a real Postgres directly —
important given the lock-trigger edge cases in `docs/architecture.md`.

**gRPC + Apple OAuth callback on one domain**: `cmd/ava` runs a gRPC listener
and a plain HTTP listener on separate internal ports; a `deploy/Caddyfile`
reverse-proxies `ava.silverblueprints.net:443` with automatic TLS, routing
`/auth/*` to HTTP and everything else to gRPC (h2c). Simpler than cmux, and
TLS termination is needed anyway.

**Auth interceptor**: resolves the caller (dev-bypass fixed user, or JWT
verification) into `ctx`. `RequireBusinessRole` is called explicitly at the
top of each handler (business-id shape varies per RPC) rather than driven by
a generic proto-option mechanism — simplest thing that works for V1.

**Dev-bypass**: `AVA_AUTH_MODE=dev|apple` env var. In `dev` mode every RPC
resolves to a fixed dev user/business bootstrapped at startup — no manual
setup to run the whole stack locally. Server refuses to start if
`AVA_AUTH_MODE=dev` and `AVA_ENV=production` are both set.

## avactl CLI architecture

Cobra command tree mapped to kubectl verbs: `get`, `describe`, `create`
(`-f file.yaml`, plus a few flag-based convenience subcommands for documents
too awkward via flags alone, e.g. `create invoice`), `apply` (create-or-update
by natural key), `delete`, `edit` (fetch → YAML → `$EDITOR` → diff → Update),
`config` (`view`/`use-context`/`set-context`/`get-contexts`), `login`, plus
two non-CRUD verb groups: `close` (`trigger`/`reverse`/`list`) and `report`
(`trial-balance`/`balance-sheet`/`income-statement`/`general-ledger`).

**`~/.avactl/config`** — kubeconfig-shaped YAML: `clusters` (server address),
`users` (refresh/access tokens — plaintext file with `0600` perms is an
acceptable V1 shortcut; OS-keychain storage via `99designs/keyring`, what
`gh` uses, is a flagged Phase 9 upgrade), `contexts` (cluster + user +
`business` id, the `--namespace` analog), `current-context`.

**`avactl login`**: loopback flow — local `net/http` listener on
`127.0.0.1:0`, opens the browser to the server's `/auth/apple/start`, server
does the Apple OAuth dance, redirects the browser to the CLI's local listener
with a short-lived one-time code, CLI exchanges it via
`AuthService.ExchangeCode`, writes tokens to `~/.avactl/config`, and if no
context exists yet, calls `BusinessService.ListMyBusinesses` for an
interactive business picker.

**Generic resource CRUD without 20x duplication** — a registry pattern
(structurally like kubectl's `RESTMapper`/`Builder`):
```go
// internal/avactl/resource/registry.go
type Resource struct {
    Name, Plural string
    Get, List, Create, Update, Delete func(...) (...)  // thin closures over the generated gRPC client
    Columns []ColumnDef  // for -o table
}
var Registry = map[string]*Resource{ "ledger-account": ..., "invoice": ..., ... }
```
The cobra commands (`get.go`, `create.go`, `delete.go`, `edit.go`) are written
once, generically, taking a resource-name arg and dispatching through
`Registry`. Per-resource adapter closures (~10-15 lines each, grouped by
domain: `ledger.go`, `trading.go`, `party.go`, `banking.go`, `context.go`)
are mechanical glue, not duplicated logic. `close` and `report` bypass the
registry — not CRUD, and reports need bespoke tabular layouts (trial balance
vs. a hierarchical balance sheet).

```
cmd/avactl/main.go
internal/avactl/
  cmd/       root.go, get.go, describe.go, create.go, apply.go, delete.go, edit.go, config.go, login.go, close.go, report.go, business.go
  config/    config.go, context.go
  resource/  registry.go, ledger.go, trading.go, party.go, banking.go, context.go
  output/    table.go, json.go, yaml.go
  apiclient/ client.go — grpc.ClientConn from current context, bearer-token interceptor with auto-refresh
```

## Phased build order

1. **Users/auth schema** — extend `migrations/00001_initial.up.sql` in place
   with `app_user`/`business_user`/`user_session`/`created_by_user_id`, plus
   the `ledger_account.id` identity fix.
2. **Codegen tooling** — buf + sqlc + Makefile wiring, `common.proto`, first
   vertical slice (`business.proto` → sqlc → `BusinessService` → dev-bypass
   interceptor → bare `grpc.Server`). Exit: `make generate && make run`,
   `grpcurl` against `BusinessService` works.
3. **Core ledger service + avactl skeleton** — `ledger.proto`,
   `ledger_service.go`, `periodclose/provision.go` (built now, used later),
   `cmd/avactl` scaffolding + registry with `ledger-account`/
   `ledger-transaction`, generic `get`/`create`/`delete`. Exit:
   `avactl create ledger-account`, double-entry posting via CLI,
   `enforce_period_lock` errors surfaced through the API.
4. **Reporting** — `reporting.proto`, `internal/reporting/*.go`,
   `avactl report` group. Exit: `avactl report trial-balance` balances
   against seeded data.
5. **Period close** — `periodclose/close.go` + `reverse.go` (the
   `docs/architecture.md` 5-step algorithm), `period_close.proto`,
   `avactl close` group, business-creation wired to auto-provision Income
   Summary/Retained Earnings, tests for the lock-trigger edge cases already
   manually verified per the docs. Exit: `avactl close trigger/reverse/list`
   end-to-end; Phase 4 reporting reflects post-close retained earnings.
6. **Trading documents** — `contact`/`service`/`tax_rate`/`estimate`/
   `invoice`/`payment`, composite invoice-with-line-items create/update,
   payment-application recompute of `paid_amount`/`balance_due`. Consider
   adding a link back from `ledger_transaction` to the invoice/payment that
   caused it here (documented gap in `docs/schema.md`) — still just an edit
   to `migrations/00001_initial.up.sql` since it's all one file. **Also PDF
   export**, deferred here from Phase 4: a `format: PDF` option (returning
   raw bytes) reused across reporting (trial balance/balance sheet/income
   statement/general ledger, already built), invoices, and a new "customer
   statement" report (activity + running balance per contact — doesn't
   exist as a concept yet, shaped like general ledger but per-contact
   instead of per-account). One shared `internal/pdf` table/document
   layout package built on `go-pdf/fpdf` — branded output (embedded
   PNG/JPEG logo, TrueType fonts, colors) with zero extra runtime
   dependency, since `fpdf` supports image embedding directly, so there's
   no need for the HTML-to-Chrome/`wkhtmltopdf` route at all. Applied to
   reporting (trial balance/balance sheet/income statement/general ledger,
   already built), invoices, and a new "customer statement" report per
   contact over a date range, showing: invoice activity, payment activity,
   a running balance, and aging buckets (current/30/60/90+ days overdue).
7. **Banking** — `banking.proto`, `bank_statement`/`bank_statement_line`
   CRUD + reconciliation linking.
8. **entity_context + attachment** — polymorphic CRUD with service-layer
   `entity_type` allowlist validation (no DB FK to lean on). Storage-backend
   decision for `attachment.storage_url` (presigned upload vs. direct URL) is
   made in this phase.
9. **Real auth: passkeys (last, per instruction)** — `webauthn_credential`
   table (new migration, since it postdates the "single schema file" call —
   this table only needs to exist once this phase starts), `auth/webauthn.go`
   (using `github.com/go-webauthn/webauthn` as the relying-party library),
   `server/httpauth.go` (serves the WebAuthn registration/login pages — plain
   HTML+JS calling `navigator.credentials`; the browser's own passkey UI
   handles cross-device QR/BLE, nothing custom to build there), `auth.proto`,
   real `avactl login` (same loopback listener, now pointed at the WebAuthn
   flow), `deploy/Caddyfile` for `ava.silverblueprints.net`, production guard
   disabling dev-bypass. Exit: `avactl login` completes real passkey
   registration/login end-to-end, and all prior CRUD now enforces real
   per-user/per-business authorization.

## Verification

- Each phase ends with `make generate && make run` plus exercising its new
  surface via `avactl` against the docker-compose Postgres (`docker compose
  up db`).
- Phase 1: `migrations/00001_initial.up.sql` applies clean to a fresh
  docker-compose Postgres.
- Phase 3/5: exercise `enforce_period_lock` and the close algorithm's
  documented edge cases (full close doesn't self-lock, boundary-dated inserts
  rejected, post-close dates accepted, edits to locked entries rejected) —
  write these as Go tests against a real Postgres rather than relying on
  manual verification going forward.
- Phase 9: full `avactl login` loopback flow against
  `ava.silverblueprints.net` with real Apple Developer portal credentials.
