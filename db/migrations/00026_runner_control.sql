-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_runner;
CREATE DOMAIN agenteam_runner.safe_id AS uuid
 CONSTRAINT runner_safe_id CHECK (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

-- +goose StatementBegin
CREATE FUNCTION agenteam_runner.valid_strings(value jsonb, max_items integer, max_bytes integer) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN jsonb_typeof(value)='array' THEN
  jsonb_array_length(value)<=max_items
  AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(value) item
   WHERE jsonb_typeof(item)<>'string' OR octet_length(item#>>'{}') NOT BETWEEN 1 AND max_bytes
    OR item#>>'{}' ~ '[[:cntrl:]]')
  AND value=coalesce((SELECT jsonb_agg(item ORDER BY item COLLATE "C")
   FROM (SELECT DISTINCT item#>>'{}' AS item FROM jsonb_array_elements(value) item) items),'[]'::jsonb)
 ELSE false END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION agenteam_runner.valid_version(value jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE WHEN jsonb_typeof(value)='string' AND value#>>'{}' ~ '^[1-9][0-9]{0,18}$'
 THEN (value#>>'{}')::numeric<=9223372036854775807 ELSE false END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION agenteam_runner.valid_changed_fields(action text, fields jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
 SELECT CASE
 WHEN action='runner.create' THEN fields='["created"]'::jsonb
 WHEN action IN ('runner.enrollment.issue','runner.enroll','runner.revoke') THEN fields='["credential"]'::jsonb
 WHEN action='runner.update' AND agenteam_runner.valid_strings(fields,3,32) THEN
  jsonb_array_length(fields)>=1
  AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text(fields) field WHERE field NOT IN ('description','name','tags'))
 ELSE false END
$$;
-- +goose StatementEnd

CREATE TABLE agenteam_runner.runners (
 id agenteam_runner.safe_id PRIMARY KEY,
 name text NOT NULL CHECK(octet_length(name) BETWEEN 1 AND 128 AND name !~ '[[:cntrl:]]'),
 description text NOT NULL CHECK(octet_length(description)<=4096 AND description !~ '[[:cntrl:]]'),
 tags jsonb NOT NULL CHECK(agenteam_runner.valid_strings(tags,32,64) IS TRUE),
 root_path text NOT NULL CHECK(octet_length(root_path) BETWEEN 1 AND 4096
  AND left(root_path,1)='/' AND position(chr(92) IN root_path)=0 AND position('//' IN root_path)=0
  AND root_path !~ '(^|/)\.{1,2}(/|$)' AND (root_path='/' OR right(root_path,1)<>'/')),
 version bigint NOT NULL CHECK(version>=1),
 credential_generation bigint NOT NULL CHECK(credential_generation>=1),
 connection_generation bigint NOT NULL DEFAULT 0 CHECK(connection_generation>=0),
 device_public_key bytea CHECK(device_public_key IS NULL OR octet_length(device_public_key)=32),
 enrolled_at timestamptz(6),
 last_seen_at timestamptz(6),
 last_hello jsonb CHECK(last_hello IS NULL OR (jsonb_typeof(last_hello)='object' AND octet_length(last_hello::text)<=16384)),
 incompatible boolean NOT NULL DEFAULT false,
 created_at timestamptz(6) NOT NULL CHECK(isfinite(created_at)),
 updated_at timestamptz(6) NOT NULL CHECK(isfinite(updated_at) AND updated_at>=created_at),
 CONSTRAINT runners_generation_key UNIQUE(id,credential_generation,connection_generation),
 CONSTRAINT runners_key_shape CHECK((device_public_key IS NULL AND enrolled_at IS NULL)
  OR (device_public_key IS NOT NULL AND enrolled_at IS NOT NULL AND isfinite(enrolled_at) AND enrolled_at>=created_at)),
 CONSTRAINT runners_seen_shape CHECK(last_seen_at IS NULL OR (isfinite(last_seen_at) AND last_seen_at>=created_at))
);

-- Metadata/credential versions are independent of connection heartbeats. A
-- consumed generation is never reset even when its current connection retires.
-- +goose StatementBegin
CREATE FUNCTION agenteam_runner.reject_runner_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE business_changed boolean;
BEGIN
 business_changed:=ROW(NEW.name,NEW.description,NEW.tags,NEW.credential_generation,NEW.device_public_key,NEW.enrolled_at)
  IS DISTINCT FROM ROW(OLD.name,OLD.description,OLD.tags,OLD.credential_generation,OLD.device_public_key,OLD.enrolled_at);
 IF ROW(NEW.id,NEW.root_path,NEW.created_at) IS DISTINCT FROM ROW(OLD.id,OLD.root_path,OLD.created_at)
 OR NEW.version::numeric-OLD.version::numeric NOT BETWEEN 0 AND 1
 OR NEW.credential_generation::numeric-OLD.credential_generation::numeric NOT BETWEEN 0 AND 1
 OR NEW.connection_generation::numeric-OLD.connection_generation::numeric NOT BETWEEN 0 AND 1
 OR (business_changed AND NEW.version::numeric-OLD.version::numeric<>1)
 OR (NOT business_changed AND (NEW.version<>OLD.version OR NEW.updated_at<>OLD.updated_at))
 OR NEW.updated_at<OLD.updated_at
 OR (NEW.credential_generation<>OLD.credential_generation AND NEW.device_public_key IS NOT NULL)
 OR (OLD.device_public_key IS NOT NULL AND NEW.device_public_key IS NULL AND NEW.credential_generation=OLD.credential_generation)
 OR (OLD.device_public_key IS NOT NULL AND NEW.device_public_key IS NOT NULL AND NEW.device_public_key<>OLD.device_public_key)
 OR (OLD.device_public_key IS NOT NULL AND NEW.device_public_key=OLD.device_public_key AND NEW.enrolled_at IS DISTINCT FROM OLD.enrolled_at)
 THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='runners_immutable', MESSAGE='immutable runner identity';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER runners_immutable BEFORE UPDATE ON agenteam_runner.runners
 FOR EACH ROW EXECUTE FUNCTION agenteam_runner.reject_runner_rewrite();

-- A management command and its public receipt are inserted in the same final
-- transaction as its facts and Audit. No secret is recoverable from a receipt.
CREATE TABLE agenteam_runner.commands (
 id agenteam_runner.safe_id PRIMARY KEY,
 runner_id agenteam_runner.safe_id NOT NULL REFERENCES agenteam_runner.runners(id) ON DELETE RESTRICT,
 actor_user_id agenteam_runner.safe_id NOT NULL,
 actor_session_id agenteam_runner.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name IN ('runner.create','runner.update','runner.enrollment.issue','runner.revoke')),
 idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 128 AND idempotency_key ~ '^[A-Za-z0-9._:/-]+$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 request jsonb NOT NULL CHECK(jsonb_typeof(request)='object' AND octet_length(request::text)<=65536),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=65536),
 created_at timestamptz(6) NOT NULL CHECK(isfinite(created_at)),
 committed_at timestamptz(6) NOT NULL CHECK(isfinite(committed_at) AND committed_at>=created_at),
 CONSTRAINT runner_commands_runner_key UNIQUE(runner_id,id),
 CONSTRAINT runner_commands_identity_key UNIQUE(actor_user_id,runner_id,command_name,idempotency_key)
);

CREATE TABLE agenteam_runner.enrollment_tokens (
 token_hash bytea PRIMARY KEY CHECK(octet_length(token_hash)=32),
 runner_id agenteam_runner.safe_id NOT NULL REFERENCES agenteam_runner.runners(id) ON DELETE RESTRICT,
 credential_generation bigint NOT NULL CHECK(credential_generation>=1),
 issued_command_id agenteam_runner.safe_id NOT NULL UNIQUE,
 issued_at timestamptz(6) NOT NULL CHECK(isfinite(issued_at)),
 expires_at timestamptz(6) NOT NULL CHECK(isfinite(expires_at) AND expires_at=issued_at+interval '10 minutes'),
 consumed_at timestamptz(6) CHECK(consumed_at IS NULL OR (isfinite(consumed_at) AND consumed_at>=issued_at AND consumed_at<expires_at)),
 revoked_at timestamptz(6) CHECK(revoked_at IS NULL OR (isfinite(revoked_at) AND revoked_at>=issued_at)),
 CONSTRAINT enrollment_tokens_command_fk FOREIGN KEY(runner_id,issued_command_id) REFERENCES agenteam_runner.commands(runner_id,id) ON DELETE RESTRICT
);
CREATE INDEX enrollment_tokens_live ON agenteam_runner.enrollment_tokens(runner_id,credential_generation)
 WHERE consumed_at IS NULL AND revoked_at IS NULL;

CREATE TABLE agenteam_runner.identity_events (
 id agenteam_runner.safe_id PRIMARY KEY,
 runner_id agenteam_runner.safe_id NOT NULL REFERENCES agenteam_runner.runners(id) ON DELETE RESTRICT,
 kind text NOT NULL CHECK(kind IN ('enrollment_issued','enrolled','revoked')),
 credential_generation bigint NOT NULL CHECK(credential_generation>=1),
 version bigint NOT NULL CHECK(version>=1),
 command_id agenteam_runner.safe_id,
 public_key_fingerprint text CHECK(public_key_fingerprint IS NULL OR public_key_fingerprint ~ '^sha256:[0-9a-f]{64}$'),
 occurred_at timestamptz(6) NOT NULL CHECK(isfinite(occurred_at)),
 CONSTRAINT identity_events_runner_key UNIQUE(runner_id,id),
 CONSTRAINT identity_events_generation_kind_key UNIQUE(runner_id,credential_generation,kind),
 CONSTRAINT identity_events_command_fk FOREIGN KEY(runner_id,command_id) REFERENCES agenteam_runner.commands(runner_id,id) ON DELETE RESTRICT,
 CONSTRAINT identity_events_kind_shape CHECK(
  (kind='enrolled' AND command_id IS NULL AND public_key_fingerprint IS NOT NULL AND version>=2)
  OR (kind='enrollment_issued' AND command_id IS NOT NULL AND public_key_fingerprint IS NULL)
  OR (kind='revoked' AND command_id IS NOT NULL AND version>=2))
);

CREATE TABLE agenteam_runner.challenges (
 nonce_hash bytea PRIMARY KEY CHECK(octet_length(nonce_hash)=32),
 runner_id agenteam_runner.safe_id NOT NULL REFERENCES agenteam_runner.runners(id) ON DELETE RESTRICT,
 credential_generation bigint NOT NULL CHECK(credential_generation>=1),
 issued_at timestamptz(6) NOT NULL CHECK(isfinite(issued_at)),
 expires_at timestamptz(6) NOT NULL CHECK(isfinite(expires_at) AND expires_at=issued_at+interval '30 seconds'),
 consumed_at timestamptz(6) CHECK(consumed_at IS NULL OR (isfinite(consumed_at) AND consumed_at>=issued_at AND consumed_at<expires_at)),
 revoked_at timestamptz(6) CHECK(revoked_at IS NULL OR (isfinite(revoked_at) AND revoked_at>=issued_at))
);
CREATE INDEX challenges_live ON agenteam_runner.challenges(runner_id,expires_at)
 WHERE consumed_at IS NULL AND revoked_at IS NULL;
CREATE INDEX challenges_expiry ON agenteam_runner.challenges(expires_at,nonce_hash);

-- One current row per Runner; the durable monotonic counter lives on runners.
-- Deferred references allow revoke/replacement to change both rows atomically.
CREATE TABLE agenteam_runner.connections (
 runner_id agenteam_runner.safe_id PRIMARY KEY,
 id agenteam_runner.safe_id NOT NULL UNIQUE,
 credential_generation bigint NOT NULL CHECK(credential_generation>=1),
 generation bigint NOT NULL CHECK(generation>=1),
 owner_id agenteam_runner.safe_id NOT NULL,
 authenticated_at timestamptz(6) NOT NULL CHECK(isfinite(authenticated_at)),
 hello_at timestamptz(6) CHECK(hello_at IS NULL OR (isfinite(hello_at) AND hello_at>=authenticated_at)),
 last_seen_at timestamptz(6) NOT NULL CHECK(isfinite(last_seen_at) AND last_seen_at>=authenticated_at),
 lease_expires_at timestamptz(6) NOT NULL CHECK(isfinite(lease_expires_at) AND lease_expires_at>last_seen_at),
 heartbeat_sequence numeric NOT NULL DEFAULT 0 CHECK(heartbeat_sequence=trunc(heartbeat_sequence) AND heartbeat_sequence BETWEEN 0 AND 18446744073709551615),
 CONSTRAINT connections_current_generation_fk FOREIGN KEY(runner_id,credential_generation,generation)
  REFERENCES agenteam_runner.runners(id,credential_generation,connection_generation)
  ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX connections_owner ON agenteam_runner.connections(owner_id,runner_id);

-- Consumption/revocation cannot be undone. Immutable identity and expiry bind
-- each hash to exactly one Runner generation, even after its cleanup becomes due.
-- +goose StatementBegin
CREATE FUNCTION agenteam_runner.reject_credential_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (to_jsonb(NEW)-ARRAY['consumed_at','revoked_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['consumed_at','revoked_at'])
 OR (OLD.consumed_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at)
 OR (OLD.revoked_at IS NOT NULL AND NEW.revoked_at IS DISTINCT FROM OLD.revoked_at)
 OR (OLD.revoked_at IS NOT NULL AND NEW.consumed_at IS DISTINCT FROM OLD.consumed_at)
 THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='runner_credential_immutable', MESSAGE='immutable runner credential';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER enrollment_tokens_immutable BEFORE UPDATE ON agenteam_runner.enrollment_tokens
 FOR EACH ROW EXECUTE FUNCTION agenteam_runner.reject_credential_rewrite();
CREATE TRIGGER challenges_immutable BEFORE UPDATE ON agenteam_runner.challenges
 FOR EACH ROW EXECUTE FUNCTION agenteam_runner.reject_credential_rewrite();

-- +goose StatementBegin
CREATE FUNCTION agenteam_runner.reject_history_rewrite() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='runner_history_immutable', MESSAGE='immutable runner history';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER runner_commands_immutable BEFORE UPDATE OR DELETE ON agenteam_runner.commands
 FOR EACH ROW EXECUTE FUNCTION agenteam_runner.reject_history_rewrite();
CREATE TRIGGER identity_events_immutable BEFORE UPDATE OR DELETE ON agenteam_runner.identity_events
 FOR EACH ROW EXECUTE FUNCTION agenteam_runner.reject_history_rewrite();

-- Preserve the actual 00024/00025 predecessor predicates, including each
-- domain's independent fact constraint. No historical CHECK is reconstructed.
-- +goose StatementBegin
DO $$
DECLARE item record; expression text;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('audit_records_action_check','action IN (''runner.create'',''runner.update'',''runner.enrollment.issue'',''runner.enroll'',''runner.revoke'')'),
  ('audit_records_resource_kind_check','resource_kind=''runner'''),
  ('audit_records_producer_check','producer=''runner'''),
  ('audit_records_check2','resource_kind=''runner'' AND resource_id IS NOT NULL'),
  ('audit_records_check1','actor_kind=''service'' AND user_id IS NULL AND session_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND actor_project_id IS NULL AND project_id IS NULL AND scope=''system'' AND service_name=''runner-identity'' AND service_cause IS NOT NULL AND service_cause=cause_ref AND producer=''runner'' AND action=''runner.enroll''')
 ) AS additions(name,extra) LOOP
  SELECT pg_get_expr(conbin,conrelid) INTO STRICT expression FROM pg_constraint
   WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname=item.name AND contype='c';
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT %I',item.name);
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT %I CHECK ((%s) OR (%s))',item.name,expression,item.extra);
 END LOOP;
END;
$$;
-- +goose StatementEnd

ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_runner_contract CHECK ((
 (producer<>'runner' AND action NOT IN ('runner.create','runner.update','runner.enrollment.issue','runner.enroll','runner.revoke')
  AND resource_kind<>'runner' AND service_name IS DISTINCT FROM 'runner-identity')
 OR (producer='runner' AND scope='system' AND project_id IS NULL AND outcome='success'
  AND resource_kind='runner' AND resource_id IS NOT NULL AND runner_id=resource_id
  AND cause_ref ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND ((actor_kind='human' AND action IN ('runner.create','runner.update','runner.enrollment.issue','runner.revoke'))
   OR (actor_kind='service' AND service_name='runner-identity' AND action='runner.enroll' AND service_cause=cause_ref))
  AND ((action='runner.enrollment.issue' AND ordinal IN (0,1)) OR (action<>'runner.enrollment.issue' AND ordinal=0))
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL AND request_id IS NULL
  AND approval_id IS NULL AND correlation_id IS NULL AND http_trace_id IS NULL
  AND metadata ?& ARRAY['runner_id','version','credential_generation','changed_fields']
  AND metadata-ARRAY['runner_id','version','credential_generation','changed_fields','public_key_fingerprint']='{}'::jsonb
  AND jsonb_typeof(metadata->'runner_id')='string' AND metadata->>'runner_id'=resource_id::text
  AND agenteam_runner.valid_version(metadata->'version') AND agenteam_runner.valid_version(metadata->'credential_generation')
  AND agenteam_runner.valid_changed_fields(action,metadata->'changed_fields')
  AND ((action='runner.create' AND metadata->>'version'='1' AND metadata->>'credential_generation'='1')
   OR (action<>'runner.create' AND metadata->>'version'<>'1'))
  AND ((action='runner.enroll' AND metadata ? 'public_key_fingerprint')
   OR (action='runner.revoke') OR (action NOT IN ('runner.enroll','runner.revoke') AND NOT metadata ? 'public_key_fingerprint'))
  AND (NOT metadata ? 'public_key_fingerprint' OR (jsonb_typeof(metadata->'public_key_fingerprint')='string'
   AND metadata->>'public_key_fingerprint' ~ '^sha256:[0-9a-f]{64}$')))
 ) IS TRUE);
