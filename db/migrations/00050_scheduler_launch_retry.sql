-- agenteam:transaction tx
-- +goose Up
-- The last proven temporary rejection survives a later attempt for diagnostics.
-- Only equality with the current known-not-created attempt can authorize retry
-- or exhaustion. Historical rows stay unclassified; no policy is backfilled.
ALTER TABLE agenteam_scheduler.dispatches
 ADD COLUMN temporary_attempt bigint,
 ADD COLUMN temporary_reason text,
 ADD COLUMN temporary_code text,
 ADD COLUMN temporary_occurred_at timestamptz(6);

ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT dispatch_retry_attempt_bound CHECK((
 retry_policy IS NULL OR attempt_count<=split_part(split_part(convert_from(retry_policy,'UTF8'),E'\n',3),'=',2)::bigint
) IS TRUE);

ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT dispatch_temporary_shape CHECK((
 (temporary_attempt IS NULL AND temporary_reason IS NULL AND temporary_code IS NULL AND temporary_occurred_at IS NULL)
 OR (temporary_attempt IS NOT NULL AND temporary_attempt>0 AND temporary_attempt<=attempt_count AND attempt_count-temporary_attempt<=1
  AND temporary_reason='launch_lock_timeout_v1' AND temporary_code='INTERNAL_ERROR'
  AND temporary_occurred_at IS NOT NULL AND temporary_occurred_at>=created_at AND temporary_occurred_at<=updated_at
  AND (
   (temporary_attempt<attempt_count AND next_retry_at IS NULL AND failure_reason IS DISTINCT FROM 'launch_retry_exhausted_v1')
   OR (temporary_attempt=attempt_count AND launch_outcome='known_not_created' AND execution_id IS NULL
    AND busy_attempt IS NULL AND skip_reason IS NULL AND skipped_at IS NULL
    AND (
     (retry_policy IS NULL AND status='pending' AND next_retry_at IS NULL AND final_attempt IS NULL)
     OR (retry_policy IS NOT NULL AND attempt_count<split_part(split_part(convert_from(retry_policy,'UTF8'),E'\n',3),'=',2)::bigint
      AND status='pending' AND next_retry_at IS NOT NULL AND next_retry_at>temporary_occurred_at AND final_attempt IS NULL)
     OR (retry_policy IS NOT NULL AND attempt_count=split_part(split_part(convert_from(retry_policy,'UTF8'),E'\n',3),'=',2)::bigint
      AND status IN ('pending','failed') AND next_retry_at IS NULL AND final_attempt=attempt_count
      AND failure_reason='launch_retry_exhausted_v1' AND failure_code=temporary_code AND failure_occurred_at=temporary_occurred_at)
    ))
  ))
) IS TRUE);

-- Preserve the existing permanent-rejection arm exactly. The new exhaustion
-- arm additionally requires its same-attempt temporary receipt and bound limit.
ALTER TABLE agenteam_scheduler.dispatches DROP CONSTRAINT dispatch_final_failure_shape;
ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT dispatch_final_failure_shape CHECK((
 (final_attempt IS NULL AND failure_reason IS NULL AND failure_code IS NULL AND failure_occurred_at IS NULL AND failed_at IS NULL)
 OR (final_attempt IS NOT NULL AND final_attempt>0 AND final_attempt=attempt_count
 AND (
  (failure_reason='unsupported_resource_constraints_v1' AND failure_code='DEPENDENCY_UNBOUND')
  OR (failure_reason='launch_retry_exhausted_v1' AND failure_code='INTERNAL_ERROR'
   AND temporary_attempt=attempt_count AND temporary_reason='launch_lock_timeout_v1' AND temporary_code=failure_code
   AND temporary_occurred_at=failure_occurred_at AND retry_policy IS NOT NULL
   AND attempt_count=split_part(split_part(convert_from(retry_policy,'UTF8'),E'\n',3),'=',2)::bigint)
 ) AND failure_occurred_at IS NOT NULL
 AND launch_outcome='known_not_created' AND execution_id IS NULL AND next_retry_at IS NULL AND claim_guard IS NOT NULL
 AND busy_attempt IS NULL AND skip_reason IS NULL AND skipped_at IS NULL
 AND ((status='pending' AND failed_at IS NULL) OR (status='failed' AND failed_at IS NOT NULL AND failed_at=updated_at AND failed_at>=failure_occurred_at)))
) IS TRUE);

