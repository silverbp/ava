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
- `docs/` — architecture notes (`architecture.md`) and schema reference (`schema.md`)

## Development

```sh
make db-up        # start Postgres (+ SeaweedFS) via docker compose
make migrate-up    # apply the schema
make generate      # regenerate proto + sqlc code after touching proto/ or sql/queries/
make build         # go build ./...
make test          # go test ./...
make run           # go run ./cmd/ava
make avactl        # build ./bin/avactl with version info stamped in
make install       # go install avactl (with version stamped) to $GOBIN — prefer this over a bare `go install ./cmd/avactl`
```

`VERSION` = `<VERSION file>.<total commit count>` — advances automatically as commits land, never
hand-bump the patch number.

---

## avactl

`avactl` is the CLI client for the `ava` gRPC API. Binary lives at `internal/avactl/cmd` (built via
`make avactl` / `make install`); source for each resource is one file under
`internal/avactl/cmd/<resource>.go`.

Full command tree at any time: `avactl commands` (flat list, good for grepping). Any command's
flags: `avactl <command> --help`.

### Config & contexts

State lives in `~/.avactl/config` (kubeconfig-style: clusters/users/contexts), never edit it by
hand — use `avactl config`. A context bundles a server address with a business id.

```sh
avactl config set-context dev --server localhost:9090 --insecure --business 1
avactl config set-context prod --server ava.silverblueprints.net:443 --business 1
avactl config use-context dev
avactl config get-contexts     # list contexts
avactl config view             # print resolved config
```

Global flags on every command override the active context for one call, they don't persist:

- `--server <addr>` / `--insecure` (skip TLS — only for a local dev server with no cert)
- `--business <id>`
- `-o, --output table|json|yaml|pdf` (default `table`; `pdf` only works on `report *`, `invoice get`, `estimate get`)

### Auth

```sh
avactl login     # passkey (WebAuthn) ceremony via browser against the current context's server; stores the session in ~/.avactl/config
avactl whoami     # show the signed-in user
```

Requires a context to already exist (`config set-context` first). `~/.avactl/config` ends up
holding a live access/refresh token per user — treat it like any other credentials file, never
commit or paste its contents.

### Conventions that hold across (almost) every resource

- **Aliases**: most resource nouns accept a plural and/or short alias, e.g. `business`/`businesses`/`biz`,
  `contact`/`contacts`, `invoice`/`invoices`/`inv`, `ledger-account`/`ledger-accounts`/`la`,
  `ledger-transaction`/`ledger-transactions`/`lt`, `bank-statement`/`bank-statements`/`bs`,
  `payment`/`payments`/`pay`, `estimate`/`estimates`/`est`, `item`/`items`, `tax-rate`/`tax-rates`.
- **CRUD shape**: `create`, `get <id>`, `list`, `update <id>` (only passed flags change — omitted
  flags leave the field alone), `deactivate <id>` (soft delete; nothing is hard-deleted through the
  CLI).
- **Optimistic concurrency**: mutating commands (`update`, `deactivate`, status transitions like
  `invoice send`) accept `--resource-version <n>` — the `VERSION` column from a prior `get`/`list`.
  Pass it to make the write fail if someone else changed the resource first; omit it to write
  unconditionally.
- **Money and dates**: amounts are decimal strings (`"150.00"`), dates are `YYYY-MM-DD`.
- **`list` visibility**: `list` defaults to active/open items only. Flags widen it per-resource:
  `--inactive` (contact, item), `--all` (ledger-account: include sub-accounts; invoice: include
  paid/cancelled; estimate: include accepted/declined/expired).

### Resource reference

