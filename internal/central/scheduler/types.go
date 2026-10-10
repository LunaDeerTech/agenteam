package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type DispatchIdentity struct{}
type DispatchID = f.ID[DispatchIdentity]

type Status string

const (
	Pending  Status = "pending"
	Launched Status = "launched"
	Failed   Status = "failed"
	Skipped  Status = "skipped"
)

func (s Status) Valid() bool { return s == Pending || s == Launched || s == Failed || s == Skipped }

type LaunchOutcome string

const (
	NotSent         LaunchOutcome = "not_sent"
	KnownNotCreated LaunchOutcome = "known_not_created"
	Unknown         LaunchOutcome = "unknown"
	Created         LaunchOutcome = "created"
)

func (s LaunchOutcome) Valid() bool {
	return s == NotSent || s == KnownNotCreated || s == Unknown || s == Created
}

// ClaimGuard records Work's logical position, never a reusable numeric rank.
// Only the Work writer's same-transaction proof permits persisting this value.
type ClaimGuard struct {
	TaskID                string    `json:"task_id"`
	ClaimedVersion        f.Version `json:"claimed_version"`
	SourceState           string    `json:"source_state"`
	SourceAssigneeID      i.AgentID `json:"source_assignee_id"`
	SourcePriority        string    `json:"source_priority"`
	SourceSprintID        string    `json:"source_sprint_id"`
	SourceOrderGeneration f.Version `json:"source_order_generation"`
	PredecessorID         string    `json:"predecessor_id,omitempty"`
	SuccessorID           string    `json:"successor_id,omitempty"`
}

func validID(s string) bool { _, err := f.ParseID[struct{}](s); return err == nil }
func validPriority(s string) bool {
	return s == "low" || s == "medium" || s == "high" || s == "critical"
}
func validState(s string) bool {
	return s == "backlog" || s == "todo" || s == "in_progress" || s == "in_review" || s == "blocked" || s == "done" || s == "cancelled"
}
func (g ClaimGuard) valid() bool {
	return validID(g.TaskID) && g.ClaimedVersion > 1 && g.SourceState == "todo" &&
		g.SourceAssigneeID.Validate() == nil && validPriority(g.SourcePriority) && validID(g.SourceSprintID) &&
		g.SourceOrderGeneration.Validate() == nil &&
		(g.PredecessorID == "" || validID(g.PredecessorID) && g.PredecessorID != g.TaskID) &&
		(g.SuccessorID == "" || validID(g.SuccessorID) && g.SuccessorID != g.TaskID) &&
		(g.PredecessorID == "" || g.PredecessorID != g.SuccessorID)
}

// Dispatch is an immutable handle. Its original input is available only via
// the explicit LaunchRequest projection; default nested logging is constant.
type Dispatch struct{ data func() dispatchRecord }
type DispatchSummary struct {
	ID               DispatchID
	ProjectID        i.ProjectID
	SprintID, TaskID string
	AgentID          i.AgentID
	Status           Status
	LaunchOutcome    LaunchOutcome
	Version          f.Version
	ExecutionID      *i.ExecutionID
	AttemptCount     int64
	SkipReason       string
	SkippedAt        *f.Instant
}

func (d Dispatch) Summary() DispatchSummary {
	if d.data == nil {
		return DispatchSummary{}
	}
	r := d.data()
	var execution *i.ExecutionID
	if r.execution != nil {
		v := *r.execution
		execution = &v
	}
	var skipped *f.Instant
	if r.skippedAt != nil {
		v := *r.skippedAt
		skipped = &v
	}
	return DispatchSummary{r.id, r.project, r.sprint, r.task, r.agent, r.status, r.outcome, r.version, execution, r.attempts, r.skipReason, skipped}
}
func (d Dispatch) LaunchRequest() (ec.LaunchRequest, error) {
	if d.data == nil {
		return ec.LaunchRequest{}, invalid()
	}
	return d.data().launch.Clone(), nil
}
func snapshot(r *dispatchRecord) Dispatch {
	if r == nil {
		return Dispatch{}
	}
	v := *r
	v.launch = v.launch.Clone()
	if v.guard != nil {
		g := *v.guard
		v.guard = &g
	}
	if v.execution != nil {
		e := *v.execution
		v.execution = &e
	}
	if v.skippedAt != nil {
		t := *v.skippedAt
		v.skippedAt = &t
	}
	return Dispatch{data: func() dispatchRecord { return v }}
}
func (Dispatch) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "scheduler_dispatch") }
func (Dispatch) LogValue() slog.Value         { return slog.StringValue("scheduler_dispatch") }
func (Dispatch) MarshalJSON() ([]byte, error) { return []byte(`"scheduler_dispatch"`), nil }

type dispatchRecord struct {
	id                   DispatchID
	project              i.ProjectID
	sprint, task         string
	agent                i.AgentID
	launch               ec.LaunchRequest
	digest               f.Digest
	status               Status
	outcome              LaunchOutcome
	version              f.Version
	guard                *ClaimGuard
	execution            *i.ExecutionID
	attempts             int64
	nextRetry            *f.Instant
	createdAt, updatedAt f.Instant
	busyAttempt          int64
	skipReason           string
	skippedAt            *f.Instant
}

func launchKey(id DispatchID) f.IdempotencyKey {
	return f.IdempotencyKey("scheduler_dispatch:" + id.String())
}
func claimCommand(project i.ProjectID, id DispatchID) (f.CommandIdentity, error) {
	return f.NewCommandIdentity("scheduler", []string{project.String()}, "claim", f.IdempotencyKey("scheduler_claim:"+id.String()))
}

// Persist the original validated JSON bytes, not PostgreSQL's jsonb rendering.
// Physical RequestID/key are stored separately because LaunchRequest omits Meta.
func encodeLaunch(r ec.LaunchRequest) ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, invalid()
	}
	b, err := json.Marshal(r)
	if err != nil || len(b) > 262144 {
		return nil, invalid()
	}
	return b, nil
}
func decodeExact(raw []byte, target any, limit int) error {
	if len(raw) == 0 || len(raw) > limit {
		return unavailable(nil)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return unavailable(nil)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return unavailable(nil)
	}
	b, err := json.Marshal(target)
	if err != nil || !bytes.Equal(raw, b) {
		return unavailable(nil)
	}
	return nil
}
