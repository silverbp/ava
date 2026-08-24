// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
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
	if req.GetPeriodEnd() == nil {
		return nil, status.Error(codes.InvalidArgument, "period_end is required")
	}
	periodEnd := datepb.ToPgDate(req.GetPeriodEnd()).Time

	var result *periodclose.CloseResult
	err := s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		var err error
		result, err = periodclose.Close(ctx, q, req.GetBusinessId(), periodEnd, &u.ID)
		return err
	})
	if err != nil {
		return nil, closeErrorStatus(err)
	}

	pb, err := periodCloseToProto(s.store, ctx, result.PeriodClose, result.LedgerTransactionIDs)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting period close: %v", err)
	}
	return &avav1.TriggerCloseResponse{PeriodClose: pb}, nil
}

func (s *periodCloseService) ReverseClose(ctx context.Context, req *avav1.ReverseCloseRequest) (*avav1.ReverseCloseResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}

	existing, err := s.store.Queries.GetPeriodClose(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "period close %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "ADMIN"); err != nil {
		return nil, err
	}

	var reversed *sqlcgen.PeriodClose
	err = s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		var err error
		reversed, err = periodclose.Reverse(ctx, q, req.GetId(), &u.ID)
		return err
	})
	if err != nil {
		return nil, closeErrorStatus(err)
	}

	pb, err := periodCloseToProto(s.store, ctx, *reversed, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting period close: %v", err)
	}
	return &avav1.ReverseCloseResponse{PeriodClose: pb}, nil
}

func (s *periodCloseService) GetPeriodClose(ctx context.Context, req *avav1.GetPeriodCloseRequest) (*avav1.GetPeriodCloseResponse, error) {
	pc, err := s.store.Queries.GetPeriodClose(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "period close %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, pc.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}

	pb, err := periodCloseToProto(s.store, ctx, pc, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting period close: %v", err)
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

	resp := &avav1.ListPeriodClosesResponse{}
	for _, pc := range rows {
		pb, err := periodCloseToProto(s.store, ctx, pc, nil)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting period close: %v", err)
		}
		resp.PeriodCloses = append(resp.PeriodCloses, pb)
	}
	return resp, nil
}

// closeErrorStatus maps periodclose's plain Go errors (contiguity/
// idempotency guard rails) to InvalidArgument, falling back to the
// standard Postgres error translation for anything else (e.g. a
// FailedPrecondition from enforce_period_lock, though that shouldn't fire
// here since Close/Reverse always compute a period_end that's ahead of the
// current lock).
func closeErrorStatus(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	if pgErr := translatePgError(err); status.Code(pgErr) != codes.Internal {
		return pgErr
	}
	return status.Error(codes.InvalidArgument, err.Error())
}

func periodCloseToProto(store *db.Store, ctx context.Context, pc sqlcgen.PeriodClose, transactionIDs []int64) (*avav1.PeriodClose, error) {
	if transactionIDs == nil {
		entries, err := store.Queries.ListPeriodCloseEntries(ctx, pc.ID)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			transactionIDs = append(transactionIDs, e.LedgerTransactionID)
		}
	}

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
	}, nil
}
