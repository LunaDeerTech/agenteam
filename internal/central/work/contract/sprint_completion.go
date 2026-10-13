package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const SprintCompleteCommandName = "work.sprint.complete"
const SprintCompletedName event.StableName = "work.sprint_completed"
const SprintCompletionSchemaVersion uint32 = 1

type SprintCompleteCommand struct{}
type SprintCompleteCommandID = f.ID[SprintCompleteCommand]

func SprintCompleteIdentity(project ProjectID, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if project.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid("", "INVALID_COMMAND")
	}
	return f.NewCommandIdentity("project", []string{project.String()}, SprintCompleteCommandName, key)
}
func CompleteSprintDigest(actor i.Actor, meta f.CommandMeta, project ProjectID, sprint SprintID) (f.Digest, error) {
	if err := ValidateActor(actor); err != nil {
		return "", err
	}
	if meta.Validate() != nil || meta.ExpectedVersion == nil || meta.ExpectedVersion.Validate() != nil || project.Validate() != nil || sprint.Validate() != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	raw, err := json.Marshal(struct {
		Format   string    `json:"format"`
		Project  ProjectID `json:"project_id"`
		Sprint   SprintID  `json:"sprint_id"`
		User     string    `json:"actor_user_id"`
		Expected f.Version `json:"expected_version"`
	}{"work-sprint-complete-v1", project, sprint, actor.Details().UserID, *meta.ExpectedVersion})
	if err != nil {
		return "", err
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

type SprintCompleteMutation struct {
	Sprint  Sprint        `json:"sprint"`
	Project pc.ProjectRef `json:"project"`
	EventID event.EventID `json:"event_id"`
}

func (v SprintCompleteMutation) Validate() error {
	if v.Sprint.Validate() != nil || v.Project.Validate() != nil || v.EventID.Validate() != nil || v.Sprint.State != Completed || v.Sprint.ProjectID != v.Project.ID || v.Project.CurrentSprintID != nil || v.Project.Lifecycle != pc.Active || v.Sprint.CompletedBy == nil || v.Sprint.CompletedBy.Kind != i.Human || v.Sprint.CompletedBy.UserID != v.Project.OwnerUserID.String() || v.Sprint.CompletedAt == nil || !v.Sprint.CompletedAt.Time().Equal(v.Sprint.UpdatedAt.Time()) || v.Sprint.Version <= 1 || v.Project.Version <= 1 || !v.Sprint.UpdatedAt.Time().Equal(v.Project.UpdatedAt.Time()) {
		return invalid("", "INVALID_SPRINT_COMPLETE_RECEIPT")
	}
	return nil
}
func (v SprintCompleteMutation) Clone() SprintCompleteMutation {
	v.Sprint = v.Sprint.Clone()
	v.Project = v.Project.Clone()
	return v
}
func (v SprintCompleteMutation) MarshalJSON() ([]byte, error) {
	type wire SprintCompleteMutation
	return checked(wire(v), v.Validate())
}
func (v *SprintCompleteMutation) UnmarshalJSON(raw []byte) error {
	type wire SprintCompleteMutation
	next, err := decodeFields[wire](raw, []string{"sprint", "project", "event_id"}, nil, nil)
	if err != nil {
		return err
	}
	x := SprintCompleteMutation(next)
	if err = x.Validate(); err == nil {
		*v = x
	}
	return err
}

type SprintCompleteLookupRequest struct {
	ProjectID ProjectID
	Key       f.IdempotencyKey
	Semantic  f.Digest
}

func (v SprintCompleteLookupRequest) Validate() error {
	if v.ProjectID.Validate() != nil || v.Key.Validate() != nil || v.Semantic.Validate() != nil {
		return invalid("", "INVALID_LOOKUP")
	}
	return nil
}

type SprintCompleteLookup struct {
	State  LookupState
	Result *SprintCompleteMutation
}
type SprintCompletionCommands interface {
	CompleteSprint(context.Context, i.Actor, f.CommandMeta, ProjectID, SprintID) (SprintCompleteMutation, error)
	LookupCompleteSprint(context.Context, i.Actor, SprintCompleteLookupRequest) (SprintCompleteLookup, error)
}
type SprintCompleted struct {
	CommandID      SprintCompleteCommandID `json:"command_id"`
	ActorUserID    i.UserID                `json:"actor_user_id"`
	MilestoneID    MilestoneID             `json:"milestone_id"`
	ProjectVersion f.Version               `json:"project_version"`
}

func (v SprintCompleted) Validate() error {
	if v.CommandID.Validate() != nil || v.ActorUserID.Validate() != nil || v.MilestoneID.Validate() != nil || v.ProjectVersion <= 1 {
		return invalid("", "INVALID_SPRINT_COMPLETED")
	}
	return nil
}
func (v SprintCompleted) MarshalJSON() ([]byte, error) {
	type wire SprintCompleted
	return checked(wire(v), v.Validate())
}
func (v *SprintCompleted) UnmarshalJSON(raw []byte) error {
	type wire SprintCompleted
	x, err := decodeFields[wire](raw, []string{"command_id", "actor_user_id", "milestone_id", "project_version"}, nil, nil)
	if err != nil {
		return err
	}
	next := SprintCompleted(x)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

type SprintCompletionEvents struct {
	catalog   *event.Catalog
	completed event.EventType[SprintCompleted]
}

func RegisterSprintCompletionEvents(catalog *event.Catalog) (SprintCompletionEvents, error) {
	if !catalog.Valid() {
		return SprintCompletionEvents{}, invalid("", "INVALID_CATALOG")
	}
	completed, err := event.DefineEvent(catalog, event.Definition[SprintCompleted]{Schema: event.Schema{Producer: WorkProducer, EventType: SprintCompletedName, AggregateType: SprintAggregate, Version: SprintCompletionSchemaVersion}, Codec: event.JSONCodec[SprintCompleted]{}, Validate: SprintCompleted.Validate})
	if err != nil {
		return SprintCompletionEvents{}, err
	}
	return SprintCompletionEvents{catalog, completed}, nil
}
func (v SprintCompletionEvents) Valid() bool { return v.catalog != nil && v.catalog.Valid() }
func (v SprintCompletionEvents) NewSprintCompleted(h event.Header, p SprintCompleted) (event.Event, error) {
	if !v.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	if h.Validate() != nil || h.EventType != SprintCompletedName || h.AggregateType != SprintAggregate || h.SchemaVersion != SprintCompletionSchemaVersion || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || *h.AggregateVersion <= 1 || h.AggregateSequence != nil {
		return event.Event{}, invalid("", "INVALID_EVENT_HEADER")
	}
	return event.NewEvent(v.completed, h, p)
}
func (v SprintCompletionEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	var p SprintCompleted
	if len(raw) > 4096 || json.Unmarshal(raw, &p) != nil {
		return event.Event{}, invalid("", "INVALID_SPRINT_COMPLETED")
	}
	return v.NewSprintCompleted(h, p)
}
func (v SprintCompletionEvents) DecodeSprintCompleted(e event.Event) (SprintCompleted, error) {
	if !v.Valid() || !v.catalog.Owns(e) || e.Summary().Producer != WorkProducer {
		return SprintCompleted{}, invalid("", "FOREIGN_EVENT")
	}
	p, err := event.DecodeEvent(v.completed, e)
	if err == nil {
		_, err = v.NewSprintCompleted(e.Header(), p)
	}
	return p, err
}
func (SprintCompleteMutation) Format(w fmt.State, r rune)      { safeFormat(w, r) }
func (SprintCompleteMutation) LogValue() slog.Value            { return safeLog() }
func (SprintCompleteLookupRequest) Format(w fmt.State, r rune) { safeFormat(w, r) }
func (SprintCompleteLookupRequest) LogValue() slog.Value       { return safeLog() }
func (SprintCompleted) Format(w fmt.State, r rune)             { safeFormat(w, r) }
func (SprintCompleted) LogValue() slog.Value                   { return safeLog() }
