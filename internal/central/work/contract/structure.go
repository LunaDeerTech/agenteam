// Package contract defines the bounded Human-owned Work structure library.
package contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type ProjectID = i.ProjectID
type SprintID = pc.SprintID
type MilestoneID = f.ID[Milestone]
type StructureCommand struct{}
type CommandID = f.ID[StructureCommand]

const (
	MaxTitleScalars     = 256
	MaxTitleBytes       = 1024
	MaxDescriptionBytes = 32768
	MaxRequestBytes     = 256 << 10
	MaxGroupSize        = 4096
)

// Placement contains two independently bounded public objects, not a command
// request or receipt. HTML escaping can make their combined encoding larger.
const maxPlacementBytes = 2 * MaxRequestBytes

type CommandName string

const (
	MilestoneCreate  CommandName = "work.milestone.create"
	MilestoneUpdate  CommandName = "work.milestone.update"
	MilestoneReorder CommandName = "work.milestone.reorder"
	SprintCreate     CommandName = "work.sprint.create"
	SprintUpdate     CommandName = "work.sprint.update"
	SprintReorder    CommandName = "work.sprint.reorder"
)

func (n CommandName) Validate() error {
	switch n {
	case MilestoneCreate, MilestoneUpdate, MilestoneReorder, SprintCreate, SprintUpdate, SprintReorder:
		return nil
	}
	return invalid("/command", "UNKNOWN_COMMAND")
}
func (n CommandName) IsCreate() bool { return n == MilestoneCreate || n == SprintCreate }
func (n CommandName) IsSprint() bool {
	return n == SprintCreate || n == SprintUpdate || n == SprintReorder
}
func (n CommandName) MarshalJSON() ([]byte, error) { return checked(string(n), n.Validate()) }
func (n *CommandName) UnmarshalJSON(raw []byte) error {
	var v string
	if json.Unmarshal(raw, &v) != nil || CommandName(v).Validate() != nil {
		return invalid("/command", "UNKNOWN_COMMAND")
	}
	*n = CommandName(v)
	return nil
}

