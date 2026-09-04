// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/datepb"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/ledgermath"
	"github.com/silverbp/ava/internal/moneypb"
	"github.com/silverbp/ava/internal/pdf"
)

// ============================================================================
// Line-item computation shared by EstimateService and InvoiceService.
// ============================================================================

// lineInput is the subset of a NewEstimateLineItem/NewInvoiceLineItem this
// package needs to compute a line — the two proto messages are distinct Go
// types, so callers map into this before calling computeLines.
type lineInput struct {
	Quantity  *avav1.Decimal
	UnitPrice *avav1.Decimal
	IsTaxable bool
	TaxRateID *int64
}

type computedLine struct {
	Quantity     pgtype.Numeric
	UnitPrice    pgtype.Numeric
	LineSubtotal pgtype.Numeric
	TaxRate      pgtype.Numeric
	TaxAmount    pgtype.Numeric
	LineTotal    pgtype.Numeric
}

// computeLines fills in each line's subtotal/tax/total server-side (never
// trusting client-supplied aggregates), snapshotting the tax_rate actually
// applied onto the line per the schema's own convention ("snapshot, don't
// reference, at the point of sale" — docs/schema.md).
func computeLines(ctx context.Context, q *sqlcgen.Queries, inputs []lineInput) (lines []computedLine, subtotal, totalTax, total decimal.Decimal, err error) {
	subtotal, totalTax = decimal.Zero, decimal.Zero

	for i, in := range inputs {
		qty, err := parseDecimalOrDefault(in.Quantity, "1")
		if err != nil {
			return nil, decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("line %d: invalid quantity: %w", i, err)
		}
		price, err := parseDecimalOrDefault(in.UnitPrice, "0")
		if err != nil {
			return nil, decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("line %d: invalid unit_price: %w", i, err)
		}
		lineSubtotal := qty.Mul(price).Round(2)

		taxRate, taxAmount := decimal.Zero, decimal.Zero
		if in.IsTaxable && in.TaxRateID != nil {
			tr, err := q.GetTaxRate(ctx, *in.TaxRateID)
			if err != nil {
				return nil, decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("line %d: %w", i, err)
			}
			taxRate, err = ledgermath.NumericToDecimal(tr.Rate)
			if err != nil {
				return nil, decimal.Zero, decimal.Zero, decimal.Zero, err
			}
			taxAmount = lineSubtotal.Mul(taxRate).Round(2)
		}
		lineTotal := lineSubtotal.Add(taxAmount)

		qtyNum, err1 := ledgermath.DecimalToNumeric(qty)
		priceNum, err2 := ledgermath.DecimalToNumeric(price)
		subtotalNum, err3 := ledgermath.DecimalToNumeric(lineSubtotal)
		taxRateNum, err4 := ledgermath.DecimalToNumeric(taxRate)
		taxAmountNum, err5 := ledgermath.DecimalToNumeric(taxAmount)
		totalNum, err6 := ledgermath.DecimalToNumeric(lineTotal)
		if err := firstErr(err1, err2, err3, err4, err5, err6); err != nil {
			return nil, decimal.Zero, decimal.Zero, decimal.Zero, err
		}

		lines = append(lines, computedLine{
			Quantity:     qtyNum,
			UnitPrice:    priceNum,
			LineSubtotal: subtotalNum,
			TaxRate:      taxRateNum,
			TaxAmount:    taxAmountNum,
			LineTotal:    totalNum,
		})
		subtotal = subtotal.Add(lineSubtotal)
		totalTax = totalTax.Add(taxAmount)
	}

	total = subtotal.Add(totalTax)
	return lines, subtotal, totalTax, total, nil
}

