// Copyright (c) 2025 Silver Blueprints LLC
// SPDX-License-Identifier: MIT

package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/datepb"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/periodclose"
)

type periodCloseService struct {
	avav1.UnimplementedPeriodCloseServiceServer
	store *db.Store
}

func newPeriodCloseService(store *db.Store) *periodCloseService {
	return &periodCloseService{store: store}
}

func (s *periodCloseService) TriggerClose(ctx context.Context, req *avav1.TriggerCloseRequest) (*avav1.TriggerCloseResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "ADMIN"); err != nil {
		return nil, err
	}
	periodEnd, err := requireDate(req.GetPeriodEnd(), "period_end")
	if err != nil {
		return nil, err
	}

	var result *periodclose.CloseResult
	err = s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		var err error
		result, err = periodclose.Close(ctx, q, req.GetBusinessId(), periodEnd, &u.ID)
		return err
	})
	if err != nil {
		return nil, txErrorStatus(err)
	}
	return &avav1.TriggerCloseResponse{PeriodClose: periodCloseWithTransactions(result.PeriodClose, result.LedgerTransactionIDs)}, nil
}

func (s *periodCloseService) ReverseClose(ctx context.Context, req *avav1.ReverseCloseRequest) (*avav1.ReverseCloseResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	pc, err := periodCloseRes.load(ctx, s.store.Queries, req.GetId(), "ADMIN")
	if err != nil {
		return nil, err
	}
	if pc.ReversedAt.Valid {
		return nil, status.Errorf(codes.FailedPrecondition, "period close %d is already reversed", pc.ID)
	}
	// Closes stack and there is no cascade: only the latest unreversed close
	// can be reversed, because any later close still locks this one's period
	// (enforce_period_lock keys on MAX(period_end) over unreversed closes) and
	// its reversing entries could never post. Closes are contiguous, so
	// "the latest unreversed close is not this one" is exactly "a later close
	// exists". periodclose.Reverse repeats the check for direct callers.
	last, err := s.store.Queries.GetLastPeriodClose(ctx, pc.BusinessID)
	if err != nil {
		return nil, translatePgError(err)
	}
	if last.ID != pc.ID {
		return nil, status.Errorf(codes.FailedPrecondition,
			"period close %d (through %s) is not the latest: reverse period close %d (through %s) first - closes are undone newest-first, there is no cascade",
			pc.ID, pc.PeriodEnd.Time.Format("2006-01-02"), last.ID, last.PeriodEnd.Time.Format("2006-01-02"))
	}

	var reversed *sqlcgen.PeriodClose
	err = s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		var err error
		reversed, err = periodclose.Reverse(ctx, q, req.GetId(), &u.ID)
		return err
	})
	if err != nil {
		return nil, txErrorStatus(err)
	}
	pb, err := periodCloseToProto(ctx, s.store.Queries, *reversed)
	if err != nil {
		return nil, err
	}
	return &avav1.ReverseCloseResponse{PeriodClose: pb}, nil
}

func (s *periodCloseService) GetPeriodClose(ctx context.Context, req *avav1.GetPeriodCloseRequest) (*avav1.GetPeriodCloseResponse, error) {
	pc, err := periodCloseRes.load(ctx, s.store.Queries, req.GetId(), "VIEWER")
	if err != nil {
		return nil, err
	}
	pb, err := periodCloseToProto(ctx, s.store.Queries, pc)
	if err != nil {
		return nil, err
	}
	return &avav1.GetPeriodCloseResponse{PeriodClose: pb}, nil
}

func (s *periodCloseService) ListPeriodCloses(ctx context.Context, req *avav1.ListPeriodClosesRequest) (*avav1.ListPeriodClosesResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListPeriodCloses(ctx, req.GetBusinessId())
	if err != nil {
		return nil, translatePgError(err)
	}
	pbs, err := periodClosesToProto(ctx, s.store.Queries, rows)
	if err != nil {
		return nil, err
	}
	return &avav1.ListPeriodClosesResponse{PeriodCloses: pbs}, nil
}

// periodCloseToProto converts one close, loading the transactions it
// generated.
func periodCloseToProto(ctx context.Context, q *sqlcgen.Queries, pc sqlcgen.PeriodClose) (*avav1.PeriodClose, error) {
	return one(periodClosesToProto(ctx, q, []sqlcgen.PeriodClose{pc}))
}

// periodClosesToProto converts a page of closes with their generated
// transaction ids loaded in one query.
func periodClosesToProto(ctx context.Context, q *sqlcgen.Queries, rows []sqlcgen.PeriodClose) ([]*avav1.PeriodClose, error) {
	return withChildren(ctx, q, rows,
		func(pc sqlcgen.PeriodClose) int64 { return pc.ID },
		(*sqlcgen.Queries).ListPeriodCloseEntriesByCloseIDs,
		func(e sqlcgen.PeriodCloseEntry) int64 { return e.PeriodCloseID },
		func(pc sqlcgen.PeriodClose, entries []sqlcgen.PeriodCloseEntry) *avav1.PeriodClose {
			return periodCloseWithTransactions(pc, idsOf(entries, func(e sqlcgen.PeriodCloseEntry) int64 { return e.LedgerTransactionID }))
		})
}

// periodCloseWithTransactions converts a close whose generated transaction
// ids the caller already holds (periodclose.Close returns them directly).
func periodCloseWithTransactions(pc sqlcgen.PeriodClose, transactionIDs []int64) *avav1.PeriodClose {
	return &avav1.PeriodClose{
		Id:                            pc.ID,
		BusinessId:                    pc.BusinessID,
		PeriodStart:                   datepb.ToProto(pc.PeriodStart),
		PeriodEnd:                     datepb.ToProto(pc.PeriodEnd),
		IncomeSummaryAccountId:        pc.IncomeSummaryAccountID,
		RetainedEarningsAccountId:     pc.RetainedEarningsAccountID,
		ClosedAt:                      timestampProto(pc.ClosedAt),
		ReversedAt:                    timestampProto(pc.ReversedAt),
		CreatedByUserId:               pc.CreatedByUserID,
		GeneratedLedgerTransactionIds: transactionIDs,
	}
}
