-- Copyright (c) 2025 Casey Entzi
-- SPDX-License-Identifier: MIT

-- name: CreateLedgerAccount :one
INSERT INTO ledger_account (
    business_id, account_type_id, parent_account_id, code, name, description,
    is_system, is_reconcilable, is_container, cash_flow_category_id,
    balance_sheet_category_id, income_statement_category_id, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING *;

-- name: GetLedgerAccount :one
SELECT * FROM ledger_account WHERE id = $1;

-- name: GetLedgerAccountByCode :one
SELECT * FROM ledger_account WHERE business_id = $1 AND code = $2;

-- name: ListLedgerAccounts :many
SELECT * FROM ledger_account WHERE business_id = $1 ORDER BY code;

-- name: UpdateLedgerAccount :one
UPDATE ledger_account SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    is_reconcilable = COALESCE(sqlc.narg('is_reconcilable'), is_reconcilable),
    is_container = COALESCE(sqlc.narg('is_container'), is_container),
    cash_flow_category_id = COALESCE(sqlc.narg('cash_flow_category_id'), cash_flow_category_id),
    balance_sheet_category_id = COALESCE(sqlc.narg('balance_sheet_category_id'), balance_sheet_category_id),
    income_statement_category_id = COALESCE(sqlc.narg('income_statement_category_id'), income_statement_category_id),
    updated_at = NOW()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeactivateLedgerAccount :one
UPDATE ledger_account SET is_active = FALSE, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: CreateLedgerTransaction :one
INSERT INTO ledger_transaction (
    business_id, transaction_date, description, reference_number, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: GetLedgerTransaction :one
SELECT * FROM ledger_transaction WHERE id = $1 AND deleted_at IS NULL;

-- name: ListLedgerTransactions :many
SELECT * FROM ledger_transaction
WHERE business_id = $1 AND deleted_at IS NULL AND id < sqlc.arg('before_id')
ORDER BY id DESC
LIMIT sqlc.arg('page_limit');

-- name: CreateLedgerEntry :one
INSERT INTO ledger_entry (
    business_id, ledger_transaction_id, account_id, debit_amount, credit_amount, description
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: ListLedgerEntriesByTransaction :many
SELECT * FROM ledger_entry WHERE ledger_transaction_id = $1 AND deleted_at IS NULL ORDER BY id;

-- name: ListLedgerEntriesByTransactionIDs :many
SELECT * FROM ledger_entry WHERE ledger_transaction_id = ANY(sqlc.arg('transaction_ids')::bigint[]) AND deleted_at IS NULL ORDER BY id;

-- name: SoftDeleteLedgerEntriesByTransaction :exec
UPDATE ledger_entry SET deleted_at = NOW()
WHERE ledger_transaction_id = $1 AND deleted_at IS NULL;

-- name: GetLedgerAccountType :one
SELECT * FROM ledger_account_type WHERE id = $1;
