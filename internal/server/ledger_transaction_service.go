// Copyright (c) 2025 Silver Blueprints LLC
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/datepb"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/ledgerpost"
	"github.com/silverbp/ava/internal/moneypb"
)

// defaultLedgerTransactionPageSize is used when a ListLedgerTransactions
// caller doesn't set page_size.
const defaultLedgerTransactionPageSize = 50

type ledgerTransactionService struct {
	avav1.UnimplementedLedgerTransactionServiceServer
	store *db.Store
}

func newLedgerTransactionService(store *db.Store) *ledgerTransactionService {
	return &ledgerTransactionService{store: store}
}

func (s *ledgerTransactionService) GetLedgerTransaction(ctx context.Context, req *avav1.GetLedgerTransactionRequest) (*avav1.GetLedgerTransactionResponse, error) {
	t, err := ledgerTransactionRes.load(ctx, s.store.Queries, req.GetId(), "VIEWER")
	if err != nil {
		return nil, err
	}
	pb, err := ledgerTransactionToProto(ctx, s.store.Queries, t)
	if err != nil {
		return nil, err
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

	// Filters are optional and AND-combined; the query treats each NULL as
	// "no constraint". account_id is vetted up front so a foreign or unknown
	// account is an error rather than a silently empty page.
	if req.AccountId != nil {
		if _, err := ledgerAccountRes.requireInBusiness(ctx, s.store.Queries, req.GetBusinessId(), int64(req.GetAccountId())); err != nil {
			return nil, err
		}
	}
	startDate, endDate := datepb.ToPgDate(req.StartDate), datepb.ToPgDate(req.EndDate)
	if startDate.Valid && endDate.Valid && startDate.Time.After(endDate.Time) {
		return nil, status.Error(codes.InvalidArgument, "start_date must not be after end_date")
	}

	txns, err := s.store.Queries.ListLedgerTransactions(ctx, sqlcgen.ListLedgerTransactionsParams{
		BusinessID:          req.GetBusinessId(),
		BeforeID:            beforeID,
		StartDate:           startDate,
		EndDate:             endDate,
		DescriptionContains: req.DescriptionContains,
		AccountID:           req.AccountId,
		PageLimit:           pageSize,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	pbs, err := ledgerTransactionsToProto(ctx, s.store.Queries, txns)
	if err != nil {
		return nil, err
	}
	resp := &avav1.ListLedgerTransactionsResponse{Transactions: pbs}
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

		for i, ne := range req.GetEntries() {
			if _, err := ledgerAccountRes.requireInBusiness(ctx, q, req.GetBusinessId(), int64(ne.GetAccountId())); err != nil {
				return prefixStatus(err, "entry %d", i)
			}
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
		return nil, txErrorStatus(err)
	}
	return &avav1.CreateLedgerTransactionResponse{Transaction: ledgerTransactionWithEntries(txn, entries)}, nil
}

func (s *ledgerTransactionService) ReverseLedgerTransaction(ctx context.Context, req *avav1.ReverseLedgerTransactionRequest) (*avav1.ReverseLedgerTransactionResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	existing, err := ledgerTransactionRes.load(ctx, s.store.Queries, req.GetId(), "MEMBER")
	if err != nil {
		return nil, err
	}

	if existing.ReversesLedgerTransactionID != nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"ledger transaction %d is itself the reversal of transaction %d - to reinstate that one, post it again rather than reversing the reversal", existing.ID, *existing.ReversesLedgerTransactionID)
	}
	if reversal, err := s.store.Queries.GetReversalOfLedgerTransaction(ctx, existing.ID); err == nil {
		return nil, status.Errorf(codes.FailedPrecondition,
			"ledger transaction %d has already been reversed by transaction %d", existing.ID, reversal.ID)
	} else if !isNoRows(err) {
		return nil, translatePgError(err)
	}

	documentCount, err := s.store.Queries.CountDocumentsForLedgerTransaction(ctx, existing.ID)
	if err != nil {
		return nil, translatePgError(err)
	}
	if documentCount > 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"ledger transaction %d is linked from an invoice or payment - correct it through `invoice cancel` or `payment void` instead, which keep paid_amount/balance_due in sync", existing.ID)
	}

	reversalDate, err := resolveReversalDate(existing.TransactionDate, req.ReversalDate, "transaction_date")
	if err != nil {
		return nil, err
	}

	var (
		reversedTxn sqlcgen.LedgerTransaction
		entries     []sqlcgen.LedgerEntry
	)
	description := fmt.Sprintf("Reversal of ledger transaction %d", existing.ID)
	// A concurrent reverse of the same id that commits first trips the
	// partial unique index on reverses_ledger_transaction_id here, surfacing
	// as AlreadyExists via txErrorStatus rather than a second reversal.
	err = s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		var err error
		reversedTxn, err = ledgerpost.ReverseTransaction(ctx, q, existing.BusinessID, existing.ID, reversalDate, description, &u.ID)
		if err != nil {
			return err
		}
		entries, err = q.ListLedgerEntriesByTransaction(ctx, reversedTxn.ID)
		return err
	})
	if err != nil {
		return nil, txErrorStatus(err)
	}
	return &avav1.ReverseLedgerTransactionResponse{Transaction: ledgerTransactionWithEntries(reversedTxn, entries)}, nil
}

