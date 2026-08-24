# Ava Schema Reference

An entity-relationship reference for the `ava` accounting schema defined in
[`migrations/00001_initial.up.sql`](../migrations/00001_initial.up.sql) — the double-entry
ledger at its core, the parties and trading documents built on top of it, and the banking, tax,
and AI-context tables that extend it.

Split into four diagrams by domain. A single-attribute box with just `id PK` is a **stub** —
its full definition lives in another diagram, included only to show the relationship.

> Regenerate this file (and the companion [styled diagram artifact](https://claude.ai/code/artifact/d9c5ecac-77c6-4f70-bbd3-c0d429db9de1))
> after schema changes rather than hand-editing it out of sync.

## 1. Core Ledger & Chart of Accounts

The double-entry spine everything else posts against, plus the account tree and classification
tables that shape it.

```mermaid
erDiagram
    business ||--o{ ledger_account : "business_id"
    ledger_account_type ||--o{ ledger_account : "account_type_id"
    cash_flow_category |o--o{ ledger_account : "cash_flow_category_id"
    balance_sheet_category |o--o{ ledger_account : "balance_sheet_category_id"
    ledger_account |o--o{ ledger_account : "parent_account_id"
    business ||--o{ ledger_transaction : "business_id"
    ledger_transaction ||--o{ ledger_entry : "ledger_transaction_id"
    ledger_account ||--o{ ledger_entry : "account_id"
    business ||--o{ period_close : "business_id"
    ledger_account ||--o{ period_close : "income_summary_account_id"
    ledger_account ||--o{ period_close : "retained_earnings_account_id"
    period_close ||--o{ period_close_entry : "period_close_id"
    ledger_transaction ||--o{ period_close_entry : "ledger_transaction_id"
    ledger_account ||--o{ period_close_entry : "source_account_id"

    business {
        bigint id PK
        varchar name
        varchar currency_code "informational only, always USD"
    }
    ledger_account_type {
        int id PK
        varchar name "ASSETS LIABILITIES EQUITY REVENUE EXPENSES TAX_LIABILITY"
        varchar normal_balance "DEBIT or CREDIT"
    }
    cash_flow_category {
        int id PK
        varchar name "Operating Investing Financing"
    }
    balance_sheet_category {
        int id PK
        varchar name "Long-term Assets, Current Assets & Liabilities, Long-term Liabilities, Capital & Reserves, Opening Balances"
    }
    ledger_account {
        int id PK
        bigint business_id FK
        int account_type_id FK
        int parent_account_id FK
        varchar code UK
        varchar name
        boolean is_system
        boolean is_container
        boolean is_reconcilable
        boolean is_cost_of_goods_sold
        int cash_flow_category_id FK
        int balance_sheet_category_id FK
        bigint default_tax_rate_id FK
    }
    ledger_transaction {
        bigint id PK
        bigint business_id FK
        date transaction_date
        text description
    }
    ledger_entry {
        bigint id PK
        bigint ledger_transaction_id FK
        int account_id FK
        decimal debit_amount
        decimal credit_amount
    }
    period_close {
        bigint id PK
        bigint business_id FK
        date period_start
        date period_end
        int income_summary_account_id FK
        int retained_earnings_account_id FK
        timestamp reversed_at
    }
    period_close_entry {
        bigint id PK
        bigint period_close_id FK
        bigint ledger_transaction_id FK
        int source_account_id FK
    }
```

`default_tax_rate_id` points forward to `tax_rate` (diagram 2) — omitted here to keep this
diagram self-contained.

- **`ledger_account_type`** — a fixed six-row enum (ASSETS, LIABILITIES, EQUITY, REVENUE,
  EXPENSES, TAX_LIABILITY), seeded once. `normal_balance` drives which side (debit or credit) is
  the "natural" positive direction for display.
- **`ledger_account`** — the chart of accounts. `parent_account_id` self-references for a tree;
  `is_system` protects default accounts from deletion or rename; `is_container` marks
  non-postable "folder" nodes used only for grouping; `is_reconcilable` flags which accounts
  (bank, cash) are eligible for statement import; `is_cost_of_goods_sold` splits an
  EXPENSES-type account into Cost of Goods Sold vs. Operating Expenses on the income statement
  (meaningless for any other account type), so Gross Profit (Revenue − COGS) can be computed —
  the standard first subtotal on a US multi-step income statement. `id` is a real identity column
  (unlike `ledger_account_type`/`cash_flow_category`/`balance_sheet_category`, plain
  `INTEGER PRIMARY KEY` since they're fixed, migration-seeded enums the app never inserts into)
  so the API can create accounts — e.g. period-close's Income Summary/Retained Earnings
  provisioning — without hand-rolling ID allocation.
- **`ledger_transaction` + `ledger_entry`** — the double-entry split: `ledger_transaction` is the
  shared event (date, description, reference); `ledger_entry` is each individual debit or credit
  posting under it. A CHECK constraint enforces exactly one side populated per entry, so
  `SUM(debit) = SUM(credit)` is a checkable property of the data, not just a hope.
- **`currency_code`** — single-currency for now. `business.currency_code` is informational only
  (no `currency`/`exchange_rate` tables, no per-account or per-entry currency tracking).
  Everything is assumed USD; multi-currency was designed once already and deliberately stripped
  back out to keep the ledger simple until there's a real need for it.
- **`cash_flow_category`** — Operating / Investing / Financing classification for cash-flow
  statements. Kept separate from `ledger_account_type` because balance-sheet placement is
  already derivable from account type + normal balance, but cash-flow placement is not.
- **`balance_sheet_category`** — presentation-only grouping for the balance sheet report (Long-term
  Assets, Current Assets & Liabilities, Long-term Liabilities, Capital & Reserves, Opening
  Balances), computed by `internal/reporting.BalanceSheet`. Independent of `ledger_account_type`
  for the same reason as `cash_flow_category`: nothing about an ASSETS account says whether it's
  a *current* or *long-term* one (a bank account and a laptop are both ASSETS) — that's a
  judgment call this table captures instead. Which column a line prints in (Asset vs. Liability)
  still comes from `normal_balance`, not this table — a category like "Current Assets &
  Liabilities" deliberately mixes both.
- **`period_close` + `period_close_entry`** — end-of-period consolidation. Closing sweeps every
  REVENUE/EXPENSE account into a per-business Income Summary `ledger_account`, then Income
  Summary into Retained Earnings — two ordinary `is_system` EQUITY accounts, so the closing
  postings are indistinguishable from any other `ledger_transaction`/`ledger_entry` pair.
  `period_close_entry` links each generated transaction back to the account it zeroed, so a close
  can be located or reversed (`reversed_at`) without pattern-matching on description text. A
  trigger (`enforce_period_lock`) hard-locks a business through the latest unreversed
  `period_end`: any `ledger_transaction`/`ledger_entry` insert or edit dated on or before that
  date is rejected, except a close's own postings, which land *before* its `period_close` row is
  inserted and so predate the lock they create.

## 2. Parties & Trading Documents

Who you do business with, and the estimates, invoices, and payments that flow between you —
generalized to carry both accounts-receivable and accounts-payable activity.

```mermaid
erDiagram
    business ||--o{ contact : "business_id"
    ledger_account |o--o{ contact : "ledger_account_id"
    business ||--o{ service : "business_id"
    business ||--o{ estimate : "business_id"
    contact ||--o{ estimate : "customer_id"
    estimate ||--o{ estimate_line_item : "estimate_id"
    service |o--o{ estimate_line_item : "service_id"
    tax_rate |o--o{ estimate_line_item : "tax_rate_id"
    business ||--o{ invoice : "business_id"
    contact ||--o{ invoice : "contact_id"
    estimate |o--o{ invoice : "estimate_id"
    invoice ||--o{ invoice_line_item : "invoice_id"
    service |o--o{ invoice_line_item : "service_id"
    tax_rate |o--o{ invoice_line_item : "tax_rate_id"
    ledger_account |o--o{ invoice_line_item : "ledger_account_id"
    ledger_transaction |o--o{ invoice : "ledger_transaction_id"
    business ||--o{ payment : "business_id"
    contact ||--o{ payment : "contact_id"
    invoice |o--o{ payment : "invoice_id"
    ledger_account |o--o{ payment : "ledger_account_id"
    ledger_transaction |o--o{ payment : "ledger_transaction_id"
    business ||--o{ tax_rate : "business_id"
    ledger_account ||--o{ tax_rate : "tax_liability_account_id"
    tax_rate |o--o{ ledger_account : "default_tax_rate_id"

    ledger_account {
        int id PK
    }
    ledger_transaction {
        bigint id PK
    }
    contact {
        bigint id PK
        bigint business_id FK
        int ledger_account_id FK
        varchar contact_number UK
        boolean is_customer
        boolean is_vendor
        varchar name
    }
    service {
        bigint id PK
        bigint business_id FK
        varchar service_code UK
        decimal retail_price
        decimal cost_price
    }
    tax_rate {
        bigint id PK
        bigint business_id FK
        varchar name
        decimal rate
        int tax_liability_account_id FK
    }
    estimate {
        bigint id PK
        bigint business_id FK
        bigint customer_id FK
        varchar estimate_number UK
        varchar status "DRAFT SENT ACCEPTED DECLINED EXPIRED"
        date expiration_date
    }
    estimate_line_item {
        bigint id PK
        bigint estimate_id FK
        bigint service_id FK
        bigint tax_rate_id FK
        decimal quantity
        decimal unit_price
        decimal line_total
    }
    invoice {
        bigint id PK
        bigint business_id FK
        bigint contact_id FK
        bigint estimate_id FK
        varchar invoice_type "SALES or PURCHASE"
        varchar invoice_number UK
        varchar status "DRAFT SENT PAID OVERDUE CANCELLED"
        date due_date
        decimal paid_amount
        decimal balance_due
        bigint ledger_transaction_id FK
    }
    invoice_line_item {
        bigint id PK
        bigint invoice_id FK
        bigint service_id FK
        bigint tax_rate_id FK
        int ledger_account_id FK
        decimal quantity
        decimal unit_price
        decimal line_total
    }
    payment {
        bigint id PK
        bigint business_id FK
        bigint contact_id FK
        bigint invoice_id FK
        varchar payment_type "RECEIVED or MADE"
        varchar payment_number UK
        decimal amount
        int ledger_account_id FK
        bigint ledger_transaction_id FK
    }
```

`ledger_account` is a stub here — full definition in diagram 1.

- **`contact`** — generalized "customer": `is_customer` / `is_vendor` booleans rather than a
  single type, since a party can be both. `ledger_account_id` optionally links to that party's
  own AR/AP sub-ledger account, for callers that model customers as literal ledger accounts.
- **`estimate`** — deliberately *not* unified with `invoice`. It has no ledger impact — nothing
  is owed until it converts — and its lifecycle genuinely differs: DRAFT → SENT → ACCEPTED /
  DECLINED / EXPIRED, with `expiration_date` rather than a payment `due_date`, and no
  `paid_amount` or `balance_due` at all.
- **`invoice`** — generalized across AR and AP via `invoice_type` rather than split into a
  separate `bill` table — a sales invoice and a vendor bill are the *same kind* of event (real
  ledger impact, same status lifecycle, same due-date/paid-amount shape), unlike estimate.
  `contact_id` resolves to a customer or vendor depending on type. `ledger_transaction_id` links
  back to the GL posting this invoice produced — set once posted, NULL for an unposted (e.g.
  still-DRAFT, or not wired to accounts) invoice.
- **`invoice_line_item.ledger_account_id`** — which revenue (SALES) or expense (PURCHASE)
  account this line posts to, picked per line at entry time rather than defaulted from
  `service` or `business`, matching the existing `contact.ledger_account_id` pattern. An invoice
  posts atomically at creation once every line has this set and its `contact` has its own
  `ledger_account_id` (the AR/AP side); otherwise it stays an unposted document.
- **`payment`** — generalized via `payment_type` (RECEIVED / MADE), tracked independently of
  `invoice_id` — which stays nullable, since a deposit or on-account payment can exist before
  it's applied to any specific invoice. `ledger_account_id` is the cash/bank account a posted
  payment hit (the other side of the contact's AR/AP account); `ledger_transaction_id` links
  back to that posting, same nullable-until-posted convention as `invoice`.
- **`tax_rate`** — a named rate (e.g. "Standard 20%") tied to its own liability account. Both
  `ledger_account.default_tax_rate_id` and each line item's `tax_rate_id` reference it; a line
  item's own `tax_rate` / `tax_amount` columns still snapshot the rate actually applied, so a
  later change here never rewrites history.

## 3. Users & Auth

Application users (authenticated via passkeys/WebAuthn) and the businesses
they can access.

```mermaid
erDiagram
    business |o--o{ app_user : "created_by_user_id"
    business ||--o{ business_user : "business_id"
    app_user ||--o{ business_user : "user_id"
    app_user ||--o{ user_session : "user_id"
    app_user ||--o{ webauthn_credential : "user_id"
    user_session |o--o{ user_session : "replaced_by_session_id"

    business {
        bigint id PK
    }
    app_user {
        bigint id PK
        varchar email UK
        varchar display_name
        boolean is_active
    }
    business_user {
        bigint id PK
        bigint business_id FK
        bigint user_id FK
        varchar role "OWNER ADMIN MEMBER VIEWER"
    }
    user_session {
        bigint id PK
        bigint user_id FK
        varchar refresh_token_hash UK
        timestamp expires_at
        timestamp revoked_at
        bigint replaced_by_session_id FK
    }
    webauthn_credential {
        bigint id PK
        bigint user_id FK
        bytea credential_id UK
        bytea public_key
        bigint sign_count
        varchar name
    }
```

`business` is a stub here — full definition in diagram 1.

- **`app_user`** — authenticated via passkeys (WebAuthn), not a third-party
  OAuth identity provider — no Apple Developer portal setup, and an
  iPhone/Mac passkey backed by iCloud Keychain works through the same
  standard flow, so Apple-device users are covered without a separate
  integration. `email` is the human-facing identifier: used for account
  lookup/display and as the WebAuthn `user.name` during passkey
  registration. The actual credential material lives in
  `webauthn_credential`, never here.
- **`webauthn_credential`** — one row per registered passkey (a user may
  register more than one, e.g. a laptop and a phone). `credential_id`/
  `public_key` are the raw values a WebAuthn relying-party library produces;
  `sign_count` backs its clone-detection check. Cross-device sign-in (scan a
  QR code from your phone) is handled natively by the browser's built-in
  passkey UI — nothing here or in the API implements that transport.
- **`business_user`** — membership + role join table; a user can belong to
  multiple businesses, a business can have multiple users. `role` is a
  `VARCHAR` + `CHECK`, matching the schema's existing string-enum convention
  (`invoice.status`, `payment.payment_type`, ...) rather than a native
  Postgres enum type.
- **`user_session`** — server-side refresh-token storage for avactl/API
  sessions. Access tokens are short-lived signed JWTs, verified in-process on
  every gRPC call with no DB hit; only the long-lived refresh token here is
  server-side and revocable (`refresh_token_hash` stores a digest, never the
  raw token). `replaced_by_session_id` tracks rotation.
- **`created_by_user_id`** — a nullable FK to `app_user`, added to header/
  parent tables across the schema (`business`, `ledger_account`,
  `ledger_transaction`, `period_close`, `contact`, `service`, `tax_rate`,
  `estimate`, `invoice`, `payment`, `bank_statement`, `entity_context`,
  `attachment`) so records can be attributed to the user who created them,
  not just scoped to a business. Stays nullable everywhere — period-close
  postings and any future legacy import have no acting user. Child rows
  (line items, `ledger_entry`, `period_close_entry`,
  `bank_statement_line`) don't get their own column; they inherit
  attribution from their parent.

## 4. Banking, AI Context & Attachments

Reconciliation against real bank statements, and two polymorphic tables that hang supplementary
material off any record elsewhere in the schema.

```mermaid
erDiagram
    business ||--o{ bank_statement : "business_id"
    ledger_account ||--o{ bank_statement : "ledger_account_id"
    bank_statement ||--o{ bank_statement_line : "bank_statement_id"
    ledger_transaction ||--o{ bank_statement_line : "ledger_transaction_id"
    business ||--o{ entity_context : "business_id"
    entity_context |o--o{ entity_context : "superseded_by_id"
    business ||--o{ attachment : "business_id"

    ledger_account {
        int id PK
    }
    ledger_transaction {
        bigint id PK
    }
    bank_statement {
        bigint id PK
        bigint business_id FK
        int ledger_account_id FK
        varchar statement_name
        date statement_date
        decimal opening_balance
        decimal closing_balance
    }
    bank_statement_line {
        bigint id PK
        bigint bank_statement_id FK
        bigint ledger_transaction_id FK
        int display_sequence
    }
    entity_context {
        bigint id PK
        bigint business_id FK
        varchar entity_type
        bigint entity_id
        varchar context_type "summary categorization_hint anomaly user_note"
        text content
        varchar source
        bigint superseded_by_id FK
    }
    attachment {
        bigint id PK
        bigint business_id FK
        varchar entity_type
        bigint entity_id
        varchar original_filename
        varchar storage_key
        varchar content_type
        bigint file_size_bytes
    }
```

`ledger_account` and `ledger_transaction` are stubs — full definitions in diagram 1.
`entity_type` / `entity_id` on the bottom two tables are deliberately **not** drawn as
relationships — see note below.

- **`bank_statement` + `bank_statement_line`** — a statement belongs to one `is_reconcilable`
  ledger account; each line links one cleared `ledger_transaction` to it, with a unique
  constraint on the pair so nothing reconciles twice.
- **`entity_context`** — polymorphic store for AI-generated context (summaries, categorization
  hints, anomaly flags, notes) tied to any record elsewhere in the schema, so a future Claude
  Code session can retrieve prior context about whatever it's working on. `entity_type` +
  `entity_id` deliberately carry **no database-level foreign key**, since they span many
  possible target tables. `superseded_by_id` lets a batch of old rows roll up into one new
  summary without deleting the trail.
- **`attachment`** — same polymorphic pattern, for files. `storage_key` is an object key in an
  internal object-storage backend (SeaweedFS locally, S3-compatible — see
  `internal/storage`, `docker-compose.yml`), not a URL, and is never exposed through the API:
  `AttachmentService.UploadAttachment`/`DownloadAttachment` (`proto/ava/v1/context.proto`) stream
  a file's bytes through gRPC itself, gated by the same `RequireBusinessRole` auth check as every
  other resource, rather than exposing public, unauthenticated attachment URLs.

## Patterns worth carrying forward

- **Generalize with a discriminator** when two things are the *same kind* of event —
  `contact.is_customer`/`is_vendor`, `invoice.invoice_type`, `payment.payment_type`. Keep tables
  separate when they're fundamentally different events, even if the shape looks similar —
  `estimate` vs `invoice`.
- **FK naming tracks generalization.** A column stays role-specific (`estimate.customer_id`)
  until its table is actually generalized, at which point it becomes generic
  (`invoice.contact_id`).
- **Snapshot, don't reference, at the point of sale.** Line items store their own `unit_price`
  and `tax_rate` rather than only a pointer to `service` / `tax_rate`, so a later catalog or
  rate change never silently rewrites a historical document.

## Not yet built

- No `purchase_order` — the AP-side equivalent of `estimate`.
- `tax_rate` rows aren't auto-*assigned* — a line item still has to explicitly pick a
  `tax_rate_id` (and, to post, a `ledger_account_id`); nothing infers either from `service` or
  `ledger_account.default_tax_rate_id` yet.
- Semantic search over `entity_context` (pgvector) — deferred until a real retrieval need exists.
- Multi-currency — built once (`currency`, `exchange_rate`, per-entry FX), then removed while
  everything stays USD-only.
- No fiscal-year-end setting on `business` and no scheduler — closes must be triggered
  explicitly; nothing auto-closes on a recurring date.
- Invoice line items can't be edited/re-posted after creation — no `UpdateInvoice` RPC beyond
  status transitions; correcting a posted invoice today means a genuine reversing entry (the
  same pattern period close reversal uses), not editing the original.
- PURCHASE-side tax is rolled into the expense line rather than split to a liability account
  (`tax_liability_account_id` models tax *collected*, which fits SALES, not tax paid to a
  vendor) — a deliberate simplification, not an oversight.
- Discounts (`estimate.discount_amount` / `invoice.discount_amount`) exist as columns but
  aren't exposed by the API yet — posting a discounted invoice would need an allocation
  decision (a contra-revenue account?) the schema doesn't have an opinion on yet.
