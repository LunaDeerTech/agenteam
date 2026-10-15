-- agenteam:transaction tx
-- +goose Up
-- A recovery owner is separate from the immutable original OAuth process.
-- These rows record canonical ownership; only the application's private
-- ConfirmStopped proof can authorize acquisition or takeover.
CREATE TABLE agenteam_mcp.oauth_recovery_claims (
 id agenteam_mcp.safe_id PRIMARY KEY,
 request_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.oauth_requests(id) ON DELETE RESTRICT,
 original_process_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 process_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 previous_claim_id agenteam_mcp.safe_id REFERENCES agenteam_mcp.oauth_recovery_claims(id) ON DELETE RESTRICT,
 phase text NOT NULL DEFAULT 'claimed' CHECK(phase IN ('claimed','complete','superseded')),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz(6),
 CHECK(original_process_id<>process_id),
 CHECK(previous_claim_id IS NULL OR previous_claim_id<>id),
 CHECK((phase='claimed')=(completed_at IS NULL))
);
CREATE UNIQUE INDEX mcp_oauth_current_recovery ON agenteam_mcp.oauth_recovery_claims(request_id) WHERE phase='claimed';
CREATE UNIQUE INDEX mcp_oauth_recovery_successor ON agenteam_mcp.oauth_recovery_claims(previous_claim_id) WHERE previous_claim_id IS NOT NULL;

-- +goose StatementBegin
CREATE FUNCTION agenteam_mcp.guard_oauth_recovery_claim() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE original agenteam_mcp.oauth_requests%ROWTYPE;
        previous agenteam_mcp.oauth_recovery_claims%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth recovery origin retained';
 END IF;
 IF TG_OP='UPDATE' THEN
  IF ROW(NEW.id,NEW.request_id,NEW.original_process_id,NEW.process_id,NEW.previous_claim_id,NEW.created_at)
     IS DISTINCT FROM ROW(OLD.id,OLD.request_id,OLD.original_process_id,OLD.process_id,OLD.previous_claim_id,OLD.created_at)
     OR (OLD.phase<>'claimed' AND ROW(NEW.phase,NEW.completed_at) IS DISTINCT FROM ROW(OLD.phase,OLD.completed_at)) THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth recovery origin immutable';
  END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO original FROM agenteam_mcp.oauth_requests WHERE id=NEW.request_id;
 IF NOT FOUND OR NOT original.process_required OR original.process_id IS DISTINCT FROM NEW.original_process_id
    OR original.operation NOT IN ('begin','exchange') OR NEW.phase<>'claimed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth recovery original request mismatch';
 END IF;
 IF NEW.previous_claim_id IS NULL THEN
  IF EXISTS(SELECT 1 FROM agenteam_mcp.oauth_recovery_claims WHERE request_id=NEW.request_id) THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth recovery predecessor required';
  END IF;
 ELSE
  SELECT * INTO previous FROM agenteam_mcp.oauth_recovery_claims WHERE id=NEW.previous_claim_id;
  IF NOT FOUND OR previous.phase<>'superseded' OR previous.request_id<>NEW.request_id
     OR previous.original_process_id<>NEW.original_process_id OR previous.process_id=NEW.process_id THEN
   RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth recovery predecessor mismatch';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER oauth_recovery_claim_origin BEFORE INSERT OR UPDATE OR DELETE ON agenteam_mcp.oauth_recovery_claims
 FOR EACH ROW EXECUTE FUNCTION agenteam_mcp.guard_oauth_recovery_claim();

-- Exactly one immutable Delete identity is selected for a flow credential.
-- An existing original retirement is adopted; recovery never replaces its key
-- after an Unknown result. If none exists, the initial recovery claim may issue
-- one new retirement, recording its actual new process rather than impersonating
-- the stopped original process. No token, verifier, code or client secret occurs.
CREATE TABLE agenteam_mcp.oauth_flow_cleanups (
 flow_id agenteam_mcp.safe_id PRIMARY KEY,
 source_request_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.oauth_requests(id) ON DELETE RESTRICT,
 delete_request_id agenteam_mcp.safe_id NOT NULL UNIQUE REFERENCES agenteam_mcp.oauth_requests(id) ON DELETE RESTRICT,
 created_by_claim_id agenteam_mcp.safe_id NOT NULL REFERENCES agenteam_mcp.oauth_recovery_claims(id) ON DELETE RESTRICT,
 source_process_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 delete_process_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 project_id agenteam_mcp.safe_id NOT NULL,
 config_id agenteam_mcp.safe_id NOT NULL,
 config_revision bigint NOT NULL CHECK(config_revision>0),
 connection_id agenteam_mcp.safe_id NOT NULL,
 connection_version bigint NOT NULL CHECK(connection_version>0),
 command_digest text NOT NULL REFERENCES agenteam_mcp.commands(command_digest),
 user_id agenteam_mcp.safe_id NOT NULL,
 session_id agenteam_mcp.safe_id NOT NULL,
 credential_id agenteam_mcp.safe_id NOT NULL,
 credential_version bigint NOT NULL CHECK(credential_version>0),
 secret_write_binding text NOT NULL CHECK(secret_write_binding ~ '^sha256:[0-9a-f]{64}$'),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 CHECK(source_request_id<>delete_request_id)
);

