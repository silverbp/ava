// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/datepb"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/ledgermath"
	"github.com/silverbp/ava/internal/moneypb"
)

type bankStatementService struct {
	avav1.UnimplementedBankStatementServiceServer
	store *db.Store
}

func newBankStatementService(store *db.Store) *bankStatementService {
	return &bankStatementService{store: store}
}

func (s *bankStatementService) GetBankStatement(ctx context.Context, req *avav1.GetBankStatementRequest) (*avav1.GetBankStatementResponse, error) {
	bs, err := s.store.Queries.GetBankStatement(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "bank statement %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, bs.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}

	pb, err := s.bankStatementToProto(ctx, bs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting bank statement: %v", err)
	}
	return &avav1.GetBankStatementResponse{BankStatement: pb}, nil
}

func (s *bankStatementService) ListBankStatements(ctx context.Context, req *avav1.ListBankStatementsRequest) (*avav1.ListBankStatementsResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListBankStatements(ctx, req.GetBusinessId())
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListBankStatementsResponse{}
	for _, bs := range rows {
		pb, err := s.bankStatementToProto(ctx, bs)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting bank statement: %v", err)
		}
		resp.BankStatements = append(resp.BankStatements, pb)
	}
	return resp, nil
}

func (s *bankStatementService) CreateBankStatement(ctx context.Context, req *avav1.CreateBankStatementRequest) (*avav1.CreateBankStatementResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if req.GetStatementName() == "" || req.GetStatementDate() == nil {
		return nil, status.Error(codes.InvalidArgument, "statement_name and statement_date are required")
	}

	account, err := s.store.Queries.GetLedgerAccount(ctx, req.GetLedgerAccountId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "ledger account %d not found", req.GetLedgerAccountId())
		}
		return nil, translatePgError(err)
	}
	if !account.IsReconcilable {
		return nil, status.Errorf(codes.InvalidArgument, "ledger account %d is not marked is_reconcilable", account.ID)
	}

	opening, err := moneypb.ToNumeric(req.OpeningBalance)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid opening_balance: %v", err)
	}
	closing, err := moneypb.ToNumeric(req.ClosingBalance)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid closing_balance: %v", err)
	}

	statementDate := datepb.ToPgDate(req.GetStatementDate())
	if err := s.checkOpeningBalanceChaining(ctx, req.GetLedgerAccountId(), 0, statementDate, opening, req.GetAllowOpeningMismatch()); err != nil {
		return nil, err
	}

	created, err := s.store.Queries.CreateBankStatement(ctx, sqlcgen.CreateBankStatementParams{
		BusinessID:      req.GetBusinessId(),
		LedgerAccountID: req.GetLedgerAccountId(),
		StatementName:   req.GetStatementName(),
		StatementDate:   statementDate,
		OpeningBalance:  opening,
		ClosingBalance:  closing,
		CreatedByUserID: &u.ID,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := s.bankStatementToProto(ctx, created)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting bank statement: %v", err)
	}
	return &avav1.CreateBankStatementResponse{BankStatement: pb}, nil
}

// checkOpeningBalanceChaining rejects an opening balance that doesn't match
// the prior statement's closing balance for the same account, unless
// allowMismatch is set (no prior statement - the first one on an account -
// or a deliberate mid-history import both need the override). Compares
// against the closest statement strictly before statementDate, skipping
// excludeID (the statement being updated, so a date moved later never
// chains against its own closing balance; 0 on create).
func (s *bankStatementService) checkOpeningBalanceChaining(ctx context.Context, ledgerAccountID int32, excludeID int64, statementDate pgtype.Date, opening pgtype.Numeric, allowMismatch bool) error {
	if allowMismatch || !opening.Valid {
		return nil
	}
	prior, err := s.store.Queries.GetLatestBankStatementForAccount(ctx, sqlcgen.GetLatestBankStatementForAccountParams{
		LedgerAccountID: ledgerAccountID,
		BeforeDate:      statementDate,
		ExcludeID:       excludeID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return translatePgError(err)
	}
	if !prior.ClosingBalance.Valid {
		return nil
	}
	openingDec, err := ledgermath.NumericToDecimal(opening)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid opening_balance: %v", err)
	}
	priorClosingDec, err := ledgermath.NumericToDecimal(prior.ClosingBalance)
	if err != nil {
		return status.Errorf(codes.Internal, "reading prior statement's closing_balance: %v", err)
	}
	if !openingDec.Equal(priorClosingDec) {
		return status.Errorf(codes.InvalidArgument,
			"opening_balance %s does not match statement %d's closing_balance %s for this account - pass allow_opening_mismatch to override",
			openingDec, prior.ID, priorClosingDec)
	}
	return nil
}

