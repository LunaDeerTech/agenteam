-- SQL COST ONLY; load after the project and retired-transfer seeds. No
-- Service consumes these rows as a completed publication or cleanup proof.
-- A committed/revoked available Object has 65 cleaned historical attempts,
-- one closed abandoned attempt whose physical cleanup is applying, and its
-- original current anchor after gateAttempt atomically set cleanup_gate and
-- abandoned before inserting its gated ProjectDeleted cleanup. The two
-- abandoned attempts obey both per-command=2 and global=64 reservation caps.
BEGIN;
INSERT INTO agenteam_object.objects(id,scope,partition_id,project_id,media_type,byte_size,sha256,state,version,candidate_key,cleaning)
VALUES('01e00000-0000-7000-8000-000000000001','project','01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000001','text/plain',4,decode(repeat('00',32),'hex'),'available',2,'candidate/01e20000-0000-7000-8000-000000000043',true);
INSERT INTO agenteam_audit.audit_records(id,scope,project_id,actor_kind,actor_project_id,service_name,service_cause,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal)
VALUES('01e10000-0000-7000-8000-000000000001','project','01910000-0000-7000-8000-000000000001','service','01910000-0000-7000-8000-000000000001','object','01e20000-0000-7000-8000-000000000043','object.upload.complete','success','stored_object','01e00000-0000-7000-8000-000000000001','{"sql_cost_fixture":true}'::jsonb,'sha256:'||repeat('0',64),'object','01e20000-0000-7000-8000-000000000043',0);
INSERT INTO agenteam_object.uploads(id,object_id,command_hash,command_key,semantic_digest,owner_kind,owner_id,project_id,stable_actor,initiator_kind,initiator_id,existence,state,disposition,receipt_id,audit_id)
VALUES('01e10000-0000-7000-8000-000000000001','01e00000-0000-7000-8000-000000000001',decode('05'||repeat('00',31),'hex'),'cost-pending',decode(repeat('11',32),'hex'),'skill_revision','01e00000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000001','cost-fixture','human','01910000-0000-7000-8000-000000000004','existing','committed','revoked','01e10000-0000-7000-8000-000000000001','01e10000-0000-7000-8000-000000000001');
INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,process_id,spool_payload_id,io_closed,cleanup_gate,byte_size,sha256)
SELECT ('01e20000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'01e10000-0000-7000-8000-000000000001','01e00000-0000-7000-8000-000000000001',n,'candidate/01e20000-0000-7000-8000-'||lpad(to_hex(n),12,'0'),CASE WHEN n<=65 THEN 'cleaned' ELSE 'abandoned' END,'01910000-0000-7000-8000-000000000005',('01e20000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,true,true,4,decode(repeat('00',32),'hex') FROM generate_series(1,67) n;
UPDATE agenteam_object.uploads SET current_attempt_id='01e20000-0000-7000-8000-000000000043' WHERE id='01e10000-0000-7000-8000-000000000001';
INSERT INTO agenteam_object.cleanup_operations(id,operation_id,object_id,attempt_id,reason,mode,phase,worker_id,fence,created_at)
SELECT ('01e30000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'01e10000-0000-7000-8000-000000000001','01e00000-0000-7000-8000-000000000001',('01e20000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'abandoned_attempt','zero_marker',CASE WHEN n=66 THEN 'applying' ELSE 'completed' END,CASE WHEN n=66 THEN '01e40000-0000-7000-8000-000000000001'::uuid END,CASE WHEN n=66 THEN 7 ELSE 1 END,'2026-01-01T00:00:00Z'::timestamptz+n*interval '1 second' FROM generate_series(1,66) n;
-- A smaller cleanup ID has a LATER original canonical ProjectDeleted cause.
-- ORDER BY id alone would therefore return a different cause than the actual
-- ORDER BY created_at,id query. Never delete history to manufacture a new
-- earliest native Audit cause: this test only reads the unchanged rows.
INSERT INTO agenteam_object.cleanup_operations(id,operation_id,object_id,attempt_id,reason,mode,phase,created_at)
VALUES('01e30000-0000-7000-8000-000000000000','01e50000-0000-7000-8000-000000000001','01e00000-0000-7000-8000-000000000001','01e20000-0000-7000-8000-000000000043','project_deleted','delete','gated','2026-01-02T00:00:00Z');
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version,cleanup_claim_fence)
VALUES('01e40000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','cleanup','01e30000-0000-7000-8000-000000000042','01e00000-0000-7000-8000-000000000001',0,7);
SET CONSTRAINTS ALL IMMEDIATE;
COMMIT;
ANALYZE agenteam_object.objects;
ANALYZE agenteam_object.uploads;
ANALYZE agenteam_object.upload_attempts;
ANALYZE agenteam_object.cleanup_operations;
ANALYZE agenteam_object.project_work;
