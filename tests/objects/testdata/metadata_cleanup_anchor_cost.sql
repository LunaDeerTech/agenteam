-- SQL COST ONLY. Add this to the separate project+retired-transfer cost DB.
-- No Service consumes these marked Audit/Artifact/publication facts. The
-- actual Migrator's FK/CHECK definitions remain enabled throughout.
-- Keep 1001 foreign available Objects, canonical references, Artifact rows,
-- upload/current cycles and released writer->attempt edges at every target
-- DELETE. This makes the remaining incoming FK probes nonempty globally.
BEGIN;
CREATE TEMP TABLE d05_anchor_background(n integer PRIMARY KEY, object_id uuid, upload_id uuid, attempt_id uuid, artifact_id uuid, lease_id uuid) ON COMMIT DROP;
INSERT INTO d05_anchor_background SELECT n,
 ('01d00000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01d10000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01d20000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01d30000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01d40000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid FROM generate_series(1,1001) n;
INSERT INTO agenteam_object.objects(id,scope,partition_id,project_id,media_type,byte_size,sha256,state,version,candidate_key)
SELECT object_id,'project','01910000-0000-7000-8000-000000000002','01910000-0000-7000-8000-000000000002','text/plain',4,decode(repeat('00',32),'hex'),'available',2,'candidate/'||attempt_id FROM d05_anchor_background;
INSERT INTO agenteam_audit.audit_records(id,scope,project_id,actor_kind,actor_project_id,service_name,service_cause,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal)
SELECT upload_id,'project','01910000-0000-7000-8000-000000000002','service','01910000-0000-7000-8000-000000000002','object',attempt_id::text,'object.upload.complete','success','stored_object',object_id,'{"sql_cost_fixture":true}'::jsonb,'sha256:'||repeat('0',64),'object',attempt_id::text,0 FROM d05_anchor_background;
INSERT INTO agenteam_object.uploads(id,object_id,command_hash,command_key,semantic_digest,owner_kind,owner_id,project_id,stable_actor,initiator_kind,initiator_id,existence,state,disposition,receipt_id,audit_id)
SELECT upload_id,object_id,decode('04'||lpad(to_hex(n),62,'0'),'hex'),'cost-anchor-'||n,decode(repeat('11',32),'hex'),'artifact',artifact_id,'01910000-0000-7000-8000-000000000002','cost-fixture','human','01910000-0000-7000-8000-000000000004','existing','committed','attached',upload_id,upload_id FROM d05_anchor_background;
INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,process_id,spool_payload_id,io_closed,byte_size,sha256)
SELECT attempt_id,upload_id,object_id,1,'candidate/'||attempt_id,'published','01910000-0000-7000-8000-000000000005',attempt_id,true,4,decode(repeat('00',32),'hex') FROM d05_anchor_background;
UPDATE agenteam_object.uploads u SET current_attempt_id=b.attempt_id FROM d05_anchor_background b WHERE u.id=b.upload_id;
INSERT INTO agenteam_object.object_references(object_id,owner_kind,owner_id,partition_id,kind,upload_id)
SELECT object_id,'artifact',artifact_id,'01910000-0000-7000-8000-000000000002','canonical',upload_id FROM d05_anchor_background;
INSERT INTO agenteam_object.object_leases(id,object_id,attempt_id,owner_kind,owner_id,process_id,state,released_at)
SELECT lease_id,object_id,attempt_id,'writer',attempt_id,'01910000-0000-7000-8000-000000000005','released',clock_timestamp() FROM d05_anchor_background;
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version,joined_at)
SELECT attempt_id,'01910000-0000-7000-8000-000000000002','01910000-0000-7000-8000-000000000005','preparation',attempt_id,object_id,0,clock_timestamp() FROM d05_anchor_background;
INSERT INTO agenteam_artifact.artifacts(id,file_id,project_id,object_id,kind,name,description,media_type,byte_size,sha256,object_version,object_created_at,creator_kind,creator_id,creation_cause)
SELECT artifact_id,artifact_id,'01910000-0000-7000-8000-000000000002',object_id,'user_upload','SQL cost fixture','Not a Service receipt','text/plain',4,decode(repeat('00',32),'hex'),2,o.created_at,'human','01910000-0000-7000-8000-000000000004','sha256:'||repeat('0',64) FROM d05_anchor_background b JOIN agenteam_object.objects o ON o.id=b.object_id;
SET CONSTRAINTS ALL IMMEDIATE;
COMMIT;
ANALYZE agenteam_object.objects;
ANALYZE agenteam_object.uploads;
ANALYZE agenteam_object.upload_attempts;
ANALYZE agenteam_object.object_references;
ANALYZE agenteam_object.object_leases;
ANALYZE agenteam_object.cleanup_operations;
ANALYZE agenteam_object.object_transfers;
ANALYZE agenteam_object.project_work;
ANALYZE agenteam_artifact.artifacts;