func parseDecimalOrDefault(d *avav1.Decimal, def string) (decimal.Decimal, error) {
	if d == nil || d.GetValue() == "" {
		return decimal.NewFromString(def)
	}
	return decimal.NewFromString(d.GetValue())
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// ============================================================================
// Item-catalog resolution shared by EstimateService and InvoiceService.
//
// Every estimate/invoice line must reference a catalog item from the document's own
// business (Xero/QuickBooks style - no free-text lines). The item supplies the line's
// defaults (description/unit_price/is_taxable/tax_rate_id, each overridable per line) and,
// for invoices, *the* ledger account the line posts to (never overridable). Enforcement is
// API-level only: the item_id/ledger_account_id columns stay nullable for pre-catalog rows.
// ============================================================================

// lookupLineItem fetches the item a line references, scoped to businessID. It is the single
// gate that makes items mandatory: a missing item_id, an item that isn't in this business
// (or doesn't exist), and an inactive item each get a distinct status so the caller knows
// which to fix. Statuses returned from inside ExecTx pass through unchanged
// (db.Store.ExecTx returns fn's error as-is; closeErrorStatus keeps an existing status).
func lookupLineItem(ctx context.Context, q *sqlcgen.Queries, businessID int64, lineIdx int, itemID int64) (sqlcgen.Item, error) {
	if itemID == 0 {
		return sqlcgen.Item{}, status.Errorf(codes.InvalidArgument, "line %d: item_id is required", lineIdx)
	}
	item, err := q.GetItemInBusiness(ctx, sqlcgen.GetItemInBusinessParams{ID: itemID, BusinessID: businessID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.Item{}, status.Errorf(codes.InvalidArgument, "line %d: item %d not found in business %d", lineIdx, itemID, businessID)
		}
		return sqlcgen.Item{}, translatePgError(err)
	}
	if err := checkLineItemUsable(lineIdx, item); err != nil {
		return sqlcgen.Item{}, err
	}
	return item, nil
}

// checkLineItemUsable is the DB-free half of lookupLineItem: an item that has been
// deactivated can't go on a new line (existing lines that already reference it are
// untouched - they're history). Split out so it's unit-testable.
func checkLineItemUsable(lineIdx int, item sqlcgen.Item) error {
	if !item.IsActive {
		return status.Errorf(codes.FailedPrecondition, "line %d: item %d (%s) is inactive", lineIdx, item.ID, item.ItemCode)
	}
	return nil
}

// itemLineDefaults is the subset of an item catalog row a line falls back to.
type itemLineDefaults struct {
	Name            string
	LedgerAccountID *int32
	UnitPrice       *avav1.Decimal
	IsTaxable       bool
	TaxRateID       *int64
}

func itemLineDefaultsFor(item sqlcgen.Item) (itemLineDefaults, error) {
	price, err := moneypb.ToProto(item.RetailPrice)
	if err != nil {
		return itemLineDefaults{}, err
	}
	return itemLineDefaults{
		Name:            item.Name,
		LedgerAccountID: item.DefaultLedgerAccountID,
		UnitPrice:       price,
		IsTaxable:       item.IsTaxable,
		TaxRateID:       item.DefaultTaxRateID,
	}, nil
}

// resolvedEstimateLine is a NewEstimateLineItem after applying item-catalog defaults (see
// resolveEstimateLine) - description/unit_price/is_taxable/tax_rate_id may have originated from
// the request or the item, but from here on every caller treats them as final. ItemID is a
// pointer only because the column is nullable; it's always set here.
type resolvedEstimateLine struct {
	ItemID      *int64
	LineNumber  int32
	Description string
	Quantity    *avav1.Decimal
	UnitPrice   *avav1.Decimal
	IsTaxable   bool
	TaxRateID   *int64
}

// resolveEstimateLine applies item's defaults to li: description falls back to the item's
// name, unit_price/is_taxable/tax_rate_id to retail_price/is_taxable/default_tax_rate_id. An
// explicit value on the line always wins (docs/schema.md, "item"). Pure - item has already
// been fetched and vetted by lookupLineItem.
func resolveEstimateLine(lineIdx int, li *avav1.NewEstimateLineItem, item sqlcgen.Item) (resolvedEstimateLine, error) {
	defaults, err := itemLineDefaultsFor(item)
	if err != nil {
		return resolvedEstimateLine{}, status.Errorf(codes.Internal, "line %d: item %d: %v", lineIdx, item.ID, err)
	}
	itemID := item.ID
	r := resolvedEstimateLine{
		ItemID:      &itemID,
		LineNumber:  li.LineNumber,
		Description: li.Description,
		Quantity:    li.Quantity,
		UnitPrice:   li.UnitPrice,
		IsTaxable:   li.GetIsTaxable(),
		TaxRateID:   li.TaxRateId,
	}
	if r.Description == "" {
		r.Description = defaults.Name
	}
	if r.UnitPrice == nil {
		r.UnitPrice = defaults.UnitPrice
	}
	if li.IsTaxable == nil {
		r.IsTaxable = defaults.IsTaxable
	}
	if r.TaxRateID == nil && r.IsTaxable {
		r.TaxRateID = defaults.TaxRateID
	}
	return r, nil
}

// resolveEstimateLines is lookupLineItem + resolveEstimateLine over every line of a request,
// scoped to the estimate's business.
func resolveEstimateLines(ctx context.Context, q *sqlcgen.Queries, businessID int64, raw []*avav1.NewEstimateLineItem) ([]resolvedEstimateLine, error) {
	resolved := make([]resolvedEstimateLine, len(raw))
	for i, li := range raw {
		item, err := lookupLineItem(ctx, q, businessID, i, li.GetItemId())
		if err != nil {
			return nil, err
		}
		r, err := resolveEstimateLine(i, li, item)
		if err != nil {
			return nil, err
		}
		resolved[i] = r
	}
	return resolved, nil
}

// resolvedInvoiceLine is the NewInvoiceLineItem equivalent of resolvedEstimateLine, additionally
// carrying ledger_account_id (estimates have no ledger impact, so estimate_line_item has no such
// column - see docs/schema.md, "estimate"). Both pointers are always set here; they're pointers
// only because the columns are nullable.
type resolvedInvoiceLine struct {
	ItemID          *int64
	LedgerAccountID *int32
	LineNumber      int32
	Description     string
	Quantity        *avav1.Decimal
	UnitPrice       *avav1.Decimal
	IsTaxable       bool
	TaxRateID       *int64
}

// resolveInvoiceLine is resolveEstimateLine plus the ledger account, which is *always* the
// item's default_ledger_account_id - there is no per-line override (NewInvoiceLineItem has no
// such field). An item without one can't be invoiced, so that's rejected up front rather than
// producing a line the ledger posting would then choke on. Pure, like resolveEstimateLine.
func resolveInvoiceLine(lineIdx int, li *avav1.NewInvoiceLineItem, item sqlcgen.Item) (resolvedInvoiceLine, error) {
	defaults, err := itemLineDefaultsFor(item)
	if err != nil {
		return resolvedInvoiceLine{}, status.Errorf(codes.Internal, "line %d: item %d: %v", lineIdx, item.ID, err)
	}
	if defaults.LedgerAccountID == nil {
		return resolvedInvoiceLine{}, status.Errorf(codes.FailedPrecondition,
			"line %d: item %d (%s) has no default_ledger_account_id - set one on the item before invoicing it", lineIdx, item.ID, item.ItemCode)
	}
	itemID := item.ID
	r := resolvedInvoiceLine{
		ItemID:          &itemID,
		LedgerAccountID: defaults.LedgerAccountID,
		LineNumber:      li.LineNumber,
		Description:     li.Description,
		Quantity:        li.Quantity,
		UnitPrice:       li.UnitPrice,
		IsTaxable:       li.GetIsTaxable(),
		TaxRateID:       li.TaxRateId,
	}
	if r.Description == "" {
		r.Description = defaults.Name
	}
	if r.UnitPrice == nil {
		r.UnitPrice = defaults.UnitPrice
	}
	if li.IsTaxable == nil {
		r.IsTaxable = defaults.IsTaxable
	}
	if r.TaxRateID == nil && r.IsTaxable {
		r.TaxRateID = defaults.TaxRateID
	}
	return r, nil
}

// resolveInvoiceLines is lookupLineItem + resolveInvoiceLine over every line of a request,
// scoped to the invoice's business.
func resolveInvoiceLines(ctx context.Context, q *sqlcgen.Queries, businessID int64, raw []*avav1.NewInvoiceLineItem) ([]resolvedInvoiceLine, error) {
	resolved := make([]resolvedInvoiceLine, len(raw))
	for i, li := range raw {
		item, err := lookupLineItem(ctx, q, businessID, i, li.GetItemId())
		if err != nil {
			return nil, err
		}
		r, err := resolveInvoiceLine(i, li, item)
		if err != nil {
			return nil, err
		}
		resolved[i] = r
	}
	return resolved, nil
}

// newInvoiceLineItemsFromEstimate converts an accepted estimate's line items into the
// NewInvoiceLineItem shape CreateInvoice expects, carrying over item_id and the
// description/quantity/price/tax fields the estimate already resolved. The ledger account is
// never carried (estimate_line_item has no such column); resolveInvoiceLines takes it from the
// item's *current* default_ledger_account_id at invoice time, same as any hand-entered line.
// A pre-catalog estimate line with no item_id becomes item_id 0 and is rejected by
// lookupLineItem ("item_id is required") - it has to be re-pointed at a real item first.
func newInvoiceLineItemsFromEstimate(estLines []sqlcgen.EstimateLineItem) ([]*avav1.NewInvoiceLineItem, error) {
	out := make([]*avav1.NewInvoiceLineItem, len(estLines))
	for i, eli := range estLines {
		quantity, err := moneypb.ToProto(eli.Quantity)
		if err != nil {
			return nil, fmt.Errorf("estimate line %d: quantity: %w", i, err)
		}
		unitPrice, err := moneypb.ToProto(eli.UnitPrice)
		if err != nil {
			return nil, fmt.Errorf("estimate line %d: unit_price: %w", i, err)
		}
		isTaxable := eli.IsTaxable
		var itemID int64
		if eli.ItemID != nil {
			itemID = *eli.ItemID
		}
		out[i] = &avav1.NewInvoiceLineItem{
			ItemId:      itemID,
			LineNumber:  eli.LineNumber,
			Description: eli.Description,
			Quantity:    quantity,
			UnitPrice:   unitPrice,
			IsTaxable:   &isTaxable,
			TaxRateId:   eli.TaxRateID,
		}
	}
	return out, nil
}

func createDecimalEntry(ctx context.Context, q *sqlcgen.Queries, businessID, txnID int64, accountID int32, debit, credit decimal.Decimal) error {
	debitNum, err := ledgermath.DecimalToNumeric(debit)
	if err != nil {
		return err
	}
	creditNum, err := ledgermath.DecimalToNumeric(credit)
	if err != nil {
		return err
	}
	_, err = q.CreateLedgerEntry(ctx, sqlcgen.CreateLedgerEntryParams{
		BusinessID:          businessID,
		LedgerTransactionID: txnID,
		AccountID:           accountID,
		DebitAmount:         debitNum,
		CreditAmount:        creditNum,
	})
	return err
}

func verifyTransactionBalances(ctx context.Context, q *sqlcgen.Queries, txnID int64) error {
	entries, err := q.ListLedgerEntriesByTransaction(ctx, txnID)
	if err != nil {
		return err
	}
	totalDebit, totalCredit := decimal.Zero, decimal.Zero
	for _, e := range entries {
		d, err := ledgermath.NumericToDecimal(e.DebitAmount)
		if err != nil {
			return err
		}
		c, err := ledgermath.NumericToDecimal(e.CreditAmount)
		if err != nil {
			return err
		}
		totalDebit = totalDebit.Add(d)
		totalCredit = totalCredit.Add(c)
	}
	if !totalDebit.Equal(totalCredit) {
		return fmt.Errorf("posting produced unbalanced entries: total debit %s != total credit %s (this is a bug, not bad input)", totalDebit, totalCredit)
	}
	return nil
}

// ============================================================================
// EstimateService — no ledger impact by design (docs/schema.md).
// ============================================================================

type estimateService struct {
	avav1.UnimplementedEstimateServiceServer
	store *db.Store
}

func newEstimateService(store *db.Store) *estimateService {
	return &estimateService{store: store}
}

func (s *estimateService) GetEstimate(ctx context.Context, req *avav1.GetEstimateRequest) (*avav1.GetEstimateResponse, error) {
	e, err := s.store.Queries.GetEstimate(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "estimate %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, e.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	lineItems, err := s.store.Queries.ListEstimateLineItems(ctx, e.ID)
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := estimateToProto(e, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting estimate: %v", err)
	}
	return &avav1.GetEstimateResponse{Estimate: pb}, nil
}

func (s *estimateService) ListEstimates(ctx context.Context, req *avav1.ListEstimatesRequest) (*avav1.ListEstimatesResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListEstimates(ctx, sqlcgen.ListEstimatesParams{
		BusinessID: req.GetBusinessId(),
		IncludeAll: req.GetIncludeAll(),
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListEstimatesResponse{}
	for _, e := range rows {
		lineItems, err := s.store.Queries.ListEstimateLineItems(ctx, e.ID)
		if err != nil {
			return nil, translatePgError(err)
		}
		pb, err := estimateToProto(e, lineItems)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting estimate: %v", err)
		}
		resp.Estimates = append(resp.Estimates, pb)
	}
	return resp, nil
}

func (s *estimateService) CreateEstimate(ctx context.Context, req *avav1.CreateEstimateRequest) (*avav1.CreateEstimateResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if len(req.GetLineItems()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one line item is required")
	}
	if req.GetEstimateDate() == nil || req.GetExpirationDate() == nil {
		return nil, status.Error(codes.InvalidArgument, "estimate_date and expiration_date are required")
	}

	var (
		estimate  sqlcgen.Estimate
		lineItems []sqlcgen.EstimateLineItem
	)
	err := s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		resolved, err := resolveEstimateLines(ctx, q, req.GetBusinessId(), req.GetLineItems())
		if err != nil {
			return err
		}
		inputs := make([]lineInput, len(resolved))
		for i, r := range resolved {
			inputs[i] = lineInput{Quantity: r.Quantity, UnitPrice: r.UnitPrice, IsTaxable: r.IsTaxable, TaxRateID: r.TaxRateID}
		}
		computed, subtotal, totalTax, total, err := computeLines(ctx, q, inputs)
		if err != nil {
			return err
		}

		claimed, err := q.ConsumeNextEstimateNumber(ctx, req.GetBusinessId())
		if err != nil {
			return err
		}
		estimateNumber := fmt.Sprintf("%s%d", derefOr(claimed.Prefix, "EST"), claimed.ClaimedNumber)

		subtotalNum, e1 := ledgermath.DecimalToNumeric(subtotal)
		totalTaxNum, e2 := ledgermath.DecimalToNumeric(totalTax)
		totalNum, e3 := ledgermath.DecimalToNumeric(total)
		if err := firstErr(e1, e2, e3); err != nil {
			return err
		}

		estimate, err = q.CreateEstimate(ctx, sqlcgen.CreateEstimateParams{
			BusinessID:      req.GetBusinessId(),
			CustomerID:      req.GetCustomerId(),
			EstimateNumber:  estimateNumber,
			EstimateDate:    datepb.ToPgDate(req.GetEstimateDate()),
			ExpirationDate:  datepb.ToPgDate(req.GetExpirationDate()),
			Subtotal:        subtotalNum,
			TotalTaxAmount:  totalTaxNum,
			TotalAmount:     totalNum,
			Notes:           req.Notes,
			Terms:           req.Terms,
			CreatedByUserID: &u.ID,
		})
		if err != nil {
			return err
		}

		for i, cl := range computed {
			src := resolved[i]
			li, err := q.CreateEstimateLineItem(ctx, sqlcgen.CreateEstimateLineItemParams{
				EstimateID:   estimate.ID,
				ItemID:       src.ItemID,
				LineNumber:   src.LineNumber,
				Description:  src.Description,
				Quantity:     cl.Quantity,
				UnitPrice:    cl.UnitPrice,
				LineSubtotal: cl.LineSubtotal,
				IsTaxable:    src.IsTaxable,
				TaxRateID:    src.TaxRateID,
				TaxRate:      cl.TaxRate,
				TaxAmount:    cl.TaxAmount,
				LineTotal:    cl.LineTotal,
			})
			if err != nil {
				return err
			}
			lineItems = append(lineItems, li)
		}
		return nil
	})
	if err != nil {
		return nil, closeErrorStatus(err)
	}

	pb, err := estimateToProto(estimate, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting estimate: %v", err)
	}
	return &avav1.CreateEstimateResponse{Estimate: pb}, nil
}

func (s *estimateService) UpdateEstimateStatus(ctx context.Context, req *avav1.UpdateEstimateStatusRequest) (*avav1.UpdateEstimateStatusResponse, error) {
	existing, err := s.store.Queries.GetEstimate(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "estimate %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}

	updated, err := s.store.Queries.UpdateEstimateStatus(ctx, sqlcgen.UpdateEstimateStatusParams{
		ID:              req.GetId(),
		Status:          req.GetStatus(),
		ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, "estimate", req.GetId(), req.GetResourceVersion())
	}
	lineItems, err := s.store.Queries.ListEstimateLineItems(ctx, updated.ID)
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := estimateToProto(updated, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting estimate: %v", err)
	}
	return &avav1.UpdateEstimateStatusResponse{Estimate: pb}, nil
}

// UpdateEstimateLineItems replaces an estimate's entire line item set and
// recomputes its totals — see the proto doc for why this is a full
// replace rather than a per-line patch.
func (s *estimateService) UpdateEstimateLineItems(ctx context.Context, req *avav1.UpdateEstimateLineItemsRequest) (*avav1.UpdateEstimateLineItemsResponse, error) {
	existing, err := s.store.Queries.GetEstimate(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "estimate %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}
	if len(req.GetLineItems()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one line item is required")
	}

	var (
		estimate  sqlcgen.Estimate
		lineItems []sqlcgen.EstimateLineItem
	)
	err = s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		resolved, err := resolveEstimateLines(ctx, q, existing.BusinessID, req.GetLineItems())
		if err != nil {
			return err
		}
		inputs := make([]lineInput, len(resolved))
		for i, r := range resolved {
			inputs[i] = lineInput{Quantity: r.Quantity, UnitPrice: r.UnitPrice, IsTaxable: r.IsTaxable, TaxRateID: r.TaxRateID}
		}
		computed, subtotal, totalTax, total, err := computeLines(ctx, q, inputs)
		if err != nil {
			return err
		}

		subtotalNum, e1 := ledgermath.DecimalToNumeric(subtotal)
		totalTaxNum, e2 := ledgermath.DecimalToNumeric(totalTax)
		totalNum, e3 := ledgermath.DecimalToNumeric(total)
		if err := firstErr(e1, e2, e3); err != nil {
			return err
		}

		// First write to the estimate row: checks the caller's resource_version
		// and takes the row lock the line-item replace below runs under, so a
		// stale caller fails here before touching anything.
		estimate, err = q.UpdateEstimateTotals(ctx, sqlcgen.UpdateEstimateTotalsParams{
			ID:              req.GetId(),
			Subtotal:        subtotalNum,
			TotalTaxAmount:  totalTaxNum,
			TotalAmount:     totalNum,
			ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
		})
		if err != nil {
			return translateUpdateError(err, "estimate", req.GetId(), req.GetResourceVersion())
		}

		if err := q.DeleteEstimateLineItems(ctx, req.GetId()); err != nil {
			return err
		}

		for i, cl := range computed {
			src := resolved[i]
			li, err := q.CreateEstimateLineItem(ctx, sqlcgen.CreateEstimateLineItemParams{
				EstimateID:   estimate.ID,
				ItemID:       src.ItemID,
				LineNumber:   src.LineNumber,
				Description:  src.Description,
				Quantity:     cl.Quantity,
				UnitPrice:    cl.UnitPrice,
				LineSubtotal: cl.LineSubtotal,
				IsTaxable:    src.IsTaxable,
				TaxRateID:    src.TaxRateID,
				TaxRate:      cl.TaxRate,
				TaxAmount:    cl.TaxAmount,
				LineTotal:    cl.LineTotal,
			})
			if err != nil {
				return err
			}
			lineItems = append(lineItems, li)
		}
		return nil
	})
	if err != nil {
		return nil, closeErrorStatus(err)
	}

	pb, err := estimateToProto(estimate, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting estimate: %v", err)
	}
	return &avav1.UpdateEstimateLineItemsResponse{Estimate: pb}, nil
}

func (s *estimateService) GetEstimatePdf(ctx context.Context, req *avav1.GetEstimatePdfRequest) (*avav1.GetEstimatePdfResponse, error) {
	e, err := s.store.Queries.GetEstimate(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "estimate %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, e.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}

	lineItems, err := s.store.Queries.ListEstimateLineItems(ctx, e.ID)
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := estimateToProto(e, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting estimate: %v", err)
	}

	business, err := s.store.Queries.GetBusiness(ctx, e.BusinessID)
	if err != nil {
		return nil, translatePgError(err)
	}
	customer, err := s.store.Queries.GetContact(ctx, e.CustomerID)
	if err != nil {
		return nil, translatePgError(err)
	}
	breakdown, err := s.estimateTaxBreakdown(ctx, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "computing tax breakdown: %v", err)
	}

	content, err := pdf.RenderEstimate(businessParty(business), billToParty(customer), pb, breakdown)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering pdf: %v", err)
	}
	return &avav1.GetEstimatePdfResponse{Content: content}, nil
}

