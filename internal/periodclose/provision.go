// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

// Package periodclose implements the period-close algorithm documented in
// docs/architecture.md, as plain Go over *sqlcgen.Queries — no gRPC
// dependency, so it's directly unit-testable against a real Postgres and
// callable both from the API and (eventually) from business-creation flow.
package periodclose

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/silverbp/ava/internal/db/sqlcgen"
)

const (
	// EquityAccountTypeID is ledger_account_type.id for EQUITY, seeded by
	// the initial migration.
	EquityAccountTypeID = 3

	// capitalAndReservesCategoryID is balance_sheet_category.id for
	// "Capital & Reserves", seeded by the initial migration - where a swept
	// P&L balance belongs on the balance sheet report
	// (internal/reporting.BalanceSheet).
	capitalAndReservesCategoryID = 4

	// QuickBooks-style numeric account codes (see docs/schema.md's chart-of-accounts numbering
	// convention: 3000s = Equity), not descriptive strings like the schema used to use.
	IncomeSummaryCode    = "3910"
	RetainedEarningsCode = "3900"
)

// ProvisionSystemAccounts ensures a business has its Income Summary and
// Retained Earnings ledger_account rows (is_system = true, EQUITY type),
// creating whichever are missing. Idempotent — safe to call on every
// business creation and again before every close.
func ProvisionSystemAccounts(ctx context.Context, q *sqlcgen.Queries, businessID int64, createdByUserID *int64) (incomeSummary, retainedEarnings sqlcgen.LedgerAccount, err error) {
	incomeSummary, err = getOrCreateSystemAccount(ctx, q, businessID, IncomeSummaryCode, "Income Summary", createdByUserID)
	if err != nil {
		return sqlcgen.LedgerAccount{}, sqlcgen.LedgerAccount{}, fmt.Errorf("provisioning %s: %w", IncomeSummaryCode, err)
	}

	retainedEarnings, err = getOrCreateSystemAccount(ctx, q, businessID, RetainedEarningsCode, "Retained Earnings", createdByUserID)
	if err != nil {
		return sqlcgen.LedgerAccount{}, sqlcgen.LedgerAccount{}, fmt.Errorf("provisioning %s: %w", RetainedEarningsCode, err)
	}

	return incomeSummary, retainedEarnings, nil
}

func getOrCreateSystemAccount(ctx context.Context, q *sqlcgen.Queries, businessID int64, code, name string, createdByUserID *int64) (sqlcgen.LedgerAccount, error) {
	existing, err := q.GetLedgerAccountByCode(ctx, sqlcgen.GetLedgerAccountByCodeParams{
		BusinessID: businessID,
		Code:       code,
	})
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.LedgerAccount{}, err
	}

	categoryID := int32(capitalAndReservesCategoryID)
	return q.CreateLedgerAccount(ctx, sqlcgen.CreateLedgerAccountParams{
		BusinessID:             businessID,
		AccountTypeID:          EquityAccountTypeID,
		Code:                   code,
		Name:                   name,
		IsSystem:               true,
		BalanceSheetCategoryID: &categoryID,
		CreatedByUserID:        createdByUserID,
	})
}
