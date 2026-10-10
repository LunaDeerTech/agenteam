package project

import (
	"context"
	"errors"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type lifecycleStopClaim struct {
	project   c.ProjectID
	operation c.OperationID
	process   oc.ProcessID
	attempt   string
	fence     int64
	phase     string
}

func (r lifecycleStopClaim) cause() f.TransactionCause {
	cause, _ := f.NewJobCause("project-lifecycle", r.operation.String(), r.attempt)
	return cause
}
func loadLifecycleStopClaim(ctx context.Context, x postgres.SQLExecutor, operation c.OperationID) (*lifecycleStopClaim, error) {
	r := &lifecycleStopClaim{operation: operation}
	var project, process string
	err := x.QueryRow(ctx, `SELECT project_id::text,process_id::text,attempt_id::text,fence,phase FROM agenteam_project.work_claims WHERE work_kind='lifecycle' AND work_id=$1`, operation.String()).Scan(&project, &process, &r.attempt, &r.fence, &r.phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if r.project, err = parseID[identity.Project](project); err != nil {
		return nil, err
	}
	if r.process, err = parseID[oc.Process](process); err != nil {
		return nil, err
	}
	if _, err = parseID[struct{}](r.attempt); err != nil {
		return nil, err
	}
	if r.fence < 1 || r.phase != "running" && r.phase != "terminal" {
		return nil, unavailable(nil)
	}
	return r, nil
}
func (st *lifecycleStopState) canJoin(ctx context.Context, claim *lifecycleStopClaim) error {
	if claim == nil || claim.phase == "terminal" {
		return nil
	}
	if claim.process == st.process {
		st.mu.Lock()
		joined, ok := st.joined[claim.operation]
		st.mu.Unlock()
		if !ok || joined != *claim {
			return fault(f.ResourceBusy)
		}
		return nil
	}
	return portError(st.processes.ConfirmStopped(ctx, claim.process))
}

// current verifies all frozen participants, including providers that are not
// bound by this limited driver. Their required/pending state is never removed.
func (st *lifecycleStopState) current(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, operation c.OperationID) (c.LifecycleCause, *lifecycleRecord, error) {
	r, err := loadLifecycleOperation(ctx, x, project, operation)
	if err != nil {
		return c.LifecycleCause{}, nil, err
	}
	if r == nil {
		return c.LifecycleCause{}, nil, fault(f.NotFound)
	}
	cause := c.LifecycleCause{OperationID: operation, Action: r.operation.Action, ProjectVersion: r.operation.ProjectVersion}
	if cause.Validate() != nil {
		return c.LifecycleCause{}, nil, unavailable(nil)
	}
	fact, err := st.authority.lifecycleFact(ctx, x, project, cause, "")
	if err != nil {
		return c.LifecycleCause{}, nil, err
	}
	if fact.state != c.OperationAccepted && fact.state != c.OperationStopping {
		return c.LifecycleCause{}, nil, fault(f.InvalidState)
	}
	for _, p := range r.participants {
		if p.stop == "failed" {
			return c.LifecycleCause{}, nil, fault(f.InvalidState)
		}
	}
	return cause, r, nil
}
func (st *lifecycleStopState) start(ctx context.Context, project c.ProjectID, operation c.OperationID) (*lifecycleStopClaim, c.LifecycleCause, error) {
	previous, err := loadLifecycleStopClaim(ctx, st.store, operation)
	if err != nil {
		return nil, c.LifecycleCause{}, err
	}
	if previous != nil && previous.project != project {
		return nil, c.LifecycleCause{}, fault(f.Forbidden)
	}
	if err = st.canJoin(ctx, previous); err != nil {
		return nil, c.LifecycleCause{}, err
	}
	if err = ctx.Err(); err != nil {
		return nil, c.LifecycleCause{}, portError(err)
	}
	attempt, err := f.NewID[struct{}]()
	if err != nil {
		return nil, c.LifecycleCause{}, unavailable(err)
	}
	claim := &lifecycleStopClaim{project: project, operation: operation, process: st.process, attempt: attempt.String(), fence: 1, phase: "running"}
	if previous != nil {
		if previous.fence == math.MaxInt64 {
			return nil, c.LifecycleCause{}, fault(f.InvalidState)
		}
		claim.fence = previous.fence + 1
	}
	var cause c.LifecycleCause
	result := st.store.WithinTx(ctx, claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
			return unavailable(err)
		}
		x, err := st.authority.lifecycleExecutor(ctx, tx, project)
		if err != nil {
			return err
		}
		var r *lifecycleRecord
		cause, r, err = st.current(ctx, x, project, operation)
		if err != nil {
			return err
		}
		actual, err := loadLifecycleStopClaim(ctx, x, operation)
		if err != nil {
			return err
		}
		if (actual == nil) != (previous == nil) || actual != nil && *actual != *previous {
			return fault(f.ResourceBusy)
		}
		if actual == nil {
			_, err = x.Exec(ctx, `INSERT INTO agenteam_project.work_claims(work_kind,work_id,project_id,process_id,attempt_id,fence,phase) VALUES('lifecycle',$1,$2,$3,$4,1,'running')`, operation.String(), project.String(), claim.process.String(), claim.attempt)
		} else {
			tag, e := x.Exec(ctx, `UPDATE agenteam_project.work_claims SET process_id=$2,attempt_id=$3,fence=$4,phase='running' WHERE work_kind='lifecycle' AND work_id=$1 AND project_id=$5 AND process_id=$6 AND attempt_id=$7 AND fence=$8 AND phase=$9`, operation.String(), claim.process.String(), claim.attempt, claim.fence, project.String(), previous.process.String(), previous.attempt, previous.fence, previous.phase)
			err = affected(tag, e)
		}
		if err != nil {
			return unavailable(err)
		}
		if r.operation.State == c.OperationAccepted {
			version, err := nextVersion(r.operation.Version)
			if err != nil {
				return err
			}
			now, err := dbNow(ctx, x)
			if err != nil {
				return err
			}
			tag, err := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='stopping',version=$3,updated_at=$4 WHERE id=$1 AND project_id=$2 AND state='accepted' AND version=$5`, operation.String(), project.String(), int64(version), now.Time(), int64(r.operation.Version))
			if err = affected(tag, err); err != nil {
				return err
			}
		}
		return nil
	})
	if err = commitError(result); err != nil {
		if result.State() == f.Unknown {
			return claim, c.LifecycleCause{}, err
		}
		return nil, c.LifecycleCause{}, err
	}
	st.mu.Lock()
	delete(st.joined, operation)
	st.mu.Unlock()
	return claim, cause, nil
}

// terminal retires this worker attempt only. Participant/operation completion
// is outside this driver, even when the local step returned without error.
func (st *lifecycleStopState) finish(ctx context.Context, claim lifecycleStopClaim, cause c.LifecycleCause) error {
	result := st.store.WithinTx(ctx, claim.cause(), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(claim.project, f.Exclusive)}); err != nil {
			return unavailable(err)
		}
		x, err := st.authority.lifecycleExecutor(ctx, tx, claim.project)
		if err != nil {
			return err
		}
		actual, err := loadLifecycleStopClaim(ctx, x, claim.operation)
		if err != nil {
			return err
		}
		if actual == nil || *actual != claim {
			return fault(f.ResourceBusy)
		}
		current, r, err := st.current(ctx, x, claim.project, claim.operation)
		if err != nil {
			return err
		}
		if current != cause || r.operation.State != c.OperationStopping {
			return fault(f.InvalidState)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.work_claims SET phase='terminal' WHERE work_kind='lifecycle' AND work_id=$1 AND project_id=$2 AND process_id=$3 AND attempt_id=$4 AND fence=$5 AND phase='running'`, claim.operation.String(), claim.project.String(), claim.process.String(), claim.attempt, claim.fence)
		return affected(tag, err)
	})
	return commitError(result)
}
