package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// SprintCompleteChange is an exact planned postimage, not permission to update a
// Project. The Work owner must prove its original completion write in this same Tx.
type SprintCompleteChange struct {
	Command       f.CommandIdentity
	CommandID     string
	PlanRevision  f.Version
	SprintID      SprintID
	SprintVersion f.Version
	CompletedAt   f.Instant
	Before        ProjectRef
	After         ProjectRef
}

func (v SprintCompleteChange) Validate() error {
	if v.Command.Validate() != nil || v.Command.Namespace() != "project" || len(v.Command.OwnerIDs()) != 1 || v.Command.OwnerIDs()[0] != v.Before.ID.String() || v.Command.Command() != "work.sprint.complete" || v.PlanRevision.Validate() != nil || v.SprintID.Validate() != nil || v.SprintVersion <= 1 || v.CompletedAt.Validate() != nil || v.Before.Validate() != nil || v.After.Validate() != nil || (v.Before.CurrentSprintID == nil || *v.Before.CurrentSprintID != v.SprintID) || v.Before.Lifecycle != Active {
		return invalid("", "INVALID_SPRINT_COMPLETE")
	}
	if _, err := f.ParseID[struct{}](v.CommandID); err != nil {
		return invalid("", "INVALID_SPRINT_COMPLETE")
	}
	if v.After.ID != v.Before.ID || v.After.Version <= v.Before.Version || v.After.Version-v.Before.Version != 1 || v.After.CurrentSprintID != nil || !v.After.UpdatedAt.Time().Equal(v.CompletedAt.Time()) || v.CompletedAt.Time().Before(v.Before.UpdatedAt.Time()) {
		return invalid("", "INVALID_SPRINT_COMPLETE")
	}
	// Compare all other Project fields; callers cannot smuggle a settings update.
	a, b := v.After.Clone(), v.Before.Clone()
	a.Version = b.Version
	a.CurrentSprintID = b.CurrentSprintID
	a.UpdatedAt = b.UpdatedAt
	if a.ID != b.ID || a.OwnerUserID != b.OwnerUserID || a.Name != b.Name || a.NormalizedName != b.NormalizedName || a.Description != b.Description || a.Lifecycle != b.Lifecycle || !a.CreatedAt.Time().Equal(b.CreatedAt.Time()) || a.ArchivedAt != nil || b.ArchivedAt != nil {
		return invalid("", "INVALID_SPRINT_COMPLETE")
	}
	return nil
}
func (v SprintCompleteChange) Clone() SprintCompleteChange {
	v.Before = v.Before.Clone()
	v.After = v.After.Clone()
	return v
}
func (v SprintCompleteChange) RequiredLocks(user i.UserID) ([]f.LockRequest, error) {
	if v.Validate() != nil || user.Validate() != nil {
		return nil, invalid("", "INVALID_SPRINT_COMPLETE")
	}
	command, e := f.CommandLock(v.Command)
	if e != nil {
		return nil, e
	}
	u, e := f.UserLock(user.String())
	if e != nil {
		return nil, e
	}
	p, e := f.ProjectLock(v.Before.ID.String())
	if e != nil {
		return nil, e
	}
	schedule, e := f.ProjectScheduleLock(v.Before.ID.String())
	if e != nil {
		return nil, e
	}
	sprint, e := f.AggregateLock(f.SprintAggregate, v.SprintID.String())
	if e != nil {
		return nil, e
	}
	return []f.LockRequest{{Key: command, Mode: f.Exclusive}, {Key: u, Mode: f.Exclusive}, {Key: p, Mode: f.Exclusive}, {Key: schedule, Mode: f.Exclusive}, {Key: sprint, Mode: f.Exclusive}}, nil
}

type SprintCompleteFacts interface {
	CheckSprintCompleteAppliedInTx(context.Context, f.Tx, i.Actor, SprintCompleteChange) error
}
type SprintCompletionAuthority interface {
	ApplySprintCompleteInTx(context.Context, f.Tx, i.Actor, SprintCompleteChange) (ProjectRef, error)
}

func (SprintCompleteChange) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_sprint_complete")
}
func (SprintCompleteChange) LogValue() slog.Value { return slog.StringValue("project_sprint_complete") }
