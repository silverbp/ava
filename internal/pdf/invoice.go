// Copyright (c) 2025 Casey Entzi
// SPDX-License-Identifier: MIT

package pdf

import (
	"fmt"

	"github.com/shopspring/decimal"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
)

// TaxBreakdownRow is one row of an invoice's tax breakdown, grouped by tax
// rate. Invoice PDFs conventionally roll tax up by rate at the bottom
// rather than showing it as a per-line column, since most line items on a
// given invoice carry no tax at all.
type TaxBreakdownRow struct {
	Label string
	Net   decimal.Decimal
	Tax   decimal.Decimal
	Total decimal.Decimal
}

// RenderInvoice renders an *avav1.Invoice (with its line items already
// populated) to PDF. Takes the proto type directly, unlike the report
// renderers — an invoice is a document the API already returns fully
// formed, with no separate Go-native computation layer to render from.
func RenderInvoice(business, billTo Party, inv *avav1.Invoice, breakdown []TaxBreakdownRow) ([]byte, error) {
	d := New()
	d.AddressBlock(billTo, business)

	title := "Sales Invoice"
	if inv.GetInvoiceType() == "PURCHASE" {
		title = "Purchase Bill"
	}
	d.CenteredTitle(title)

	d.KeyValueRow("Invoice Number", inv.GetInvoiceNumber())
	d.KeyValueRow("Invoice Date", formatProtoDate(inv.GetInvoiceDate()))
	d.KeyValueRow("Due Date", formatProtoDate(inv.GetDueDate()))
	d.KeyValueRow("Status", inv.GetStatus())
	d.Spacer(4)

	cols := []TableColumn{
		{Header: "Description", Width: 0.5},
		{Header: "Qty", Width: 0.12, Right: true},
		{Header: "Unit Price", Width: 0.18, Right: true},
		{Header: "Line Total", Width: 0.2, Right: true},
	}
	var rows [][]string
	for _, li := range inv.GetLineItems() {
		rows = append(rows, []string{
			li.GetDescription(),
			li.GetQuantity().GetValue(),
			li.GetUnitPrice().GetValue(),
			li.GetLineTotal().GetValue(),
		})
	}
	d.Table(cols, rows, nil)

	if showsTaxBreakdown(breakdown) {
		d.Spacer(2)
		d.SetSectionTitle("Tax Breakdown")
		breakdownCols := []TableColumn{
			{Header: "Rate", Width: 0.4},
			{Header: "Net", Width: 0.2, Right: true},
			{Header: "Tax", Width: 0.2, Right: true},
			{Header: "Total", Width: 0.2, Right: true},
		}
		var breakdownRows [][]string
		for _, b := range breakdown {
			breakdownRows = append(breakdownRows, []string{b.Label, formatMoney(b.Net), formatMoney(b.Tax), formatMoney(b.Total)})
		}
		d.Table(breakdownCols, breakdownRows, nil)
	}

	d.Spacer(2)
	d.KeyValueRow("Subtotal", inv.GetSubtotal().GetValue())
	d.KeyValueRow("Tax", inv.GetTotalTaxAmount().GetValue())
	d.KeyValueRow("Total", inv.GetTotalAmount().GetValue())
	d.KeyValueRow("Paid", inv.GetPaidAmount().GetValue())
	d.KeyValueRow("Balance Due", inv.GetBalanceDue().GetValue())

	return d.Bytes()
}

// showsTaxBreakdown reports whether the breakdown table is worth printing:
// skip it when there's a single untaxed bucket, since the Tax line below
// already says 0.00.
func showsTaxBreakdown(breakdown []TaxBreakdownRow) bool {
	if len(breakdown) > 1 {
		return true
	}
	return len(breakdown) == 1 && !breakdown[0].Tax.IsZero()
}

func formatProtoDate(d interface {
	GetYear() int32
	GetMonth() int32
	GetDay() int32
}) string {
	if d == nil {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.GetYear(), d.GetMonth(), d.GetDay())
}
