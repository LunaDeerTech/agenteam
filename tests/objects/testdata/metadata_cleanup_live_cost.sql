-- SQL COST ONLY: loaded after the project and retired-transfer cost seeds,
-- into a database that is never attached to an Object/Downloads Service.
-- Live shapes do not assert authorization, real I/O, ProcessGuard, a native
-- retirement witness or a signed download binding. No constraint is disabled.
-- 33 GETs share the available Object; 33 PUTs have separate pending commands.
-- Only one PUT has a second private candidate: 34 nonterminal attempts in
-- total, at most two per command, below the existing global quota of 64.
BEGIN;
CREATE TEMP TABLE d05_live_puts(n integer PRIMARY KEY, object_id uuid, upload_id uuid, stage_id uuid, transfer_id uuid, lease_id uuid) ON COMMIT DROP;
INSERT INTO d05_live_puts SELECT n,
 ('01c00000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01c10000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01c20000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01c30000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01c40000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid
FROM generate_series(1,33) n;
INSERT INTO agenteam_object.objects(id,scope,partition_id,project_id,media_type,byte_size,sha256,state)
SELECT object_id,'project','01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000001','text/plain',4,decode(repeat('00',32),'hex'),'pending' FROM d05_live_puts;
INSERT INTO agenteam_object.uploads(id,object_id,command_hash,command_key,semantic_digest,owner_kind,owner_id,project_id,stable_actor,initiator_kind,initiator_id,existence,state,disposition,receipt_id)
SELECT upload_id,object_id,decode('01'||lpad(to_hex(n),62,'0'),'hex'),'live-cost-'||n,decode(repeat('11',32),'hex'),'skill_revision',object_id,'01910000-0000-7000-8000-000000000001','cost-fixture','human','01910000-0000-7000-8000-000000000004','existing','pending','reserved',upload_id FROM d05_live_puts;
INSERT INTO agenteam_object.object_references(object_id,owner_kind,owner_id,partition_id,kind,upload_id)
SELECT object_id,'skill_revision',object_id,'01910000-0000-7000-8000-000000000001','reserved',upload_id FROM d05_live_puts;
INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,kind,transfer_id,maybe_late,byte_size,sha256)
SELECT stage_id,upload_id,object_id,1,'staging/'||stage_id,'reserved','runner_staging',transfer_id,true,4,decode(repeat('00',32),'hex') FROM d05_live_puts;
INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,state)
SELECT lease_id,object_id,'transfer',transfer_id,'active' FROM d05_live_puts;
INSERT INTO agenteam_object.object_transfers(id,issue_hash,issue_key,issue_request_id,semantic_digest,actor_json,stable_actor,project_id,owner_kind,owner_id,runner_id,operation_id,runner_generation,operation_version,execution_id,direction,object_id,upload_id,upload_request_id,upload_key,staging_id,lease_id,media_type,byte_size,sha256,duration_seconds,expires_at,phase)
SELECT transfer_id,decode('02'||lpad(to_hex(n),62,'0'),'hex'),'live-cost-put-'||n,transfer_id,decode(repeat('22',32),'hex'),
 '{"Kind":"human","UserID":"01910000-0000-7000-8000-000000000004","SessionID":"01910000-0000-7000-8000-000000000006"}'::jsonb,
 '{"Agent":"","Cause":"","Execution":"","Project":"","Service":"","User":"01910000-0000-7000-8000-000000000004","kind":"human"}',
 '01910000-0000-7000-8000-000000000001','skill_revision',object_id,'01910000-0000-7000-8000-000000000007','01910000-0000-7000-8000-000000000008',1,1,'01910000-0000-7000-8000-000000000009','put',object_id,upload_id,upload_id,'live-cost-'||n,stage_id,lease_id,'text/plain',4,decode(repeat('00',32),'hex'),60,'2099-01-01T00:01:00Z','issued' FROM d05_live_puts;
UPDATE agenteam_object.uploads u SET current_attempt_id=p.stage_id FROM d05_live_puts p WHERE u.id=p.upload_id;

