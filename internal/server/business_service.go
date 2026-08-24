package server

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
	"github.com/silverbp/ava/internal/auth"
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/moneypb"
	"github.com/silverbp/ava/internal/periodclose"
)

type businessService struct {
	avav1.UnimplementedBusinessServiceServer
	store *db.Store
}

func newBusinessService(store *db.Store) *businessService {
	return &businessService{store: store}
}

func (s *businessService) GetBusiness(ctx context.Context, req *avav1.GetBusinessRequest) (*avav1.GetBusinessResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetId(), "VIEWER"); err != nil {
		return nil, err
	}

	b, err := s.store.Queries.GetBusiness(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "business %d not found", req.GetId())
		}
		return nil, status.Errorf(codes.Internal, "getting business: %v", err)
	}

	pb, err := businessToProto(b)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting business: %v", err)
	}
	return &avav1.GetBusinessResponse{Business: pb}, nil
}

func (s *businessService) ListMyBusinesses(ctx context.Context, _ *avav1.ListMyBusinessesRequest) (*avav1.ListMyBusinessesResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}

	rows, err := s.store.Queries.ListBusinessesForUser(ctx, u.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing businesses: %v", err)
	}

	resp := &avav1.ListMyBusinessesResponse{}
	for _, row := range rows {
		pb, err := businessToProto(sqlcgen.Business{
			ID:                      row.ID,
			Name:                    row.Name,
			TaxID:                   row.TaxID,
			AddressLine1:            row.AddressLine1,
			AddressLine2:            row.AddressLine2,
			City:                    row.City,
			State:                   row.State,
			PostalCode:              row.PostalCode,
			Country:                 row.Country,
			Phone:                   row.Phone,
			Email:                   row.Email,
			WebsiteUrl:              row.WebsiteUrl,
			LogoUrl:                 row.LogoUrl,
			DefaultPaymentTermsDays: row.DefaultPaymentTermsDays,
			DefaultTaxRate:          row.DefaultTaxRate,
			DefaultInvoiceTerms:     row.DefaultInvoiceTerms,
			DefaultEstimateTerms:    row.DefaultEstimateTerms,
			InvoiceNumberPrefix:     row.InvoiceNumberPrefix,
			EstimateNumberPrefix:    row.EstimateNumberPrefix,
			NextInvoiceNumber:       row.NextInvoiceNumber,
			NextEstimateNumber:      row.NextEstimateNumber,
			Timezone:                row.Timezone,
			CurrencyCode:            row.CurrencyCode,
			IsActive:                row.IsActive,
			CreatedByUserID:         row.CreatedByUserID,
			CreatedAt:               row.CreatedAt,
			UpdatedAt:               row.UpdatedAt,
			DeletedAt:               row.DeletedAt,
		})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting business: %v", err)
		}
		resp.Memberships = append(resp.Memberships, &avav1.BusinessMembership{
			Business: pb,
			Role:     row.MembershipRole,
		})
	}
	return resp, nil
}

func (s *businessService) CreateBusiness(ctx context.Context, req *avav1.CreateBusinessRequest) (*avav1.CreateBusinessResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	var created sqlcgen.Business
	err := s.store.ExecTx(ctx, func(q *sqlcgen.Queries) error {
		var err error
		created, err = q.CreateBusiness(ctx, sqlcgen.CreateBusinessParams{
			Name:            req.GetName(),
			TaxID:           req.TaxId,
			AddressLine1:    req.AddressLine1,
			AddressLine2:    req.AddressLine2,
			City:            req.City,
			State:           req.State,
			PostalCode:      req.PostalCode,
			Country:         req.Country,
			Phone:           req.Phone,
			Email:           req.Email,
			CreatedByUserID: &u.ID,
		})
		if err != nil {
			return err
		}
		if _, err = q.CreateBusinessUser(ctx, sqlcgen.CreateBusinessUserParams{
			BusinessID: created.ID,
			UserID:     u.ID,
			Role:       "OWNER",
		}); err != nil {
			return err
		}

		// Every business needs Income Summary / Retained Earnings before it
		// can ever be closed (see docs/architecture.md#period-close).
		_, _, err = periodclose.ProvisionSystemAccounts(ctx, q, created.ID, &u.ID)
		return err
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "creating business: %v", err)
	}

	pb, err := businessToProto(created)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting business: %v", err)
	}
	return &avav1.CreateBusinessResponse{Business: pb}, nil
}

func (s *businessService) UpdateBusiness(ctx context.Context, req *avav1.UpdateBusinessRequest) (*avav1.UpdateBusinessResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetId(), "ADMIN"); err != nil {
		return nil, err
	}

	updated, err := s.store.Queries.UpdateBusiness(ctx, sqlcgen.UpdateBusinessParams{
		ID:           req.GetId(),
		Name:         req.Name,
		TaxID:        req.TaxId,
		AddressLine1: req.AddressLine1,
		AddressLine2: req.AddressLine2,
		City:         req.City,
		State:        req.State,
		PostalCode:   req.PostalCode,
		Country:      req.Country,
		Phone:        req.Phone,
		Email:        req.Email,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "business %d not found", req.GetId())
		}
		return nil, status.Errorf(codes.Internal, "updating business: %v", err)
	}

	pb, err := businessToProto(updated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting business: %v", err)
	}
	return &avav1.UpdateBusinessResponse{Business: pb}, nil
}

func (s *businessService) DeactivateBusiness(ctx context.Context, req *avav1.DeactivateBusinessRequest) (*avav1.DeactivateBusinessResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetId(), "ADMIN"); err != nil {
		return nil, err
	}

	deactivated, err := s.store.Queries.DeactivateBusiness(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "business %d not found", req.GetId())
		}
		return nil, status.Errorf(codes.Internal, "deactivating business: %v", err)
	}

	pb, err := businessToProto(deactivated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting business: %v", err)
	}
	return &avav1.DeactivateBusinessResponse{Business: pb}, nil
}

func businessToProto(b sqlcgen.Business) (*avav1.Business, error) {
	taxRate, err := moneypb.ToProto(b.DefaultTaxRate)
	if err != nil {
		return nil, err
	}
	return &avav1.Business{
		Id:                      b.ID,
		Name:                    b.Name,
		TaxId:                   b.TaxID,
		AddressLine1:            b.AddressLine1,
		AddressLine2:            b.AddressLine2,
		City:                    b.City,
		State:                   b.State,
		PostalCode:              b.PostalCode,
		Country:                 b.Country,
		Phone:                   b.Phone,
		Email:                   b.Email,
		DefaultPaymentTermsDays: b.DefaultPaymentTermsDays,
		DefaultTaxRate:          taxRate,
		InvoiceNumberPrefix:     derefOr(b.InvoiceNumberPrefix, ""),
		EstimateNumberPrefix:    derefOr(b.EstimateNumberPrefix, ""),
		Timezone:                derefOr(b.Timezone, ""),
		CurrencyCode:            derefOr(b.CurrencyCode, ""),
		IsActive:                b.IsActive,
		CreatedByUserId:         b.CreatedByUserID,
		CreatedAt:               timestampProto(b.CreatedAt),
		UpdatedAt:               timestampProto(b.UpdatedAt),
	}, nil
}

func timestampProto(t pgtype.Timestamp) *timestamppb.Timestamp {
	if !t.Valid {
		return nil
	}
	return timestamppb.New(t.Time)
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}