type Milestone struct {
	ID          MilestoneID `json:"id"`
	ProjectID   ProjectID   `json:"project_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	ManualRank  string      `json:"manual_rank"`
	Version     f.Version   `json:"version"`
	CreatedAt   f.Instant   `json:"created_at"`
	UpdatedAt   f.Instant   `json:"updated_at"`
}

func (m Milestone) Validate() error {
	if m.ID.Validate() != nil || m.ProjectID.Validate() != nil || m.Version.Validate() != nil || ValidateRank(m.ManualRank) != nil || !validTimes(m.CreatedAt, m.UpdatedAt) {
		return invalid("", "INVALID_MILESTONE")
	}
	if err := ValidateTitle(m.Title); err != nil {
		return err
	}
	return ValidateDescription(m.Description)
}

// ActorHistory is a fact projection, never a reconstructed current Actor.
// Only the exact fields for its kind are encoded; Session is deliberately absent.
type ActorHistory struct {
	Kind        i.ActorKind
	UserID      string
	ProjectID   string
	AgentID     string
	ExecutionID string
	ServiceName i.ServiceName
	CauseRef    string
}

func (a ActorHistory) Validate() error {
	switch a.Kind {
	case i.Human:
		if validID[i.User](a.UserID) && a.ProjectID == "" && a.AgentID == "" && a.ExecutionID == "" && a.ServiceName == "" && a.CauseRef == "" {
			return nil
		}
	case i.AgentRun:
		if a.UserID == "" && validID[i.Project](a.ProjectID) && validID[i.Agent](a.AgentID) && validID[i.Execution](a.ExecutionID) && a.ServiceName == "" && a.CauseRef == "" {
			return nil
		}
	case i.Service:
		_, err := i.RegisterService(a.ServiceName)
		if err == nil && a.UserID == "" && a.AgentID == "" && a.ExecutionID == "" && i.ValidCauseRef(a.CauseRef) && (a.ProjectID == "" || validID[i.Project](a.ProjectID)) {
			return nil
		}
	}
	return invalid("", "INVALID_ACTOR_HISTORY")
}
func (a ActorHistory) InProject(project ProjectID) bool {
	return a.Validate() == nil && project.Validate() == nil && (a.Kind == i.Human || a.ProjectID == project.String())
}
func (a ActorHistory) MarshalJSON() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	v := map[string]any{"kind": a.Kind}
	switch a.Kind {
	case i.Human:
		v["user_id"] = a.UserID
	case i.AgentRun:
		v["project_id"] = a.ProjectID
		v["agent_id"] = a.AgentID
		v["execution_id"] = a.ExecutionID
	case i.Service:
		v["service_name"] = a.ServiceName
		v["cause_ref"] = a.CauseRef
		if a.ProjectID == "" {
			v["project_id"] = nil
		} else {
			v["project_id"] = a.ProjectID
		}
	}
	return json.Marshal(v)
}
func (a *ActorHistory) UnmarshalJSON(raw []byte) error {
	var v struct {
		Kind        i.ActorKind   `json:"kind"`
		UserID      string        `json:"user_id"`
		ProjectID   *string       `json:"project_id"`
		AgentID     string        `json:"agent_id"`
		ExecutionID string        `json:"execution_id"`
		ServiceName i.ServiceName `json:"service_name"`
		CauseRef    string        `json:"cause_ref"`
	}
	if strictRaw(raw) != nil || json.Unmarshal(raw, &v) != nil {
		return invalid("", "INVALID_ACTOR_HISTORY")
	}
	fields := []string{"kind"}
	nullable := []string{}
	switch v.Kind {
	case i.Human:
		fields = append(fields, "user_id")
	case i.AgentRun:
		fields = append(fields, "project_id", "agent_id", "execution_id")
	case i.Service:
		fields = append(fields, "service_name", "cause_ref", "project_id")
		nullable = append(nullable, "project_id")
	default:
		return invalid("", "INVALID_ACTOR_HISTORY")
	}
	next, err := decodeFields[struct {
		Kind        i.ActorKind   `json:"kind"`
		UserID      string        `json:"user_id"`
		ProjectID   *string       `json:"project_id"`
		AgentID     string        `json:"agent_id"`
		ExecutionID string        `json:"execution_id"`
		ServiceName i.ServiceName `json:"service_name"`
		CauseRef    string        `json:"cause_ref"`
	}](raw, fields, nil, nullable)
	if err != nil {
		return err
	}
	result := ActorHistory{Kind: next.Kind, UserID: next.UserID, AgentID: next.AgentID, ExecutionID: next.ExecutionID, ServiceName: next.ServiceName, CauseRef: next.CauseRef}
	if next.ProjectID != nil {
		result.ProjectID = *next.ProjectID
	}
	if err = result.Validate(); err == nil {
		*a = result
	}
	return err
}

type SprintState string

const (
	Planned   SprintState = "planned"
	Current   SprintState = "current"
	Completed SprintState = "completed"
)

type Sprint struct {
	ID          SprintID      `json:"id"`
	ProjectID   ProjectID     `json:"project_id"`
	MilestoneID MilestoneID   `json:"milestone_id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	ManualRank  string        `json:"manual_rank"`
	Version     f.Version     `json:"version"`
	CreatedAt   f.Instant     `json:"created_at"`
	UpdatedAt   f.Instant     `json:"updated_at"`
	StartedAt   *f.Instant    `json:"started_at"`
	StartedBy   *ActorHistory `json:"started_by"`
	CompletedAt *f.Instant    `json:"completed_at"`
	CompletedBy *ActorHistory `json:"completed_by"`
	State       SprintState   `json:"state"`
}

