-- name: ChatSummaryModel :one
SELECT sqlc.embed(models), sqlc.embed(providers)
FROM chats
JOIN models ON models.id = chats.summary_model_id
JOIN providers ON providers.id = models.provider_id
WHERE chats.id = ?;
