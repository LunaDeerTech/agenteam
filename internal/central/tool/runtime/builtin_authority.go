package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sync/atomic"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// BuiltinAuthority is constructed before domain Services and Registry. It has
// no mutable driver binding. Each actual executor lends its own original call
// privately for the duration of that synchronous domain invocation.
type BuiltinAuthority struct {
	store   Store
	guard   *object.ProcessGuard
	process oc.ProcessID
}

func NewBuiltinAuthority(store Store, guard *object.ProcessGuard, process oc.ProcessID) (*BuiltinAuthority, error) {
	if nilPort(store) || guard == nil || process.Validate() != nil {
		return nil, fail(f.DependencyUnbound)
	}
	return &BuiltinAuthority{store: store, guard: guard, process: process}, nil
}

type builtinWitnessKey struct{}
type builtinWitness struct {
	owner       *BuiltinExecutor
	call        *builtinCall
	active      atomic.Bool
	cleanupOnly atomic.Bool
}

func (a *BuiltinAuthority) witness(ctx context.Context, actor i.Actor, project i.ProjectID) (*builtinWitness, error) {
	if contextError(ctx) != nil || a == nil || actor.Validate() != nil || actor.Details().Kind != i.AgentRun || project.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(builtinWitnessKey{}).(*builtinWitness)
	if !ok || w == nil || !w.active.Load() || w.owner == nil || w.owner.authority != a || w.call == nil ||
		!w.call.binding.Actor.Equal(actor) || w.call.binding.ProjectID != project {
		return nil, fail(f.Forbidden)
	}
	if process, err := a.guard.CurrentProcess(); err != nil || process != a.process {
		return nil, fail(f.DependencyUnavailable)
	}
	return w, nil
}

func (a *BuiltinAuthority) withCurrent(ctx context.Context, tx f.Tx, w *builtinWitness, consume func(context.Context, []byte) error) error {
	if w == nil || !w.active.Load() || w.cleanupOnly.Load() || w.owner.authority != a || consume == nil {
		return fail(f.Forbidden)
	}
	if _, err := a.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, w.call.locks); err != nil {
		return portError(err)
	}
	if err := a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = requireBuiltinAttempt(ctx, x, a.process, w.call.record, w.call.cold); err != nil {
		return err
	}
	calls := 0
	loan := w.owner.executions.WithBuiltinToolCallInTx
	if w.call.lookup {
		committed, ok := w.owner.executions.(tc.CommittedBuiltinInputAuthority)
		if !ok {
			return fail(f.DependencyUnbound)
		}
		loan = committed.WithCommittedBuiltinToolCallInTx
	}
	err = loan(ctx, tx, w.call.binding, w.call.plan, func(current context.Context, b tc.ToolCallBinding, raw []byte) error {
		calls++
		if calls != 1 || !sameRunnerCallBinding(b, w.call.binding) || !bytes.Equal(raw, w.call.arguments) || !w.active.Load() {
			return fail(f.Forbidden)
		}
		return consume(current, raw)
	})
	if err != nil {
		return err
	}
	if calls != 1 {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}

func (a *BuiltinAuthority) DiscoverTaskTool(ctx context.Context, actor i.Actor, project i.ProjectID, task, intent string) ([]f.LockRequest, error) {
	w, err := a.witness(ctx, actor, project)
	if err != nil {
		return nil, err
	}
	if err = checkBuiltinTask(w.call, task, intent); err != nil {
		return nil, err
	}
	return slices.Clone(w.call.locks), nil
}

func (a *BuiltinAuthority) RequireTaskToolInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, task, intent string) error {
	w, err := a.witness(ctx, actor, project)
	if err != nil {
		return err
	}
	if err = checkBuiltinTask(w.call, task, intent); err != nil {
		return err
	}
	return a.withCurrent(ctx, tx, w, func(context.Context, []byte) error { return nil })
}

func checkBuiltinTask(call *builtinCall, task, intent string) error {
	if _, err := f.ParseID[struct{}](task); err != nil {
		return fail(f.InvalidArgument)
	}
	want := ""
	switch intent {
	case "read":
		want = "task.read"
	case "transfer", "lookup-transfer":
		want = "task.transfer"
	}
	if call == nil || want == "" || call.binding.Binding.HandlerID != want {
		return fail(f.Forbidden)
	}
	if call.lookup && intent != "lookup-transfer" {
		return fail(f.Forbidden)
	}
	var raw struct {
		TaskID string `json:"task_id"`
	}
	if json.Unmarshal(call.arguments, &raw) != nil || raw.TaskID != task {
		return fail(f.Forbidden)
	}
	return nil
}

func (a *BuiltinAuthority) artifactWitness(ctx context.Context, subject ac.AccessSubject) (*builtinWitness, error) {
	if subject.Validate() != nil || subject.Maintenance {
		return nil, fail(f.Forbidden)
	}
	w, err := a.witness(ctx, subject.Actor, subject.ProjectID)
	if err != nil {
		return nil, err
	}
	b := w.call.binding
	switch b.Binding.HandlerID {
	case "artifact.list", "artifact.create", "artifact.read":
	default:
		return nil, fail(f.Forbidden)
	}
	if subject.ExecutionID != b.ExecutionID.String() ||
		subject.OperationID != "" && subject.OperationID != w.call.record.ID.String() ||
		subject.ToolID != "" && subject.ToolID != b.Spec.ToolID.String() ||
		subject.ToolCallID != "" && subject.ToolCallID != b.Input.PayloadID {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

func (a *BuiltinAuthority) DiscoverArtifactRun(ctx context.Context, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	w, err := a.artifactWitness(ctx, subject)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	raw, err := encode(struct {
		Input     f.Digest
		Operation tc.OperationID
		Attempt   tc.AttemptID
	}{w.call.record.InputDigest, w.call.record.ID, w.call.record.Attempt})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest(raw), w.call.locks)
}

func (a *BuiltinAuthority) DiscoverArtifactRunInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	w, err := a.artifactWitness(ctx, subject)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if err = a.withCurrent(ctx, tx, w, func(context.Context, []byte) error { return nil }); err != nil {
		return oc.AccessDependencies{}, err
	}
	return a.DiscoverArtifactRun(ctx, subject)
}

func (a *BuiltinAuthority) RequireArtifactRunInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject, intent i.AccessIntent) error {
	w, err := a.artifactWitness(ctx, subject)
	if err != nil {
		return err
	}
	if intent != i.Read && intent != i.Mutate && intent != i.Converge {
		return fail(f.Forbidden)
	}
	if intent != i.Read && w.call.binding.Binding.HandlerID != "artifact.create" {
		return fail(f.Forbidden)
	}
	if w.call.lookup && intent != i.Read {
		return fail(f.Forbidden)
	}
	return a.withCurrent(ctx, tx, w, func(context.Context, []byte) error { return nil })
}
