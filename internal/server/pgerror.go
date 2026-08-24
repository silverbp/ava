package server

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// translatePgError maps a raw Postgres error into a gRPC status, so schema
// invariants (the period-lock trigger, CHECK constraints, unique indexes)
// surface as meaningful client errors instead of an opaque Internal.
func translatePgError(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "P0001": // RAISE EXCEPTION, e.g. enforce_period_lock
			return status.Error(codes.FailedPrecondition, pgErr.Message)
		case "23505": // unique_violation
			return status.Error(codes.AlreadyExists, pgErr.Message)
		case "23514", "23503", "23502": // check_violation, foreign_key_violation, not_null_violation
			return status.Error(codes.InvalidArgument, pgErr.Message)
		}
	}

	return status.Errorf(codes.Internal, "%v", err)
}
