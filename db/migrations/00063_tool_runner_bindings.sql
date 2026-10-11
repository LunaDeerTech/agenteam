-- agenteam:transaction tx
-- +goose Up
-- Add one metadata-only Runner binding. Existing Builtin rows and immutable
-- definition/capture bytes remain unchanged; no Operation or runtime is enabled.
ALTER TABLE agenteam_tool.identities DROP CONSTRAINT identities_stable_key_check;
ALTER TABLE agenteam_tool.identities ADD CONSTRAINT tool_identity_stable_key
 CHECK(stable_key ~ '^builtin:[a-z0-9_.-]{1,128}$' OR stable_key='runner:read-file');

ALTER TABLE agenteam_tool.registrations
 ALTER COLUMN handler_id DROP NOT NULL,
 ALTER COLUMN contract_revision DROP NOT NULL,
 ADD COLUMN binding_kind text NOT NULL DEFAULT 'builtin',
 ADD COLUMN operation_name text,
 ADD COLUMN operation_revision text,
 ADD COLUMN protocol_major integer,
 ADD CONSTRAINT tool_registration_binding CHECK((
  (binding_kind='builtin' AND handler_id IS NOT NULL AND contract_revision IS NOT NULL
   AND operation_name IS NULL AND operation_revision IS NULL AND protocol_major IS NULL)
  OR (binding_kind='runner' AND handler_id IS NULL AND contract_revision IS NULL
   AND operation_name='read-file' AND operation_revision='1' AND protocol_major=1
   AND scope_resolver_id='runner.read-file' AND risk_classifier_id='runner.read-file' AND class='ordinary')
 ) IS TRUE);

-- +goose StatementBegin
CREATE FUNCTION agenteam_tool.check_runner_binding_identity() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE stable text;
BEGIN
 SELECT stable_key INTO STRICT stable FROM agenteam_tool.identities WHERE tool_id=NEW.tool_id;
 IF (NEW.binding_kind='runner' AND stable<>'runner:read-file')
 OR (NEW.binding_kind='builtin' AND stable!~'^builtin:[a-z0-9_.-]{1,128}$') THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='tool_binding_identity', MESSAGE='invalid Tool binding identity';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER tool_registration_binding_identity BEFORE INSERT OR UPDATE
 ON agenteam_tool.registrations FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_runner_binding_identity();

ALTER TABLE agenteam_tool.execution_references
 ALTER COLUMN handler_id DROP NOT NULL,
 ALTER COLUMN contract_revision DROP NOT NULL,
 ADD COLUMN binding_kind text NOT NULL DEFAULT 'builtin',
 ADD COLUMN operation_name text,
 ADD COLUMN operation_revision text,
 ADD COLUMN protocol_major integer,
 ADD CONSTRAINT tool_execution_reference_binding CHECK((
  (binding_kind='builtin' AND handler_id IS NOT NULL AND contract_revision IS NOT NULL
   AND operation_name IS NULL AND operation_revision IS NULL AND protocol_major IS NULL)
  OR (binding_kind='runner' AND handler_id IS NULL AND contract_revision IS NULL
   AND operation_name='read-file' AND operation_revision='1' AND protocol_major=1
   AND scope_resolver_id='runner.read-file' AND risk_classifier_id='runner.read-file' AND class='ordinary')
 ) IS TRUE);

CREATE TRIGGER tool_execution_binding_identity BEFORE INSERT
 ON agenteam_tool.execution_references FOR EACH ROW EXECUTE FUNCTION agenteam_tool.check_runner_binding_identity();
