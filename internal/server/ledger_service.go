// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/datepb"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/moneypb"
)

// defaultLedgerTransactionPageSize is used when a ListLedgerTransactions
// caller doesn't set page_size.
const defaultLedgerTransactionPageSize = 50

type ledgerAccountService struct {
	avav1.UnimplementedLedgerAccountServiceServer
	store *db.Store
}

func newLedgerAccountService(store *db.Store) *ledgerAccountService {
	return &ledgerAccountService{store: store}
}

func (s *ledgerAccountService) GetLedgerAccount(ctx context.Context, req *avav1.GetLedgerAccountRequest) (*avav1.GetLedgerAccountResponse, error) {
	a, err := s.store.Queries.GetLedgerAccount(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "ledger account %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, a.BusinessID, "VIEWER"); err != nil {
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

	created, err := s.store.Queries.CreateLedgerAccount(ctx, sqlcgen.CreateLedgerAccountParams{
		BusinessID:             req.GetBusinessId(),
		AccountTypeID:          req.GetAccountTypeId(),
		ParentAccountID:        req.ParentAccountId,
		Code:                   req.GetCode(),
		Name:                   req.GetName(),
		Description:            req.Description,
		IsSystem:               false, // system accounts are provisioned internally, never via the API
		IsReconcilable:         req.GetIsReconcilable(),
		IsContainer:            req.GetIsContainer(),
		CashFlowCategoryID:     req.CashFlowCategoryId,
		BalanceSheetCategoryID: req.BalanceSheetCategoryId,
		IsCostOfGoodsSold:      req.GetIsCostOfGoodsSold(),
		CreatedByUserID:        &u.ID,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	return &avav1.CreateLedgerAccountResponse{Account: ledgerAccountToProto(created)}, nil
}

func (s *ledgerAccountService) UpdateLedgerAccount(ctx context.Context, req *avav1.UpdateLedgerAccountRequest) (*avav1.UpdateLedgerAccountResponse, error) {
	existing, err := s.store.Queries.GetLedgerAccount(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "ledger account %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "ADMIN"); err != nil {
		return nil, err
	}

	updated, err := s.store.Queries.UpdateLedgerAccount(ctx, sqlcgen.UpdateLedgerAccountParams{
		ID:                     req.GetId(),
		Name:                   req.Name,
		Description:            req.Description,
		IsReconcilable:         req.IsReconcilable,
		IsContainer:            req.IsContainer,
		CashFlowCategoryID:     req.CashFlowCategoryId,
		BalanceSheetCategoryID: req.BalanceSheetCategoryId,
		IsCostOfGoodsSold:      req.IsCostOfGoodsSold,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	return &avav1.UpdateLedgerAccountResponse{Account: ledgerAccountToProto(updated)}, nil
}

func (s *ledgerAccountService) DeactivateLedgerAccount(ctx context.Context, req *avav1.DeactivateLedgerAccountRequest) (*avav1.DeactivateLedgerAccountResponse, error) {
	existing, err := s.store.Queries.GetLedgerAccount(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "ledger account %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "ADMIN"); err != nil {
		return nil, err
	}
	if existing.IsSystem {
		return nil, status.Error(codes.FailedPrecondition, "system ledger accounts cannot be deactivated")
	}

	deactivated, err := s.store.Queries.DeactivateLedgerAccount(ctx, req.GetId())
	if err != nil {
		return nil, translatePgError(err)
	}
	return &avav1.DeactivateLedgerAccountResponse{Account: ledgerAccountToProto(deactivated)}, nil
}

type ledgerTransactionService struct {
	avav1.UnimplementedLedgerTransactionServiceServer
	store *db.Store
}

func newLedgerTransactionService(store *db.Store) *ledgerTransactionService {
	return &ledgerTransactionService{store: store}
}

func (s *ledgerTransactionService) GetLedgerTransaction(ctx context.Context, req *avav1.GetLedgerTransactionRequest) (*avav1.GetLedgerTransactionResponse, error) {
	t, err := s.store.Queries.GetLedgerTransaction(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "ledger transaction %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, t.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}

	entries, err := s.store.Queries.ListLedgerEntriesByTransaction(ctx, t.ID)
	if err != nil {
		return nil, translatePgError(err)
	}

	pb, err := ledgerTransactionToProto(t, entries)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting ledger transaction: %v", err)
	}
	return &avav1.GetLedgerTransactionResponse{Transaction: pb}, nil
}

func (s *ledgerTransactionService) ListLedgerTransactions(ctx context.Context, req *avav1.ListLedgerTransactionsRequest) (*avav1.ListLedgerTransactionsResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = defaultLedgerTransactionPageSize
	}
	beforeID := int64(1<<63 - 1) // no cursor yet: start from the newest transaction
	if req.GetPageToken() != "" {
		if _, err := fmt.Sscanf(req.GetPageToken(), "%d", &beforeID); err != nil {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
	}

	txns, err := s.store.Queries.ListLedgerTransactions(ctx, sqlcgen.ListLedgerTransactionsParams{
		BusinessID: req.GetBusinessId(),
		BeforeID:   beforeID,
		PageLimit:  pageSize,
	})
	if err != nil {
		return nil, translatePgError(err)
	}

	resp := &avav1.ListLedgerTransactionsResponse{}
	if len(txns) == 0 {
		return resp, nil
	}

	ids := make([]int64, len(txns))
	for i, t := range txns {
		ids[i] = t.ID
	}
	entries, err := s.store.Queries.ListLedgerEntriesByTransactionIDs(ctx, ids)
	if err != nil {
		return nil, translatePgError(err)
	}
	entriesByTxn := make(map[int64][]sqlcgen.LedgerEntry)
	for _, e := range entries {
		entriesByTxn[e.LedgerTransactionID] = append(entriesByTxn[e.LedgerTransactionID], e)
	}

	for _, t := range txns {
		pb, err := ledgerTransactionToProto(t, entriesByTxn[t.ID])
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting ledger transaction: %v", err)
		}
		resp.Transactions = append(resp.Transactions, pb)
	}
	if len(txns) == int(pageSize) {
		resp.NextPageToken = fmt.Sprintf("%d", txns[len(txns)-1].ID)
	}
	return resp, nil
}

func (s *ledgerTransactionService) CreateLedgerTransaction(ctx context.Context, req *avav1.CreateLedgerTransactionRequest) (*avav1.CreateLedgerTransactionResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if len(req.GetEntries()) < 2 {
		return nil, status.Error(codes.InvalidArgument, "a ledger transaction needs at least two entries")
	}
	if req.GetTransactionDate() == nil {
		return nil, status.Error(codes.InvalidArgument, "transaction_date is required")
	}
	if err := validateEntriesBalance(req.GetEntries()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	var (
		txn     sqlcgen.LedgerTransaction
		entries []sqlcgen.LedgerEntry
	)
	err := s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		var err error
		txn, err = q.CreateLedgerTransaction(ctx, sqlcgen.CreateLedgerTransactionParams{
			BusinessID:      req.GetBusinessId(),
			TransactionDate: datepb.ToPgDate(req.GetTransactionDate()),
			Description:     req.Description,
			ReferenceNumber: req.ReferenceNumber,
			CreatedByUserID: &u.ID,
		})
		if err != nil {
			return err
		}

		for _, ne := range req.GetEntries() {
			debit, err := moneypb.ToNumericOrZero(ne.GetDebitAmount())
			if err != nil {
				return err
			}
			credit, err := moneypb.ToNumericOrZero(ne.GetCreditAmount())
			if err != nil {
				return err
			}
			entry, err := q.CreateLedgerEntry(ctx, sqlcgen.CreateLedgerEntryParams{
				BusinessID:          req.GetBusinessId(),
				LedgerTransactionID: txn.ID,
				AccountID:           ne.GetAccountId(),
				DebitAmount:         debit,
				CreditAmount:        credit,
				Description:         ne.Description,
			})
			if err != nil {
				return err
			}
			entries = append(entries, entry)
		}
		return nil
	})
	if err != nil {
		return nil, translatePgError(err)
	}

	pb, err := ledgerTransactionToProto(txn, entries)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting ledger transaction: %v", err)
	}
	return &avav1.CreateLedgerTransactionResponse{Transaction: pb}, nil
}

// validateEntriesBalance enforces SUM(debit) == SUM(credit) across a
// transaction's entries. The DB CHECK on ledger_entry only guarantees each
// individual row has exactly one side populated; nothing in the schema
// enforces the transaction as a whole balances, so the API must.
func validateEntriesBalance(entries []*avav1.NewLedgerEntry) error {
	totalDebit := decimal.Zero
	totalCredit := decimal.Zero
	for i, e := range entries {
		d, err := parseDecimalOrZero(e.GetDebitAmount())
		if err != nil {
			return fmt.Errorf("entry %d: invalid debit_amount: %w", i, err)
		}
		c, err := parseDecimalOrZero(e.GetCreditAmount())
		if err != nil {
			return fmt.Errorf("entry %d: invalid credit_amount: %w", i, err)
		}
		if (d.IsZero() && c.IsZero()) || (!d.IsZero() && !c.IsZero()) {
			return fmt.Errorf("entry %d: exactly one of debit_amount or credit_amount must be set", i)
		}
		totalDebit = totalDebit.Add(d)
		totalCredit = totalCredit.Add(c)
	}
	if !totalDebit.Equal(totalCredit) {
		return fmt.Errorf("entries do not balance: total debits %s != total credits %s", totalDebit, totalCredit)
	}
	return nil
}

func parseDecimalOrZero(d *avav1.Decimal) (decimal.Decimal, error) {
	if d == nil || d.GetValue() == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(d.GetValue())
}

func ledgerAccountToProto(a sqlcgen.LedgerAccount) *avav1.LedgerAccount {
	return &avav1.LedgerAccount{
		Id:                     a.ID,
		BusinessId:             a.BusinessID,
		AccountTypeId:          a.AccountTypeID,
		ParentAccountId:        a.ParentAccountID,
		Code:                   a.Code,
		Name:                   a.Name,
		Description:            a.Description,
		IsSystem:               a.IsSystem,
		IsReconcilable:         a.IsReconcilable,
		IsContainer:            a.IsContainer,
		CashFlowCategoryId:     a.CashFlowCategoryID,
		BalanceSheetCategoryId: a.BalanceSheetCategoryID,
		IsCostOfGoodsSold:      a.IsCostOfGoodsSold,
		DefaultTaxRateId:       a.DefaultTaxRateID,
		IsActive:               a.IsActive,
		CreatedByUserId:        a.CreatedByUserID,
		CreatedAt:              timestampProto(a.CreatedAt),
		UpdatedAt:              timestampProto(a.UpdatedAt),
	}
}

func ledgerTransactionToProto(t sqlcgen.LedgerTransaction, entries []sqlcgen.LedgerEntry) (*avav1.LedgerTransaction, error) {
	pb := &avav1.LedgerTransaction{
		Id:              t.ID,
		BusinessId:      t.BusinessID,
		TransactionDate: datepb.ToProto(t.TransactionDate),
		Description:     t.Description,
		ReferenceNumber: t.ReferenceNumber,
		CreatedByUserId: t.CreatedByUserID,
		CreatedAt:       timestampProto(t.CreatedAt),
	}
	for _, e := range entries {
		pe, err := ledgerEntryToProto(e)
		if err != nil {
			return nil, err
		}
		pb.Entries = append(pb.Entries, pe)
	}
	return pb, nil
}

func ledgerEntryToProto(e sqlcgen.LedgerEntry) (*avav1.LedgerEntry, error) {
	debit, err := moneypb.ToProto(e.DebitAmount)
	if err != nil {
		return nil, err
	}
	credit, err := moneypb.ToProto(e.CreditAmount)
	if err != nil {
		return nil, err
	}
	return &avav1.LedgerEntry{
		Id:                  e.ID,
		LedgerTransactionId: e.LedgerTransactionID,
		AccountId:           e.AccountID,
		DebitAmount:         debit,
		CreditAmount:        credit,
		Description:         e.Description,
	}, nil
}
