-- agenteam:transaction tx
-- +goose Up
-- Only new Claims may bind explicit settings. Historical NULL policy pairs
-- stay NULL; a later process's deployment settings cannot authorize retries.
ALTER TABLE agenteam_scheduler.dispatches
 ADD COLUMN retry_policy bytea,
 ADD COLUMN retry_policy_digest text;

ALTER TABLE agenteam_scheduler.dispatches ADD CONSTRAINT dispatch_retry_policy_shape CHECK((
 (retry_policy IS NULL AND retry_policy_digest IS NULL)
 OR (retry_policy IS NOT NULL AND retry_policy_digest IS NOT NULL
  AND octet_length(retry_policy) BETWEEN 1 AND 512
  AND array_length(string_to_array(convert_from(retry_policy,'UTF8'),E'\n'),1)=6
  AND convert_from(retry_policy,'UTF8') ~ E'^agenteam\\.scheduler\\.launch_retry\nalgorithm=capped_exponential_v1\nmax_attempts=[1-9][0-9]{0,18}\ninitial_backoff_ns=[1-9][0-9]{0,18}\nmax_backoff_ns=[1-9][0-9]{0,18}\n$'
  AND split_part(split_part(convert_from(retry_policy,'UTF8'),E'\n',3),'=',2)::bigint>=1
  AND split_part(split_part(convert_from(retry_policy,'UTF8'),E'\n',4),'=',2)::bigint>=1
  AND split_part(split_part(convert_from(retry_policy,'UTF8'),E'\n',5),'=',2)::bigint
      >=split_part(split_part(convert_from(retry_policy,'UTF8'),E'\n',4),'=',2)::bigint
  AND retry_policy_digest='sha256:'||encode(sha256(retry_policy),'hex'))
) IS TRUE);

-- +goose StatementBegin
CREATE FUNCTION agenteam_scheduler.guard_dispatch_retry_policy() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.retry_policy,NEW.retry_policy_digest) IS DISTINCT FROM ROW(OLD.retry_policy,OLD.retry_policy_digest) THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='dispatch_retry_policy_immutable',MESSAGE='immutable dispatch retry policy';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER dispatch_retry_policy_immutable BEFORE UPDATE ON agenteam_scheduler.dispatches
 FOR EACH ROW EXECUTE FUNCTION agenteam_scheduler.guard_dispatch_retry_policy();
