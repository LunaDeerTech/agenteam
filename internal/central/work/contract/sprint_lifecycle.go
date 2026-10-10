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

const SprintStartCommandName = "work.sprint.start"
const SprintStartedName event.StableName = "work.sprint_started"
const SprintLifecycleSchemaVersion uint32 = 1

type SprintStartCommand struct{}
type SprintStartCommandID = f.ID[SprintStartCommand]

func SprintStartIdentity(project ProjectID, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if project.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid("", "INVALID_COMMAND")
	}
	return f.NewCommandIdentity("project", []string{project.String()}, SprintStartCommandName, key)
}
func StartSprintDigest(actor i.Actor, meta f.CommandMeta, project ProjectID, sprint SprintID) (f.Digest, error) {
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
	}{"work-sprint-start-v1", project, sprint, actor.Details().UserID, *meta.ExpectedVersion})
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

type SprintStartMutation struct {
	Sprint  Sprint        `json:"sprint"`
	Project pc.ProjectRef `json:"project"`
	EventID event.EventID `json:"event_id"`
}

func (v SprintStartMutation) Validate() error {
	if v.Sprint.Validate() != nil || v.Project.Validate() != nil || v.EventID.Validate() != nil || v.Sprint.State != Current || v.Sprint.ProjectID != v.Project.ID || v.Project.CurrentSprintID == nil || *v.Project.CurrentSprintID != v.Sprint.ID || v.Project.Lifecycle != pc.Active || v.Sprint.Version <= 1 || v.Project.Version <= 1 || !v.Sprint.UpdatedAt.Time().Equal(v.Project.UpdatedAt.Time()) {
		return invalid("", "INVALID_SPRINT_START_RECEIPT")
	}
	return nil
}
func (v SprintStartMutation) Clone() SprintStartMutation {
	v.Sprint = v.Sprint.Clone()
	v.Project = v.Project.Clone()
	return v
}
func (v SprintStartMutation) MarshalJSON() ([]byte, error) {
	type wire SprintStartMutation
	return checked(wire(v), v.Validate())
}
func (v *SprintStartMutation) UnmarshalJSON(raw []byte) error {
	type wire SprintStartMutation
	next, err := decodeFields[wire](raw, []string{"sprint", "project", "event_id"}, nil, nil)
	if err != nil {
		return err
	}
	x := SprintStartMutation(next)
	if err = x.Validate(); err == nil {
		*v = x
	}
	return err
}

type SprintStartLookupRequest struct {
	ProjectID ProjectID
	Key       f.IdempotencyKey
	Semantic  f.Digest
}

func (v SprintStartLookupRequest) Validate() error {
	if v.ProjectID.Validate() != nil || v.Key.Validate() != nil || v.Semantic.Validate() != nil {
		return invalid("", "INVALID_LOOKUP")
	}
	return nil
}

type SprintStartLookup struct {
	State  LookupState
	Result *SprintStartMutation
}
type SprintLifecycleCommands interface {
	StartSprint(context.Context, i.Actor, f.CommandMeta, ProjectID, SprintID) (SprintStartMutation, error)
	LookupStartSprint(context.Context, i.Actor, SprintStartLookupRequest) (SprintStartLookup, error)
}
type SprintStarted struct {
	CommandID      SprintStartCommandID `json:"command_id"`
	ActorUserID    i.UserID             `json:"actor_user_id"`
	MilestoneID    MilestoneID          `json:"milestone_id"`
	ProjectVersion f.Version            `json:"project_version"`
}

func (v SprintStarted) Validate() error {
	if v.CommandID.Validate() != nil || v.ActorUserID.Validate() != nil || v.MilestoneID.Validate() != nil || v.ProjectVersion <= 1 {
		return invalid("", "INVALID_SPRINT_STARTED")
	}
	return nil
}
func (v SprintStarted) MarshalJSON() ([]byte, error) {
	type wire SprintStarted
	return checked(wire(v), v.Validate())
}
func (v *SprintStarted) UnmarshalJSON(raw []byte) error {
	type wire SprintStarted
	x, err := decodeFields[wire](raw, []string{"command_id", "actor_user_id", "milestone_id", "project_version"}, nil, nil)
	if err != nil {
		return err
	}
	next := SprintStarted(x)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

type SprintLifecycleEvents struct {
	catalog *event.Catalog
	started event.EventType[SprintStarted]
}

func RegisterSprintLifecycleEvents(catalog *event.Catalog) (SprintLifecycleEvents, error) {
	if !catalog.Valid() {
		return SprintLifecycleEvents{}, invalid("", "INVALID_CATALOG")
	}
	started, err := event.DefineEvent(catalog, event.Definition[SprintStarted]{Schema: event.Schema{Producer: WorkProducer, EventType: SprintStartedName, AggregateType: SprintAggregate, Version: SprintLifecycleSchemaVersion}, Codec: event.JSONCodec[SprintStarted]{}, Validate: SprintStarted.Validate})
	if err != nil {
		return SprintLifecycleEvents{}, err
	}
	return SprintLifecycleEvents{catalog, started}, nil
}
func (v SprintLifecycleEvents) Valid() bool { return v.catalog != nil && v.catalog.Valid() }
func (v SprintLifecycleEvents) NewSprintStarted(h event.Header, p SprintStarted) (event.Event, error) {
	if !v.Valid() {
		return event.Event{}, invalid("", "INVALID_CATALOG")
	}
	if h.Validate() != nil || h.EventType != SprintStartedName || h.AggregateType != SprintAggregate || h.SchemaVersion != SprintLifecycleSchemaVersion || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || *h.AggregateVersion <= 1 || h.AggregateSequence != nil {
		return event.Event{}, invalid("", "INVALID_EVENT_HEADER")
	}
	return event.NewEvent(v.started, h, p)
}
func (v SprintLifecycleEvents) Restore(h event.Header, raw []byte) (event.Event, error) {
	var p SprintStarted
	if len(raw) > 4096 || json.Unmarshal(raw, &p) != nil {
		return event.Event{}, invalid("", "INVALID_SPRINT_STARTED")
	}
	return v.NewSprintStarted(h, p)
}
func (v SprintLifecycleEvents) DecodeSprintStarted(e event.Event) (SprintStarted, error) {
	if !v.Valid() || !v.catalog.Owns(e) || e.Summary().Producer != WorkProducer {
		return SprintStarted{}, invalid("", "FOREIGN_EVENT")
	}
	p, err := event.DecodeEvent(v.started, e)
	if err == nil {
		_, err = v.NewSprintStarted(e.Header(), p)
	}
	return p, err
}
func (SprintStartMutation) Format(w fmt.State, r rune)      { safeFormat(w, r) }
func (SprintStartMutation) LogValue() slog.Value            { return safeLog() }
func (SprintStartLookupRequest) Format(w fmt.State, r rune) { safeFormat(w, r) }
func (SprintStartLookupRequest) LogValue() slog.Value       { return safeLog() }
func (SprintStarted) Format(w fmt.State, r rune)            { safeFormat(w, r) }
func (SprintStarted) LogValue() slog.Value                  { return safeLog() }
