-- Copyright (c) 2025 Casey Entzi
-- SPDX-License-Identifier: MIT

-- name: CreateEstimate :one
INSERT INTO estimate (
    business_id, customer_id, estimate_number, estimate_date, expiration_date,
    subtotal, total_tax_amount, total_amount, notes, terms, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: CreateEstimateLineItem :one
INSERT INTO estimate_line_item (
    estimate_id, service_id, line_number, description, quantity, unit_price,
    line_subtotal, is_taxable, tax_rate_id, tax_rate, tax_amount, line_total
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
)
RETURNING *;

-- name: GetEstimate :one
SELECT * FROM estimate WHERE id = $1 AND deleted_at IS NULL;

-- name: ListEstimateLineItems :many
SELECT * FROM estimate_line_item WHERE estimate_id = $1 AND deleted_at IS NULL ORDER BY line_number;

-- name: ListEstimates :many
SELECT * FROM estimate
WHERE business_id = sqlc.arg('business_id') AND deleted_at IS NULL
    AND (sqlc.arg('include_all')::bool OR status IN ('DRAFT', 'SENT'))
ORDER BY estimate_date DESC;

-- name: UpdateEstimateStatus :one
UPDATE estimate SET status = $2, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: CreateInvoice :one
INSERT INTO invoice (
    business_id, contact_id, invoice_type, estimate_id, invoice_number, invoice_date,
    due_date, subtotal, total_tax_amount, total_amount, balance_due, notes, terms,
    created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING *;

-- name: CreateInvoiceLineItem :one
INSERT INTO invoice_line_item (
    invoice_id, service_id, ledger_account_id, line_number, description, quantity,
    unit_price, line_subtotal, is_taxable, tax_rate_id, tax_rate, tax_amount, line_total
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
)
RETURNING *;

-- name: GetInvoice :one
SELECT * FROM invoice WHERE id = $1 AND deleted_at IS NULL;

-- name: ListInvoiceLineItems :many
SELECT * FROM invoice_line_item WHERE invoice_id = $1 AND deleted_at IS NULL ORDER BY line_number;

-- name: ListInvoices :many
SELECT * FROM invoice
WHERE business_id = sqlc.arg('business_id') AND deleted_at IS NULL
    AND (sqlc.arg('include_all')::bool OR status NOT IN ('PAID', 'CANCELLED'))
ORDER BY invoice_date DESC;

-- name: UpdateInvoiceStatus :one
UPDATE invoice SET status = $2, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: SetInvoiceLedgerTransaction :one
UPDATE invoice SET ledger_transaction_id = $2, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: ApplyPaymentToInvoice :one
UPDATE invoice SET
    paid_amount = paid_amount + sqlc.arg('amount'),
    balance_due = balance_due - sqlc.arg('amount'),
    status = CASE WHEN balance_due - sqlc.arg('amount') <= 0 THEN 'PAID' ELSE status END,
    updated_at = NOW()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: CreatePayment :one
INSERT INTO payment (
    business_id, contact_id, payment_type, payment_number, payment_date,
    amount, payment_method, ledger_account_id, reference_number, notes, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: GetPayment :one
SELECT * FROM payment WHERE id = $1 AND deleted_at IS NULL;

-- name: ListPayments :many
SELECT * FROM payment WHERE business_id = $1 AND deleted_at IS NULL ORDER BY payment_date DESC;

-- name: SetPaymentLedgerTransaction :one
UPDATE payment SET ledger_transaction_id = $2, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: CreatePaymentApplication :one
INSERT INTO payment_application (payment_id, invoice_id, applied_amount)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListPaymentApplicationsForPayment :many
SELECT * FROM payment_application WHERE payment_id = $1 ORDER BY id;

-- name: ListInvoicesForContact :many
SELECT * FROM invoice
WHERE contact_id = sqlc.arg('contact_id') AND deleted_at IS NULL
    AND invoice_date BETWEEN sqlc.arg('period_start') AND sqlc.arg('period_end')
ORDER BY invoice_date;

-- name: ListPaymentsForContact :many
SELECT * FROM payment
WHERE contact_id = sqlc.arg('contact_id') AND deleted_at IS NULL
    AND payment_date BETWEEN sqlc.arg('period_start') AND sqlc.arg('period_end')
ORDER BY payment_date;
