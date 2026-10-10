-- SQL COST ONLY; no Skills/Object Service or authority consumes this database.
-- Full 00027 core shape retains its two cycles and all deferred FKs. The
-- marked manifest is a CHECK-valid cost value, not a builtin package receipt.
BEGIN;
CREATE TEMP TABLE d05_cost_skills(n integer PRIMARY KEY, project_id uuid, skill_id uuid, revision_id uuid, attempt_id uuid, object_id uuid, upload_id uuid) ON COMMIT DROP;
INSERT INTO d05_cost_skills
SELECT n,('01b00000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01b10000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01b20000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01b30000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01b40000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01b60000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid
FROM generate_series(1,2) n;
INSERT INTO agenteam_skill.initializations(project_id,creation_id,initialization_key,skill_id,revision_id,semantic_digest,bundle_id,revision,package_sha256,manifest_sha256,byte_size,manifest,name,description,phase,version,object_id,upload_id,current_attempt_id,created_at,updated_at)
SELECT project_id,project_id,'cost-'||n,skill_id,revision_id,'sha256:'||repeat('0',64),'builtin.add-skills.v1',1,'sha256:'||repeat('0',64),'sha256:'||repeat('1',64),4,'{"sql_cost_fixture":true}'::jsonb,'Add Skills','SQL cost shape only','published',1,object_id,upload_id,attempt_id,'2026-01-01T00:00:00Z','2026-01-01T00:00:01Z' FROM d05_cost_skills;
INSERT INTO agenteam_skill.object_attempts(attempt_id,project_id,creation_id,skill_id,revision_id,object_id,upload_id,process_id,created_at)
SELECT attempt_id,project_id,project_id,skill_id,revision_id,object_id,upload_id,'01b70000-0000-7000-8000-000000000001','2026-01-01T00:00:00Z' FROM d05_cost_skills;
INSERT INTO agenteam_skill.skills(id,project_id,creation_id,revision_id,name,normalized_name,description,protected,current_revision,version,serving)
SELECT skill_id,project_id,project_id,revision_id,'Add Skills','add-skills','SQL cost shape only',true,1,1,false FROM d05_cost_skills;
INSERT INTO agenteam_skill.revisions(id,project_id,skill_id,revision,object_id,object_version,object_created_at,published_at)
SELECT revision_id,project_id,skill_id,1,object_id,1,'2026-01-01T00:00:00Z','2026-01-01T00:00:01Z' FROM d05_cost_skills;
INSERT INTO agenteam_skill.cleanup(id,project_id,lifecycle_operation_id,project_version,action,skill_id,revision_id,object_id,upload_id,phase,version,created_at,updated_at)
SELECT ('01b80000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,project_id,project_id,2,'delete',skill_id,revision_id,object_id,upload_id,'pending',1,'2026-01-01T00:00:02Z','2026-01-01T00:00:02Z' FROM d05_cost_skills;
INSERT INTO agenteam_skill.work(id,project_id,skill_id,process_id,kind,phase,fence,created_at,joined_at)
SELECT ('01b50000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 CASE WHEN n<=1001 THEN '01b00000-0000-7000-8000-000000000001'::uuid ELSE '01b00000-0000-7000-8000-000000000002'::uuid END,
 CASE WHEN n<=1001 THEN '01b10000-0000-7000-8000-000000000001'::uuid ELSE '01b10000-0000-7000-8000-000000000002'::uuid END,
 '01b70000-0000-7000-8000-000000000001','package_reader','joined',1,'2026-01-01T00:00:01Z','2026-01-01T00:00:02Z' FROM generate_series(1,11002) n;
-- A single lower-ID unjoined row makes the joined predicate observable; it
-- is not a native reader and will never be submitted for Service retirement.
INSERT INTO agenteam_skill.work(id,project_id,skill_id,process_id,kind,phase,fence,created_at)
VALUES('01b50000-0000-7000-8000-000000000000','01b00000-0000-7000-8000-000000000001','01b10000-0000-7000-8000-000000000001','01b70000-0000-7000-8000-000000000001','package_reader','running',1,'2026-01-01T00:00:01Z');
SET CONSTRAINTS ALL IMMEDIATE;
COMMIT;
ANALYZE agenteam_skill.initializations;
ANALYZE agenteam_skill.object_attempts;
ANALYZE agenteam_skill.skills;
ANALYZE agenteam_skill.revisions;
ANALYZE agenteam_skill.cleanup;
ANALYZE agenteam_skill.work;
