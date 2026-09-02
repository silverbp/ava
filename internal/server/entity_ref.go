// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/silverbp/ava/internal/db/sqlcgen"
)

// validEntityTypes is the allowlist entity_context/attachment validate
// entity_type against — the schema deliberately carries no FK for these
// polymorphic references (docs/schema.md), so this is the only check
// standing between a typo/nonsense entity_type and a silently-orphaned row.
var validEntityTypes = map[string]bool{
	"business":           true,
	"contact":            true,
	"ledger_account":     true,
	"ledger_transaction": true,
	"invoice":            true,
	"payment":            true,
	"estimate":           true,
	"item":               true,
	"tax_rate":           true,
	"bank_statement":     true,
	"period_close":       true,
}

// validateEntityRef confirms entity_type is a known target, entity_id
// exists, and it belongs to businessID — tenant isolation the schema can't
// enforce here, since entity_id has no FK to lean on.
func validateEntityRef(ctx context.Context, q *sqlcgen.Queries, businessID int64, entityType string, entityID int64) error {
	if !validEntityTypes[entityType] {
		return status.Errorf(codes.InvalidArgument, "unknown entity_type %q", entityType)
	}

	var (
		ownerBusinessID int64
		err             error
	)
	switch entityType {
	case "business":
		var b sqlcgen.Business
		b, err = q.GetBusiness(ctx, entityID)
		ownerBusinessID = b.ID
	case "contact":
		var c sqlcgen.Contact
		c, err = q.GetContact(ctx, entityID)
		ownerBusinessID = c.BusinessID
	case "ledger_account":
		var a sqlcgen.LedgerAccount
		a, err = q.GetLedgerAccount(ctx, int32(entityID))
		ownerBusinessID = a.BusinessID
	case "ledger_transaction":
		var t sqlcgen.LedgerTransaction
		t, err = q.GetLedgerTransaction(ctx, entityID)
		ownerBusinessID = t.BusinessID
	case "invoice":
		var i sqlcgen.Invoice
		i, err = q.GetInvoice(ctx, entityID)
		ownerBusinessID = i.BusinessID
	case "payment":
		var p sqlcgen.Payment
		p, err = q.GetPayment(ctx, entityID)
		ownerBusinessID = p.BusinessID
	case "estimate":
		var e sqlcgen.Estimate
		e, err = q.GetEstimate(ctx, entityID)
		ownerBusinessID = e.BusinessID
	case "item":
		var s sqlcgen.Item
		s, err = q.GetItem(ctx, entityID)
		ownerBusinessID = s.BusinessID
	case "tax_rate":
		var t sqlcgen.TaxRate
		t, err = q.GetTaxRate(ctx, entityID)
		ownerBusinessID = t.BusinessID
	case "bank_statement":
		var b sqlcgen.BankStatement
		b, err = q.GetBankStatement(ctx, entityID)
		ownerBusinessID = b.BusinessID
	case "period_close":
		var p sqlcgen.PeriodClose
		p, err = q.GetPeriodClose(ctx, entityID)
		ownerBusinessID = p.BusinessID
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return status.Errorf(codes.NotFound, "%s %d not found", entityType, entityID)
		}
		return translatePgError(err)
	}

	if ownerBusinessID != businessID {
		return status.Errorf(codes.InvalidArgument, "%s %d does not belong to business %d", entityType, entityID, businessID)
	}
	return nil
}