func (s Sprint) Validate() error {
	if s.ID.Validate() != nil || s.ProjectID.Validate() != nil || s.MilestoneID.Validate() != nil || s.Version.Validate() != nil || ValidateRank(s.ManualRank) != nil || !validTimes(s.CreatedAt, s.UpdatedAt) {
		return invalid("", "INVALID_SPRINT")
	}
	if err := ValidateTitle(s.Title); err != nil {
		return err
	}
	if err := ValidateDescription(s.Description); err != nil {
		return err
	}
	if (s.StartedAt == nil) != (s.StartedBy == nil) || (s.CompletedAt == nil) != (s.CompletedBy == nil) {
		return invalid("", "INVALID_LIFECYCLE")
	}
	if s.StartedAt != nil && (s.StartedAt.Validate() != nil || !s.StartedBy.InProject(s.ProjectID) || s.StartedAt.Time().Before(s.CreatedAt.Time()) || s.StartedAt.Time().After(s.UpdatedAt.Time())) {
		return invalid("", "INVALID_LIFECYCLE")
	}
	if s.CompletedAt != nil && (s.StartedAt == nil || s.CompletedAt.Validate() != nil || !s.CompletedBy.InProject(s.ProjectID) || s.CompletedAt.Time().Before(s.StartedAt.Time()) || s.CompletedAt.Time().After(s.UpdatedAt.Time())) {
		return invalid("", "INVALID_LIFECYCLE")
	}
	switch s.State {
	case Planned:
		if s.StartedAt == nil && s.CompletedAt == nil {
			return nil
		}
	case Current:
		if s.StartedAt != nil && s.CompletedAt == nil {
			return nil
		}
	case Completed:
		if s.StartedAt != nil && s.CompletedAt != nil {
			return nil
		}
	}
	return invalid("", "INVALID_LIFECYCLE")
}
func (s Sprint) Clone() Sprint {
	if s.StartedAt != nil {
		x := *s.StartedAt
		s.StartedAt = &x
	}
	if s.StartedBy != nil {
		x := *s.StartedBy
		s.StartedBy = &x
	}
	if s.CompletedAt != nil {
		x := *s.CompletedAt
		s.CompletedAt = &x
	}
	if s.CompletedBy != nil {
		x := *s.CompletedBy
		s.CompletedBy = &x
	}
	return s
}

type CreateMilestoneRequest struct {
	MilestoneID MilestoneID `json:"milestone_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
}
type CreateSprintRequest struct {
	SprintID    SprintID    `json:"sprint_id"`
	MilestoneID MilestoneID `json:"milestone_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
}
type UpdateFields struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
}
type ReorderMilestoneRequest struct {
	BeforeID *MilestoneID `json:"before_id,omitempty"`
}
type ReorderSprintRequest struct {
	MilestoneID MilestoneID `json:"milestone_id"`
	BeforeID    *SprintID   `json:"before_id,omitempty"`
}

func (r CreateMilestoneRequest) Validate() error {
	if r.MilestoneID.Validate() != nil {
		return invalid("/milestone_id", "INVALID_ID")
	}
	if err := ValidateTitle(r.Title); err != nil {
		return err
	}
	return ValidateDescription(r.Description)
}
func (r CreateSprintRequest) Validate() error {
	if r.SprintID.Validate() != nil || r.MilestoneID.Validate() != nil {
		return invalid("", "INVALID_ID")
	}
	if err := ValidateTitle(r.Title); err != nil {
		return err
	}
	return ValidateDescription(r.Description)
}
func (r UpdateFields) Validate() error {
	if r.Title == nil && r.Description == nil {
		return invalid("", "EMPTY_PATCH")
	}
	if r.Title != nil {
		if err := ValidateTitle(*r.Title); err != nil {
			return err
		}
	}
	if r.Description != nil {
		return ValidateDescription(*r.Description)
	}
	return nil
}
func (r ReorderMilestoneRequest) Validate() error {
	if r.BeforeID != nil && r.BeforeID.Validate() != nil {
		return invalid("/before_id", "INVALID_ID")
	}
	return nil
}
func (r ReorderSprintRequest) Validate() error {
	if r.MilestoneID.Validate() != nil || r.BeforeID != nil && r.BeforeID.Validate() != nil {
		return invalid("", "INVALID_ID")
	}
	return nil
}

type StructureMutation struct {
	Command   CommandName    `json:"command"`
	Changed   bool           `json:"changed"`
	Milestone *Milestone     `json:"milestone"`
	Sprint    *Sprint        `json:"sprint"`
	EventID   *event.EventID `json:"event_id"`
}

