-- name: UpsertUser :exec
INSERT INTO users (id, username, first_name) VALUES (?, ?, ?)
ON CONFLICT (id) DO UPDATE SET username = excluded.username, first_name = excluded.first_name;
