# Ava Schema Reference

An entity-relationship reference for the `ava` accounting schema defined in
[`migrations/00001_initial.up.sql`](../migrations/00001_initial.up.sql) — the double-entry
ledger at its core, the parties and trading documents built on top of it, and the banking, tax,
and AI-context tables that extend it.

Split into three diagrams by domain. A single-attribute box with just `id PK` is a **stub** —
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
    ledger_account |o--o{ ledger_account : "parent_account_id"
    business ||--o{ ledger_transaction : "business_id"
    ledger_transaction ||--o{ ledger_entry : "ledger_transaction_id"
    ledger_account ||--o{ ledger_entry : "account_id"

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
        int cash_flow_category_id FK
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
```

`default_tax_rate_id` points forward to `tax_rate` (diagram 2) — omitted here to keep this
diagram self-contained.

- **`ledger_account_type`** — a fixed six-row enum (ASSETS, LIABILITIES, EQUITY, REVENUE,
  EXPENSES, TAX_LIABILITY), seeded once. `normal_balance` drives which side (debit or credit) is
  the "natural" positive direction for display.
- **`ledger_account`** — the chart of accounts. `parent_account_id` self-references for a tree;
  `is_system` protects default accounts from deletion or rename; `is_container` marks
  non-postable "folder" nodes used only for grouping; `is_reconcilable` flags which accounts
  (bank, cash) are eligible for statement import.
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
    business ||--o{ payment : "business_id"
    contact ||--o{ payment : "contact_id"
    invoice |o--o{ payment : "invoice_id"
    business ||--o{ tax_rate : "business_id"
    ledger_account ||--o{ tax_rate : "tax_liability_account_id"
    tax_rate |o--o{ ledger_account : "default_tax_rate_id"

    ledger_account {
        int id PK
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
    }
    invoice_line_item {
        bigint id PK
        bigint invoice_id FK
        bigint service_id FK
        bigint tax_rate_id FK
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
  `contact_id` resolves to a customer or vendor depending on type.
- **`payment`** — generalized via `payment_type` (RECEIVED / MADE), tracked independently of
  `invoice_id` — which stays nullable, since a deposit or on-account payment can exist before
  it's applied to any specific invoice.
- **`tax_rate`** — a named rate (e.g. "Standard 20%") tied to its own liability account. Both
  `ledger_account.default_tax_rate_id` and each line item's `tax_rate_id` reference it; a line
  item's own `tax_rate` / `tax_amount` columns still snapshot the rate actually applied, so a
  later change here never rewrites history.

## 3. Banking, AI Context & Attachments

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
        varchar storage_url
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
- **`attachment`** — same polymorphic pattern, for files. `storage_url` for anything migrated
  in from elsewhere currently points at its original hosting — a re-hosting job, not yet done.

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
- `ledger_transaction` has no link back to the `invoice` or `payment` that caused it — the GL
  posting and the business document aren't connected yet.
- `tax_rate` rows aren't auto-wired to any account or line item — manual assignment.
- Semantic search over `entity_context` (pgvector) — deferred until a real retrieval need exists.
- Multi-currency — built once (`currency`, `exchange_rate`, per-entry FX), then removed while
  everything stays USD-only.
