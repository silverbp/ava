// Package reporting computes the financial reports over the ledger — trial
// balance, balance sheet, income statement, general ledger — as plain Go
// (native time.Time/decimal.Decimal, no proto dependency), so it's directly
// unit-testable against a real Postgres, matching internal/periodclose.
package reporting

import (
	"time"

	"github.com/shopspring/decimal"
)

// AccountLine is one row of a report keyed to a single ledger_account
// (Amount's sign convention depends on the report: a balance-sheet net
// balance, a revenue/expense total, ...).
type AccountLine struct {
	AccountID int32
	Code      string
	Name      string
	Amount    decimal.Decimal
}

type TrialBalanceLine struct {
	AccountID int32
	Code      string
	Name      string
	Debit     decimal.Decimal
	Credit    decimal.Decimal
}

type TrialBalanceResult struct {
	Lines       []TrialBalanceLine
	TotalDebit  decimal.Decimal
	TotalCredit decimal.Decimal
}

type BalanceSheetResult struct {
	Assets           []AccountLine
	TotalAssets      decimal.Decimal
	Liabilities      []AccountLine
	TotalLiabilities decimal.Decimal
	Equity           []AccountLine
	TotalEquity      decimal.Decimal
}

type IncomeStatementResult struct {
	Revenue       []AccountLine
	TotalRevenue  decimal.Decimal
	Expenses      []AccountLine
	TotalExpenses decimal.Decimal
	NetIncome     decimal.Decimal
}

type GeneralLedgerLine struct {
	LedgerTransactionID int64
	TransactionDate     time.Time
	Description         *string
	Debit               decimal.Decimal
	Credit              decimal.Decimal
	RunningBalance      decimal.Decimal
}

type GeneralLedgerResult struct {
	AccountID     int32
	Code          string
	Name          string
	Lines         []GeneralLedgerLine
	EndingBalance decimal.Decimal
}
