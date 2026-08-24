-- name: CreateBusiness :one
INSERT INTO business (
    name, tax_id, address_line1, address_line2, city, state, postal_code,
    country, phone, email, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: GetBusiness :one
SELECT * FROM business WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateBusiness :one
UPDATE business SET
    name = COALESCE(sqlc.narg('name'), name),
    tax_id = COALESCE(sqlc.narg('tax_id'), tax_id),
    address_line1 = COALESCE(sqlc.narg('address_line1'), address_line1),
    address_line2 = COALESCE(sqlc.narg('address_line2'), address_line2),
    city = COALESCE(sqlc.narg('city'), city),
    state = COALESCE(sqlc.narg('state'), state),
    postal_code = COALESCE(sqlc.narg('postal_code'), postal_code),
    country = COALESCE(sqlc.narg('country'), country),
    phone = COALESCE(sqlc.narg('phone'), phone),
    email = COALESCE(sqlc.narg('email'), email),
    updated_at = NOW()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
RETURNING *;

-- name: DeactivateBusiness :one
UPDATE business SET is_active = FALSE, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: ListBusinessesForUser :many
SELECT b.*, bu.role AS membership_role
FROM business b
JOIN business_user bu ON bu.business_id = b.id
WHERE bu.user_id = $1 AND b.deleted_at IS NULL
ORDER BY b.name;

-- name: ConsumeNextEstimateNumber :one
-- Atomically claims and increments business.next_estimate_number, returning
-- the prefix + the number just claimed (the pre-increment value).
UPDATE business SET next_estimate_number = next_estimate_number + 1, updated_at = NOW()
WHERE id = $1
RETURNING estimate_number_prefix AS prefix, (next_estimate_number - 1)::int AS claimed_number;

-- name: ConsumeNextInvoiceNumber :one
UPDATE business SET next_invoice_number = next_invoice_number + 1, updated_at = NOW()
WHERE id = $1
RETURNING invoice_number_prefix AS prefix, (next_invoice_number - 1)::int AS claimed_number;
