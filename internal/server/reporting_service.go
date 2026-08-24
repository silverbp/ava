// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
	typepb "google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/reporting"
)

type reportingService struct {
	avav1.UnimplementedReportingServiceServer
	store *db.Store
}

func newReportingService(store *db.Store) *reportingService {
	return &reportingService{store: store}
}

func (s *reportingService) GetTrialBalance(ctx context.Context, req *avav1.GetTrialBalanceRequest) (*avav1.GetTrialBalanceResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
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

	pb := &avav1.TrialBalance{}
	for _, l := range result.Lines {
		pb.Lines = append(pb.Lines, &avav1.TrialBalanceLine{
			AccountId:   l.AccountID,
			AccountCode: l.Code,
			AccountName: l.Name,
			Debit:       decimalToProto(l.Debit),
			Credit:      decimalToProto(l.Credit),
		})
	}
	pb.TotalDebit = decimalToProto(result.TotalDebit)
	pb.TotalCredit = decimalToProto(result.TotalCredit)
	return &avav1.GetTrialBalanceResponse{TrialBalance: pb}, nil
}

func (s *reportingService) GetBalanceSheet(ctx context.Context, req *avav1.GetBalanceSheetRequest) (*avav1.GetBalanceSheetResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
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
		TotalAssets:                       decimalToProto(result.TotalAssets),
		TotalLiabilities:                  decimalToProto(result.TotalLiabilities),
		NetCurrentAssets:                  decimalToProto(result.NetCurrentAssets),
		TotalAssetsLessCurrentLiabilities: decimalToProto(result.TotalAssetsLessCurrentLiabilities),
		TotalNetAssets:                    decimalToProto(result.TotalNetAssets),
	}
	for _, s := range result.Sections {
		pb.Sections = append(pb.Sections, &avav1.BalanceSheetSection{
			Title:            s.Title,
			AssetLines:       accountLinesToProto(s.AssetLines),
			LiabilityLines:   accountLinesToProto(s.LiabilityLines),
			TotalAssets:      decimalToProto(s.TotalAssets),
			TotalLiabilities: decimalToProto(s.TotalLiabilities),
		})
	}
	return &avav1.GetBalanceSheetResponse{BalanceSheet: pb}, nil
}

func (s *reportingService) GetIncomeStatement(ctx context.Context, req *avav1.GetIncomeStatementRequest) (*avav1.GetIncomeStatementResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	start, err := requireDate(req.GetPeriodStart(), "period_start")
	if err != nil {
		return nil, err
	}
	end, err := requireDate(req.GetPeriodEnd(), "period_end")
	if err != nil {
		return nil, err
	}

	result, err := reporting.IncomeStatement(ctx, s.store.Queries, req.GetBusinessId(), start, end)
	if err != nil {
		return nil, translatePgError(err)
	}

	pb := &avav1.IncomeStatement{
		Revenue:                incomeStatementLinesToProto(result.Revenue),
		TotalRevenue:           decimalToProto(result.TotalRevenue),
		CostOfGoodsSold:        incomeStatementLinesToProto(result.CostOfGoodsSold),
		TotalCostOfGoodsSold:   decimalToProto(result.TotalCostOfGoodsSold),
		GrossProfit:            decimalToProto(result.GrossProfit),
		OperatingExpenses:      incomeStatementLinesToProto(result.OperatingExpenses),
		TotalOperatingExpenses: decimalToProto(result.TotalOperatingExpenses),
		TotalExpenses:          decimalToProto(result.TotalExpenses),
		NetIncome:              decimalToProto(result.NetIncome),
	}
	return &avav1.GetIncomeStatementResponse{IncomeStatement: pb}, nil
}

func incomeStatementLinesToProto(lines []reporting.AccountLine) []*avav1.IncomeStatementLine {
	out := make([]*avav1.IncomeStatementLine, len(lines))
	for i, l := range lines {
		out[i] = &avav1.IncomeStatementLine{AccountId: l.AccountID, AccountCode: l.Code, AccountName: l.Name, Amount: decimalToProto(l.Amount)}
	}
	return out
}

