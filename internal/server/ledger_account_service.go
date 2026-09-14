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

type ledgerAccountService struct {
	avav1.UnimplementedLedgerAccountServiceServer
	store *db.Store
}

func newLedgerAccountService(store *db.Store) *ledgerAccountService {
	return &ledgerAccountService{store: store}
}

func (s *ledgerAccountService) GetLedgerAccount(ctx context.Context, req *avav1.GetLedgerAccountRequest) (*avav1.GetLedgerAccountResponse, error) {
	a, err := ledgerAccountRes.load(ctx, s.store.Queries, int64(req.GetId()), "VIEWER")
	if err != nil {
		return nil, err
	}
	return &avav1.GetLedgerAccountResponse{Account: ledgerAccountToProto(a)}, nil
}

func (s *ledgerAccountService) ListLedgerAccounts(ctx context.Context, req *avav1.ListLedgerAccountsRequest) (*avav1.ListLedgerAccountsResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}

	rows, err := s.store.Queries.ListLedgerAccounts(ctx, req.GetBusinessId())
	if err != nil {
		return nil, translatePgError(err)
	}

	resp := &avav1.ListLedgerAccountsResponse{}
	for _, a := range rows {
		resp.Accounts = append(resp.Accounts, ledgerAccountToProto(a))
	}
	return resp, nil
}

func (s *ledgerAccountService) CreateLedgerAccount(ctx context.Context, req *avav1.CreateLedgerAccountRequest) (*avav1.CreateLedgerAccountResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if req.GetCode() == "" || req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "code and name are required")
	}
	if req.ParentAccountId != nil {
		if _, err := ledgerAccountRes.requireInBusiness(ctx, s.store.Queries, req.GetBusinessId(), int64(req.GetParentAccountId())); err != nil {
			return nil, err
		}
	}

	created, err := s.store.Queries.CreateLedgerAccount(ctx, sqlcgen.CreateLedgerAccountParams{
		BusinessID:                req.GetBusinessId(),
		AccountTypeID:             req.GetAccountTypeId(),
		ParentAccountID:           req.ParentAccountId,
		Code:                      req.GetCode(),
		Name:                      req.GetName(),
		Description:               req.Description,
		IsSystem:                  false, // system accounts are provisioned internally, never via the API
		IsReconcilable:            req.GetIsReconcilable(),
		IsContainer:               req.GetIsContainer(),
		CashFlowCategoryID:        req.CashFlowCategoryId,
		BalanceSheetCategoryID:    req.BalanceSheetCategoryId,
		IncomeStatementCategoryID: req.IncomeStatementCategoryId,
		CreatedByUserID:           &u.ID,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	return &avav1.CreateLedgerAccountResponse{Account: ledgerAccountToProto(created)}, nil
}

func (s *ledgerAccountService) UpdateLedgerAccount(ctx context.Context, req *avav1.UpdateLedgerAccountRequest) (*avav1.UpdateLedgerAccountResponse, error) {
	if _, err := ledgerAccountRes.load(ctx, s.store.Queries, int64(req.GetId()), "ADMIN"); err != nil {
		return nil, err
	}

	updated, err := s.store.Queries.UpdateLedgerAccount(ctx, sqlcgen.UpdateLedgerAccountParams{
		ID:                        req.GetId(),
		Name:                      req.Name,
		Description:               req.Description,
		IsReconcilable:            req.IsReconcilable,
		IsContainer:               req.IsContainer,
		CashFlowCategoryID:        req.CashFlowCategoryId,
		BalanceSheetCategoryID:    req.BalanceSheetCategoryId,
		IncomeStatementCategoryID: req.IncomeStatementCategoryId,
		ResourceVersion:           expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, ledgerAccountRes.kind, int64(req.GetId()), req.GetResourceVersion())
	}
	return &avav1.UpdateLedgerAccountResponse{Account: ledgerAccountToProto(updated)}, nil
}

func (s *ledgerAccountService) DeactivateLedgerAccount(ctx context.Context, req *avav1.DeactivateLedgerAccountRequest) (*avav1.DeactivateLedgerAccountResponse, error) {
	existing, err := ledgerAccountRes.load(ctx, s.store.Queries, int64(req.GetId()), "ADMIN")
	if err != nil {
		return nil, err
	}
	if existing.IsSystem {
		return nil, status.Error(codes.FailedPrecondition, "system ledger accounts cannot be deactivated")
	}

	deactivated, err := s.store.Queries.DeactivateLedgerAccount(ctx, sqlcgen.DeactivateLedgerAccountParams{
		ID:              req.GetId(),
		ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, ledgerAccountRes.kind, int64(req.GetId()), req.GetResourceVersion())
	}
	return &avav1.DeactivateLedgerAccountResponse{Account: ledgerAccountToProto(deactivated)}, nil
}

func ledgerAccountToProto(a sqlcgen.LedgerAccount) *avav1.LedgerAccount {
	return &avav1.LedgerAccount{
		Id:                        a.ID,
		BusinessId:                a.BusinessID,
		AccountTypeId:             a.AccountTypeID,
		ParentAccountId:           a.ParentAccountID,
		Code:                      a.Code,
		Name:                      a.Name,
		Description:               a.Description,
		IsSystem:                  a.IsSystem,
		IsReconcilable:            a.IsReconcilable,
		IsContainer:               a.IsContainer,
		CashFlowCategoryId:        a.CashFlowCategoryID,
		BalanceSheetCategoryId:    a.BalanceSheetCategoryID,
		IncomeStatementCategoryId: a.IncomeStatementCategoryID,
		IsActive:                  a.IsActive,
		CreatedByUserId:           a.CreatedByUserID,
		CreatedAt:                 timestampProto(a.CreatedAt),
		UpdatedAt:                 timestampProto(a.UpdatedAt),
		ResourceVersion:           a.ResourceVersion,
	}
}