// estimateTaxBreakdown maps an estimate's line items into taxBreakdown's
// shared shape — see taxBreakdown.
func (s *estimateService) estimateTaxBreakdown(ctx context.Context, lineItems []sqlcgen.EstimateLineItem) ([]pdf.TaxBreakdownRow, error) {
	lines := make([]taxBreakdownLine, len(lineItems))
	for i, li := range lineItems {
		lines[i] = taxBreakdownLine{TaxRateID: li.TaxRateID, LineSubtotal: li.LineSubtotal, TaxAmount: li.TaxAmount, LineTotal: li.LineTotal}
	}
	return taxBreakdown(ctx, s.store.Queries, lines)
}

func estimateToProto(e sqlcgen.Estimate, lineItems []sqlcgen.EstimateLineItem) (*avav1.Estimate, error) {
	subtotal, err := moneypb.ToProto(e.Subtotal)
	if err != nil {
		return nil, err
	}
	totalTax, err := moneypb.ToProto(e.TotalTaxAmount)
	if err != nil {
		return nil, err
	}
	total, err := moneypb.ToProto(e.TotalAmount)
	if err != nil {
		return nil, err
	}
	pb := &avav1.Estimate{
		Id:              e.ID,
		BusinessId:      e.BusinessID,
		CustomerId:      e.CustomerID,
		EstimateNumber:  e.EstimateNumber,
		EstimateDate:    datepb.ToProto(e.EstimateDate),
		ExpirationDate:  datepb.ToProto(e.ExpirationDate),
		Subtotal:        subtotal,
		TotalTaxAmount:  totalTax,
		TotalAmount:     total,
		Status:          e.Status,
		Notes:           e.Notes,
		Terms:           e.Terms,
		CreatedByUserId: e.CreatedByUserID,
		CreatedAt:       timestampProto(e.CreatedAt),
		ResourceVersion: e.ResourceVersion,
	}
	for _, li := range lineItems {
		pli, err := estimateLineItemToProto(li)
		if err != nil {
			return nil, err
		}
		pb.LineItems = append(pb.LineItems, pli)
	}
	return pb, nil
}

