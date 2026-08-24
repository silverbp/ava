// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

// Package moneypb converts between Postgres NUMERIC columns (as scanned by
// sqlc/pgx into pgtype.Numeric) and the wire-level ava.v1.Decimal message,
// keeping every conversion an exact decimal-string round trip.
package moneypb

import (
	"github.com/jackc/pgx/v5/pgtype"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
)

// ToProto converts a pgtype.Numeric into an *avav1.Decimal, returning nil
// for a SQL NULL.
func ToProto(n pgtype.Numeric) (*avav1.Decimal, error) {
	if !n.Valid {
		return nil, nil
	}
	v, err := n.Value()
	if err != nil {
		return nil, err
	}
	s, _ := v.(string)
	return &avav1.Decimal{Value: s}, nil
}

// ToNumeric converts an *avav1.Decimal into a pgtype.Numeric, returning a
// SQL NULL for a nil message.
func ToNumeric(d *avav1.Decimal) (pgtype.Numeric, error) {
	var n pgtype.Numeric
	if d == nil {
		return n, nil
	}
	if err := n.Scan(d.GetValue()); err != nil {
		return n, err
	}
	return n, nil
}

// ToNumericOrZero is ToNumeric, but a nil message converts to numeric 0
// rather than SQL NULL — for NOT NULL DEFAULT 0.00 columns like
// ledger_entry.debit_amount/credit_amount, where an explicit NULL would
// violate the column's NOT NULL constraint instead of falling back to its
// default.
func ToNumericOrZero(d *avav1.Decimal) (pgtype.Numeric, error) {
	if d == nil {
		var n pgtype.Numeric
		if err := n.Scan("0"); err != nil {
			return n, err
		}
		return n, nil
	}
	return ToNumeric(d)
}
