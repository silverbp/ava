// Copyright (c) 2025 Silver Blueprints LLC
// SPDX-License-Identifier: MIT

package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/db/sqlcgen"
)

type referenceDataService struct {
	avav1.UnimplementedReferenceDataServiceServer
	store *db.Store
}

func newReferenceDataService(store *db.Store) *referenceDataService {
	return &referenceDataService{store: store}
}

// ListLedgerReferenceData is global (not business-scoped): the four tables
// it reads are seeded once and shared across every business, so the only
// check is that the caller is authenticated at all.
func (s *referenceDataService) ListLedgerReferenceData(ctx context.Context, _ *avav1.ListLedgerReferenceDataRequest) (*avav1.ListLedgerReferenceDataResponse, error) {
	if _, ok := auth.UserFromContext(ctx); !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}

	accountTypes, err := s.store.Queries.ListLedgerAccountTypes(ctx)
	if err != nil {
		return nil, translatePgError(err)
	}
	cashFlowCategories, err := s.store.Queries.ListCashFlowCategories(ctx)
	if err != nil {
		return nil, translatePgError(err)
	}
	balanceSheetCategories, err := s.store.Queries.ListBalanceSheetCategories(ctx)
	if err != nil {
		return nil, translatePgError(err)
	}
	incomeStatementCategories, err := s.store.Queries.ListIncomeStatementCategories(ctx)
	if err != nil {
		return nil, translatePgError(err)
	}

	resp := &avav1.ListLedgerReferenceDataResponse{}
	for _, at := range accountTypes {
		resp.AccountTypes = append(resp.AccountTypes, ledgerAccountTypeToProto(at))
	}
	for _, c := range cashFlowCategories {
		resp.CashFlowCategories = append(resp.CashFlowCategories, &avav1.CashFlowCategory{
			Id:              c.ID,
			Name:            c.Name,
			DisplaySequence: c.DisplaySequence,
		})
	}
	for _, c := range balanceSheetCategories {
		resp.BalanceSheetCategories = append(resp.BalanceSheetCategories, &avav1.BalanceSheetCategory{
			Id:              c.ID,
			Name:            c.Name,
			DisplaySequence: c.DisplaySequence,
		})
	}
	for _, c := range incomeStatementCategories {
		resp.IncomeStatementCategories = append(resp.IncomeStatementCategories, &avav1.IncomeStatementCategory{
			Id:              c.ID,
			Name:            c.Name,
			DisplaySequence: c.DisplaySequence,
		})
	}
	return resp, nil
}

func ledgerAccountTypeToProto(at sqlcgen.LedgerAccountType) *avav1.LedgerAccountType {
	var description string
	if at.Description != nil {
		description = *at.Description
	}
	return &avav1.LedgerAccountType{
		Id:            at.ID,
		Name:          at.Name,
		Description:   description,
		NormalBalance: at.NormalBalance,
	}
}
