-- agenteam:transaction tx
-- +goose Up
-- Existing addresses remain unverified. Neither administrator status nor a
-- historical recovery-log delivery is evidence of mailbox ownership.
ALTER TABLE agenteam_account.users
 ADD COLUMN email_verified_at timestamptz(6),
 ADD COLUMN passkey_version bigint NOT NULL DEFAULT 1 CHECK(passkey_version>0);

CREATE TABLE agenteam_account.passkeys (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES agenteam_account.users(id),
 credential_id bytea NOT NULL UNIQUE CHECK(octet_length(credential_id) BETWEEN 1 AND 1023),
 public_data jsonb NOT NULL CHECK(jsonb_typeof(public_data)='object'),
 label text NOT NULL CHECK(char_length(label) BETWEEN 1 AND 64 AND octet_length(label)<=256),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 last_used_at timestamptz(6),
 revoked_at timestamptz(6)
);
CREATE INDEX account_passkeys_user ON agenteam_account.passkeys(user_id,id) WHERE revoked_at IS NULL;

CREATE TABLE agenteam_account.passkey_operations (
 id uuid PRIMARY KEY,
 purpose text NOT NULL CHECK(purpose IN ('login','register','manage_passkeys','change_email')),
 browser_id uuid,
 user_id uuid REFERENCES agenteam_account.users(id),
 session_id uuid REFERENCES agenteam_account.sessions(id),
 grant_id uuid,
 action text CHECK(action IN ('register','rename','delete')),
 target_id uuid,
 label text,
 expected_version bigint CHECK(expected_version>0),
 session_data jsonb NOT NULL CHECK(jsonb_typeof(session_data)='object'),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz(6) NOT NULL,
 consumed_command_id uuid REFERENCES agenteam_account.commands(id),
 failures integer NOT NULL DEFAULT 0 CHECK(failures BETWEEN 0 AND 5),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '5 minutes'),
 CHECK((purpose='login' AND browser_id IS NOT NULL AND user_id IS NULL AND session_id IS NULL)
    OR (purpose<>'login' AND browser_id IS NULL AND user_id IS NOT NULL AND session_id IS NOT NULL)),
 CHECK((purpose='register' AND grant_id IS NOT NULL AND label IS NOT NULL AND expected_version IS NOT NULL)
    OR (purpose<>'register' AND grant_id IS NULL AND label IS NULL AND expected_version IS NULL))
);
CREATE INDEX account_passkey_operations_expiry ON agenteam_account.passkey_operations(expires_at,id);

CREATE TABLE agenteam_account.reauth_grants (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES agenteam_account.users(id),
 session_id uuid NOT NULL REFERENCES agenteam_account.sessions(id),
 purpose text NOT NULL CHECK(purpose IN ('manage_passkeys','change_email')),
 action text CHECK(action IN ('register','rename','delete')),
 target_id uuid,
 method text NOT NULL CHECK(method IN ('passkey','mail_recovery')),
 credential_id uuid REFERENCES agenteam_account.passkeys(id),
 password_version bigint NOT NULL CHECK(password_version>0),
 passkey_version bigint NOT NULL CHECK(passkey_version>0),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz(6) NOT NULL,
 consumed_by uuid,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '5 minutes'),
 CHECK((purpose='manage_passkeys' AND action IS NOT NULL) OR (purpose='change_email' AND action IS NULL AND target_id IS NULL)),
 CHECK((action IN ('rename','delete') AND target_id IS NOT NULL) OR ((action IS NULL OR action='register') AND target_id IS NULL)),
 CHECK((method='passkey' AND credential_id IS NOT NULL) OR (method='mail_recovery' AND credential_id IS NULL AND purpose='manage_passkeys' AND action IS NOT NULL AND action='register'))
);
ALTER TABLE agenteam_account.passkey_operations ADD CONSTRAINT account_passkey_operation_grant FOREIGN KEY(grant_id) REFERENCES agenteam_account.reauth_grants(id);

