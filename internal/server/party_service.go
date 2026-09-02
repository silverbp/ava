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
	pb, err := contactToProto(ctx, s.store.Queries, c)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting contact: %v", err)
	}
	return &avav1.GetContactResponse{Contact: pb}, nil
}

func (s *contactService) ListContacts(ctx context.Context, req *avav1.ListContactsRequest) (*avav1.ListContactsResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListContacts(ctx, sqlcgen.ListContactsParams{
		BusinessID:      req.GetBusinessId(),
		IncludeInactive: req.GetIncludeInactive(),
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListContactsResponse{}
	for _, c := range rows {
		pb, err := contactToProto(ctx, s.store.Queries, c)
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
		BusinessID:          req.GetBusinessId(),
		ContactNumber:       req.GetContactNumber(),
		Name:                req.GetName(),
		Email:               req.Email,
		Phone:               req.Phone,
		PaymentTermsDays:    req.PaymentTermsDays,
		CreditLimit:         creditLimit,
		CreatedByUserID:     &u.ID,
		BillingAddressLine1: req.BillingAddressLine1,
		BillingAddressLine2: req.BillingAddressLine2,
		BillingCity:         req.BillingCity,
		BillingState:        req.BillingState,
		BillingPostalCode:   req.BillingPostalCode,
		BillingCountry:      req.BillingCountry,
	})
	if err != nil {
		return nil, translatePgError(err)
	}

	if req.GetIsCustomer() {
		if _, err := s.store.Queries.CreateCustomer(ctx, sqlcgen.CreateCustomerParams{
			ContactID:       created.ID,
			LedgerAccountID: req.CustomerLedgerAccountId,
		}); err != nil {
			return nil, translatePgError(err)
		}
	}
	if req.GetIsVendor() {
		if _, err := s.store.Queries.CreateVendor(ctx, sqlcgen.CreateVendorParams{
			ContactID:       created.ID,
			LedgerAccountID: req.VendorLedgerAccountId,
		}); err != nil {
			return nil, translatePgError(err)
		}
	}

	pb, err := contactToProto(ctx, s.store.Queries, created)
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
		ID:                  req.GetId(),
		Name:                req.Name,
		Email:               req.Email,
		Phone:               req.Phone,
		PaymentTermsDays:    req.PaymentTermsDays,
		CreditLimit:         creditLimit,
		BillingAddressLine1: req.BillingAddressLine1,
		BillingAddressLine2: req.BillingAddressLine2,
		BillingCity:         req.BillingCity,
		BillingState:        req.BillingState,
		BillingPostalCode:   req.BillingPostalCode,
		BillingCountry:      req.BillingCountry,
		ResourceVersion:     expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, "contact", req.GetId(), req.GetResourceVersion())
	}
	pb, err := contactToProto(ctx, s.store.Queries, updated)
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
	deactivated, err := s.store.Queries.DeactivateContact(ctx, sqlcgen.DeactivateContactParams{
		ID:              req.GetId(),
		ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, "contact", req.GetId(), req.GetResourceVersion())
	}
	pb, err := contactToProto(ctx, s.store.Queries, deactivated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting contact: %v", err)
	}
	return &avav1.DeactivateContactResponse{Contact: pb}, nil
}

