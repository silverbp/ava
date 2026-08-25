// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"fmt"

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
	rows, err := s.store.Queries.ListEstimates(ctx, req.GetBusinessId())
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
		inputs := make([]lineInput, len(req.GetLineItems()))
		for i, li := range req.GetLineItems() {
			inputs[i] = lineInput{Quantity: li.Quantity, UnitPrice: li.UnitPrice, IsTaxable: li.IsTaxable, TaxRateID: li.TaxRateId}
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
			src := req.GetLineItems()[i]
			li, err := q.CreateEstimateLineItem(ctx, sqlcgen.CreateEstimateLineItemParams{
				EstimateID:   estimate.ID,
				ServiceID:    src.ServiceId,
				LineNumber:   src.LineNumber,
				Description:  src.Description,
				Quantity:     cl.Quantity,
				UnitPrice:    cl.UnitPrice,
				LineSubtotal: cl.LineSubtotal,
				IsTaxable:    src.IsTaxable,
				TaxRateID:    src.TaxRateId,
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

	updated, err := s.store.Queries.UpdateEstimateStatus(ctx, sqlcgen.UpdateEstimateStatusParams{ID: req.GetId(), Status: req.GetStatus()})
	if err != nil {
		return nil, translatePgError(err)
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
		ServiceId:    li.ServiceID,
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
	rows, err := s.store.Queries.ListInvoices(ctx, req.GetBusinessId())
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
	if len(req.GetLineItems()) == 0 {
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
		inputs := make([]lineInput, len(req.GetLineItems()))
		for i, li := range req.GetLineItems() {
			inputs[i] = lineInput{Quantity: li.Quantity, UnitPrice: li.UnitPrice, IsTaxable: li.IsTaxable, TaxRateID: li.TaxRateId}
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
			src := req.GetLineItems()[i]
			li, err := q.CreateInvoiceLineItem(ctx, sqlcgen.CreateInvoiceLineItemParams{
				InvoiceID:       invoice.ID,
				ServiceID:       src.ServiceId,
				LedgerAccountID: src.LedgerAccountId,
				LineNumber:      src.LineNumber,
				Description:     src.Description,
				Quantity:        cl.Quantity,
				UnitPrice:       cl.UnitPrice,
				LineSubtotal:    cl.LineSubtotal,
				IsTaxable:       src.IsTaxable,
				TaxRateID:       src.TaxRateId,
				TaxRate:         cl.TaxRate,
				TaxAmount:       cl.TaxAmount,
				LineTotal:       cl.LineTotal,
			})
			if err != nil {
				return err
			}
			lineItems = append(lineItems, li)
		}

		txnID, err := maybePostInvoice(ctx, q, req.GetBusinessId(), invoice, lineItems, &u.ID)
		if err != nil {
			return err
		}
		if txnID != nil {
			invoice, err = q.SetInvoiceLedgerTransaction(ctx, sqlcgen.SetInvoiceLedgerTransactionParams{ID: invoice.ID, LedgerTransactionID: txnID})
			if err != nil {
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

	updated, err := s.store.Queries.UpdateInvoiceStatus(ctx, sqlcgen.UpdateInvoiceStatusParams{ID: req.GetId(), Status: req.GetStatus()})
	if err != nil {
		return nil, translatePgError(err)
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

	content, err := pdf.RenderInvoice(business.Name, contact.Name, pb)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering pdf: %v", err)
	}
	return &avav1.GetInvoicePdfResponse{Content: content}, nil
}

// maybePostInvoice posts invoice to the ledger iff every line item has
// ledger_account_id set; returns (nil, nil) — not an error — if any line
// omits it, leaving the invoice an unposted document. See the InvoiceService
// proto doc for the design rationale (per-line account selection).
func maybePostInvoice(ctx context.Context, q *sqlcgen.Queries, businessID int64, invoice sqlcgen.Invoice, lineItems []sqlcgen.InvoiceLineItem, createdByUserID *int64) (*int64, error) {
	for _, li := range lineItems {
		if li.LedgerAccountID == nil {
			return nil, nil
		}
	}

	contact, err := q.GetContact(ctx, invoice.ContactID)
	if err != nil {
		return nil, err
	}
	if contact.LedgerAccountID == nil {
		return nil, fmt.Errorf("cannot post invoice: contact %d has no ledger_account_id set", contact.ID)
	}

	description := fmt.Sprintf("Invoice %s", invoice.InvoiceNumber)
	txn, err := q.CreateLedgerTransaction(ctx, sqlcgen.CreateLedgerTransactionParams{
		BusinessID:      businessID,
		TransactionDate: invoice.InvoiceDate,
		Description:     &description,
		CreatedByUserID: createdByUserID,
	})
	if err != nil {
		return nil, err
	}

	totalAmount, err := ledgermath.NumericToDecimal(invoice.TotalAmount)
	if err != nil {
		return nil, err
	}
	isSales := invoice.InvoiceType == "SALES"

	// AR/AP leg, against the contact's own ledger account.
	if isSales {
		if err := createDecimalEntry(ctx, q, businessID, txn.ID, *contact.LedgerAccountID, totalAmount, decimal.Zero); err != nil {
			return nil, err
		}
	} else {
		if err := createDecimalEntry(ctx, q, businessID, txn.ID, *contact.LedgerAccountID, decimal.Zero, totalAmount); err != nil {
			return nil, err
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
		lineSubtotal, err := ledgermath.NumericToDecimal(li.LineSubtotal)
		if err != nil {
			return nil, err
		}
		taxAmount, err := ledgermath.NumericToDecimal(li.TaxAmount)
		if err != nil {
			return nil, err
		}

		if isSales {
			if err := createDecimalEntry(ctx, q, businessID, txn.ID, *li.LedgerAccountID, decimal.Zero, lineSubtotal); err != nil {
				return nil, err
			}
			if li.TaxRateID != nil && !taxAmount.IsZero() {
				tr, err := q.GetTaxRate(ctx, *li.TaxRateID)
				if err != nil {
					return nil, err
				}
				taxByLiabilityAccount[tr.TaxLiabilityAccountID] = taxByLiabilityAccount[tr.TaxLiabilityAccountID].Add(taxAmount)
			}
		} else {
			if err := createDecimalEntry(ctx, q, businessID, txn.ID, *li.LedgerAccountID, lineSubtotal.Add(taxAmount), decimal.Zero); err != nil {
				return nil, err
			}
		}
	}
	for accountID, amount := range taxByLiabilityAccount {
		if err := createDecimalEntry(ctx, q, businessID, txn.ID, accountID, decimal.Zero, amount); err != nil {
			return nil, err
		}
	}

	if err := verifyTransactionBalances(ctx, q, txn.ID); err != nil {
		return nil, err
	}
	return &txn.ID, nil
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
		ServiceId:       li.ServiceID,
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
	contact, err := q.GetContact(ctx, payment.ContactID)
	if err != nil {
		return nil, err
	}
	if contact.LedgerAccountID == nil {
		return nil, fmt.Errorf("cannot post payment: contact %d has no ledger_account_id set", contact.ID)
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
		if err := createDecimalEntry(ctx, q, businessID, txn.ID, *contact.LedgerAccountID, decimal.Zero, amount); err != nil {
			return nil, err
		}
	} else {
		// MADE: DEBIT the contact's AP, CREDIT cash.
		if err := createDecimalEntry(ctx, q, businessID, txn.ID, *contact.LedgerAccountID, amount, decimal.Zero); err != nil {
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