| Resource | Subcommands | Notes |
|---|---|---|
| `business` | create, get, list, update, deactivate, invite create/list/revoke | `create`/`invite create` require global-admin (or OWNER/ADMIN for invites). `invite create` prints a one-time token to hand the invitee yourself — ava never emails it, and it's never shown again. |
| `contact` | create, get, list, update, deactivate | A contact can be `--customer` and/or `--vendor`; each side needs its own ledger account (`--customer-ledger-account` / `--vendor-ledger-account`, AR/AP respectively) before you can invoice against it. |
| `item` | create, get, list, update, deactivate | Catalog entry. `--type SERVICE\|NON_INVENTORY\|INVENTORY`. `--default-ledger-account-id` is **required** — it's the account every invoice line for this item posts to (not overridable per line). `--default-tax-rate-id` / `--price` / `--taxable` / `--name` are the defaults that `estimate`/`invoice` `--line item=<id>` pulls from. Deactivated items can't go on new lines. |
| `ledger-account` | create, get, list, update, deactivate | Chart of accounts. `--account-type` is required: `1=ASSETS 2=LIABILITIES 3=EQUITY 4=REVENUE 5=EXPENSES 6=TAX_LIABILITY`. `--container` marks a non-postable roll-up node (e.g. "Accounts Receivable") with real accounts hung off it via `--parent`. `--reconcilable` makes it eligible for `bank-statement`. |
| `ledger-transaction` | get, list, post | `post` takes ≥2 `--entry account=<id>,debit=<amt>` / `,credit=<amt>` flags; posting is atomic and validated balanced. **No void/reverse RPC exists yet** — fixing a mistake means posting a new transaction with reversed entries, not editing/deleting this one. |
| `estimate` | create, get, list, update-lines, send, accept, decline, expire | Lines: repeat `--line "item=<id>[,desc=...][,qty=][,price=][,taxable][,tax-rate=<id>]"`. `item=` is **required** on every line (no free-text lines); desc/price/taxable/tax-rate default from the item's catalog entry when omitted. Unknown keys are rejected. `update-lines` replaces the *entire* line set. |
| `invoice` | create, get, list, update-lines, send, cancel, mark-overdue | `--type SALES\|PURCHASE`. Same `--line` syntax as `estimate`: `item=` is **required** and the line always posts to that item's `default_ledger_account_id` — there is no `account=` key. The contact needs a matching customer/vendor ledger account. **Creating an invoice posts it to the ledger atomically.** `--estimate <id>` with no `--line` flags converts an estimate's lines over instead of specifying lines by hand. `update-lines` on an already-posted invoice regenerates its linked transaction's entries in place (and needs `item=` on every line, including on pre-catalog invoices). `get -o pdf > file.pdf` renders the invoice as PDF. |
| `payment` | create, get, list | `--apply invoice_id:amount` (repeatable) applies the payment across one or more invoices. Add `--account <id>` (cash/bank account) to post the payment to the ledger atomically in the same call. |
| `bank-statement` | create, get, list, reconcile, unreconciled | `create` needs `--account` (a `--reconcilable` ledger account). `unreconciled --account --through <date>` lists candidate ledger transactions; `reconcile <id> --transaction <id> [--transaction <id> ...]` links them to the statement. |
| `close` | trigger, reverse, list | `trigger --through <date>` closes the books through that date; `reverse <period-close-id>` undoes one; `list` shows close history. |
| `tax-rate` | create, get, list, update, deactivate | `--rate` is a decimal fraction (`0.0825` = 8.25%), posted into `--liability-account` (a `TAX_LIABILITY` ledger account). |
| `report` | balance-sheet, trial-balance, income-statement, general-ledger, customer-statement | All take date range/as-of flags and default to today; `general-ledger` needs `--account`, `customer-statement` needs `--contact`. Support `-o pdf`. |
| `context` | attach, download, get, get-attachment, list, note, remove-attachment | Generic AI/user annotations + file attachments on *any* entity (`--entity-type invoice --entity-id 42`, etc). `note` records a `summary`/`categorization_hint`/`anomaly`/`user_note` (`--context-type`), optionally `--supersedes` an older note. `attach`/`download` stream file bytes through ava's own object storage — there's no direct storage URL. |
| `admin` | grant, revoke | Global-admin-only. There is exactly one global admin at a time; `grant --user <id>` transfers it, `revoke --user <id>` leaves zero until someone is granted it again. |
| `accept-invite` | — | `avactl accept-invite <token>` — redeem a `business invite create` token; you must already be logged in as the invited email. |

### Common workflows

```sh
# Stand up a chart of accounts entry, then post an opening balance
avactl ledger-account create --code 1000 --name Cash --account-type 1 --reconcilable
avactl ledger-account create --code 3000 --name "Opening Balance Equity" --account-type 3
avactl ledger-transaction post --date 2026-01-01 \
  --entry account=1,debit=10000.00 --entry account=2,credit=10000.00

# Customer + catalog item + invoice + payment
avactl contact create --contact-number C-1 --name "Acme Co" --customer-ledger-account 12
avactl item create --code CONSULT --name Consulting --price 150.00 --default-ledger-account-id 40
avactl invoice create --contact 5 --type SALES --date 2026-01-01 --due 2026-01-31 \
  --line "item=71,qty=10"
avactl payment create --contact 5 --type RECEIVED --number PAY-1 --date 2026-01-15 \
  --amount 1500.00 --method CASH --apply 42:1500.00 --account 1

# Estimate → invoice conversion
avactl estimate create --customer 5 --date 2026-01-01 --expires 2026-02-01 \
  --line "item=71,qty=10"
avactl estimate send 12
avactl estimate accept 12
avactl invoice create --contact 5 --type SALES --date 2026-01-05 --due 2026-02-05 --estimate 12

# Reports
avactl report trial-balance --as-of 2026-01-31
avactl report income-statement --start 2026-01-01 --end 2026-01-31
avactl report balance-sheet --as-of 2026-01-31 -o pdf > balance-sheet.pdf

# Bank reconciliation
avactl bank-statement create --account 1 --name "Jan 2026" --date 2026-01-31 --opening 10000.00 --closing 11500.00
avactl bank-statement unreconciled --account 1 --through 2026-01-31
avactl bank-statement reconcile 3 --transaction 12 --transaction 13
```
