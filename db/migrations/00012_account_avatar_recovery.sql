-- agenteam:transaction tx
-- +goose Up
CREATE INDEX account_avatar_changes_fair ON agenteam_account.avatar_changes(pass,id)
WHERE phase IN ('preparing','published','cancelled','cleanup_pending');
