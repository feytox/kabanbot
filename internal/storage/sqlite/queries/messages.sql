-- name: InsertMessage :exec
INSERT OR IGNORE INTO messages (chat_id, message_id, user_id, username, text, sent_at, is_bot, reply_to_username, reply_to_text)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: PruneBoundary :one
-- Returns the ID of the newest message that falls outside the retention window.
SELECT message_id FROM messages
WHERE chat_id = ?
ORDER BY message_id DESC
LIMIT 1 OFFSET ?;

-- name: DeleteMessagesUpTo :exec
DELETE FROM messages
WHERE chat_id = ? AND message_id <= ?;

-- name: MessagesSince :many
SELECT * FROM messages
WHERE chat_id = ? AND message_id >= ?
ORDER BY message_id;

-- name: RecentMessages :many
-- The newest messages of the chat, newest first.
SELECT * FROM messages
WHERE chat_id = ?
ORDER BY message_id DESC
LIMIT ?;

-- name: ChatUsers :many
-- Every human who wrote in the chat. SQLite takes the bare username column
-- from the row holding max(message_id), i.e. the latest known name.
SELECT user_id, username, max(message_id) AS last_message_id
FROM messages
WHERE chat_id = ? AND user_id != 0 AND NOT is_bot
GROUP BY user_id;
