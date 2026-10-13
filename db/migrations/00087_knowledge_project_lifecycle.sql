-- agenteam:transaction tx
-- +goose Up
-- Independent Knowledge ownership. A closed row records its technical phase,
-- never proof of the original Store/call return, a business commit or permission.
CREATE TABLE agenteam_knowledge.project_calls (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 source_project_id agenteam_knowledge.safe_id REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 process_id agenteam_knowledge.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id) ON DELETE RESTRICT,
 instance_id agenteam_knowledge.safe_id NOT NULL,
 lane text NOT NULL CHECK(lane IN ('read','command','source','recovery')),
 identity jsonb NOT NULL CHECK(jsonb_typeof(identity)='object' AND octet_length(identity::text)<=8192),
 identity_digest text NOT NULL CHECK(identity_digest ~ '^sha256:[0-9a-f]{64}$'),
 phase text NOT NULL CHECK(phase IN ('running','business_closed')),
 registered_at timestamptz(6) NOT NULL,
 closed_at timestamptz(6),
 CONSTRAINT knowledge_project_calls_time CHECK((phase='running' AND closed_at IS NULL) OR (phase='business_closed' AND closed_at IS NOT NULL AND closed_at>=registered_at))
);
CREATE INDEX knowledge_project_calls_target ON agenteam_knowledge.project_calls(project_id,id);
CREATE INDEX knowledge_project_calls_source ON agenteam_knowledge.project_calls(source_project_id,id) WHERE source_project_id IS NOT NULL;

-- Not a Knowledge command or a replacement for an Object cleanup record.
-- There is deliberately no FK to the Object anchors: the original Object
-- owner purges them before completing and deleting this exact leaf in one Tx.
CREATE TABLE agenteam_knowledge.project_object_retirements (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 operation_id agenteam_knowledge.safe_id NOT NULL REFERENCES agenteam_project.lifecycle_operations(id) ON DELETE RESTRICT,
 project_id agenteam_knowledge.safe_id NOT NULL REFERENCES agenteam_project.projects(id) ON DELETE RESTRICT,
 project_version bigint NOT NULL CHECK(project_version>0),
 document_id agenteam_knowledge.safe_id NOT NULL,
 object_id agenteam_knowledge.safe_id NOT NULL,
 upload_id agenteam_knowledge.safe_id NOT NULL,
 phase text NOT NULL CHECK(phase IN ('reference','object','metadata','completed')),
 version bigint NOT NULL CHECK(version>0),
 CONSTRAINT knowledge_project_object_identity UNIQUE(operation_id,object_id)
);
CREATE INDEX knowledge_project_object_retirements_target ON agenteam_knowledge.project_object_retirements(project_id,id);

