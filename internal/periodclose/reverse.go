// Copyright (c) 2025 Silver Blueprints LLC
// SPDX-License-Identifier: MIT

package periodclose

import (
	"context"
	"fmt"

	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/ledgerpost"
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
		// date is the original close's period_end, kept the same for the
		// reversal — it landed while that date was still unlocked, and the
		// reversal runs immediately after re-unlocking it (reversed_at was
		// set above, before this loop).
		description := fmt.Sprintf("Reversal of period-close transaction %d", pce.LedgerTransactionID)
		if _, err := ledgerpost.ReverseTransaction(ctx, q, pc.BusinessID, pce.LedgerTransactionID, pc.PeriodEnd, description, createdByUserID); err != nil {
			return nil, err
		}
	}

	return &reversed, nil
}
