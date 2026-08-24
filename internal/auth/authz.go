package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/silverbp/ava/internal/db/sqlcgen"
)

var roleRank = map[string]int{
	"VIEWER": 0,
	"MEMBER": 1,
	"ADMIN":  2,
	"OWNER":  3,
}

// RequireBusinessRole checks that the calling user (resolved onto ctx by
// UnaryInterceptor) is a member of businessID with at least minRole.
// Returns Unauthenticated if no user was resolved, NotFound if the caller
// has no membership at all (avoids leaking whether a business id exists to
// non-members), or PermissionDenied if their role is too low.
func RequireBusinessRole(ctx context.Context, q *sqlcgen.Queries, businessID int64, minRole string) error {
	u, ok := UserFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "no authenticated user")
	}

	membership, err := q.GetBusinessUser(ctx, sqlcgen.GetBusinessUserParams{
		BusinessID: businessID,
		UserID:     u.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return status.Errorf(codes.NotFound, "business %d not found", businessID)
		}
		return status.Errorf(codes.Internal, "checking business membership: %v", err)
	}

	if roleRank[membership.Role] < roleRank[minRole] {
		return status.Errorf(codes.PermissionDenied, "role %s does not meet required role %s for business %d", membership.Role, minRole, businessID)
	}
	return nil
}
