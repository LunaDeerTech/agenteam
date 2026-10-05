package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// This is a private persisted plan, never a client-provided manifest or grant.
// It survives failed acceptance and keeps the first operation and event IDs.
type lifecycleCommandPlan struct {
	ExpectedVersion foundation.Version          `json:"expected_version"`
	OperationID     c.OperationID               `json:"operation_id"`
	From            c.Lifecycle                 `json:"from"`
	To              c.Lifecycle                 `json:"to"`
	Action          c.LifecycleAction           `json:"action"`
	Manifest        []c.ParticipantRegistration `json:"required_manifest"`
	ManifestDigest  foundation.Digest           `json:"manifest_digest"`
	Confirmation    *c.DeleteProjectRequest     `json:"confirmation,omitempty"`
	Header          json.RawMessage             `json:"event_header"`
	Payload         json.RawMessage             `json:"event_payload"`
}

type lifecycleCommandRecord struct {
	id, user string
	project  c.ProjectID
	name     c.CommandName
	key      foundation.IdempotencyKey
	semantic foundation.Digest
	state    string
	plan     lifecycleCommandPlan
	manifest c.RequiredManifest
	header   event.Header
}

func (r *lifecycleCommandRecord) identity() foundation.CommandIdentity {
	id, _ := c.CommandIdentity(r.project, r.name, r.key)
	return id
}

func decodeLifecycleJSON(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return unavailable(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return unavailable(err)
	}
	return nil
}

func storedLifecycleManifest(entries []c.ParticipantRegistration, expected foundation.Digest) (c.RequiredManifest, error) {
	manifest, err := c.NewRequiredManifest(entries)
	if err != nil || expected.Validate() != nil {
		return c.RequiredManifest{}, unavailable(err)
	}
	digest, err := manifest.Digest()
	if err != nil || digest != expected {
		return c.RequiredManifest{}, unavailable(err)
	}
	return manifest, nil
}

func validateLifecyclePlan(project c.ProjectID, command c.CommandName, plan lifecycleCommandPlan) (c.RequiredManifest, event.Header, error) {
	empty := c.RequiredManifest{}
	next, err := nextVersion(plan.ExpectedVersion)
	if err != nil || project.Validate() != nil || plan.OperationID.Validate() != nil {
		return empty, event.Header{}, unavailable(err)
	}
	if command == c.ArchiveCommand {
		if plan.Action != c.Archive || plan.From != c.Active || plan.To != c.Archiving || plan.Confirmation != nil {
			return empty, event.Header{}, unavailable(nil)
		}
	} else if command == c.DeleteCommand {
		if plan.Action != c.Delete || plan.From != c.Active && plan.From != c.Archived || plan.To != c.Deleting || plan.Confirmation == nil || plan.Confirmation.Validate() != nil {
			return empty, event.Header{}, unavailable(nil)
		}
		path, _ := c.NormalizeConfirmationPath(plan.Confirmation.NormalizedCurrentPath)
		if path != plan.Confirmation.NormalizedCurrentPath {
			return empty, event.Header{}, unavailable(nil)
		}
	} else {
		return empty, event.Header{}, unavailable(nil)
	}
	manifest, err := storedLifecycleManifest(plan.Manifest, plan.ManifestDigest)
	if err != nil {
		return empty, event.Header{}, err
	}
	header, err := event.DecodeHeader(plan.Header)
	if err != nil || header.EventType != c.LifecycleChangedEventName || header.SchemaVersion != c.ProjectEventSchemaVersion || header.Scope.Kind != event.ProjectScope || header.Scope.ProjectID.String() != project.String() || header.AggregateType != c.ProjectAggregate || header.AggregateID.String() != project.String() || header.AggregateVersion == nil || *header.AggregateVersion != next || header.AggregateSequence != nil {
		return empty, event.Header{}, unavailable(err)
	}
	var payload c.LifecycleChangedPayload
	if err = decodeLifecycleJSON(plan.Payload, &payload); err != nil || payload.OperationID == nil || *payload.OperationID != plan.OperationID || payload.From != plan.From || payload.To != plan.To || payload.Action != plan.Action {
		return empty, event.Header{}, unavailable(err)
	}
	return manifest, header, nil
}

