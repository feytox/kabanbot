-- +goose Up
ALTER TABLE chats ADD COLUMN member BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX idx_providers_owner ON providers (owner_user_id);
CREATE INDEX idx_models_provider ON models (provider_id);
CREATE INDEX idx_chats_summary_model ON chats (summary_model_id);

-- +goose Down
DROP INDEX idx_chats_summary_model;
DROP INDEX idx_models_provider;
DROP INDEX idx_providers_owner;
ALTER TABLE chats DROP COLUMN member;