func (s *bankStatementService) UpdateBankStatement(ctx context.Context, req *avav1.UpdateBankStatementRequest) (*avav1.UpdateBankStatementResponse, error) {
	existing, err := s.store.Queries.GetBankStatement(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "bank statement %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}

	opening, err := moneypb.ToNumeric(req.OpeningBalance)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid opening_balance: %v", err)
	}
	closing, err := moneypb.ToNumeric(req.ClosingBalance)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid closing_balance: %v", err)
	}
	statementDate := datepb.ToPgDate(req.GetStatementDate())

	// Only re-run the chaining check when something it depends on is
	// changing - a rename or closing-balance fix on a statement that was
	// imported with allow_opening_mismatch must not be re-judged on its
	// (still deliberately mismatched) opening balance.
	if opening.Valid || statementDate.Valid {
		effectiveOpening, effectiveDate := opening, statementDate
		if !effectiveOpening.Valid {
			effectiveOpening = existing.OpeningBalance
		}
		if !effectiveDate.Valid {
			effectiveDate = existing.StatementDate
		}
		if err := s.checkOpeningBalanceChaining(ctx, existing.LedgerAccountID, existing.ID, effectiveDate, effectiveOpening, req.GetAllowOpeningMismatch()); err != nil {
			return nil, err
		}
	}

	updated, err := s.store.Queries.UpdateBankStatement(ctx, sqlcgen.UpdateBankStatementParams{
		ID:              req.GetId(),
		StatementName:   req.StatementName,
		StatementDate:   statementDate,
		OpeningBalance:  opening,
		ClosingBalance:  closing,
		ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, "bank statement", req.GetId(), req.GetResourceVersion())
	}
	pb, err := s.bankStatementToProto(ctx, updated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting bank statement: %v", err)
	}
	return &avav1.UpdateBankStatementResponse{BankStatement: pb}, nil
}

func (s *bankStatementService) DeactivateBankStatement(ctx context.Context, req *avav1.DeactivateBankStatementRequest) (*avav1.DeactivateBankStatementResponse, error) {
	existing, err := s.store.Queries.GetBankStatement(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "bank statement %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}

	lineCount, err := s.store.Queries.CountBankStatementLines(ctx, existing.ID)
	if err != nil {
		return nil, translatePgError(err)
	}
	if lineCount > 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"bank statement %d still has %d reconciled line(s) - unreconcile them first", existing.ID, lineCount)
	}

	deactivated, err := s.store.Queries.DeactivateBankStatement(ctx, sqlcgen.DeactivateBankStatementParams{
		ID:              req.GetId(),
		ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, "bank statement", req.GetId(), req.GetResourceVersion())
	}
	pb, err := s.bankStatementToProto(ctx, deactivated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting bank statement: %v", err)
	}
	return &avav1.DeactivateBankStatementResponse{BankStatement: pb}, nil
}

func (s *bankStatementService) ReconcileLedgerTransactions(ctx context.Context, req *avav1.ReconcileLedgerTransactionsRequest) (*avav1.ReconcileLedgerTransactionsResponse, error) {
	bs, err := s.store.Queries.GetBankStatement(ctx, req.GetBankStatementId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "bank statement %d not found", req.GetBankStatementId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, bs.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}
	if len(req.GetLedgerTransactionIds()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one ledger_transaction_id is required")
	}

	err = s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		existing, err := q.ListBankStatementLines(ctx, bs.ID)
		if err != nil {
			return err
		}
		nextSeq := int32(len(existing)) + 1

		for _, txnID := range req.GetLedgerTransactionIds() {
			exists, err := q.LedgerEntryExistsForAccount(ctx, sqlcgen.LedgerEntryExistsForAccountParams{
				LedgerTransactionID: txnID,
				AccountID:           bs.LedgerAccountID,
			})
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("ledger transaction %d does not post to account %d", txnID, bs.LedgerAccountID)
			}

			if _, err := q.CreateBankStatementLine(ctx, sqlcgen.CreateBankStatementLineParams{
				BankStatementID:     bs.ID,
				LedgerTransactionID: txnID,
				DisplaySequence:     nextSeq,
			}); err != nil {
				return err
			}
			nextSeq++
		}
		return nil
	})
	if err != nil {
		return nil, closeErrorStatus(err)
	}

	pb, err := s.bankStatementToProto(ctx, bs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting bank statement: %v", err)
	}
	return &avav1.ReconcileLedgerTransactionsResponse{BankStatement: pb}, nil
}

