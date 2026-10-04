-- agenteam:transaction tx
-- +goose Up

CREATE SCHEMA agenteam_outbox;
CREATE DOMAIN agenteam_outbox.safe_id AS uuid CONSTRAINT outbox_safe_id_check CHECK (VALUE::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$');
CREATE SEQUENCE agenteam_outbox.outbox_sequence AS bigint MINVALUE 1 MAXVALUE 9223372036854775807 START 1 INCREMENT 1 CACHE 1 NO CYCLE;
CREATE TABLE agenteam_outbox.control (
 singleton boolean NOT NULL DEFAULT true CONSTRAINT outbox_control_pk PRIMARY KEY CONSTRAINT outbox_control_singleton CHECK (singleton),
 format integer NOT NULL CONSTRAINT outbox_control_format CHECK (format=1)
);
INSERT INTO agenteam_outbox.control(singleton,format) VALUES(true,1);

CREATE TABLE agenteam_outbox.events (
 id agenteam_outbox.safe_id CONSTRAINT outbox_events_pk PRIMARY KEY,
 format integer NOT NULL DEFAULT 1 CONSTRAINT outbox_events_format CHECK (format=1),
 sequence bigint NOT NULL DEFAULT nextval('agenteam_outbox.outbox_sequence') CONSTRAINT outbox_events_sequence_unique UNIQUE CONSTRAINT outbox_events_sequence_check CHECK(sequence>0),
 producer text NOT NULL CONSTRAINT outbox_events_producer_check CHECK(producer ~ '^[a-z][a-z0-9_.-]{0,127}$'),
 event_type text NOT NULL CONSTRAINT outbox_events_type_check CHECK(event_type ~ '^[a-z][a-z0-9_.-]{0,127}$'),
 schema_version bigint NOT NULL CONSTRAINT outbox_events_schema_check CHECK(schema_version BETWEEN 1 AND 4294967295),
 scope text NOT NULL,
 project_id agenteam_outbox.safe_id,
 aggregate_type text NOT NULL CONSTRAINT outbox_events_aggregate_check CHECK(aggregate_type ~ '^[a-z][a-z0-9_.-]{0,127}$'),
 aggregate_id agenteam_outbox.safe_id NOT NULL,
 aggregate_version bigint,
 aggregate_sequence bigint,
 occurred_at timestamptz(6) NOT NULL,
 header jsonb NOT NULL CONSTRAINT outbox_events_header_check CHECK(jsonb_typeof(header)='object' AND octet_length(header::text)<=4096),
 payload bytea NOT NULL CONSTRAINT outbox_events_payload_check CHECK(octet_length(payload) BETWEEN 1 AND 65536),
 semantic_digest text NOT NULL CONSTRAINT outbox_events_digest_check CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 stable_actor text NOT NULL CONSTRAINT outbox_events_actor_check CHECK(octet_length(stable_actor) BETWEEN 1 AND 1024),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT outbox_events_scope_check CHECK((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id IS NOT NULL)),
 CONSTRAINT outbox_events_version_check CHECK((aggregate_version IS NOT NULL OR aggregate_sequence IS NOT NULL) AND (aggregate_version IS NULL OR aggregate_version>0) AND (aggregate_sequence IS NULL OR aggregate_sequence>0))
);
CREATE INDEX outbox_events_project_sequence ON agenteam_outbox.events(project_id,sequence);
CREATE INDEX outbox_events_aggregate ON agenteam_outbox.events(scope,project_id,aggregate_type,aggregate_id,sequence);
-- +goose StatementBegin
CREATE FUNCTION agenteam_outbox.reject_event_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'immutable outbox event' USING ERRCODE='23514';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER outbox_events_immutable BEFORE UPDATE ON agenteam_outbox.events FOR EACH ROW EXECUTE FUNCTION agenteam_outbox.reject_event_update();

CREATE TABLE agenteam_outbox.handlers (
 id text CONSTRAINT outbox_handlers_pk PRIMARY KEY CONSTRAINT outbox_handlers_id_check CHECK(id ~ '^[a-z][a-z0-9_.-]{0,127}$'),
 format integer NOT NULL DEFAULT 1 CONSTRAINT outbox_handlers_format CHECK(format=1),
 effect text NOT NULL CONSTRAINT outbox_handlers_effect_check CHECK(effect IN ('canonical_converge','domain_ingress')),
 ordering_policy text NOT NULL CONSTRAINT outbox_handlers_ordering_check CHECK(ordering_policy IN ('version_guarded','canonical_reconcile')),
 declaration_digest text NOT NULL CONSTRAINT outbox_handlers_digest_check CHECK(declaration_digest ~ '^sha256:[0-9a-f]{64}$'),
 registered_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE agenteam_outbox.subscriptions (
 handler_id text NOT NULL CONSTRAINT outbox_subscriptions_handler_fk REFERENCES agenteam_outbox.handlers(id),
 event_type text NOT NULL CONSTRAINT outbox_subscriptions_type_check CHECK(event_type ~ '^[a-z][a-z0-9_.-]{0,127}$'),
 format integer NOT NULL DEFAULT 1 CONSTRAINT outbox_subscriptions_format CHECK(format=1),
 accepted_versions bigint[] NOT NULL CONSTRAINT outbox_subscriptions_versions_check CHECK(cardinality(accepted_versions) BETWEEN 1 AND 128 AND array_ndims(accepted_versions)=1 AND array_position(accepted_versions,NULL) IS NULL AND 0<ALL(accepted_versions) AND 4294967295>=ALL(accepted_versions)),
 accepted_after_sequence bigint NOT NULL CONSTRAINT outbox_subscriptions_boundary_check CHECK(accepted_after_sequence>=0),
 registered_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 registration_id agenteam_outbox.safe_id NOT NULL,
 CONSTRAINT outbox_subscriptions_pk PRIMARY KEY(handler_id,event_type)
);
CREATE INDEX outbox_subscriptions_type ON agenteam_outbox.subscriptions(event_type,accepted_after_sequence,handler_id);

CREATE TABLE agenteam_outbox.deliveries (
 id agenteam_outbox.safe_id CONSTRAINT outbox_deliveries_pk PRIMARY KEY,
 format integer NOT NULL DEFAULT 1 CONSTRAINT outbox_deliveries_format CHECK(format=1),
 event_id agenteam_outbox.safe_id NOT NULL CONSTRAINT outbox_deliveries_event_fk REFERENCES agenteam_outbox.events(id),
 handler_id text NOT NULL CONSTRAINT outbox_deliveries_handler_fk REFERENCES agenteam_outbox.handlers(id),
 scope text NOT NULL,
 project_id agenteam_outbox.safe_id,
 phase text NOT NULL DEFAULT 'pending' CONSTRAINT outbox_deliveries_phase_check CHECK(phase IN ('pending','processing','retry_wait','succeeded','failed','dead_letter')),
 version bigint NOT NULL DEFAULT 1 CONSTRAINT outbox_deliveries_version_check CHECK(version>0),
 redrive_cycle bigint NOT NULL DEFAULT 0 CONSTRAINT outbox_deliveries_cycle_check CHECK(redrive_cycle>=0),
 cycle_attempts bigint NOT NULL DEFAULT 0,
 lifetime_attempts bigint NOT NULL DEFAULT 0,
 next_attempt_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 current_attempt_id agenteam_outbox.safe_id,
 fence bigint NOT NULL DEFAULT 0 CONSTRAINT outbox_deliveries_fence_check CHECK(fence>=0),
 safe_reason text,
 recovery_pass bigint NOT NULL DEFAULT 0 CONSTRAINT outbox_deliveries_pass_check CHECK(recovery_pass>=0),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 last_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 CONSTRAINT outbox_deliveries_event_handler_unique UNIQUE(event_id,handler_id),
 CONSTRAINT outbox_deliveries_identity_unique UNIQUE(id,event_id,handler_id),
 CONSTRAINT outbox_deliveries_scope_check CHECK((scope='system' AND project_id IS NULL) OR (scope='project' AND project_id IS NOT NULL)),
 CONSTRAINT outbox_deliveries_counts_check CHECK(cycle_attempts>=0 AND cycle_attempts<=8 AND lifetime_attempts>=cycle_attempts),
 CONSTRAINT outbox_deliveries_attempt_check CHECK((current_attempt_id IS NULL AND fence=0 AND phase='pending') OR (current_attempt_id IS NOT NULL AND fence>0)),
 CONSTRAINT outbox_deliveries_reason_check CHECK(safe_reason IS NULL OR safe_reason IN ('handler_retry','unavailable','deadline','handler_panic','unsupported_schema','invalid_event','source_terminal','authorization_changed','project_stopped','unbound_handler','commit_unknown','process_unconfirmed','shutdown'))
);
CREATE INDEX outbox_deliveries_due ON agenteam_outbox.deliveries(next_attempt_at,id) WHERE phase IN ('pending','retry_wait');
CREATE INDEX outbox_deliveries_project ON agenteam_outbox.deliveries(project_id,phase,id);
CREATE INDEX outbox_deliveries_handler_phase ON agenteam_outbox.deliveries(handler_id,phase,last_at,id);
CREATE INDEX outbox_deliveries_recovery ON agenteam_outbox.deliveries(recovery_pass,id) WHERE phase='processing';

CREATE TABLE agenteam_outbox.attempts (
 id agenteam_outbox.safe_id CONSTRAINT outbox_attempts_pk PRIMARY KEY,
 format integer NOT NULL DEFAULT 1 CONSTRAINT outbox_attempts_format CHECK(format=1),
 delivery_id agenteam_outbox.safe_id NOT NULL CONSTRAINT outbox_attempts_delivery_fk REFERENCES agenteam_outbox.deliveries(id),
 process_id agenteam_outbox.safe_id NOT NULL,
 fence bigint NOT NULL CONSTRAINT outbox_attempts_fence_check CHECK(fence>0),
 redrive_cycle bigint NOT NULL CONSTRAINT outbox_attempts_cycle_check CHECK(redrive_cycle>=0),
 cycle_number bigint NOT NULL CONSTRAINT outbox_attempts_number_check CHECK(cycle_number BETWEEN 1 AND 8),
 lifetime_number bigint NOT NULL CONSTRAINT outbox_attempts_lifetime_check CHECK(lifetime_number>=cycle_number),
 started_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 deadline timestamptz(6) NOT NULL,
 handler_returned_at timestamptz(6),
 joined_at timestamptz(6),
 finished_at timestamptz(6),
 commit_unknown boolean NOT NULL DEFAULT false,
 checkpoint text NOT NULL DEFAULT 'claimed' CONSTRAINT outbox_attempts_checkpoint_check CHECK(checkpoint IN ('claimed','applying','unknown','succeeded','not_committed','retry','failed','dead_letter','stopped')),
 safe_reason text,
 CONSTRAINT outbox_attempts_delivery_fence_unique UNIQUE(delivery_id,fence),
 CONSTRAINT outbox_attempts_identity_unique UNIQUE(id,delivery_id,fence),
 CONSTRAINT outbox_attempts_deadline_check CHECK(deadline>started_at),
 CONSTRAINT outbox_attempts_join_check CHECK(joined_at IS NULL OR handler_returned_at IS NOT NULL),
 CONSTRAINT outbox_attempts_reason_check CHECK(safe_reason IS NULL OR safe_reason IN ('handler_retry','unavailable','deadline','handler_panic','unsupported_schema','invalid_event','source_terminal','authorization_changed','project_stopped','unbound_handler','commit_unknown','process_unconfirmed','shutdown'))
);
ALTER TABLE agenteam_outbox.deliveries ADD CONSTRAINT outbox_deliveries_current_attempt_fk FOREIGN KEY(current_attempt_id,id,fence) REFERENCES agenteam_outbox.attempts(id,delivery_id,fence) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX outbox_attempts_process ON agenteam_outbox.attempts(process_id,id) WHERE joined_at IS NULL;

CREATE TABLE agenteam_outbox.processed (
 event_id agenteam_outbox.safe_id NOT NULL,
 handler_id text NOT NULL,
 format integer NOT NULL DEFAULT 1 CONSTRAINT outbox_processed_format CHECK(format=1),
 delivery_id agenteam_outbox.safe_id NOT NULL,
 attempt_id agenteam_outbox.safe_id NOT NULL,
 fence bigint NOT NULL CONSTRAINT outbox_processed_fence_check CHECK(fence>0),
 processed_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 result_digest text NOT NULL CONSTRAINT outbox_processed_digest_check CHECK(result_digest ~ '^sha256:[0-9a-f]{64}$'),
 CONSTRAINT outbox_processed_pk PRIMARY KEY(event_id,handler_id),
 CONSTRAINT outbox_processed_delivery_fk FOREIGN KEY(delivery_id,event_id,handler_id) REFERENCES agenteam_outbox.deliveries(id,event_id,handler_id),
 CONSTRAINT outbox_processed_attempt_fk FOREIGN KEY(attempt_id,delivery_id,fence) REFERENCES agenteam_outbox.attempts(id,delivery_id,fence)
);
CREATE TABLE agenteam_outbox.requeue_commands (
 command_hash text CONSTRAINT outbox_requeue_pk PRIMARY KEY CONSTRAINT outbox_requeue_hash_check CHECK(command_hash ~ '^sha256:[0-9a-f]{64}$'),
 format integer NOT NULL DEFAULT 1 CONSTRAINT outbox_requeue_format CHECK(format=1),
 semantic_digest text NOT NULL CONSTRAINT outbox_requeue_digest_check CHECK(semantic_digest ~ '^sha256:[0-9a-f]{64}$'),
 user_id agenteam_outbox.safe_id NOT NULL,
 delivery_id agenteam_outbox.safe_id NOT NULL CONSTRAINT outbox_requeue_delivery_fk REFERENCES agenteam_outbox.deliveries(id),
 from_state text NOT NULL CONSTRAINT outbox_requeue_from_check CHECK(from_state IN ('failed','dead_letter')),
 expected_version bigint NOT NULL CONSTRAINT outbox_requeue_expected_check CHECK(expected_version>0),
 redrive_cycle bigint NOT NULL CONSTRAINT outbox_requeue_cycle_check CHECK(redrive_cycle>0),
 result_version bigint NOT NULL CONSTRAINT outbox_requeue_version_check CHECK(result_version>expected_version),
 reason_code text NOT NULL CONSTRAINT outbox_requeue_reason_check CHECK(reason_code IN ('operator_retry','schema_available','dependency_restored')),
 -- The typed Audit append and this receipt share one transaction. Audit is
 -- cleaned before Outbox, so retain its ID as a fact without a deletion FK.
 audit_id uuid NOT NULL,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE agenteam_outbox.project_lifecycle (
 project_id agenteam_outbox.safe_id CONSTRAINT outbox_lifecycle_pk PRIMARY KEY,
 format integer NOT NULL DEFAULT 1 CONSTRAINT outbox_lifecycle_format CHECK(format=1),
 operation_id agenteam_outbox.safe_id NOT NULL,
 action text NOT NULL CONSTRAINT outbox_lifecycle_action_check CHECK(action IN ('archive','delete')),
 project_version bigint NOT NULL CONSTRAINT outbox_lifecycle_version_check CHECK(project_version>0),
 phase text NOT NULL CONSTRAINT outbox_lifecycle_phase_check CHECK(phase IN ('stopping','stopped','cleaning','completed')),
 recovery_pass bigint NOT NULL DEFAULT 0 CONSTRAINT outbox_lifecycle_pass_check CHECK(recovery_pass>=0),
 scan_sequence bigint NOT NULL DEFAULT 0 CONSTRAINT outbox_lifecycle_scan_check CHECK(scan_sequence>=0),
 started_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz(6),
 CONSTRAINT outbox_lifecycle_complete_check CHECK((phase='completed')=(completed_at IS NOT NULL)),
 CONSTRAINT outbox_lifecycle_archive_check CHECK(action='delete' OR phase IN ('stopping','stopped'))
);

-- Extend Audit without granting the delivery service a write exemption.
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_action_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_action_check CHECK (action IN (
  'secret.create','secret.update','secret.delete','secret.resolve','secret.master.register','secret.master.rotation.start','secret.master.rotation.complete','secret.master.rotation.failed','outbound.policy.update','outbound.access.deny',
  'object.upload.complete','object.upload.failed','object.delete','object.transfer.issue','object.transfer.complete','object.transfer.revoke','artifact.create','artifact.list','artifact.read','artifact.download','outbox.delivery.requeue'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_resource_kind_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_resource_kind_check CHECK (resource_kind IN ('secret','secret_master','secret_rotation','outbound_policy','agent','stored_object','object_transfer','artifact','artifact_collection','outbox_delivery'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_producer_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_producer_check CHECK (producer IN ('secret','secret.master','outbound.policy','outbound.access','object','artifact','outbox'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_check1;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_check1 CHECK (
  (actor_kind='human' AND user_id IS NOT NULL AND session_id IS NOT NULL AND actor_project_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name IS NULL AND service_cause IS NULL)
  OR (actor_kind='agent_run' AND user_id IS NULL AND session_id IS NULL AND actor_project_id IS NOT NULL AND agent_id IS NOT NULL AND actor_execution_id IS NOT NULL AND service_name IS NULL AND service_cause IS NULL AND scope='project' AND actor_project_id=project_id AND execution_id=actor_execution_id)
  OR (actor_kind='service' AND user_id IS NULL AND session_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name IN ('secret','secret-maintenance','outbound','project-lifecycle','object','object-maintenance') AND service_cause IS NOT NULL AND actor_project_id IS NOT DISTINCT FROM project_id));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_check2;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_check2 CHECK (
  (resource_kind IN ('secret_master','outbound_policy') AND resource_id IS NULL)
  OR (resource_kind IN ('secret','secret_rotation','agent','stored_object','object_transfer','artifact','artifact_collection','outbox_delivery') AND resource_id IS NOT NULL));
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_outbox_contract CHECK (
 (producer<>'outbox' AND action<>'outbox.delivery.requeue' AND resource_kind<>'outbox_delivery')
 OR (producer='outbox' AND action='outbox.delivery.requeue' AND resource_kind='outbox_delivery' AND actor_kind='human' AND outcome='success'
  AND metadata ?& ARRAY['delivery_id','event_id','handler_id','from_state','redrive_cycle','reason_code']
  AND metadata-ARRAY['delivery_id','event_id','handler_id','from_state','redrive_cycle','reason_code']='{}'::jsonb
  AND jsonb_typeof(metadata->'delivery_id')='string' AND metadata->>'delivery_id'=resource_id::text
  AND jsonb_typeof(metadata->'event_id')='string' AND metadata->>'event_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
  AND jsonb_typeof(metadata->'handler_id')='string' AND metadata->>'handler_id' ~ '^[a-z][a-z0-9_.-]{0,127}$'
  AND jsonb_typeof(metadata->'from_state')='string' AND metadata->>'from_state' IN ('failed','dead_letter')
  AND jsonb_typeof(metadata->'redrive_cycle')='string' AND metadata->>'redrive_cycle' ~ '^[1-9][0-9]{0,18}$'
  AND (metadata->>'redrive_cycle')::numeric<=9223372036854775807
  AND jsonb_typeof(metadata->'reason_code')='string' AND metadata->>'reason_code' IN ('operator_retry','schema_available','dependency_restored'))
);