func estimateLineItemToProto(li sqlcgen.EstimateLineItem) (*avav1.EstimateLineItem, error) {
	quantity, err := moneypb.ToProto(li.Quantity)
	if err != nil {
		return nil, err
	}
	unitPrice, err := moneypb.ToProto(li.UnitPrice)
	if err != nil {
		return nil, err
	}
	lineSubtotal, err := moneypb.ToProto(li.LineSubtotal)
	if err != nil {
		return nil, err
	}
	taxAmount, err := moneypb.ToProto(li.TaxAmount)
	if err != nil {
		return nil, err
	}
	lineTotal, err := moneypb.ToProto(li.LineTotal)
	if err != nil {
		return nil, err
	}
	return &avav1.EstimateLineItem{
		Id:           li.ID,
		EstimateId:   li.EstimateID,
		ItemId:       li.ItemID,
		LineNumber:   li.LineNumber,
		Description:  li.Description,
		Quantity:     quantity,
		UnitPrice:    unitPrice,
		LineSubtotal: lineSubtotal,
		IsTaxable:    li.IsTaxable,
		TaxRateId:    li.TaxRateID,
		TaxAmount:    taxAmount,
		LineTotal:    lineTotal,
	}, nil
}

// ============================================================================
// InvoiceService — optionally posts to the ledger (see proto comment).
// ============================================================================

type invoiceService struct {
	avav1.UnimplementedInvoiceServiceServer
	store *db.Store
}

func newInvoiceService(store *db.Store) *invoiceService {
	return &invoiceService{store: store}
}

func (s *invoiceService) GetInvoice(ctx context.Context, req *avav1.GetInvoiceRequest) (*avav1.GetInvoiceResponse, error) {
	inv, err := s.store.Queries.GetInvoice(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "invoice %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, inv.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	lineItems, err := s.store.Queries.ListInvoiceLineItems(ctx, inv.ID)
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := invoiceToProto(inv, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting invoice: %v", err)
	}
	return &avav1.GetInvoiceResponse{Invoice: pb}, nil
}

func (s *invoiceService) ListInvoices(ctx context.Context, req *avav1.ListInvoicesRequest) (*avav1.ListInvoicesResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListInvoices(ctx, sqlcgen.ListInvoicesParams{
		BusinessID: req.GetBusinessId(),
		IncludeAll: req.GetIncludeAll(),
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListInvoicesResponse{}
	for _, inv := range rows {
		lineItems, err := s.store.Queries.ListInvoiceLineItems(ctx, inv.ID)
		if err != nil {
			return nil, translatePgError(err)
		}
		pb, err := invoiceToProto(inv, lineItems)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting invoice: %v", err)
		}
		resp.Invoices = append(resp.Invoices, pb)
	}
	return resp, nil
}

func (s *invoiceService) CreateInvoice(ctx context.Context, req *avav1.CreateInvoiceRequest) (*avav1.CreateInvoiceResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if req.GetInvoiceType() != "SALES" && req.GetInvoiceType() != "PURCHASE" {
		return nil, status.Error(codes.InvalidArgument, "invoice_type must be SALES or PURCHASE")
	}
	if len(req.GetLineItems()) == 0 && req.EstimateId == nil {
		return nil, status.Error(codes.InvalidArgument, "at least one line item is required")
	}
	if req.GetInvoiceType() == "PURCHASE" && req.GetInvoiceNumber() == "" {
		return nil, status.Error(codes.InvalidArgument, "invoice_number is required for PURCHASE invoices")
	}
	if req.GetInvoiceDate() == nil || req.GetDueDate() == nil {
		return nil, status.Error(codes.InvalidArgument, "invoice_date and due_date are required")
	}

	var (
		invoice   sqlcgen.Invoice
		lineItems []sqlcgen.InvoiceLineItem
	)
	err := s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		rawLineItems := req.GetLineItems()
		if len(rawLineItems) == 0 {
			est, err := q.GetEstimate(ctx, req.GetEstimateId())
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return status.Errorf(codes.InvalidArgument, "estimate %d not found", req.GetEstimateId())
				}
				return err
			}
			if est.BusinessID != req.GetBusinessId() {
				return status.Errorf(codes.InvalidArgument, "estimate %d does not belong to business %d", req.GetEstimateId(), req.GetBusinessId())
			}
			estLines, err := q.ListEstimateLineItems(ctx, est.ID)
			if err != nil {
				return err
			}
			if len(estLines) == 0 {
				return status.Errorf(codes.InvalidArgument, "estimate %d has no line items", est.ID)
			}
			rawLineItems, err = newInvoiceLineItemsFromEstimate(estLines)
			if err != nil {
				return err
			}
		}

		resolved, err := resolveInvoiceLines(ctx, q, req.GetBusinessId(), rawLineItems)
		if err != nil {
			return err
		}
		inputs := make([]lineInput, len(resolved))
		for i, r := range resolved {
			inputs[i] = lineInput{Quantity: r.Quantity, UnitPrice: r.UnitPrice, IsTaxable: r.IsTaxable, TaxRateID: r.TaxRateID}
		}
		computed, subtotal, totalTax, total, err := computeLines(ctx, q, inputs)
		if err != nil {
			return err
		}

		invoiceNumber := req.GetInvoiceNumber()
		if req.GetInvoiceType() == "SALES" {
			claimed, err := q.ConsumeNextInvoiceNumber(ctx, req.GetBusinessId())
			if err != nil {
				return err
			}
			invoiceNumber = fmt.Sprintf("%s%d", derefOr(claimed.Prefix, "INV"), claimed.ClaimedNumber)
		}

		subtotalNum, e1 := ledgermath.DecimalToNumeric(subtotal)
		totalTaxNum, e2 := ledgermath.DecimalToNumeric(totalTax)
		totalNum, e3 := ledgermath.DecimalToNumeric(total)
		if err := firstErr(e1, e2, e3); err != nil {
			return err
		}

		invoice, err = q.CreateInvoice(ctx, sqlcgen.CreateInvoiceParams{
			BusinessID:      req.GetBusinessId(),
			ContactID:       req.GetContactId(),
			InvoiceType:     req.GetInvoiceType(),
			EstimateID:      req.EstimateId,
			InvoiceNumber:   invoiceNumber,
			InvoiceDate:     datepb.ToPgDate(req.GetInvoiceDate()),
			DueDate:         datepb.ToPgDate(req.GetDueDate()),
			Subtotal:        subtotalNum,
			TotalTaxAmount:  totalTaxNum,
			TotalAmount:     totalNum,
			BalanceDue:      totalNum,
			Notes:           req.Notes,
			Terms:           req.Terms,
			CreatedByUserID: &u.ID,
		})
		if err != nil {
			return err
		}

		for i, cl := range computed {
			src := resolved[i]
			li, err := q.CreateInvoiceLineItem(ctx, sqlcgen.CreateInvoiceLineItemParams{
				InvoiceID:       invoice.ID,
				ItemID:          src.ItemID,
				LedgerAccountID: src.LedgerAccountID,
				LineNumber:      src.LineNumber,
				Description:     src.Description,
				Quantity:        cl.Quantity,
				UnitPrice:       cl.UnitPrice,
				LineSubtotal:    cl.LineSubtotal,
				IsTaxable:       src.IsTaxable,
				TaxRateID:       src.TaxRateID,
				TaxRate:         cl.TaxRate,
				TaxAmount:       cl.TaxAmount,
				LineTotal:       cl.LineTotal,
			})
			if err != nil {
				return err
			}
			lineItems = append(lineItems, li)
		}

		txnID, err := postInvoiceLedger(ctx, q, req.GetBusinessId(), invoice, lineItems, &u.ID)
		if err != nil {
			return err
		}
		invoice, err = q.SetInvoiceLedgerTransaction(ctx, sqlcgen.SetInvoiceLedgerTransactionParams{ID: invoice.ID, LedgerTransactionID: &txnID})
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, closeErrorStatus(err)
	}

	pb, err := invoiceToProto(invoice, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting invoice: %v", err)
	}
	return &avav1.CreateInvoiceResponse{Invoice: pb}, nil
}

