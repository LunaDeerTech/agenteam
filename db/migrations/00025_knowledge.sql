-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_knowledge;
CREATE DOMAIN agenteam_knowledge.safe_id AS uuid
 CHECK(VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');

CREATE TABLE agenteam_knowledge.documents (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL,
 parent_document_id agenteam_knowledge.safe_id,
 title text CHECK(title IS NULL OR (char_length(title) BETWEEN 1 AND 512 AND title !~ '[[:cntrl:]]')),
 content_version bigint NOT NULL CHECK(content_version>0),
 source_kind text CHECK(source_kind IN ('text','file')),
 media_type text,
 current_object_id agenteam_knowledge.safe_id,
 current_upload_id agenteam_knowledge.safe_id,
 status text NOT NULL CHECK(status IN ('active','deleted')),
 indexing_status text CHECK(indexing_status IN ('pending','processing','ready','failed')),
 creator_user_id agenteam_knowledge.safe_id,
 created_at timestamptz(6),
 updated_at timestamptz(6),
 deleted_at timestamptz(6),
 CONSTRAINT documents_project_id_key UNIQUE(project_id,id),
 CONSTRAINT documents_parent_fk FOREIGN KEY(project_id,parent_document_id)
  REFERENCES agenteam_knowledge.documents(project_id,id) ON DELETE RESTRICT,
 CONSTRAINT documents_not_self CHECK(parent_document_id IS NULL OR parent_document_id<>id),
 CONSTRAINT documents_current_shape CHECK(((status='active'
  AND title IS NOT NULL AND current_object_id IS NOT NULL AND current_upload_id IS NOT NULL
  AND indexing_status IS NOT NULL AND creator_user_id IS NOT NULL
  AND created_at IS NOT NULL AND updated_at IS NOT NULL AND updated_at>=created_at AND deleted_at IS NULL
  AND ((source_kind='text' AND media_type IN ('text/plain','text/markdown'))
    OR (source_kind='file' AND media_type IN ('application/pdf','application/vnd.openxmlformats-officedocument.wordprocessingml.document'))))
 OR (status='deleted' AND deleted_at IS NOT NULL AND parent_document_id IS NULL AND title IS NULL
  AND source_kind IS NULL AND media_type IS NULL AND current_object_id IS NULL AND current_upload_id IS NULL
  AND indexing_status IS NULL AND creator_user_id IS NULL AND created_at IS NULL AND updated_at IS NULL)) IS TRUE)
);
CREATE INDEX documents_children ON agenteam_knowledge.documents(project_id,parent_document_id,title COLLATE "C",id) WHERE status='active';
CREATE INDEX documents_project_titles ON agenteam_knowledge.documents(project_id,title COLLATE "C",id) WHERE status='active';

CREATE TABLE agenteam_knowledge.commands (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL,
 document_id agenteam_knowledge.safe_id NOT NULL,
 actor_user_id agenteam_knowledge.safe_id NOT NULL,
 command_name text NOT NULL CHECK(command_name IN ('create','update','move','delete-subtree')),
 command_key text NOT NULL CHECK(octet_length(command_key) BETWEEN 1 AND 128 AND command_key ~ '^[!-~]+$'),
 semantic_digest text NOT NULL CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 request jsonb CHECK(request IS NULL OR (jsonb_typeof(request)='object' AND octet_length(request::text)<=524288)),
 plan jsonb CHECK(plan IS NULL OR (jsonb_typeof(plan)='object' AND octet_length(plan::text)<=4194304)),
 state text NOT NULL CHECK(state IN ('planned','completed')),
 receipt jsonb CHECK(receipt IS NULL OR (jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=4194304)),
 created_at timestamptz(6) NOT NULL,
 committed_at timestamptz(6),
 CONSTRAINT commands_project_id_key UNIQUE(project_id,id),
 CONSTRAINT commands_identity_key UNIQUE(project_id,command_name,command_key),
 CONSTRAINT commands_result_shape CHECK(((state='planned' AND request IS NOT NULL AND committed_at IS NULL AND receipt IS NULL)
  OR (state='completed' AND request IS NULL AND committed_at IS NOT NULL AND committed_at>=created_at AND receipt IS NOT NULL)) IS TRUE)
);
CREATE UNIQUE INDEX commands_create_target ON agenteam_knowledge.commands(document_id) WHERE command_name='create';

CREATE TABLE agenteam_knowledge.command_events (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL,
 command_id agenteam_knowledge.safe_id NOT NULL,
 document_id agenteam_knowledge.safe_id NOT NULL,
 header jsonb NOT NULL CHECK(jsonb_typeof(header)='object' AND octet_length(header::text)<=4096),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=4096),
 event_type text NOT NULL CHECK(event_type IN ('knowledge.content_changed','knowledge.deleted')),
 CONSTRAINT command_events_command_fk FOREIGN KEY(project_id,command_id) REFERENCES agenteam_knowledge.commands(project_id,id) ON DELETE RESTRICT,
 CONSTRAINT command_events_target_key UNIQUE(command_id,document_id)
);

