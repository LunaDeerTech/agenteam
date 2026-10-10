// Package contract defines Execution-owned identities and launch inputs. A
// valid input is not an active Execution, a Snapshot, or a Tool permission.
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type Snapshot struct{}
type Round struct{}
type InputBinding struct{}
type Payload struct{}
type SnapshotID = f.ID[Snapshot]
type RoundID = f.ID[Round]
type InputBindingID = f.ID[InputBinding]
type PayloadID = f.ID[Payload]

type Status string

const (
	Created   Status = "created"
	Preparing Status = "preparing"
	Running   Status = "running"
	Waiting   Status = "waiting"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
)

func (s Status) Valid() bool {
	return s == Created || s == Preparing || s == Running || s == Waiting || s == Succeeded || s == Failed || s == Cancelled
}
func (s Status) Terminal() bool { return s == Succeeded || s == Failed || s == Cancelled }

// Trigger names only the source identity. The actual source provider must
// validate all relationships, launch intent and current permission in the
// caller's transaction; these IDs do not prove Task/Meeting facts.
type Trigger struct {
	Kind           string `json:"kind"`
	TaskID         string `json:"task_id,omitempty"`
	MeetingID      string `json:"meeting_id,omitempty"`
	TurnID         string `json:"turn_id,omitempty"`
	ContributionID string `json:"contribution_id,omitempty"`
	ParticipantID  string `json:"participant_id,omitempty"`
}