func (m StructureMutation) Validate() error {
	if m.Command.Validate() != nil || m.Changed != (m.EventID != nil) || m.EventID != nil && m.EventID.Validate() != nil || m.Command.IsCreate() && !m.Changed {
		return invalid("", "INVALID_RESULT")
	}
	if m.Command.IsSprint() {
		if m.Milestone != nil || m.Sprint == nil {
			return invalid("", "INVALID_RESULT")
		}
		return m.Sprint.Validate()
	}
	if m.Sprint != nil || m.Milestone == nil {
		return invalid("", "INVALID_RESULT")
	}
	return m.Milestone.Validate()
}
func (m StructureMutation) Clone() StructureMutation {
	if m.Milestone != nil {
		v := *m.Milestone
		m.Milestone = &v
	}
	if m.Sprint != nil {
		v := m.Sprint.Clone()
		m.Sprint = &v
	}
	if m.EventID != nil {
		v := *m.EventID
		m.EventID = &v
	}
	return m
}

type CommandLookupRequest struct {
	ProjectID ProjectID        `json:"project_id"`
	Command   CommandName      `json:"command"`
	Key       f.IdempotencyKey `json:"key"`
	Semantic  f.Digest         `json:"semantic"`
}

func (r CommandLookupRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.Command.Validate() != nil || r.Key.Validate() != nil || r.Semantic.Validate() != nil {
		return invalid("", "INVALID_LOOKUP")
	}
	return nil
}

type LookupState string

const (
	LookupCommitted   LookupState = "committed"
	LookupInProgress  LookupState = "in_progress"
	LookupNotObserved LookupState = "not_observed"
)

type CommandLookup struct {
	State  LookupState        `json:"state"`
	Result *StructureMutation `json:"result"`
}

func (r CommandLookup) Validate() error {
	switch r.State {
	case LookupCommitted:
		if r.Result != nil {
			return r.Result.Validate()
		}
	case LookupInProgress, LookupNotObserved:
		if r.Result == nil {
			return nil
		}
	}
	return invalid("", "INVALID_LOOKUP_RESULT")
}

type Placement struct {
	Milestone Milestone `json:"milestone"`
	Sprint    Sprint    `json:"sprint"`
}

func (p Placement) Validate() error {
	if p.Milestone.Validate() != nil || p.Sprint.Validate() != nil || p.Milestone.ID != p.Sprint.MilestoneID || p.Milestone.ProjectID != p.Sprint.ProjectID {
		return invalid("", "INVALID_PLACEMENT")
	}
	return nil
}

type Commands interface {
	CreateMilestone(context.Context, i.Actor, f.CommandMeta, ProjectID, CreateMilestoneRequest) (StructureMutation, error)
	UpdateMilestone(context.Context, i.Actor, f.CommandMeta, ProjectID, MilestoneID, UpdateFields) (StructureMutation, error)
	ReorderMilestone(context.Context, i.Actor, f.CommandMeta, ProjectID, MilestoneID, ReorderMilestoneRequest) (StructureMutation, error)
	CreateSprint(context.Context, i.Actor, f.CommandMeta, ProjectID, CreateSprintRequest) (StructureMutation, error)
	UpdateSprint(context.Context, i.Actor, f.CommandMeta, ProjectID, SprintID, UpdateFields) (StructureMutation, error)
	ReorderSprint(context.Context, i.Actor, f.CommandMeta, ProjectID, SprintID, ReorderSprintRequest) (StructureMutation, error)
	LookupCommand(context.Context, i.Actor, CommandLookupRequest) (CommandLookup, error)
}
type Reader interface {
	GetMilestone(context.Context, i.Actor, ProjectID, MilestoneID) (Milestone, error)
	ListMilestones(context.Context, i.Actor, ProjectID, f.PageRequest) (f.Page[Milestone], error)
	GetSprint(context.Context, i.Actor, ProjectID, SprintID) (Sprint, error)
	ListSprints(context.Context, i.Actor, ProjectID, MilestoneID, f.PageRequest) (f.Page[Sprint], error)
	ReadPlacementInTx(context.Context, f.Tx, i.Actor, ProjectID, SprintID) (Placement, error)
}

