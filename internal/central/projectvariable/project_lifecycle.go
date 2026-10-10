package projectvariable

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// ProjectCallStopper owns only the original calls of these two service instances.
// It is not a ProjectLifecycleParticipant: foreign-process join, Variables
// cleanup and the other enabled agent-skills-variables providers remain required.
type ProjectCallStopper struct {
	data func() (*Service, *SecretService)
}

func NewProjectCallStopper(ordinary *Service, secrets *SecretService) (*ProjectCallStopper, error) {
	a, b := ordinary.state(), secrets.state()
	if a == nil || b == nil || !sameStore(a.store, b.store) || a.deps.Authority.state() == nil ||
		a.deps.Authority != b.deps.Authority || !sameStore(a.store, a.deps.Authority.state().store) ||
		nilPort(a.deps.Projects) || nilPort(b.deps.Projects) || !reflect.TypeOf(a.deps.Projects).Comparable() ||
		reflect.TypeOf(a.deps.Projects) != reflect.TypeOf(b.deps.Projects) || a.deps.Projects != b.deps.Projects {
		return nil, fault(f.DependencyUnbound)
	}
	return &ProjectCallStopper{data: func() (*Service, *SecretService) { return ordinary, secrets }}, nil
}

// LocalJoined describes a snapshot of this provider, never a durable or foreign
// process proof. The zero report is invalid and cannot authorize a transition.
type ProjectCallStopDetails struct {
	ProjectID      c.ProjectID
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
	if !r.Valid() || pc.RequireProjectScope(scope) != nil || cause.Validate() != nil {
		return false
	}
	d := r.Details()
	return d.ProjectID == scope.ProjectID && d.OperationID == cause.OperationID && d.Action == cause.Action && d.ProjectVersion == cause.ProjectVersion
}
func (ProjectCallStopReport) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_variable_local_stop_report")
}
func (ProjectCallStopReport) LogValue() slog.Value {
	return slog.StringValue("project_variable_local_stop_report")
}
func (ProjectCallStopReport) MarshalJSON() ([]byte, error) {
	return []byte(`"project_variable_local_stop_report"`), nil
}
func (*ProjectCallStopReport) UnmarshalJSON([]byte) error { return fault(f.InvalidArgument) }

// RequestStop requests cancellation only after the exact current authorization
// transaction is confirmed committed. Cancellation is not call retirement.
func (s *ProjectCallStopper) RequestStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ProjectCallStopReport, error) {
	return s.inspect(ctx, actor, cause, scope, true)
}

// InspectStop never cancels work. Like RequestStop it is valid only while the
// canonical operation is stopping with a nonfailed Skills participant.
func (s *ProjectCallStopper) InspectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ProjectCallStopReport, error) {
	return s.inspect(ctx, actor, cause, scope, false)
}

func projectStopArguments(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx == nil || actor.Validate() != nil || cause.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := pc.RequireProjectScope(scope); err != nil {
		return err
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != scope.ProjectID.String() || d.CauseRef != cause.OperationID.String() {
		return fault(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
	}
	return nil
}

func selectedProjectCall(entry *call, cause pc.LifecycleCause, scope pc.ScopeRef) bool {
	return entry.project == scope.ProjectID && (entry.kind == mutationCall || cause.Action == pc.Delete && entry.kind == readCall)
}

func (s *ProjectCallStopper) inspect(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, cancelCalls bool) (ProjectCallStopReport, error) {
	if err := projectStopArguments(ctx, actor, cause, scope); err != nil {
		return ProjectCallStopReport{}, err
	}
	if s == nil || s.data == nil {
		return ProjectCallStopReport{}, fault(f.DependencyUnbound)
	}
	ordinary, secrets := s.data()
	// Both owners must retain this control operation through its original Tx
	// return. A concurrent process shutdown must not overtake the provider.
	ctx, _, ordinaryDone, err := ordinary.begin(ctx)
	if err != nil {
		return ProjectCallStopReport{}, err
	}
	defer ordinaryDone()
	ctx, _, secretsDone, err := secrets.begin(ctx)
	if err != nil {
		return ProjectCallStopReport{}, err
	}
	defer secretsDone()
	a, b := ordinary.state(), secrets.state()
	txCause, err := f.NewRecoveryCause("projectvariable-stop", cause.OperationID.String(), scope.ProjectID.String())
	if err != nil {
		return ProjectCallStopReport{}, portError(err)
	}
	var ordinaryCalls, secretCalls []*call
	result := a.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(scope.ProjectID, f.Shared)}); err != nil {
			return portError(err)
		}
		if _, err := a.store.InTx(tx); err != nil {
			return portError(err)
		}
		if err := a.deps.Projects.ValidateLifecycleInTx(ctx, tx, actor, cause, pc.SkillsParticipant, pc.StopPhase); err != nil {
			return portError(err)
		}
		// Always ordinary then Secret. No mutex survives callback return/COMMIT.
		a.mu.Lock()
		b.mu.Lock()
		defer a.mu.Unlock()
		defer b.mu.Unlock()
		for entry := range a.calls {
			if selectedProjectCall(entry, cause, scope) {
				ordinaryCalls = append(ordinaryCalls, entry)
			}
		}
		for entry := range b.calls {
			if selectedProjectCall(entry, cause, scope) {
				secretCalls = append(secretCalls, entry)
			}
		}
		return nil
	})
	if err := txError(result); err != nil {
		return ProjectCallStopReport{}, err
	}
	// This is the caller context, not Store's callback context (which is
	// normally canceled when the original transaction retires).
	a.mu.Lock()
	b.mu.Lock()
	defer a.mu.Unlock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ProjectCallStopReport{}, canceled(err)
	}
	finishProjectStop(a.calls, ordinaryCalls, cancelCalls)
	finishProjectStop(b.calls, secretCalls, cancelCalls)
	// Newly admitted calls are not retroactively canceled by the old snapshot,
	// but must prevent a claim that this instance has no pending calls.
	pending := 0
	for entry := range a.calls {
		if selectedProjectCall(entry, cause, scope) {
			pending++
		}
	}
	for entry := range b.calls {
		if selectedProjectCall(entry, cause, scope) {
			pending++
		}
	}
	details := ProjectCallStopDetails{ProjectID: scope.ProjectID, OperationID: cause.OperationID, Action: cause.Action, ProjectVersion: cause.ProjectVersion, PendingCalls: pending, LocalJoined: pending == 0}
	return ProjectCallStopReport{data: func() ProjectCallStopDetails { return details }}, nil
}

// Called only under the original owner's mutex, also used to register detached
// confirmation contexts. The flag covers confirmation registration after Stop.
func finishProjectStop(current map[*call]struct{}, captured []*call, cancelCalls bool) {
	for _, entry := range captured {
		if _, alive := current[entry]; !alive {
			continue
		}
		if cancelCalls {
			entry.stopRequested = true
			entry.cancel()
			for confirmation := range entry.confirmations {
				confirmation.cancel()
			}
		}
	}
}