func uuid(v string) bool { _, err := f.ParseID[struct{}](v); return err == nil }
func invalid() error     { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func (v Trigger) Validate(purpose string) error {
	switch v.Kind {
	case "task":
		if !uuid(v.TaskID) || v.MeetingID != "" || v.TurnID != "" || v.ContributionID != "" || v.ParticipantID != "" || purpose != "task/work" && purpose != "task/review" {
			return invalid()
		}
	case "meeting":
		if v.TaskID != "" || !uuid(v.MeetingID) || !uuid(v.TurnID) || !uuid(v.ContributionID) || !uuid(v.ParticipantID) || purpose != "meeting/response" {
			return invalid()
		}
	default:
		return f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	return nil
}

// Policy preserves the original source's resource constraints. The source's
// policy validator must interpret them before launch/capture; canonical JSON
// alone never establishes permission. This module cannot silently drop an
// unknown constraint or replace it with an empty list.
type Policy struct {
	SchemaVersion              f.Version         `json:"schema_version"`
	DeniedToolIDs              []id.ToolID       `json:"denied_tool_ids"`
	AllowedResourceConstraints []json.RawMessage `json:"allowed_resource_constraints"`
}

func (p Policy) Validate() error {
	if p.SchemaVersion != 1 || p.DeniedToolIDs == nil || len(p.DeniedToolIDs) > 128 || p.AllowedResourceConstraints == nil || len(p.AllowedResourceConstraints) > 128 {
		return invalid()
	}
	for n, tool := range p.DeniedToolIDs {
		if tool.Validate() != nil || n > 0 && p.DeniedToolIDs[n-1].String() >= tool.String() {
			return invalid()
		}
	}
	total := 0
	for _, constraint := range p.AllowedResourceConstraints {
		total += len(constraint)
		if len(constraint) == 0 || len(constraint) > 8192 || total > 65536 {
			return invalid()
		}
		if _, err := cursor.CanonicalJSON(constraint); err != nil {
			return invalid()
		}
	}
	return nil
}
func (p Policy) Clone() Policy {
	p.DeniedToolIDs = slices.Clone(p.DeniedToolIDs)
	p.AllowedResourceConstraints = slices.Clone(p.AllowedResourceConstraints)
	for n := range p.AllowedResourceConstraints {
		p.AllowedResourceConstraints[n] = slices.Clone(p.AllowedResourceConstraints[n])
	}
	return p
}

type Lineage struct {
	DispatchID             string          `json:"dispatch_id,omitempty"`
	ContributionGeneration *f.Version      `json:"contribution_generation,omitempty"`
	ContributionAttempt    *f.Version      `json:"contribution_attempt,omitempty"`
	RetryOf                *id.ExecutionID `json:"retry_of_execution_id,omitempty"`
	RegenerateOf           *id.ExecutionID `json:"regenerate_of_execution_id,omitempty"`
}

type LaunchRequest struct {
	ProjectID id.ProjectID  `json:"project_id"`
	AgentID   id.AgentID    `json:"agent_id"`
	Trigger   Trigger       `json:"trigger"`
	Purpose   string        `json:"purpose"`
	Policy    Policy        `json:"execution_policy"`
	Lineage   Lineage       `json:"lineage"`
	Meta      f.CommandMeta `json:"-"`
}

func (r LaunchRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.Meta.Validate() != nil || r.Meta.ExpectedVersion != nil || r.Policy.Validate() != nil {
		return invalid()
	}
	if err := r.Trigger.Validate(r.Purpose); err != nil {
		return err
	}
	l := r.Lineage
	if l.DispatchID != "" && (!uuid(l.DispatchID) || r.Trigger.Kind != "task") || (l.ContributionGeneration == nil) != (l.ContributionAttempt == nil) {
		return invalid()
	}
	if l.ContributionGeneration != nil && (r.Trigger.Kind != "meeting" || l.ContributionGeneration.Validate() != nil || l.ContributionAttempt.Validate() != nil) {
		return invalid()
	}
	if l.RetryOf != nil && l.RetryOf.Validate() != nil || l.RegenerateOf != nil && l.RegenerateOf.Validate() != nil || l.RetryOf != nil && l.RegenerateOf != nil {
		return invalid()
	}
	return nil
}
func (r LaunchRequest) Clone() LaunchRequest {
	r.Policy = r.Policy.Clone()
	if r.Lineage.ContributionGeneration != nil {
		v := *r.Lineage.ContributionGeneration
		r.Lineage.ContributionGeneration = &v
	}
	if r.Lineage.ContributionAttempt != nil {
		v := *r.Lineage.ContributionAttempt
		r.Lineage.ContributionAttempt = &v
	}
	if r.Lineage.RetryOf != nil {
		v := *r.Lineage.RetryOf
		r.Lineage.RetryOf = &v
	}
	if r.Lineage.RegenerateOf != nil {
		v := *r.Lineage.RegenerateOf
		r.Lineage.RegenerateOf = &v
	}
	return r
}
func (r LaunchRequest) Command() (f.CommandIdentity, error) {
	if err := r.Validate(); err != nil {
		return f.CommandIdentity{}, err
	}
	return f.NewCommandIdentity("execution", []string{r.ProjectID.String(), r.AgentID.String()}, "launch", r.Meta.IdempotencyKey)
}
func (r LaunchRequest) Digest() (f.Digest, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", invalid()
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return "", invalid()
	}
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

type Summary struct {
	ID                id.ExecutionID
	ProjectID         id.ProjectID
	AgentID           id.AgentID
	Trigger           Trigger
	Purpose           string
	Status            Status
	Version           f.Version
	CancelRequestedAt *f.Instant
	SnapshotID        *SnapshotID
	CreatedAt         f.Instant
	StartedAt         *f.Instant
	CompletedAt       *f.Instant
}

func (s Summary) Clone() Summary {
	if s.CancelRequestedAt != nil {
		v := *s.CancelRequestedAt
		s.CancelRequestedAt = &v
	}
	if s.SnapshotID != nil {
		v := *s.SnapshotID
		s.SnapshotID = &v
	}
	if s.StartedAt != nil {
		v := *s.StartedAt
		s.StartedAt = &v
	}
	if s.CompletedAt != nil {
		v := *s.CompletedAt
		s.CompletedAt = &v
	}
	return s
}

type LaunchResult struct {
	Execution Summary
	Replayed  bool
}

func (LaunchRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_launch_request")
}
func (LaunchRequest) LogValue() slog.Value { return slog.StringValue("execution_launch_request") }
func (Policy) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "execution_policy") }
func (Policy) LogValue() slog.Value        { return slog.StringValue("execution_policy") }