-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.guard_dispatch_temporary() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.temporary_attempt IS NOT NULL OR NEW.temporary_reason IS NOT NULL OR NEW.temporary_code IS NOT NULL OR NEW.temporary_occurred_at IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_temporary_immutable',MESSAGE='invalid temporary rejection fact';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.temporary_attempt,NEW.temporary_reason,NEW.temporary_code,NEW.temporary_occurred_at)
  IS DISTINCT FROM ROW(OLD.temporary_attempt,OLD.temporary_reason,OLD.temporary_code,OLD.temporary_occurred_at) THEN
  IF (OLD.status='pending' AND OLD.launch_outcome='unknown' AND OLD.attempt_count>0
   AND NEW.status='pending' AND NEW.launch_outcome='known_not_created' AND NEW.attempt_count=OLD.attempt_count
   AND NEW.temporary_attempt=OLD.attempt_count AND NEW.temporary_occurred_at=NEW.updated_at
   AND NEW.temporary_reason='launch_lock_timeout_v1' AND NEW.temporary_code='INTERNAL_ERROR') IS NOT TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_temporary_immutable',MESSAGE='invalid temporary rejection checkpoint';
  END IF;
 END IF;
 IF NEW.attempt_count>OLD.attempt_count AND OLD.launch_outcome='known_not_created' THEN
  IF (OLD.status='pending' AND OLD.temporary_attempt=OLD.attempt_count
   AND OLD.temporary_reason='launch_lock_timeout_v1' AND OLD.temporary_code='INTERNAL_ERROR'
   AND OLD.retry_policy IS NOT NULL AND OLD.attempt_count<split_part(split_part(convert_from(OLD.retry_policy,'UTF8'),E'\n',3),'=',2)::bigint
   AND OLD.next_retry_at IS NOT NULL AND OLD.next_retry_at<=NEW.updated_at
   AND OLD.final_attempt IS NULL AND OLD.busy_attempt IS NULL
   AND NEW.status='pending' AND NEW.launch_outcome='unknown' AND NEW.attempt_count=OLD.attempt_count+1
   AND NEW.next_retry_at IS NULL
   AND ROW(NEW.temporary_attempt,NEW.temporary_reason,NEW.temporary_code,NEW.temporary_occurred_at)
       IS NOT DISTINCT FROM ROW(OLD.temporary_attempt,OLD.temporary_reason,OLD.temporary_code,OLD.temporary_occurred_at)) IS NOT TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_temporary_immutable',MESSAGE='invalid retry send marker';
  END IF;
 END IF;
 IF NEW.next_retry_at IS DISTINCT FROM OLD.next_retry_at THEN
  IF (
   (OLD.status='pending' AND OLD.launch_outcome='unknown' AND NEW.status='pending' AND NEW.launch_outcome='known_not_created'
    AND NEW.attempt_count=OLD.attempt_count AND NEW.temporary_attempt=NEW.attempt_count AND NEW.temporary_occurred_at=NEW.updated_at)
   OR (OLD.status='pending' AND OLD.launch_outcome='known_not_created' AND NEW.status='pending' AND NEW.launch_outcome='unknown'
    AND NEW.attempt_count=OLD.attempt_count+1 AND NEW.next_retry_at IS NULL)
  ) IS NOT TRUE THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_temporary_immutable',MESSAGE='invalid retry deadline mutation';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER dispatch_temporary_immutable BEFORE INSERT OR UPDATE ON agenteam_scheduler.dispatches
 FOR EACH ROW EXECUTE FUNCTION agenteam_scheduler.guard_dispatch_temporary();

-- Work retains its existing owner, caller-Tx proof, immutable result, blocker,
-- history and event schema. Only the formally closed reason is extended.
ALTER TABLE agenteam_work.task_launch_failures DROP CONSTRAINT task_launch_failures_reason_check;
ALTER TABLE agenteam_work.task_launch_failures ADD CONSTRAINT task_launch_failures_reason_check
 CHECK(reason IN ('unsupported_resource_constraints_v1','launch_retry_exhausted_v1'));
