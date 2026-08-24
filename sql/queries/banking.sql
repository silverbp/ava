-- name: CreateBankStatement :one
INSERT INTO bank_statement (
    business_id, ledger_account_id, statement_name, statement_date,
    opening_balance, closing_balance, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetBankStatement :one
SELECT * FROM bank_statement WHERE id = $1 AND deleted_at IS NULL;

-- name: ListBankStatements :many
SELECT * FROM bank_statement WHERE business_id = $1 AND deleted_at IS NULL ORDER BY statement_date DESC;

-- name: CreateBankStatementLine :one
INSERT INTO bank_statement_line (bank_statement_id, ledger_transaction_id, display_sequence)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListBankStatementLines :many
SELECT * FROM bank_statement_line WHERE bank_statement_id = $1 ORDER BY display_sequence, id;

-- name: LedgerEntryExistsForAccount :one
SELECT EXISTS(
    SELECT 1 FROM ledger_entry
    WHERE ledger_transaction_id = sqlc.arg('ledger_transaction_id')
        AND account_id = sqlc.arg('account_id')
        AND deleted_at IS NULL
) AS entry_exists;

-- name: SumReconciledActivity :one
-- Total debit/credit, for one account, across every ledger_transaction
-- already reconciled (linked via bank_statement_line) to one bank_statement.
SELECT
    COALESCE(SUM(le.debit_amount), 0)::numeric AS total_debit,
    COALESCE(SUM(le.credit_amount), 0)::numeric AS total_credit
FROM bank_statement_line bsl
JOIN ledger_entry le ON le.ledger_transaction_id = bsl.ledger_transaction_id
    AND le.account_id = sqlc.arg('account_id')
    AND le.deleted_at IS NULL
WHERE bsl.bank_statement_id = sqlc.arg('bank_statement_id');

-- name: ListUnreconciledLedgerTransactions :many
-- ledger_transaction rows touching account_id, through_date, that have no
-- bank_statement_line yet under any bank_statement for THIS account
-- (reconciliation is scoped per-account: a transaction reconciled on one
-- side of a transfer isn't automatically reconciled on the other).
SELECT DISTINCT lt.*
FROM ledger_transaction lt
JOIN ledger_entry le ON le.ledger_transaction_id = lt.id AND le.deleted_at IS NULL
WHERE le.account_id = sqlc.arg('account_id')
    AND lt.deleted_at IS NULL
    AND lt.transaction_date <= sqlc.arg('through_date')
    AND NOT EXISTS (
        SELECT 1 FROM bank_statement_line bsl
        JOIN bank_statement bs ON bs.id = bsl.bank_statement_id
        WHERE bsl.ledger_transaction_id = lt.id AND bs.ledger_account_id = sqlc.arg('account_id')
    )
ORDER BY lt.transaction_date;
