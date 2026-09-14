# DRY / code-reuse review

Written 2026-09-13 from a read of `internal/server`, `internal/avactl`, `proto/`, and `sql/queries`.
The goal behind it: as the app grows, keep each change small enough that an AI session (or a
person) can make it by reading one file and a checklist, not by reading a 2000-line sibling and
pattern-matching. Items are ordered by context saved per hour of work; the first three are
mechanical and carry no behavior risk.

Line numbers are as of commit `1433628` and will drift.

**Status (2026-09-13): implemented.** Every finding below has landed; the "Recommended order"
at the end doubles as the change list. Where to look now: `internal/server/resources.go`
(finding 1), `document_lines.go` and `NewDocumentLineItem` (2), `children.go` and the
`*ByIDs` queries (3), one file per service plus `convert.go`/`pgerror.go` (4),
`internal/avactl/cmd/actions.go` + `flags.go` + `resource/columns.go` (5-7), `cmd/report.go`
and `server/reporting_*.go` (8), `internal/server/*_integration_test.go` on embedded Postgres
(10), and the "Adding a resource" checklist in `CLAUDE.md`. `moneypb.ToProto` is now
infallible and `parseDateFlag` names its flag (11). `internal/periodclose` had its own entry
helpers; they now come from `internal/ledgerpost` (the "Not verified" item).

## What is already in good shape

- **CLI verb scaffolding.** `newGetCmd` / `newListCmd` / `newMutateCmd` / `newVersionedMutateCmd`
  in `internal/avactl/cmd/actions.go` plus `resource.Doc` mean a noun's get/list/deactivate is
  wired in one line each.
- **Cross-cutting server helpers exist once.** `translatePgError`, `translateUpdateError`,
  `expectedResourceVersion`, `resolveReversalDate`, `ledgerpost.ReverseTransaction`,
  `ledgermath`, `moneypb`, `datepb`.
- **Shared line math.** `computeLines`, `lookupLineItem`, `taxBreakdown`, `debitCreditFor` are
  already shared between estimate and invoice.

## Repeat counts that drove the priorities

Across `internal/server` and `internal/avactl/cmd`, non-test files:

| Pattern | Copies |
|---|---|
| load row, map `ErrNoRows` to NotFound, then `RequireBusinessRole` | 62 |
| `status.Errorf(codes.Internal, "converting x: %v", err)` | 58 |
| CLI `strconv.ParseInt` id parsing with "invalid x id" message | 42 |
| CLI `Flags().Changed(...)` field-to-pointer mapping | 45 |
| CLI `fmt.Sprintf("%d"/"%v")` column lambdas | 39 |
| `auth.RequireBusinessRole` call sites | 75 |
| `translatePgError(err)` call sites | 110 |

## Findings

### 1. Every mutating handler opens with the same 8-line prologue

See `internal/server/party_service.go:134` (UpdateContact) and
`internal/server/trading_service.go:1062` (UpdateInvoice). The shape is always:

```go
existing, err := s.store.Queries.GetX(ctx, req.GetId())
if err != nil {
    if errors.Is(err, pgx.ErrNoRows) {
        return nil, status.Errorf(codes.NotFound, "x %d not found", req.GetId())
    }
    return nil, translatePgError(err)
}
if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
    return nil, err
}
```

**Fix.** A generic `loadForBusiness[T]` taking a getter, a `businessOf` accessor, a kind name, and
a role turns each into one line. Define the getter/accessor once per resource as a small table
(`var invoiceRes = resourceDef[sqlcgen.Invoice]{kind: "invoice", get: ..., businessOf: ...}`).

**Bonus.** `internal/server/entity_ref.go:38` (`validateEntityRef`, an 11-case switch) and
`requireLedgerAccountInBusiness` at `party_service.go:467` both become callers of that same
table. Today there are three different mechanisms for "does this id belong to this business":
that switch, the party helper, and the `GetItemInBusiness` / `GetTaxRateInBusiness` sqlc
queries. Pick one.

### 2. Estimate and invoice line pipelines are near-identical twins

