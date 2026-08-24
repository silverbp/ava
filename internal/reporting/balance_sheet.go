package reporting

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/silverbp/ava/internal/db/sqlcgen"
	"github.com/silverbp/ava/internal/ledgermath"
)

// currentEarningsLineCode labels the synthetic equity line below — not a
// real ledger_account, so it needs a code distinct from any real one
// (AccountID 0, which no real ledger_account uses, since ids start at 1).
const currentEarningsLineCode = "CURRENT-EARNINGS"

// BalanceSheet reports ASSETS/LIABILITIES/EQUITY balances as of asOf. If
// asOf falls after the business's latest unreversed period close (or no
// close exists yet), REVENUE/EXPENSE activity since that close hasn't been
// swept into Retained Earnings, so it's reported as a synthetic "Current
// Period Earnings" equity line - the standard accounting treatment for an
// as-of date within a still-open period
// (docs/architecture.md#period-close, "Reporting integration").
func BalanceSheet(ctx context.Context, q *sqlcgen.Queries, businessID int64, asOf time.Time) (*BalanceSheetResult, error) {
	rows, err := q.AccountBalancesAsOf(ctx, sqlcgen.AccountBalancesAsOfParams{
		BusinessID:  businessID,
		PeriodStart: ledgermath.PgDate(ledgermath.InceptionDate),
		PeriodEnd:   ledgermath.PgDate(asOf),
	})
	if err != nil {
		return nil, err
	}

	result := &BalanceSheetResult{
		TotalAssets:      decimal.Zero,
		TotalLiabilities: decimal.Zero,
		TotalEquity:      decimal.Zero,
	}
	for _, r := range rows {
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

		switch r.AccountTypeID {
		case assetsTypeID:
			result.Assets = append(result.Assets, line)
			result.TotalAssets = result.TotalAssets.Add(net)
		case liabilitiesTypeID:
			result.Liabilities = append(result.Liabilities, line)
			result.TotalLiabilities = result.TotalLiabilities.Add(net)
		case equityTypeID:
			result.Equity = append(result.Equity, line)
			result.TotalEquity = result.TotalEquity.Add(net)
		}
	}

	lastClose, err := q.GetLastPeriodClose(ctx, businessID)
	periodStart := ledgermath.InceptionDate
	withinOpenPeriod := true
	switch {
	case err == nil:
		periodStart = lastClose.PeriodEnd.Time.AddDate(0, 0, 1)
		withinOpenPeriod = asOf.After(lastClose.PeriodEnd.Time)
	case errors.Is(err, pgx.ErrNoRows):
		// No close yet: the whole ledger-to-date is "the open period".
	default:
		return nil, err
	}

	if withinOpenPeriod {
		earnings, err := currentPeriodEarnings(ctx, q, businessID, periodStart, asOf)
		if err != nil {
			return nil, err
		}
		if !earnings.IsZero() {
			result.Equity = append(result.Equity, AccountLine{
				AccountID: 0,
				Code:      currentEarningsLineCode,
				Name:      "Current Period Earnings",
				Amount:    earnings,
			})
			result.TotalEquity = result.TotalEquity.Add(earnings)
		}
	}

	return result, nil
}

func currentPeriodEarnings(ctx context.Context, q *sqlcgen.Queries, businessID int64, start, end time.Time) (decimal.Decimal, error) {
	rows, err := q.AccountBalancesAsOf(ctx, sqlcgen.AccountBalancesAsOfParams{
		BusinessID:  businessID,
		PeriodStart: ledgermath.PgDate(start),
		PeriodEnd:   ledgermath.PgDate(end),
	})
	if err != nil {
		return decimal.Zero, err
	}

	total := decimal.Zero
	for _, r := range rows {
		if r.AccountTypeID != revenueTypeID && r.AccountTypeID != expensesTypeID {
			continue
		}
		debit, err := ledgermath.NumericToDecimal(r.TotalDebit)
		if err != nil {
			return decimal.Zero, err
		}
		credit, err := ledgermath.NumericToDecimal(r.TotalCredit)
		if err != nil {
			return decimal.Zero, err
		}
		// Net income = revenue - expenses. REVENUE's normal_balance is
		// CREDIT, so its net-income contribution is credit - debit;
		// EXPENSES's is DEBIT, and an expense must SUBTRACT from net
		// income, which is exactly credit - debit for a DEBIT-normal
		// account too (a debit posting is negative here). So summing
		// credit - debit across both account types in one pass gives net
		// income directly.
		total = total.Add(credit.Sub(debit))
	}
	return total, nil
}
