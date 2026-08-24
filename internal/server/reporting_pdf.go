// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/pdf"
	"github.com/silverbp/ava/internal/reporting"
)

func (s *reportingService) GetTrialBalancePdf(ctx context.Context, req *avav1.GetTrialBalancePdfRequest) (*avav1.GetTrialBalancePdfResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	asOf, err := requireDate(req.GetAsOf(), "as_of")
	if err != nil {
		return nil, err
	}
	businessName, err := s.businessName(ctx, req.GetBusinessId())
	if err != nil {
		return nil, err
	}

	result, err := reporting.TrialBalance(ctx, s.store.Queries, req.GetBusinessId(), asOf)
	if err != nil {
		return nil, translatePgError(err)
	}
	content, err := pdf.RenderTrialBalance(businessName, asOf.Format("2006-01-02"), result)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering pdf: %v", err)
	}
	return &avav1.GetTrialBalancePdfResponse{Content: content}, nil
}

func (s *reportingService) GetBalanceSheetPdf(ctx context.Context, req *avav1.GetBalanceSheetPdfRequest) (*avav1.GetBalanceSheetPdfResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	asOf, err := requireDate(req.GetAsOf(), "as_of")
	if err != nil {
		return nil, err
	}
	businessName, err := s.businessName(ctx, req.GetBusinessId())
	if err != nil {
		return nil, err
	}

	result, err := reporting.BalanceSheet(ctx, s.store.Queries, req.GetBusinessId(), asOf)
	if err != nil {
		return nil, translatePgError(err)
	}
	content, err := pdf.RenderBalanceSheet(businessName, asOf.Format("2006-01-02"), result)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering pdf: %v", err)
	}
	return &avav1.GetBalanceSheetPdfResponse{Content: content}, nil
}

func (s *reportingService) GetIncomeStatementPdf(ctx context.Context, req *avav1.GetIncomeStatementPdfRequest) (*avav1.GetIncomeStatementPdfResponse, error) {
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
	businessName, err := s.businessName(ctx, req.GetBusinessId())
	if err != nil {
		return nil, err
	}

	result, err := reporting.IncomeStatement(ctx, s.store.Queries, req.GetBusinessId(), start, end)
	if err != nil {
		return nil, translatePgError(err)
	}
	periodLabel := start.Format("2006-01-02") + " through " + end.Format("2006-01-02")
	content, err := pdf.RenderIncomeStatement(businessName, periodLabel, result)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering pdf: %v", err)
	}
	return &avav1.GetIncomeStatementPdfResponse{Content: content}, nil
}

func (s *reportingService) GetGeneralLedgerPdf(ctx context.Context, req *avav1.GetGeneralLedgerPdfRequest) (*avav1.GetGeneralLedgerPdfResponse, error) {
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
	businessName, err := s.businessName(ctx, req.GetBusinessId())
	if err != nil {
		return nil, err
	}

	result, err := reporting.GeneralLedger(ctx, s.store.Queries, req.GetAccountId(), start, end)
	if err != nil {
		return nil, translatePgError(err)
	}
	periodLabel := start.Format("2006-01-02") + " through " + end.Format("2006-01-02")
	content, err := pdf.RenderGeneralLedger(businessName, periodLabel, result)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering pdf: %v", err)
	}
	return &avav1.GetGeneralLedgerPdfResponse{Content: content}, nil
}

func (s *reportingService) GetCustomerStatementPdf(ctx context.Context, req *avav1.GetCustomerStatementPdfRequest) (*avav1.GetCustomerStatementPdfResponse, error) {
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
	businessName, err := s.businessName(ctx, contact.BusinessID)
	if err != nil {
		return nil, err
	}

	result, err := reporting.CustomerStatement(ctx, s.store.Queries, req.GetContactId(), start, end)
	if err != nil {
		return nil, translatePgError(err)
	}
	content, err := pdf.RenderCustomerStatement(businessName, result)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rendering pdf: %v", err)
	}
	return &avav1.GetCustomerStatementPdfResponse{Content: content}, nil
}

func (s *reportingService) businessName(ctx context.Context, businessID int64) (string, error) {
	b, err := s.store.Queries.GetBusiness(ctx, businessID)
	if err != nil {
		return "", translatePgError(err)
	}
	return b.Name, nil
}
