package outbox

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type lifecycleCheckpoint struct {
	cause oc.LifecycleCause
	phase string
}

func (r *runtimeState) recoverLifecycleBatch(ctx context.Context) error {
	select {
	case r.recoverSlot <- struct{}{}:
	case <-ctx.Done():
		return portError(ctx.Err())
	}
	defer func() { <-r.recoverSlot }()
	return r.recoverLifecycles(ctx, false)
}

// A page is a fair cursor over exact durable operations. Even refusal advances
// the local scan; the original checkpoint remains intact for a later round.
func (r *runtimeState) recoverLifecycles(ctx context.Context, all bool) error {
	after := r.lifecycleAfter
	if all {
		after = ""
	}
	var first error
	for {
		if ctx.Err() != nil {
			if first != nil {
				return first
			}
			return portError(ctx.Err())
		}
		rows, err := r.svc.state().store.Query(ctx, `SELECT project_id::text,operation_id::text,action,project_version,phase FROM agenteam_outbox.project_lifecycle WHERE phase<>'completed' AND (action='delete' OR phase='stopping') AND ($1::uuid IS NULL OR project_id>$1) ORDER BY project_id LIMIT $2`, nullableUUID(after), r.options.Batch)
		if err != nil {
			return unavailable(err)
		}
		var batch []lifecycleCheckpoint
		for rows.Next() {
			var project, operation, action, phase string
			var version int64
			if err = rows.Scan(&project, &operation, &action, &version, &phase); err != nil {
				rows.Close()
				return unavailable(err)
			}
			p, e1 := foundation.ParseID[identity.Project](project)
			op, e2 := foundation.ParseID[oc.LifecycleOperation](operation)
			cause, e3 := oc.NewLifecycleCause(oc.LifecycleDetails{ProjectID: p, OperationID: op, Action: oc.LifecycleAction(action), ProjectVersion: foundation.Version(version)})
			if e1 != nil || e2 != nil || e3 != nil {
				rows.Close()
				return unavailable(nil)
			}
			batch = append(batch, lifecycleCheckpoint{cause, phase})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return unavailable(err)
		}
		if len(batch) == 0 {
			if !all {
				r.lifecycleAfter = ""
			}
			return first
		}
		for _, c := range batch {
			if ctx.Err() != nil {
				if first != nil {
					return first
				}
				return portError(ctx.Err())
			}
			after = c.cause.Details().ProjectID.String()
			if !all {
				r.lifecycleAfter = after
			}
			item, cancel := context.WithTimeout(ctx, r.options.ItemTimeout)
			err = r.recoverLifecycle(item, c)
			err = recoveryBudgetError(item, ctx, err)
			cancel()
			if err != nil && !protectedError(err) && first == nil {
				first = err
			}
		}
		if !all {
			return first
		}
	}
}
func (r *runtimeState) recoverLifecycle(ctx context.Context, c lifecycleCheckpoint) error {
	resolver, ok := r.svc.state().auth.Projects.(oc.LifecycleActorResolver)
	if !ok || nilPort(resolver) {
		return failure(foundation.DependencyUnbound, nil)
	}
	actor, err := resolver.ResolveLifecycleActor(ctx, c.cause)
	if err != nil {
		return portError(err)
	}
	d := actor.Details()
	if actor.Validate() != nil || d.Kind != identity.Service || d.ServiceName != identity.ProjectLifecycle || d.ProjectID != c.cause.Details().ProjectID.String() {
		return invalid()
	}
	// Public lifecycle methods reread the exact checkpoint and current formal
	// cause under their full locks. No new stop operation is created by recovery.
	if c.phase == "stopping" {
		if _, err = r.svc.InspectStop(ctx, actor, c.cause); err != nil {
			return err
		}
	}
	if c.cause.Details().Action == oc.DeleteProject {
		_, err = r.svc.Cleanup(ctx, actor, c.cause)
		return err
	}
	return nil
}

func (r *runtimeState) checkLifecycleResolver(ctx context.Context) error {
	var pending bool
	if err := r.svc.state().store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_outbox.project_lifecycle WHERE phase<>'completed' AND (action='delete' OR phase='stopping'))`).Scan(&pending); err != nil {
		return unavailable(err)
	}
	if pending {
		resolver, ok := r.svc.state().auth.Projects.(oc.LifecycleActorResolver)
		if !ok || nilPort(resolver) {
			return failure(foundation.DependencyUnbound, nil)
		}
	}
	return nil
}
