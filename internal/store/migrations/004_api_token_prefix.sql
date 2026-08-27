-- Add prefix column to api_token for display in the Settings UI.
-- The prefix is the first 12 characters of the token, stored at creation time.
-- Tokens created before this migration show an em-dash in the UI.

ALTER TABLE api_token ADD COLUMN prefix TEXT NOT NULL DEFAULT '';
