-- agenteam:transaction tx
-- +goose Up
-- Nonempty preparation material only. Legacy empty Mount inputs keep their
-- original byte representation and acquire no new rows. These references do
-- not authorize Runner operations or claim that any directory exists.
CREATE TABLE agenteam_mount.execution_captures (
 execution_id agenteam_execution.safe_id PRIMARY KEY,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 agent_version bigint NOT NULL CHECK(agent_version>0),
 attempt_binding text NOT NULL CHECK(attempt_binding ~ '^sha256:[0-9a-f]{64}$'),
 capture_digest text NOT NULL CHECK(capture_digest='sha256:'||encode(sha256(material),'hex')),
 mount_count integer NOT NULL CHECK(mount_count BETWEEN 1 AND 128),
 material bytea NOT NULL CHECK(octet_length(material) BETWEEN 2 AND 8388608),
 FOREIGN KEY(execution_id,project_id,agent_id)
  REFERENCES agenteam_execution.executions(id,project_id,agent_id) ON DELETE RESTRICT,
 CHECK(jsonb_typeof(convert_from(material,'UTF8')::jsonb)='object')
);
CREATE INDEX mount_execution_capture_project
 ON agenteam_mount.execution_captures(project_id,execution_id);

CREATE TABLE agenteam_mount.execution_references (
 execution_id agenteam_execution.safe_id NOT NULL
  REFERENCES agenteam_mount.execution_captures(execution_id) ON DELETE RESTRICT,
 mount_id agenteam_mount.safe_id NOT NULL
  REFERENCES agenteam_mount.mounts(id) ON DELETE RESTRICT,
 workspace_id agenteam_mount.safe_id NOT NULL
  REFERENCES agenteam_mount.runner_workspaces(workspace_id) ON DELETE RESTRICT,
 runner_id agenteam_mount.safe_id NOT NULL,
 mount_version bigint NOT NULL CHECK(mount_version>0),
 runner_version bigint NOT NULL CHECK(runner_version>0),
 PRIMARY KEY(execution_id,mount_id),
 UNIQUE(execution_id,workspace_id)
);
CREATE INDEX mount_execution_definition_references
 ON agenteam_mount.execution_references(mount_id,execution_id);
CREATE INDEX mount_execution_workspace_references
 ON agenteam_mount.execution_references(workspace_id,execution_id);

-- No retirement contract is invented here. Agent configuration changes may
-- remove current allowlist references without deleting this original capture.
-- Outer transaction rollback is the only failure cleanup in this slice.
-- +goose StatementBegin
CREATE FUNCTION agenteam_mount.immutable_execution_capture() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_capture_immutable', MESSAGE='immutable mount execution capture';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER mount_execution_capture_immutable BEFORE UPDATE OR DELETE
 ON agenteam_mount.execution_captures FOR EACH ROW
 EXECUTE FUNCTION agenteam_mount.immutable_execution_capture();
CREATE TRIGGER mount_execution_reference_immutable BEFORE UPDATE OR DELETE
 ON agenteam_mount.execution_references FOR EACH ROW
 EXECUTE FUNCTION agenteam_mount.immutable_execution_capture();

-- Check the complete final same-Tx set, not merely one insert direction.
-- Execution IDs/scopes use its text domain; owned Mount identities use UUID.
-- Current definition checks run on initial insertion only. Historical reads
-- compare immutable material/references and never reinterpret later metadata.
-- +goose StatementBegin
CREATE FUNCTION agenteam_mount.check_execution_capture() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE head agenteam_mount.execution_captures%ROWTYPE;
 body jsonb; item jsonb; actual bigint; matching bigint; previous text;
