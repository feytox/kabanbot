-- +goose Up
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL DEFAULT '',
    first_name TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE providers (
    id INTEGER PRIMARY KEY,
    owner_user_id INTEGER NOT NULL REFERENCES users (id),
    kind TEXT NOT NULL CHECK (kind IN ('openai', 'openrouter', 'gemini')),
    name TEXT NOT NULL,
    base_url TEXT NOT NULL DEFAULT '',
    api_key_enc BLOB NOT NULL,
    api_key_hint TEXT NOT NULL,
    shared BOOLEAN NOT NULL DEFAULT FALSE,
    created_at INTEGER NOT NULL DEFAULT (unixepoch()),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

CREATE TABLE models (
    id INTEGER PRIMARY KEY,
    provider_id INTEGER NOT NULL REFERENCES providers (id) ON DELETE CASCADE,
    model_name TEXT NOT NULL,
    display_name TEXT NOT NULL,
    params_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE chats (
    id INTEGER PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    summary_model_id INTEGER REFERENCES models (id) ON DELETE SET NULL,
    chat_model_id INTEGER REFERENCES models (id) ON DELETE SET NULL,
    personality TEXT NOT NULL DEFAULT '',
    settings_json TEXT NOT NULL DEFAULT '{}',
    updated_by INTEGER,
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

-- +goose Down
DROP TABLE chats;
DROP TABLE models;
DROP TABLE providers;
DROP TABLE users;
