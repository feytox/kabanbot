-- Schema of the Python version, kept so its databases upgrade in place.

-- +goose Up
CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    chat_id INTEGER,
    message_id INTEGER,
    user_id INTEGER,
    username TEXT,
    text TEXT,
    timestamp REAL,
    reply_to_text TEXT,
    reply_to_username TEXT
);

-- +goose Down
DROP TABLE messages;
