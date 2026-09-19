-- +goose Up
-- Providers are personal: sharing a provider with every group was dropped.
ALTER TABLE providers DROP COLUMN shared;

-- +goose Down
ALTER TABLE providers ADD COLUMN shared BOOLEAN NOT NULL DEFAULT FALSE;
