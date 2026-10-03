-- agenteam:transaction tx
-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector VERSION '0.8.1';
-- +goose StatementBegin
DO $$
BEGIN
  IF (SELECT extversion FROM pg_extension WHERE extname = 'vector') IS DISTINCT FROM '0.8.1' THEN
    RAISE EXCEPTION USING ERRCODE = 'P0001', MESSAGE = 'EXTENSION_VERSION_UNSUPPORTED';
  END IF;
END;
$$;
-- +goose StatementEnd
CREATE TABLE agenteam_meta.health_probe (
  probe_id uuid PRIMARY KEY,
  value bigint NOT NULL
);