`resolveEstimateLine` at `trading_service.go:222` and `resolveInvoiceLine` at
`trading_service.go:289` differ only by `LedgerAccountID`. The same holds for:

- `resolvedEstimateLine` vs `resolvedInvoiceLine` (structs)
- `resolveEstimateLines` vs `resolveInvoiceLines`
- `estimateLineItemToProto` vs `invoiceLineItemToProto`
- `estimateTaxBreakdown` vs `invoiceTaxBreakdown`
- CLI `newEstimateLineItems` (`estimate.go:197`) vs `newInvoiceLineItems` (`invoice.go:231`)

Inside the four create / update-lines handlers, these blocks are each copied four times:

- the `inputs := make([]lineInput, len(resolved))` mapping loop
  (`trading_service.go:527`, `:658`, `:970`, `:1299`)
- the "convert three totals, `firstErr`, then insert each line" block
  (`trading_service.go:566`, `:692`, `:1015`, `:1341`)

**Root cause.** `proto/ava/v1/trading.proto` defines `NewEstimateLineItem` and
`NewInvoiceLineItem` field-for-field identical.

**Fix.** One shared `NewDocumentLineItem` proto message used by both request types. Then a single
`resolveLines` and a single `replaceDocumentLines(ctx, q, businessID, lines, insertFn)` serve all
four handlers, and the CLI maps `--line` once. Estimate line = invoice line minus
`ledger_account_id`, which is not in the request message anyway (it always comes from the item).

### 3. List handlers with children are N+1, each written by hand

- `ListEstimates` at `trading_service.go:489` (one `ListEstimateLineItems` per row)
- `ListInvoices` at `trading_service.go:901`
- `ListPayments` at `trading_service.go:1893`
- `contactToProto` at `party_service.go:200` (two queries per contact: customer + vendor)
- `bankStatementToProto` at `banking_service.go:393` (four queries per statement)

`ListLedgerTransactions` at `ledger_service.go:219` already does it right: one
`ListLedgerEntriesByTransactionIDs` batch query and a map keyed by parent id.

**Fix.** Add `*ByParentIDs` sqlc queries for line items, payment applications, customer/vendor,
bank statement lines, and a small `groupBy[K, V]` helper. Ideally put the whole "list parents,
batch-load children, convert" shape behind one function so a new resource cannot pick the slow
form.

### 4. File layout hides shared code inside unrelated services

- `trading_service.go` is 2230 lines: three gRPC services, the line pipeline, invoice posting,
  and PDF party formatting.
- `party_service.go` holds three services; `context_service.go` holds two.
- `timestampProto` and `derefOr` live at `business_service.go:400` but are used everywhere.
- `closeErrorStatus` at `period_close_service.go:141` is really the generic "ExecTx error to
  status" translator, used by 18 sites in trading and ledger.
- `parseDecimalOrDefault` (`trading_service.go:123`) and `parseDecimalOrZero`
  (`ledger_service.go:412`) are the same function.
- `createDecimalEntry`, `verifyTransactionBalances`, `debitCreditFor` (`trading_service.go:378`,
  `:418`, `:407`) belong in `internal/ledgerpost` next to `ReverseTransaction`.
- `decimalToProto` (`reporting_service.go:247`) belongs in `moneypb`.

**Fix.** One gRPC service per file. Shared helpers in `convert.go` (proto conversion helpers),
`pgerror.go` (all error translation, with `closeErrorStatus` renamed to something like
`txErrorStatus`), `document_lines.go`, `invoice_posting.go`, `pdf_party.go`. For context cost
this matters more than line count: a session touching payments currently reads 2230 lines, and a
session looking for helpers will not find them in `business_service.go`.

### 5. CLI per-noun closures are pure ceremony

`internal/avactl/cmd/contact.go:52` through `:86` is three functions that parse an id, call the
client, and unwrap the response. Every noun has the same three. There are 13 copies of the
`items := make([]proto.Message, len(resp.GetXs()))` loop.

**Fix.**

