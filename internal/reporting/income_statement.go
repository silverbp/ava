package reporting

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/ledgermath"
)

// IncomeStatement sums REVENUE/EXPENSE activity over [start, end]. Reads
// ledger_entry directly regardless of period-close state — a P&L for a
// range doesn't care whether that range has since been closed
// (docs/architecture.md#period-close, "Reporting integration").
func IncomeStatement(ctx context.Context, q *sqlcgen.Queries, businessID int64, start, end time.Time) (*IncomeStatementResult, error) {
	rows, err := q.AccountBalancesAsOf(ctx, sqlcgen.AccountBalancesAsOfParams{
		BusinessID:  businessID,
		PeriodStart: ledgermath.PgDate(start),
		PeriodEnd:   ledgermath.PgDate(end),
	})
	if err != nil {
		return nil, err
	}

	result := &IncomeStatementResult{TotalRevenue: decimal.Zero, TotalExpenses: decimal.Zero}
	for _, r := range rows {
		if r.AccountTypeID != revenueTypeID && r.AccountTypeID != expensesTypeID {
			continue
		}
		debit, err := ledgermath.NumericToDecimal(r.TotalDebit)
		if err != nil {
			return nil, err
		}
		credit, err := ledgermath.NumericToDecimal(r.TotalCredit)
		if err != nil {
			return nil, err
		}
		if debit.IsZero() && credit.IsZero() {
			continue
		}

		net := ledgermath.NetBalance(r.NormalBalance, debit, credit)
		line := AccountLine{AccountID: r.AccountID, Code: r.Code, Name: r.Name, Amount: net}

		if r.AccountTypeID == revenueTypeID {
			result.Revenue = append(result.Revenue, line)
			result.TotalRevenue = result.TotalRevenue.Add(net)
		} else {
			result.Expenses = append(result.Expenses, line)
			result.TotalExpenses = result.TotalExpenses.Add(net)
		}
	}
	result.NetIncome = result.TotalRevenue.Sub(result.TotalExpenses)
	return result, nil
}
