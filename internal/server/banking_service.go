package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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

	created, err := s.store.Queries.CreateBankStatement(ctx, sqlcgen.CreateBankStatementParams{
		BusinessID:      req.GetBusinessId(),
		LedgerAccountID: req.GetLedgerAccountId(),
		StatementName:   req.GetStatementName(),
		StatementDate:   datepb.ToPgDate(req.GetStatementDate()),
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

	return pb, nil
}