-- +goose StatementBegin
CREATE FUNCTION agenteam_mcp.guard_oauth_flow_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source agenteam_mcp.oauth_requests%ROWTYPE;
        retirement agenteam_mcp.oauth_requests%ROWTYPE;
        claim agenteam_mcp.oauth_recovery_claims%ROWTYPE;
        source_credential text;
        source_version bigint;
BEGIN
 IF TG_OP<>'INSERT' THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth flow cleanup identity immutable';
 END IF;
 SELECT * INTO source FROM agenteam_mcp.oauth_requests WHERE id=NEW.source_request_id;
 IF NOT FOUND OR source.operation NOT IN ('begin','exchange') OR NOT source.process_required
    OR source.process_id IS DISTINCT FROM NEW.source_process_id OR NOT source.io_retired OR NOT source.lease_released THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth flow cleanup source mismatch';
 END IF;
 IF source.operation='begin' THEN
  source_credential:=source.result_credential_id;
  source_version:=source.result_credential_version;
 ELSE
  source_credential:=source.credential_id;
  source_version:=source.credential_version;
 END IF;
 IF ROW(NEW.flow_id,NEW.project_id,NEW.config_id,NEW.config_revision,NEW.connection_id,NEW.connection_version,
        NEW.command_digest,NEW.user_id,NEW.session_id,NEW.credential_id,NEW.credential_version)
    IS DISTINCT FROM ROW(source.flow_id,source.project_id,source.config_id,source.config_revision,source.connection_id,source.connection_version,
        source.command_digest,source.user_id,source.session_id,source_credential,source_version) THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth flow cleanup original tuple mismatch';
 END IF;
 SELECT * INTO claim FROM agenteam_mcp.oauth_recovery_claims WHERE id=NEW.created_by_claim_id;
 IF NOT FOUND OR claim.request_id<>source.id OR claim.original_process_id<>source.process_id OR claim.phase<>'claimed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth flow cleanup claim mismatch';
 END IF;
 SELECT * INTO retirement FROM agenteam_mcp.oauth_requests WHERE id=NEW.delete_request_id;
 IF NOT FOUND OR retirement.operation<>'retire' OR NOT retirement.process_required
    OR NOT retirement.io_retired OR NOT retirement.lease_released OR retirement.credential_lease_id IS NOT NULL
    OR retirement.process_id IS DISTINCT FROM NEW.delete_process_id
    OR retirement.process_id NOT IN (source.process_id,claim.process_id)
    OR retirement.secret_write_binding IS DISTINCT FROM NEW.secret_write_binding
    OR ROW(retirement.flow_id,retirement.project_id,retirement.config_id,retirement.config_revision,retirement.connection_id,retirement.connection_version,
           retirement.command_digest,retirement.user_id,retirement.session_id,retirement.credential_id,retirement.credential_version)
       IS DISTINCT FROM ROW(NEW.flow_id,NEW.project_id,NEW.config_id,NEW.config_revision,NEW.connection_id,NEW.connection_version,
           NEW.command_digest,NEW.user_id,NEW.session_id,NEW.credential_id,NEW.credential_version) THEN
  RAISE EXCEPTION USING ERRCODE='23514', MESSAGE='OAuth flow cleanup Delete mismatch';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER oauth_flow_cleanup_origin BEFORE INSERT OR UPDATE OR DELETE ON agenteam_mcp.oauth_flow_cleanups
 FOR EACH ROW EXECUTE FUNCTION agenteam_mcp.guard_oauth_flow_cleanup();
