-- +goose Up
-- Phase 4: chatting with the bot, per-chat personality and usage accounting.
ALTER TABLE chats ADD COLUMN summary_style TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_chats_chat_model ON chats (chat_model_id);

CREATE TABLE personality_history (
    id INTEGER PRIMARY KEY,
    chat_id INTEGER NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    text TEXT NOT NULL,
    changed_by INTEGER NOT NULL,
    via TEXT NOT NULL CHECK (via IN ('menu', 'tool')),
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX idx_personality_history_chat ON personality_history (chat_id, id);

CREATE TABLE llm_usage (
    id INTEGER PRIMARY KEY,
    chat_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    model_id INTEGER REFERENCES models (id) ON DELETE SET NULL,
    kind TEXT NOT NULL CHECK (kind IN ('summary', 'chat')),
    tokens_in INTEGER NOT NULL,
    tokens_out INTEGER NOT NULL,
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX idx_llm_usage_chat ON llm_usage (chat_id, created_at);

-- +goose Down
DROP TABLE llm_usage;
DROP TABLE personality_history;
DROP INDEX idx_chats_chat_model;
ALTER TABLE chats DROP COLUMN summary_style;