func (s *invoiceService) UpdateInvoiceStatus(ctx context.Context, req *avav1.UpdateInvoiceStatusRequest) (*avav1.UpdateInvoiceStatusResponse, error) {
	existing, err := s.store.Queries.GetInvoice(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "invoice %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}

	updated, err := s.store.Queries.UpdateInvoiceStatus(ctx, sqlcgen.UpdateInvoiceStatusParams{
		ID:              req.GetId(),
		Status:          req.GetStatus(),
		ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, "invoice", req.GetId(), req.GetResourceVersion())
	}
	lineItems, err := s.store.Queries.ListInvoiceLineItems(ctx, updated.ID)
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := invoiceToProto(updated, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting invoice: %v", err)
	}
	return &avav1.UpdateInvoiceStatusResponse{Invoice: pb}, nil
}

// UpdateInvoiceLineItems replaces an invoice's entire line item set and
// recomputes its totals — see the proto doc for why this is a full replace
// rather than a per-line patch. If the invoice is already posted to the
// ledger, its entries are regenerated in place (repostInvoiceLedger) rather
// than rejecting the edit.
func (s *invoiceService) UpdateInvoiceLineItems(ctx context.Context, req *avav1.UpdateInvoiceLineItemsRequest) (*avav1.UpdateInvoiceLineItemsResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	existing, err := s.store.Queries.GetInvoice(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "invoice %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}
	if len(req.GetLineItems()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one line item is required")
	}

	paidAmount, err := ledgermath.NumericToDecimal(existing.PaidAmount)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "reading paid_amount: %v", err)
	}

	var (
		invoice   sqlcgen.Invoice
		lineItems []sqlcgen.InvoiceLineItem
	)
	err = s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		resolved, err := resolveInvoiceLines(ctx, q, existing.BusinessID, req.GetLineItems())
		if err != nil {
			return err
		}
		inputs := make([]lineInput, len(resolved))
		for i, r := range resolved {
			inputs[i] = lineInput{Quantity: r.Quantity, UnitPrice: r.UnitPrice, IsTaxable: r.IsTaxable, TaxRateID: r.TaxRateID}
		}
		computed, subtotal, totalTax, total, err := computeLines(ctx, q, inputs)
		if err != nil {
			return err
		}
		balanceDue := total.Sub(paidAmount)
		if balanceDue.IsNegative() {
			return fmt.Errorf("new total %s is less than the %s already paid on this invoice", total, paidAmount)
		}

		subtotalNum, e1 := ledgermath.DecimalToNumeric(subtotal)
		totalTaxNum, e2 := ledgermath.DecimalToNumeric(totalTax)
		totalNum, e3 := ledgermath.DecimalToNumeric(total)
		balanceDueNum, e4 := ledgermath.DecimalToNumeric(balanceDue)
		if err := firstErr(e1, e2, e3, e4); err != nil {
			return err
		}

		// First write to the invoice row: checks the caller's resource_version
		// and takes the row lock the line-item replace and ledger (re)post
		// below run under, so a stale caller fails here before touching
		// anything. SetInvoiceLedgerTransaction later in this transaction
		// bumps the version again - the returned invoice carries the final one.
		invoice, err = q.UpdateInvoiceTotals(ctx, sqlcgen.UpdateInvoiceTotalsParams{
			ID:              req.GetId(),
			Subtotal:        subtotalNum,
			TotalTaxAmount:  totalTaxNum,
			TotalAmount:     totalNum,
			BalanceDue:      balanceDueNum,
			ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
		})
		if err != nil {
			return translateUpdateError(err, "invoice", req.GetId(), req.GetResourceVersion())
		}

		if err := q.DeleteInvoiceLineItems(ctx, req.GetId()); err != nil {
			return err
		}

		for i, cl := range computed {
			src := resolved[i]
			li, err := q.CreateInvoiceLineItem(ctx, sqlcgen.CreateInvoiceLineItemParams{
				InvoiceID:       invoice.ID,
				ItemID:          src.ItemID,
				LedgerAccountID: src.LedgerAccountID,
				LineNumber:      src.LineNumber,
				Description:     src.Description,
				Quantity:        cl.Quantity,
				UnitPrice:       cl.UnitPrice,
				LineSubtotal:    cl.LineSubtotal,
				IsTaxable:       src.IsTaxable,
				TaxRateID:       src.TaxRateID,
				TaxRate:         cl.TaxRate,
				TaxAmount:       cl.TaxAmount,
				LineTotal:       cl.LineTotal,
			})
			if err != nil {
				return err
			}
			lineItems = append(lineItems, li)
		}

		if existing.LedgerTransactionID == nil {
			txnID, err := postInvoiceLedger(ctx, q, existing.BusinessID, invoice, lineItems, &u.ID)
			if err != nil {
				return err
			}
			invoice, err = q.SetInvoiceLedgerTransaction(ctx, sqlcgen.SetInvoiceLedgerTransactionParams{ID: invoice.ID, LedgerTransactionID: &txnID})
			if err != nil {
				return err
			}
		} else {
			if err := repostInvoiceLedger(ctx, q, existing.BusinessID, *existing.LedgerTransactionID, invoice, lineItems); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, closeErrorStatus(err)
	}

	pb, err := invoiceToProto(invoice, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting invoice: %v", err)
	}
	return &avav1.UpdateInvoiceLineItemsResponse{Invoice: pb}, nil
}

func (s *invoiceService) GetInvoicePdf(ctx context.Context, req *avav1.GetInvoicePdfRequest) (*avav1.GetInvoicePdfResponse, error) {
	inv, err := s.store.Queries.GetInvoice(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "invoice %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, inv.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}

	lineItems, err := s.store.Queries.ListInvoiceLineItems(ctx, inv.ID)
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := invoiceToProto(inv, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting invoice: %v", err)
	}

	business, err := s.store.Queries.GetBusiness(ctx, inv.BusinessID)
	if err != nil {
		return nil, translatePgError(err)
	}
	contact, err := s.store.Queries.GetContact(ctx, inv.ContactID)
	if err != nil {
		return nil, translatePgError(err)
	}
	breakdown, err := s.invoiceTaxBreakdown(ctx, lineItems)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "computing tax breakdown: %v", err)
	}

	content, err := pdf.RenderInvoice(businessParty(business), billToParty(contact), pb, breakdown)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering pdf: %v", err)
	}
	return &avav1.GetInvoicePdfResponse{Content: content}, nil
}

