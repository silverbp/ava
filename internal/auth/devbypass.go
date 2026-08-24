package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/periodclose"
)

const (
	// DevUserEmail is the fixed identity used for every RPC when
	// AVA_AUTH_MODE=dev.
	DevUserEmail    = "dev@localhost"
	DevBusinessName = "Dev Business"
	devDisplayName  = "Dev User"
)

// EnsureDevUser upserts the fixed local dev app_user and — the first time
// it's called — a "Dev Business" with an OWNER membership, so avactl and the
// API work end-to-end on localhost with zero manual setup before real
// passkey auth (Phase 9) exists.
func EnsureDevUser(ctx context.Context, store *db.Store) (*User, error) {
	u, err := getOrCreateDevAppUser(ctx, store.Queries)
	if err != nil {
		return nil, err
	}

	memberships, err := store.Queries.ListBusinessesForUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		if err := store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
			biz, err := q.CreateBusiness(ctx, sqlcgen.CreateBusinessParams{
				Name:            DevBusinessName,
				CreatedByUserID: &u.ID,
			})
			if err != nil {
				return err
			}
			if _, err = q.CreateBusinessUser(ctx, sqlcgen.CreateBusinessUserParams{
				BusinessID: biz.ID,
				UserID:     u.ID,
				Role:       "OWNER",
			}); err != nil {
				return err
			}
			_, _, err = periodclose.ProvisionSystemAccounts(ctx, q, biz.ID, &u.ID)
			return err
		}); err != nil {
			return nil, err
		}
	}

	return u, nil
}

func getOrCreateDevAppUser(ctx context.Context, q *sqlcgen.Queries) (*User, error) {
	existing, err := q.GetAppUserByEmail(ctx, DevUserEmail)
	if err == nil {
		return &User{ID: existing.ID, Email: existing.Email}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	name := devDisplayName
	created, err := q.CreateAppUser(ctx, sqlcgen.CreateAppUserParams{
		Email:       DevUserEmail,
		DisplayName: &name,
	})
	if err != nil {
		return nil, err
	}
	return &User{ID: created.ID, Email: created.Email}, nil
}