func contactToProto(ctx context.Context, q *sqlcgen.Queries, c sqlcgen.Contact) (*avav1.Contact, error) {
	creditLimit, err := moneypb.ToProto(c.CreditLimit)
	if err != nil {
		return nil, err
	}

	var customer *avav1.Customer
	if cust, err := q.GetCustomerByContactID(ctx, c.ID); err == nil {
		customer = &avav1.Customer{
			Id:              cust.ID,
			ContactId:       cust.ContactID,
			LedgerAccountId: cust.LedgerAccountID,
			CreatedAt:       timestampProto(cust.CreatedAt),
			UpdatedAt:       timestampProto(cust.UpdatedAt),
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	var vendor *avav1.Vendor
	if ven, err := q.GetVendorByContactID(ctx, c.ID); err == nil {
		vendor = &avav1.Vendor{
			Id:              ven.ID,
			ContactId:       ven.ContactID,
			LedgerAccountId: ven.LedgerAccountID,
			CreatedAt:       timestampProto(ven.CreatedAt),
			UpdatedAt:       timestampProto(ven.UpdatedAt),
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	return &avav1.Contact{
		Id:                  c.ID,
		BusinessId:          c.BusinessID,
		ContactNumber:       c.ContactNumber,
		Name:                c.Name,
		Email:               c.Email,
		Phone:               c.Phone,
		PaymentTermsDays:    c.PaymentTermsDays,
		CreditLimit:         creditLimit,
		IsActive:            c.IsActive,
		CreatedByUserId:     c.CreatedByUserID,
		CreatedAt:           timestampProto(c.CreatedAt),
		UpdatedAt:           timestampProto(c.UpdatedAt),
		BillingAddressLine1: c.BillingAddressLine1,
		BillingAddressLine2: c.BillingAddressLine2,
		BillingCity:         c.BillingCity,
		BillingState:        c.BillingState,
		BillingPostalCode:   c.BillingPostalCode,
		BillingCountry:      c.BillingCountry,
		Customer:            customer,
		Vendor:              vendor,
		ResourceVersion:     c.ResourceVersion,
	}, nil
}

// --- ItemService ---

// item.item_type values - mirrors the CHECK constraint on item.item_type
// (migrations/00001_initial.up.sql). A string enum like invoice.invoice_type,
// not a proto enum, since more modes are expected and the schema's other
// enums already take that shape.
const (
	itemTypeService      = "SERVICE"       // labour/time, nothing physical
	itemTypeNonInventory = "NON_INVENTORY" // physical product, stock not tracked
	itemTypeInventory    = "INVENTORY"     // physical product, on-hand quantity tracked
)

var validItemTypes = map[string]bool{
	itemTypeService:      true,
	itemTypeNonInventory: true,
	itemTypeInventory:    true,
}

func itemTypeList() string {
	return itemTypeService + ", " + itemTypeNonInventory + ", " + itemTypeInventory
}

type itemService struct {
	avav1.UnimplementedItemServiceServer
	store *db.Store
}

func newItemService(store *db.Store) *itemService {
	return &itemService{store: store}
}

func (s *itemService) GetItem(ctx context.Context, req *avav1.GetItemRequest) (*avav1.GetItemResponse, error) {
	item, err := s.store.Queries.GetItem(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "item %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, item.BusinessID, "VIEWER"); err != nil {
		return nil, err
	}
	pb, err := itemToProto(item)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting item: %v", err)
	}
	return &avav1.GetItemResponse{Item: pb}, nil
}

func (s *itemService) ListItems(ctx context.Context, req *avav1.ListItemsRequest) (*avav1.ListItemsResponse, error) {
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "VIEWER"); err != nil {
		return nil, err
	}
	rows, err := s.store.Queries.ListItems(ctx, sqlcgen.ListItemsParams{
		BusinessID:      req.GetBusinessId(),
		IncludeInactive: req.GetIncludeInactive(),
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	resp := &avav1.ListItemsResponse{}
	for _, item := range rows {
		pb, err := itemToProto(item)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "converting item: %v", err)
		}
		resp.Items = append(resp.Items, pb)
	}
	return resp, nil
}

func (s *itemService) CreateItem(ctx context.Context, req *avav1.CreateItemRequest) (*avav1.CreateItemResponse, error) {
	u, ok := auth.UserFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "no authenticated user")
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, req.GetBusinessId(), "MEMBER"); err != nil {
		return nil, err
	}
	if req.GetItemCode() == "" || req.GetName() == "" || req.GetRetailPrice() == nil {
		return nil, status.Error(codes.InvalidArgument, "item_code, name, and retail_price are required")
	}
	itemType := itemTypeService
	if req.ItemType != nil {
		itemType = req.GetItemType()
		if !validItemTypes[itemType] {
			return nil, status.Errorf(codes.InvalidArgument, "invalid item_type %q (want one of %s)", itemType, itemTypeList())
		}
	}

	costPrice, err := moneypb.ToNumeric(req.CostPrice)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid cost_price: %v", err)
	}
	retailPrice, err := moneypb.ToNumeric(req.GetRetailPrice())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid retail_price: %v", err)
	}

	created, err := s.store.Queries.CreateItem(ctx, sqlcgen.CreateItemParams{
		BusinessID:             req.GetBusinessId(),
		ItemCode:               req.GetItemCode(),
		Name:                   req.GetName(),
		Description:            req.Description,
		UnitOfMeasure:          req.UnitOfMeasure,
		CostPrice:              costPrice,
		RetailPrice:            retailPrice,
		IsTaxable:              req.GetIsTaxable(),
		DefaultTaxRateID:       req.DefaultTaxRateId,
		DefaultLedgerAccountID: req.DefaultLedgerAccountId,
		CreatedByUserID:        &u.ID,
	})
	if err != nil {
		return nil, translatePgError(err)
	}
	pb, err := itemToProto(created)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting item: %v", err)
	}
	return &avav1.CreateItemResponse{Item: pb}, nil
}

