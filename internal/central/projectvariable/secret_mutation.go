package projectvariable

import (
	"context"
	"math"
	"slices"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func secretPlanPayload(p *secretMutationPlan) c.SecretVariableChanged {
	r := p.Request.Fields()
	kind := c.Updated
	if r.Kind == sc.Create {
		kind = c.Created
	}
	if r.Kind == sc.Delete {
		kind = c.Deleted
	}
	return c.SecretVariableChanged{VariableID: r.VariableID, OperationID: p.Operation, Change: kind, ChangedFields: slices.Clone(p.Fields)}
}
func secretPlanHeader(p *secretMutationPlan) event.Header {
	r := p.Request.Fields()
	project, _ := f.ParseID[event.Project](r.ProjectID.String())
	target, _ := f.ParseID[event.Aggregate](r.VariableID.String())
	version := p.After.Fields().Version
	return event.Header{EventID: p.Event, EventType: c.SecretVariableChangedName, SchemaVersion: 1, OccurredAt: p.After.Fields().UpdatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: project}, AggregateType: c.SecretVariableAggregate, AggregateID: target, AggregateVersion: &version}
}
func secretPlanAudit(p *secretMutationPlan) (ac.Entry, ac.AppendKey, error) {
	empty := ac.Entry{}
	key := ac.AppendKey{}
	if p == nil || len(p.Fields) == 0 || p.Event.Validate() != nil {
		return empty, key, internal(nil)
	}
	r := p.Request.Fields()
	action := ac.Action(r.Identity.Command())
	metadata, err := ac.ProjectSecretVariableMetadata(action, ac.ProjectSecretVariableMetadataFields{VariableID: r.VariableID.String(), Version: p.After.Fields().Version, ChangedFields: p.Fields})
	if err != nil {
		return empty, key, internal(err)
	}
	scope, err := i.InProject(r.ProjectID)
	if err != nil {
		return empty, key, internal(err)
	}
	resource, err := ac.NewResource(ac.ProjectVariableResource, r.VariableID.String())
	if err != nil {
		return empty, key, internal(err)
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: r.Actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		return empty, key, internal(err)
	}
	cause, err := cursor.Digest([]byte(r.Identity.Canonical()))
	if err != nil {
		return empty, key, internal(err)
	}
	key, err = ac.NewAppendKey(ac.ProjectVariableProducer, cause.String(), 0)
	return entry, key, portError(err)
}
func checkSecretPreimage(ctx context.Context, x postgres.SQLExecutor, p *secretMutationPlan) error {
	if p == nil || p.Request.Validate() != nil || p.After.Validate() != nil {
		return internal(nil)
	}
	r := p.Request.Fields()
	if r.Kind == sc.Create {
		if err := secretRequireUnusedID(ctx, x, r.ProjectID, r.VariableID); err != nil {
			return err
		}
		var count int64
		if err := x.QueryRow(ctx, `SELECT count(*) FROM agenteam_projectvariable.variables WHERE project_id=$1 AND type='secret' AND deleted_at IS NULL`, r.ProjectID.String()).Scan(&count); err != nil {
			return unavailable(err)
		}
		if count >= c.MaxSecretVariables {
			return field(f.ResourceBusy, "/variable_id", "PROJECT_SECRET_VARIABLE_LIMIT")
		}
	} else {
		before, err := loadSecretVariable(ctx, x, r.ProjectID, r.VariableID, false)
		if err != nil {
			return err
		}
		if !secretRowSame(before, p.Before) {
			return fault(f.VersionConflict)
		}
	}
	if p.Deleted {
		var referenced bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_references WHERE project_id=$1 AND variable_id=$2)`, r.ProjectID.String(), r.VariableID.String()).Scan(&referenced); err != nil {
			return unavailable(err)
		}
		if referenced {
			return field(f.ResourceBusy, "/variable_id", "SECRET_VARIABLE_REFERENCED")
		}
	} else {
		var conflict bool
		if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.variables WHERE project_id=$1 AND name=$2 AND id<>$3 AND deleted_at IS NULL)`, r.ProjectID.String(), p.After.Fields().Name, r.VariableID.String()).Scan(&conflict); err != nil {
			return unavailable(err)
		}
		if conflict {
			return field(f.ResourceBusy, "/request/name", "NAME_CONFLICT")
		}
	}
	if len(p.Fields) != 0 {
		generation, err := secretGeneration(ctx, x, r.ProjectID)
		if err != nil {
			return err
		}
		if generation == math.MaxInt64 {
			return field(f.ResourceBusy, "/project_id", "VERSION_EXHAUSTED")
		}
	}
	return nil
}

