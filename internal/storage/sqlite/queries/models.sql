-- name: ChatSummaryModel :one
SELECT sqlc.embed(models), sqlc.embed(providers)
FROM chats
JOIN models ON models.id = chats.summary_model_id
JOIN providers ON providers.id = models.provider_id
WHERE chats.id = ?;

-- name: ChatChatModel :one
-- The model a chat talks with: its chat model, or else its summary model.
SELECT sqlc.embed(models), sqlc.embed(providers)
FROM chats
JOIN models ON models.id = coalesce(chats.chat_model_id, chats.summary_model_id)
JOIN providers ON providers.id = models.provider_id
WHERE chats.id = ?;

-- name: ModelWithProvider :one
SELECT sqlc.embed(models), sqlc.embed(providers)
FROM models
JOIN providers ON providers.id = models.provider_id
WHERE models.id = ?;

-- name: InsertModel :one
INSERT INTO models (provider_id, model_name, display_name, params_json)
VALUES (?, ?, ?, ?)
RETURNING id;

-- name: UpdateModel :exec
UPDATE models SET model_name = ?, display_name = ?, params_json = ? WHERE id = ?;

-- name: DeleteModel :exec
DELETE FROM models WHERE id = ?;

-- name: ModelsByOwner :many
SELECT models.* FROM models
JOIN providers ON providers.id = models.provider_id
WHERE providers.owner_user_id = ?
ORDER BY models.id;

-- name: GetModel :one
SELECT * FROM models WHERE id = ?;

-- name: ModelOptionByID :one
SELECT sqlc.embed(models), providers.name AS provider_name, providers.kind AS provider_kind,
       providers.owner_user_id, providers.shared, coalesce(users.username, '') AS owner_username,
       coalesce(users.first_name, '') AS owner_first_name
FROM models
JOIN providers ON providers.id = models.provider_id
LEFT JOIN users ON users.id = providers.owner_user_id
WHERE models.id = ?;

-- name: UsableModels :many
-- Models the user may bind to a chat: their own and shared ones.
SELECT sqlc.embed(models), providers.name AS provider_name, providers.kind AS provider_kind,
       providers.owner_user_id, providers.shared, coalesce(users.username, '') AS owner_username,
       coalesce(users.first_name, '') AS owner_first_name
FROM models
JOIN providers ON providers.id = models.provider_id
LEFT JOIN users ON users.id = providers.owner_user_id
WHERE providers.owner_user_id = @user_id OR providers.shared
ORDER BY providers.shared, models.id;