BEGIN
 SELECT * INTO STRICT head FROM agenteam_mount.execution_captures WHERE execution_id=NEW.execution_id;
 SELECT count(*) INTO actual FROM agenteam_mount.execution_references WHERE execution_id=NEW.execution_id;
 IF actual<>head.mount_count THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_reference_set', MESSAGE='incomplete mount execution references';
 END IF;
 -- The mandatory head insert verifies the full immutable material once. Each
 -- reference insert still checks completeness, including attempts to append
 -- to an older capture, without reparsing the whole material up to 128 times.
 IF TG_TABLE_NAME='execution_references' THEN
  RETURN NEW;
 END IF;
 body := convert_from(head.material,'UTF8')::jsonb;
 IF (body ?& ARRAY['request','agent_version','mounts','attempt_binding']
  AND body-ARRAY['request','agent_version','mounts','attempt_binding']='{}'::jsonb
  AND jsonb_typeof(body->'request')='object'
  AND body->'request' ?& ARRAY['project_id','agent_id','execution_id']
  AND (body->'request')-ARRAY['project_id','agent_id','execution_id']='{}'::jsonb
  AND body->'request'->>'project_id'=head.project_id::text
  AND body->'request'->>'agent_id'=head.agent_id::text
  AND body->'request'->>'execution_id'=head.execution_id::text
  AND jsonb_typeof(body->'agent_version')='string'
  AND body->>'agent_version'=head.agent_version::text
  AND body->>'attempt_binding'=head.attempt_binding
  AND jsonb_typeof(body->'mounts')='array') IS NOT TRUE THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_material', MESSAGE='invalid mount execution material';
 END IF;
 IF jsonb_array_length(body->'mounts')<>head.mount_count THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_reference_set', MESSAGE='incomplete mount execution references';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads h
  WHERE h.project_id::text=head.project_id AND h.agent_id::text=head.agent_id
   AND h.owner_version=head.agent_version) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_current_head', MESSAGE='mount execution head mismatch';
 END IF;
 SELECT count(*) INTO actual FROM agenteam_mount.agent_mount_refs r
  WHERE r.project_id::text=head.project_id AND r.agent_id::text=head.agent_id;
 IF actual<>head.mount_count THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_current_refs', MESSAGE='mount execution current references mismatch';
 END IF;
 FOR item IN SELECT value FROM jsonb_array_elements(body->'mounts') LOOP
  IF (jsonb_typeof(item)='object'
   AND item ?& ARRAY['id','workspace_id','runner_id','runner_version','runner_name','runner_description','name','description','workspace','version']
   AND item-ARRAY['id','workspace_id','runner_id','runner_version','runner_name','runner_description','name','description','workspace','version']='{}'::jsonb
   AND jsonb_typeof(item->'version')='string'
   AND jsonb_typeof(item->'runner_version')='string'
   AND jsonb_typeof(item->'runner_name')='string'
   AND jsonb_typeof(item->'runner_description')='string'
   AND (previous IS NULL OR previous COLLATE "C" < (item->>'id') COLLATE "C")) IS NOT TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_material', MESSAGE='invalid mount execution metadata';
  END IF;
  SELECT count(*) INTO matching
   FROM agenteam_mount.execution_references r
   JOIN agenteam_mount.runner_workspaces w ON w.workspace_id=r.workspace_id AND w.mount_id=r.mount_id AND w.runner_id=r.runner_id
   JOIN agenteam_mount.mounts m ON m.id=r.mount_id AND m.project_id=w.project_id AND m.agent_id=w.agent_id AND m.runner_id=w.runner_id AND m.workspace=w.workspace
   JOIN agenteam_mount.agent_mount_refs a ON a.project_id=m.project_id AND a.agent_id=m.agent_id AND a.mount_id=m.id
   WHERE r.execution_id=NEW.execution_id
    AND r.mount_id::text=item->>'id' AND r.workspace_id::text=item->>'workspace_id'
    AND r.runner_id::text=item->>'runner_id' AND r.mount_version::text=item->>'version'
    AND r.runner_version::text=item->>'runner_version'
    AND m.project_id::text=head.project_id AND m.agent_id::text=head.agent_id
    AND m.lifecycle='active' AND m.version=r.mount_version
    AND m.name=item->>'name' AND m.description IS NOT DISTINCT FROM item->>'description'
    AND m.workspace=item->>'workspace';
  IF matching<>1 THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='mount_execution_reference_mapping', MESSAGE='mount execution mapping mismatch';
  END IF;
  previous:=item->>'id';
 END LOOP;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER mount_execution_capture_complete AFTER INSERT
 ON agenteam_mount.execution_captures DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_mount.check_execution_capture();
CREATE CONSTRAINT TRIGGER mount_execution_reference_complete AFTER INSERT
 ON agenteam_mount.execution_references DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION agenteam_mount.check_execution_capture();
