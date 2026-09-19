-- name: InsertUsage :exec
INSERT INTO llm_usage (chat_id, user_id, model_id, kind, tokens_in, tokens_out) VALUES (?, ?, ?, ?, ?, ?);

-- name: UsageTotals :one
SELECT count(*) AS requests,
       CAST(coalesce(sum(tokens_in), 0) AS INTEGER) AS tokens_in,
       CAST(coalesce(sum(tokens_out), 0) AS INTEGER) AS tokens_out
FROM llm_usage
WHERE chat_id = ? AND created_at >= ?;

-- name: UsageByUser :many
SELECT llm_usage.user_id, coalesce(users.username, '') AS username, coalesce(users.first_name, '') AS first_name,
       count(*) AS requests, CAST(sum(tokens_in + tokens_out) AS INTEGER) AS tokens
FROM llm_usage
LEFT JOIN users ON users.id = llm_usage.user_id
WHERE llm_usage.chat_id = ? AND llm_usage.created_at >= ?
GROUP BY llm_usage.user_id
ORDER BY requests DESC
LIMIT ?;
