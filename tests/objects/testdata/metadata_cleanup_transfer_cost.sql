-- SQL-COST SUPPLEMENT; source-only, not yet executed. Load only after the
-- project cost fixture in its separate SQL database, never into a Service.
-- 65 target and 1001 other-Project retired PUT packages. A package preserves
-- the original deferred staging<->transfer cycle, its completed cleanup,
-- candidate FK and both external/source leases. No physical completion,
-- EvidenceAuthority, ProcessGuard or Audit witness is asserted by this seed.
BEGIN;
CREATE TEMP TABLE d05_cost_puts(n integer PRIMARY KEY, local_n integer, object_id uuid, upload_id uuid, project_id uuid, transfer_id uuid, stage_id uuid, candidate_id uuid, lease_id uuid, source_id uuid) ON COMMIT DROP;
INSERT INTO d05_cost_puts
SELECT n,CASE WHEN n<=65 THEN n ELSE n-65 END,
 CASE WHEN n<=65 THEN '01920000-0000-7000-8000-000000000001'::uuid ELSE '01920000-0000-7000-8000-000000000402'::uuid END,
 CASE WHEN n<=65 THEN '01930000-0000-7000-8000-000000000001'::uuid ELSE '01930000-0000-7000-8000-000000000402'::uuid END,
 CASE WHEN n<=65 THEN '01910000-0000-7000-8000-000000000001'::uuid ELSE '01910000-0000-7000-8000-000000000002'::uuid END,
 ('019c0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('019d0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 CASE WHEN n<=65 THEN ('01960000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid ELSE ('019e0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid END,
 ('019f0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01a00000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid
FROM generate_series(1,1066) n;

-- The canonical current candidate has a later ordinal than every historical
-- private/staging attempt. No unique(upload_id,ordinal) check is disabled.
UPDATE agenteam_object.upload_attempts SET ordinal=132 WHERE id='01940000-0000-7000-8000-000000000001';
UPDATE agenteam_object.upload_attempts SET ordinal=2003 WHERE id='01940000-0000-7000-8000-000000000402';
INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,process_id,spool_payload_id,io_closed,cleanup_gate,byte_size,sha256)
SELECT candidate_id,upload_id,object_id,local_n,'candidate/'||candidate_id,'cleaned','01910000-0000-7000-8000-000000000005',candidate_id,true,true,4,decode(repeat('00',32),'hex') FROM d05_cost_puts WHERE n>65;
INSERT INTO agenteam_object.cleanup_operations(id,operation_id,object_id,attempt_id,reason,mode,phase)
SELECT ('01a10000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,upload_id,object_id,candidate_id,'abandoned_attempt','zero_marker','completed' FROM d05_cost_puts WHERE n>65;

INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,process_id,state,released_at)
SELECT lease_id,object_id,'transfer',transfer_id,NULL,'released',clock_timestamp() FROM d05_cost_puts;
INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,process_id,state,released_at)
SELECT source_id,object_id,'source',source_id,'01910000-0000-7000-8000-000000000005','released',clock_timestamp() FROM d05_cost_puts;

-- Each INSERT is in the same original deferred transaction. The staging FK
-- points to the corresponding transfer, and the transfer FK points back to
-- the exact staging row. SET CONSTRAINTS below forces both real FK checks.
INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,kind,transfer_id,io_closed,cleanup_gate,byte_size,sha256)
SELECT stage_id,upload_id,object_id,local_n+CASE WHEN n<=65 THEN 65 ELSE 1001 END,'staging/'||stage_id,'cleaned','runner_staging',transfer_id,true,true,4,decode(repeat('00',32),'hex') FROM d05_cost_puts;
INSERT INTO agenteam_object.object_transfers(id,issue_hash,issue_key,issue_request_id,semantic_digest,actor_json,stable_actor,project_id,owner_kind,owner_id,runner_id,operation_id,runner_generation,operation_version,execution_id,direction,object_id,upload_id,upload_request_id,upload_key,staging_id,candidate_id,lease_id,source_lease_id,source_process_id,media_type,byte_size,sha256,duration_seconds,expires_at,phase,version,revoked_at,completed_evidence,completed_digest,retirement_evidence,retirement_kind,retirement_digest,cleanup_gate)
SELECT transfer_id,decode(lpad(to_hex(n),64,'0'),'hex'),'cost-put-'||n,transfer_id,decode(repeat('22',32),'hex'),
 '{"Kind":"human","UserID":"01910000-0000-7000-8000-000000000004","SessionID":"01910000-0000-7000-8000-000000000006"}'::jsonb,
 '{"Agent":"","Cause":"","Execution":"","Project":"","Service":"","User":"01910000-0000-7000-8000-000000000004","kind":"human"}',
 project_id,'skill_revision',object_id,'01910000-0000-7000-8000-000000000007','01910000-0000-7000-8000-000000000008',1,1,'01910000-0000-7000-8000-000000000009','put',object_id,upload_id,upload_id,CASE WHEN n<=65 THEN 'cost-1' ELSE 'cost-1026' END,stage_id,candidate_id,lease_id,source_id,'01910000-0000-7000-8000-000000000005','text/plain',4,decode(repeat('00',32),'hex'),60,'2026-01-01T00:01:00Z','failed',2,'2026-01-01T00:02:00Z',transfer_id,decode(repeat('33',32),'hex'),transfer_id,'completed',decode(repeat('33',32),'hex'),true
FROM d05_cost_puts;
INSERT INTO agenteam_object.cleanup_operations(id,operation_id,object_id,attempt_id,reason,mode,phase)
SELECT ('01a20000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,upload_id,object_id,stage_id,'abandoned_attempt','zero_marker','completed' FROM d05_cost_puts;
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version,joined_at)
SELECT source_id,project_id,'01910000-0000-7000-8000-000000000005','source',source_id,object_id,0,clock_timestamp() FROM d05_cost_puts;
SET CONSTRAINTS ALL IMMEDIATE;
COMMIT;
ANALYZE agenteam_object.upload_attempts;
ANALYZE agenteam_object.cleanup_operations;
ANALYZE agenteam_object.object_leases;
ANALYZE agenteam_object.object_transfers;
ANALYZE agenteam_object.project_work;

-- The later test must assert all 1066 packages and 2132 leases still exist at
-- EXPLAIN time, measure target/absent ranges, and keep foreign packages while
-- deleting only a valid target package in a rollback Tx for actual FK costs.
-- This is not yet the live GET/PUT external-lease scenario; no SQL mutation of
-- a deleted Object to an active transfer is a legal substitute for that case.
