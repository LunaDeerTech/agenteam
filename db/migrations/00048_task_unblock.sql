-- agenteam:transaction tx
-- +goose Up
-- Keep creation provenance (including immutable Scheduler failure facts) and
-- distinguish a combined Human transition from the older standalone command.
ALTER TABLE agenteam_work.task_blockers
 ADD COLUMN resolved_transition_operation_id agenteam_work.safe_id;
ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_resolved_transition_fk
 FOREIGN KEY(project_id,resolved_transition_operation_id)
 REFERENCES agenteam_work.task_transition_commands(project_id,id) ON DELETE RESTRICT;
-- Preserve the complete old resolution predicate, including unresolved rows.
-- A technical blocker can only acquire the new transition resolution parent.
-- +goose StatementBegin
DO $$
DECLARE old_expr text;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT old_expr FROM pg_constraint
 WHERE conrelid='agenteam_work.task_blockers'::regclass AND conname='task_blockers_resolution_check' AND contype='c';
 ALTER TABLE agenteam_work.task_blockers DROP CONSTRAINT task_blockers_resolution_check;
 EXECUTE format($check$ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_resolution_check CHECK((
 (resolved_transition_operation_id IS NULL AND (type<>'technical' OR resolved_at IS NULL) AND (%s))
 OR (resolved_transition_operation_id IS NOT NULL AND resolved_operation_id IS NULL
 AND resolved_at IS NOT NULL AND resolved_at>=created_at AND resolved_by IS NOT NULL
 AND resolution_comment IS NULL AND octet_length(resolved_by::text)<=1024
 AND resolved_by=jsonb_build_object('type','human','source','task_domain','user_id',resolved_by->>'user_id')
 AND resolved_by->>'user_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')) IS TRUE)$check$,old_expr);
END;
$$;
-- +goose StatementEnd
ALTER TABLE agenteam_work.task_blockers DROP CONSTRAINT task_blockers_creation_source;
ALTER TABLE agenteam_work.task_blockers ADD CONSTRAINT task_blockers_creation_source CHECK((
 (type IN ('rely_on','waiting_for_human') AND created_operation_id IS NOT NULL AND failure_operation_id IS NULL)
 OR (type='technical' AND created_operation_id IS NULL AND failure_operation_id IS NOT NULL)) IS TRUE);
-- The existing immutable trigger still rejects creation changes, a second
-- resolution, and clearing a resolution. No Scheduler row is changed here.