func loadLifecycleCommand(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, name c.CommandName, key foundation.IdempotencyKey) (*lifecycleCommandRecord, error) {
	var r lifecycleCommandRecord
	var rawPlan, rawResult, rawEventIDs []byte
	var storedProject, eventID, cause string
	var created time.Time
	err := x.QueryRow(ctx, `SELECT id::text,project_id::text,actor_user_id::text,command_name,key,semantic_digest,state,plan,safe_result,event_id::text,event_ids,audit_cause,created_at FROM agenteam_project.commands WHERE project_id=$1 AND command_name=$2 AND key=$3`, project.String(), string(name), string(key)).Scan(&r.id, &storedProject, &r.user, &r.name, &r.key, &r.semantic, &r.state, &rawPlan, &rawResult, &eventID, &rawEventIDs, &cause, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	r.project = project
	if storedProject != project.String() || r.name != name || r.key != key || r.semantic.Validate() != nil || r.state != "planned" && r.state != "completed" {
		return nil, unavailable(nil)
	}
	if _, err = parseID[struct{}](r.id); err != nil {
		return nil, err
	}
	if _, err = parseID[identity.User](r.user); err != nil {
		return nil, err
	}
	if err = decodeLifecycleJSON(rawPlan, &r.plan); err != nil {
		return nil, err
	}
	r.manifest, r.header, err = validateLifecyclePlan(project, name, r.plan)
	if err != nil {
		return nil, err
	}
	var eventIDs []string
	if decodeLifecycleJSON(rawEventIDs, &eventIDs) != nil || !slices.Equal(eventIDs, []string{eventID}) || eventID != r.header.EventID.String() || cause != commandAuditCause(r.identity()) || !created.Equal(r.header.OccurredAt.Time()) {
		return nil, unavailable(nil)
	}
	if r.state == "completed" {
		// This accepted snapshot proves the command's original association; all
		// public replays load the current canonical operation below instead.
		var receipt c.CommandResult
		if decodeLifecycleJSON(rawResult, &receipt) != nil || receipt.Command != name || receipt.Lifecycle == nil || receipt.Lifecycle.Operation == nil {
			return nil, unavailable(nil)
		}
		operation := receipt.Lifecycle.Operation
		if operation.ID != r.plan.OperationID || operation.ProjectID != project || operation.Action != r.plan.Action || operation.ProjectVersion != *r.header.AggregateVersion || operation.State != c.OperationAccepted || operation.Version != 1 {
			return nil, unavailable(nil)
		}
	} else if len(rawResult) != 0 {
		return nil, unavailable(nil)
	}
	return &r, nil
}

type lifecycleParticipantRecord struct {
	name          c.ParticipantName
	version       foundation.Version
	stop, cleanup string
	refs          []c.PendingRef
	reason        c.SafeReason
}

type lifecycleRecord struct {
	operation    c.LifecycleOperation
	owner        string
	manifest     c.RequiredManifest
	digest       foundation.Digest
	resume       c.OperationPhase
	cleanupStage string
	participants []lifecycleParticipantRecord
}

func loadLifecycleOperation(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, id c.OperationID) (*lifecycleRecord, error) {
	var r lifecycleRecord
	var storedID, storedProject string
	var projectVersion, version int64
	var completedVersion *int64
	var created, updated time.Time
	var completed *time.Time
	var rawManifest []byte
	err := x.QueryRow(ctx, `SELECT id::text,project_id::text,owner_user_id::text,action,project_version,completed_project_version,state,COALESCE(resume_phase,''),COALESCE(cleanup_stage,''),version,required_manifest,manifest_digest,COALESCE(safe_reason,''),created_at,updated_at,completed_at FROM agenteam_project.lifecycle_operations WHERE project_id=$1 AND id=$2`, project.String(), id.String()).Scan(&storedID, &storedProject, &r.owner, &r.operation.Action, &projectVersion, &completedVersion, &r.operation.State, &r.resume, &r.cleanupStage, &version, &rawManifest, &r.digest, &r.operation.SafeReason, &created, &updated, &completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if storedID != id.String() || storedProject != project.String() {
		return nil, unavailable(nil)
	}
	if _, err = parseID[identity.User](r.owner); err != nil {
		return nil, err
	}
	var entries []c.ParticipantRegistration
	if err = decodeLifecycleJSON(rawManifest, &entries); err != nil {
		return nil, err
	}
	r.manifest, err = storedLifecycleManifest(entries, r.digest)
	if err != nil {
		return nil, err
	}
	o := &r.operation
	o.ID, o.ProjectID, o.ProjectVersion, o.Version = id, project, foundation.Version(projectVersion), foundation.Version(version)
	o.CreatedAt, o.UpdatedAt = instant(created), instant(updated)
	if completed != nil {
		v := instant(*completed)
		o.CompletedAt = &v
	}
	if completedVersion != nil {
		v := foundation.Version(*completedVersion)
		o.CompletedProjectVersion = &v
	}
	rows, err := x.Query(ctx, `SELECT participant_name,contract_version,stop_state,cleanup_state,safe_pending_refs,COALESCE(safe_reason,''),version FROM agenteam_project.lifecycle_participants WHERE operation_id=$1 ORDER BY participant_name`, id.String())
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	for rows.Next() {
		var participant lifecycleParticipantRecord
		var rawRefs []byte
		var rowVersion int64
		if err = rows.Scan(&participant.name, &participant.version, &participant.stop, &participant.cleanup, &rawRefs, &participant.reason, &rowVersion); err != nil {
			return nil, unavailable(err)
		}
		if err = decodeLifecycleJSON(rawRefs, &participant.refs); err != nil || rowVersion < 1 {
			return nil, unavailable(err)
		}
		r.participants = append(r.participants, participant)
	}
	if err = rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	if err = projectLifecycleProgress(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Validate every participant and reference before applying the public DTO cap.
func projectLifecycleProgress(r *lifecycleRecord) error {
	o := &r.operation
	entries := r.manifest.Entries()
	if len(entries) != len(r.participants) || r.resume != "" && r.resume.Validate() != nil || o.Action == c.Archive && (r.resume == c.CleanupPhase || r.cleanupStage != "") {
		return unavailable(nil)
	}
	cleaning := o.Action == c.Delete && (o.State == c.OperationCleaning || o.State == c.OperationFailed && r.resume == c.CleanupPhase)
	if cleaning != (r.cleanupStage != "") || r.cleanupStage != "" && !slices.Contains([]string{"domains", "outbox", "audit", "final"}, r.cleanupStage) || o.State == c.OperationFailed && r.resume == "" || o.State != c.OperationFailed && r.resume != "" {
		return unavailable(nil)
	}
	o.RequiredParticipants = make([]c.ParticipantName, 0, len(entries))
	o.CompletedParticipants = make([]c.ParticipantName, 0, len(entries))
	refs := []c.PendingRef{}
	for i, entry := range entries {
		p := r.participants[i]
		if p.name != entry.Name || p.version != entry.ContractVersion || !slices.Contains([]string{"required", "pending", "stopped", "failed"}, p.stop) || p.reason != "" && p.reason.Validate() != nil {
			return unavailable(nil)
		}
		if o.Action == c.Archive && p.cleanup != "not_applicable" || o.Action == c.Delete && !slices.Contains([]string{"required", "pending", "completed", "failed"}, p.cleanup) || cleaning && p.stop != "stopped" {
			return unavailable(nil)
		}
		if o.State == c.OperationAccepted && (o.Version != 1 || p.stop != "required" || o.Action == c.Delete && p.cleanup != "required" || len(p.refs) != 0 || p.reason != "") {
			return unavailable(nil)
		}
		complete := p.stop == "stopped"
		if cleaning {
			complete = p.cleanup == "completed"
		}
		if complete && len(p.refs) != 0 {
			return unavailable(nil)
		}
		o.RequiredParticipants = append(o.RequiredParticipants, p.name)
		if complete {
			o.CompletedParticipants = append(o.CompletedParticipants, p.name)
		}
		for _, ref := range p.refs {
			if ref.Participant != p.name {
				return unavailable(nil)
			}
		}
		refs = append(refs, p.refs...)
	}
	if err := r.manifest.ValidateRefs(refs); err != nil {
		return unavailable(err)
	}
	o.PendingRefsTruncated = len(refs) > 100
	if o.PendingRefsTruncated {
		refs = refs[:100]
	}
	o.PendingResources = refs
	if err := (c.LifecycleResult{Operation: o}).Validate(); err != nil {
		return unavailable(err)
	}
	return nil
}

func loadDeletionReceipt(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID) (*c.ProjectDeletionReceipt, error) {
	var operation, target, owner, status string
	var data c.DeletionReceiptData
	var completed time.Time
	err := x.QueryRow(ctx, `SELECT operation_id::text,deleted_project_id::text,original_owner_user_id::text,command_key_hash,request_digest,completed_at,status FROM agenteam_project.deletion_receipts WHERE deleted_project_id=$1`, project.String()).Scan(&operation, &target, &owner, &data.CommandKeyHash, &data.RequestDigest, &completed, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if target != project.String() || status != "completed" {
		return nil, unavailable(nil)
	}
	data.OperationID, err = parseID[c.Operation](operation)
	if err != nil {
		return nil, err
	}
	data.OriginalOwnerUserID, err = parseID[identity.User](owner)
	if err != nil {
		return nil, err
	}
	data.DeletedProjectID, data.CompletedAt = project, instant(completed)
	receipt, err := c.NewProjectDeletionReceipt(data)
	if err != nil {
		return nil, unavailable(err)
	}
	return &receipt, nil
}

func lifecycleCommandResult(ctx context.Context, x postgres.SQLExecutor, command *lifecycleCommandRecord) (c.LifecycleResult, error) {
	r, err := loadLifecycleOperation(ctx, x, command.project, command.plan.OperationID)
	if err != nil {
		return c.LifecycleResult{}, err
	}
	if r == nil || r.owner != command.user || r.operation.Action != command.plan.Action || r.operation.ProjectVersion != *command.header.AggregateVersion || r.digest != command.plan.ManifestDigest {
		return c.LifecycleResult{}, unavailable(nil)
	}
	return c.LifecycleResult{Operation: &r.operation}, nil
}

func sameLifecyclePlan(a, b *lifecycleCommandRecord) bool {
	if a == nil || b == nil || a.id != b.id || a.semantic != b.semantic {
		return false
	}
	x, err := json.Marshal(a.plan)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b.plan)
	if err != nil {
		return false
	}
	xd, err := cursor.Digest(x)
	if err != nil {
		return false
	}
	yd, err := cursor.Digest(y)
	return err == nil && xd == yd
}