func (s *bankStatementService) UnreconcileLedgerTransactions(ctx context.Context, req *avav1.UnreconcileLedgerTransactionsRequest) (*avav1.UnreconcileLedgerTransactionsResponse, error) {
	bs, err := s.store.Queries.GetBankStatement(ctx, req.GetBankStatementId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "bank statement %d not found", req.GetBankStatementId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, bs.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}
	if len(req.GetLedgerTransactionIds()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one ledger_transaction_id is required")
	}

	err = s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		for _, txnID := range req.GetLedgerTransactionIds() {
			if err := q.DeleteBankStatementLine(ctx, sqlcgen.DeleteBankStatementLineParams{
				BankStatementID:     bs.ID,
				LedgerTransactionID: txnID,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, closeErrorStatus(err)
	}

	pb, err := s.bankStatementToProto(ctx, bs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting bank statement: %v", err)
	}
	return &avav1.UnreconcileLedgerTransactionsResponse{BankStatement: pb}, nil
}

func (s *bankStatementService) ListUnreconciledLedgerTransactions(ctx context.Context, req *avav1.ListUnreconciledLedgerTransactionsRequest) (*avav1.ListUnreconciledLedgerTransactionsResponse, error) {
	account, err := s.store.Queries.GetLedgerAccount(ctx, req.GetLedgerAccountId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "ledger account %d not found", req.GetLedgerAccountId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, account.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	throughDate, err := requireDate(req.GetThroughDate(), "through_date")
	if err != nil {
		return nil, err
	}

	rows, err := s.store.Queries.ListUnreconciledLedgerTransactions(ctx, sqlcgen.ListUnreconciledLedgerTransactionsParams{
		AccountID:   req.GetLedgerAccountId(),
		ThroughDate: ledgermath.PgDate(throughDate),
	})
	if err != nil {
		return nil, translatePgError(err)
	}

	resp := &avav1.ListUnreconciledLedgerTransactionsResponse{}
	for _, t := range rows {
		entries, err := s.store.Queries.ListLedgerEntriesByTransaction(ctx, t.ID)
		if err != nil {
			return nil, translatePgError(err)
		}
		pb, err := ledgerTransactionToProto(t, entries)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting ledger transaction: %v", err)
		}
		resp.Transactions = append(resp.Transactions, pb)
	}
	return resp, nil
}

func (s *bankStatementService) bankStatementToProto(ctx context.Context, bs sqlcgen.BankStatement) (*avav1.BankStatement, error) {
	opening, err := moneypb.ToProto(bs.OpeningBalance)
	if err != nil {
		return nil, err
	}
	closing, err := moneypb.ToProto(bs.ClosingBalance)
	if err != nil {
		return nil, err
	}

	lines, err := s.store.Queries.ListBankStatementLines(ctx, bs.ID)
	if err != nil {
		return nil, err
	}
	pb := &avav1.BankStatement{
		Id:              bs.ID,
		BusinessId:      bs.BusinessID,
		LedgerAccountId: bs.LedgerAccountID,
		StatementName:   bs.StatementName,
		StatementDate:   datepb.ToProto(bs.StatementDate),
		OpeningBalance:  opening,
		ClosingBalance:  closing,
		CreatedByUserId: bs.CreatedByUserID,
		CreatedAt:       timestampProto(bs.CreatedAt),
		ResourceVersion: bs.ResourceVersion,
	}
	for _, l := range lines {
		pb.Lines = append(pb.Lines, &avav1.BankStatementLine{
			Id:                  l.ID,
			BankStatementId:     l.BankStatementID,
			LedgerTransactionId: l.LedgerTransactionID,
			DisplaySequence:     l.DisplaySequence,
		})
	}

	activity, err := s.store.Queries.SumReconciledActivity(ctx, sqlcgen.SumReconciledActivityParams{
		AccountID:       bs.LedgerAccountID,
		BankStatementID: bs.ID,
	})
	if err != nil {
		return nil, err
	}
	account, err := s.store.Queries.GetLedgerAccount(ctx, bs.LedgerAccountID)
	if err != nil {
		return nil, err
	}
	accountType, err := s.store.Queries.GetLedgerAccountType(ctx, account.AccountTypeID)
	if err != nil {
		return nil, err
	}
	debit, err := ledgermath.NumericToDecimal(activity.TotalDebit)
	if err != nil {
		return nil, err
	}
	credit, err := ledgermath.NumericToDecimal(activity.TotalCredit)
	if err != nil {
		return nil, err
	}
	openingDec, err := ledgermath.NumericToDecimal(bs.OpeningBalance)
	if err != nil {
		return nil, err
	}
	reconciled := openingDec.Add(ledgermath.NetBalance(accountType.NormalBalance, debit, credit))
	pb.ReconciledBalance = decimalToProto(reconciled)

	if bs.ClosingBalance.Valid {
		closingDec, err := ledgermath.NumericToDecimal(bs.ClosingBalance)
		if err != nil {
			return nil, err
		}
		pb.Difference = decimalToProto(closingDec.Sub(reconciled))
	}

	return pb, nil
}
