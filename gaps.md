# Ava Edit/Delete Functionality Gaps

This document collects gaps in Ava's edit/delete surface found by operating the real books through
`avactl`, not by reading the code. Every item below traces back to a specific correction that had to be
made this session: 2026 tax payments miscoded against `2100 Sales Tax` instead of `2110 Sales Tax
Payable` / `3010 Owners Pay`, and two 2022/2023 purchases miscoded as paydowns against the `2010
Synchrony Bank` liability. Fixing those exposed real friction in the ledger's append-only design — see
[`docs/architecture.md`](docs/architecture.md) and [`docs/schema.md`](docs/schema.md) for how the
current model works. Each gap below cites the friction that surfaced it and what closing it would take.

### 1. No first-class correct/reclassify operation

Every mistake found this session was "right amount, wrong account" — never a wrong amount, never an
extra or missing entry. The only tool available for fixing that is `ledger-transaction post` with a
hand-computed offsetting entry, which means the operator has to work out the compensating debit/credit
themselves and risks getting the sign backwards.

- **Evidence**: fixing the sales tax miscoding required manually deriving a credit to account `81` and a
  debit to account `85` for $278.90, then repeating the derivation for a second entry and a second
  target account (`3010 Owners Pay`) for the $250.00 payment.
- **What would close it**: a `ledger-transaction reclassify --entry-id <id> --to-account <id>` command
  that reads the original entry's amount and side, and generates the compensating entry server-side
  instead of leaving the arithmetic to the caller.

### 2. No link between a correcting entry and what it corrects

Once a compensating entry is posted, its relationship to the mistake it fixes lives only in free text in
the `description` field. There's no structured way to ask "what corrected this transaction" or "has this
transaction already been corrected."

- **Evidence**: the correcting entries posted this session (transaction `2299`, the sales-tax-payable
  zero-out; transaction `2300`, the Synchrony reclass to Job Supplies) reference their originals only in
  prose — "corrects txn 2288," "misposted to 13090" — not through any queryable field.
- **What would close it**: a `corrects_transaction_id` column on `ledger_transaction`, so tooling and
  reports can walk correction chains instead of parsing descriptions.

### 3. Amending a just-created transaction still requires raw SQL

Ava has no amend/edit RPC for `ledger_transaction` at all — the design is deliberately append-only. That
holds up fine for anything that's actually been relied on downstream, but it also blocks the case where
a transaction was posted seconds ago, by the same session, in an open period, referenced by nothing yet.

- **Evidence**: transaction `2299` was posted with the wrong `transaction_date` and needed correcting
  less than a minute later. It had not appeared in any report and the period was open, but the only way
  to fix it was a direct `UPDATE` against the production `ledger_transaction` table. This is the riskiest
  operation performed this session, and it only worked because the period-lock trigger
  (`enforce_period_lock`) doesn't fire outside a closed period — the fix bypassed application-level
  validation entirely rather than being blocked by it.
- **What would close it**: a narrow amend window — the transaction's own creator, before any close or
  report has referenced it, within some short time bound — that lets a real RPC (not a raw SQL session)
  fix an honest, harmless typo.

### 4. No anomaly / sign-check report

Both underlying bugs this session were caught by a human eyeballing a full trial balance and noticing a
liability account sitting on the wrong side of zero. Nothing in Ava's reporting surface does that check
automatically.

- **Evidence**: `2100 Sales Tax` and `2010 Synchrony Bank` were both flagged only because the trial
  balance was read line by line, not because any tool surfaced them.
- **What would close it**: a report (e.g. `report anomalies`) that flags liability, equity, and revenue
  accounts carrying an unexpected debit balance (and asset/expense accounts carrying an unexpected
  credit balance), so bank-feed miscoding gets caught without a manual review.

### 5. No search/filter on `ledger-transaction list`

`ledger-transaction list` takes no flags at all. Finding the history needed to understand the sales-tax
pattern — the recurring "VAT Return" and "Sales Tax Payment" entries — required pulling the entire
general-ledger report for an account and grepping the JSON by hand.

- **Evidence**: reconstructing the multi-year sales-tax accrual/remittance pattern meant fetching
  `report general-ledger` for accounts `81` and `85` across the account's whole history and filtering the
  output in a Python one-liner, rather than querying for it directly.
- **What would close it**: basic filters on `ledger-transaction list` — `--account`, `--start`/`--end`,
  `--description-contains` — so this kind of lookup doesn't require going through the reporting layer.

### 6. `close reverse` semantics with stacked closes are undocumented

The manifest exposes `close reverse`, but the 2022 period has closes layered on top of it through 2024,
and nothing in `avactl commands -o json` or `--help` output says whether reversing an older period
cascades to the closes stacked above it, or what happens to the retained-earnings sweeps those later
closes already generated.

- **Evidence**: that ambiguity is precisely why the Synchrony correction was posted as a new entry in the
  open 2025 period instead of reopening 2022/2023 to fix the original transactions in place — reopening
  a period several closes deep was judged too risky to attempt without documentation.
- **What would close it**: either document the cascade behavior explicitly, or have the RPC itself
  enforce/guard it (e.g. refuse to reverse a period with dependent closes still in place, or reverse them
  in order automatically).

### 7. No bulk reclassify by criteria

There's no way to correct more than one transaction at a time by matching criteria rather than by ID.

- **Evidence**: this session's fixes only involved two mistaken entries and were correctable by hand, but
  the same bank-feed miscoding pattern (a vendor's transactions landing against the wrong account for an
  extended period) would require one hand-rolled correcting entry per mistake with today's tooling.
- **What would close it**: builds directly on gap 1 — a bulk form of the reclassify operation that takes
  an account, a date range, and optionally a description filter, and moves every matching entry to a new
  account in one operation.

Gaps 1 and 2 are the highest-leverage, lowest-risk places to start: a reclassify RPC and a
`corrects_transaction_id` column would have covered every correction made this session without any raw
SQL, any hand-derived arithmetic, or any loss of audit trail.
