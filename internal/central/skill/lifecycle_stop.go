package skill

import (
	"context"
	"sort"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// These two methods implement only the Skills stop sub-capability. A root must
// compose every enabled domain of agent-skills-variables, and bind real cleanup,
// before registering a complete ProjectLifecycleParticipant.
func (s *Service) RequestStop(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	call, e := s.begin(ctx, false)
	if e != nil {
		return pc.StopReport{}, e
	}
	defer s.end(call)
	snapshot, e := s.captureStop(call.ctx, actor, cause, scope)
	if e != nil {
		return pc.StopReport{}, e
	}
	// No cancellation escapes an unconfirmed authorization transaction. The
	// pointers name the captured original calls, never a later replacement.
	for _, original := range snapshot.calls {
		original.cancel()
	}
	return snapshot.report(cause, scope)
}

func (s *Service) InspectStop(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	call, e := s.begin(ctx, false)
	if e != nil {
		return pc.StopReport{}, e
	}
	defer s.end(call)
	snapshot, e := s.captureStop(call.ctx, actor, cause, scope)
	if e != nil {
		return pc.StopReport{}, e
	}
	state := s.state()
	proven := map[skillWorkID]bool{}
	var firstErr error
	for workID, local := range snapshot.local {
		state.mu.Lock()
		pendingDiscard := local.fact.kind == installationWork && local.installationCallerReturned && !local.returned
		state.mu.Unlock()
		if pendingDiscard {
			if err := s.joinInstallationDiscard(local); err != nil && firstErr == nil {
				firstErr = err
			}
			state.mu.Lock()
			snapshot.returned[workID] = local.returned
			state.mu.Unlock()
		}
	}
	// The first current gate has committed before any process proof is asked.
	// Proof occurs outside SQL; the second transaction revalidates the same
	// gate and acquires the original writer's complete lock set after proof.
	for workID, w := range snapshot.work {
		if w.process == state.process {
			proven[workID] = snapshot.returned[workID]
			continue
		}
		if e := call.ctx.Err(); e != nil {
			return pc.StopReport{}, portError(e)
		}
		if e := state.processes.ConfirmStopped(call.ctx, w.process); e != nil {
			if firstErr == nil {
				firstErr = portError(e)
			}
		} else {
			proven[workID] = true
		}
	}
	// An unknown registration may have no durable row. Only this instance's
	// exact returned owner plus the original lock boundary can retire it.
	for workID := range snapshot.local {
		if snapshot.returned[workID] {
			proven[workID] = true
		}
	}
	joined := map[skillWorkID]bool{}
	var installationLocks []f.LockRequest
	for _, parent := range snapshot.installations {
		locks, err := parent.locks(f.Exclusive)
		if err != nil {
			return pc.StopReport{}, err
		}
		installationLocks = append(installationLocks, locks...)
	}
	e = s.stopTransaction(call.ctx, actor, cause, scope, snapshot.row, func(ctx context.Context, x postgres.SQLExecutor, row *initializationRow) error {
		for workID := range proven {
			if !proven[workID] {
				continue
			}
			expected, exists := snapshot.work[workID]
			if !exists {
				expected = snapshot.local[workID].fact
			}
			if row == nil || expected.project != scope.ProjectID {
				return unavailable(nil)
			}
			if installedWork(expected.kind) {
				parent := snapshot.installations[workID]
				if parent == nil || parent.project != scope.ProjectID || parent.skill != expected.skill {
					return unavailable(nil)
				}
				owner := &ownedWork{fact: expected, installation: parent}
				if err := owner.checkParent(ctx, x); err != nil {
					return err
				}
			} else if expected.skill != row.skill {
				return unavailable(nil)
			}
			current, e := loadWork(ctx, x, workID)
			if e != nil {
				return e
			}
			if current == nil {
				if snapshot.local[workID] == nil || !snapshot.returned[workID] {
					return unavailable(nil)
				}
			} else {
				if !sameWorkOwner(*current, expected) {
					return fault(f.ResourceBusy)
				}
				if e = joinWork(ctx, x, expected); e != nil {
					return e
				}
			}
			joined[workID] = true
		}
		return nil
	}, installationLocks)
	if e != nil {
		return pc.StopReport{}, e
	}
	for workID := range joined {
		delete(snapshot.work, workID)
		if local := snapshot.local[workID]; local != nil {
			s.forgetOwnedWork(local)
			delete(snapshot.local, workID)
		}
	}
	// Captured actual calls remain pending even if their durable I/O accounting
	// just joined. A later inspection observes their real end; no cancel or
	// process proof is substituted for this process's unfinished call.
	report, e := snapshot.report(cause, scope)
	if e != nil {
		return pc.StopReport{}, e
	}
	return report, firstErr
}

type stopSnapshot struct {
	row           *initializationRow
	work          map[skillWorkID]workFact
	local         map[skillWorkID]*ownedWork
	returned      map[skillWorkID]bool
	calls         map[skillWorkID]*serviceCall
	installations map[skillWorkID]*installationRow
	more          bool
}

func stoppedKind(action pc.LifecycleAction, kind workKind) bool {
	return kind == initializationWork || kind == installationWork || action == pc.Delete && (kind == packageReaderWork || kind == installedPackageReaderWork)
}

func stopArguments(actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if e := pc.RequireProjectScope(scope); e != nil {
		return e
	}
	if actor.Validate() != nil || cause.Validate() != nil {
		return invalid()
	}
	d := actor.Details()
	if d.Kind != id.Service || d.ServiceName != id.ProjectLifecycle || d.ProjectID != scope.ProjectID.String() || d.CauseRef != cause.OperationID.String() {
		return fault(f.Forbidden)
	}
	return nil
}

func (s *Service) captureStop(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (stopSnapshot, error) {
	out := stopSnapshot{work: map[skillWorkID]workFact{}, local: map[skillWorkID]*ownedWork{}, returned: map[skillWorkID]bool{}, calls: map[skillWorkID]*serviceCall{}, installations: map[skillWorkID]*installationRow{}}
	if e := stopArguments(actor, cause, scope); e != nil {
		return out, e
	}
	state := s.state()
	discovered, e := loadInitialization(ctx, state.authority.state().store, scope.ProjectID)
	if e != nil {
		return out, e
	}
	e = s.stopTransaction(ctx, actor, cause, scope, discovered, func(ctx context.Context, x postgres.SQLExecutor, row *initializationRow) error {
		// The current Project lifecycle gate requires an initialized Project.
		// Every such Project in an enabled Skills participant has this original
		// initialization; missing domain facts cannot be reported as empty.
		if row == nil {
			return unavailable(nil)
		}
		out.row = row
		var raw []string
		if e := x.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY id),ARRAY[]::text[]) FROM (SELECT id FROM agenteam_skill.work WHERE project_id=$1 AND phase<>'joined' AND ($2 OR kind IN ('initialization','installation')) ORDER BY id LIMIT 101) pending`, scope.ProjectID.String(), cause.Action == pc.Delete).Scan(&raw); e != nil {
			return unavailable(e)
		}
		if len(raw) > MaxRecoveryWork+1 {
			return unavailable(nil)
		}
		if len(raw) > MaxRecoveryWork {
			out.more = true
			raw = raw[:MaxRecoveryWork]
		}
		for _, text := range raw {
			workID, e := f.ParseID[skillWork](text)
			if e != nil {
				return unavailable(e)
			}
			if _, duplicate := out.work[workID]; duplicate {
				return unavailable(nil)
			}
			w, e := loadWork(ctx, x, workID)
			if e != nil {
				return e
			}
			if w == nil || row == nil || w.project != scope.ProjectID || w.phase == workJoined || !stoppedKind(cause.Action, w.kind) {
				return unavailable(nil)
			}
			if installedWork(w.kind) {
				parent, err := loadInstallationSkill(ctx, x, w.project, w.skill)
				if err != nil {
					return err
				}
				if parent == nil || parent.project != w.project || parent.skill != w.skill {
					return unavailable(nil)
				}
				out.installations[workID] = parent
			} else if w.skill != row.skill {
				return unavailable(nil)
			}
			out.work[workID] = *w
		}
		state.mu.Lock()
		defer state.mu.Unlock()
		for original := range state.calls {
			if original.project == scope.ProjectID && stoppedKind(cause.Action, original.kind) {
				out.calls[original.workID] = original
			}
		}
		for workID, w := range state.work {
			if w.fact.project != scope.ProjectID || !stoppedKind(cause.Action, w.fact.kind) {
				continue
			}
			if row == nil {
				return unavailable(nil)
			}
			if installedWork(w.fact.kind) {
				parent := w.installation
				if parent == nil || parent.validate() != nil || parent.project != scope.ProjectID || parent.skill != w.fact.skill {
					return unavailable(nil)
				}
				if stored := out.installations[workID]; stored != nil && (stored.id != parent.id || stored.revision != parent.revision || stored.semantic != parent.semantic) {
					return fault(f.ResourceBusy)
				}
				copy := *parent
				out.installations[workID] = &copy
			} else if w.fact.skill != row.skill {
				return unavailable(nil)
			}
			if persisted, exists := out.work[workID]; exists && !sameWorkOwner(persisted, w.fact) {
				return fault(f.ResourceBusy)
			}
			out.local[workID] = w
			out.returned[workID] = w.returned
		}
		return nil
	})
	return out, e
}

func (s *Service) stopTransaction(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, discovered *initializationRow, fn func(context.Context, postgres.SQLExecutor, *initializationRow) error, extra ...[]f.LockRequest) error {
	if e := stopArguments(actor, cause, scope); e != nil {
		return e
	}
	locks := []f.LockRequest{projectLock(scope.ProjectID, f.Exclusive)}
	var e error
	if discovered != nil {
		if discovered.request.ProjectID != scope.ProjectID {
			return unavailable(nil)
		}
		locks, e = discovered.locks(f.Exclusive, discovered.object)
		if e != nil {
			return e
		}
	}
	for _, additional := range extra {
		locks = append(locks, additional...)
	}
	locks, e = oc.NormalizeAccessLocks(locks)
	if e != nil {
		return e
	}
	transactionCause, e := f.NewRecoveryCause("skill-stop", cause.OperationID.String(), scope.ProjectID.String())
	if e != nil {
		return e
	}
	store := s.state().authority.state().store
	var callbackErr error
	result := store.WithinTx(ctx, transactionCause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if e := store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		x, e := store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		if e = s.state().authority.state().projects.ValidateLifecycleInTx(ctx, tx, actor, cause, pc.SkillsParticipant, pc.StopPhase); e != nil {
			return portError(e)
		}
		row, e := loadInitialization(ctx, x, scope.ProjectID)
		if e != nil {
			return e
		}
		if (row == nil) != (discovered == nil) || row != nil && (row.request != discovered.request || row.skill != discovered.skill || row.revision != discovered.revision || row.object != discovered.object) {
			return fault(f.ResourceBusy)
		}
		return fn(ctx, x, row)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return callbackErr
	}
	return commitError(result)
}

func (snapshot stopSnapshot) report(cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	refs := map[skillWorkID]pc.PendingRef{}
	unknown := map[skillWorkID]bool{}
	add := func(workID skillWorkID, kind workKind, uncertain bool) {
		name := pc.ReferenceKind("skill-initialization")
		if kind == packageReaderWork || kind == installedPackageReaderWork {
			name = "skill-package-reader"
		} else if kind == installationWork {
			name = "skill-installation"
		}
		resource, _ := f.ParseID[pc.ResourceIdentity](workID.String())
		refs[workID] = pc.PendingRef{Participant: pc.SkillsParticipant, Kind: name, ID: resource}
		unknown[workID] = uncertain
	}
	for workID, w := range snapshot.work {
		add(workID, w.kind, true)
	}
	for workID, w := range snapshot.local {
		add(workID, w.fact.kind, snapshot.returned[workID])
	}
	for workID, original := range snapshot.calls {
		add(workID, original.kind, false)
	}
	ordered := make([]skillWorkID, 0, len(refs))
	for workID := range refs {
		ordered = append(ordered, workID)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].String() < ordered[j].String() })
	details := pc.StopDetails{State: pc.Stopped}
	for _, workID := range ordered {
		if unknown[workID] {
			details.UnknownRefs = append(details.UnknownRefs, refs[workID])
		} else {
			details.ActiveRefs = append(details.ActiveRefs, refs[workID])
		}
	}
	if len(ordered) > 0 || snapshot.more {
		details.State = pc.StopPending
		details.SafeReason = pc.ReasonWorkPending
		if len(details.UnknownRefs) > 0 {
			details.SafeReason = pc.ReasonOutcomeUnknown
		}
	}
	return pc.NewStopReport(pc.SkillsParticipant, cause, scope, details)
}
