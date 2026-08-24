package pdf

import (
	"fmt"

	"github.com/silverbp/ava/internal/reporting"
)

func fmtDate(t interface{ Format(string) string }) string {
	return t.Format("2006-01-02")
}

// RenderTrialBalance renders a TrialBalanceResult to PDF.
func RenderTrialBalance(businessName string, asOf string, r *reporting.TrialBalanceResult) ([]byte, error) {
	d := New()
	d.Header(businessName)
	d.Title("Trial Balance")
	d.Subtitle("As of " + asOf)

	cols := []TableColumn{
		{Header: "Code", Width: 0.15},
		{Header: "Account", Width: 0.45},
		{Header: "Debit", Width: 0.20, Right: true},
		{Header: "Credit", Width: 0.20, Right: true},
	}
	var rows [][]string
	for _, l := range r.Lines {
		rows = append(rows, []string{l.Code, l.Name, l.Debit.StringFixed(2), l.Credit.StringFixed(2)})
	}
	d.Table(cols, rows, []string{"", "Total", r.TotalDebit.StringFixed(2), r.TotalCredit.StringFixed(2)})

	return d.Bytes()
}

// RenderBalanceSheet renders a BalanceSheetResult to PDF.
func RenderBalanceSheet(businessName string, asOf string, r *reporting.BalanceSheetResult) ([]byte, error) {
	d := New()
	d.Header(businessName)
	d.Title("Balance Sheet")
	d.Subtitle("As of " + asOf)

	cols := []TableColumn{
		{Header: "Code", Width: 0.2},
		{Header: "Account", Width: 0.55},
		{Header: "Balance", Width: 0.25, Right: true},
	}
	section := func(title string, lines []reporting.AccountLine, total interface{ StringFixed(int32) string }) {
		d.Spacer(3)
		d.SetSectionTitle(title)
		var rows [][]string
		for _, l := range lines {
			rows = append(rows, []string{l.Code, l.Name, l.Amount.StringFixed(2)})
		}
		d.Table(cols, rows, []string{"", "Total " + title, total.StringFixed(2)})
	}
	section("Assets", r.Assets, r.TotalAssets)
	section("Liabilities", r.Liabilities, r.TotalLiabilities)
	section("Equity", r.Equity, r.TotalEquity)

	return d.Bytes()
}

// RenderIncomeStatement renders an IncomeStatementResult to PDF.
func RenderIncomeStatement(businessName string, periodLabel string, r *reporting.IncomeStatementResult) ([]byte, error) {
	d := New()
	d.Header(businessName)
	d.Title("Income Statement")
	d.Subtitle(periodLabel)

	cols := []TableColumn{
		{Header: "Code", Width: 0.2},
		{Header: "Account", Width: 0.55},
		{Header: "Amount", Width: 0.25, Right: true},
	}
	section := func(title string, lines []reporting.AccountLine, total interface{ StringFixed(int32) string }) {
		d.Spacer(3)
		d.SetSectionTitle(title)
		var rows [][]string
		for _, l := range lines {
			rows = append(rows, []string{l.Code, l.Name, l.Amount.StringFixed(2)})
		}
		d.Table(cols, rows, []string{"", "Total " + title, total.StringFixed(2)})
	}
	section("Revenue", r.Revenue, r.TotalRevenue)
	section("Expenses", r.Expenses, r.TotalExpenses)

	d.Spacer(3)
	d.KeyValueRow("Net Income", r.NetIncome.StringFixed(2))

	return d.Bytes()
}

// RenderGeneralLedger renders a GeneralLedgerResult to PDF.
func RenderGeneralLedger(businessName string, periodLabel string, r *reporting.GeneralLedgerResult) ([]byte, error) {
	d := New()
	d.Header(businessName)
	d.Title(fmt.Sprintf("General Ledger - %s %s", r.Code, r.Name))
	d.Subtitle(periodLabel)

	cols := []TableColumn{
		{Header: "Date", Width: 0.15},
		{Header: "Txn", Width: 0.15, Right: true},
		{Header: "Debit", Width: 0.2, Right: true},
		{Header: "Credit", Width: 0.2, Right: true},
		{Header: "Balance", Width: 0.3, Right: true},
	}
	var rows [][]string
	for _, l := range r.Lines {
		rows = append(rows, []string{
			fmtDate(l.TransactionDate), fmt.Sprintf("%d", l.LedgerTransactionID),
			l.Debit.StringFixed(2), l.Credit.StringFixed(2), l.RunningBalance.StringFixed(2),
		})
	}
	d.Table(cols, rows, []string{"", "", "", "Ending Balance", r.EndingBalance.StringFixed(2)})

	return d.Bytes()
}

// RenderCustomerStatement renders a CustomerStatementResult to PDF.
func RenderCustomerStatement(businessName string, r *reporting.CustomerStatementResult) ([]byte, error) {
	d := New()
	d.Header(businessName)
	d.Title("Customer Statement - " + r.ContactName)
	d.Subtitle(fmt.Sprintf("%s through %s", fmtDate(r.PeriodStart), fmtDate(r.PeriodEnd)))

	d.SetSectionTitle("Activity")
	activityCols := []TableColumn{
		{Header: "Date", Width: 0.15},
		{Header: "Description", Width: 0.45},
		{Header: "Debit", Width: 0.13, Right: true},
		{Header: "Credit", Width: 0.13, Right: true},
		{Header: "Balance", Width: 0.14, Right: true},
	}
	var activityRows [][]string
	for _, a := range r.Activity {
		activityRows = append(activityRows, []string{
			fmtDate(a.Date), a.Description, a.Debit.StringFixed(2), a.Credit.StringFixed(2), a.RunningBalance.StringFixed(2),
		})
	}
	d.Table(activityCols, activityRows, []string{"", "", "", "Ending Balance", r.EndingBalance.StringFixed(2)})

	d.Spacer(4)
	d.SetSectionTitle("Aging")
	agingCols := []TableColumn{
		{Header: "Current", Width: 0.2, Right: true},
		{Header: "1-30", Width: 0.2, Right: true},
		{Header: "31-60", Width: 0.2, Right: true},
		{Header: "61-90", Width: 0.2, Right: true},
		{Header: "90+", Width: 0.2, Right: true},
	}
	var agingRow []string
	for _, b := range r.AgingBuckets {
		agingRow = append(agingRow, b.Amount.StringFixed(2))
	}
	d.Table(agingCols, [][]string{agingRow}, nil)

	return d.Bytes()
}
