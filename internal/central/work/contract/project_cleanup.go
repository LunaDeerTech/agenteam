package contract

import (
	"bytes"
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ProjectCleanupAuthority consumes the original fixed Work cleanup round.
// Every mutation and retained-Unknown read must use the same Store's live Tx,
// Project EX and ProjectSchedule EX. Public values and an empty table are not
// authority. A Stop loan cannot authorize cleanup.
type ProjectCleanupAuthority interface {
	RequireProjectCleanupInTx(context.Context, f.Tx, i.Actor, pc.LifecycleCause, pc.ScopeRef) error
}

// ProjectCleanupProgress is a bounded diagnostic checkpoint, not an issuer or
// evidence that the original owner returned. The provider verifies its actual
// process/instance and retained calls before consuming these values.
type ProjectCleanupProgress struct {
	ProjectID      i.ProjectID    `json:"project_id"`
	OperationID    pc.OperationID `json:"operation_id"`
	ProjectVersion f.Version      `json:"project_version"`
	ProcessID      oc.ProcessID   `json:"process_id"`
	InstanceID     string         `json:"instance_id"`
	Stage          string         `json:"stage"`
}

func (p ProjectCleanupProgress) Validate() error {
	if p.ProjectID.Validate() != nil || p.OperationID.Validate() != nil || p.ProjectVersion.Validate() != nil || p.ProcessID.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotCommitted)
	}
	if _, err := f.ParseID[struct{}](p.InstanceID); err != nil {
		return f.NewFault(f.InvalidArgument, f.NotCommitted)
	}
	switch p.Stage {
	case "task_events", "task_blockers", "task_launch_failures", "task_busy_compensations", "task_scheduler_claims", "task_scheduler_relaunches", "task_blocker_commands", "task_transition_commands", "task_commands", "sprint_start_commands", "sprint_complete_commands", "structure_commands", "tasks", "task_order_groups", "task_query_generations", "sprints", "sprint_order_groups", "milestones", "milestone_order_groups", "project_calls", "completed":
		return nil
	}
	return f.NewFault(f.InvalidArgument, f.NotCommitted)
}
func NewProjectCleanupCheckpoint(p ProjectCleanupProgress, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.CleanupCheckpoint, error) {
	if p.Validate() != nil || pc.RequireProjectScope(scope) != nil || cause.Validate() != nil || cause.Action != pc.Delete || p.ProjectID != scope.ProjectID || p.OperationID != cause.OperationID || p.ProjectVersion != cause.ProjectVersion {
		return pc.CleanupCheckpoint{}, f.NewFault(f.InvalidArgument, f.NotCommitted)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return pc.CleanupCheckpoint{}, err
	}
	return pc.NewCleanupCheckpoint(pc.WorkParticipant, cause, scope, 1, raw)
}
func DecodeProjectCleanupCheckpoint(cp *pc.CleanupCheckpoint, cause pc.LifecycleCause, scope pc.ScopeRef) (ProjectCleanupProgress, error) {
	var p ProjectCleanupProgress
	if cp == nil {
		return p, nil
	}
	if cp.Schema() != 1 || !cp.Matches(pc.WorkParticipant, cause, scope) {
		return p, f.NewFault(f.InvalidArgument, f.NotCommitted)
	}
	if err := json.Unmarshal(cp.Bytes(), &p); err != nil {
		return ProjectCleanupProgress{}, f.NewFault(f.InvalidArgument, f.NotCommitted)
	}
	canonical, err := NewProjectCleanupCheckpoint(p, cause, scope)
	if err != nil || !bytes.Equal(cp.Bytes(), canonical.Bytes()) {
		return ProjectCleanupProgress{}, f.NewFault(f.InvalidArgument, f.NotCommitted)
	}
	return p, nil
}