-- Source is an exact semantic descriptor, never a body or an open reader.
CREATE TABLE agenteam_knowledge.publications (
 command_id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL,
 document_id agenteam_knowledge.safe_id NOT NULL,
 source jsonb CHECK(source IS NULL OR (jsonb_typeof(source)='object' AND octet_length(source::text)<=16384)),
 source_project_id agenteam_knowledge.safe_id,
 source_lease_id agenteam_knowledge.safe_id,
 object_id agenteam_knowledge.safe_id,
 upload_id agenteam_knowledge.safe_id,
 attempt_id agenteam_knowledge.safe_id,
 object_meta jsonb CHECK(object_meta IS NULL OR (jsonb_typeof(object_meta)='object' AND octet_length(object_meta::text)<=4096)),
 phase text NOT NULL CHECK(phase IN ('planned','reserved','uploaded','published','cancelled')),
 CONSTRAINT publications_command_fk FOREIGN KEY(project_id,command_id) REFERENCES agenteam_knowledge.commands(project_id,id) ON DELETE RESTRICT,
 CONSTRAINT publications_upload_shape CHECK((object_id IS NULL AND upload_id IS NULL AND attempt_id IS NULL)
  OR (object_id IS NOT NULL AND upload_id IS NOT NULL AND attempt_id IS NOT NULL)),
 CONSTRAINT publications_phase_shape CHECK(((phase='planned' AND object_id IS NULL AND object_meta IS NULL AND source IS NOT NULL)
  OR (phase IN ('reserved','uploaded') AND object_id IS NOT NULL AND source IS NOT NULL)
  OR (phase='published' AND object_id IS NOT NULL AND object_meta IS NOT NULL AND source IS NULL AND source_lease_id IS NULL)
  OR (phase='cancelled' AND source IS NULL AND source_lease_id IS NULL)) IS TRUE)
);

CREATE TABLE agenteam_knowledge.work_claims (
 command_id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL,
 source_project_id agenteam_knowledge.safe_id,
 process_id agenteam_knowledge.safe_id NOT NULL,
 attempt_id agenteam_knowledge.safe_id NOT NULL UNIQUE,
 fence bigint NOT NULL CHECK(fence>0),
 phase text NOT NULL CHECK(phase IN ('active','joined')),
 CONSTRAINT work_claims_command_fk FOREIGN KEY(project_id,command_id) REFERENCES agenteam_knowledge.commands(project_id,id) ON DELETE RESTRICT
);
CREATE INDEX work_claims_project ON agenteam_knowledge.work_claims(project_id) WHERE phase='active';
CREATE INDEX work_claims_source_project ON agenteam_knowledge.work_claims(source_project_id) WHERE phase='active';

CREATE TABLE agenteam_knowledge.object_cleanup (
 id agenteam_knowledge.safe_id PRIMARY KEY,
 project_id agenteam_knowledge.safe_id NOT NULL,
 document_id agenteam_knowledge.safe_id NOT NULL,
 command_id agenteam_knowledge.safe_id NOT NULL,
 object_id agenteam_knowledge.safe_id NOT NULL,
 upload_id agenteam_knowledge.safe_id NOT NULL,
 reason text NOT NULL CHECK(reason IN ('replaced_object','cancelled_upload','owner_deleted')),
 phase text NOT NULL CHECK(phase IN ('reference','object','completed')),
 CONSTRAINT object_cleanup_command_fk FOREIGN KEY(project_id,command_id) REFERENCES agenteam_knowledge.commands(project_id,id) ON DELETE RESTRICT,
 CONSTRAINT object_cleanup_exact_key UNIQUE(command_id,object_id,upload_id)
);
CREATE INDEX object_cleanup_pending ON agenteam_knowledge.object_cleanup(project_id,id) WHERE phase<>'completed';

-- Preserve the exact released CHECK expressions, including earlier domains'
-- additions. Only the four named closed lists gain Knowledge's explicit arm.
-- +goose StatementBegin
DO $$
DECLARE item record; expression text;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('audit_records_action_check','action=''knowledge.delete_subtree'''),
  ('audit_records_resource_kind_check','resource_kind=''knowledge_document'''),
  ('audit_records_producer_check','producer=''knowledge'''),
  ('audit_records_check2','resource_kind=''knowledge_document'' AND resource_id IS NOT NULL')
 ) AS additions(name,extra) LOOP
  SELECT pg_get_expr(conbin,conrelid) INTO STRICT expression FROM pg_constraint
   WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname=item.name AND contype='c';
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT %I',item.name);
  EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT %I CHECK ((%s) OR (%s))',item.name,expression,item.extra);
 END LOOP;
END;
$$;
-- +goose StatementEnd
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_knowledge_contract CHECK ((
 (producer<>'knowledge' AND action<>'knowledge.delete_subtree' AND resource_kind<>'knowledge_document')
 OR (producer='knowledge' AND action='knowledge.delete_subtree' AND resource_kind='knowledge_document'
  AND scope='project' AND actor_kind='human' AND outcome='success' AND ordinal=0
  AND cause_ref ~ '^sha256:[0-9a-f]{64}$'
  AND metadata=jsonb_build_object('project_id',project_id::text,'root_id',resource_id::text,
   'initiator_id',user_id::text,'scope_digest',metadata->'scope_digest','deleted_count',metadata->'deleted_count')
  AND jsonb_typeof(metadata->'scope_digest')='string' AND metadata->>'scope_digest' ~ '^sha256:[0-9a-f]{64}$'
  AND jsonb_typeof(metadata->'deleted_count')='string'
  AND CASE WHEN metadata->>'deleted_count' ~ '^[1-9][0-9]{0,18}$'
   THEN (metadata->>'deleted_count')::numeric<=9223372036854775807 ELSE false END
  AND tool_id IS NULL AND execution_id IS NULL AND tool_call_id IS NULL AND operation_id IS NULL
  AND request_id IS NULL AND approval_id IS NULL AND runner_id IS NULL)) IS TRUE);
