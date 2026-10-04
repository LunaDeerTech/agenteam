-- agenteam:transaction tx
-- +goose Up

ALTER TABLE agenteam_outbox.deliveries
  DROP CONSTRAINT outbox_deliveries_attempt_check;
ALTER TABLE agenteam_outbox.deliveries
  ADD CONSTRAINT outbox_deliveries_attempt_check CHECK (
    (current_attempt_id IS NULL AND fence=0 AND phase='pending')
    OR (current_attempt_id IS NOT NULL AND fence>0)
    OR (scope='project' AND project_id IS NOT NULL
        AND current_attempt_id IS NULL AND fence=0
        AND cycle_attempts=0 AND lifetime_attempts=0
        AND phase='dead_letter'
        AND safe_reason IS NOT DISTINCT FROM 'project_stopped')
  );
