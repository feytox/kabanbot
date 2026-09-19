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
SET enabled = ?, settings_json = ?, summary_model_id = ?, chat_model_id = ?, updated_by = ?, updated_at = unixepoch()
WHERE id = ?;

-- name: ChatsByModel :many
-- Chats that use the model for summaries or for chatting.
SELECT * FROM chats WHERE summary_model_id = @model_id OR chat_model_id = @model_id ORDER BY title;

-- name: UnbindModel :exec
UPDATE chats
SET summary_model_id = CASE WHEN summary_model_id = @model_id THEN NULL ELSE summary_model_id END,
    chat_model_id = CASE WHEN chat_model_id = @model_id THEN NULL ELSE chat_model_id END,
    updated_at = unixepoch()
WHERE id = @chat_id;

-- name: SetPersonality :exec
UPDATE chats SET personality = ?, updated_by = ?, updated_at = unixepoch() WHERE id = ?;

-- name: SetSummaryStyle :exec
UPDATE chats SET summary_style = ?, updated_by = ?, updated_at = unixepoch() WHERE id = ?;

-- name: InsertPersonalityChange :exec
INSERT INTO personality_history (chat_id, text, changed_by, via) VALUES (?, ?, ?, ?);

-- name: PersonalityHistory :many
SELECT personality_history.*, coalesce(users.username, '') AS username, coalesce(users.first_name, '') AS first_name
FROM personality_history
LEFT JOIN users ON users.id = personality_history.changed_by
WHERE chat_id = ?
ORDER BY personality_history.id DESC
LIMIT ?;

-- name: GetPersonalityChange :one
SELECT * FROM personality_history WHERE id = ?;