// taxBreakdownLine is the subset of an EstimateLineItem/InvoiceLineItem
// taxBreakdown needs — the two proto messages are distinct Go types, so
// callers map into this first, same convention as lineInput/computeLines.
type taxBreakdownLine struct {
	TaxRateID    *int64
	LineSubtotal pgtype.Numeric
	TaxAmount    pgtype.Numeric
	LineTotal    pgtype.Numeric
}

// taxBreakdown groups line items by tax rate, in the order each rate first
// appears, and sums their net/tax/total amounts — a PDF renders one row per
// group instead of a per-line tax column. Shared by estimateService and
// invoiceService.
func taxBreakdown(ctx context.Context, q *sqlcgen.Queries, lines []taxBreakdownLine) ([]pdf.TaxBreakdownRow, error) {
	type group struct {
		label           string
		net, tax, total decimal.Decimal
	}
	const noTaxKey = int64(0) // tax_rate.id is a BIGSERIAL, so 0 never occurs — safe sentinel for "no tax rate".

	groups := map[int64]*group{}
	var order []int64
	rateNames := map[int64]string{}

	for _, li := range lines {
		key := noTaxKey
		if li.TaxRateID != nil {
			key = *li.TaxRateID
		}
		g, ok := groups[key]
		if !ok {
			label := ""
			if key != noTaxKey {
				name, ok := rateNames[key]
				if !ok {
					tr, err := q.GetTaxRate(ctx, key)
					if err != nil {
						return nil, err
					}
					name = tr.Name
					rateNames[key] = name
				}
				label = name
			}
			g = &group{label: label}
			groups[key] = g
			order = append(order, key)
		}

		net, err := ledgermath.NumericToDecimal(li.LineSubtotal)
		if err != nil {
			return nil, err
		}
		tax, err := ledgermath.NumericToDecimal(li.TaxAmount)
		if err != nil {
			return nil, err
		}
		total, err := ledgermath.NumericToDecimal(li.LineTotal)
		if err != nil {
			return nil, err
		}
		g.net = g.net.Add(net)
		g.tax = g.tax.Add(tax)
		g.total = g.total.Add(total)
	}

	rows := make([]pdf.TaxBreakdownRow, len(order))
	for i, key := range order {
		g := groups[key]
		if key == noTaxKey {
			// The label depends on the group's fully-aggregated tax, not
			// just its first line — a line with no linked tax rate but a
			// non-zero tax_amount (e.g. tax data migrated from another
			// system without a matching tax rate) still needs to show its
			// tax; labeling it "No Tax" would hide a real tax figure under
			// a name that says there isn't one.
			g.label = "No Tax"
			if !g.tax.IsZero() {
				g.label = "Tax (no rate)"
			}
		}
		rows[i] = pdf.TaxBreakdownRow{Label: g.label, Net: g.net, Tax: g.tax, Total: g.total}
	}
	return rows, nil
}

// invoiceTaxBreakdown maps an invoice's line items into taxBreakdown's
// shared shape — see taxBreakdown.
func (s *invoiceService) invoiceTaxBreakdown(ctx context.Context, lineItems []sqlcgen.InvoiceLineItem) ([]pdf.TaxBreakdownRow, error) {
	lines := make([]taxBreakdownLine, len(lineItems))
	for i, li := range lineItems {
		lines[i] = taxBreakdownLine{TaxRateID: li.TaxRateID, LineSubtotal: li.LineSubtotal, TaxAmount: li.TaxAmount, LineTotal: li.LineTotal}
	}
	return taxBreakdown(ctx, s.store.Queries, lines)
}

// businessParty builds the PDF address block for the business issuing an
// invoice: name, mailing address, phone, and email.
func businessParty(b sqlcgen.Business) pdf.Party {
	lines := formatAddressLines(b.AddressLine1, b.AddressLine2, b.City, b.State, b.PostalCode)
	if v := derefOr(b.Phone, ""); v != "" {
		lines = append(lines, v)
	}
	if v := derefOr(b.Email, ""); v != "" {
		lines = append(lines, v)
	}
	return pdf.Party{Name: b.Name, Lines: lines}
}

// billToParty builds the PDF address block for the contact being billed:
// name, billing address, phone, and email.
func billToParty(c sqlcgen.Contact) pdf.Party {
	lines := formatAddressLines(c.BillingAddressLine1, c.BillingAddressLine2, c.BillingCity, c.BillingState, c.BillingPostalCode)
	if v := derefOr(c.Phone, ""); v != "" {
		lines = append(lines, v)
	}
	if v := derefOr(c.Email, ""); v != "" {
		lines = append(lines, v)
	}
	return pdf.Party{Name: c.Name, Lines: lines}
}

// formatAddressLines renders a street address as "line1", "line2" (if
// present), and "City, State PostalCode" — omitting any piece that's unset
// rather than leaving stray commas or blank lines. line1/line2 are used
// verbatim, one PDF line each — bad data (e.g. a migrated contact whose
// billing_address_line1 crams in more than a street address) needs
// cleaning up at the contact record itself, not parsed back apart here.
func formatAddressLines(line1, line2, city, state, postal *string) []string {
	var lines []string
	if v := strings.TrimSpace(derefOr(line1, "")); v != "" {
		lines = append(lines, v)
	}
	if v := strings.TrimSpace(derefOr(line2, "")); v != "" {
		lines = append(lines, v)
	}
	c, stateZip := derefOr(city, ""), strings.TrimSpace(derefOr(state, "")+" "+derefOr(postal, ""))
	var cityLine string
	switch {
	case c != "" && stateZip != "":
		cityLine = c + ", " + stateZip
	case c != "":
		cityLine = c
	default:
		cityLine = stateZip
	}
	if cityLine != "" {
		lines = append(lines, cityLine)
	}
	return lines
}

// resolveContactLedgerAccountID looks up a contact's own AR sub-ledger account (customer side)
// or AP sub-ledger account (vendor side) - the role tables' ledger_account_id, not a column on
// contact itself (see customer/vendor in migrations/00001_initial.up.sql). Returns (nil, nil),
// not an error, if the contact has no row in that role's table at all.
func resolveContactLedgerAccountID(ctx context.Context, q *sqlcgen.Queries, contactID int64, isCustomerSide bool) (*int32, error) {
	if isCustomerSide {
		customer, err := q.GetCustomerByContactID(ctx, contactID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, nil
			}
			return nil, err
		}
		return customer.LedgerAccountID, nil
	}
	vendor, err := q.GetVendorByContactID(ctx, contactID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return vendor.LedgerAccountID, nil
}

// postInvoiceLedger posts an invoice to the ledger: creates a new
// ledger_transaction and its balanced entries. CreateInvoice/
// UpdateInvoiceLineItems require every line item's ledger_account_id up
// front, so the nil checks here (and in writeInvoiceLedgerEntries) are a
// safety net, not the primary enforcement.
func postInvoiceLedger(ctx context.Context, q *sqlcgen.Queries, businessID int64, invoice sqlcgen.Invoice, lineItems []sqlcgen.InvoiceLineItem, createdByUserID *int64) (int64, error) {
	contactLedgerAccountID, err := resolveContactLedgerAccountID(ctx, q, invoice.ContactID, invoice.InvoiceType == "SALES")
	if err != nil {
		return 0, err
	}
	if contactLedgerAccountID == nil {
		return 0, fmt.Errorf("cannot post invoice: contact %d has no customer/vendor ledger_account_id set", invoice.ContactID)
	}

	description := fmt.Sprintf("Invoice %s", invoice.InvoiceNumber)
	txn, err := q.CreateLedgerTransaction(ctx, sqlcgen.CreateLedgerTransactionParams{
		BusinessID:      businessID,
		TransactionDate: invoice.InvoiceDate,
		Description:     &description,
		CreatedByUserID: createdByUserID,
	})
	if err != nil {
		return 0, err
	}
	if err := writeInvoiceLedgerEntries(ctx, q, businessID, txn.ID, *contactLedgerAccountID, invoice, lineItems); err != nil {
		return 0, err
	}
	return txn.ID, nil
}

