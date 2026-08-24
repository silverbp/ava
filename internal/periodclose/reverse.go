// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package periodclose

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/silverbp/ava/internal/db/sqlcgen"
)

// Reverse undoes a period close: marks it reversed_at, then posts a genuine
// reversing ledger_transaction (debits/credits swapped) for every
// transaction the original close generated — never editing or deleting the
// original postings, consistent with the schema's soft-delete-over-mutation
// convention elsewhere (docs/architecture.md#period-close, "Reversal /
// reopen"). Must run inside a single transaction — q should be bound via
// store.ExecTx.
//
// reversed_at is set FIRST, not last: the lock trigger's MAX(period_end)
// check only considers unreversed closes, so marking this one reversed
// before posting the reversing entries is what allows those entries —
// dated on or before what was, a moment ago, the lock boundary — to post at
// all.
func Reverse(ctx context.Context, q *sqlcgen.Queries, periodCloseID int64, createdByUserID *int64) (*sqlcgen.PeriodClose, error) {
	pc, err := q.GetPeriodClose(ctx, periodCloseID)
	if err != nil {
		return nil, err
	}
	if pc.ReversedAt.Valid {
		return nil, fmt.Errorf("period close %d is already reversed", periodCloseID)
	}

	entries, err := q.ListPeriodCloseEntries(ctx, periodCloseID)
	if err != nil {
		return nil, err
	}

	reversed, err := q.ReversePeriodClose(ctx, periodCloseID)
	if err != nil {
		return nil, err
	}

	for _, pce := range entries {
		if err := reverseTransaction(ctx, q, pc.BusinessID, pce.LedgerTransactionID, pc.PeriodEnd, createdByUserID); err != nil {
			return nil, err
		}
	}

	return &reversed, nil
}

// reverseTransaction posts a new ledger_transaction dated `date` (the
// original close's period_end, kept the same for the reversal — it landed
// while that date was still unlocked, and the reversal runs immediately
// after re-unlocking it) with one entry per originalTxnID entry, debit and
// credit swapped.
func reverseTransaction(ctx context.Context, q *sqlcgen.Queries, businessID, originalTxnID int64, date pgtype.Date, createdByUserID *int64) error {
	originalEntries, err := q.ListLedgerEntriesByTransaction(ctx, originalTxnID)
	if err != nil {
		return err
	}

	description := fmt.Sprintf("Reversal of period-close transaction %d", originalTxnID)
	txn, err := q.CreateLedgerTransaction(ctx, sqlcgen.CreateLedgerTransactionParams{
		BusinessID:      businessID,
		TransactionDate: date,
		Description:     &description,
		CreatedByUserID: createdByUserID,
	})
	if err != nil {
		return err
	}

	for _, e := range originalEntries {
		if err := createEntry(ctx, q, businessID, txn.ID, e.AccountID, e.CreditAmount, e.DebitAmount); err != nil {
			return err
		}
	}
	return nil
}
