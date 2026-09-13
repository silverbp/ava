-- Copyright (c) 2025 Casey Entzi
-- SPDX-License-Identifier: MIT

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

-- name: GetLatestBankStatementForAccount :one
-- Most recent non-deleted statement for an account, strictly before a given
-- date - the chaining check in Create/UpdateBankStatement compares its
-- opening balance against this one's closing balance. exclude_id keeps an
-- update from chaining a statement against itself when its date moves later
-- (pass 0 on create).
SELECT * FROM bank_statement
WHERE ledger_account_id = sqlc.arg('ledger_account_id')
    AND statement_date < sqlc.arg('before_date')
    AND id <> sqlc.arg('exclude_id')
    AND deleted_at IS NULL
ORDER BY statement_date DESC, id DESC
LIMIT 1;

-- name: UpdateBankStatement :one
UPDATE bank_statement SET
    statement_name = COALESCE(sqlc.narg('statement_name'), statement_name),
    statement_date = COALESCE(sqlc.narg('statement_date'), statement_date),
    opening_balance = COALESCE(sqlc.narg('opening_balance'), opening_balance),
    closing_balance = COALESCE(sqlc.narg('closing_balance'), closing_balance),
    updated_at = NOW()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
    AND (sqlc.narg('resource_version')::bigint IS NULL OR resource_version = sqlc.narg('resource_version'))
RETURNING *;

-- name: DeactivateBankStatement :one
UPDATE bank_statement SET deleted_at = NOW(), updated_at = NOW()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
    AND (sqlc.narg('resource_version')::bigint IS NULL OR resource_version = sqlc.narg('resource_version'))
RETURNING *;

-- name: CreateBankStatementLine :one
INSERT INTO bank_statement_line (bank_statement_id, ledger_transaction_id, display_sequence)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListBankStatementLines :many
SELECT * FROM bank_statement_line WHERE bank_statement_id = $1 ORDER BY display_sequence, id;

-- name: CountBankStatementLines :one
SELECT COUNT(*) FROM bank_statement_line WHERE bank_statement_id = $1;

-- name: DeleteBankStatementLine :exec
-- Hard delete, not soft: the unique index on (bank_statement_id,
-- ledger_transaction_id) would otherwise block re-reconciling the same
-- transaction, and a line carries no financial content of its own to keep
-- history of.
DELETE FROM bank_statement_line
WHERE bank_statement_id = sqlc.arg('bank_statement_id') AND ledger_transaction_id = sqlc.arg('ledger_transaction_id');

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
            AND bs.deleted_at IS NULL
    )
ORDER BY lt.transaction_date;
