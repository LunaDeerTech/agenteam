package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type ProjectCallOwnershipDetails struct {
	ProjectID        i.ProjectID
	OperationID      pc.OperationID
	Action           pc.LifecycleAction
	ProjectVersion   f.Version
	PendingCalls     int
	HasMore          bool
	RegisteredJoined bool
}

// PendingCalls is bounded when HasMore is true. RegisteredJoined describes
// opted-in calls at this observation point, never a full lifecycle participant.
type ProjectCallOwnershipReport struct {
	data func() ProjectCallOwnershipDetails
}

func (r ProjectCallOwnershipReport) Valid() bool { return r.data != nil }
func (r ProjectCallOwnershipReport) Details() ProjectCallOwnershipDetails {
	if r.data == nil {
		return ProjectCallOwnershipDetails{}
	}
	return r.data()
}
func (r ProjectCallOwnershipReport) Matches(cause pc.LifecycleCause, scope pc.ScopeRef) bool {
	if !r.Valid() || cause.Validate() != nil || pc.RequireProjectScope(scope) != nil {
		return false
	}
	d := r.Details()
	return d.ProjectID == scope.ProjectID && d.OperationID == cause.OperationID && d.Action == cause.Action && d.ProjectVersion == cause.ProjectVersion
}
func (ProjectCallOwnershipReport) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "agent_project_call_ownership_report")
}
func (ProjectCallOwnershipReport) LogValue() slog.Value {
	return slog.StringValue("agent_project_call_ownership_report")
}
func (ProjectCallOwnershipReport) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_project_call_ownership_report"`), nil
}
func (*ProjectCallOwnershipReport) UnmarshalJSON([]byte) error { return invalid() }

type projectCallCandidate struct {
	record projectCallRecord
	local  *projectCallOwner
	proven bool
}

func selectedProjectRecord(r projectCallRecord, cause pc.LifecycleCause, scope pc.ScopeRef) bool {
	return r.Project == scope.ProjectID && (r.Class == "mutation" || cause.Action == pc.Delete && r.Class == "read")
}

