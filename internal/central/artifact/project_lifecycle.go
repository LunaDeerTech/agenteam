package artifact

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

var _ oc.ObjectProjectStop = (*Service)(nil)

func artifactStopReport(cause oc.ProjectStopCause, stopped bool, reason oc.ProjectStopReason) oc.ObjectStopReport {
	state := oc.ProjectStopPending
	if stopped {
		state = oc.ProjectStopped
		reason = ""
	}
	r, _ := oc.NewObjectStopReport(oc.ArtifactStopComponent, cause, oc.ObjectStopDetails{State: state, SafeReason: reason})
	return r
}
func artifactStopFailure(cause oc.ProjectStopCause, err error) (oc.ObjectStopReport, error) {
	reason := oc.ProjectStopDependencyUnavailable
	var fault *f.Fault
	if errors.As(err, &fault) {
		if fault.Code == f.CommitUnknown {
			reason = oc.ProjectStopOutcomeUnknown
		}
		if fault.Code == f.DependencyUnbound {
			reason = oc.ProjectStopDependencyUnbound
		}
	}
	return artifactStopReport(cause, false, reason), err
}
func (s *Service) RequestProjectStop(ctx context.Context, actor i.Actor, cause oc.ProjectStopCause) (oc.ObjectStopReport, error) {
	return s.projectStop(ctx, actor, cause, oc.RequestProjectStopStep)
}
func (s *Service) InspectProjectStop(ctx context.Context, actor i.Actor, cause oc.ProjectStopCause) (oc.ObjectStopReport, error) {
	return s.projectStop(ctx, actor, cause, oc.InspectProjectStopStep)
}

func (st *projectCallOwnershipState) stopTx(ctx context.Context, request oc.ProjectStopRequest, fn func(context.Context, postgres.SQLExecutor, oc.ProjectStopAuthorization) error) f.CommitResult {
	deps, err := st.authority.DiscoverProjectStop(ctx, request)
	if err != nil {
		return rejected(err)
	}
	if deps.Validate() != nil {
		return rejected(unavailable(nil))
	}
	d := request.Details().Cause.Details()
	cause, _ := f.NewRecoveryCause("artifact-project-stop", d.OperationID.String(), string(request.Details().Step))
	return st.service.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locks := append([]f.LockRequest{}, deps.Locks()...)
		locks = append(locks, artifactProjectLock(d.ProjectID, f.Exclusive))
		if err := st.service.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := st.requireProcess(ctx, tx); err != nil {
			return portError(err)
		}
		grant, err := st.authority.ValidateProjectStopInTx(ctx, tx, request, deps)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(tx, request, deps) {
			return failure(f.Forbidden, nil)
		}
		x, err := st.service.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, x, grant)
	})
}

func readArtifactStop(ctx context.Context, x postgres.SQLExecutor, cause oc.ProjectStopCause) (string, error) {
	d := cause.Details()
	var action, state string
	var version int64
	err := x.QueryRow(ctx, `SELECT action,project_version,state FROM agenteam_artifact.project_stops WHERE project_id=$1 AND operation_id=$2`, d.ProjectID.String(), d.OperationID.String()).Scan(&action, &version, &state)
	if noRows(err) {
		return "", nil
	}
	if err != nil {
		return "", unavailable(err)
	}
	if action != string(d.Action) || version != int64(d.ProjectVersion) || state != "stopping" && state != "stopped" {
		return "", failure(f.InvalidState, nil)
	}
	return state, nil
}

type artifactStopOwner struct{ process, instance string }

func readArtifactStopOwner(ctx context.Context, x postgres.SQLExecutor, cause oc.ProjectStopCause) (artifactStopOwner, error) {
	var out artifactStopOwner
	d := cause.Details()
	err := x.QueryRow(ctx, `SELECT COALESCE(owner_process_id::text,''),COALESCE(owner_instance_id::text,'') FROM agenteam_artifact.project_stops WHERE project_id=$1 AND operation_id=$2`, d.ProjectID.String(), d.OperationID.String()).Scan(&out.process, &out.instance)
	if noRows(err) {
		return out, nil
	}
	if err != nil {
		return out, unavailable(err)
	}
	if (out.process == "") != (out.instance == "") {
		return out, unavailable(nil)
	}
	if out.process != "" {
		if _, err := f.ParseID[oc.Process](out.process); err != nil {
			return out, unavailable(err)
		}
		if _, err := f.ParseID[artifactInstanceID](out.instance); err != nil {
			return out, unavailable(err)
		}
	}
	return out, nil
}

