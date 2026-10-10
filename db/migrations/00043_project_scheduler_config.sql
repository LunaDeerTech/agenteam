-- agenteam:transaction tx
-- +goose Up
-- The published system default is unlimited. There is no public management
-- transport yet. Changing this row affects only subsequent Project inserts.
CREATE TABLE agenteam_project.scheduler_defaults (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 default_project_scheduler_max_concurrency bigint CHECK(default_project_scheduler_max_concurrency>0),
 version bigint NOT NULL CHECK(version>0)
);
INSERT INTO agenteam_project.scheduler_defaults(singleton,default_project_scheduler_max_concurrency,version) VALUES(true,NULL,1);

-- Read actual persisted defaults in the original Project creation transaction.
-- Missing defaults are a dependency failure, never an implicit unlimited grant.
-- +goose StatementBegin
CREATE FUNCTION agenteam_project.initial_scheduler_max_concurrency() RETURNS bigint
LANGUAGE plpgsql STABLE AS $$
DECLARE result bigint;
BEGIN
 SELECT default_project_scheduler_max_concurrency INTO STRICT result
 FROM agenteam_project.scheduler_defaults WHERE singleton;
 RETURN result;
END;
$$;
-- +goose StatementEnd

-- This unbound candidate starts paused to preserve existing non-scheduling
-- behavior. It does not decide the eventual production product default.
ALTER TABLE agenteam_project.projects
 ADD COLUMN scheduler_enabled boolean NOT NULL DEFAULT false,
 ADD COLUMN scheduler_max_concurrency bigint DEFAULT agenteam_project.initial_scheduler_max_concurrency()
 CHECK(scheduler_max_concurrency>0);

-- Keep the inherited Project/Variable/Secret predicate exactly as PostgreSQL
-- stores it. Expand only the released project.update changed_fields IN set.
-- IN is deparsed as ANY(ARRAY[...]); require the exact original set once before
-- replacing it. Unexpected inherited definitions abort this whole migration.
-- Enumerating all 15 nonempty sorted subsets also rejects duplicates, unknown
-- names, wrong order, nulls and non-arrays without weakening any other branch.
-- +goose StatementBegin
DO $project_scheduler_audit$
DECLARE
 original_guard text;
 expanded_guard text;
 old_fields constant text := $old_fields$ARRAY['["name"]'::jsonb, '["description"]'::jsonb, '["description", "name"]'::jsonb]$old_fields$;
 new_fields constant text := $new_fields$ARRAY['["description"]'::jsonb, '["name"]'::jsonb, '["scheduler_enabled"]'::jsonb, '["scheduler_max_concurrency"]'::jsonb, '["description", "name"]'::jsonb, '["description", "scheduler_enabled"]'::jsonb, '["description", "scheduler_max_concurrency"]'::jsonb, '["name", "scheduler_enabled"]'::jsonb, '["name", "scheduler_max_concurrency"]'::jsonb, '["scheduler_enabled", "scheduler_max_concurrency"]'::jsonb, '["description", "name", "scheduler_enabled"]'::jsonb, '["description", "name", "scheduler_max_concurrency"]'::jsonb, '["description", "scheduler_enabled", "scheduler_max_concurrency"]'::jsonb, '["name", "scheduler_enabled", "scheduler_max_concurrency"]'::jsonb, '["description", "name", "scheduler_enabled", "scheduler_max_concurrency"]'::jsonb]$new_fields$;
BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT original_guard
 FROM pg_constraint
 WHERE conrelid='agenteam_audit.audit_records'::regclass
   AND conname='audit_records_project_contract' AND contype='c';
 IF (length(original_guard)-length(replace(original_guard,old_fields,'')))<>length(old_fields) THEN
  RAISE EXCEPTION 'unexpected Project update Audit field constraint';
 END IF;
 expanded_guard := replace(original_guard,old_fields,new_fields);
 ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_project_contract;
 EXECUTE 'ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_project_contract CHECK ('
  || expanded_guard || ')';
END;
$project_scheduler_audit$;
-- +goose StatementEnd