// validateEntriesBalance enforces SUM(debit) == SUM(credit) across a
// transaction's entries. The DB CHECK on ledger_entry only guarantees each
// individual row has exactly one side populated; nothing in the schema
// enforces the transaction as a whole balances, so the API must.
func validateEntriesBalance(entries []*avav1.NewLedgerEntry) error {
	totalDebit := decimal.Zero
	totalCredit := decimal.Zero
	for i, e := range entries {
		d, err := parseDecimalOrDefault(e.GetDebitAmount(), "0")
		if err != nil {
			return fmt.Errorf("entry %d: invalid debit_amount: %w", i, err)
		}
		c, err := parseDecimalOrDefault(e.GetCreditAmount(), "0")
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

// ledgerTransactionToProto converts one transaction, loading its entries.
func ledgerTransactionToProto(ctx context.Context, q *sqlcgen.Queries, t sqlcgen.LedgerTransaction) (*avav1.LedgerTransaction, error) {
	return one(ledgerTransactionsToProto(ctx, q, []sqlcgen.LedgerTransaction{t}))
}

// ledgerTransactionsToProto converts a page of transactions with their
// entries loaded in one query.
func ledgerTransactionsToProto(ctx context.Context, q *sqlcgen.Queries, txns []sqlcgen.LedgerTransaction) ([]*avav1.LedgerTransaction, error) {
	return withChildren(ctx, q, txns,
		func(t sqlcgen.LedgerTransaction) int64 { return t.ID },
		(*sqlcgen.Queries).ListLedgerEntriesByTransactionIDs,
		func(e sqlcgen.LedgerEntry) int64 { return e.LedgerTransactionID },
		ledgerTransactionWithEntries)
}

// ledgerTransactionWithEntries converts a transaction whose entries the
// caller already holds (just written inside the same ExecTx).
func ledgerTransactionWithEntries(t sqlcgen.LedgerTransaction, entries []sqlcgen.LedgerEntry) *avav1.LedgerTransaction {
	pb := &avav1.LedgerTransaction{
		Id:              t.ID,
		BusinessId:      t.BusinessID,
		TransactionDate: datepb.ToProto(t.TransactionDate),
		Description:     t.Description,
		ReferenceNumber: t.ReferenceNumber,
		CreatedByUserId: t.CreatedByUserID,
		CreatedAt:       timestampProto(t.CreatedAt),

		ReversesLedgerTransactionId: t.ReversesLedgerTransactionID,
	}
	for _, e := range entries {
		pb.Entries = append(pb.Entries, &avav1.LedgerEntry{
			Id:                  e.ID,
			LedgerTransactionId: e.LedgerTransactionID,
			AccountId:           e.AccountID,
			DebitAmount:         moneypb.ToProto(e.DebitAmount),
			CreditAmount:        moneypb.ToProto(e.CreditAmount),
			Description:         e.Description,
		})
	}
	return pb
}