func ValidateActor(actor i.Actor) error {
	if actor.Validate() != nil {
		return f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	switch actor.Details().Kind {
	case i.Human:
		return nil
	case i.AgentRun:
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	default:
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
}
func Identity(project ProjectID, name CommandName, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if project.Validate() != nil || name.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid("", "INVALID_COMMAND")
	}
	return f.NewCommandIdentity("project", []string{project.String()}, string(name), key)
}
func commandDigest(actor i.Actor, meta f.CommandMeta, project ProjectID, name CommandName, target string, request any) (f.Digest, error) {
	if err := ValidateActor(actor); err != nil {
		return "", err
	}
	if meta.Validate() != nil || project.Validate() != nil || name.Validate() != nil || !validID[struct{}](target) || name.IsCreate() != (meta.ExpectedVersion == nil) {
		return "", invalid("", "INVALID_COMMAND")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	var r map[string]json.RawMessage
	if json.Unmarshal(raw, &r) != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	if name == MilestoneReorder || name == SprintReorder {
		if _, ok := r["before_id"]; !ok {
			r["before_id"] = json.RawMessage("null")
		}
	}
	raw, err = json.Marshal(struct {
		Format   string                     `json:"format"`
		Command  CommandName                `json:"command"`
		Project  ProjectID                  `json:"project_id"`
		Target   string                     `json:"target_id"`
		User     string                     `json:"actor_user_id"`
		Expected *f.Version                 `json:"expected_version"`
		Request  map[string]json.RawMessage `json:"request"`
	}{"work-structure-command-v1", name, project, target, actor.Details().UserID, meta.ExpectedVersion, r})
	if err != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return "", invalid("", "INVALID_COMMAND")
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}
func CreateMilestoneDigest(a i.Actor, m f.CommandMeta, p ProjectID, r CreateMilestoneRequest) (f.Digest, error) {
	return commandDigest(a, m, p, MilestoneCreate, r.MilestoneID.String(), r)
}
func UpdateMilestoneDigest(a i.Actor, m f.CommandMeta, p ProjectID, id MilestoneID, r UpdateFields) (f.Digest, error) {
	return commandDigest(a, m, p, MilestoneUpdate, id.String(), r)
}
func ReorderMilestoneDigest(a i.Actor, m f.CommandMeta, p ProjectID, id MilestoneID, r ReorderMilestoneRequest) (f.Digest, error) {
	if err := ValidateActor(a); err != nil {
		return "", err
	}
	if r.BeforeID != nil && *r.BeforeID == id {
		return "", invalid("/before_id", "SELF_ANCHOR")
	}
	return commandDigest(a, m, p, MilestoneReorder, id.String(), r)
}
func CreateSprintDigest(a i.Actor, m f.CommandMeta, p ProjectID, r CreateSprintRequest) (f.Digest, error) {
	return commandDigest(a, m, p, SprintCreate, r.SprintID.String(), r)
}
func UpdateSprintDigest(a i.Actor, m f.CommandMeta, p ProjectID, id SprintID, r UpdateFields) (f.Digest, error) {
	return commandDigest(a, m, p, SprintUpdate, id.String(), r)
}
func ReorderSprintDigest(a i.Actor, m f.CommandMeta, p ProjectID, id SprintID, r ReorderSprintRequest) (f.Digest, error) {
	if err := ValidateActor(a); err != nil {
		return "", err
	}
	if r.BeforeID != nil && *r.BeforeID == id {
		return "", invalid("/before_id", "SELF_ANCHOR")
	}
	return commandDigest(a, m, p, SprintReorder, id.String(), r)
}

func ValidateTitle(s string) error {
	if len(s) < 1 || len(s) > MaxTitleBytes || !utf8.ValidString(s) || utf8.RuneCountInString(s) > MaxTitleScalars {
		return invalid("/title", "INVALID_TITLE")
	}
	nonspace := false
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) {
			return invalid("/title", "INVALID_TITLE")
		}
		nonspace = nonspace || !unicode.IsSpace(r)
	}
	if !nonspace {
		return invalid("/title", "INVALID_TITLE")
	}
	return nil
}
func ValidateDescription(s string) error {
	if len(s) > MaxDescriptionBytes || !utf8.ValidString(s) {
		return invalid("/description", "INVALID_DESCRIPTION")
	}
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) && r != '\t' && r != '\n' && r != '\r' {
			return invalid("/description", "INVALID_DESCRIPTION")
		}
	}
	return nil
}
func ValidateRank(s string) error {
	if len(s) != 32 || s == strings.Repeat("0", 32) || s == strings.Repeat("f", 32) {
		return invalid("/manual_rank", "INVALID_RANK")
	}
	for _, b := range []byte(s) {
		if !(b >= '0' && b <= '9' || b >= 'a' && b <= 'f') {
			return invalid("/manual_rank", "INVALID_RANK")
		}
	}
	return nil
}
func validID[K any](s string) bool { _, err := f.ParseID[K](s); return err == nil }
func validTimes(a, b f.Instant) bool {
	return a.Validate() == nil && b.Validate() == nil && !b.Time().Before(a.Time())
}
func invalid(path, code string) error {
	v := f.NewFault(f.InvalidArgument, f.NotStarted)
	if code != "" {
		v.FieldErrors = []f.FieldError{{Path: path, Code: code}}
	}
	return v
}
func checked(v any, err error) ([]byte, error) {
	return checkedLimit(v, err, MaxRequestBytes)
}
func checkedLimit(v any, err error, limit int) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if len(raw) > limit {
		return nil, invalid("", "ENCODING_TOO_LARGE")
	}
	return raw, err
}

