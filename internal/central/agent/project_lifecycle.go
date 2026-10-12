package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ProjectCallStopper owns only the calls of these exact local instances. It is
// not a ProjectLifecycleParticipant or evidence of foreign process retirement.
type ProjectCallStopper struct {
	data func() (*Service, *DirectoryReader)
}

func NewProjectCallStopper(service *Service, directory *DirectoryReader) (*ProjectCallStopper, error) {
	if service == nil || service.state == nil || directory == nil || directory.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	a, b := service.state, directory.state
	if !sameProjectCallStore(a.store, b.store) || a.deps.Authority == nil || a.deps.Authority.state == nil ||
		a.deps.Authority != b.authority || !sameProjectCallStore(a.store, a.deps.Authority.state.store) ||
		nilPort(a.deps.Authority.state.projects) || a.calls == nil || b.calls == nil || a.calls == b.calls ||
		a.calls.active == nil || b.calls.active == nil || a.calls.drained == nil || b.calls.drained == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectCallStopper{data: func() (*Service, *DirectoryReader) { return service, directory }}, nil
}

func sameProjectCallStore(a, b Store) bool {
	return !nilPort(a) && !nilPort(b) && reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.TypeOf(a).Comparable() && a == b
}

// LocalJoined is a snapshot of the two local owners, not a persisted lifecycle
// result, permission to clean references, or a global participant completion.
type ProjectCallStopDetails struct {
	ProjectID      i.ProjectID
	OperationID    pc.OperationID
	Action         pc.LifecycleAction
	ProjectVersion f.Version
	PendingCalls   int
	LocalJoined    bool
}
type ProjectCallStopReport struct{ data func() ProjectCallStopDetails }

func (r ProjectCallStopReport) Valid() bool { return r.data != nil }
func (r ProjectCallStopReport) Details() ProjectCallStopDetails {
	if r.data == nil {
		return ProjectCallStopDetails{}
	}
	return r.data()
}
func (r ProjectCallStopReport) Matches(cause pc.LifecycleCause, scope pc.ScopeRef) bool {
	if !r.Valid() || cause.Validate() != nil || pc.RequireProjectScope(scope) != nil {
		return false
	}
	d := r.Details()
	return d.ProjectID == scope.ProjectID && d.OperationID == cause.OperationID && d.Action == cause.Action && d.ProjectVersion == cause.ProjectVersion
}
func (ProjectCallStopReport) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "agent_project_local_stop_report")
}
func (ProjectCallStopReport) LogValue() slog.Value {
	return slog.StringValue("agent_project_local_stop_report")
}
func (ProjectCallStopReport) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_project_local_stop_report"`), nil
}
func (*ProjectCallStopReport) UnmarshalJSON([]byte) error { return invalid() }

// RequestStop cancels captured original calls only after the current exact
// lifecycle authorization transaction has actually returned Committed.
func (s *ProjectCallStopper) RequestStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ProjectCallStopReport, error) {
	return s.inspect(ctx, actor, cause, scope, true)
}

// InspectStop never cancels work. This local provider uses the same current
// stopping-phase authorization as RequestStop; it does not accept old receipts.
func (s *ProjectCallStopper) InspectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ProjectCallStopReport, error) {
	return s.inspect(ctx, actor, cause, scope, false)
}

func projectCallStopArguments(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx == nil || actor.Validate() != nil || cause.Validate() != nil {
		return invalid()
	}
	if err := pc.RequireProjectScope(scope); err != nil {
		return err
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != scope.ProjectID.String() || d.CauseRef != cause.OperationID.String() {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

func selectedProjectCall(entry *ownedCall, cause pc.LifecycleCause, scope pc.ScopeRef) bool {
	return entry.project == scope.ProjectID && (entry.kind == mutationCall || cause.Action == pc.Delete && entry.kind == readCall)
}

func (s *ProjectCallStopper) inspect(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, cancelCalls bool) (ProjectCallStopReport, error) {
	if err := projectCallStopArguments(ctx, actor, cause, scope); err != nil {
		return ProjectCallStopReport{}, err
	}
	if s == nil || s.data == nil {
		return ProjectCallStopReport{}, fault(f.DependencyUnbound)
	}
	service, directory := s.data()
	// Both original owners retain this control operation until its transaction
	// and function actually return, including during process-wide Stop/Drain.
	ctx, serviceDone, err := service.begin(ctx)
	if err != nil {
		return ProjectCallStopReport{}, err
	}
	defer serviceDone()
	ctx, directoryDone, err := directory.state.calls.begin(ctx)
	if err != nil {
		return ProjectCallStopReport{}, err
	}
	defer directoryDone()
	a, b := service.state, directory.state
	txCause, err := f.NewRecoveryCause("agent-project-stop", cause.OperationID.String(), scope.ProjectID.String())
	if err != nil {
		return ProjectCallStopReport{}, portError(err)
	}
	var serviceCalls, directoryCalls []*ownedCall
	result := a.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(scope.ProjectID, f.Shared)}); err != nil {
			return portError(err)
		}
		if _, err := a.store.InTx(tx); err != nil {
			return portError(err)
		}
		if err := a.deps.Authority.state.projects.ValidateLifecycleInTx(ctx, tx, actor, cause, pc.SkillsParticipant, pc.StopPhase); err != nil {
			return portError(err)
		}
		// Always Service then DirectoryReader. No mutex survives this callback.
		a.calls.mu.Lock()
		b.calls.mu.Lock()
		defer a.calls.mu.Unlock()
		defer b.calls.mu.Unlock()
		for entry := range a.calls.active {
			if selectedProjectCall(entry, cause, scope) {
				serviceCalls = append(serviceCalls, entry)
			}
		}
		for entry := range b.calls.active {
			if selectedProjectCall(entry, cause, scope) {
				directoryCalls = append(directoryCalls, entry)
			}
		}
		return nil
	})
	if err := commitError(result); err != nil {
		return ProjectCallStopReport{}, err
	}
	// Use the original control context, not the retired Store callback context.
	a.calls.mu.Lock()
	b.calls.mu.Lock()
	defer a.calls.mu.Unlock()
	defer b.calls.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ProjectCallStopReport{}, err
	}
	if cancelCalls {
		cancelCapturedProjectCalls(a.calls, serviceCalls)
		cancelCapturedProjectCalls(b.calls, directoryCalls)
	}
	// Calls admitted after the authorized snapshot are counted, but the old
	// snapshot cannot retroactively authorize cancelling these new identities.
	pending := 0
	for _, calls := range []*callSet{a.calls, b.calls} {
		for entry := range calls.active {
			if selectedProjectCall(entry, cause, scope) {
				pending++
			}
		}
	}
	details := ProjectCallStopDetails{ProjectID: scope.ProjectID, OperationID: cause.OperationID, Action: cause.Action, ProjectVersion: cause.ProjectVersion, PendingCalls: pending, LocalJoined: pending == 0}
	return ProjectCallStopReport{data: func() ProjectCallStopDetails { return details }}, nil
}

// The caller holds this owner's mutex, which also protects confirmation
// admission. A later confirmation cannot escape its original stop request.
func cancelCapturedProjectCalls(calls *callSet, captured []*ownedCall) {
	for _, entry := range captured {
		if _, alive := calls.active[entry]; !alive {
			continue
		}
		entry.stopRequested = true
		entry.cancel(nil)
		for confirmation := range entry.confirmations {
			confirmation.cancel(nil)
		}
	}
}