-- Extends the existing durable login receipt instead of introducing a second
-- cookie or plaintext response store. One assertion can commit one command.
CREATE TABLE agenteam_account.passkey_logins (
 command_id uuid PRIMARY KEY REFERENCES agenteam_account.commands(id),
 operation_id uuid NOT NULL UNIQUE REFERENCES agenteam_account.passkey_operations(id),
 credential_id uuid NOT NULL REFERENCES agenteam_account.passkeys(id),
 credential_version bigint NOT NULL CHECK(credential_version>0)
);

CREATE TABLE agenteam_account.email_requests (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES agenteam_account.users(id),
 session_id uuid REFERENCES agenteam_account.sessions(id),
 kind text NOT NULL CHECK(kind IN ('passkey_recovery','email_change','initialization','operator_recovery','old_email_notice')),
 original_email text NOT NULL,
 target_email text NOT NULL CHECK(target_email=lower(target_email) AND octet_length(target_email) BETWEEN 3 AND 254),
 user_version bigint NOT NULL CHECK(user_version>0),
 password_version bigint NOT NULL CHECK(password_version>0),
 grant_id uuid REFERENCES agenteam_account.reauth_grants(id),
 passkey_version bigint NOT NULL CHECK(passkey_version>0),
 authorization_id uuid,
 phase text NOT NULL CHECK(phase IN ('pending','verified','consumed','cancelled','locked')),
 code_kid text REFERENCES agenteam_account.account_key_registry(kid),
 code_mac bytea CHECK(octet_length(code_mac)=32),
 material_ref uuid,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5),
 deliveries integer NOT NULL DEFAULT 1 CHECK(deliveries BETWEEN 1 AND 5),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 cleanup_pass bigint NOT NULL DEFAULT 0 CHECK(cleanup_pass>=0),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz(6) NOT NULL,
 last_delivery_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz(6),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '10 minutes'),
 CHECK((kind='old_email_notice' AND code_kid IS NULL AND code_mac IS NULL AND material_ref IS NULL)
   OR (kind<>'old_email_notice' AND code_kid IS NOT NULL AND code_mac IS NOT NULL AND (material_ref IS NOT NULL OR phase<>'pending')))
);
CREATE UNIQUE INDEX account_email_request_active ON agenteam_account.email_requests(user_id,kind) WHERE phase='pending';
CREATE INDEX account_email_request_expiry ON agenteam_account.email_requests(expires_at,id);

CREATE TABLE agenteam_account.email_rate_limits (
 user_id uuid NOT NULL REFERENCES agenteam_account.users(id),
 kind text NOT NULL CHECK(kind IN ('passkey_recovery','email_change','initialization','operator_recovery')),
 window_started_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 failures integer NOT NULL DEFAULT 0 CHECK(failures BETWEEN 0 AND 5),
 deliveries integer NOT NULL DEFAULT 0 CHECK(deliveries BETWEEN 0 AND 5),
 last_delivery_at timestamptz(6),
 PRIMARY KEY(user_id,kind)
);

CREATE TABLE agenteam_account.operator_authorizations (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES agenteam_account.users(id),
 purpose text NOT NULL CHECK(purpose IN ('initialize_email','recover_account')),
 target_email text NOT NULL CHECK(target_email=lower(target_email) AND octet_length(target_email) BETWEEN 3 AND 254),
 token_verifier bytea NOT NULL UNIQUE CHECK(octet_length(token_verifier)=32),
 created_at timestamptz(6) NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz(6) NOT NULL,
 consumed_request_id uuid REFERENCES agenteam_account.email_requests(id),
 revoked_at timestamptz(6),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '30 minutes')
);
ALTER TABLE agenteam_account.email_requests ADD CONSTRAINT account_email_operator_authorization FOREIGN KEY(authorization_id) REFERENCES agenteam_account.operator_authorizations(id);

ALTER TABLE agenteam_account.commands DROP CONSTRAINT commands_command_name_check;
ALTER TABLE agenteam_account.commands ADD CONSTRAINT commands_command_name_check CHECK(command_name IN (
 'login','logout','invite-create','invite-revoke','invite-redeem','password-change','reset-request','reset-complete','profile-update','avatar-update','settings-update','smtp-settings-update','smtp-test','mail-retry',
 'passkey-register','passkey-rename','passkey-delete','passkey-reauth','passkey-recovery-request','passkey-recovery-confirm','email-change-request','email-change-confirm','email-code-resend','email-initialize-request','email-initialize-confirm','operator-recovery-request','operator-recovery-confirm'));

