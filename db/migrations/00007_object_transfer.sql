-- agenteam:transaction tx
-- +goose Up

-- Historical candidates retain all their original constraints. An external
-- staging attempt has neither a fictitious sealed spool nor a Central writer.
ALTER TABLE agenteam_object.upload_attempts ADD COLUMN kind text NOT NULL DEFAULT 'private_candidate';
ALTER TABLE agenteam_object.upload_attempts ADD COLUMN transfer_id agenteam_object.safe_id;
ALTER TABLE agenteam_object.upload_attempts ALTER COLUMN process_id DROP NOT NULL;
ALTER TABLE agenteam_object.upload_attempts ALTER COLUMN spool_payload_id DROP NOT NULL;
ALTER TABLE agenteam_object.upload_attempts DROP CONSTRAINT upload_attempts_candidate_key_check;
ALTER TABLE agenteam_object.upload_attempts ADD CONSTRAINT attempt_storage_kind CHECK (
 (kind='private_candidate' AND process_id IS NOT NULL AND spool_payload_id IS NOT NULL
  AND transfer_id IS NULL AND candidate_key ~ '^candidate/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$')
 OR (kind='runner_staging' AND process_id IS NULL AND spool_payload_id IS NULL
  AND transfer_id IS NOT NULL AND phase NOT IN ('verified','published')
  AND candidate_key ~ '^staging/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'));

