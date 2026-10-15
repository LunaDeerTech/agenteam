-- agenteam:transaction tx
-- +goose Up
-- Startup refresh borrows the original guarded startup request. The nullable
-- column preserves historical Human/Agent rows; it is never a public grant.
ALTER TABLE agenteam_mcp.oauth_requests
 ADD COLUMN startup_request_id agenteam_mcp.safe_id
 REFERENCES agenteam_mcp.startup_requests(id) ON DELETE RESTRICT;

-- Locate the original caller-union check by its exact referenced columns,
-- not PostgreSQL's generated name or its ordering among anonymous checks.
-- A changed schema must reject this migration rather than drop another gate.
-- +goose StatementBegin
DO $$
DECLARE caller_check name;
BEGIN
 SELECT c.conname INTO STRICT caller_check
 FROM pg_constraint c
 WHERE c.conrelid='agenteam_mcp.oauth_requests'::regclass AND c.contype='c'
   AND (SELECT array_agg(a.attname ORDER BY a.attname)
        FROM pg_attribute a
        WHERE a.attrelid=c.conrelid AND a.attnum=ANY(c.conkey))
       = ARRAY['attempt_id','backend_request_id','command_digest','execution_id',
               'operation_id','runtime_binding_id','session_id','user_id']::name[];
 EXECUTE format('ALTER TABLE agenteam_mcp.oauth_requests DROP CONSTRAINT %I', caller_check);
END;
$$;
-- +goose StatementEnd

ALTER TABLE agenteam_mcp.oauth_requests
 ADD CONSTRAINT mcp_oauth_original_caller CHECK(
   (startup_request_id IS NULL
    AND user_id IS NOT NULL AND session_id IS NOT NULL AND command_digest IS NOT NULL
    AND execution_id IS NULL AND operation_id IS NULL AND attempt_id IS NULL
    AND backend_request_id IS NULL AND runtime_binding_id IS NULL)
   OR
   (startup_request_id IS NULL
    AND user_id IS NULL AND session_id IS NULL AND command_digest IS NULL
    AND execution_id IS NOT NULL AND operation_id IS NOT NULL AND attempt_id IS NOT NULL
    AND backend_request_id IS NOT NULL AND runtime_binding_id IS NOT NULL)
   OR
   (startup_request_id IS NOT NULL AND operation='refresh' AND flow_id IS NULL
    AND user_id IS NULL AND session_id IS NULL AND command_digest IS NULL
    AND execution_id IS NULL AND operation_id IS NULL AND attempt_id IS NULL
    AND backend_request_id IS NULL AND runtime_binding_id IS NULL)
 );

-- A startup request can issue at most one refresh. The existing credential
-- index still excludes any concurrent Human/Agent/startup refresh attempt.
CREATE UNIQUE INDEX mcp_oauth_startup_single_refresh
 ON agenteam_mcp.oauth_requests(startup_request_id)
 WHERE startup_request_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION agenteam_mcp.guard_oauth_request_origin() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NOT NEW.process_required OR NEW.process_id IS NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth request process required';
  END IF;
 ELSIF ROW(NEW.id,NEW.operation,NEW.project_id,NEW.config_id,NEW.config_revision,
           NEW.connection_id,NEW.connection_version,NEW.flow_id,NEW.command_digest,
           NEW.user_id,NEW.session_id,NEW.execution_id,NEW.operation_id,NEW.attempt_id,
           NEW.backend_request_id,NEW.runtime_binding_id,NEW.credential_id,
           NEW.credential_version,NEW.credential_lease_id,NEW.created_at,
           NEW.process_id,NEW.process_required,NEW.startup_request_id)
       IS DISTINCT FROM
       ROW(OLD.id,OLD.operation,OLD.project_id,OLD.config_id,OLD.config_revision,
           OLD.connection_id,OLD.connection_version,OLD.flow_id,OLD.command_digest,
           OLD.user_id,OLD.session_id,OLD.execution_id,OLD.operation_id,OLD.attempt_id,
           OLD.backend_request_id,OLD.runtime_binding_id,OLD.credential_id,
           OLD.credential_version,OLD.credential_lease_id,OLD.created_at,
           OLD.process_id,OLD.process_required,OLD.startup_request_id) THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth request origin immutable';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
