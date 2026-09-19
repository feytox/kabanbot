-- name: InsertProvider :one
INSERT INTO providers (owner_user_id, kind, name, base_url, api_key_enc, api_key_hint, shared)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: UpdateProvider :exec
UPDATE providers
SET name = ?, base_url = ?, shared = ?, updated_at = unixepoch()
WHERE id = ?;

-- name: UpdateProviderKey :exec
UPDATE providers
SET name = ?, base_url = ?, api_key_enc = ?, api_key_hint = ?, shared = ?, updated_at = unixepoch()
WHERE id = ?;

-- name: DeleteProvider :exec
DELETE FROM providers WHERE id = ?;

-- name: GetProvider :one
SELECT * FROM providers WHERE id = ?;

-- name: ProvidersByOwner :many
SELECT * FROM providers WHERE owner_user_id = ? ORDER BY id;