// repostInvoiceLedger regenerates an already-posted invoice's ledger entries
// in place after its line items change, rather than rejecting the edit or
// posting a reversal: the existing entries under ledgerTransactionID are
// soft-deleted and replaced with entries built from the invoice's current
// state, so the ledger transaction itself (and its id) stays put. The
// ledger_entry period-lock trigger fires on both the soft-delete (an UPDATE
// of deleted_at) and the inserts, so editing a transaction dated in a closed
// period is still rejected.
func repostInvoiceLedger(ctx context.Context, q *sqlcgen.Queries, businessID, ledgerTransactionID int64, invoice sqlcgen.Invoice, lineItems []sqlcgen.InvoiceLineItem) error {
	contactLedgerAccountID, err := resolveContactLedgerAccountID(ctx, q, invoice.ContactID, invoice.InvoiceType == "SALES")
	if err != nil {
		return err
	}
	if contactLedgerAccountID == nil {
		return fmt.Errorf("cannot repost invoice: contact %d has no customer/vendor ledger_account_id set", invoice.ContactID)
	}
	if err := q.SoftDeleteLedgerEntriesByTransaction(ctx, ledgerTransactionID); err != nil {
		return err
	}
	return writeInvoiceLedgerEntries(ctx, q, businessID, ledgerTransactionID, *contactLedgerAccountID, invoice, lineItems)
}

// writeInvoiceLedgerEntries generates and inserts the balanced ledger_entry
// rows for an invoice against an existing ledger transaction: an AR/AP leg
// against the contact's own account, a revenue/expense leg per line, and
// sales tax split out to each tax rate's own liability account. Shared by
// postInvoiceLedger (new transaction) and repostInvoiceLedger (existing
// transaction, entries replaced).
func writeInvoiceLedgerEntries(ctx context.Context, q *sqlcgen.Queries, businessID, txnID int64, contactLedgerAccountID int32, invoice sqlcgen.Invoice, lineItems []sqlcgen.InvoiceLineItem) error {
	totalAmount, err := ledgermath.NumericToDecimal(invoice.TotalAmount)
	if err != nil {
		return err
	}
	isSales := invoice.InvoiceType == "SALES"

	// AR/AP leg, against the contact's own ledger account. Skipped when the
	// invoice totals zero — ledger_entry's debit_or_credit check constraint
	// requires exactly one side strictly positive, which a zero amount on
	// either side can never satisfy.
	if !totalAmount.IsZero() {
		if isSales {
			if err := createDecimalEntry(ctx, q, businessID, txnID, contactLedgerAccountID, totalAmount, decimal.Zero); err != nil {
				return err
			}
		} else {
			if err := createDecimalEntry(ctx, q, businessID, txnID, contactLedgerAccountID, decimal.Zero, totalAmount); err != nil {
				return err
			}
		}
	}

	// Per-line revenue/expense legs. SALES tax is split out to each tax
	// rate's own liability account (grouped, in case multiple lines share
	// one); PURCHASE tax is rolled into the line's own account instead of a
	// liability account — tax_liability_account_id models tax the business
	// COLLECTED and owes to a government, which fits sales tax, not tax
	// paid to a vendor.
	taxByLiabilityAccount := map[int32]decimal.Decimal{}
	for _, li := range lineItems {
		if li.LedgerAccountID == nil {
			return fmt.Errorf("line %d is missing ledger_account_id", li.LineNumber)
		}
		lineSubtotal, err := ledgermath.NumericToDecimal(li.LineSubtotal)
		if err != nil {
			return err
		}
		taxAmount, err := ledgermath.NumericToDecimal(li.TaxAmount)
		if err != nil {
			return err
		}

		if isSales {
			if !lineSubtotal.IsZero() {
				if err := createDecimalEntry(ctx, q, businessID, txnID, *li.LedgerAccountID, decimal.Zero, lineSubtotal); err != nil {
					return err
				}
			}
			if li.TaxRateID != nil && !taxAmount.IsZero() {
				tr, err := q.GetTaxRate(ctx, *li.TaxRateID)
				if err != nil {
					return err
				}
				taxByLiabilityAccount[tr.TaxLiabilityAccountID] = taxByLiabilityAccount[tr.TaxLiabilityAccountID].Add(taxAmount)
			}
		} else {
			lineTotal := lineSubtotal.Add(taxAmount)
			if !lineTotal.IsZero() {
				if err := createDecimalEntry(ctx, q, businessID, txnID, *li.LedgerAccountID, lineTotal, decimal.Zero); err != nil {
					return err
				}
			}
		}
	}
	for accountID, amount := range taxByLiabilityAccount {
		if err := createDecimalEntry(ctx, q, businessID, txnID, accountID, decimal.Zero, amount); err != nil {
			return err
		}
	}

	return verifyTransactionBalances(ctx, q, txnID)
}

func invoiceToProto(inv sqlcgen.Invoice, lineItems []sqlcgen.InvoiceLineItem) (*avav1.Invoice, error) {
	subtotal, err := moneypb.ToProto(inv.Subtotal)
	if err != nil {
		return nil, err
	}
	totalTax, err := moneypb.ToProto(inv.TotalTaxAmount)
	if err != nil {
		return nil, err
	}
	total, err := moneypb.ToProto(inv.TotalAmount)
	if err != nil {
		return nil, err
	}
	paid, err := moneypb.ToProto(inv.PaidAmount)
	if err != nil {
		return nil, err
	}
	balanceDue, err := moneypb.ToProto(inv.BalanceDue)
	if err != nil {
		return nil, err
	}
	pb := &avav1.Invoice{
		Id:                  inv.ID,
		BusinessId:          inv.BusinessID,
		ContactId:           inv.ContactID,
		InvoiceType:         inv.InvoiceType,
		EstimateId:          inv.EstimateID,
		InvoiceNumber:       inv.InvoiceNumber,
		InvoiceDate:         datepb.ToProto(inv.InvoiceDate),
		DueDate:             datepb.ToProto(inv.DueDate),
		Subtotal:            subtotal,
		TotalTaxAmount:      totalTax,
		TotalAmount:         total,
		PaidAmount:          paid,
		BalanceDue:          balanceDue,
		Status:              inv.Status,
		Notes:               inv.Notes,
		Terms:               inv.Terms,
		LedgerTransactionId: inv.LedgerTransactionID,
		CreatedByUserId:     inv.CreatedByUserID,
		CreatedAt:           timestampProto(inv.CreatedAt),
		ResourceVersion:     inv.ResourceVersion,
	}
	for _, li := range lineItems {
		pli, err := invoiceLineItemToProto(li)
		if err != nil {
			return nil, err
		}
		pb.LineItems = append(pb.LineItems, pli)
	}
	return pb, nil
}

func invoiceLineItemToProto(li sqlcgen.InvoiceLineItem) (*avav1.InvoiceLineItem, error) {
	quantity, err := moneypb.ToProto(li.Quantity)
	if err != nil {
		return nil, err
	}
	unitPrice, err := moneypb.ToProto(li.UnitPrice)
	if err != nil {
		return nil, err
	}
	lineSubtotal, err := moneypb.ToProto(li.LineSubtotal)
	if err != nil {
		return nil, err
	}
	taxAmount, err := moneypb.ToProto(li.TaxAmount)
	if err != nil {
		return nil, err
	}
	lineTotal, err := moneypb.ToProto(li.LineTotal)
	if err != nil {
		return nil, err
	}
	return &avav1.InvoiceLineItem{
		Id:              li.ID,
		InvoiceId:       li.InvoiceID,
		ItemId:          li.ItemID,
		LedgerAccountId: li.LedgerAccountID,
		LineNumber:      li.LineNumber,
		Description:     li.Description,
		Quantity:        quantity,
		UnitPrice:       unitPrice,
		LineSubtotal:    lineSubtotal,
		IsTaxable:       li.IsTaxable,
		TaxRateId:       li.TaxRateID,
		TaxAmount:       taxAmount,
		LineTotal:       lineTotal,
	}, nil
}

// ============================================================================
// PaymentService — applies to invoice paid_amount/balance_due regardless of
// posting, and optionally posts to the ledger (see proto comment).
// ============================================================================

type paymentService struct {
	avav1.UnimplementedPaymentServiceServer
	store *db.Store
}

func newPaymentService(store *db.Store) *paymentService {
	return &paymentService{store: store}
}

func (s *paymentService) GetPayment(ctx context.Context, req *avav1.GetPaymentRequest) (*avav1.GetPaymentResponse, error) {
	p, err := s.store.Queries.GetPayment(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "payment %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, p.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	applications, err := s.store.Queries.ListPaymentApplicationsForPayment(ctx, p.ID)
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := paymentToProto(p, applications)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting payment: %v", err)
	}
	return &avav1.GetPaymentResponse{Payment: pb}, nil
}