// encoding/json replaces isolated UTF-16 surrogates. Reject them in the raw
// boundary before decoding, along with UTF-8 damage, duplicates and extra values.
func strictRaw(raw []byte) error {
	return strictRawLimit(raw, MaxRequestBytes)
}
func strictRawLimit(raw []byte, limit int) error {
	if len(raw) > limit || !utf8.Valid(raw) {
		return invalid("", "INVALID_ENCODING")
	}
	inString := false
	for n := 0; n < len(raw); n++ {
		if raw[n] == '"' {
			inString = !inString
			continue
		}
		if !inString || raw[n] != '\\' {
			continue
		}
		n++
		if n >= len(raw) {
			return invalid("", "INVALID_ENCODING")
		}
		if raw[n] != 'u' {
			continue
		}
		if n+4 >= len(raw) {
			return invalid("", "INVALID_ENCODING")
		}
		x, err := strconv.ParseUint(string(raw[n+1:n+5]), 16, 16)
		if err != nil {
			return invalid("", "INVALID_ENCODING")
		}
		n += 4
		if x >= 0xdc00 && x <= 0xdfff {
			return invalid("", "INVALID_ENCODING")
		}
		if x >= 0xd800 && x <= 0xdbff {
			if n+6 >= len(raw) || raw[n+1] != '\\' || raw[n+2] != 'u' {
				return invalid("", "INVALID_ENCODING")
			}
			y, err := strconv.ParseUint(string(raw[n+3:n+7]), 16, 16)
			if err != nil || y < 0xdc00 || y > 0xdfff {
				return invalid("", "INVALID_ENCODING")
			}
			n += 6
		}
	}
	if _, err := cursor.CanonicalJSON(raw); err != nil {
		return invalid("", "INVALID_ENCODING")
	}
	return nil
}
func decodeFields[T any](raw []byte, required, optional, nullable []string) (T, error) {
	return decodeFieldsLimit[T](raw, required, optional, nullable, MaxRequestBytes)
}

