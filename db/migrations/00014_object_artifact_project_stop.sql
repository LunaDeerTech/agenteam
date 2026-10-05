-- agenteam:transaction tx
-- +goose Up

-- These are D05 technical admission/join facts. They neither duplicate the
-- Project lifecycle nor authorize work. No Project/Audit cascade may erase an
-- unresolved writer or a still-live process's I/O.
CREATE TABLE agenteam_object.project_stops (
  project_id agenteam_object.safe_id NOT NULL,
  operation_id agenteam_object.safe_id NOT NULL,
  action text NOT NULL CHECK (action IN ('archive','delete')),
  project_version bigint NOT NULL CHECK (project_version>0),
  state text NOT NULL DEFAULT 'stopping' CHECK (state IN ('stopping','stopped')),
  scan_kind integer NOT NULL DEFAULT 0 CHECK (scan_kind BETWEEN 0 AND 4),
  scan_after agenteam_object.safe_id,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  stopped_at timestamptz,
  PRIMARY KEY(project_id,operation_id),
  UNIQUE(project_id,project_version),
  CHECK ((state='stopped')=(stopped_at IS NOT NULL)),
  CHECK (state<>'stopped' OR (scan_kind=0 AND scan_after IS NULL))
);

CREATE TABLE agenteam_object.project_work (
  id agenteam_object.safe_id PRIMARY KEY,
  project_id agenteam_object.safe_id NOT NULL,
  process_id agenteam_object.safe_id NOT NULL,
  kind text NOT NULL CHECK (kind IN ('preparation','reader','source','download','transfer_get','transfer_put')),
  resource_id agenteam_object.safe_id NOT NULL,
  object_id agenteam_object.safe_id,
  admission_version bigint NOT NULL CHECK (admission_version>=0),
  admission_operation agenteam_object.safe_id,
  revoked_by agenteam_object.safe_id,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  joined_at timestamptz,
  CHECK ((admission_version=0)=(admission_operation IS NULL)),
  FOREIGN KEY(project_id,revoked_by) REFERENCES agenteam_object.project_stops(project_id,operation_id)
);
CREATE INDEX object_project_work_scan ON agenteam_object.project_work(project_id,id);
CREATE INDEX object_project_work_process ON agenteam_object.project_work(process_id,id) WHERE joined_at IS NULL;
CREATE INDEX object_project_work_resource ON agenteam_object.project_work(kind,resource_id);

CREATE TABLE agenteam_artifact.project_stops (
  project_id agenteam_object.safe_id NOT NULL,
  operation_id agenteam_object.safe_id NOT NULL,
  action text NOT NULL CHECK (action IN ('archive','delete')),
  project_version bigint NOT NULL CHECK (project_version>0),
  state text NOT NULL DEFAULT 'stopping' CHECK (state IN ('stopping','stopped')),
  scan_kind integer NOT NULL DEFAULT 0 CHECK (scan_kind BETWEEN 0 AND 3),
  scan_after agenteam_object.safe_id,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  stopped_at timestamptz,
  PRIMARY KEY(project_id,operation_id),
  UNIQUE(project_id,project_version),
  CHECK ((state='stopped')=(stopped_at IS NOT NULL)),
  CHECK (state<>'stopped' OR (scan_kind=0 AND scan_after IS NULL))
);

CREATE TABLE agenteam_artifact.project_work (
  id agenteam_object.safe_id PRIMARY KEY,
  project_id agenteam_object.safe_id NOT NULL,
  process_id agenteam_object.safe_id NOT NULL,
  kind text NOT NULL CHECK (kind IN ('upload','creation')),
  resource_id agenteam_object.safe_id NOT NULL,
  command_hash bytea CHECK (octet_length(command_hash)=32),
  source_project_id agenteam_object.safe_id,
  source_lease_id agenteam_object.safe_id,
  admission_version bigint NOT NULL CHECK (admission_version>=0),
  admission_operation agenteam_object.safe_id,
  revoked_by agenteam_object.safe_id,
  source_revoked_by agenteam_object.safe_id,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  joined_at timestamptz,
  source_joined_at timestamptz,
  CHECK ((admission_version=0)=(admission_operation IS NULL)),
  CHECK (source_project_id IS NOT NULL OR (source_revoked_by IS NULL AND source_joined_at IS NULL AND source_lease_id IS NULL)),
  FOREIGN KEY(project_id,revoked_by) REFERENCES agenteam_artifact.project_stops(project_id,operation_id),
  FOREIGN KEY(source_project_id,source_revoked_by) REFERENCES agenteam_artifact.project_stops(project_id,operation_id)
);
CREATE INDEX artifact_project_work_scan ON agenteam_artifact.project_work(project_id,id);
CREATE INDEX artifact_project_work_source ON agenteam_artifact.project_work(source_project_id,id) WHERE source_project_id IS NOT NULL;
CREATE INDEX artifact_project_work_process ON agenteam_artifact.project_work(process_id,id) WHERE joined_at IS NULL;
