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
	"github.com/silverbp/ava/internal/db"
	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/moneypb"
)

// --- ContactService ---

type contactService struct {
	avav1.UnimplementedContactServiceServer
	store *db.Store
}

func newContactService(store *db.Store) *contactService {
	return &contactService{store: store}
}

func (s *contactService) GetContact(ctx context.Context, req *avav1.GetContactRequest) (*avav1.GetContactResponse, error) {
	c, err := s.store.Queries.GetContact(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "contact %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, c.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	pb, err := contactToProto(c)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting contact: %v", err)
	}
	return &avav1.GetContactResponse{Contact: pb}, nil
}

func (s *contactService) ListContacts(ctx context.Context, req *avav1.ListContactsRequest) (*avav1.ListContactsResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListContacts(ctx, req.GetBusinessId())
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListContactsResponse{}
	for _, c := range rows {
		pb, err := contactToProto(c)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting contact: %v", err)
		}
		resp.Contacts = append(resp.Contacts, pb)
	}
	return resp, nil
}

func (s *contactService) CreateContact(ctx context.Context, req *avav1.CreateContactRequest) (*avav1.CreateContactResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if req.GetContactNumber() == "" || req.GetName() == "" {
		return nil, status.Error(codes.InvalidArgument, "contact_number and name are required")
	}

	creditLimit, err := moneypb.ToNumeric(req.CreditLimit)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid credit_limit: %v", err)
	}

	created, err := s.store.Queries.CreateContact(ctx, sqlcgen.CreateContactParams{
		BusinessID:       req.GetBusinessId(),
		LedgerAccountID:  req.LedgerAccountId,
		ContactNumber:    req.GetContactNumber(),
		IsCustomer:       req.GetIsCustomer(),
		IsVendor:         req.GetIsVendor(),
		Name:             req.GetName(),
		Email:            req.Email,
		Phone:            req.Phone,
		PaymentTermsDays: req.PaymentTermsDays,
		CreditLimit:      creditLimit,
		CreatedByUserID:  &u.ID,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := contactToProto(created)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting contact: %v", err)
	}
	return &avav1.CreateContactResponse{Contact: pb}, nil
}

func (s *contactService) UpdateContact(ctx context.Context, req *avav1.UpdateContactRequest) (*avav1.UpdateContactResponse, error) {
	existing, err := s.store.Queries.GetContact(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "contact %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}

	creditLimit, err := moneypb.ToNumeric(req.CreditLimit)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid credit_limit: %v", err)
	}

	updated, err := s.store.Queries.UpdateContact(ctx, sqlcgen.UpdateContactParams{
		ID:               req.GetId(),
		Name:             req.Name,
		Email:            req.Email,
		Phone:            req.Phone,
		LedgerAccountID:  req.LedgerAccountId,
		PaymentTermsDays: req.PaymentTermsDays,
		CreditLimit:      creditLimit,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := contactToProto(updated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting contact: %v", err)
	}
	return &avav1.UpdateContactResponse{Contact: pb}, nil
}

func (s *contactService) DeactivateContact(ctx context.Context, req *avav1.DeactivateContactRequest) (*avav1.DeactivateContactResponse, error) {
	existing, err := s.store.Queries.GetContact(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "contact %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}
	deactivated, err := s.store.Queries.DeactivateContact(ctx, req.GetId())
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := contactToProto(deactivated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting contact: %v", err)
	}
	return &avav1.DeactivateContactResponse{Contact: pb}, nil
}

func contactToProto(c sqlcgen.Contact) (*avav1.Contact, error) {
	creditLimit, err := moneypb.ToProto(c.CreditLimit)
	if err != nil {
		return nil, err
	}
	return &avav1.Contact{
		Id:               c.ID,
		BusinessId:       c.BusinessID,
		LedgerAccountId:  c.LedgerAccountID,
		ContactNumber:    c.ContactNumber,
		IsCustomer:       c.IsCustomer,
		IsVendor:         c.IsVendor,
		Name:             c.Name,
		Email:            c.Email,
		Phone:            c.Phone,
		PaymentTermsDays: c.PaymentTermsDays,
		CreditLimit:      creditLimit,
		IsActive:         c.IsActive,
		CreatedByUserId:  c.CreatedByUserID,
		CreatedAt:        timestampProto(c.CreatedAt),
		UpdatedAt:        timestampProto(c.UpdatedAt),
	}, nil
}

// --- ServiceCatalogService ---

type serviceCatalogService struct {
	avav1.UnimplementedServiceCatalogServiceServer
	store *db.Store
}

func newServiceCatalogService(store *db.Store) *serviceCatalogService {
	return &serviceCatalogService{store: store}
}

func (s *serviceCatalogService) GetService(ctx context.Context, req *avav1.GetServiceRequest) (*avav1.GetServiceResponse, error) {
	svc, err := s.store.Queries.GetService(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "service %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, svc.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	pb, err := serviceToProto(svc)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting service: %v", err)
	}
	return &avav1.GetServiceResponse{Service: pb}, nil
}

func (s *serviceCatalogService) ListServices(ctx context.Context, req *avav1.ListServicesRequest) (*avav1.ListServicesResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListServices(ctx, req.GetBusinessId())
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListServicesResponse{}
	for _, svc := range rows {
		pb, err := serviceToProto(svc)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting service: %v", err)
		}
		resp.Services = append(resp.Services, pb)
	}
	return resp, nil
}

func (s *serviceCatalogService) CreateService(ctx context.Context, req *avav1.CreateServiceRequest) (*avav1.CreateServiceResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if req.GetServiceCode() == "" || req.GetName() == "" || req.GetRetailPrice() == nil {
		return nil, status.Error(codes.InvalidArgument, "service_code, name, and retail_price are required")
	}

	costPrice, err := moneypb.ToNumeric(req.CostPrice)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid cost_price: %v", err)
	}
	retailPrice, err := moneypb.ToNumeric(req.GetRetailPrice())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid retail_price: %v", err)
	}
	defaultTaxRate, err := moneypb.ToNumeric(req.DefaultTaxRate)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid default_tax_rate: %v", err)
	}

	created, err := s.store.Queries.CreateService(ctx, sqlcgen.CreateServiceParams{
		BusinessID:      req.GetBusinessId(),
		ServiceCode:     req.GetServiceCode(),
		Name:            req.GetName(),
		Description:     req.Description,
		UnitOfMeasure:   req.UnitOfMeasure,
		CostPrice:       costPrice,
		RetailPrice:     retailPrice,
		IsTaxable:       req.GetIsTaxable(),
		DefaultTaxRate:  defaultTaxRate,
		CreatedByUserID: &u.ID,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := serviceToProto(created)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting service: %v", err)
	}
	return &avav1.CreateServiceResponse{Service: pb}, nil
}

