# Code review: working-tree changes (2026-09-15)

Scope: uncommitted changes in the working tree. Auto-provisioned AR/AP contact
sub-accounts, persisted system-account ids on `business`, ledger-sourced customer
statement, docker-compose migrate/ava services, `.env.example`, iCloud backup script,
and the quickstart doc.

Result: 11 confirmed, 6 plausible, 0 refuted. `go build ./...` and `go vet ./...` pass,
sqlc/buf output is in sync, all new Go files carry the Silver Blueprints header.

## Bugs (most severe first)

### 1. Duplicate contact names cannot be created
`internal/server/system_accounts.go:144`

Auto-provisioned sub-accounts are named after `contact.name` and all hang under one
shared AR (or AP) container. `ledger_account_business_parent_name_uindex`
(`migrations/00001_initial.up.sql:280`) is unique on
`(business_id, COALESCE(parent_account_id, 0), name)`, so:

- `contact create --contact-number C-1 --name "John Smith" --customer` followed by
  `contact create --contact-number C-2 --name "John Smith" --customer` fails with
  AlreadyExists carrying a ledger_account message.
- Deactivating a contact only sets `is_active = false` on its account; the name stays
  and the index is not partial, so re-creating a deactivated contact with the same name
  also fails.
- The removed `customer_ledger_account_id` field means there is no workaround.

Fix: include the contact id or contact number in the sub-account name, or exclude
`is_system` rows from the index.

### 2. Customer statement contradicts itself for unposted payments
`internal/reporting/statement.go:184`

Activity and EndingBalance now come solely from `ledger_entry`. A payment created
without `--account` is allowed (`maybePostPayment` returns nil, nil) and still applies
to the invoice, so:

- Invoice INV-1 for 100.00, then `payment create --amount 100.00 --apply <inv>:100.00`
  with no `--account`.
- `balance_due` goes to 0 and the invoice becomes PAID. The Payments section lists
  PAY-1 and aging shows 0 outstanding.
- `loadStatementActivity` finds only the invoice debit and reports EndingBalance
  100.00. At HEAD this was 0.00.

Fix: flag or filter unposted payments in the Payments section, or document that
unposted payments do not affect the ledger-sourced balance. Add a test for an unposted
payment on a statement.

### 3. Self-heal adopts any account with code 1100/2000 unchecked
`internal/server/system_accounts.go:100`

`resolveARContainer`/`resolveAPContainer` adopt whatever `ledger_account` carries code
1100 or 2000 with no `is_container`, `is_active`, or `account_type` check, then pin it
forever via the `IS NULL`-guarded UPDATE. No RPC can reset the id.

- A pre-existing business with a postable non-container 1100 (the old CLAUDE.md
  workflow created exactly that): first `contact create --customer` persists that id and
  every future customer sub-account is parented under a postable, possibly inactive
  account. Nothing validates `parent.is_container`.
- A business whose chart has "Accounts Receivable" at code 1200:
  `GetLedgerAccountByCode('1100')` misses, `CreateLedgerAccount(name='Accounts
  Receivable', parent NULL)` hits the parent/name unique index, and every customer
  create fails AlreadyExists until the user renames the old account (code is not
  updatable).

Fix: require `is_container AND account_type_id matches AND is_active` on the found
row (FailedPrecondition otherwise), and surface a clear error instead of the raw
unique violation.

### 4. Quickstart restore command silently restores nothing
`docs/quickstart.md:163`

The restore line quotes a tilde path (`"~/Library/Mobile Documents/..."`). bash, zsh,
and fish all leave `~` literal inside double quotes, so `gunzip -c` fails with "No such
file", the pipeline status is 0 (pipefail off interactively), `psql` reads empty stdin
and exits 0, and the next documented step (`docker compose up -d --build`) proceeds on
an empty database. The Attachments example three lines later correctly uses `"$HOME/..."`.

Fix: use `"$HOME/Library/..."` on the restore line.

### 5. Backup script leaves orphaned `.partial` files
`scripts/backup-to-icloud.sh:28`

With `set -euo pipefail` and no EXIT trap, a failing `docker compose exec ... pg_dump`
aborts after `> "$tmp"` has created `ava-<stamp>.sql.gz.partial` (gzip writes a 20-byte
empty stream) and before the `mv`. The prune glob `ava-*.sql.gz` never matches
`*.partial`, so one orphan per failed run accumulates in iCloud. Docker Desktop not
running at 02:00 is the case the doc itself calls out.

Reproduced with:

```sh
bash -c 'set -euo pipefail; false | gzip > t.partial; mv t.partial t.gz'
```

Fix: `trap 'rm -f -- "$tmp"' EXIT`, or prune `*.partial` too.

### 6. Vendor creates run the AR path for nothing
`internal/server/system_accounts.go:128`

