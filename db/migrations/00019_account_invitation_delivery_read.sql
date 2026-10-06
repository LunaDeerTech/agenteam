-- agenteam:transaction tx
-- +goose Up
CREATE INDEX account_invitation_delivery_link ON agenteam_account.delivery_intents(link_id,id) WHERE kind='invitation';