CREATE TABLE agenteam_account.security_receipts (
 command_id uuid PRIMARY KEY REFERENCES agenteam_account.commands(id),
 receipt jsonb NOT NULL CHECK(jsonb_typeof(receipt)='object')
);

ALTER TABLE agenteam_account.delivery_intents DROP CONSTRAINT delivery_intents_kind_check;
ALTER TABLE agenteam_account.delivery_intents ADD CONSTRAINT delivery_intents_kind_check CHECK(kind IN ('invitation','password_reset','test','email_code','email_notice'));
ALTER TABLE agenteam_account.material_cleanup DROP CONSTRAINT material_cleanup_owner_kind_check;
ALTER TABLE agenteam_account.material_cleanup ADD CONSTRAINT material_cleanup_owner_kind_check CHECK(owner_kind IN ('invitation','password_reset','login_response','smtp_settings','email_code','email_notice'));

-- Keep a current strong session while publishing revocation of its peers.
-- Locate the original unnamed CHECK by its columns, rather than a generated
-- PostgreSQL constraint suffix that can vary with preceding schema changes.
-- +goose StatementBegin
DO $$ DECLARE original_name text; BEGIN
 SELECT conname INTO STRICT original_name FROM pg_constraint
 WHERE conrelid='agenteam_account.commands'::regclass AND contype='c'
   AND pg_get_expr(conbin,conrelid) LIKE '%event_id%'
   AND pg_get_expr(conbin,conrelid) LIKE '%command_name%';
 EXECUTE format('ALTER TABLE agenteam_account.commands DROP CONSTRAINT %I',original_name);
END $$;
-- +goose StatementEnd
ALTER TABLE agenteam_account.commands ADD CONSTRAINT commands_revocation_event_check CHECK(
 (event_id IS NULL AND event_header IS NULL AND event_payload IS NULL AND event_digest IS NULL)
 OR (event_id IS NOT NULL AND command_name IN ('logout','password-change','reset-complete','email-change-confirm','email-initialize-confirm','operator-recovery-confirm','passkey-register')
 AND event_header IS NOT NULL AND jsonb_typeof(event_header)='object' AND event_payload IS NOT NULL
 AND octet_length(event_payload)<=1024 AND event_digest IS NOT NULL AND event_digest ~ '^sha256:[0-9a-f]{64}$'));

-- Preserve every existing module's audit action while extending the closed set.
-- +goose StatementBegin
DO $$ DECLARE original text; BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT original FROM pg_constraint
 WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname='audit_records_action_check' AND contype='c';
 EXECUTE 'ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_action_check';
 EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_action_check CHECK ((%s) OR action IN (''account.passkey.register'',''account.passkey.rename'',''account.passkey.revoke'',''account.passkey.reauthenticate'',''account.email.proof.request'',''account.email.proof.confirm'',''account.email.change'',''account.email.authorize''))', original);
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE original text; BEGIN
 SELECT pg_get_expr(conbin,conrelid) INTO STRICT original FROM pg_constraint
 WHERE conrelid='agenteam_audit.audit_records'::regclass AND conname='audit_records_account_contract' AND contype='c';
 EXECUTE 'ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_account_contract';
 EXECUTE format('ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_account_contract CHECK ((%s) OR (action IN (''account.passkey.register'',''account.passkey.rename'',''account.passkey.revoke'',''account.passkey.reauthenticate'',''account.email.proof.request'',''account.email.proof.confirm'',''account.email.change'',''account.email.authorize'') AND producer=''account'' AND scope=''system'' AND resource_kind=''user'' AND resource_id IS NOT NULL AND ((action<>''account.email.authorize'' AND actor_kind=''human'' AND user_id=resource_id) OR (action=''account.email.authorize'' AND actor_kind=''service'' AND service_name=''account-maintenance'' AND service_cause=cause_ref))))', original);
END $$;
-- +goose StatementEnd
