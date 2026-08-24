package pdf

import (
	"fmt"

	avav1 "github.com/silverbp/ava/gen/ava/v1"
)

// RenderInvoice renders an *avav1.Invoice (with its line items already
// populated) to PDF. Takes the proto type directly, unlike the report
// renderers — an invoice is a document the API already returns fully
// formed, with no separate Go-native computation layer to render from.
func RenderInvoice(businessName, contactName string, inv *avav1.Invoice) ([]byte, error) {
	d := New()
	d.Header(businessName)

	title := "INVOICE"
	if inv.GetInvoiceType() == "PURCHASE" {
		title = "BILL"
	}
	d.Title(fmt.Sprintf("%s %s", title, inv.GetInvoiceNumber()))

	d.KeyValueRow("Bill To", contactName)
	d.KeyValueRow("Invoice Date", formatProtoDate(inv.GetInvoiceDate()))
	d.KeyValueRow("Due Date", formatProtoDate(inv.GetDueDate()))
	d.KeyValueRow("Status", inv.GetStatus())
	d.Spacer(4)

	cols := []TableColumn{
		{Header: "Description", Width: 0.4},
		{Header: "Qty", Width: 0.1, Right: true},
		{Header: "Unit Price", Width: 0.15, Right: true},
		{Header: "Tax", Width: 0.15, Right: true},
		{Header: "Line Total", Width: 0.2, Right: true},
	}
	var rows [][]string
	for _, li := range inv.GetLineItems() {
		rows = append(rows, []string{
			li.GetDescription(),
			li.GetQuantity().GetValue(),
			li.GetUnitPrice().GetValue(),
			li.GetTaxAmount().GetValue(),
			li.GetLineTotal().GetValue(),
		})
	}
	d.Table(cols, rows, nil)

	d.Spacer(2)
	d.KeyValueRow("Subtotal", inv.GetSubtotal().GetValue())
	d.KeyValueRow("Tax", inv.GetTotalTaxAmount().GetValue())
	d.KeyValueRow("Total", inv.GetTotalAmount().GetValue())
	d.KeyValueRow("Paid", inv.GetPaidAmount().GetValue())
	d.KeyValueRow("Balance Due", inv.GetBalanceDue().GetValue())

	return d.Bytes()
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