CREATE TEMP TABLE d05_live_gets(n integer PRIMARY KEY, transfer_id uuid, lease_id uuid) ON COMMIT DROP;
INSERT INTO d05_live_gets SELECT n,
 ('01c50000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 ('01c60000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid FROM generate_series(1,33) n;
INSERT INTO agenteam_object.object_leases(id,object_id,owner_kind,owner_id,state)
SELECT lease_id,'01920000-0000-7000-8000-000000000803','transfer',transfer_id,'active' FROM d05_live_gets;
INSERT INTO agenteam_object.object_transfers(id,issue_hash,issue_key,issue_request_id,semantic_digest,actor_json,stable_actor,project_id,owner_kind,owner_id,runner_id,operation_id,runner_generation,operation_version,execution_id,direction,object_id,lease_id,media_type,byte_size,sha256,candidate_key,duration_seconds,expires_at,phase)
SELECT transfer_id,decode('03'||lpad(to_hex(n),62,'0'),'hex'),'live-cost-get-'||n,transfer_id,decode(repeat('22',32),'hex'),
 '{"Kind":"human","UserID":"01910000-0000-7000-8000-000000000004","SessionID":"01910000-0000-7000-8000-000000000006"}'::jsonb,
 '{"Agent":"","Cause":"","Execution":"","Project":"","Service":"","User":"01910000-0000-7000-8000-000000000004","kind":"human"}',
 '01910000-0000-7000-8000-000000000001','skill_revision','01920000-0000-7000-8000-000000000803','01910000-0000-7000-8000-000000000007','01910000-0000-7000-8000-000000000008',1,1,'01910000-0000-7000-8000-000000000009','get','01920000-0000-7000-8000-000000000803',lease_id,'text/plain',4,decode(repeat('00',32),'hex'),'candidate/01940000-0000-7000-8000-000000000803',60,'2099-01-01T00:01:00Z','issued' FROM d05_live_gets;

-- Issue has returned locally; its joined work does not retire the external
-- lease. PUT1 is sending a private candidate after its source closed; PUT2 still
-- retains its live source. One command does not hold both stages at once.
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version,joined_at)
SELECT transfer_id,'01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','transfer_put',transfer_id,object_id,0,clock_timestamp() FROM d05_live_puts;
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version,joined_at)
SELECT transfer_id,'01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','transfer_get',transfer_id,'01920000-0000-7000-8000-000000000803',0,clock_timestamp() FROM d05_live_gets;
INSERT INTO agenteam_object.upload_attempts(id,upload_id,object_id,ordinal,candidate_key,phase,process_id,spool_payload_id,byte_size,sha256)
VALUES('01c70000-0000-7000-8000-000000000001','01c10000-0000-7000-8000-000000000001','01c00000-0000-7000-8000-000000000001',2,'candidate/01c70000-0000-7000-8000-000000000001','sending','01910000-0000-7000-8000-000000000005','01c70000-0000-7000-8000-000000000001',4,decode(repeat('00',32),'hex'));
UPDATE agenteam_object.uploads SET current_attempt_id='01c70000-0000-7000-8000-000000000001' WHERE id='01c10000-0000-7000-8000-000000000001';
INSERT INTO agenteam_object.object_leases(id,object_id,attempt_id,owner_kind,owner_id,process_id,state) VALUES
 ('01c80000-0000-7000-8000-000000000001','01c00000-0000-7000-8000-000000000001','01c70000-0000-7000-8000-000000000001','writer','01c70000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','active'),
 ('01c90000-0000-7000-8000-000000000001','01c00000-0000-7000-8000-000000000001',NULL,'source','01c90000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','active'),
 ('01c90000-0000-7000-8000-000000000002','01c00000-0000-7000-8000-000000000002',NULL,'source','01c90000-0000-7000-8000-000000000002','01910000-0000-7000-8000-000000000005','active');
UPDATE agenteam_object.object_leases SET state='released',released_at=clock_timestamp() WHERE id='01c90000-0000-7000-8000-000000000001';
UPDATE agenteam_object.object_transfers t SET phase='completing',version=CASE WHEN p.n=1 THEN 3 ELSE 2 END,completed_evidence=t.id,completed_digest=decode(repeat('33',32),'hex'),source_lease_id=('01c90000-0000-7000-8000-'||lpad(to_hex(p.n),12,'0'))::uuid,source_process_id='01910000-0000-7000-8000-000000000005',candidate_id=CASE WHEN p.n=1 THEN '01c70000-0000-7000-8000-000000000001'::uuid END FROM d05_live_puts p WHERE t.id=p.transfer_id AND p.n<=2;
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version) VALUES
 ('01c70000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','preparation','01c70000-0000-7000-8000-000000000001','01c00000-0000-7000-8000-000000000001',0),
 ('01c90000-0000-7000-8000-000000000002','01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','source','01c90000-0000-7000-8000-000000000002','01c00000-0000-7000-8000-000000000002',0);
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version,joined_at) VALUES
 ('01c90000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','source','01c90000-0000-7000-8000-000000000001','01c00000-0000-7000-8000-000000000001',0,clock_timestamp());

-- A binding is deliberately marked SQL-only, never decoded by Downloads.
-- Revoked grants can outlive their Object. The 33 live grants target the
-- available Object. An older revoked grant retains an unresolved started
-- attempt even though its original work is joined: join is not a byte result.
INSERT INTO agenteam_download.grants(id,project_id,user_id,object_id,binding,binding_sha256,expires_at,revoked)
SELECT ('01ca0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,
 CASE WHEN n<=1001 THEN '01910000-0000-7000-8000-000000000001'::uuid ELSE '01910000-0000-7000-8000-000000000002'::uuid END,
 '01910000-0000-7000-8000-000000000004',CASE WHEN n<=1001 THEN '01920000-0000-7000-8000-000000000001'::uuid ELSE '01920000-0000-7000-8000-000000000402'::uuid END,
 '{"sql_cost_fixture":true}'::jsonb,decode(repeat('00',32),'hex'),'2026-01-01T00:01:00Z',true FROM generate_series(1,11002) n;
INSERT INTO agenteam_download.grants(id,project_id,user_id,object_id,binding,binding_sha256,expires_at)
SELECT ('01cb0000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,'01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000004','01920000-0000-7000-8000-000000000803','{"sql_cost_fixture":true}'::jsonb,decode(repeat('00',32),'hex'),'2099-01-01T00:01:00Z' FROM generate_series(1,33) n;
INSERT INTO agenteam_download.attempts(id,grant_id,session_id,offset_bytes,length_bytes,phase)
VALUES('01cc0000-0000-7000-8000-000000000001','01ca0000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000006',0,4,'started');
INSERT INTO agenteam_object.project_work(id,project_id,process_id,kind,resource_id,object_id,admission_version,joined_at)
VALUES('01cd0000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000001','01910000-0000-7000-8000-000000000005','download','01cc0000-0000-7000-8000-000000000001','01920000-0000-7000-8000-000000000001',0,clock_timestamp());
SET CONSTRAINTS ALL IMMEDIATE;
COMMIT;
ANALYZE agenteam_object.objects;
ANALYZE agenteam_object.uploads;
ANALYZE agenteam_object.upload_attempts;
ANALYZE agenteam_object.object_references;
ANALYZE agenteam_object.object_transfers;
ANALYZE agenteam_object.object_leases;
ANALYZE agenteam_object.project_work;
ANALYZE agenteam_download.grants;
ANALYZE agenteam_download.attempts;
