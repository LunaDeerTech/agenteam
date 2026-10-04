-- agenteam:transaction tx
-- +goose Up
ALTER TABLE agenteam_account.smtp_settings
 ADD COLUMN sender_name text NOT NULL DEFAULT '',
 ADD COLUMN auto_retry_count bigint NOT NULL DEFAULT 3,
 ADD COLUMN retry_interval_seconds bigint NOT NULL DEFAULT 60,
 ADD CONSTRAINT smtp_sender_name CHECK (char_length(sender_name)<=80 AND (configured OR sender_name='')),
 ADD CONSTRAINT smtp_retry_count CHECK (auto_retry_count BETWEEN 0 AND 5),
 ADD CONSTRAINT smtp_retry_interval CHECK (retry_interval_seconds BETWEEN 10 AND 3600);

ALTER TABLE agenteam_account.mail_jobs
 DROP CONSTRAINT mail_jobs_phase_check,
 DROP CONSTRAINT mail_jobs_attempts_check,
 ADD CONSTRAINT mail_jobs_phase_check CHECK (phase IN ('pending','processing','claimed','sending','retry_wait','sent','failed','cancelled','unknown')),
 ADD CONSTRAINT mail_jobs_attempts_check CHECK (attempts BETWEEN 0 AND 6);
DROP INDEX agenteam_account.account_mail_fair;
CREATE INDEX account_mail_fair ON agenteam_account.mail_jobs(pass,next_at,id) WHERE phase IN ('pending','retry_wait');

-- A legacy NULL ref is deliberately not inferred from today's configuration.
ALTER TABLE agenteam_account.mail_attempts
 ADD COLUMN token_ref uuid,
 ADD COLUMN credential_ref uuid,
 ADD CONSTRAINT mail_token_ref_lease CHECK (token_ref IS NULL OR token_lease_id IS NOT NULL),
 ADD CONSTRAINT mail_credential_ref_lease CHECK (credential_ref IS NULL OR credential_lease_id IS NOT NULL);

ALTER TABLE agenteam_account.commands DROP CONSTRAINT commands_command_name_check;
ALTER TABLE agenteam_account.commands ADD CONSTRAINT commands_command_name_check CHECK(command_name IN ('login','logout','invite-create','invite-revoke','invite-redeem','password-change','reset-request','reset-complete','profile-update','avatar-update','settings-update','smtp-settings-update','smtp-test','mail-retry'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_action_check;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_action_check CHECK (action IN (
  'secret.create','secret.update','secret.delete','secret.resolve','secret.master.register','secret.master.rotation.start','secret.master.rotation.complete','secret.master.rotation.failed','outbound.policy.update','outbound.access.deny',
  'object.upload.complete','object.upload.failed','object.delete','object.transfer.issue','object.transfer.complete','object.transfer.revoke','artifact.create','artifact.list','artifact.read','artifact.download','outbox.delivery.requeue','account.bootstrap','account.login','account.logout','account.invite.create','account.invite.revoke','account.invite.redeem','account.password.change','account.password.reset.request','account.password.reset.complete','account.profile.update','account.avatar.update','account.settings.update','smtp.settings.update','smtp.test.request','smtp.delivery','smtp.delivery.retry'));
ALTER TABLE agenteam_audit.audit_records DROP CONSTRAINT audit_records_account_contract;
ALTER TABLE agenteam_audit.audit_records ADD CONSTRAINT audit_records_account_contract CHECK (
 (producer NOT IN ('account','account.mail') AND action NOT LIKE 'account.%' AND action NOT LIKE 'smtp.%' AND resource_kind NOT IN ('user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job'))
 OR (scope='system' AND actor_kind IN ('human','service') AND ((producer='account' AND action IN ('account.bootstrap','account.login','account.logout','account.invite.create','account.invite.revoke','account.invite.redeem','account.password.change','account.password.reset.request','account.password.reset.complete','account.profile.update','account.avatar.update','account.settings.update','smtp.settings.update','smtp.test.request')) OR (producer='account.mail' AND action='smtp.delivery')) AND resource_kind IN ('user','session','account_attempt','invitation','password_reset','account_settings','smtp_settings','mail_job') AND resource_id IS NOT NULL AND (actor_kind<>'service' OR (service_name IS NOT NULL AND service_name IN ('account-bootstrap','account-auth','account-maintenance','account-mail') AND service_cause=cause_ref)))
 OR (scope='system' AND actor_kind='human' AND producer='account' AND action='smtp.delivery.retry' AND resource_kind='mail_job' AND resource_id IS NOT NULL)
);

ALTER TABLE agenteam_account.delivery_intents ADD COLUMN origin_intent_id uuid;
-- +goose StatementBegin
DO $body$
BEGIN
 IF EXISTS (
  SELECT 1 FROM agenteam_account.delivery_intents i
  LEFT JOIN agenteam_account.commands c ON c.id=i.id
  WHERE (
   c.id IS NOT NULL AND c.phase='committed'
   AND c.attempt_id IS NOT NULL AND c.attempt_id IS NOT DISTINCT FROM i.job_id
   AND c.resource_id IS NOT NULL AND c.resource_id IS NOT DISTINCT FROM i.link_id
   AND c.created_at IS NOT DISTINCT FROM i.created_at
   AND (
    (i.kind='invitation' AND c.command_name='invite-create' AND c.actor_kind='human'
     AND c.user_id IS NOT NULL AND c.user_id IS NOT DISTINCT FROM i.initiator_id)
    OR (i.kind='password_reset' AND c.command_name='reset-request' AND c.actor_kind='browser'
     AND c.browser_id IS NOT NULL AND c.browser_id IS NOT DISTINCT FROM i.initiator_id
     AND c.user_id IS NOT NULL AND c.password_version IS NOT NULL AND c.password_version>0)
   )
  ) IS NOT TRUE
 ) THEN
  RAISE EXCEPTION 'ACCOUNT_DELIVERY_ORIGIN_INVALID';
 END IF;
END
$body$;
-- +goose StatementEnd
UPDATE agenteam_account.delivery_intents SET origin_intent_id=id;
ALTER TABLE agenteam_account.delivery_intents
 ALTER COLUMN origin_intent_id SET NOT NULL,
 ADD CONSTRAINT account_delivery_origin_fk FOREIGN KEY(origin_intent_id) REFERENCES agenteam_account.delivery_intents(id) ON DELETE RESTRICT;
CREATE INDEX account_delivery_origin ON agenteam_account.delivery_intents(origin_intent_id,id);
