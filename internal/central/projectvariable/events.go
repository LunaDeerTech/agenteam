package projectvariable

import (
	"context"
	"slices"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func recordPayload(r *commandRecord) c.VariableChanged {
	change := c.Updated
	if r.Command == c.CreateCommand {
		change = c.Created
	}
	if r.Command == c.DeleteCommand {
		change = c.Deleted
	}
	return c.VariableChanged{VariableID: r.Target, OperationID: r.ID, Change: change, ChangedFields: slices.Clone(r.Plan.Fields)}
}
func recordHeader(r *commandRecord) event.Header {
	p, _ := f.ParseID[event.Project](r.Project.String())
	id, _ := f.ParseID[event.Aggregate](r.Target.String())
	v := r.Plan.After.Fields()
	return event.Header{EventID: *r.EventID, EventType: c.VariableChangedName, SchemaVersion: c.VariableSchemaVersion, OccurredAt: v.UpdatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: p}, AggregateType: c.VariableAggregate, AggregateID: id, AggregateVersion: &v.Version}
}
func recordAudit(r *commandRecord, a i.Actor) (ac.Entry, ac.AppendKey, error) {
	empty := ac.Entry{}
	key := ac.AppendKey{}
	if r == nil || r.Plan == nil {
		return empty, key, internal(nil)
	}
	v := r.Plan.After.Fields()
	action := ac.Action(r.Command)
	metadata, e := ac.ProjectVariableMetadata(action, ac.ProjectVariableMetadataFields{VariableID: r.Target.String(), Version: v.Version, ChangedFields: r.Plan.Fields})
	if e != nil {
		return empty, key, internal(e)
	}
	scope, e := i.InProject(r.Project)
	if e != nil {
		return empty, key, internal(e)
	}
	resource, e := ac.NewResource(ac.ProjectVariableResource, r.Target.String())
	if e != nil {
		return empty, key, internal(e)
	}
	entry, e := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: a, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if e != nil {
		return empty, key, internal(e)
	}
	id, e := c.VariableCommandIdentity(r.Project, r.Command, r.Key)
	if e != nil {
		return empty, key, internal(e)
	}
	cause, e := cursor.Digest([]byte(id.Canonical()))
	if e != nil {
		return empty, key, internal(e)
	}
	key, e = ac.NewAppendKey(ac.ProjectVariableProducer, cause.String(), 0)
	if e != nil {
		return empty, key, internal(e)
	}
	return entry, key, nil
}
func verifyPostimage(ctx context.Context, x postgres.SQLExecutor, r *commandRecord) error {
	if r == nil || r.State != "planned" || r.Plan == nil || r.EventID == nil {
		return fault(f.Forbidden)
	}
	p := r.Plan
	v, deleted, e := loadVariable(ctx, x, r.Project, r.Target, true)
	if e != nil {
		return e
	}
	if !sameValue(v, p.After) || (deleted != nil) != p.Deleted {
		return fault(f.Forbidden)
	}
	if deleted != nil && !deleted.Time().Equal(p.After.Fields().UpdatedAt.Time()) {
		return fault(f.Forbidden)
	}
	var id, project, target, operation, kind, user, eventID string
	var version int64
	var fields []byte
	var at time.Time
	e = x.QueryRow(ctx, `SELECT id::text,project_id::text,variable_id::text,operation_id::text,version,kind,changed_fields,actor_user_id::text,occurred_at,event_id::text FROM agenteam_projectvariable.history WHERE operation_id=$1`, r.ID.String()).Scan(&id, &project, &target, &operation, &version, &kind, &fields, &user, &at, &eventID)
	if e != nil {
		return unavailable(e)
	}
	expected, e := canonical(p.Fields)
	if e != nil {
		return e
	}
	actual, e := cursor.CanonicalJSON(fields)
	if e != nil {
		return internal(nil)
	}
	if id != p.HistoryID.String() || project != r.Project.String() || target != r.Target.String() || operation != r.ID.String() || version != int64(p.After.Fields().Version) || kind != string(recordPayload(r).Change) || user != r.User.String() || eventID != r.EventID.String() || !at.Equal(p.After.Fields().UpdatedAt.Time()) || string(expected) != string(actual) {
		return fault(f.Forbidden)
	}
	return nil
}