- `parseID(noun, s string) (int64, error)` with the standard error message.
- `toMessages[T proto.Message](xs []T) []proto.Message`.
- Change `getFunc` / `mutateFunc` in `actions.go:24` to take `id int64`, so the generic command
  parses and no closure does.

One wrinkle: ledger-account ids are `int32` while everything else is `int64`.

### 6. Table columns repeat the type assertion per cell

Each `resource.Noun` has five to eight lambdas that all cast `v.(*avav1.X)` and format with
`fmt.Sprintf`. Example: `contact.go:20`.

**Fix.** A generic `resource.Columns[T proto.Message]` with constructors like `ID(func(T) int64)`,
`Str(func(T) string)`, `Bool(...)`, `Money(func(T) *avav1.Decimal)`. Each noun becomes a
one-line-per-column table and the 39 `Sprintf` lambdas go away.

### 7. Create/update flag mapping scales with field count

The `if x != "" { req.X = &x }` blocks in `contact.go:112` and the `cmd.Flags().Changed("...")`
blocks at `contact.go:203` are the part of each noun that grows as resources gain fields.

**Fix, small.** Helpers like `optString(cmd, "name", &name) *string` (nil unless the flag was
passed) and `optDecimal`, `optInt32`, `optDate`. Halves the lines with no framework.

**Fix, larger.** A declarative field table per command that both registers the flag and applies
it to the request. Removes the blocks entirely but is a mini-framework; decide separately.

### 8. Reports are five copies of one shape

`reporting_pdf.go:18` onward repeats role check, date parsing, business-name lookup, compute,
render for each of the five reports. The JSON handlers in `reporting_service.go` repeat the
first three steps. CLI `report.go` is 364 lines for five commands.

**Fix.** A per-report spec (name, which date flags, compute fn, render fn) would serve both
handlers and the CLI. The distinct proto response types mean each report still needs a small
adapter, so this is medium value; the CLI side (parse `--as-of` / `--start` / `--end` once) is
the better first target.

### 9. The proto style is the multiplier, and it should stay

58 per-resource verb request messages with 69 copies of the resource_version comment. Typed
messages are what make the API self-describing for agents and `avactl commands`, so this is the
right trade. Be aware that every new resource costs four proto messages, four handlers, four CLI
functions, and four queries. The fixes above shrink each of those, not the count.

### 10. No safety net for the server refactors

Six test files exist, all pure-function unit tests; none exercise a handler. Findings 1 through 3
touch every handler.

**Fix.** A table-driven integration test that runs create / get / update / deactivate per
resource against embedded-postgres (works on this machine without Docker). It both protects the
refactor and is a DRY win in its own right.

### 11. Small items

- `parseDateFlag` at `flags.go:28` hardcodes `--date` in its error even when the flag is
  `--due`, `--expires`, or `--as-of`. Take the flag name as a parameter.
- `moneypb.ToProto` returns an error that, for a NUMERIC that already scanned successfully,
  essentially cannot occur. The 58 "converting x" wrappers exist to carry it. Either wrap
  centrally inside the `*ToProto` functions (return a status error) or accept an infallible
  variant.

## Not verified

Whether `internal/periodclose` writes ledger entries through its own code path or through the
trading helpers. If it has its own, that is a fourth copy to fold into `ledgerpost`.

## Recommended order

1. **Split files** (finding 4). Mechanical, zero behavior risk, immediate context reduction.
2. **CLI generics** (findings 5, 6). Small, safe, covered by existing `flags_test.go` style tests.
3. **Add a "how to add a resource" checklist to CLAUDE.md** naming the helpers from steps 1
   and 2. Cheapest lever on AI cost: a future session follows the list instead of
   reverse-engineering a sibling file.
4. **Integration test harness** (finding 10), then `loadForBusiness` and the resource table
   (finding 1), with `entity_ref.go` collapsed onto it.
5. **Unify the line-item proto message** and collapse the estimate/invoice pipeline (finding 2).
6. **Batch child loading helper** (finding 3).
7. **CLI flag-to-request helpers** (finding 7).
8. **Report spec table** (finding 8), CLI side first.
