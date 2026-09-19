-- +goose Up
CREATE TABLE messages_v2 (
    chat_id INTEGER NOT NULL,
    message_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    username TEXT NOT NULL,
    text TEXT NOT NULL,
    sent_at INTEGER NOT NULL,
    is_bot BOOLEAN NOT NULL DEFAULT FALSE,
    reply_to_username TEXT,
    reply_to_text TEXT,
    PRIMARY KEY (chat_id, message_id)
) WITHOUT ROWID;

INSERT OR IGNORE INTO messages_v2 (chat_id, message_id, user_id, username, text, sent_at, reply_to_username, reply_to_text)
SELECT chat_id, message_id, coalesce(user_id, 0), coalesce(username, 'Unknown'), coalesce(text, ''),
       cast(coalesce(timestamp, 0) AS INTEGER), reply_to_username, reply_to_text
FROM messages
WHERE chat_id IS NOT NULL AND message_id IS NOT NULL;

DROP TABLE messages;
ALTER TABLE messages_v2 RENAME TO messages;

-- +goose Down
DROP TABLE messages;
CREATE TABLE messages (
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
