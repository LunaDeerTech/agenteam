-- SQL COST ONLY. Never load these rows into a Service fixture database.
-- The production continuous Migrator creates all actual CHECKs/FKs first.
-- 1025 deleted Objects per Project; one additional available target Object
-- keeps the later reader state legal. Current-attempt cycles are filled only
-- after the referenced attempts exist; the available Object retains a
-- committed attached upload, published private candidate and canonical ref.
-- All UUIDs are valid v7-domain values; no constraint/trigger is disabled.
-- Marked audit rows satisfy SQL FK shape, not native Audit witness semantics.
-- 65 cleaned old candidates and 1001/10001 retired reader histories remain
-- throughout every EXPLAIN. Active rows are added by a separate test stage.
-- Transfer/download/Skills tables intentionally remain empty and do not count
-- as representative evidence for their indexes or incoming FK trigger costs.

BEGIN;
CREATE TEMP TABLE d05_cost_objects(n integer PRIMARY KEY, object_id uuid, upload_id uuid, attempt_id uuid, project_id uuid) ON COMMIT DROP;
INSERT INTO d05_cost_objects
SELECT n,
 ('01920000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01930000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01940000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 CASE WHEN n<=1025 OR n=2051 THEN '01910000-0000-7000-8000-000000000001'::uuid ELSE '01910000-0000-7000-8000-000000000002'::uuid END
FROM generate_series(1,2051) n;

INSERT INTO agenteam_object.objects(id,scope,partition_id,project_id,media_type,byte_size,sha256,state,version,cleaning,deleted_at,candidate_key)
SELECT object_id,'project',project_id,project_id,'text/plain',4,decode(repeat('00',32),'hex'),CASE WHEN n=2051 THEN 'available' ELSE 'deleted' END,2,n<>2051,CASE WHEN n<>2051 THEN '2026-01-02T00:00:00Z'::timestamptz END,CASE WHEN n=2051 THEN 'candidate/'||attempt_id END FROM d05_cost_objects;

-- SQL-only marked rows satisfy uploads' committed/audit_id FK shape; they
-- deliberately do not claim a real Audit appender/witness or physical return.
INSERT INTO agenteam_audit.audit_records(id,scope,project_id,actor_kind,actor_project_id,service_name,service_cause,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal)
SELECT upload_id,'project',project_id,'service',project_id,'object',attempt_id::text,'object.upload.complete','success','stored_object',object_id,'{"sql_cost_fixture":true}'::jsonb,'sha256:'||repeat('0',64),'object',attempt_id::text,0 FROM d05_cost_objects;
INSERT INTO agenteam_object.uploads(id,object_id,command_hash,command_key,semantic_digest,owner_kind,owner_id,project_id,stable_actor,initiator_kind,initiator_id,existence,state,disposition,receipt_id,audit_id)
SELECT upload_id,object_id,decode(lpad(to_hex(n),64,'0'),'hex'),'cost-'||n,decode(repeat('11',32),'hex'),'skill_revision',object_id,project_id,'cost-fixture','human','01910000-0000-7000-8000-000000000004','existing','committed',CASE WHEN n=2051 THEN 'attached' ELSE 'revoked' END,upload_id,upload_id FROM d05_cost_objects;

INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,process_id,spool_payload_id,io_closed,cleanup_gate,byte_size,sha256)
SELECT attempt_id,upload_id,object_id,CASE WHEN n=1 THEN 66 ELSE 1 END,'candidate/'||attempt_id,CASE WHEN n=2051 THEN 'published' ELSE 'cleaned' END,'01910000-0000-7000-8000-000000000005',attempt_id,true,n<>2051,4,decode(repeat('00',32),'hex') FROM d05_cost_objects;
UPDATE agenteam_object.uploads u SET current_attempt_id=d.attempt_id FROM d05_cost_objects d WHERE u.id=d.upload_id;

INSERT INTO agenteam_object.cleanup_operations(id,operation_id,object_id,attempt_id,reason,mode,phase)
SELECT ('01950000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,upload_id,object_id,attempt_id,'project_deleted','zero_marker','completed' FROM d05_cost_objects WHERE n<=2050;
INSERT INTO agenteam_object.object_references(object_id,owner_kind,owner_id,partition_id,kind,upload_id)
SELECT object_id,'skill_revision',object_id,project_id,'canonical',upload_id FROM d05_cost_objects WHERE n=2051;

INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,process_id,spool_payload_id,io_closed,cleanup_gate,byte_size,sha256)
SELECT ('01960000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'01930000-0000-7000-8000-000000000001','01920000-0000-7000-8000-000000000001',n,'candidate/01960000-0000-7000-8000-'||lpad(to_hex(n),12,'0'),'cleaned','01910000-0000-7000-8000-000000000005',('01960000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,true,true,4,decode(repeat('00',32),'hex') FROM generate_series(1,65) n;
INSERT INTO agenteam_object.cleanup_operations(id,operation_id,object_id,attempt_id,reason,mode,phase)
SELECT ('01970000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'01930000-0000-7000-8000-000000000001','01920000-0000-7000-8000-000000000001',('01960000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'abandoned_attempt','zero_marker','completed' FROM generate_series(1,65) n;

CREATE TEMP TABLE d05_cost_readers(n integer PRIMARY KEY, lease_id uuid, object_id uuid, project_id uuid) ON COMMIT DROP;
INSERT INTO d05_cost_readers
SELECT n,('01980000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 CASE WHEN n<=1001 THEN '01920000-0000-7000-8000-000000000001'::uuid ELSE '01920000-0000-7000-8000-000000000402'::uuid END,
 CASE WHEN n<=1001 THEN '01910000-0000-7000-8000-000000000001'::uuid ELSE '01910000-0000-7000-8000-000000000002'::uuid END
FROM generate_series(1,11002) n;
INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,process_id,state,created_at,released_at)
SELECT lease_id,object_id,'reader',lease_id,'01910000-0000-7000-8000-000000000005','released','2026-01-01T00:00:00Z'::timestamptz,'2026-01-02T00:00:00Z'::timestamptz FROM d05_cost_readers;
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version,created_at,joined_at)
SELECT ('01990000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,project_id,'01910000-0000-7000-8000-000000000005','reader',lease_id,object_id,0,'2026-01-01T00:00:00Z'::timestamptz,'2026-01-02T00:00:00Z'::timestamptz FROM d05_cost_readers;
SET CONSTRAINTS ALL IMMEDIATE;
COMMIT;

ANALYZE agenteam_object.objects;
ANALYZE agenteam_object.uploads;
ANALYZE agenteam_object.upload_attempts;
ANALYZE agenteam_object.cleanup_operations;
ANALYZE agenteam_object.object_leases;
ANALYZE agenteam_object.project_work;

