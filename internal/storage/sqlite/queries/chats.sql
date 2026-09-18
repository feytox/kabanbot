-- name: TouchChat :exec
-- Records that the bot is (or is no longer) in the chat.
INSERT INTO chats (id, title, member) VALUES (?, ?, ?)
ON CONFLICT (id) DO UPDATE SET title = excluded.title, member = excluded.member;

-- name: GetChat :one
SELECT * FROM chats WHERE id = ?;

-- name: MemberChats :many
SELECT * FROM chats WHERE member ORDER BY title;

-- name: UpdateChatSettings :exec
UPDATE chats
SET enabled = ?, settings_json = ?, summary_model_id = ?, updated_by = ?, updated_at = unixepoch()
WHERE id = ?;

-- name: ChatsBySummaryModel :many
SELECT * FROM chats WHERE summary_model_id = ? ORDER BY title;

-- name: UnbindSummaryModel :exec
UPDATE chats SET summary_model_id = NULL, updated_at = unixepoch()
WHERE id = ? AND summary_model_id = ?;
