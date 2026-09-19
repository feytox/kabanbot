-- +goose Up
-- 00005 dropped sharing by mistake. Providers that were shared must be shared again by hand.
ALTER TABLE providers ADD COLUMN shared BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE providers DROP COLUMN shared;
