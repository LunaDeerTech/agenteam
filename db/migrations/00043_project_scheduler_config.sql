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