// A direct UnmarshalJSON call bounds its complete raw argument. The standard
// json.Unmarshal entry strips outer whitespace before invoking that method;
// any future transport must therefore limit the complete request body first.
func decodeFieldsLimit[T any](raw []byte, required, optional, nullable []string, limit int) (T, error) {
	var zero T
	if err := strictRawLimit(raw, limit); err != nil {
		return zero, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return zero, invalid("", "INVALID_ENCODING")
	}
	for _, key := range required {
		if _, ok := fields[key]; !ok {
			return zero, invalid("/"+key, "REQUIRED")
		}
	}
	for key, v := range fields {
		if !slices.Contains(required, key) && !slices.Contains(optional, key) {
			return zero, invalid("", "UNKNOWN_FIELD")
		}
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) && !slices.Contains(nullable, key) {
			return zero, invalid("/"+key, "NULL_NOT_ALLOWED")
		}
	}
	var value T
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&value) != nil {
		return zero, invalid("", "INVALID_ENCODING")
	}
	return value, nil
}
func safeFormat(w fmt.State, _ rune) { _, _ = io.WriteString(w, "work_structure") }
func safeLog() slog.Value            { return slog.StringValue("work_structure") }

func (v Milestone) MarshalJSON() ([]byte, error) {
	type wire Milestone
	return checked(wire(v), v.Validate())
}
func (v *Milestone) UnmarshalJSON(raw []byte) error {
	type wire Milestone
	w, err := decodeFields[wire](raw, []string{"id", "project_id", "title", "description", "manual_rank", "version", "created_at", "updated_at"}, nil, nil)
	if err != nil {
		return err
	}
	next := Milestone(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v Sprint) MarshalJSON() ([]byte, error) {
	type wire Sprint
	return checked(wire(v), v.Validate())
}
func (v *Sprint) UnmarshalJSON(raw []byte) error {
	type wire Sprint
	w, err := decodeFields[wire](raw, []string{"id", "project_id", "milestone_id", "title", "description", "manual_rank", "version", "created_at", "updated_at", "started_at", "started_by", "completed_at", "completed_by", "state"}, nil, []string{"started_at", "started_by", "completed_at", "completed_by"})
	if err != nil {
		return err
	}
	next := Sprint(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v CreateMilestoneRequest) MarshalJSON() ([]byte, error) {
	type wire CreateMilestoneRequest
	return checked(wire(v), v.Validate())
}
func (v *CreateMilestoneRequest) UnmarshalJSON(raw []byte) error {
	type wire CreateMilestoneRequest
	w, err := decodeFields[wire](raw, []string{"milestone_id", "title"}, []string{"description"}, nil)
	if err != nil {
		return err
	}
	next := CreateMilestoneRequest(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v CreateSprintRequest) MarshalJSON() ([]byte, error) {
	type wire CreateSprintRequest
	return checked(wire(v), v.Validate())
}
func (v *CreateSprintRequest) UnmarshalJSON(raw []byte) error {
	type wire CreateSprintRequest
	w, err := decodeFields[wire](raw, []string{"sprint_id", "milestone_id", "title"}, []string{"description"}, nil)
	if err != nil {
		return err
	}
	next := CreateSprintRequest(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v UpdateFields) MarshalJSON() ([]byte, error) {
	type wire UpdateFields
	return checked(wire(v), v.Validate())
}
func (v *UpdateFields) UnmarshalJSON(raw []byte) error {
	type wire UpdateFields
	w, err := decodeFields[wire](raw, nil, []string{"title", "description"}, nil)
	if err != nil {
		return err
	}
	next := UpdateFields(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v ReorderMilestoneRequest) MarshalJSON() ([]byte, error) {
	type wire ReorderMilestoneRequest
	return checked(wire(v), v.Validate())
}
func (v *ReorderMilestoneRequest) UnmarshalJSON(raw []byte) error {
	type wire ReorderMilestoneRequest
	w, err := decodeFields[wire](raw, nil, []string{"before_id"}, nil)
	if err != nil {
		return err
	}
	next := ReorderMilestoneRequest(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v ReorderSprintRequest) MarshalJSON() ([]byte, error) {
	type wire ReorderSprintRequest
	return checked(wire(v), v.Validate())
}
func (v *ReorderSprintRequest) UnmarshalJSON(raw []byte) error {
	type wire ReorderSprintRequest
	w, err := decodeFields[wire](raw, []string{"milestone_id"}, []string{"before_id"}, nil)
	if err != nil {
		return err
	}
	next := ReorderSprintRequest(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v StructureMutation) MarshalJSON() ([]byte, error) {
	type wire StructureMutation
	return checked(wire(v), v.Validate())
}
func (v *StructureMutation) UnmarshalJSON(raw []byte) error {
	type wire StructureMutation
	w, err := decodeFields[wire](raw, []string{"command", "changed", "milestone", "sprint", "event_id"}, nil, []string{"milestone", "sprint", "event_id"})
	if err != nil {
		return err
	}
	next := StructureMutation(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v CommandLookupRequest) MarshalJSON() ([]byte, error) {
	type wire CommandLookupRequest
	return checked(wire(v), v.Validate())
}
func (v *CommandLookupRequest) UnmarshalJSON(raw []byte) error {
	type wire CommandLookupRequest
	w, err := decodeFields[wire](raw, []string{"project_id", "command", "key", "semantic"}, nil, nil)
	if err != nil {
		return err
	}
	next := CommandLookupRequest(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v CommandLookup) MarshalJSON() ([]byte, error) {
	type wire CommandLookup
	return checked(wire(v), v.Validate())
}
func (v *CommandLookup) UnmarshalJSON(raw []byte) error {
	type wire CommandLookup
	w, err := decodeFields[wire](raw, []string{"state", "result"}, nil, []string{"result"})
	if err != nil {
		return err
	}
	next := CommandLookup(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}

func (v Placement) MarshalJSON() ([]byte, error) {
	type wire Placement
	return checkedLimit(wire(v), v.Validate(), maxPlacementBytes)
}
func (v *Placement) UnmarshalJSON(raw []byte) error {
	type wire Placement
	w, err := decodeFieldsLimit[wire](raw, []string{"milestone", "sprint"}, nil, nil, maxPlacementBytes)
	if err != nil {
		return err
	}
	next := Placement(w)
	if err = next.Validate(); err == nil {
		*v = next
	}
	return err
}
func (v Milestone) Format(w fmt.State, r rune)               { safeFormat(w, r) }
func (v Milestone) LogValue() slog.Value                     { return safeLog() }
func (v Sprint) Format(w fmt.State, r rune)                  { safeFormat(w, r) }
func (v Sprint) LogValue() slog.Value                        { return safeLog() }
func (v CreateMilestoneRequest) Format(w fmt.State, r rune)  { safeFormat(w, r) }
func (v CreateMilestoneRequest) LogValue() slog.Value        { return safeLog() }
func (v CreateSprintRequest) Format(w fmt.State, r rune)     { safeFormat(w, r) }
func (v CreateSprintRequest) LogValue() slog.Value           { return safeLog() }
func (v UpdateFields) Format(w fmt.State, r rune)            { safeFormat(w, r) }
func (v UpdateFields) LogValue() slog.Value                  { return safeLog() }
func (v ReorderMilestoneRequest) Format(w fmt.State, r rune) { safeFormat(w, r) }
func (v ReorderMilestoneRequest) LogValue() slog.Value       { return safeLog() }
func (v ReorderSprintRequest) Format(w fmt.State, r rune)    { safeFormat(w, r) }
func (v ReorderSprintRequest) LogValue() slog.Value          { return safeLog() }
func (v StructureMutation) Format(w fmt.State, r rune)       { safeFormat(w, r) }
func (v StructureMutation) LogValue() slog.Value             { return safeLog() }
func (v CommandLookupRequest) Format(w fmt.State, r rune)    { safeFormat(w, r) }
func (v CommandLookupRequest) LogValue() slog.Value          { return safeLog() }
func (v CommandLookup) Format(w fmt.State, r rune)           { safeFormat(w, r) }
func (v CommandLookup) LogValue() slog.Value                 { return safeLog() }
func (v Placement) Format(w fmt.State, r rune)               { safeFormat(w, r) }
func (v Placement) LogValue() slog.Value                     { return safeLog() }
func (v ActorHistory) Format(w fmt.State, r rune)            { safeFormat(w, r) }
func (v ActorHistory) LogValue() slog.Value                  { return safeLog() }
