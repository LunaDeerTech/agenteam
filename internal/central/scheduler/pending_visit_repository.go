package scheduler

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const pendingVisitSQL = `SELECT ` + dispatchColumns + ` FROM agenteam_scheduler.dispatches
 WHERE project_id=$1 AND status='pending' AND id COLLATE "C">$2::text COLLATE "C"
 ORDER BY id COLLATE "C" LIMIT 1`

func (s *PendingVisitor) readNext(ctx context.Context, p i.ProjectID, after string) (*dispatchRecord, pc.SchedulerProject, error) {
	command, _ := f.NewCommandIdentity("scheduler", []string{p.String()}, "visit_pending", "pending_visit")
	key, _ := f.CommandLock(command)
	locks := append(pendingLocks(p), f.LockRequest{Key: key, Mode: f.Exclusive})
	cause, _ := f.NewCommandsCause(command)
	var found *dispatchRecord
	var current pc.SchedulerProject
	result := s.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.authority.inTx(ctx, tx, p)
		if err != nil {
			return err
		}
		// No Current Sprint, Work status, retry date or group filter can hide
		// an old pending identity. QueryRow.Scan owns the bounded row tail.
		found, err = scanDispatch(x.QueryRow(ctx, pendingVisitSQL, p.String(), after))
		if err != nil {
			return err
		}
		if found != nil && (found.project != p || found.status != Pending || found.id.String() <= after) {
			return unavailable(nil)
		}
		current, err = s.busy.deps.Projects.RequireSchedulerProjectInTx(ctx, tx, p)
		if err != nil {
			return portError(err)
		}
		if current.Project.ID != p || current.Config.Validate() != nil {
			return unavailable(nil)
		}
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return nil, pc.SchedulerProject{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, pc.SchedulerProject{}, err
	}
	return found, current.Clone(), nil
}

// A visit can only add a current pause/Sprint restriction. It cannot mint a
// launch intent: markSending and associate keep all their original authority,
// canonical identity, physical commit and held-lock checks. Standalone
// handoffs retain their existing behavior when no private visit is present.
func requirePendingVisitInTx(ctx context.Context, tx f.Tx, h *LaunchHandoff, r *dispatchRecord, sending bool) error {
	value := ctx.Value(pendingVisitContextKey{})
	if value == nil {
		return nil
	}
	call, ok := value.(*pendingVisitCall)
	if !ok || call == nil || call.owner == nil || r == nil {
		return fault(f.Forbidden)
	}
	s := call.owner
	s.mu.Lock()
	live := s.calls[call.project] == call && call.project == r.project && call.id == r.id && s.handoff == h && s.authority == h.authority
	s.mu.Unlock()
	if !live {
		return fault(f.Forbidden)
	}
	if _, err := s.authority.inTx(ctx, tx, r.project); err != nil {
		return err
	}
	current, err := s.busy.deps.Projects.RequireSchedulerProjectInTx(ctx, tx, r.project)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return portError(err)
	}
	if current.Project.ID != r.project || current.Config.Validate() != nil {
		return unavailable(nil)
	}
	if !current.Config.Enabled || sending && (current.Project.CurrentSprintID == nil || current.Project.CurrentSprintID.String() != r.sprint) {
		return fault(f.InvalidState)
	}
	s.mu.Lock()
	live = s.calls[call.project] == call && call.id == r.id
	s.mu.Unlock()
	if !live {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}