func (s *itemService) UpdateItem(ctx context.Context, req *avav1.UpdateItemRequest) (*avav1.UpdateItemResponse, error) {
	existing, err := s.store.Queries.GetItem(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "item %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}

	if req.ItemType != nil && !validItemTypes[req.GetItemType()] {
		return nil, status.Errorf(codes.InvalidArgument, "invalid item_type %q (want one of %s)", req.GetItemType(), itemTypeList())
	}
	retailPrice, err := moneypb.ToNumeric(req.RetailPrice)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid retail_price: %v", err)
	}
	costPrice, err := moneypb.ToNumeric(req.CostPrice)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid cost_price: %v", err)
	}

	updated, err := s.store.Queries.UpdateItem(ctx, sqlcgen.UpdateItemParams{
		ID:                     req.GetId(),
		ItemType:               req.ItemType,
		Name:                   req.Name,
		Description:            req.Description,
		RetailPrice:            retailPrice,
		CostPrice:              costPrice,
		IsTaxable:              req.IsTaxable,
		DefaultTaxRateID:       req.DefaultTaxRateId,
		DefaultLedgerAccountID: req.DefaultLedgerAccountId,
		ResourceVersion:        expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, "item", req.GetId(), req.GetResourceVersion())
	}
	pb, err := itemToProto(updated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting item: %v", err)
	}
	return &avav1.UpdateItemResponse{Item: pb}, nil
}

func (s *itemService) DeactivateItem(ctx context.Context, req *avav1.DeactivateItemRequest) (*avav1.DeactivateItemResponse, error) {
	existing, err := s.store.Queries.GetItem(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "item %d not found", req.GetId())
		}
		return nil, translatePgError(err)
	}
	if err := auth.RequireBusinessRole(ctx, s.store.Queries, existing.BusinessID, "MEMBER"); err != nil {
		return nil, err
	}
	deactivated, err := s.store.Queries.DeactivateItem(ctx, sqlcgen.DeactivateItemParams{
		ID:              req.GetId(),
		ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, "item", req.GetId(), req.GetResourceVersion())
	}
	pb, err := itemToProto(deactivated)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "converting item: %v", err)
	}
	return &avav1.DeactivateItemResponse{Item: pb}, nil
}

func itemToProto(item sqlcgen.Item) (*avav1.Item, error) {
	costPrice, err := moneypb.ToProto(item.CostPrice)
	if err != nil {
		return nil, err
	}
	retailPrice, err := moneypb.ToProto(item.RetailPrice)
	if err != nil {
		return nil, err
	}
	return &avav1.Item{
		Id:                     item.ID,
		BusinessId:             item.BusinessID,
		ItemCode:               item.ItemCode,
		Name:                   item.Name,
		Description:            item.Description,
		UnitOfMeasure:          derefOr(item.UnitOfMeasure, "EACH"),
		CostPrice:              costPrice,
		RetailPrice:            retailPrice,
		IsTaxable:              item.IsTaxable,
		DefaultTaxRateId:       item.DefaultTaxRateID,
		DefaultLedgerAccountId: item.DefaultLedgerAccountID,
		IsActive:               item.IsActive,
		CreatedByUserId:        item.CreatedByUserID,
		CreatedAt:              timestampProto(item.CreatedAt),
		UpdatedAt:              timestampProto(item.UpdatedAt),
		ResourceVersion:        item.ResourceVersion,
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
		ID:              req.GetId(),
		Name:            req.Name,
		Rate:            rate,
		ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, "tax rate", req.GetId(), req.GetResourceVersion())
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
	deactivated, err := s.store.Queries.DeactivateTaxRate(ctx, sqlcgen.DeactivateTaxRateParams{
		ID:              req.GetId(),
		ResourceVersion: expectedResourceVersion(req.GetResourceVersion()),
	})
	if err != nil {
		return nil, translateUpdateError(err, "tax rate", req.GetId(), req.GetResourceVersion())
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
		ResourceVersion:       tr.ResourceVersion,
	}, nil
}