CREATE TABLE agenteam_object.object_transfers (
 id agenteam_object.safe_id PRIMARY KEY,
 issue_hash bytea NOT NULL UNIQUE CHECK (octet_length(issue_hash)=32),
 issue_key text NOT NULL,
 issue_request_id agenteam_object.safe_id NOT NULL,
 issue_expected bigint CHECK (issue_expected>0),
 semantic_digest bytea NOT NULL CHECK (octet_length(semantic_digest)=32),
 actor_json jsonb NOT NULL,
 stable_actor text NOT NULL,
 project_id agenteam_object.safe_id NOT NULL,
 owner_kind text NOT NULL,
 owner_id agenteam_object.safe_id NOT NULL,
 runner_id agenteam_object.safe_id NOT NULL,
 operation_id agenteam_object.safe_id NOT NULL,
 runner_generation bigint NOT NULL CHECK (runner_generation>0),
 operation_version bigint NOT NULL CHECK (operation_version>0),
 execution_id agenteam_object.safe_id NOT NULL,
 direction text NOT NULL CHECK (direction IN ('get','put')),
 object_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.objects(id),
 upload_id agenteam_object.safe_id REFERENCES agenteam_object.uploads(id),
 upload_request_id agenteam_object.safe_id,
 upload_key text,
 upload_expected bigint CHECK (upload_expected>0),
 staging_id agenteam_object.safe_id UNIQUE REFERENCES agenteam_object.upload_attempts(id) DEFERRABLE INITIALLY DEFERRED,
 candidate_id agenteam_object.safe_id REFERENCES agenteam_object.upload_attempts(id),
 lease_id agenteam_object.safe_id NOT NULL UNIQUE REFERENCES agenteam_object.object_leases(id) DEFERRABLE INITIALLY DEFERRED,
 source_lease_id agenteam_object.safe_id REFERENCES agenteam_object.object_leases(id),
 source_process_id agenteam_object.safe_id,
 media_type text NOT NULL CHECK (octet_length(media_type) BETWEEN 1 AND 256),
 byte_size bigint NOT NULL CHECK (byte_size BETWEEN 0 AND 1073741824),
 sha256 bytea NOT NULL CHECK (octet_length(sha256)=32),
 candidate_key text CHECK (candidate_key ~ '^candidate/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 duration_seconds bigint NOT NULL CHECK (duration_seconds BETWEEN 1 AND 300),
 expires_at timestamptz(0) NOT NULL,
 phase text NOT NULL CHECK (phase IN ('issued','completing','complete','failed','unknown')),
 version bigint NOT NULL DEFAULT 1 CHECK (version>0),
 revoked_at timestamptz(6),
 completed_evidence agenteam_object.safe_id,
 completed_digest bytea CHECK (octet_length(completed_digest)=32),
 retirement_evidence agenteam_object.safe_id,
 retirement_kind text CHECK (retirement_kind IN ('completed','stopped')),
 retirement_digest bytea CHECK (octet_length(retirement_digest)=32),
 retirement_pending agenteam_object.safe_id,
 retirement_pending_kind text CHECK (retirement_pending_kind IN ('completed','stopped')),
 cancel_key text,
 cancel_digest bytea CHECK (octet_length(cancel_digest)=32),
 cleanup_gate boolean NOT NULL DEFAULT false,
 recovery_pass bigint NOT NULL DEFAULT 0 CHECK (recovery_pass>=0),
 fault_code text,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 CHECK ((direction='get' AND upload_id IS NULL AND staging_id IS NULL AND candidate_id IS NULL AND upload_key IS NULL AND upload_request_id IS NULL AND candidate_key IS NOT NULL)
     OR (direction='put' AND upload_id IS NOT NULL AND staging_id IS NOT NULL AND upload_key IS NOT NULL AND upload_request_id IS NOT NULL AND candidate_key IS NULL)),
 CHECK ((source_lease_id IS NULL)=(source_process_id IS NULL)),
 CHECK ((completed_evidence IS NULL)=(completed_digest IS NULL)),
 CHECK ((retirement_evidence IS NULL)=(retirement_digest IS NULL)),
 CHECK ((retirement_evidence IS NULL)=(retirement_kind IS NULL)),
 CHECK ((retirement_pending IS NULL)=(retirement_pending_kind IS NULL)),
 CHECK (phase<>'complete' OR completed_evidence IS NOT NULL)
);
ALTER TABLE agenteam_object.upload_attempts ADD CONSTRAINT staging_transfer_fk FOREIGN KEY(transfer_id) REFERENCES agenteam_object.object_transfers(id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX transfer_project_recovery ON agenteam_object.object_transfers(project_id,recovery_pass,id);
CREATE INDEX transfer_fair_recovery ON agenteam_object.object_transfers(recovery_pass,id);

CREATE TABLE agenteam_object.process_claims (
 process_id agenteam_object.safe_id PRIMARY KEY,
 store_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.store_identity(instance_id),
 deployment_id agenteam_object.safe_id NOT NULL,
 spool_identity bytea NOT NULL CHECK (octet_length(spool_identity)=32),
 spool_device bigint NOT NULL,
 spool_inode bigint NOT NULL,
 claim_device bigint NOT NULL,
 claim_inode bigint NOT NULL,
 host_identity bytea NOT NULL CHECK (octet_length(host_identity)=32),
 boot_id text NOT NULL,
 claim_nonce bytea NOT NULL CHECK (octet_length(claim_nonce)=32),
 state text NOT NULL CHECK (state IN ('claimed','stopped')),
 stopped_at timestamptz(6),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 CHECK ((state='stopped')=(stopped_at IS NOT NULL))
);

CREATE TABLE agenteam_object.startup_probes (
 id agenteam_object.safe_id PRIMARY KEY,
 process_id agenteam_object.safe_id NOT NULL REFERENCES agenteam_object.process_claims(process_id),
 storage_key text NOT NULL UNIQUE CHECK (storage_key ~ '^control/probe/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 byte_size bigint NOT NULL CHECK (byte_size=32),
 sha256 bytea NOT NULL CHECK (octet_length(sha256)=32),
 phase text NOT NULL CHECK (phase IN ('reserved','verified','cleaning','complete')),
 cleanup_mode text NOT NULL DEFAULT 'zero_marker' CHECK (cleanup_mode IN ('delete','zero_marker')),
 io_closed boolean NOT NULL DEFAULT false,
 fence bigint NOT NULL DEFAULT 1 CHECK (fence>0),
 recovery_pass bigint NOT NULL DEFAULT 0 CHECK (recovery_pass>=0),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX startup_probe_recovery ON agenteam_object.startup_probes(recovery_pass,id) WHERE phase<>'complete';
