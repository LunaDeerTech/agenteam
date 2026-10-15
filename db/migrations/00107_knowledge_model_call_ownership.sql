-- agenteam:transaction tx
-- +goose Up
-- Preserve the original operation's model deadline and terminal version so
-- lease retirement cannot invent a later, independent domain completion.
ALTER TABLE agenteam_knowledge.model_operations
 ADD COLUMN version bigint,
 ADD COLUMN deadline_at timestamptz(6);
UPDATE agenteam_knowledge.model_operations
 SET version=CASE state WHEN 'active' THEN 1 WHEN 'retired' THEN 3 ELSE 2 END,
 deadline_at=created_at+interval '2 minutes';
ALTER TABLE agenteam_knowledge.model_operations
 ALTER COLUMN version SET DEFAULT 1,
 ALTER COLUMN version SET NOT NULL,
 ALTER COLUMN deadline_at SET NOT NULL,
 ADD CONSTRAINT knowledge_model_operation_version CHECK(version>0);
