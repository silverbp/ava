// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
)

func TestDebitCreditFor(t *testing.T) {
	cases := []struct {
		name         string
		amount       string
		naturalDebit bool
		wantDebit    string
		wantCredit   string
	}{
		{"positive natural-debit stays a debit", "1000.00", true, "1000.00", "0"},
		{"positive natural-credit stays a credit", "1000.00", false, "0", "1000.00"},
		{"negative natural-debit flips to a credit", "-100.00", true, "0", "100.00"},
		{"negative natural-credit flips to a debit", "-100.00", false, "100.00", "0"},
		{"zero stays zero either way", "0", true, "0", "0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			amount := decimal.RequireFromString(c.amount)
			debit, credit := debitCreditFor(amount, c.naturalDebit)
			if !debit.Equal(decimal.RequireFromString(c.wantDebit)) {
				t.Errorf("debit = %s, want %s", debit, c.wantDebit)
			}
			if !credit.Equal(decimal.RequireFromString(c.wantCredit)) {
				t.Errorf("credit = %s, want %s", credit, c.wantCredit)
			}
			// Exactly one side must ever be strictly positive, matching
			// ledger_entry_debit_or_credit — except the zero case, which
			// callers must skip rather than insert.
			if !amount.IsZero() {
				if debit.IsPositive() == credit.IsPositive() {
					t.Errorf("expected exactly one side positive, got debit=%s credit=%s", debit, credit)
				}
			}
		})
	}
}

func decimalPB(v string) *avav1.Decimal { return &avav1.Decimal{Value: v} }

func TestComputeLines_RejectsNegativeTotal(t *testing.T) {
	// Both lines are non-taxable, so computeLines never touches q — nil is safe,
	// same pattern as TestLookupLineItem_ZeroItemIDIsInvalidArgument.
	inputs := []lineInput{
		{Quantity: decimalPB("1"), UnitPrice: decimalPB("100.00")},
		{Quantity: decimalPB("1"), UnitPrice: decimalPB("-200.00")},
	}
	_, _, _, _, err := computeLines(context.Background(), nil, 1, inputs)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v (%v), want InvalidArgument", status.Code(err), err)
	}
}

func TestComputeLines_AllowsNegativeLineWithinPositiveTotal(t *testing.T) {
	inputs := []lineInput{
		{Quantity: decimalPB("1"), UnitPrice: decimalPB("1000.00")},
		{Quantity: decimalPB("1"), UnitPrice: decimalPB("-100.00")},
	}
	_, subtotal, _, total, err := computeLines(context.Background(), nil, 1, inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !subtotal.Equal(decimal.RequireFromString("900.00")) {
		t.Errorf("subtotal = %s, want 900.00", subtotal)
	}
	if !total.Equal(decimal.RequireFromString("900.00")) {
		t.Errorf("total = %s, want 900.00", total)
	}
}