func (s *reportingService) GetGeneralLedger(ctx context.Context, req *avav1.GetGeneralLedgerRequest) (*avav1.GetGeneralLedgerResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	start, err := requireDate(req.GetPeriodStart(), "period_start")
	if err != nil {
		return nil, err
	}
	end, err := requireDate(req.GetPeriodEnd(), "period_end")
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
		EndingBalance: decimalToProto(result.EndingBalance),
	}
	for _, l := range result.Lines {
		pb.Lines = append(pb.Lines, &avav1.GeneralLedgerLine{
			LedgerTransactionId: l.LedgerTransactionID,
			TransactionDate:     &typepb.Date{Year: int32(l.TransactionDate.Year()), Month: int32(l.TransactionDate.Month()), Day: int32(l.TransactionDate.Day())},
			Description:         l.Description,
			Debit:               decimalToProto(l.Debit),
			Credit:              decimalToProto(l.Credit),
			RunningBalance:      decimalToProto(l.RunningBalance),
		})
	}
	return &avav1.GetGeneralLedgerResponse{GeneralLedger: pb}, nil
}

func (s *reportingService) GetCustomerStatement(ctx context.Context, req *avav1.GetCustomerStatementRequest) (*avav1.GetCustomerStatementResponse, error) {
	contact, err := s.store.Queries.GetContact(ctx, req.GetContactId())
	if err != nil {
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, contact.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	start, err := requireDate(req.GetPeriodStart(), "period_start")
	if err != nil {
		return nil, err
	}
	end, err := requireDate(req.GetPeriodEnd(), "period_end")
	if err != nil {
		return nil, err
	}

	result, err := reporting.CustomerStatement(ctx, s.store.Queries, req.GetContactId(), start, end)
	if err != nil {
		return nil, translatePgError(err)
	}

	return &avav1.GetCustomerStatementResponse{Statement: customerStatementToProto(result)}, nil
}

func customerStatementToProto(r *reporting.CustomerStatementResult) *avav1.CustomerStatement {
	pb := &avav1.CustomerStatement{
		ContactId:     r.ContactID,
		ContactName:   r.ContactName,
		PeriodStart:   dateToProto(r.PeriodStart),
		PeriodEnd:     dateToProto(r.PeriodEnd),
		EndingBalance: decimalToProto(r.EndingBalance),
	}
	for _, inv := range r.Invoices {
		pb.Invoices = append(pb.Invoices, &avav1.StatementInvoiceLine{
			InvoiceId:     inv.InvoiceID,
			InvoiceNumber: inv.InvoiceNumber,
			InvoiceDate:   dateToProto(inv.InvoiceDate),
			DueDate:       dateToProto(inv.DueDate),
			TotalAmount:   decimalToProto(inv.TotalAmount),
			BalanceDue:    decimalToProto(inv.BalanceDue),
			Status:        inv.Status,
		})
	}
	for _, p := range r.Payments {
		pb.Payments = append(pb.Payments, &avav1.StatementPaymentLine{
			PaymentId:     p.PaymentID,
			PaymentNumber: p.PaymentNumber,
			PaymentDate:   dateToProto(p.PaymentDate),
			Amount:        decimalToProto(p.Amount),
		})
	}
	for _, a := range r.Activity {
		pb.Activity = append(pb.Activity, &avav1.StatementActivityLine{
			Date:           dateToProto(a.Date),
			Description:    a.Description,
			Debit:          decimalToProto(a.Debit),
			Credit:         decimalToProto(a.Credit),
			RunningBalance: decimalToProto(a.RunningBalance),
		})
	}
	for _, b := range r.AgingBuckets {
		pb.AgingBuckets = append(pb.AgingBuckets, &avav1.AgingBucket{Label: b.Label, Amount: decimalToProto(b.Amount)})
	}
	return pb
}

func dateToProto(t time.Time) *typepb.Date {
	return &typepb.Date{Year: int32(t.Year()), Month: int32(t.Month()), Day: int32(t.Day())}
}

func requireDate(d *typepb.Date, field string) (time.Time, error) {
	if d == nil {
		return time.Time{}, status.Errorf(codes.InvalidArgument, "%s is required", field)
	}
	return time.Date(int(d.GetYear()), time.Month(d.GetMonth()), int(d.GetDay()), 0, 0, 0, 0, time.UTC), nil
}

func decimalToProto(d decimal.Decimal) *avav1.Decimal {
	return &avav1.Decimal{Value: d.String()}
}

func accountLinesToProto(lines []reporting.AccountLine) []*avav1.BalanceSheetLine {
	out := make([]*avav1.BalanceSheetLine, len(lines))
	for i, l := range lines {
		out[i] = &avav1.BalanceSheetLine{AccountId: l.AccountID, AccountCode: l.Code, AccountName: l.Name, Balance: decimalToProto(l.Amount)}
	}
	return out
}
