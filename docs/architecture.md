# Ava Architecture Notes

Design notes for application-layer work that isn't visible in the schema itself. Currently just
one section: what's left to make period close (`migrations/00001_initial.up.sql`, `period_close` /
`period_close_entry` / `enforce_period_lock`, documented in
[`schema.md`](schema.md#1-core-ledger--chart-of-accounts)) actually usable. `cmd/ava` and
`internal/` are still empty scaffolding — none of this exists in code yet.

## Period close

The schema enforces correctness (balanced entries, the hard lock) but has no opinion on process.
Six pieces of application logic are needed to drive it.

### 1. System account provisioning

Every business needs its own `Income Summary` and `Retained Earnings` `ledger_account` rows
(`account_type_id = 3` EQUITY, `is_system = true`) before it can ever be closed —
`period_close.income_summary_account_id` / `retained_earnings_account_id` are `NOT NULL`, so
there's no lazy-create path. Do this as part of business creation/provisioning, alongside whatever
else seeds a new business's default chart of accounts. Fixed `code` values (e.g.
`INCOME-SUMMARY`, `RETAINED-EARNINGS`) let the close service look them up without a naming
convention on `name`.

### 2. The close service

The core transaction script, run inside a single DB transaction (required — see the lock-ordering
note in `schema.md`):

1. Resolve `period_start` = the day after the business's last unreversed `period_close.period_end`
   (or business inception if none), `period_end` = the requested close date.
2. For every `REVENUE`/`EXPENSE` `ledger_account` on the business, sum `ledger_entry` activity in
   `[period_start, period_end]`. Entries before `period_start` are already covered by a prior
   close and are unreachable anyway once locked.
3. For each account with nonzero net movement, insert one `ledger_transaction` dated
   `period_end` with two `ledger_entry` rows: one zeroing the account (opposite side of its
   normal balance), one hitting Income Summary.
4. Insert one more `ledger_transaction` sweeping Income Summary's resulting balance into
   Retained Earnings.
5. Insert the `period_close` row, then one `period_close_entry` per transaction from steps 3–4
   (`source_account_id` = the account each transaction zeroed — including Income Summary itself
   for the step-4 transaction).

Steps 3–4 must run *before* step 5 — the lock trigger only sees already-committed `period_close`
rows, so a close's own postings land while still unlocked.

### 3. Reversal / reopen

To undo a close: set `period_close.reversed_at`, which immediately drops that `period_end` from
the `MAX(period_end)` the lock trigger checks. Post genuine reversing entries for every
transaction in that close's `period_close_entry` rows rather than editing or deleting them —
consistent with the schema's existing soft-delete-over-mutation convention elsewhere. A
subsequent close can then re-cover the same (or an extended) range.

### 4. Guard rails the schema doesn't enforce

- **Contiguity**: nothing stops `period_start` from skipping or overlapping a prior close's
  range. The close service should reject a gap/overlap rather than rely on the DB.
- **Idempotency**: the partial unique index on `(business_id, period_end) WHERE reversed_at IS
  NULL` will reject a second concurrent close on the same date, but the service should check for
  an existing unreversed close first and fail with a clear error rather than surfacing a raw
  constraint violation.
- **Balance sanity**: `ledger_entry`'s debit/credit CHECK already guarantees each generated
  transaction balances; the service should still assert the sum of all step-3 zeroing entries
  equals the step-4 Income Summary sweep, as a defense-in-depth check on its own arithmetic.

### 5. API / MCP surface

Not designed yet. At minimum: trigger a close for a business + date, reverse a given
`period_close`, and list a business's close history. Given `entity_context` already anticipates an
MCP server as a consumer of this schema, period close is a natural candidate for an MCP tool
(e.g. "close the books through Dec 31") rather than (or in addition to) a REST endpoint.

### 6. Reporting integration

- A balance sheet as of any date *after* the latest close reads `Retained Earnings` directly.
- A balance sheet as of a date *within* the still-open current period needs an "current period
  earnings" line computed live (sum of REVENUE/EXPENSE activity since the last close) — the
  standard accounting treatment, and pure query logic, no schema change needed.
- P&L reports are unaffected either way; they already read `ledger_entry` over an arbitrary date
  range regardless of close state.

### Deferred / not required to ship this

- **Scheduling**: nothing here auto-triggers a close on a fiscal year end. `business` has no
  fiscal-year-end column today; adding one plus a scheduled job is a reasonable follow-up but not
  needed for the close service to work when invoked explicitly.
- **Automated tests**: the trigger logic was verified manually against a local Postgres
  container (full close doesn't self-lock; boundary-dated inserts rejected; post-close dates
  accepted; edits to locked entries rejected) but there's no test suite yet, since there's no
  application code to test against.