-- This persisted predicate is an additional guard, not the private same-Store
-- Project claim, original pair's real returned calls or ProcessGuard proof.
-- +goose StatementBegin
CREATE FUNCTION agenteam_knowledge.project_cleanup_allowed(target_project uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (
  SELECT 1 FROM agenteam_project.projects p
  JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id
  JOIN agenteam_project.work_claims w ON w.work_kind='lifecycle' AND w.work_id=o.id AND w.project_id=p.id
  JOIN agenteam_project.lifecycle_participants lp ON lp.operation_id=o.id AND lp.participant_name='knowledge'
  WHERE p.id=target_project AND p.lifecycle='deleting' AND p.initialized_at IS NOT NULL
  AND o.owner_user_id=p.owner_user_id AND o.project_version=p.version
  AND o.action='delete' AND o.state='cleaning' AND o.cleanup_stage='domains'
  AND w.phase='running' AND lp.stop_state='stopped' AND lp.cleanup_state IN ('required','pending') AND lp.attempt_count>0
  AND NOT EXISTS(SELECT 1 FROM agenteam_project.lifecycle_participants other WHERE other.operation_id=o.id AND other.stop_state<>'stopped')
 );
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION agenteam_knowledge.project_call_cleanup_allowed(target_project uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT agenteam_knowledge.project_cleanup_allowed(target_project)
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.documents WHERE project_id=target_project)
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.commands WHERE project_id=target_project)
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.command_events WHERE project_id=target_project)
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.publications WHERE project_id=target_project)
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.work_claims WHERE project_id=target_project)
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.object_cleanup WHERE project_id=target_project)
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.project_object_retirements WHERE project_id=target_project)
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.project_calls WHERE (project_id=target_project OR source_project_id=target_project) AND phase<>'business_closed');
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION agenteam_knowledge.guard_project_call() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.phase='business_closed' AND (agenteam_knowledge.project_call_cleanup_allowed(OLD.project_id)
   OR (OLD.source_project_id IS NOT NULL AND agenteam_knowledge.project_call_cleanup_allowed(OLD.source_project_id))) THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='knowledge_project_calls_retained', MESSAGE='knowledge call is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'running' OR NEW.closed_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='knowledge_project_calls_initial', MESSAGE='knowledge call must start running';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.source_project_id,NEW.process_id,NEW.instance_id,NEW.lane,NEW.identity,NEW.identity_digest,NEW.registered_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.source_project_id,OLD.process_id,OLD.instance_id,OLD.lane,OLD.identity,OLD.identity_digest,OLD.registered_at)
 OR OLD.phase<>'running' OR NEW.phase<>'business_closed' THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='knowledge_project_calls_monotonic', MESSAGE='immutable knowledge call ownership';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER knowledge_project_calls_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_knowledge.project_calls FOR EACH ROW EXECUTE FUNCTION agenteam_knowledge.guard_project_call();

-- +goose StatementBegin
CREATE FUNCTION agenteam_knowledge.guard_project_object_retirement() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 target_project uuid;
 target_operation uuid;
 target_version bigint;
BEGIN
 IF TG_OP='DELETE' THEN
  target_project:=OLD.project_id; target_operation:=OLD.operation_id; target_version:=OLD.project_version;
 ELSE
  target_project:=NEW.project_id; target_operation:=NEW.operation_id; target_version:=NEW.project_version;
 END IF;
 IF NOT agenteam_knowledge.project_cleanup_allowed(target_project)
 OR NOT EXISTS(SELECT 1 FROM agenteam_project.projects p JOIN agenteam_project.lifecycle_operations o ON o.id=p.current_lifecycle_operation_id AND o.project_id=p.id WHERE p.id=target_project AND o.id=target_operation AND o.project_version=target_version)
 THEN RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='knowledge_project_object_current', MESSAGE='knowledge retirement requires current project cleanup'; END IF;
 IF TG_OP='DELETE' THEN
  IF OLD.phase='completed' THEN RETURN OLD; END IF;
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='knowledge_project_object_retained', MESSAGE='knowledge retirement is retained';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.phase<>'reference' OR NEW.version<>1 THEN
   RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='knowledge_project_object_initial', MESSAGE='knowledge retirement must start at reference';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.operation_id,NEW.project_id,NEW.project_version,NEW.document_id,NEW.object_id,NEW.upload_id)
 IS DISTINCT FROM ROW(OLD.id,OLD.operation_id,OLD.project_id,OLD.project_version,OLD.document_id,OLD.object_id,OLD.upload_id)
 OR NEW.version::numeric<>OLD.version::numeric+1
 OR NOT ((OLD.phase='reference' AND NEW.phase='object') OR (OLD.phase='object' AND NEW.phase='metadata') OR (OLD.phase='metadata' AND NEW.phase='completed')) THEN
  RAISE EXCEPTION USING ERRCODE='23514', CONSTRAINT='knowledge_project_object_monotonic', MESSAGE='immutable knowledge retirement';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER knowledge_project_object_retirements_guard BEFORE INSERT OR UPDATE OR DELETE ON agenteam_knowledge.project_object_retirements FOR EACH ROW EXECUTE FUNCTION agenteam_knowledge.guard_project_object_retirement();
