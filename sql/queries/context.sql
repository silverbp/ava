-- name: CreateEntityContext :one
INSERT INTO entity_context (
    business_id, entity_type, entity_id, context_type, content, metadata,
    source, confidence, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: GetEntityContext :one
SELECT * FROM entity_context WHERE id = $1 AND deleted_at IS NULL;

-- name: ListEntityContextForEntity :many
SELECT * FROM entity_context
WHERE business_id = sqlc.arg('business_id')
    AND entity_type = sqlc.arg('entity_type')
    AND entity_id = sqlc.arg('entity_id')
    AND deleted_at IS NULL
    AND (sqlc.arg('include_superseded')::boolean OR superseded_by_id IS NULL)
ORDER BY created_at DESC;

-- name: SupersedeEntityContext :exec
UPDATE entity_context SET superseded_by_id = $2, updated_at = NOW()
WHERE id = $1;

-- name: CreateAttachment :one
INSERT INTO attachment (
    business_id, entity_type, entity_id, original_filename, storage_key,
    content_type, file_size_bytes, display_sequence, created_by_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachment WHERE id = $1 AND deleted_at IS NULL;

-- name: ListAttachmentsForEntity :many
SELECT * FROM attachment
WHERE business_id = $1 AND entity_type = $2 AND entity_id = $3 AND deleted_at IS NULL
ORDER BY display_sequence, id;

-- name: DeleteAttachment :one
UPDATE attachment SET deleted_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;
