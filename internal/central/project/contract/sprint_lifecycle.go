package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// SprintStartChange is an exact planned postimage, not permission to update a
// Project. The Work owner must prove its original write in this same Tx.
type SprintStartChange struct {
	Command       f.CommandIdentity
	CommandID     string
	PlanRevision  f.Version
	SprintID      SprintID
	SprintVersion f.Version
	StartedAt     f.Instant
	Before        ProjectRef
	After         ProjectRef
}

func (v SprintStartChange) Validate() error {
	if v.Command.Validate() != nil || v.Command.Namespace() != "project" || len(v.Command.OwnerIDs()) != 1 || v.Command.OwnerIDs()[0] != v.Before.ID.String() || v.Command.Command() != "work.sprint.start" || v.PlanRevision.Validate() != nil || v.SprintID.Validate() != nil || v.SprintVersion <= 1 || v.StartedAt.Validate() != nil || v.Before.Validate() != nil || v.After.Validate() != nil || v.Before.CurrentSprintID != nil || v.Before.Lifecycle != Active {
		return invalid("", "INVALID_SPRINT_START")
	}
	if _, err := f.ParseID[struct{}](v.CommandID); err != nil {
		return invalid("", "INVALID_SPRINT_START")
	}
	if v.After.ID != v.Before.ID || v.After.Version <= v.Before.Version || v.After.Version-v.Before.Version != 1 || v.After.CurrentSprintID == nil || *v.After.CurrentSprintID != v.SprintID || !v.After.UpdatedAt.Time().Equal(v.StartedAt.Time()) || v.StartedAt.Time().Before(v.Before.UpdatedAt.Time()) {
		return invalid("", "INVALID_SPRINT_START")
	}
	// Compare all other Project fields; callers cannot smuggle a settings update.
	a, b := v.After.Clone(), v.Before.Clone()
	a.Version = b.Version
	a.CurrentSprintID = nil
	a.UpdatedAt = b.UpdatedAt
	if a.ID != b.ID || a.OwnerUserID != b.OwnerUserID || a.Name != b.Name || a.NormalizedName != b.NormalizedName || a.Description != b.Description || a.Lifecycle != b.Lifecycle || !a.CreatedAt.Time().Equal(b.CreatedAt.Time()) || a.ArchivedAt != nil || b.ArchivedAt != nil {
		return invalid("", "INVALID_SPRINT_START")
	}
	return nil
}
func (v SprintStartChange) Clone() SprintStartChange {
	v.Before = v.Before.Clone()
	v.After = v.After.Clone()
	return v
}
func (v SprintStartChange) RequiredLocks(user i.UserID) ([]f.LockRequest, error) {
	if v.Validate() != nil || user.Validate() != nil {
		return nil, invalid("", "INVALID_SPRINT_START")
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

type SprintStartFacts interface {
	CheckSprintStartAppliedInTx(context.Context, f.Tx, i.Actor, SprintStartChange) error
}
type SprintLifecycleAuthority interface {
	ApplySprintStartInTx(context.Context, f.Tx, i.Actor, SprintStartChange) (ProjectRef, error)
}

func (SprintStartChange) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_sprint_start")
}
func (SprintStartChange) LogValue() slog.Value { return slog.StringValue("project_sprint_start") }

// Clone detaches both optional pointer fields while preserving the complete
// Project value. Use the same copy semantics as ProjectAccess.Project.
func (p ProjectRef) Clone() ProjectRef { return cloneProject(p) }
