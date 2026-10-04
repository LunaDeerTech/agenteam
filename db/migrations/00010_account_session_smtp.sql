-- agenteam:transaction tx
-- +goose Up
CREATE SCHEMA agenteam_account;
CREATE TABLE agenteam_account.account_key_registry (
 kid text PRIMARY KEY CHECK(kid ~ '^[A-Za-z0-9_-]{1,64}$'),
 fingerprint bytea NOT NULL UNIQUE CHECK(octet_length(fingerprint)=32),
 registered_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 anonymous_until timestamptz(6)
);
CREATE TABLE agenteam_account.users (
 id uuid PRIMARY KEY,
 email text NOT NULL CONSTRAINT account_email_unique UNIQUE CHECK(email=lower(email) AND octet_length(email) BETWEEN 3 AND 254 AND email ~ '^[!-~]+@[!-~]+$'),
 username text NOT NULL CONSTRAINT account_username_unique UNIQUE CHECK(username ~ '^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$'),
 display_name text NOT NULL CHECK(octet_length(display_name)<=320 AND char_length(display_name)<=80),
 role text NOT NULL CHECK(role IN ('admin','user')),
 password_phc text NOT NULL CHECK(octet_length(password_phc) BETWEEN 1 AND 256),
 password_version bigint NOT NULL CHECK(password_version>0),
 version bigint NOT NULL CHECK(version>0),
 auth_sequence bigint NOT NULL CHECK(auth_sequence>0),
 initial_password_suggestion boolean NOT NULL,
 theme text NOT NULL CHECK(theme IN ('system','light','dark')),
 avatar_object_id uuid,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE agenteam_account.bootstrap (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 operation_id uuid NOT NULL UNIQUE,
 user_id uuid NOT NULL UNIQUE REFERENCES agenteam_account.users(id),
 creator_process_id uuid NOT NULL,
 completed_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 log_state text NOT NULL CHECK(log_state IN ('eligible','attempted','written','failed','unknown','abandoned')),
 log_attempt_id uuid,
 CHECK((log_state IN ('eligible','abandoned') AND log_attempt_id IS NULL) OR (log_state IN ('attempted','written','failed','unknown') AND log_attempt_id IS NOT NULL))
);
CREATE TABLE agenteam_account.account_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),id uuid NOT NULL UNIQUE,
 version bigint NOT NULL CHECK(version>0),
 session_idle_seconds bigint NOT NULL CHECK(session_idle_seconds BETWEEN 900 AND 2592000),
 session_absolute_seconds bigint NOT NULL CHECK(session_absolute_seconds BETWEEN 3600 AND 7776000 AND session_absolute_seconds>=session_idle_seconds),
 password_reset_seconds bigint NOT NULL CHECK(password_reset_seconds BETWEEN 300 AND 7200),
 challenge_after_failures bigint NOT NULL CHECK(challenge_after_failures BETWEEN 1 AND 20)
);
CREATE TABLE agenteam_account.smtp_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),id uuid NOT NULL UNIQUE,
 version bigint NOT NULL CHECK(version>0),configured boolean NOT NULL,enabled boolean NOT NULL,
 host text,port integer,tls_mode text,auth_username text,from_address text,password_ref uuid,
 CHECK((NOT configured AND NOT enabled AND host IS NULL AND port IS NULL AND tls_mode IS NULL AND auth_username IS NULL AND from_address IS NULL AND password_ref IS NULL)
 OR (configured AND host IS NOT NULL AND octet_length(host) BETWEEN 1 AND 253 AND port IS NOT NULL AND port BETWEEN 1 AND 65535 AND tls_mode IS NOT NULL AND tls_mode IN ('none','starttls','tls') AND from_address IS NOT NULL AND octet_length(from_address) BETWEEN 3 AND 254 AND ((auth_username IS NULL AND password_ref IS NULL) OR (auth_username IS NOT NULL AND octet_length(auth_username) BETWEEN 1 AND 512 AND password_ref IS NOT NULL))))
);
CREATE TABLE agenteam_account.sessions (
 id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES agenteam_account.users(id),
 token_verifier bytea NOT NULL UNIQUE CHECK(octet_length(token_verifier)=32),
 csrf_kid text NOT NULL REFERENCES agenteam_account.account_key_registry(kid),
 issued_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 last_activity_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 idle_seconds bigint NOT NULL CHECK(idle_seconds BETWEEN 900 AND 2592000),
 absolute_expires_at timestamptz(6) NOT NULL,
 revoked_at timestamptz(6),revoked_reason text,
 CHECK(absolute_expires_at>issued_at AND last_activity_at>=issued_at),
 CHECK((revoked_at IS NULL AND revoked_reason IS NULL) OR (revoked_at IS NOT NULL AND revoked_reason IS NOT NULL AND revoked_reason IN ('logout','password_changed','password_reset','administrative')))
);
CREATE INDEX account_sessions_user ON agenteam_account.sessions(user_id,id) WHERE revoked_at IS NULL;
CREATE TABLE agenteam_account.commands (
 id uuid PRIMARY KEY,namespace text NOT NULL CHECK(namespace ~ '^[a-z][a-z0-9_.-]{0,127}$'),
 owner_id uuid NOT NULL,command_key text NOT NULL CHECK(octet_length(command_key) BETWEEN 1 AND 128),
 command_name text NOT NULL CHECK(command_name IN ('login','logout','invite-create','invite-revoke','invite-redeem','password-change','reset-request','reset-complete','profile-update','avatar-update','settings-update','smtp-settings-update','smtp-test')),
 identity_digest text NOT NULL UNIQUE CHECK(identity_digest ~ '^sha256:[0-9a-f]{64}$'),
 semantic_kid text NOT NULL REFERENCES agenteam_account.account_key_registry(kid),semantic_mac bytea NOT NULL CHECK(octet_length(semantic_mac)=32),
 actor_kind text NOT NULL CHECK(actor_kind IN ('human','browser','service')),
 user_id uuid,session_id uuid,browser_id uuid,browser_expires_at timestamptz(6),
 origin_process_id uuid NOT NULL,
 expected_version bigint NOT NULL DEFAULT 0 CHECK(expected_version>=0),password_version bigint CHECK(password_version>0),
 attempt_id uuid,resource_id uuid,
 phase text NOT NULL CHECK(phase IN ('planned','committed','failed','cancelled')),
 cleanup_pass bigint NOT NULL DEFAULT 0 CHECK(cleanup_pass>=0),
 result_version bigint CHECK(result_version>0),
 result_code text CHECK(result_code IN ('UNAUTHENTICATED','CHALLENGE_REQUIRED','CHALLENGE_INVALID','SESSION_REVOKED','INVALID_STATE','COMPLETED')),
 secret_command_digest text CHECK(secret_command_digest ~ '^sha256:[0-9a-f]{64}$'),
 secret_write_binding text CHECK(secret_write_binding ~ '^sha256:[0-9a-f]{64}$'),
 planned_secret_ref uuid,response_secret_ref uuid,response_expires_at timestamptz(6),
 event_id uuid UNIQUE,event_header jsonb,event_payload bytea,event_digest text,
 CHECK((event_id IS NULL AND event_header IS NULL AND event_payload IS NULL AND event_digest IS NULL) OR (event_id IS NOT NULL AND command_name IN ('logout','password-change','reset-complete') AND event_header IS NOT NULL AND jsonb_typeof(event_header)='object' AND event_payload IS NOT NULL AND octet_length(event_payload)<=1024 AND event_digest IS NOT NULL AND event_digest ~ '^sha256:[0-9a-f]{64}$')),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz(6),
 UNIQUE(namespace,owner_id,command_key),
 CHECK((actor_kind='browser' AND browser_id IS NOT NULL AND browser_expires_at IS NOT NULL) OR (actor_kind IN ('human','service') AND browser_id IS NULL AND browser_expires_at IS NULL)),
 CHECK((phase='planned' AND completed_at IS NULL) OR (phase<>'planned' AND completed_at IS NOT NULL)),
 CHECK((response_secret_ref IS NULL AND response_expires_at IS NULL) OR (response_secret_ref IS NOT NULL AND response_expires_at IS NOT NULL AND command_name='login' AND phase='committed' AND user_id IS NOT NULL AND session_id IS NOT NULL AND password_version IS NOT NULL))
);
CREATE INDEX account_commands_cleanup ON agenteam_account.commands(cleanup_pass,id);
CREATE TABLE agenteam_account.auth_attempts (
 id uuid PRIMARY KEY,kind text NOT NULL CHECK(kind IN ('login','reset_request','token_redeem','response_read')),
 command_id uuid NOT NULL REFERENCES agenteam_account.commands(id),user_id uuid,
 outcome text NOT NULL CHECK(outcome IN ('planned','success','denied','failed','unknown')),
 phase text NOT NULL CHECK(phase IN ('planned','active','stopping','released','completed')),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz(6),
 browser_id uuid,session_id uuid,password_version bigint,semantic_kid text,semantic_mac bytea,
 credential_id uuid,scope text,purpose text,consumer text,expires_at timestamptz(6),
 process_id uuid,fence bigint,lease_id uuid UNIQUE,
 CHECK((kind='response_read' AND browser_id IS NOT NULL AND session_id IS NOT NULL AND user_id IS NOT NULL AND password_version IS NOT NULL AND password_version>0 AND semantic_kid IS NOT NULL AND semantic_mac IS NOT NULL AND octet_length(semantic_mac)=32 AND credential_id IS NOT NULL AND scope IS NOT NULL AND scope='system' AND purpose IS NOT NULL AND purpose='system' AND consumer IS NOT NULL AND consumer='system' AND expires_at IS NOT NULL AND process_id IS NOT NULL AND fence IS NOT NULL AND fence>0 AND lease_id IS NOT NULL AND phase IN ('active','stopping','released'))
 OR (kind<>'response_read' AND browser_id IS NULL AND session_id IS NULL AND password_version IS NULL AND semantic_kid IS NULL AND semantic_mac IS NULL AND credential_id IS NULL AND scope IS NULL AND purpose IS NULL AND consumer IS NULL AND expires_at IS NULL AND process_id IS NULL AND fence IS NULL AND lease_id IS NULL AND phase IN ('planned','completed')))
);
CREATE INDEX account_response_active ON agenteam_account.auth_attempts(process_id,id) WHERE kind='response_read' AND phase<>'released';
CREATE TABLE agenteam_account.reset_requests (
 id uuid PRIMARY KEY,command_id uuid NOT NULL UNIQUE REFERENCES agenteam_account.commands(id),
 canonical_email text NOT NULL CHECK(octet_length(canonical_email) BETWEEN 3 AND 254),browser_id uuid NOT NULL,
 phase text NOT NULL CHECK(phase IN ('accepted','processed','cancelled')),pass bigint NOT NULL DEFAULT 0 CHECK(pass>=0),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz(6)
);
CREATE INDEX account_reset_pending ON agenteam_account.reset_requests(pass,id) WHERE phase='accepted';
CREATE TABLE agenteam_account.invitations (
 id uuid PRIMARY KEY,canonical_email text NOT NULL UNIQUE CHECK(octet_length(canonical_email) BETWEEN 3 AND 254),
 token_verifier bytea NOT NULL UNIQUE CHECK(octet_length(token_verifier)=32),material_ref uuid NOT NULL UNIQUE,
 version bigint NOT NULL CHECK(version>0),created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),expires_at timestamptz(6) NOT NULL,
 last_delivery_at timestamptz(6),created_by uuid NOT NULL,
 CHECK(expires_at=created_at+interval '24 hours')
);
CREATE TABLE agenteam_account.password_resets (
 id uuid PRIMARY KEY,user_id uuid NOT NULL UNIQUE REFERENCES agenteam_account.users(id),
 token_verifier bytea NOT NULL UNIQUE CHECK(octet_length(token_verifier)=32),material_ref uuid NOT NULL UNIQUE,
 version bigint NOT NULL CHECK(version>0),password_version bigint NOT NULL CHECK(password_version>0),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),expires_at timestamptz(6) NOT NULL CHECK(expires_at>created_at),last_delivery_at timestamptz(6)
);
CREATE TABLE agenteam_account.auth_failures (
 kind text NOT NULL CHECK(kind IN ('subject','ip','reset')),kid text NOT NULL REFERENCES agenteam_account.account_key_registry(kid),
 digest bytea NOT NULL CHECK(octet_length(digest)=32),failures bigint NOT NULL CHECK(failures>=0),
 window_start timestamptz(6) NOT NULL,expires_at timestamptz(6) NOT NULL CHECK(expires_at>window_start),
 PRIMARY KEY(kind,kid,digest)
);
CREATE INDEX account_failures_expiry ON agenteam_account.auth_failures(expires_at);
CREATE TABLE agenteam_account.challenges (
 id uuid PRIMARY KEY,process_id uuid NOT NULL,browser_id uuid NOT NULL,
 purpose text NOT NULL CHECK(purpose='login'),subject_kid text NOT NULL REFERENCES agenteam_account.account_key_registry(kid),subject_digest bytea NOT NULL CHECK(octet_length(subject_digest)=32),
 login_key text NOT NULL CHECK(octet_length(login_key) BETWEEN 1 AND 128),pass_verifier bytea UNIQUE CHECK(octet_length(pass_verifier)=32),
 phase text NOT NULL CHECK(phase IN ('challenge','passed','consumed','failed')),expires_at timestamptz(6) NOT NULL,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),CHECK(expires_at>created_at),CHECK(phase NOT IN ('passed','consumed') OR pass_verifier IS NOT NULL)
);
CREATE INDEX account_challenge_expiry ON agenteam_account.challenges(expires_at,id);
CREATE TABLE agenteam_account.delivery_intents (
 id uuid PRIMARY KEY,job_id uuid NOT NULL UNIQUE,kind text NOT NULL CHECK(kind IN ('invitation','password_reset','test')),
 link_id uuid,initiator_id uuid NOT NULL,recipient text,created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 CHECK((kind='test' AND link_id IS NULL AND recipient IS NOT NULL AND octet_length(recipient) BETWEEN 3 AND 254) OR (kind<>'test' AND link_id IS NOT NULL AND recipient IS NULL))
);
CREATE TABLE agenteam_account.mail_jobs (
 id uuid PRIMARY KEY,intent_id uuid NOT NULL UNIQUE REFERENCES agenteam_account.delivery_intents(id),
 phase text NOT NULL CHECK(phase IN ('pending','processing','sent','failed','cancelled','unknown')),
 attempts bigint NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),version bigint NOT NULL CHECK(version>0),fence bigint NOT NULL DEFAULT 0 CHECK(fence>=0),
 current_attempt_id uuid,next_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),pass bigint NOT NULL DEFAULT 0 CHECK(pass>=0),
 reason text CHECK(reason IN ('sent','token_invalid','configuration_invalid','policy_rejected','network_failed','smtp_rejected','timeout','cancelled','unknown')),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz(6)
);
CREATE INDEX account_mail_fair ON agenteam_account.mail_jobs(pass,next_at,id) WHERE phase='pending';
CREATE TABLE agenteam_account.mail_attempts (
 id uuid PRIMARY KEY,job_id uuid NOT NULL REFERENCES agenteam_account.mail_jobs(id),fence bigint NOT NULL CHECK(fence>0),process_id uuid NOT NULL,
 config_version bigint NOT NULL CHECK(config_version>0),credential_lease_id uuid,token_lease_id uuid,
 phase text NOT NULL CHECK(phase IN ('negotiation','auth','envelope','data','awaiting_acceptance','closed')),
 result text CHECK(result IN ('sent','failed','unknown','cancelled')),io_joined boolean NOT NULL DEFAULT false,
 channel text NOT NULL CHECK(channel IN ('smtp','log')),terminal boolean NOT NULL DEFAULT false,
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz(6),UNIQUE(job_id,fence)
);
CREATE TABLE agenteam_account.avatar_changes (
 id uuid PRIMARY KEY,command_id uuid NOT NULL UNIQUE REFERENCES agenteam_account.commands(id),user_id uuid NOT NULL REFERENCES agenteam_account.users(id),
 previous_object_id uuid,new_object_id uuid,upload_id uuid,attempt_id uuid,origin_process_id uuid NOT NULL,cleanup_cause uuid,
 phase text NOT NULL CHECK(phase IN ('preparing','published','applied','cancelled','cleanup_pending','completed')),
 pass bigint NOT NULL DEFAULT 0 CHECK(pass>=0),version bigint NOT NULL CHECK(version>0),created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz(6),
 CHECK((upload_id IS NULL AND attempt_id IS NULL) OR (upload_id IS NOT NULL AND attempt_id IS NOT NULL AND new_object_id IS NOT NULL))
);
CREATE TABLE agenteam_account.avatar_cleanup (
 id uuid PRIMARY KEY,user_id uuid NOT NULL,object_id uuid NOT NULL,change_id uuid NOT NULL,
 phase text NOT NULL CHECK(phase IN ('pending','completed')),pass bigint NOT NULL DEFAULT 0 CHECK(pass>=0),created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz(6)
);
CREATE INDEX account_avatar_cleanup_fair ON agenteam_account.avatar_cleanup(pass,id) WHERE phase='pending';
CREATE TABLE agenteam_account.material_cleanup (
 id uuid PRIMARY KEY,owner_kind text NOT NULL CHECK(owner_kind IN ('invitation','password_reset','login_response','smtp_settings')),
 owner_id uuid NOT NULL,credential_id uuid NOT NULL,purpose text NOT NULL CHECK(purpose IN ('system','smtp')),
 phase text NOT NULL CHECK(phase IN ('reference','leases','delete','completed')),pass bigint NOT NULL DEFAULT 0 CHECK(pass>=0),
 version bigint NOT NULL CHECK(version>0),secret_command_digest text CHECK(secret_command_digest ~ '^sha256:[0-9a-f]{64}$'),created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),completed_at timestamptz(6),
 UNIQUE(owner_kind,owner_id,credential_id),
 CHECK((owner_kind='smtp_settings' AND purpose='smtp') OR (owner_kind<>'smtp_settings' AND purpose='system'))
);
CREATE INDEX account_material_cleanup_fair ON agenteam_account.material_cleanup(pass,id) WHERE phase<>'completed';
ALTER TABLE agenteam_secret.secret_leases DROP CONSTRAINT secret_leases_owner_kind_check;
ALTER TABLE agenteam_secret.secret_leases ADD CONSTRAINT secret_leases_owner_kind_check CHECK(owner_kind IN ('execution','model_call','account_delivery_attempt','account_response'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_action_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_action_check CHECK (action IN (
  'secret.create','secret.update','secret.delete','secret.resolve','secret.master.register','secret.master.rotation.start','secret.master.rotation.complete','secret.master.rotation.failed','outbound.policy.update','outbound.access.deny',
  'object.upload.complete','object.upload.failed','object.delete','object.transfer.issue','object.transfer.complete','object.transfer.revoke','artifact.create','artifact.list','artifact.read','artifact.download','outbox.delivery.requeue','account.bootstrap','account.login','account.logout','account.invite.create','account.invite.revoke','account.invite.redeem','account.password.change','account.password.reset.request','account.password.reset.complete','account.profile.update','account.avatar.update','account.settings.update','smtp.settings.update','smtp.test.request','smtp.delivery'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_resource_kind_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_resource_kind_check CHECK (resource_kind IN ('secret','secret_master','secret_rotation','outbound_policy','agent','stored_object','object_transfer','artifact','artifact_collection','outbox_delivery','user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_producer_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_producer_check CHECK (producer IN ('secret','secret.master','outbound.policy','outbound.access','object','artifact','outbox','account','account.mail'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_check1;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_check1 CHECK (
  (actor_kind='human' AND user_id IS NOT NULL AND session_id IS NOT NULL AND actor_project_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name IS NULL AND service_cause IS NULL)
  OR (actor_kind='agent_run' AND user_id IS NULL AND session_id IS NULL AND actor_project_id IS NOT NULL AND agent_id IS NOT NULL AND actor_execution_id IS NOT NULL AND service_name IS NULL AND service_cause IS NULL AND scope='project' AND actor_project_id=project_id AND execution_id=actor_execution_id)
  OR (actor_kind='service' AND user_id IS NULL AND session_id IS NULL AND agent_id IS NULL AND actor_execution_id IS NULL AND service_name IN ('secret','secret-maintenance','outbound','project-lifecycle','object','object-maintenance','account-bootstrap','account-auth','account-maintenance','account-mail') AND service_cause IS NOT NULL AND actor_project_id IS NOT DISTINCT FROM project_id));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_check2;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_check2 CHECK (
 (resource_kind IN ('secret_master','outbound_policy') AND resource_id IS NULL)
 OR (resource_kind IN ('secret','secret_rotation','agent','stored_object','object_transfer','artifact','artifact_collection','outbox_delivery','user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job') AND resource_id IS NOT NULL));
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_account_contract CHECK (
 (producer NOT IN ('account','account.mail') AND action NOT LIKE 'account.%' AND action NOT LIKE 'smtp.%' AND resource_kind NOT IN ('user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job'))
 OR (scope='system' AND actor_kind IN ('human','service') AND ((producer='account' AND action IN ('account.bootstrap','account.login','account.logout','account.invite.create','account.invite.revoke','account.invite.redeem','account.password.change','account.password.reset.request','account.password.reset.complete','account.profile.update','account.avatar.update','account.settings.update','smtp.settings.update','smtp.test.request')) OR (producer='account.mail' AND action='smtp.delivery')) AND resource_kind IN ('user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job') AND resource_id IS NOT NULL AND (actor_kind<>'service' OR (service_name IS NOT NULL AND service_name IN ('account-bootstrap','account-auth','account-maintenance','account-mail') AND service_cause=cause_ref)))
);

CREATE TABLE agenteam_account.response_plans (
 id uuid PRIMARY KEY,command_id uuid NOT NULL REFERENCES agenteam_account.commands(id),
 browser_id uuid NOT NULL,user_id uuid NOT NULL,session_id uuid NOT NULL,
 semantic_kid text NOT NULL,semantic_mac bytea NOT NULL CHECK(octet_length(semantic_mac)=32),
 password_version bigint NOT NULL CHECK(password_version>0),credential_id uuid NOT NULL,
 scope text NOT NULL CHECK(scope='system'),purpose text NOT NULL CHECK(purpose='system'),consumer text NOT NULL CHECK(consumer='system'),
 expires_at timestamptz(6) NOT NULL,process_id uuid NOT NULL,fence bigint NOT NULL CHECK(fence>0),lease_id uuid NOT NULL UNIQUE,
 joined_at timestamptz(6),pass bigint NOT NULL DEFAULT 0 CHECK(pass>=0),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX account_response_plan_fair ON agenteam_account.response_plans(pass,id);
