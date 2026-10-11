-- agenteam:transaction tx
-- +goose Up
-- A determined successor input is not another issued Model Round. Keep the
-- original rounds.execution_id UNIQUE, through=2/tool_pending and all 66 rows.
CREATE TABLE agenteam_execution.round_inputs (
 id agenteam_execution.safe_id PRIMARY KEY,
 execution_id agenteam_execution.safe_id NOT NULL,
 project_id agenteam_execution.safe_id NOT NULL,
 agent_id agenteam_execution.safe_id NOT NULL,
 snapshot_id agenteam_execution.safe_id NOT NULL,
 predecessor_round_id agenteam_execution.safe_id NOT NULL,
 skill_input_binding_id agenteam_execution.safe_id NOT NULL UNIQUE,
 initial_sequence bigint NOT NULL CHECK(initial_sequence>0),
 assignment_cutoff bigint NOT NULL CHECK(assignment_cutoff>=initial_sequence),
 transcript_through bigint NOT NULL CHECK(transcript_through IN (3,4)),
 checkpoint_version bigint NOT NULL CHECK(checkpoint_version>=5),
 content bytea NOT NULL CHECK(octet_length(content) BETWEEN 2 AND 4194304),
 digest text NOT NULL CHECK(digest='sha256:'||encode(sha256(content),'hex')),
 created_at timestamptz(6) NOT NULL,
 UNIQUE(execution_id,id),
 UNIQUE(execution_id,predecessor_round_id),
 CHECK(id<>predecessor_round_id),
 FOREIGN KEY(execution_id,project_id,agent_id) REFERENCES agenteam_execution.executions(id,project_id,agent_id),
 FOREIGN KEY(execution_id,snapshot_id) REFERENCES agenteam_execution.snapshots(execution_id,id),
 FOREIGN KEY(execution_id,predecessor_round_id) REFERENCES agenteam_execution.rounds(execution_id,id),
 FOREIGN KEY(execution_id) REFERENCES agenteam_execution.tool_result_consumptions(execution_id),
 CHECK(jsonb_typeof(convert_from(content,'UTF8')::jsonb)='object')
);
CREATE TABLE agenteam_execution.skill_assignment_dispositions (
 execution_id agenteam_execution.safe_id NOT NULL,
 change_id agenteam_execution.safe_id NOT NULL,
 round_input_id agenteam_execution.safe_id NOT NULL,
 assignment_sequence bigint NOT NULL CHECK(assignment_sequence>1),
 state text NOT NULL CHECK(state IN ('covered','skipped','applied')),
 reason text NOT NULL,
 PRIMARY KEY(execution_id,change_id),
 UNIQUE(execution_id,assignment_sequence),
 CHECK((state IN ('covered','applied') AND reason='') OR (state='skipped' AND reason IN ('removed','disabled','deleted','superseded'))),
 FOREIGN KEY(execution_id,change_id) REFERENCES agenteam_execution.skill_assignment_ingress(execution_id,change_id),
 FOREIGN KEY(execution_id,round_input_id) REFERENCES agenteam_execution.round_inputs(execution_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE agenteam_execution.control_inputs (
 id agenteam_execution.safe_id PRIMARY KEY,
 execution_id agenteam_execution.safe_id NOT NULL,
 round_input_id agenteam_execution.safe_id NOT NULL,
 kind text NOT NULL CHECK(kind='skills_added'),
 transcript_sequence bigint NOT NULL CHECK(transcript_sequence=4),
 content bytea NOT NULL CHECK(octet_length(content) BETWEEN 2 AND 4194304),
 digest text NOT NULL CHECK(digest='sha256:'||encode(sha256(content),'hex')),
 UNIQUE(execution_id,round_input_id,kind),
 FOREIGN KEY(execution_id,round_input_id) REFERENCES agenteam_execution.round_inputs(execution_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TRIGGER execution_round_input_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.round_inputs FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();
CREATE TRIGGER execution_skill_disposition_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.skill_assignment_dispositions FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();
CREATE TRIGGER execution_control_input_immutable BEFORE UPDATE OR DELETE ON agenteam_execution.control_inputs FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_direct_text_immutable();

-- Preserve the old Round FK. Only typed control has a distinct, explicit
-- RoundInput identity. The old 66 pairing function skips sequence<>3 unchanged.
ALTER TABLE agenteam_execution.transcript_entries
 ADD COLUMN round_input_id agenteam_execution.safe_id,
 ALTER COLUMN round_id DROP NOT NULL,
 DROP CONSTRAINT execution_transcript_sequence,
 DROP CONSTRAINT execution_transcript_kind,
 DROP CONSTRAINT execution_transcript_shape,
 DROP CONSTRAINT execution_transcript_role,
 ADD CONSTRAINT execution_transcript_sequence CHECK(sequence IN (1,2,3,4)),
 ADD CONSTRAINT execution_transcript_kind CHECK(kind IN ('input','assistant','tool_result','control')),
 ADD CONSTRAINT execution_transcript_shape CHECK((
  (round_id IS NOT NULL AND round_input_id IS NULL AND ((sequence=1 AND kind='input') OR (sequence=2 AND kind='assistant') OR (sequence=3 AND kind='tool_result')))
  OR (round_id IS NULL AND round_input_id IS NOT NULL AND sequence=4 AND kind='control')) IS TRUE),
 ADD CONSTRAINT execution_transcript_role CHECK((
  jsonb_typeof(convert_from(message,'UTF8')::jsonb)='object' AND
  ((kind<>'control' AND convert_from(message,'UTF8')::jsonb->>'role'=CASE kind WHEN 'input' THEN 'user' WHEN 'assistant' THEN 'assistant' ELSE 'tool' END)
   OR (kind='control' AND convert_from(message,'UTF8')::jsonb->>'kind'='skills_added'))
 ) IS TRUE),
 ADD CONSTRAINT execution_transcript_round_input FOREIGN KEY(execution_id,round_input_id) REFERENCES agenteam_execution.round_inputs(execution_id,id) DEFERRABLE INITIALLY DEFERRED,
 ADD CONSTRAINT execution_transcript_control_identity UNIQUE(execution_id,sequence,round_input_id);
ALTER TABLE agenteam_execution.control_inputs ADD CONSTRAINT execution_control_transcript_pair
 FOREIGN KEY(execution_id,transcript_sequence,round_input_id) REFERENCES agenteam_execution.transcript_entries(execution_id,sequence,round_input_id) DEFERRABLE INITIALLY DEFERRED;

-- Pair only Execution-owned facts. Current Skill validity/protection and H
-- remain checked by their owner in the original fully locked transaction.
-- +goose StatementBegin
CREATE FUNCTION agenteam_execution.guard_round_input_pair() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE v record; b jsonb; source jsonb; control jsonb; n bigint;
BEGIN
 IF TG_TABLE_NAME='transcript_entries' THEN
  IF NEW.sequence<>4 THEN RETURN NULL; END IF;
 END IF;
 SELECT q.*,r.schema_version AS old_schema,r.tool_pending,r.transcript_through AS old_through,
  r.terminal_status,r.snapshot_id AS old_snapshot,s.digest AS source_snapshot_digest,e.version AS current_version,
  c.round_id AS source_round,c.checkpoint_version AS source_version,c.transcript_sequence AS source_through,
  c.call_id,c.operation_id,c.attempt_id,c.message_digest,c.result_digest,c.created_at AS source_created
 INTO v FROM agenteam_execution.round_inputs q
 JOIN agenteam_execution.executions e ON e.id=q.execution_id
 JOIN agenteam_execution.rounds r ON r.execution_id=q.execution_id AND r.id=q.predecessor_round_id
 JOIN agenteam_execution.snapshots s ON s.execution_id=q.execution_id AND s.id=q.snapshot_id
 JOIN agenteam_execution.tool_result_consumptions c ON c.execution_id=q.execution_id
 WHERE q.execution_id=NEW.execution_id;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='missing round input pair'; END IF;
 b:=convert_from(v.content,'UTF8')::jsonb;source:=b->'source_consumption';control:=b->'control';
 IF NOT (v.old_schema=2 AND v.tool_pending AND v.old_through=2 AND v.terminal_status IS NULL
  AND v.old_snapshot=v.snapshot_id AND v.source_round=v.predecessor_round_id AND v.source_through=3
  AND v.checkpoint_version>v.source_version AND v.current_version>=v.checkpoint_version AND v.created_at>=v.source_created
  AND b->>'schema_version'='1' AND b->>'round_id'=v.id AND b->>'execution_id'=v.execution_id
  AND b->>'snapshot_id'=v.snapshot_id AND b->>'predecessor_round_id'=v.predecessor_round_id
  AND b->>'snapshot_digest'=v.source_snapshot_digest
  AND b->>'skill_input_binding_id'=v.skill_input_binding_id AND b->>'assignment_cutoff'=v.assignment_cutoff::text
  AND b#>>'{initial_bindings,assignment_sequence}'=v.initial_sequence::text
  AND b#>>'{initial_bindings,request,project_id}'=v.project_id AND b#>>'{initial_bindings,request,agent_id}'=v.agent_id
  AND b#>>'{initial_bindings,request,execution_id}'=v.execution_id
  AND b->>'checkpoint_version'=v.checkpoint_version::text AND b->>'transcript_through'=v.transcript_through::text
  AND (b->>'created_at')::timestamptz=v.created_at AND b->'compaction_ref'='null'::jsonb
  AND source->>'execution_id'=v.execution_id AND source->>'round_id'=v.source_round
  AND source->>'call_id'=v.call_id AND source->>'operation_id'=v.operation_id AND source->>'attempt_id'=v.attempt_id
  AND source->>'version'=v.source_version::text AND source->>'transcript_through'='3'
  AND source->>'message_digest'=v.message_digest AND source->>'result_digest'=v.result_digest
  AND (source->>'created_at')::timestamptz=v.source_created
  AND jsonb_typeof(b->'bindings')='array' AND jsonb_typeof(b->'dispositions')='array'
  AND jsonb_typeof(b->'applied_change_ids')='array') IS TRUE
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='invalid round input source'; END IF;
 SELECT count(*) INTO n FROM agenteam_execution.skill_assignment_dispositions d WHERE d.execution_id=v.execution_id;
 IF n<>jsonb_array_length(b->'dispositions') OR EXISTS (
  SELECT 1 FROM jsonb_array_elements(b->'dispositions') item
  WHERE NOT EXISTS (SELECT 1 FROM agenteam_execution.skill_assignment_dispositions d
   JOIN agenteam_execution.skill_assignment_ingress g ON (g.execution_id,g.change_id)=(d.execution_id,d.change_id)
   WHERE d.execution_id=v.execution_id AND d.round_input_id=v.id AND d.change_id=item->>'change_id'
    AND d.assignment_sequence::text=item->>'assignment_sequence' AND d.state=item->>'state' AND d.reason=item->>'reason'
    AND g.project_id=v.project_id AND g.agent_id=v.agent_id AND g.outcome='queued'
    AND convert_from(g.change_data,'UTF8')::jsonb->>'assignment_sequence'=d.assignment_sequence::text
    AND d.assignment_sequence<=v.assignment_cutoff
    AND ((d.state='covered' AND d.assignment_sequence<=v.initial_sequence)
      OR (d.state<>'covered' AND d.assignment_sequence>v.initial_sequence))))
 OR EXISTS (SELECT 1 FROM agenteam_execution.skill_assignment_ingress g WHERE g.execution_id=v.execution_id
  AND (convert_from(g.change_data,'UTF8')::jsonb->>'assignment_sequence')::bigint<=v.assignment_cutoff
  AND NOT EXISTS (SELECT 1 FROM agenteam_execution.skill_assignment_dispositions d WHERE (d.execution_id,d.change_id)=(g.execution_id,g.change_id)))
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='invalid round input dispositions'; END IF;
 IF b->'applied_change_ids' IS DISTINCT FROM COALESCE((SELECT jsonb_agg(d.change_id ORDER BY d.assignment_sequence)
  FROM agenteam_execution.skill_assignment_dispositions d WHERE d.execution_id=v.execution_id AND d.state='applied'),'[]'::jsonb)
 THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='invalid applied change set'; END IF;
 IF jsonb_array_length(b->'applied_change_ids')=0 THEN
  IF v.transcript_through<>3 OR control IS DISTINCT FROM 'null'::jsonb
   OR EXISTS(SELECT 1 FROM agenteam_execution.control_inputs c WHERE c.execution_id=v.execution_id)
   OR EXISTS(SELECT 1 FROM agenteam_execution.transcript_entries t WHERE t.execution_id=v.execution_id AND t.sequence=4)
  THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='unexpected empty skill control'; END IF;
 ELSE
  IF v.transcript_through<>4 OR NOT EXISTS (
   SELECT 1 FROM agenteam_execution.control_inputs c JOIN agenteam_execution.transcript_entries t
    ON (t.execution_id,t.sequence,t.round_input_id)=(c.execution_id,c.transcript_sequence,c.round_input_id)
   WHERE c.execution_id=v.execution_id AND c.round_input_id=v.id AND c.id=control->>'id'
    AND convert_from(c.content,'UTF8')::jsonb=control AND t.message=c.content AND t.digest=c.digest AND t.created_at=v.created_at
    AND control->>'execution_id'=v.execution_id AND control->>'round_id'=v.id AND control->>'source_id'=v.id
    AND control->>'kind'='skills_added' AND control->>'schema_version'='1' AND control->>'transcript_position'='4'
    AND jsonb_array_length(control->'payload')=jsonb_array_length(b->'applied_change_ids'))
  THEN RAISE EXCEPTION USING ERRCODE='23514',MESSAGE='missing skill control pair'; END IF;
 END IF;
 RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER execution_round_input_pair AFTER INSERT ON agenteam_execution.round_inputs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_round_input_pair();
CREATE CONSTRAINT TRIGGER execution_disposition_round_pair AFTER INSERT ON agenteam_execution.skill_assignment_dispositions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_round_input_pair();
CREATE CONSTRAINT TRIGGER execution_control_round_pair AFTER INSERT ON agenteam_execution.control_inputs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_round_input_pair();
CREATE CONSTRAINT TRIGGER execution_transcript_round_pair AFTER INSERT ON agenteam_execution.transcript_entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION agenteam_execution.guard_round_input_pair();