// ObserveStop never cancels local or remote work. Local cancellation remains an
// explicit LocalStopper.RequestStop, with its own committed authorization.
func (p *ProjectCallOwnership) ObserveStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ProjectCallOwnershipReport, error) {
	if err := projectCallStopArguments(ctx, actor, cause, scope); err != nil {
		return ProjectCallOwnershipReport{}, err
	}
	if p == nil || p.state == nil {
		return ProjectCallOwnershipReport{}, fault(f.DependencyUnbound)
	}
	s := p.state
	ctx, serviceDone, err := s.service.begin(ctx)
	if err != nil {
		return ProjectCallOwnershipReport{}, err
	}
	defer serviceDone()
	ctx, directoryDone, err := s.directory.state.calls.begin(ctx)
	if err != nil {
		return ProjectCallOwnershipReport{}, err
	}
	defer directoryDone()
	txCause, _ := f.NewRecoveryCause("agent-call-observe", cause.OperationID.String(), scope.ProjectID.String())
	var candidates []projectCallCandidate
	result := s.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := s.observationGate(ctx, tx, actor, cause, scope, f.Shared); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		records, _, err := listProjectCalls(ctx, x, scope.ProjectID, cause.Action == pc.Delete)
		if err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		seen := make(map[f.ID[projectCallIdentity]]bool, len(records))
		for _, r := range records {
			candidate := projectCallCandidate{record: r}
			if owner := s.owners[r.ID]; owner != nil && owner.record.sameOwner(r) {
				candidate.local = owner
			}
			candidates = append(candidates, candidate)
			seen[r.ID] = true
		}
		// Include retained Unknown/previously joined metadata absent from the
		// running page, but keep the same total per-observation bound of 100.
		for _, owner := range s.owners {
			if len(candidates) >= 100 {
				break
			}
			if !seen[owner.record.ID] && owner.returned && selectedProjectRecord(owner.record, cause, scope) {
				candidates = append(candidates, projectCallCandidate{record: owner.record, local: owner})
			}
		}
		return nil
	})
	if err := commitError(result); err != nil {
		return ProjectCallOwnershipReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return ProjectCallOwnershipReport{}, err
	}
	// Reserve exact returned proofs; no mutex survives SQL or death proof.
	var reserved []*projectCallOwner
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, owner := range reserved {
			owner.settling = false
		}
		s.signalLocked()
	}()
	for n := range candidates {
		candidate := &candidates[n]
		if candidate.record.Process == s.process {
			s.mu.Lock()
			owner := candidate.local
			if owner != nil && s.owners[owner.record.ID] == owner && owner.returned && !owner.settling && owner.record.sameOwner(candidate.record) {
				owner.settling = true
				candidate.proven = true
				reserved = append(reserved, owner)
				s.signalLocked()
			}
			s.mu.Unlock()
			continue
		}
		// A live/self/foreign-host/missing-claim/error result is not proof.
		candidate.proven = s.proof.ConfirmStopped(ctx, candidate.record.Process) == nil
		if err := ctx.Err(); err != nil {
			return ProjectCallOwnershipReport{}, err
		}
	}
	var remaining []projectCallRecord
	var more bool
	result = s.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := s.observationGate(ctx, tx, actor, cause, scope, f.Exclusive); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		for _, candidate := range candidates {
			if !candidate.proven {
				continue
			}
			current, err := loadProjectCall(ctx, x, candidate.record.ID)
			if err != nil {
				return err
			}
			if current == nil {
				if candidate.local != nil && candidate.local.registration.State() == f.Unknown {
					continue
				}
				return fault(f.InvalidState)
			}
			if !candidate.record.sameOwner(*current) {
				return fault(f.InvalidState)
			}
			if current.Phase == "running" {
				if err := joinProjectCall(ctx, x, *current); err != nil {
					return err
				}
			}
		}
		// Re-read the current running set, including rows admitted after capture.
		remaining, more, err = listProjectCalls(ctx, x, scope.ProjectID, cause.Action == pc.Delete)
		return err
	})
	if err := commitError(result); err != nil {
		s.mu.Lock()
		for _, owner := range reserved {
			owner.checkpoint = result
			owner.err = err
		}
		s.mu.Unlock()
		return ProjectCallOwnershipReport{}, err
	}
	// No SQL follows. Service -> Directory -> ownership is the only nested
	// mutex order; none of the repository paths hold ownership while taking a
	// callSet mutex. Original checkpoint/control actual returns still count.
	a, b := s.service.state.calls, s.directory.state.calls
	a.mu.Lock()
	b.mu.Lock()
	s.mu.Lock()
	defer a.mu.Unlock()
	defer b.mu.Unlock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		for _, owner := range reserved {
			owner.checkpoint, owner.err = result, err
		}
		return ProjectCallOwnershipReport{}, err
	}
	for _, owner := range reserved {
		if s.owners[owner.record.ID] == owner {
			delete(s.owners, owner.record.ID)
		}
	}
	pending := make(map[f.ID[projectCallIdentity]]bool, len(remaining))
	for _, r := range remaining {
		pending[r.ID] = true
	}
	localCalls := make(map[*ownedCall]bool)
	for _, owner := range s.owners {
		if selectedProjectRecord(owner.record, cause, scope) {
			pending[owner.record.ID] = true
			localCalls[owner.call] = true
		}
	}
	count := len(pending)
	if more {
		count++
	}
	for _, set := range []*callSet{a, b} {
		for call := range set.active {
			if selectedProjectCall(call, cause, scope) && !localCalls[call] {
				count++
			}
		}
	}
	details := ProjectCallOwnershipDetails{ProjectID: scope.ProjectID, OperationID: cause.OperationID, Action: cause.Action, ProjectVersion: cause.ProjectVersion, PendingCalls: count, HasMore: more, RegisteredJoined: count == 0 && !more}
	return ProjectCallOwnershipReport{data: func() ProjectCallOwnershipDetails { return details }}, nil
}

func (s *projectCallOwnershipState) observationGate(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, mode f.LockMode) error {
	if err := s.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(scope.ProjectID, mode)}); err != nil {
		return portError(err)
	}
	if err := s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
		return portError(err)
	}
	return portError(s.service.state.deps.Authority.state.projects.ValidateLifecycleInTx(ctx, tx, actor, cause, pc.SkillsParticipant, pc.StopPhase))
}
