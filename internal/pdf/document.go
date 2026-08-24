// Package pdf renders reports and trading documents to PDF using
// github.com/go-pdf/fpdf — a pure-Go library, so cmd/ava stays a single
// static binary with no runtime dependency (no headless Chrome, no
// wkhtmltopdf). fpdf supports embedding PNG/JPEG images directly, so a
// business logo can be added later once there's a binary source for one
// (business.logo_url today is just a URL string, not stored image bytes —
// that needs Phase 8's attachment infrastructure first).
package pdf

import (
	"bytes"
	"fmt"

	"github.com/go-pdf/fpdf"
)

const (
	pageWidth   = 210.0 // A4, mm
	marginLeft  = 15.0
	marginRight = 15.0
	contentW    = pageWidth - marginLeft - marginRight
)

// Document wraps fpdf with the layout conventions shared by every report
// and trading document this package renders: a business header, a title,
// and bordered tables.
type Document struct {
	pdf *fpdf.Fpdf
	// tr converts UTF-8 Go strings to cp1252, which fpdf's core Helvetica
	// font expects — every string handed to fpdf goes through this. Without
	// it, any non-ASCII character (an accented business name, a curly quote
	// pasted from a word processor, an em dash) renders as mojibake: fpdf's
	// core fonts don't auto-transcode UTF-8 input.
	tr func(string) string
}

// New starts a new A4 document with margins set and the first page added.
func New() *Document {
	p := fpdf.New("P", "mm", "A4", "")
	p.SetMargins(marginLeft, 15, marginRight)
	p.SetAutoPageBreak(true, 15)
	p.AddPage()
	return &Document{pdf: p, tr: p.UnicodeTranslatorFromDescriptor("")}
}

// Header prints the business name (bold) and any additional lines
// (address, phone, email — caller's choice) below it, left-aligned.
func (d *Document) Header(businessName string, lines ...string) {
	d.pdf.SetFont("Helvetica", "B", 14)
	d.pdf.CellFormat(contentW, 7, d.tr(businessName), "", 1, "L", false, 0, "")
	d.pdf.SetFont("Helvetica", "", 9)
	for _, l := range lines {
		d.pdf.CellFormat(contentW, 5, d.tr(l), "", 1, "L", false, 0, "")
	}
	d.pdf.Ln(4)
}

// Title prints a document title, e.g. "INVOICE INV1000" or "Trial Balance".
func (d *Document) Title(text string) {
	d.pdf.SetFont("Helvetica", "B", 16)
	d.pdf.CellFormat(contentW, 9, d.tr(text), "", 1, "L", false, 0, "")
	d.pdf.Ln(2)
}

// Subtitle prints a smaller line under the title, e.g. a date range or
// as-of date.
func (d *Document) Subtitle(text string) {
	d.pdf.SetFont("Helvetica", "I", 10)
	d.pdf.CellFormat(contentW, 6, d.tr(text), "", 1, "L", false, 0, "")
	d.pdf.Ln(2)
}

// KeyValueRow prints a label/value pair on one line — for a document's
// metadata block (invoice number, date, due date, bill-to, ...).
func (d *Document) KeyValueRow(label, value string) {
	d.pdf.SetFont("Helvetica", "B", 9)
	d.pdf.CellFormat(35, 5.5, d.tr(label), "", 0, "L", false, 0, "")
	d.pdf.SetFont("Helvetica", "", 9)
	d.pdf.CellFormat(contentW-35, 5.5, d.tr(value), "", 1, "L", false, 0, "")
}

func (d *Document) Spacer(h float64) {
	d.pdf.Ln(h)
}

// SummaryLine prints a bold label/value line with no border — for a
// derived subtotal that stands outside any bordered table (e.g. a balance
// sheet's "Net current assets" row between sections).
func (d *Document) SummaryLine(label, value string) {
	d.pdf.SetFont("Helvetica", "B", 10)
	d.pdf.CellFormat(contentW*0.75, 6, d.tr(label), "", 0, "L", false, 0, "")
	d.pdf.CellFormat(contentW*0.25, 6, d.tr(value), "", 1, "R", false, 0, "")
}

// SetSectionTitle prints a bold section heading, e.g. "Assets" on a
// balance sheet or "Activity" on a statement.
func (d *Document) SetSectionTitle(text string) {
	d.pdf.SetFont("Helvetica", "B", 11)
	d.pdf.CellFormat(contentW, 6, d.tr(text), "", 1, "L", false, 0, "")
}

// TableColumn is one column of a Table — Header text, its width as a
// fraction of the content width (must sum to ~1.0 across all columns), and
// whether values right-align (numbers) or left-align (text).
type TableColumn struct {
	Header string
	Width  float64
	Right  bool
}

// Table renders a bordered table: a shaded header row, then one row per
// entry in rows (each must have len(cols) cells), then — if totalRow is
// non-nil — a bold total row.
func (d *Document) Table(cols []TableColumn, rows [][]string, totalRow []string) {
	widths := make([]float64, len(cols))
	for i, c := range cols {
		widths[i] = contentW * c.Width
	}

	d.pdf.SetFont("Helvetica", "B", 9)
	d.pdf.SetFillColor(230, 230, 230)
	for i, c := range cols {
		d.pdf.CellFormat(widths[i], 7, d.tr(c.Header), "1", 0, alignOf(c.Right), true, 0, "")
	}
	d.pdf.Ln(-1)

	d.pdf.SetFont("Helvetica", "", 9)
	for _, row := range rows {
		for i, cell := range row {
			d.pdf.CellFormat(widths[i], 6, d.tr(cell), "1", 0, alignOf(cols[i].Right), false, 0, "")
		}
		d.pdf.Ln(-1)
	}

	if totalRow != nil {
		d.pdf.SetFont("Helvetica", "B", 9)
		for i, cell := range totalRow {
			d.pdf.CellFormat(widths[i], 7, d.tr(cell), "1", 0, alignOf(cols[i].Right), false, 0, "")
		}
		d.pdf.Ln(-1)
	}
}

func alignOf(right bool) string {
	if right {
		return "R"
	}
	return "L"
}

// Bytes finalizes the document and returns its PDF bytes.
func (d *Document) Bytes() ([]byte, error) {
	var buf bytes.Buffer
	if err := d.pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("rendering pdf: %w", err)
	}
	return buf.Bytes(), nil
}
