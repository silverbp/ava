// Copyright (c) 2025 Silver Blueprints LLC
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/datepb"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/moneypb"
	"github.com/silverbp/ava/internal/reporting"
)

// Every report handler has the same prologue - authorize the business (or,
// for the customer statement, the contact's business), parse the dates -
// then computes via internal/reporting and either converts to proto (here)
// or renders a PDF (reporting_pdf.go). The shared steps are s.scope /
// requireDate / requirePeriod; the per-report adapters below are what
// remains, since each report has its own proto shape.

type reportingService struct {
	avav1.UnimplementedReportingServiceServer
	store *db.Store
}

func newReportingService(store *db.Store) *reportingService {
	return &reportingService{store: store}
}

// scope authorizes a business-wide report.
func (s *reportingService) scope(ctx context.Context, businessID int64) error {
	return auth.RequireBusinessRole(ctx, s.store.Queries, businessID, "VIEWER")
}

// translateStatementError maps reporting.ErrNotACustomer/ErrNoLedgerAccount
// to FailedPrecondition (a real contact, just missing what a statement
// needs) ahead of the generic pgError translation - shared by the JSON and
// PDF customer statement handlers.
func translateStatementError(err error) error {
	if errors.Is(err, reporting.ErrNotACustomer) || errors.Is(err, reporting.ErrNoLedgerAccount) {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return translatePgError(err)
}

// contactScope authorizes a per-contact report, returning the contact's
// business for the PDF header.
func (s *reportingService) contactScope(ctx context.Context, contactID int64) (businessID int64, err error) {
	c, err := contactRes.load(ctx, s.store.Queries, contactID, "VIEWER")
	if err != nil {
		return 0, err
	}
	return c.BusinessID, nil
}

func (s *reportingService) GetTrialBalance(ctx context.Context, req *avav1.GetTrialBalanceRequest) (*avav1.GetTrialBalanceResponse, error) {
	if err := s.scope(ctx, req.GetBusinessId()); err != nil {
		return nil, err
	}
	asOf, err := requireDate(req.GetAsOf(), "as_of")
	if err != nil {
		return nil, err
	}
	result, err := reporting.TrialBalance(ctx, s.store.Queries, req.GetBusinessId(), asOf)
	if err != nil {
		return nil, translatePgError(err)
	}

	pb := &avav1.TrialBalance{
		TotalDebit:  moneypb.FromDecimal(result.TotalDebit),
		TotalCredit: moneypb.FromDecimal(result.TotalCredit),
	}
	for _, l := range result.Lines {
		pb.Lines = append(pb.Lines, &avav1.TrialBalanceLine{
			AccountId:   l.AccountID,
			AccountCode: l.Code,
			AccountName: l.Name,
			Debit:       moneypb.FromDecimal(l.Debit),
			Credit:      moneypb.FromDecimal(l.Credit),
		})
	}
	return &avav1.GetTrialBalanceResponse{TrialBalance: pb}, nil
}

func (s *reportingService) GetBalanceSheet(ctx context.Context, req *avav1.GetBalanceSheetRequest) (*avav1.GetBalanceSheetResponse, error) {
	if err := s.scope(ctx, req.GetBusinessId()); err != nil {
		return nil, err
	}
	asOf, err := requireDate(req.GetAsOf(), "as_of")
	if err != nil {
		return nil, err
	}
	result, err := reporting.BalanceSheet(ctx, s.store.Queries, req.GetBusinessId(), asOf)
	if err != nil {
		return nil, translatePgError(err)
	}

	pb := &avav1.BalanceSheet{
		TotalAssets:                       moneypb.FromDecimal(result.TotalAssets),
		TotalLiabilities:                  moneypb.FromDecimal(result.TotalLiabilities),
		NetCurrentAssets:                  moneypb.FromDecimal(result.NetCurrentAssets),
		TotalAssetsLessCurrentLiabilities: moneypb.FromDecimal(result.TotalAssetsLessCurrentLiabilities),
		TotalNetAssets:                    moneypb.FromDecimal(result.TotalNetAssets),
	}
	for _, sec := range result.Sections {
		pb.Sections = append(pb.Sections, &avav1.BalanceSheetSection{
			Title:            sec.Title,
			AssetLines:       accountLinesToProto(sec.AssetLines),
			LiabilityLines:   accountLinesToProto(sec.LiabilityLines),
			TotalAssets:      moneypb.FromDecimal(sec.TotalAssets),
			TotalLiabilities: moneypb.FromDecimal(sec.TotalLiabilities),
		})
	}
	return &avav1.GetBalanceSheetResponse{BalanceSheet: pb}, nil
}

func (s *reportingService) GetIncomeStatement(ctx context.Context, req *avav1.GetIncomeStatementRequest) (*avav1.GetIncomeStatementResponse, error) {
	if err := s.scope(ctx, req.GetBusinessId()); err != nil {
		return nil, err
	}
	start, end, err := requirePeriod(req.GetPeriodStart(), req.GetPeriodEnd())
	if err != nil {
		return nil, err
	}
	result, err := reporting.IncomeStatement(ctx, s.store.Queries, req.GetBusinessId(), start, end)
	if err != nil {
		return nil, translatePgError(err)
	}

	pb := &avav1.IncomeStatement{
		Revenue:                incomeStatementLinesToProto(result.Revenue),
		TotalRevenue:           moneypb.FromDecimal(result.TotalRevenue),
		CostOfGoodsSold:        incomeStatementLinesToProto(result.CostOfGoodsSold),
		TotalCostOfGoodsSold:   moneypb.FromDecimal(result.TotalCostOfGoodsSold),
		GrossProfit:            moneypb.FromDecimal(result.GrossProfit),
		OperatingExpenses:      incomeStatementLinesToProto(result.OperatingExpenses),
		TotalOperatingExpenses: moneypb.FromDecimal(result.TotalOperatingExpenses),
		TotalExpenses:          moneypb.FromDecimal(result.TotalExpenses),
		NetIncome:              moneypb.FromDecimal(result.NetIncome),
	}
	return &avav1.GetIncomeStatementResponse{IncomeStatement: pb}, nil
}

func (s *reportingService) GetGeneralLedger(ctx context.Context, req *avav1.GetGeneralLedgerRequest) (*avav1.GetGeneralLedgerResponse, error) {
	if err := s.scope(ctx, req.GetBusinessId()); err != nil {
		return nil, err
	}
	if _, err := ledgerAccountRes.requireInBusiness(ctx, s.store.Queries, req.GetBusinessId(), int64(req.GetAccountId())); err != nil {
		return nil, err
	}
	start, end, err := requirePeriod(req.GetPeriodStart(), req.GetPeriodEnd())
	if err != nil {
		return nil, err
	}
	result, err := reporting.GeneralLedger(ctx, s.store.Queries, req.GetAccountId(), start, end)
	if err != nil {
		return nil, translatePgError(err)
	}

	pb := &avav1.GeneralLedger{
		AccountId:     result.AccountID,
		AccountCode:   result.Code,
		AccountName:   result.Name,
		EndingBalance: moneypb.FromDecimal(result.EndingBalance),
	}
	for _, l := range result.Lines {
		pb.Lines = append(pb.Lines, &avav1.GeneralLedgerLine{
			LedgerTransactionId: l.LedgerTransactionID,
			TransactionDate:     datepb.FromTime(l.TransactionDate),
			Description:         l.Description,
			Debit:               moneypb.FromDecimal(l.Debit),
			Credit:              moneypb.FromDecimal(l.Credit),
			RunningBalance:      moneypb.FromDecimal(l.RunningBalance),
		})
	}
	return &avav1.GetGeneralLedgerResponse{GeneralLedger: pb}, nil
}

func (s *reportingService) GetCustomerStatement(ctx context.Context, req *avav1.GetCustomerStatementRequest) (*avav1.GetCustomerStatementResponse, error) {
	if _, err := s.contactScope(ctx, req.GetContactId()); err != nil {
		return nil, err
	}
	start, end, err := requirePeriod(req.GetPeriodStart(), req.GetPeriodEnd())
	if err != nil {
		return nil, err
	}
	result, err := reporting.CustomerStatement(ctx, s.store.Queries, req.GetContactId(), start, end)
	if err != nil {
		return nil, translateStatementError(err)
	}
	return &avav1.GetCustomerStatementResponse{Statement: customerStatementToProto(result)}, nil
}

func customerStatementToProto(r *reporting.CustomerStatementResult) *avav1.CustomerStatement {
	pb := &avav1.CustomerStatement{
		ContactId:     r.ContactID,
		ContactName:   r.ContactName,
		PeriodStart:   datepb.FromTime(r.PeriodStart),
		PeriodEnd:     datepb.FromTime(r.PeriodEnd),
		EndingBalance: moneypb.FromDecimal(r.EndingBalance),
	}
	for _, inv := range r.Invoices {
		pb.Invoices = append(pb.Invoices, &avav1.StatementInvoiceLine{
			InvoiceId:     inv.InvoiceID,
			InvoiceNumber: inv.InvoiceNumber,
			InvoiceDate:   datepb.FromTime(inv.InvoiceDate),
			DueDate:       datepb.FromTime(inv.DueDate),
			TotalAmount:   moneypb.FromDecimal(inv.TotalAmount),
			BalanceDue:    moneypb.FromDecimal(inv.BalanceDue),
			Status:        inv.Status,
		})
	}
	for _, p := range r.Payments {
		pb.Payments = append(pb.Payments, &avav1.StatementPaymentLine{
			PaymentId:     p.PaymentID,
			PaymentNumber: p.PaymentNumber,
			PaymentDate:   datepb.FromTime(p.PaymentDate),
			Amount:        moneypb.FromDecimal(p.Amount),
		})
	}
	for _, a := range r.Activity {
		pb.Activity = append(pb.Activity, &avav1.StatementActivityLine{
			Date:           datepb.FromTime(a.Date),
			Description:    a.Description,
			Debit:          moneypb.FromDecimal(a.Debit),
			Credit:         moneypb.FromDecimal(a.Credit),
			RunningBalance: moneypb.FromDecimal(a.RunningBalance),
		})
	}
	for _, b := range r.AgingBuckets {
		pb.AgingBuckets = append(pb.AgingBuckets, &avav1.AgingBucket{Label: b.Label, Amount: moneypb.FromDecimal(b.Amount)})
	}
	return pb
}

func incomeStatementLinesToProto(lines []reporting.AccountLine) []*avav1.IncomeStatementLine {
	out := make([]*avav1.IncomeStatementLine, len(lines))
	for i, l := range lines {
		out[i] = &avav1.IncomeStatementLine{AccountId: l.AccountID, AccountCode: l.Code, AccountName: l.Name, Amount: moneypb.FromDecimal(l.Amount)}
	}
	return out
}

func accountLinesToProto(lines []reporting.AccountLine) []*avav1.BalanceSheetLine {
	out := make([]*avav1.BalanceSheetLine, len(lines))
	for i, l := range lines {
		out[i] = &avav1.BalanceSheetLine{AccountId: l.AccountID, AccountCode: l.Code, AccountName: l.Name, Balance: moneypb.FromDecimal(l.Amount)}
	}
	return out
}