func (s *paymentService) ListPayments(ctx context.Context, req *avav1.ListPaymentsRequest) (*avav1.ListPaymentsResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListPayments(ctx, req.GetBusinessId())
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListPaymentsResponse{}
	for _, p := range rows {
		applications, err := s.store.Queries.ListPaymentApplicationsForPayment(ctx, p.ID)
		if err != nil {
			return nil, translatePgError(err)
		}
		pb, err := paymentToProto(p, applications)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting payment: %v", err)
		}
		resp.Payments = append(resp.Payments, pb)
	}
	return resp, nil
}

func (s *paymentService) CreatePayment(ctx context.Context, req *avav1.CreatePaymentRequest) (*avav1.CreatePaymentResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if req.GetPaymentType() != "RECEIVED" && req.GetPaymentType() != "MADE" {
		return nil, status.Error(codes.InvalidArgument, "payment_type must be RECEIVED or MADE")
	}
	if req.GetPaymentNumber() == "" || req.GetAmount() == nil || req.GetPaymentDate() == nil || req.GetPaymentMethod() == "" {
		return nil, status.Error(codes.InvalidArgument, "payment_number, amount, payment_date, and payment_method are required")
	}

	amount, err := decimal.NewFromString(req.GetAmount().GetValue())
	if err != nil || !amount.IsPositive() {
		return nil, status.Error(codes.InvalidArgument, "amount must be a positive number")
	}
	amountNum, err := ledgermath.DecimalToNumeric(amount)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid amount: %v", err)
	}

	// Each application's own amount, validated up front so a bad one fails
	// before anything's written - and their sum can't exceed what was
	// actually received (a payment may be partially or fully unapplied,
	// but never over-applied).
	appliedAmounts := make([]decimal.Decimal, len(req.GetApplications()))
	total := decimal.Zero
	for i, app := range req.GetApplications() {
		if app.GetInvoiceId() == 0 {
			return nil, status.Error(codes.InvalidArgument, "applications[].invoice_id is required")
		}
		applied, err := decimal.NewFromString(app.GetAppliedAmount().GetValue())
		if err != nil || !applied.IsPositive() {
			return nil, status.Errorf(codes.InvalidArgument, "applications[%d].applied_amount must be a positive number", i)
		}
		appliedAmounts[i] = applied
		total = total.Add(applied)
	}
	if total.GreaterThan(amount) {
		return nil, status.Error(codes.InvalidArgument, "sum of applications[].applied_amount exceeds amount")
	}

	var payment sqlcgen.Payment
	var applications []sqlcgen.PaymentApplication
	err = s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		var err error
		payment, err = q.CreatePayment(ctx, sqlcgen.CreatePaymentParams{
			BusinessID:      req.GetBusinessId(),
			ContactID:       req.GetContactId(),
			PaymentType:     req.GetPaymentType(),
			PaymentNumber:   req.GetPaymentNumber(),
			PaymentDate:     datepb.ToPgDate(req.GetPaymentDate()),
			Amount:          amountNum,
			PaymentMethod:   req.GetPaymentMethod(),
			LedgerAccountID: req.LedgerAccountId,
			ReferenceNumber: req.ReferenceNumber,
			Notes:           req.Notes,
			CreatedByUserID: &u.ID,
		})
		if err != nil {
			return err
		}

		for i, app := range req.GetApplications() {
			appliedNum, err := ledgermath.DecimalToNumeric(appliedAmounts[i])
			if err != nil {
				return err
			}
			application, err := q.CreatePaymentApplication(ctx, sqlcgen.CreatePaymentApplicationParams{
				PaymentID:     payment.ID,
				InvoiceID:     app.GetInvoiceId(),
				AppliedAmount: appliedNum,
			})
			if err != nil {
				return err
			}
			applications = append(applications, application)
			if _, err := q.ApplyPaymentToInvoice(ctx, sqlcgen.ApplyPaymentToInvoiceParams{ID: app.GetInvoiceId(), Amount: appliedNum}); err != nil {
				return err
			}
		}

		txnID, err := maybePostPayment(ctx, q, req.GetBusinessId(), payment, &u.ID)
		if err != nil {
			return err
		}
		if txnID != nil {
			payment, err = q.SetPaymentLedgerTransaction(ctx, sqlcgen.SetPaymentLedgerTransactionParams{ID: payment.ID, LedgerTransactionID: txnID})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, closeErrorStatus(err)
	}

	pb, err := paymentToProto(payment, applications)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting payment: %v", err)
	}
	return &avav1.CreatePaymentResponse{Payment: pb}, nil
}

// maybePostPayment posts payment to the ledger iff both ledger_account_id
// (the cash/bank account) and the contact's own ledger_account_id are set;
// returns (nil, nil) — not an error — otherwise, leaving it unposted.
func maybePostPayment(ctx context.Context, q *sqlcgen.Queries, businessID int64, payment sqlcgen.Payment, createdByUserID *int64) (*int64, error) {
	if payment.LedgerAccountID == nil {
		return nil, nil
	}
	contactLedgerAccountID, err := resolveContactLedgerAccountID(ctx, q, payment.ContactID, payment.PaymentType == "RECEIVED")
	if err != nil {
		return nil, err
	}
	if contactLedgerAccountID == nil {
		return nil, fmt.Errorf("cannot post payment: contact %d has no customer/vendor ledger_account_id set", payment.ContactID)
	}

	amount, err := ledgermath.NumericToDecimal(payment.Amount)
	if err != nil {
		return nil, err
	}

	description := fmt.Sprintf("Payment %s", payment.PaymentNumber)
	txn, err := q.CreateLedgerTransaction(ctx, sqlcgen.CreateLedgerTransactionParams{
		BusinessID:      businessID,
		TransactionDate: payment.PaymentDate,
		Description:     &description,
		CreatedByUserID: createdByUserID,
	})
	if err != nil {
		return nil, err
	}

	if payment.PaymentType == "RECEIVED" {
		// DEBIT cash, CREDIT the contact's AR.
		if err := createDecimalEntry(ctx, q, businessID, txn.ID, *payment.LedgerAccountID, amount, decimal.Zero); err != nil {
			return nil, err
		}
		if err := createDecimalEntry(ctx, q, businessID, txn.ID, *contactLedgerAccountID, decimal.Zero, amount); err != nil {
			return nil, err
		}
	} else {
		// MADE: DEBIT the contact's AP, CREDIT cash.
		if err := createDecimalEntry(ctx, q, businessID, txn.ID, *contactLedgerAccountID, amount, decimal.Zero); err != nil {
			return nil, err
		}
		if err := createDecimalEntry(ctx, q, businessID, txn.ID, *payment.LedgerAccountID, decimal.Zero, amount); err != nil {
			return nil, err
		}
	}

	if err := verifyTransactionBalances(ctx, q, txn.ID); err != nil {
		return nil, err
	}
	return &txn.ID, nil
}

func paymentToProto(p sqlcgen.Payment, applications []sqlcgen.PaymentApplication) (*avav1.Payment, error) {
	amount, err := moneypb.ToProto(p.Amount)
	if err != nil {
		return nil, err
	}
	pbApplications := make([]*avav1.PaymentApplication, len(applications))
	for i, a := range applications {
		pb, err := paymentApplicationToProto(a)
		if err != nil {
			return nil, err
		}
		pbApplications[i] = pb
	}
	return &avav1.Payment{
		Id:                  p.ID,
		BusinessId:          p.BusinessID,
		ContactId:           p.ContactID,
		PaymentType:         p.PaymentType,
		PaymentNumber:       p.PaymentNumber,
		PaymentDate:         datepb.ToProto(p.PaymentDate),
		Amount:              amount,
		PaymentMethod:       p.PaymentMethod,
		LedgerAccountId:     p.LedgerAccountID,
		ReferenceNumber:     p.ReferenceNumber,
		Notes:               p.Notes,
		LedgerTransactionId: p.LedgerTransactionID,
		CreatedByUserId:     p.CreatedByUserID,
		CreatedAt:           timestampProto(p.CreatedAt),
		Applications:        pbApplications,
	}, nil
}

func paymentApplicationToProto(a sqlcgen.PaymentApplication) (*avav1.PaymentApplication, error) {
	appliedAmount, err := moneypb.ToProto(a.AppliedAmount)
	if err != nil {
		return nil, err
	}
	return &avav1.PaymentApplication{
		Id:            a.ID,
		PaymentId:     a.PaymentID,
		InvoiceId:     a.InvoiceID,
		AppliedAmount: appliedAmount,
		CreatedAt:     timestampProto(a.CreatedAt),
	}, nil
}
