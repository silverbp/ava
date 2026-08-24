package reporting

// account_type_id values seeded by migrations/00001_initial.up.sql. Sign
// arithmetic (net balances, decimal/date conversion) lives in
// internal/ledgermath, shared with internal/periodclose.
const (
	assetsTypeID      = 1
	liabilitiesTypeID = 2
	equityTypeID      = 3
	revenueTypeID     = 4
	expensesTypeID    = 5
)
