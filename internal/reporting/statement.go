// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package reporting

import (
	"context"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/ledgermath"
)

type StatementInvoiceLine struct {
	InvoiceID     int64
	InvoiceNumber string
	InvoiceDate   time.Time
	DueDate       time.Time
	TotalAmount   decimal.Decimal
	BalanceDue    decimal.Decimal
	Status        string
}

type StatementPaymentLine struct {
	PaymentID     int64
	PaymentNumber string
	PaymentDate   time.Time
	Amount        decimal.Decimal
}

// StatementActivityLine is one row of the combined, date-ordered
// invoice+payment feed: an invoice increases the balance owed (Debit), a
// payment decreases it (Credit) — the same convention as a customer's own
// AR sub-ledger.
type StatementActivityLine struct {
	Date           time.Time
	Description    string
	Debit          decimal.Decimal
	Credit         decimal.Decimal
	RunningBalance decimal.Decimal
}

type AgingBucket struct {
	Label  string // "Current", "1-30", "31-60", "61-90", "90+"
	Amount decimal.Decimal
}

type CustomerStatementResult struct {
	ContactID     int64
	ContactName   string
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Invoices      []StatementInvoiceLine
	Payments      []StatementPaymentLine
	Activity      []StatementActivityLine
	EndingBalance decimal.Decimal
	AgingBuckets  []AgingBucket
}

// CustomerStatement reports invoice/payment activity for one contact over
// [start, end], a running balance across that activity, and an AR aging
// snapshot (as of end) over every still-open invoice through end —
// independent of the [start, end] window, since aging is conventionally a
// point-in-time snapshot, not scoped to an activity range.
func CustomerStatement(ctx context.Context, q *sqlcgen.Queries, contactID int64, start, end time.Time) (*CustomerStatementResult, error) {
	contact, err := q.GetContact(ctx, contactID)
	if err != nil {
		return nil, err
	}

	invoiceRows, err := q.ListInvoicesForContact(ctx, sqlcgen.ListInvoicesForContactParams{
		ContactID:   contactID,
		PeriodStart: ledgermath.PgDate(start),
		PeriodEnd:   ledgermath.PgDate(end),
	})
	if err != nil {
		return nil, err
	}
	paymentRows, err := q.ListPaymentsForContact(ctx, sqlcgen.ListPaymentsForContactParams{
		ContactID:   contactID,
		PeriodStart: ledgermath.PgDate(start),
		PeriodEnd:   ledgermath.PgDate(end),
	})
	if err != nil {
		return nil, err
	}

	result := &CustomerStatementResult{
		ContactID:   contactID,
		ContactName: contact.Name,
		PeriodStart: start,
		PeriodEnd:   end,
	}

	type activityRaw struct {
		date        time.Time
		description string
		debit       decimal.Decimal
	}
	var raw []activityRaw

	for _, inv := range invoiceRows {
		total, err := ledgermath.NumericToDecimal(inv.TotalAmount)
		if err != nil {
			return nil, err
		}
		balanceDue, err := ledgermath.NumericToDecimal(inv.BalanceDue)
		if err != nil {
			return nil, err
		}
		result.Invoices = append(result.Invoices, StatementInvoiceLine{
			InvoiceID:     inv.ID,
			InvoiceNumber: inv.InvoiceNumber,
			InvoiceDate:   inv.InvoiceDate.Time,
			DueDate:       inv.DueDate.Time,
			TotalAmount:   total,
			BalanceDue:    balanceDue,
			Status:        inv.Status,
		})
		raw = append(raw, activityRaw{date: inv.InvoiceDate.Time, description: "Invoice " + inv.InvoiceNumber, debit: total})
	}

	for _, p := range paymentRows {
		amount, err := ledgermath.NumericToDecimal(p.Amount)
		if err != nil {
			return nil, err
		}
		result.Payments = append(result.Payments, StatementPaymentLine{
			PaymentID:     p.ID,
			PaymentNumber: p.PaymentNumber,
			PaymentDate:   p.PaymentDate.Time,
			Amount:        amount,
		})
		raw = append(raw, activityRaw{date: p.PaymentDate.Time, description: "Payment " + p.PaymentNumber, debit: amount.Neg()})
	}

	sort.SliceStable(raw, func(i, j int) bool { return raw[i].date.Before(raw[j].date) })

	running := decimal.Zero
	for _, r := range raw {
		running = running.Add(r.debit)
		line := StatementActivityLine{Date: r.date, Description: r.description, RunningBalance: running}
		if r.debit.IsPositive() {
			line.Debit = r.debit
			line.Credit = decimal.Zero
		} else {
			line.Debit = decimal.Zero
			line.Credit = r.debit.Neg()
		}
		result.Activity = append(result.Activity, line)
	}
	result.EndingBalance = running

	agingRows, err := q.ListInvoicesForContact(ctx, sqlcgen.ListInvoicesForContactParams{
		ContactID:   contactID,
		PeriodStart: ledgermath.PgDate(ledgermath.InceptionDate),
		PeriodEnd:   ledgermath.PgDate(end),
	})
	if err != nil {
		return nil, err
	}
	result.AgingBuckets, err = computeAgingBuckets(agingRows, end)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func computeAgingBuckets(invoices []sqlcgen.Invoice, asOf time.Time) ([]AgingBucket, error) {
	buckets := []AgingBucket{
		{Label: "Current", Amount: decimal.Zero},
		{Label: "1-30", Amount: decimal.Zero},
		{Label: "31-60", Amount: decimal.Zero},
		{Label: "61-90", Amount: decimal.Zero},
		{Label: "90+", Amount: decimal.Zero},
	}

	for _, inv := range invoices {
		balanceDue, err := ledgermath.NumericToDecimal(inv.BalanceDue)
		if err != nil {
			return nil, err
		}
		if !balanceDue.IsPositive() {
			continue
		}

		daysOverdue := int(asOf.Sub(inv.DueDate.Time).Hours() / 24)
		idx := 0
		switch {
		case daysOverdue <= 0:
			idx = 0
		case daysOverdue <= 30:
			idx = 1
		case daysOverdue <= 60:
			idx = 2
		case daysOverdue <= 90:
			idx = 3
		default:
			idx = 4
		}
		buckets[idx].Amount = buckets[idx].Amount.Add(balanceDue)
	}
	return buckets, nil
}