func (s *Service) projectStop(ctx context.Context, actor i.Actor, cause oc.ProjectStopCause, step oc.ProjectStopStep) (oc.ObjectStopReport, error) {
	request, err := oc.NewProjectStopRequest(oc.ProjectStopRequestDetails{Actor: actor, Cause: cause, Component: oc.ArtifactStopComponent, Step: step})
	if err != nil || ctx == nil {
		return oc.ObjectStopReport{}, invalid()
	}
	if s == nil || s.data == nil || s.state().ownership == nil {
		return artifactStopFailure(cause, failure(f.DependencyUnbound, nil))
	}
	st := s.state().ownership
	done, err := st.beginControl()
	if err != nil {
		return artifactStopFailure(cause, err)
	}
	defer done()
	d := cause.Details()
	st.mu.Lock()
	call := st.stops[d.ProjectID]
	if call != nil && (call.active || !call.cause.Equal(cause) || !call.actor.Equal(actor)) {
		st.mu.Unlock()
		return artifactStopFailure(cause, failure(f.ResourceBusy, nil))
	}
	if call == nil {
		if len(st.stops) >= 256 {
			st.mu.Unlock()
			return artifactStopFailure(cause, failure(f.ResourceBusy, nil))
		}
		call = &artifactStopCall{actor: actor, cause: cause}
		st.stops[d.ProjectID] = call
	}
	call.active = true
	retained := call.retained
	st.signalLocked()
	st.mu.Unlock()
	defer func() {
		st.mu.Lock()
		call.active = false
		if !call.retained {
			delete(st.stops, d.ProjectID)
		}
		st.signalLocked()
		st.mu.Unlock()
	}()
	if retained {
		validated := false
		result := st.stopTx(ctx, request, func(ctx context.Context, x postgres.SQLExecutor, _ oc.ProjectStopAuthorization) error {
			_, err := readArtifactStop(ctx, x, cause)
			validated = err == nil && ctx.Err() == nil
			return err
		})
		if err := artifactJoinedError(ctx, result, validated); err != nil {
			return artifactStopFailure(cause, errors.Join(projectCleanupUnknown(call.original), err))
		}
		st.mu.Lock()
		call.retained = false
		st.mu.Unlock()
		// This invocation only confirms the original outcome. A later explicit
		// Request/Inspect performs revocation or another checkpoint.
		return artifactStopReport(cause, false, oc.ProjectStopWorkPending), nil
	}
	// A stopped receipt may already be visible while its original Store call
	// is still returning. Only this exact instance, or the exact foreign
	// ProcessGuard death proof, can consume that receipt.
	var prior artifactStopOwner
	preflightValid := false
	preflight := st.stopTx(ctx, request, func(ctx context.Context, x postgres.SQLExecutor, _ oc.ProjectStopAuthorization) error {
		_, err := readArtifactStop(ctx, x, cause)
		if err != nil {
			return err
		}
		prior, err = readArtifactStopOwner(ctx, x, cause)
		preflightValid = err == nil && ctx.Err() == nil
		return err
	})
	if err := st.stopResult(ctx, call, preflight, preflightValid); err != nil {
		return artifactStopFailure(cause, err)
	}
	foreignOwnerDead := false
	if prior.process != "" && prior.process != st.process.String() {
		process, _ := f.ParseID[oc.Process](prior.process)
		foreignOwnerDead = st.proof.ConfirmStopped(ctx, process) == nil
	}
	var captured []artifactCallRecord
	var readOnly bool
	ownerPending := false
	validated := false
	gate := st.stopTx(ctx, request, func(ctx context.Context, x postgres.SQLExecutor, grant oc.ProjectStopAuthorization) error {
		state, err := readArtifactStop(ctx, x, cause)
		if err != nil {
			return err
		}
		if state != "" {
			current, err := readArtifactStopOwner(ctx, x, cause)
			if err != nil {
				return err
			}
			if current.process != st.process.String() || current.instance != st.instance.String() {
				if current.process == "" || !foreignOwnerDead || current != prior {
					ownerPending = true
					validated = ctx.Err() == nil
					return ctx.Err()
				}
				if grant.Mode() != oc.ContinueProjectStop {
					// Read-only confirmation can use the proved original foreign
					// receipt, but never rewrites its recorded owner.
					readOnly = true
				} else if _, err = x.Exec(ctx, `UPDATE agenteam_artifact.project_stops SET owner_process_id=$3,owner_instance_id=$4 WHERE project_id=$1 AND operation_id=$2 AND owner_process_id=$5 AND owner_instance_id=$6`, d.ProjectID.String(), d.OperationID.String(), st.process.String(), st.instance.String(), current.process, current.instance); err != nil {
					return unavailable(err)
				}
			}
		}
		if grant.Mode() == oc.ReadProjectStop {
			if state != "stopped" {
				return failure(f.InvalidState, nil)
			}
			readOnly = true
		} else if grant.Mode() != oc.ContinueProjectStop {
			return failure(f.Forbidden, nil)
		}
		if state == "" {
			if step != oc.RequestProjectStopStep || readOnly {
				return failure(f.InvalidState, nil)
			}
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_artifact.project_stops(project_id,operation_id,action,project_version,owner_process_id,owner_instance_id) VALUES($1,$2,$3,$4,$5,$6)`, d.ProjectID.String(), d.OperationID.String(), string(d.Action), int64(d.ProjectVersion), st.process.String(), st.instance.String()); err != nil {
				return unavailable(err)
			}
		}
		if !readOnly {
			// Capture only calls admitted before this exact stop epoch. A stale
			// archive cannot target later calls admitted after Restore.
			var raw []byte
			if err = x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(record ORDER BY id),'[]'::jsonb) FROM (SELECT id,`+artifactCallProjection+` AS record FROM agenteam_artifact.project_work WHERE project_id=$1 AND joined_at IS NULL AND admission_version<$2 AND (kind<>'read' OR $3::boolean) ORDER BY id LIMIT 100) owned`, d.ProjectID.String(), int64(d.ProjectVersion), d.Action == oc.ProjectStopDelete).Scan(&raw); err != nil {
				return unavailable(err)
			}
			if len(raw) > 65536 || json.Unmarshal(raw, &captured) != nil || captured == nil || len(captured) > 100 {
				return unavailable(nil)
			}
			for _, r := range captured {
				if !r.valid() || r.Project != d.ProjectID || r.Joined {
					return unavailable(nil)
				}
				if _, err = x.Exec(ctx, `UPDATE agenteam_artifact.project_work SET revoked_by=$2 WHERE id=$1 AND (revoked_by IS NULL OR revoked_by=$2)`, r.ID.String(), d.OperationID.String()); err != nil {
					return unavailable(err)
				}
			}
		}
		validated = ctx.Err() == nil
		return ctx.Err()
	})
	if err := st.stopResult(ctx, call, gate, validated); err != nil {
		return artifactStopFailure(cause, err)
	}
	if ownerPending {
		return artifactStopReport(cause, false, oc.ProjectStopWorkPending), nil
	}
	// No cancellation occurs before the actual confirmed Store return above.
	var cancels []context.CancelFunc
	st.mu.Lock()
	for _, r := range captured {
		if local := st.owners[r.ID]; local != nil && local.record.same(r) && !local.returned {
			cancels = append(cancels, local.cancel)
		}
	}
	st.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	// Source work is converged only after the original stop gate has returned.
	sourcePending := false
	if !readOnly {
		sourcePending, err = s.convergeProjectSources(ctx, actor, cause)
		if err != nil {
			return artifactStopFailure(cause, err)
		}
	}
	// ConfirmStopped runs outside every caller Tx. It proves only the selected
	// foreign process; current-process unknown instances never obtain it.
	foreign, unknown, err := st.foreignStopProcesses(ctx, request, call)
	if err != nil {
		return artifactStopFailure(cause, err)
	}
	dead := make(map[oc.ProcessID]bool, len(foreign))
	for _, process := range foreign {
		if err := st.proof.ConfirmStopped(ctx, process); err == nil {
			dead[process] = true
		} else {
			unknown = true
		}
	}
	pending := unknown || sourcePending
	validated = false
	checkpoint := st.stopTx(ctx, request, func(ctx context.Context, x postgres.SQLExecutor, grant oc.ProjectStopAuthorization) error {
		state, err := readArtifactStop(ctx, x, cause)
		if err != nil {
			return err
		}
		if state == "" {
			return failure(f.InvalidState, nil)
		}
		if grant.Mode() != oc.ContinueProjectStop && !(grant.Mode() == oc.ReadProjectStop && state == "stopped") {
			return failure(f.Forbidden, nil)
		}
		calls, more, err := listArtifactCalls(ctx, x, d.ProjectID, d.Action == oc.ProjectStopDelete)
		if err != nil {
			return err
		}
		pending = pending || more
		for _, record := range calls {
			if dead[record.Process] && grant.Mode() == oc.ContinueProjectStop {
				if _, err = x.Exec(ctx, `UPDATE agenteam_artifact.project_work SET joined_at=GREATEST(clock_timestamp(),created_at) WHERE id=$1 AND process_id=$2 AND joined_at IS NULL`, record.ID.String(), record.Process.String()); err != nil {
					return unavailable(err)
				}
			} else {
				pending = true
			}
		}
		st.mu.Lock()
		for _, local := range st.owners {
			if local.record.Project == d.ProjectID && (d.Action == oc.ProjectStopDelete || local.record.Kind != "read") {
				pending = true
			}
		}
		st.mu.Unlock()
		sources, err := s.projectCleanupSourcesPending(ctx, x, d.ProjectID)
		if err != nil {
			return err
		}
		pending = pending || sources
		// Re-read foreign identity facts under the current cut: a newly arrived
		// identity cannot borrow the earlier process proof set.
		var processesRaw []byte
		if err = x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(process_id),'[]'::jsonb) FROM (SELECT DISTINCT process_id FROM agenteam_artifact.project_work WHERE project_id=$1 AND (kind<>'read' OR $2::boolean) AND (process_id<>$3 OR instance_id IS DISTINCT FROM $4) ORDER BY process_id) foreign_owners`, d.ProjectID.String(), d.Action == oc.ProjectStopDelete, st.process.String(), st.instance.String()).Scan(&processesRaw); err != nil {
			return unavailable(err)
		}
		var processes []string
		if json.Unmarshal(processesRaw, &processes) != nil || processes == nil {
			return unavailable(nil)
		}
		for _, raw := range processes {
			process, err := f.ParseID[oc.Process](raw)
			if err != nil {
				return unavailable(err)
			}
			if !dead[process] {
				pending = true
			}
		}
		if !pending && state != "stopped" {
			if grant.Mode() != oc.ContinueProjectStop {
				return failure(f.InvalidState, nil)
			}
			if _, err = x.Exec(ctx, `UPDATE agenteam_artifact.project_stops SET state='stopped',stopped_at=clock_timestamp(),scan_kind=0,scan_after=NULL WHERE project_id=$1 AND operation_id=$2 AND state='stopping'`, d.ProjectID.String(), d.OperationID.String()); err != nil {
				return unavailable(err)
			}
		}
		validated = ctx.Err() == nil
		return ctx.Err()
	})
	if err := st.stopResult(ctx, call, checkpoint, validated); err != nil {
		return artifactStopFailure(cause, err)
	}
	return artifactStopReport(cause, !pending, oc.ProjectStopWorkPending), nil
}

func (st *projectCallOwnershipState) stopResult(ctx context.Context, call *artifactStopCall, result f.CommitResult, validated bool) error {
	if result.State() == f.Unknown {
		st.mu.Lock()
		call.original = result
		call.retained = true
		st.mu.Unlock()
	}
	return artifactJoinedError(ctx, result, validated)
}
func (st *projectCallOwnershipState) foreignStopProcesses(ctx context.Context, request oc.ProjectStopRequest, call *artifactStopCall) ([]oc.ProcessID, bool, error) {
	var found []oc.ProcessID
	unknown := false
	validated := false
	d := request.Details().Cause.Details()
	result := st.stopTx(ctx, request, func(ctx context.Context, x postgres.SQLExecutor, _ oc.ProjectStopAuthorization) error {
		var raw []byte
		if err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(process_id),'[]'::jsonb) FROM (SELECT DISTINCT process_id FROM agenteam_artifact.project_work WHERE project_id=$1 AND (kind<>'read' OR $2::boolean) AND (process_id<>$3 OR instance_id IS DISTINCT FROM $4) ORDER BY process_id) foreign_owners`, d.ProjectID.String(), d.Action == oc.ProjectStopDelete, st.process.String(), st.instance.String()).Scan(&raw); err != nil {
			return unavailable(err)
		}
		var rows []string
		if json.Unmarshal(raw, &rows) != nil || rows == nil {
			return unavailable(nil)
		}
		for _, raw := range rows {
			p, err := f.ParseID[oc.Process](raw)
			if err != nil {
				return unavailable(err)
			}
			if p == st.process {
				unknown = true
			} else {
				found = append(found, p)
			}
		}
		validated = ctx.Err() == nil
		return ctx.Err()
	})
	return found, unknown, st.stopResult(ctx, call, result, validated)
}