func validateSecretApplied(p *secretMutationPlan, preparation sc.ProjectVariablePreparationFields, observation sc.ProjectVariableWriteObservation, value bool) error {
	if p == nil || !observation.Observed() {
		return internal(nil)
	}
	r := p.Request.Fields()
	actual, err := observation.Result()
	if err != nil {
		return internal(err)
	}
	pr := preparation.Request.Fields()
	if !r.Actor.Equal(pr.Actor) || r.ProjectID != pr.ProjectID || r.VariableID != pr.VariableID || r.Identity.Canonical() != pr.Identity.Canonical() || r.Kind != pr.Kind || !sameSecretExpected(r.ExpectedVersion, pr.ExpectedVersion) ||
		actual.ReceiptID != preparation.ReceiptID || !actual.Ref.Equal(preparation.Ref) || actual.ProjectID != r.ProjectID || actual.VariableID != r.VariableID || actual.UserID.String() != r.Actor.Details().UserID || actual.Identity.Canonical() != r.Identity.Canonical() || actual.Kind != r.Kind || !sameSecretExpected(actual.ExpectedVersion, r.ExpectedVersion) {
		return internal(nil)
	}
	effect, version, deleted := sc.ProjectVariableCreated, f.Version(1), false
	if r.Kind != sc.Create {
		if p.Before == nil {
			return internal(nil)
		}
		version = p.Before.CredentialVersion
		effect = sc.ProjectVariableUnchanged
		if value || r.Kind == sc.Delete {
			version, err = nextVersion(version)
			if err != nil {
				return err
			}
			effect = sc.ProjectVariableReplaced
		}
		if r.Kind == sc.Delete {
			effect = sc.ProjectVariableDeleted
			deleted = true
		}
	}
	if actual.Effect != effect || actual.Version != version || actual.Deleted != deleted {
		return internal(nil)
	}
	return nil
}
func applySecretPlan(ctx context.Context, x postgres.SQLExecutor, p *secretMutationPlan, observation sc.ProjectVariableWriteObservation) error {
	if p == nil || len(p.Fields) == 0 || p.Event.Validate() != nil {
		return internal(nil)
	}
	r := p.Request.Fields()
	v := p.After.Fields()
	o, err := observation.Result()
	if err != nil {
		return internal(err)
	}
	if r.Kind == sc.Create {
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_projectvariable.variables(id,project_id,type,name,description,value,version,created_at,updated_at,credential_id,credential_version) VALUES($1,$2,'secret',$3,$4,NULL,1,$5,$5,$6,$7) ON CONFLICT ON CONSTRAINT variables_pkey DO NOTHING`, v.ID.String(), v.ProjectID.String(), v.Name, v.Description, v.CreatedAt.Time(), o.Ref.Details().ID.String(), int64(o.Version))
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() == 0 {
			return fault(f.NotFound)
		}
		if err = affected(tag, nil); err != nil {
			return err
		}
	} else {
		var deleted, credential, credentialVersion any
		if p.Deleted {
			deleted = v.UpdatedAt.Time()
		} else {
			credential = o.Ref.Details().ID.String()
			credentialVersion = int64(o.Version)
		}
		if err = affected(x.Exec(ctx, `UPDATE agenteam_projectvariable.variables SET name=$3,description=$4,version=$5,updated_at=$6,deleted_at=$7,credential_id=$8,credential_version=$9 WHERE project_id=$1 AND id=$2 AND type='secret' AND version=$10 AND deleted_at IS NULL`, v.ProjectID.String(), v.ID.String(), v.Name, v.Description, int64(v.Version), v.UpdatedAt.Time(), deleted, credential, credentialVersion, int64(p.Before.Variable.Fields().Version))); err != nil {
			return err
		}
	}
	if err = affected(x.Exec(ctx, `INSERT INTO agenteam_projectvariable.secret_project_generations(project_id,query_generation) VALUES($1,2) ON CONFLICT(project_id) DO UPDATE SET query_generation=agenteam_projectvariable.secret_project_generations.query_generation+1`, r.ProjectID.String())); err != nil {
		return err
	}
	fields, err := canonical(p.Fields)
	if err != nil {
		return err
	}
	return affected(x.Exec(ctx, `INSERT INTO agenteam_projectvariable.secret_history(id,project_id,variable_id,operation_id,version,kind,changed_fields,actor_user_id,occurred_at,event_id) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)`, p.History.String(), r.ProjectID.String(), r.VariableID.String(), p.Operation.String(), int64(v.Version), string(secretPlanPayload(p).Change), string(fields), r.Actor.Details().UserID, v.UpdatedAt.Time(), p.Event.String()))
}
func secretCompletedRecord(p *secretMutationPlan, observation sc.ProjectVariableWriteObservation, metadata sc.ProjectVariableIntentMetadata, auditID *ac.ID) (*secretCommandRecord, error) {
	if p == nil {
		return nil, internal(nil)
	}
	r := p.Request.Fields()
	fields := c.SecretVariableMutationFields{Command: c.SecretCommandName(r.Identity.Command()), Changed: len(p.Fields) != 0, Variable: p.After}
	if fields.Changed {
		fields.EventID = &p.Event
		fields.AuditID = auditID
	}
	if p.Deleted {
		v := p.After.Fields()
		fields.Variable = c.SecretVariable{}
		fields.Deleted = &c.SecretVariableDeleted{ID: v.ID, ProjectID: v.ProjectID, Type: v.Type, Version: v.Version, DeletedAt: v.UpdatedAt}
	}
	receipt, err := c.NewSecretVariableMutation(fields)
	if err != nil {
		return nil, internal(err)
	}
	user, err := f.ParseID[i.User](r.Actor.Details().UserID)
	if err != nil {
		return nil, internal(err)
	}
	result := &secretCommandRecord{ID: p.Operation, Project: r.ProjectID, User: user, Command: fields.Command, Key: r.Identity.Key(), Target: r.VariableID, Expected: r.ExpectedVersion, NamePresent: metadata.Name != nil, DescriptionPresent: metadata.Description != nil, ValuePresent: metadata.ValuePresent, Observation: observation, Receipt: receipt, CommittedAt: p.At}
	if err = validateSecretRecord(result); err != nil {
		return nil, err
	}
	return result, nil
}
func verifySecretPostimage(ctx context.Context, x postgres.SQLExecutor, p *secretMutationPlan, observation sc.ProjectVariableWriteObservation) error {
	if p == nil || len(p.Fields) == 0 || p.Event.Validate() != nil {
		return fault(f.Forbidden)
	}
	r := p.Request.Fields()
	v, err := loadSecretVariable(ctx, x, r.ProjectID, r.VariableID, true)
	if err != nil {
		return err
	}
	if !sameValue(v.Variable, p.After) || (v.Deleted != nil) != p.Deleted {
		return fault(f.Forbidden)
	}
	o, err := observation.Result()
	if err != nil {
		return fault(f.Forbidden)
	}
	if p.Deleted {
		if *v.Deleted != p.After.Fields().UpdatedAt {
			return fault(f.Forbidden)
		}
	} else if !v.Ref.Equal(o.Ref) || v.CredentialVersion != o.Version {
		return fault(f.Forbidden)
	}
	var id, project, target, operation, kind, user, eventID string
	var version int64
	var fields []byte
	var at time.Time
	err = x.QueryRow(ctx, `SELECT id::text,project_id::text,variable_id::text,operation_id::text,version,kind,changed_fields,actor_user_id::text,occurred_at,event_id::text FROM agenteam_projectvariable.secret_history WHERE operation_id=$1`, p.Operation.String()).Scan(&id, &project, &target, &operation, &version, &kind, &fields, &user, &at, &eventID)
	if err != nil {
		return unavailable(err)
	}
	expected, err := canonical(p.Fields)
	if err != nil {
		return err
	}
	actual, err := cursor.CanonicalJSON(fields)
	if err != nil {
		return internal(err)
	}
	if id != p.History.String() || project != r.ProjectID.String() || target != r.VariableID.String() || operation != p.Operation.String() || version != int64(p.After.Fields().Version) || kind != string(secretPlanPayload(p).Change) || user != r.Actor.Details().UserID || eventID != p.Event.String() || !at.Equal(p.After.Fields().UpdatedAt.Time()) || string(expected) != string(actual) {
		return fault(f.Forbidden)
	}
	return nil
}