func (s *serviceCatalogService) UpdateService(ctx context.Context, req *avav1.UpdateServiceRequest) (*avav1.UpdateServiceResponse, error) {
	existing, err := s.store.Queries.GetService(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "service %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}

	retailPrice, err := moneypb.ToNumeric(req.RetailPrice)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid retail_price: %v", err)
	}
	costPrice, err := moneypb.ToNumeric(req.CostPrice)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid cost_price: %v", err)
	}
	defaultTaxRate, err := moneypb.ToNumeric(req.DefaultTaxRate)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid default_tax_rate: %v", err)
	}

	updated, err := s.store.Queries.UpdateService(ctx, sqlcgen.UpdateServiceParams{
		ID:             req.GetId(),
		Name:           req.Name,
		Description:    req.Description,
		RetailPrice:    retailPrice,
		CostPrice:      costPrice,
		IsTaxable:      req.IsTaxable,
		DefaultTaxRate: defaultTaxRate,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := serviceToProto(updated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting service: %v", err)
	}
	return &avav1.UpdateServiceResponse{Service: pb}, nil
}

func (s *serviceCatalogService) DeactivateService(ctx context.Context, req *avav1.DeactivateServiceRequest) (*avav1.DeactivateServiceResponse, error) {
	existing, err := s.store.Queries.GetService(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "service %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}
	deactivated, err := s.store.Queries.DeactivateService(ctx, req.GetId())
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := serviceToProto(deactivated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting service: %v", err)
	}
	return &avav1.DeactivateServiceResponse{Service: pb}, nil
}

func serviceToProto(svc sqlcgen.Service) (*avav1.Service, error) {
	costPrice, err := moneypb.ToProto(svc.CostPrice)
	if err != nil {
		return nil, err
	}
	retailPrice, err := moneypb.ToProto(svc.RetailPrice)
	if err != nil {
		return nil, err
	}
	defaultTaxRate, err := moneypb.ToProto(svc.DefaultTaxRate)
	if err != nil {
		return nil, err
	}
	return &avav1.Service{
		Id:              svc.ID,
		BusinessId:      svc.BusinessID,
		ServiceCode:     svc.ServiceCode,
		Name:            svc.Name,
		Description:     svc.Description,
		UnitOfMeasure:   derefOr(svc.UnitOfMeasure, "EACH"),
		CostPrice:       costPrice,
		RetailPrice:     retailPrice,
		IsTaxable:       svc.IsTaxable,
		DefaultTaxRate:  defaultTaxRate,
		IsActive:        svc.IsActive,
		CreatedByUserId: svc.CreatedByUserID,
		CreatedAt:       timestampProto(svc.CreatedAt),
		UpdatedAt:       timestampProto(svc.UpdatedAt),
	}, nil
}

// --- TaxRateService ---

type taxRateService struct {
	avav1.UnimplementedTaxRateServiceServer
	store *db.Store
}

func newTaxRateService(store *db.Store) *taxRateService {
	return &taxRateService{store: store}
}

func (s *taxRateService) GetTaxRate(ctx context.Context, req *avav1.GetTaxRateRequest) (*avav1.GetTaxRateResponse, error) {
	tr, err := s.store.Queries.GetTaxRate(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "tax rate %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, tr.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	pb, err := taxRateToProto(tr)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting tax rate: %v", err)
	}
	return &avav1.GetTaxRateResponse{TaxRate: pb}, nil
}

func (s *taxRateService) ListTaxRates(ctx context.Context, req *avav1.ListTaxRatesRequest) (*avav1.ListTaxRatesResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListTaxRates(ctx, req.GetBusinessId())
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListTaxRatesResponse{}
	for _, tr := range rows {
		pb, err := taxRateToProto(tr)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting tax rate: %v", err)
		}
		resp.TaxRates = append(resp.TaxRates, pb)
	}
	return resp, nil
}

