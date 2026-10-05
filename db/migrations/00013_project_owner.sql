-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_project;
CREATE DOMAIN agenteam_project.safe_id AS uuid CHECK (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');
CREATE TABLE agenteam_project.projects (
 id agenteam_project.safe_id PRIMARY KEY,
 owner_user_id agenteam_project.safe_id NOT NULL,
 name text NOT NULL CHECK(name ~ '^[A-Za-z0-9._-]{1,64}$' AND name NOT IN ('.','..')),
 normalized_name text NOT NULL CHECK(normalized_name=translate(name,'ABCDEFGHIJKLMNOPQRSTUVWXYZ','abcdefghijklmnopqrstuvwxyz')),
 description text NOT NULL CHECK(octet_length(description)<=8192 AND description !~ '[\x01-\x08\x0B-\x1F\x7F]'),
 lifecycle text NOT NULL CHECK(lifecycle IN ('active','archiving','archived','deleting')),
 version bigint NOT NULL CHECK(version>0), current_sprint_id agenteam_project.safe_id,
 created_at timestamptz(6) NOT NULL,updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),archived_at timestamptz(6),
 creation_id agenteam_project.safe_id NOT NULL UNIQUE,initialized_at timestamptz(6),current_lifecycle_operation_id agenteam_project.safe_id,
 CONSTRAINT projects_owner_name_key UNIQUE(owner_user_id,normalized_name),
 CHECK((lifecycle='archived' AND archived_at IS NOT NULL) OR lifecycle<>'archived'),
 CHECK((lifecycle IN ('archiving','deleting') AND current_lifecycle_operation_id IS NOT NULL) OR lifecycle NOT IN ('archiving','deleting')),
 CHECK(initialized_at IS NOT NULL OR (lifecycle='active' AND version=1 AND current_sprint_id IS NULL AND current_lifecycle_operation_id IS NULL))
);
CREATE INDEX projects_owned_page ON agenteam_project.projects(owner_user_id,created_at DESC,id DESC) WHERE initialized_at IS NOT NULL;
CREATE TABLE agenteam_project.creations (
 id agenteam_project.safe_id PRIMARY KEY,
 project_id agenteam_project.safe_id NOT NULL UNIQUE REFERENCES agenteam_project.projects(id) DEFERRABLE INITIALLY DEFERRED,
 owner_user_id agenteam_project.safe_id NOT NULL,
 command_key text NOT NULL CHECK(octet_length(command_key) BETWEEN 1 AND 128 AND command_key ~ '^[!-~]+$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 request_name text,request_description text,
 state text NOT NULL CHECK(state IN ('accepted','initializing','failed','completed')),
 initialization_key text NOT NULL CHECK(octet_length(initialization_key) BETWEEN 1 AND 128 AND initialization_key ~ '^[!-~]+$'),
 protected_skill_id agenteam_project.safe_id,protected_revision bigint CHECK(protected_revision>0),
 safe_reason text CHECK(safe_reason IN ('dependency_unbound','dependency_unavailable','work_pending','outcome_unknown','operation_failed')),
 version bigint NOT NULL CHECK(version>0),created_at timestamptz(6) NOT NULL,updated_at timestamptz(6) NOT NULL CHECK(updated_at>=created_at),
 event_id agenteam_project.safe_id NOT NULL UNIQUE,event_header jsonb,event_payload jsonb,safe_result jsonb,
 CHECK((event_header IS NULL AND event_payload IS NULL) OR (event_header IS NOT NULL AND jsonb_typeof(event_header)='object' AND event_payload IS NOT NULL AND jsonb_typeof(event_payload)='object' AND octet_length(event_header::text)<=2048 AND octet_length(event_payload::text)<=1024)),
 CHECK((state='completed' AND request_name IS NULL AND request_description IS NULL AND protected_skill_id IS NOT NULL AND protected_revision IS NOT NULL AND safe_result IS NOT NULL AND jsonb_typeof(safe_result)='object' AND event_header IS NOT NULL AND safe_reason IS NULL)
 OR (state<>'completed' AND request_name IS NOT NULL AND request_description IS NOT NULL AND protected_skill_id IS NULL AND protected_revision IS NULL AND safe_result IS NULL)),
 CHECK(state<>'failed' OR safe_reason IS NOT NULL),CHECK(state<>'accepted' OR safe_reason IS NULL)
);
ALTER TABLE agenteam_project.projects ADD CONSTRAINT projects_creation_fk FOREIGN KEY(creation_id) REFERENCES agenteam_project.creations(id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX project_creations_recovery ON agenteam_project.creations(id) WHERE state<>'completed';
CREATE TABLE agenteam_project.commands (
 id agenteam_project.safe_id PRIMARY KEY,project_id agenteam_project.safe_id NOT NULL REFERENCES agenteam_project.projects(id) DEFERRABLE INITIALLY DEFERRED,
 actor_user_id agenteam_project.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name IN ('update','archive','restore','delete','retry-lifecycle')),
 key text NOT NULL CHECK(octet_length(key) BETWEEN 1 AND 128 AND key ~ '^[!-~]+$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 state text NOT NULL CHECK(state IN ('planned','completed')),safe_result jsonb,plan jsonb,
 event_id agenteam_project.safe_id UNIQUE,event_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(event_ids)='array'),
 audit_cause text NOT NULL UNIQUE CHECK(audit_cause ~ '^sha256:[0-9a-f]{64}$'),
 created_at timestamptz(6) NOT NULL,committed_at timestamptz(6),
 UNIQUE(project_id,command_name,key),
 CHECK((state='planned' AND plan IS NOT NULL AND safe_result IS NULL AND committed_at IS NULL) OR (state='completed' AND safe_result IS NOT NULL AND committed_at IS NOT NULL)),
 CHECK(plan IS NULL OR (jsonb_typeof(plan)='object' AND octet_length(plan::text)<=65536)),
 CHECK(safe_result IS NULL OR (jsonb_typeof(safe_result)='object' AND octet_length(safe_result::text)<=65536)),
 CHECK(committed_at IS NULL OR committed_at>=created_at)
);
CREATE TABLE agenteam_project.lifecycle_operations (
 id agenteam_project.safe_id PRIMARY KEY,project_id agenteam_project.safe_id NOT NULL REFERENCES agenteam_project.projects(id) DEFERRABLE INITIALLY DEFERRED,
 owner_user_id agenteam_project.safe_id NOT NULL,action text NOT NULL CHECK(action IN ('archive','delete')),
 project_version bigint NOT NULL CHECK(project_version>0),completed_project_version bigint CHECK(completed_project_version>0),
 state text NOT NULL CHECK(state IN ('accepted','stopping','cleaning','completed','failed')),
 resume_phase text CHECK(resume_phase IN ('stop','cleanup')),cleanup_stage text CHECK(cleanup_stage IN ('domains','outbox','audit','final')),
 version bigint NOT NULL CHECK(version>0),required_manifest jsonb NOT NULL,manifest_digest text NOT NULL CHECK(manifest_digest ~ '^sha256:[0-9a-f]{64}$'),
 safe_reason text CHECK(safe_reason IN ('dependency_unbound','dependency_unavailable','work_pending','outcome_unknown','operation_failed')),retry_after timestamptz(6),
 created_at timestamptz(6) NOT NULL,updated_at timestamptz(6) NOT NULL,completed_at timestamptz(6),
 CHECK(updated_at>=created_at),CHECK(action<>'archive' OR (state<>'cleaning' AND cleanup_stage IS NULL AND resume_phase IS DISTINCT FROM 'cleanup')),
 CHECK((state='completed' AND completed_at IS NOT NULL) OR (state<>'completed' AND completed_at IS NULL)),
 CHECK(completed_project_version IS NULL OR (action='archive' AND state='completed' AND completed_project_version::numeric=project_version::numeric+1)),
 CHECK(cleanup_stage IS NULL OR (action='delete' AND (state='cleaning' OR (state='failed' AND resume_phase='cleanup'))))
);
CREATE UNIQUE INDEX project_lifecycle_one_active ON agenteam_project.lifecycle_operations(project_id) WHERE state<>'completed';
ALTER TABLE agenteam_project.projects ADD CONSTRAINT projects_operation_fk FOREIGN KEY(current_lifecycle_operation_id) REFERENCES agenteam_project.lifecycle_operations(id) DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE agenteam_project.lifecycle_participants (
 operation_id agenteam_project.safe_id NOT NULL REFERENCES agenteam_project.lifecycle_operations(id) DEFERRABLE INITIALLY DEFERRED,
 participant_name text NOT NULL CHECK(participant_name ~ '^[a-z][a-z0-9-]{0,63}$'),contract_version bigint NOT NULL CHECK(contract_version>0),
 stop_state text NOT NULL CHECK(stop_state IN ('required','pending','stopped','failed')),
 cleanup_state text NOT NULL CHECK(cleanup_state IN ('not_applicable','required','pending','completed','failed')),
 checkpoint_schema bigint CHECK(checkpoint_schema>0),checkpoint bytea,safe_pending_refs jsonb NOT NULL DEFAULT '[]'::jsonb,
 safe_reason text CHECK(safe_reason IN ('dependency_unbound','dependency_unavailable','work_pending','outcome_unknown','operation_failed')),
 attempt_count bigint NOT NULL DEFAULT 0 CHECK(attempt_count>=0),next_attempt_at timestamptz(6),version bigint NOT NULL CHECK(version>0),
 PRIMARY KEY(operation_id,participant_name),CHECK((checkpoint IS NULL)=(checkpoint_schema IS NULL)),CHECK(checkpoint IS NULL OR octet_length(checkpoint)<=65536),CHECK(jsonb_typeof(safe_pending_refs)='array')
);
CREATE TABLE agenteam_project.work_claims (
 work_kind text NOT NULL CHECK(work_kind IN ('creation','lifecycle')),work_id agenteam_project.safe_id NOT NULL,
 project_id agenteam_project.safe_id NOT NULL REFERENCES agenteam_project.projects(id) DEFERRABLE INITIALLY DEFERRED,
 process_id agenteam_project.safe_id NOT NULL,attempt_id agenteam_project.safe_id NOT NULL,fence bigint NOT NULL CHECK(fence>0),
 phase text NOT NULL CHECK(phase IN ('running','terminal')),PRIMARY KEY(work_kind,work_id)
);
CREATE TABLE agenteam_project.deletion_receipts (
 operation_id agenteam_project.safe_id PRIMARY KEY,deleted_project_id agenteam_project.safe_id NOT NULL UNIQUE,
 original_owner_user_id agenteam_project.safe_id NOT NULL,command_key_hash text NOT NULL CHECK(command_key_hash ~ '^sha256:[0-9a-f]{64}$'),
 request_digest text NOT NULL CHECK(request_digest ~ '^sha256:[0-9a-f]{64}$'),completed_at timestamptz(6) NOT NULL,status text NOT NULL DEFAULT 'completed' CHECK(status='completed')
);
CREATE INDEX project_deleted_owner_operation ON agenteam_project.deletion_receipts(original_owner_user_id,operation_id);
CREATE INDEX project_deleted_command ON agenteam_project.deletion_receipts(deleted_project_id,original_owner_user_id,command_key_hash);

ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_action_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_action_check CHECK (action IN (
  'secret.create','secret.update','secret.delete','secret.resolve','secret.master.register','secret.master.rotation.start','secret.master.rotation.complete','secret.master.rotation.failed','outbound.policy.update','outbound.access.deny',
  'object.upload.complete','object.upload.failed','object.delete','object.transfer.issue','object.transfer.complete','object.transfer.revoke','artifact.create','artifact.list','artifact.read','artifact.download','outbox.delivery.requeue','account.bootstrap','account.login','account.logout','account.invite.create','account.invite.revoke','account.invite.redeem','account.password.change','account.password.reset.request','account.password.reset.complete','account.profile.update','account.avatar.update','account.settings.update','smtp.settings.update','smtp.test.request','smtp.delivery','smtp.delivery.retry','project.create.accepted','project.create.completed','project.update','project.archive.accepted','project.archive.completed','project.restore','project.delete.accepted','project.lifecycle.retry'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_resource_kind_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_resource_kind_check CHECK (resource_kind IN ('secret','secret_master','secret_rotation','outbound_policy','agent','stored_object','object_transfer','artifact','artifact_collection','outbox_delivery','user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job','project','project_operation','project_creation'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_producer_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_producer_check CHECK (producer IN ('secret','secret.master','outbound.policy','outbound.access','object','artifact','outbox','account','account.mail','project'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_check1;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_check1 CHECK (
  (actor_kind='human' AND user_id IS NOT NULL AND session_id IS NOT NULL AND actor_project_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name IS NULL AND service_cause IS NULL)
  OR (actor_kind='agent_run' AND user_id IS NULL AND session_id IS NULL AND actor_project_id IS NOT NULL AND agent_id IS NOT NULL AND actor_execution_id IS NOT NULL AND service_name IS NULL AND service_cause IS NULL AND scope='project' AND actor_project_id=project_id AND execution_id=actor_execution_id)
  OR (actor_kind='service' AND user_id IS NULL AND session_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name IN ('secret','secret-maintenance','outbound','project-lifecycle','object','object-maintenance','account-bootstrap','account-auth','account-maintenance','account-mail') AND service_cause IS NOT NULL AND actor_project_id IS NOT DISTINCT FROM project_id)
 OR (actor_kind='service' AND user_id IS NULL AND session_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name='project-initialization' AND service_cause IS NOT NULL AND scope='project' AND actor_project_id=project_id AND producer='project' AND action='project.create.completed'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_check2;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_check2 CHECK (
 (resource_kind IN ('secret_master','outbound_policy') AND resource_id IS NULL)
 OR (resource_kind IN ('secret','secret_rotation','agent','stored_object','object_transfer','artifact','artifact_collection','outbox_delivery','user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job') AND resource_id IS NOT NULL)
 OR (resource_kind IN ('project','project_operation','project_creation') AND resource_id IS NOT NULL));

-- Existing account/content/outbox contract checks are unchanged. The new
-- closed branch cannot re-label an old action/resource or borrow its service.
-- +goose StatementBegin
CREATE FUNCTION agenteam_project.audit_version(v jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $fn$
 SELECT CASE WHEN jsonb_typeof(v)='string' AND (v#>>'{}') ~ '^[1-9][0-9]{0,18}$'
 THEN (v#>>'{}')::numeric<=9223372036854775807 ELSE false END
$fn$;
-- +goose StatementEnd
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_project_contract CHECK ((
 (producer<>'project' AND action NOT LIKE 'project.%' AND resource_kind NOT IN ('project','project_operation','project_creation') AND service_name IS DISTINCT FROM 'project-initialization')
 OR (
  producer='project' AND scope='project' AND outcome='success' AND ordinal=0
  AND project_id::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND resource_id::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND metadata->>'project_id'=project_id::text
  AND jsonb_typeof(metadata->'project_id')='string'
  AND jsonb_typeof(metadata->'initiator_id')='string'
  AND metadata->>'initiator_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND agenteam_project.audit_version(metadata->'project_version')
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND approval_id IS NULL
  AND (actor_kind<>'human' OR metadata->>'initiator_id'=user_id::text)
  AND (
   (action='project.create.accepted' AND resource_kind='project_creation' AND metadata ?& ARRAY['project_id','initiator_id','project_version','creation_id','creation_version'] AND metadata-ARRAY['project_id','initiator_id','project_version','creation_id','creation_version']='{}'::jsonb AND actor_kind='human' AND metadata->>'project_version'='1' AND resource_id::text=metadata->>'creation_id' AND agenteam_project.audit_version(metadata->'creation_version') AND operation_id IS NULL)
   OR    (action='project.create.completed' AND resource_kind='project_creation' AND metadata ?& ARRAY['project_id','initiator_id','project_version','creation_id','creation_version'] AND metadata-ARRAY['project_id','initiator_id','project_version','creation_id','creation_version']='{}'::jsonb AND actor_kind='service' AND service_name='project-initialization' AND service_cause=cause_ref AND service_cause=metadata->>'creation_id' AND metadata->>'project_version'='1' AND resource_id::text=metadata->>'creation_id' AND agenteam_project.audit_version(metadata->'creation_version') AND operation_id IS NULL)
   OR    (action='project.update' AND resource_kind='project' AND metadata ?& ARRAY['project_id','initiator_id','project_version','changed_fields'] AND metadata-ARRAY['project_id','initiator_id','project_version','changed_fields']='{}'::jsonb AND actor_kind='human' AND resource_id=project_id AND metadata->>'project_version'<>'1' AND metadata->'changed_fields' IN ('["name"]'::jsonb,'["description"]'::jsonb,'["description","name"]'::jsonb) AND operation_id IS NULL)
   OR    (action='project.archive.accepted' AND resource_kind='project_operation' AND metadata ?& ARRAY['project_id','initiator_id','project_version','operation_id','operation_version','from','to','action'] AND metadata-ARRAY['project_id','initiator_id','project_version','operation_id','operation_version','from','to','action']='{}'::jsonb AND actor_kind='human' AND metadata->>'from'='active' AND metadata->>'to'='archiving' AND metadata->>'action'='archive' AND resource_id::text=metadata->>'operation_id' AND agenteam_project.audit_version(metadata->'operation_version') AND (operation_id IS NULL OR operation_id::text=metadata->>'operation_id'))
   OR    (action='project.archive.completed' AND resource_kind='project_operation' AND metadata ?& ARRAY['project_id','initiator_id','project_version','operation_id','operation_version','from','to','action'] AND metadata-ARRAY['project_id','initiator_id','project_version','operation_id','operation_version','from','to','action']='{}'::jsonb AND actor_kind='service' AND service_name='project-lifecycle' AND service_cause=cause_ref AND service_cause=metadata->>'operation_id' AND metadata->>'from'='archiving' AND metadata->>'to'='archived' AND metadata->>'action'='archive' AND resource_id::text=metadata->>'operation_id' AND agenteam_project.audit_version(metadata->'operation_version') AND (operation_id IS NULL OR operation_id::text=metadata->>'operation_id'))
   OR    (action='project.restore' AND resource_kind='project' AND metadata ?& ARRAY['project_id','initiator_id','project_version','from','to','action'] AND metadata-ARRAY['project_id','initiator_id','project_version','from','to','action']='{}'::jsonb AND actor_kind='human' AND resource_id=project_id AND metadata->>'from'='archived' AND metadata->>'to'='active' AND metadata->>'action'='restore' AND operation_id IS NULL)
   OR    (action='project.delete.accepted' AND resource_kind='project_operation' AND metadata ?& ARRAY['project_id','initiator_id','project_version','operation_id','operation_version','from','to','action'] AND metadata-ARRAY['project_id','initiator_id','project_version','operation_id','operation_version','from','to','action']='{}'::jsonb AND actor_kind='human' AND metadata->>'from' IN ('active','archived') AND metadata->>'to'='deleting' AND metadata->>'action'='delete' AND resource_id::text=metadata->>'operation_id' AND agenteam_project.audit_version(metadata->'operation_version') AND (operation_id IS NULL OR operation_id::text=metadata->>'operation_id'))
   OR    (action='project.lifecycle.retry' AND resource_kind='project_operation' AND metadata ?& ARRAY['project_id','initiator_id','project_version','operation_id','operation_version','action'] AND metadata-ARRAY['project_id','initiator_id','project_version','operation_id','operation_version','action']='{}'::jsonb AND actor_kind='human' AND metadata->>'action' IN ('archive','delete') AND resource_id::text=metadata->>'operation_id' AND agenteam_project.audit_version(metadata->'operation_version') AND (operation_id IS NULL OR operation_id::text=metadata->>'operation_id'))
  )
 )
) IS TRUE);
