-- name: GetAppUser :one
SELECT * FROM app_user WHERE id = $1 AND deleted_at IS NULL;

-- name: GetAppUserByEmail :one
SELECT * FROM app_user WHERE email = $1 AND deleted_at IS NULL;

-- name: CreateAppUser :one
INSERT INTO app_user (email, display_name)
VALUES ($1, $2)
RETURNING *;

-- name: GetBusinessUser :one
SELECT * FROM business_user WHERE business_id = $1 AND user_id = $2;

-- name: CreateBusinessUser :one
INSERT INTO business_user (business_id, user_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateUserSession :one
INSERT INTO user_session (user_id, refresh_token_hash, client_name, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetUserSessionByHash :one
SELECT * FROM user_session
WHERE refresh_token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW();

-- name: RevokeUserSession :one
UPDATE user_session SET revoked_at = NOW(), replaced_by_session_id = sqlc.narg('replaced_by_session_id')
WHERE id = sqlc.arg('id') AND revoked_at IS NULL
RETURNING *;

-- name: CreateWebAuthnCredential :one
INSERT INTO webauthn_credential (
    user_id, credential_id, public_key, attestation_type, transports,
    aaguid, sign_count, name
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: ListWebAuthnCredentialsForUser :many
SELECT * FROM webauthn_credential WHERE user_id = $1 ORDER BY id;

-- name: GetWebAuthnCredentialByCredentialID :one
SELECT * FROM webauthn_credential WHERE credential_id = $1;

-- name: UpdateWebAuthnCredentialSignCount :exec
UPDATE webauthn_credential SET sign_count = $2, last_used_at = NOW() WHERE id = $1;