func (s *taxRateService) CreateTaxRate(ctx context.Context, req *avav1.CreateTaxRateRequest) (*avav1.CreateTaxRateResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "ADMIN"); err != nil {
		return nil, err
	}
	if req.GetName() == "" || req.GetRate() == nil || req.GetTaxLiabilityAccountId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "name, rate, and tax_liability_account_id are required")
	}

	rate, err := moneypb.ToNumeric(req.GetRate())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid rate: %v", err)
	}

	created, err := s.store.Queries.CreateTaxRate(ctx, sqlcgen.CreateTaxRateParams{
		BusinessID:            req.GetBusinessId(),
		Name:                  req.GetName(),
		Rate:                  rate,
		TaxLiabilityAccountID: req.GetTaxLiabilityAccountId(),
		CreatedByUserID:       &u.ID,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := taxRateToProto(created)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting tax rate: %v", err)
	}
	return &avav1.CreateTaxRateResponse{TaxRate: pb}, nil
}

func (s *taxRateService) UpdateTaxRate(ctx context.Context, req *avav1.UpdateTaxRateRequest) (*avav1.UpdateTaxRateResponse, error) {
	existing, err := s.store.Queries.GetTaxRate(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "tax rate %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "ADMIN"); err != nil {
		return nil, err
	}

	rate, err := moneypb.ToNumeric(req.Rate)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid rate: %v", err)
	}

	updated, err := s.store.Queries.UpdateTaxRate(ctx, sqlcgen.UpdateTaxRateParams{
		ID:   req.GetId(),
		Name: req.Name,
		Rate: rate,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := taxRateToProto(updated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting tax rate: %v", err)
	}
	return &avav1.UpdateTaxRateResponse{TaxRate: pb}, nil
}

func (s *taxRateService) DeactivateTaxRate(ctx context.Context, req *avav1.DeactivateTaxRateRequest) (*avav1.DeactivateTaxRateResponse, error) {
	existing, err := s.store.Queries.GetTaxRate(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "tax rate %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "ADMIN"); err != nil {
		return nil, err
	}
	deactivated, err := s.store.Queries.DeactivateTaxRate(ctx, req.GetId())
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := taxRateToProto(deactivated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting tax rate: %v", err)
	}
	return &avav1.DeactivateTaxRateResponse{TaxRate: pb}, nil
}

func taxRateToProto(tr sqlcgen.TaxRate) (*avav1.TaxRate, error) {
	rate, err := moneypb.ToProto(tr.Rate)
	if err != nil {
		return nil, err
	}
	return &avav1.TaxRate{
		Id:                    tr.ID,
		BusinessId:            tr.BusinessID,
		Name:                  tr.Name,
		Rate:                  rate,
		TaxLiabilityAccountId: tr.TaxLiabilityAccountID,
		IsActive:              tr.IsActive,
		CreatedByUserId:       tr.CreatedByUserID,
		CreatedAt:             timestampProto(tr.CreatedAt),
		UpdatedAt:             timestampProto(tr.UpdatedAt),
	}, nil
}