`getOrCreateContactAccount` always calls `resolveARContainer` first and then overwrites
`containerID, err` with `resolveAPContainer` for vendors. On a legacy business a
`contact create --vendor` does GetBusiness + GetLedgerAccountByCode('1100') + possibly
CreateLedgerAccount('Accounts Receivable') + SetBusinessARAccountID, all discarded. If
the AR create fails (the name collision in item 3) the transaction is aborted and the
surfaced error is the AP side's `25P02 current transaction is aborted` rather than the
real cause. Steady state it is one extra GetBusiness per vendor create.

Fix: `if isCustomerSide { resolveAR } else { resolveAP }`, and rename to
`createContactAccount` (it never looks anything up).

### 7. Guarded Set*AccountID updates bump `business.resource_version`
`sql/queries/business.sql:64`

`bump_resource_version` is unconditional (`NEW.resource_version := OLD.resource_version
+ 1`, migration lines 76-81, trigger at 129-131), so the `IS NULL`-guarded UPDATEs bump
the version whenever they actually match. On a legacy business a MEMBER's first
`contact create` (or an ADMIN's first `close trigger`) bumps `business.resource_version`
as a side effect. An operator holding VERSION 7 then gets Aborted on
`business update --resource-version 7` though no business field changed. The comments
at `system_accounts.go:78` and `business.sql:61` only describe the already-set no-op
path.

Low severity (one-off per legacy business) but real and undocumented.

## Duplication

### 8. `loadStatementActivity` copies `reporting.GeneralLedger`
`internal/reporting/statement.go:184`

Line-for-line the same as `general_ledger.go:18-61`: GetLedgerAccount ->
GetLedgerAccountType -> ListLedgerEntriesForAccount -> NetBalance running loop ->
EndingBalance. Any later change to GL semantics (tie-break ordering, filtering) must be
mirrored or `report customer-statement` and `report general-ledger --account <custAR>`
will disagree. `GeneralLedger` is exported and already called from
`contact_service.go:248`.

Drop-in:

```go
gl, err := GeneralLedger(ctx, q, *customer.LedgerAccountID, start, end)
// map gl.Lines (deref the *string Description) into result.Activity
// result.EndingBalance = gl.EndingBalance
```

### 9. `resolveSystemAccount` copies `periodclose/provision.go`
`internal/server/system_accounts.go:81`

Byte-for-byte identical to `internal/periodclose/provision.go:85` (verified by diff).
`getOrCreateContainerAccount` / `getOrCreateSystemAccount` differ only in
`accountTypeID`, `categoryID`, and `IsContainer`. `periodclose` imports only
sqlcgen/ledgermath/ledgerpost and `server` already imports `periodclose`, so exporting
`periodclose.ResolveSystemAccount` and generalizing
`getOrCreateSystemAccount(ctx, q, businessID, accountTypeID, categoryID, isContainer,
code, name, createdBy)` lets the server copies be deleted. Any fix to the resolve path
(the is_container validation above, or a FOR UPDATE lock for the concurrent first-create
race) otherwise has to land twice.

The same pair makes `CreateBusiness` issue 5 GetBusiness + 4 always-miss code lookups
+ 4 single-column UPDATEs; a fresh business is returned at `resource_version` 5.

## Stale docs

### 10. Docs the diff makes stale
`CLAUDE.md:146` and others

- `docs/schema.md:42-46` business block lists only id/name/currency_code; the four new
  `*_account_id` columns and FKs are missing, despite CLAUDE.md step 1 pointing there.
- `docs/architecture.md:11-18` still says Income Summary / Retained Earnings are looked
  up by fixed code and never mentions AR/AP containers or per-contact sub-accounts.
- `docs/quickstart.md:93` cites a CLAUDE.md section ("keep the server surface small")
  that does not exist.
- `CLAUDE.md:146` ledger-account example still tells users to hand-create an
  "Accounts Receivable" container. With code 1100 that now collides with the
  auto-provisioned account on `ledger_account_business_code_uindex`; with another code
  they silently get a second non-system AR container.
- `docs/schema.md:309` says sub-accounts leave `balance_sheet_category_id` unset by
  convention, while `getOrCreateContactAccount` sets it. Harmless to the balance sheet
  (which skips children) but contradicts the reference.

## Verified but cut (plausible or pre-existing)

- Legacy customers sharing one `ledger_account_id` now see pooled activity in their
  statement (`statement.go:191`).
- A legacy credit-normal customer account gets a sign-flipped EndingBalance
  (`statement.go:212`).
- Two concurrent first `contact create` calls on a legacy business race to insert code
  1100 and the loser fails AlreadyExists (no FOR UPDATE / advisory lock in
  `store.ExecTx`).
- EndingBalance omits the pre-`start` opening balance while aging includes it.
  Pre-existing at HEAD and identical to the general-ledger report, so carried over
  rather than introduced.

## Deployment note

The hand-applied ALTER delta for the four new `business` columns and FKs must reach the
live DB before this binary deploys. `GetBusiness` now selects those columns, so every
RPC would fail on an un-altered DB.
