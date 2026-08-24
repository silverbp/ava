-- name: CreateContact :one
INSERT INTO contact (
    business_id, ledger_account_id, contact_number, is_customer, is_vendor, name,
    email, phone, payment_terms_days, credit_limit, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: GetContact :one
SELECT * FROM contact WHERE id = $1 AND deleted_at IS NULL;

-- name: ListContacts :many
SELECT * FROM contact WHERE business_id = $1 AND deleted_at IS NULL ORDER BY name;

-- name: UpdateContact :one
UPDATE contact SET
    name = COALESCE(sqlc.narg('name'), name),
    email = COALESCE(sqlc.narg('email'), email),
    phone = COALESCE(sqlc.narg('phone'), phone),
    ledger_account_id = COALESCE(sqlc.narg('ledger_account_id'), ledger_account_id),
    payment_terms_days = COALESCE(sqlc.narg('payment_terms_days'), payment_terms_days),
    credit_limit = COALESCE(sqlc.narg('credit_limit'), credit_limit),
    updated_at = NOW()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: DeactivateContact :one
UPDATE contact SET is_active = FALSE, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: CreateService :one
INSERT INTO service (
    business_id, service_code, name, description, unit_of_measure, cost_price,
    retail_price, is_taxable, default_tax_rate, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: GetService :one
SELECT * FROM service WHERE id = $1 AND deleted_at IS NULL;

-- name: ListServices :many
SELECT * FROM service WHERE business_id = $1 AND deleted_at IS NULL ORDER BY service_code;

-- name: UpdateService :one
UPDATE service SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    retail_price = COALESCE(sqlc.narg('retail_price'), retail_price),
    cost_price = COALESCE(sqlc.narg('cost_price'), cost_price),
    is_taxable = COALESCE(sqlc.narg('is_taxable'), is_taxable),
    default_tax_rate = COALESCE(sqlc.narg('default_tax_rate'), default_tax_rate),
    updated_at = NOW()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: DeactivateService :one
UPDATE service SET is_active = FALSE, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: CreateTaxRate :one
INSERT INTO tax_rate (
    business_id, name, rate, tax_liability_account_id, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: GetTaxRate :one
SELECT * FROM tax_rate WHERE id = $1;

-- name: ListTaxRates :many
SELECT * FROM tax_rate WHERE business_id = $1 ORDER BY name;

-- name: UpdateTaxRate :one
UPDATE tax_rate SET
    name = COALESCE(sqlc.narg('name'), name),
    rate = COALESCE(sqlc.narg('rate'), rate),
    updated_at = NOW()
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeactivateTaxRate :one
UPDATE tax_rate SET is_active = FALSE, updated_at = NOW()
WHERE id = $1
RETURNING *;
