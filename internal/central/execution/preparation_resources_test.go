package execution

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// These providers control domain facts and tentative reference counts. They
// exercise the real driver, claim repository, private proofs and rollback;
// they do not establish real Skill/Registry publication or a complete input.
type resourceCaptureControl struct {
	p                    *preparationControl
	locks                []f.LockRequest
	order                []string
	skillCtx             context.Context
	skillRequest         sc.SkillCaptureRequest
	lastCaptureCtx       context.Context
	lastCaptureTx        f.Tx
	mutateAfterDiscovery func()
	entered, release     chan struct{}
	reject               string
}
type resourcePlanControl struct {
	owner   *resourceCaptureControl
	kind    string
	binding f.Digest
	locks   []f.LockRequest
}

func (p *resourcePlanControl) RequiredLocks() []f.LockRequest {
	return append([]f.LockRequest(nil), p.locks...)
}

func newResourceCaptureControl(t *testing.T) *resourceCaptureControl {
	t.Helper()
	p := newPreparationControl(t)
	skill, _ := f.AggregateLock(f.SkillAggregate, newTestID[struct{}](t).String())
	tool, _ := f.AggregateLock(f.ToolSpecAggregate, newTestID[i.Tool](t).String())
	r := &resourceCaptureControl{p: p, locks: []f.LockRequest{{Key: skill, Mode: f.Shared}, {Key: tool, Mode: f.Shared}}}
	p.driver.state.skills, p.driver.state.tools = r, r
	return r
}

func (r *resourceCaptureControl) discovery(ctx context.Context, kind string, fn func(context.Context, f.Tx) (f.Digest, error)) (*resourcePlanControl, error) {
	r.order = append(r.order, kind+"-discover")
	if r.entered != nil && kind == "skill" {
		close(r.entered)
		<-r.release
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := r.p
	locks, err := oc.NormalizeLocks(append(preparationDiscoveryLocks(p.project.ID, p.launch.request.AgentID, p.execution), r.locks...))
	if err != nil {
		return nil, err
	}
	cause, err := f.NewJobCause("capture-control", p.execution.String(), newTestID[struct{}](p.launch.t).String())
	if err != nil {
		return nil, err
	}
	var binding f.Digest
	result := p.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := p.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		var e error
		binding, e = fn(ctx, tx)
		return e
	})
	if err = commitError(result); err != nil {
		return nil, err
	}
	return &resourcePlanControl{owner: r, kind: kind, binding: binding, locks: locks}, nil
}

func (r *resourceCaptureControl) DiscoverInitialBindings(ctx context.Context, request sc.SkillCaptureRequest) (sc.InitialBindingsPlan, error) {
	r.skillCtx, r.skillRequest = ctx, request
	return r.discovery(ctx, "skill", func(ctx context.Context, tx f.Tx) (f.Digest, error) {
		a, t := r.p.launch.authority, r.p.launch.t
		_, err := a.RequireSkillCaptureDiscoveryInTx(context.Background(), tx, request)
		requireCode(t, err, f.Forbidden)
		_, err = a.RequireSkillCaptureInTx(ctx, tx, request)
		requireCode(t, err, f.Forbidden)
		other := request
		other.AgentID = newTestID[i.Agent](t)
		_, err = a.RequireSkillCaptureDiscoveryInTx(ctx, tx, other)
		requireCode(t, err, f.Forbidden)
		_, err = a.RequireSkillCaptureDiscoveryInTx(ctx, f.NewTx(), request)
		requireCode(t, err, f.Forbidden)
		locks := r.p.store.locks
		r.p.store.locks = nil
		_, err = a.RequireSkillCaptureDiscoveryInTx(ctx, tx, request)
		requireCode(t, err, f.Forbidden)
		r.p.store.locks = locks
		scope, err := a.RequireSkillCaptureDiscoveryInTx(ctx, tx, request)
		return scope.AttemptBinding, err
	})
}

func (r *resourceCaptureControl) DiscoverExecutionTools(ctx context.Context, request tc.ExecutionToolCaptureRequest) (tc.ExecutionToolPlan, error) {
	plan, err := r.discovery(ctx, "tool", func(ctx context.Context, tx f.Tx) (f.Digest, error) {
		a, t := r.p.launch.authority, r.p.launch.t
		// A saved planning context expires at its original provider return,
		// even while the enclosing Run and a new real transaction are live.
		_, err := a.RequireSkillCaptureDiscoveryInTx(r.skillCtx, tx, r.skillRequest)
		requireCode(t, err, f.Forbidden)
		_, err = a.RequireToolCaptureInTx(ctx, tx, request)
		requireCode(t, err, f.Forbidden)
		facts, err := a.RequireToolDiscoveryInTx(ctx, tx, request)
		if err != nil {
			return "", err
		}
		if err = facts.Validate(request); err != nil {
			return "", err
		}
		if !reflect.DeepEqual(facts.DeniedToolIDs, r.p.launch.request.Policy.DeniedToolIDs) {
			t.Fatal("discovery replaced original policy")
		}
		return facts.AttemptBinding, nil
	})
	if err == nil && r.mutateAfterDiscovery != nil {
		r.mutateAfterDiscovery()
	}
	return plan, err
}

func (r *resourceCaptureControl) requirePlan(ctx context.Context, tx f.Tx, plan any, kind string, binding f.Digest) error {
	p, ok := plan.(*resourcePlanControl)
	if !ok || p.owner != r || p.kind != kind || p.binding != binding {
		return fault(f.ConfirmationStale)
	}
	if err := r.p.store.RequireHeldLocks(ctx, tx, r.locks); err != nil {
		return err
	}
	if r.p.captures != 1 || r.p.launch.captures != 1 {
		r.p.launch.t.Fatal("resource proof preceded actual Trigger/Agent return")
	}
	r.order = append(r.order, kind+"-capture")
	r.lastCaptureCtx, r.lastCaptureTx = ctx, tx
	if r.reject == kind {
		return fault(f.ConfirmationStale)
	}
	r.p.store.providerRefs++
	return nil
}

func (r *resourceCaptureControl) ResolveInitialBindingsInTx(ctx context.Context, tx f.Tx, request sc.SkillCaptureRequest, plan sc.InitialBindingsPlan) (sc.InitialSkillBindings, error) {
	a := r.p.launch.authority
	scope, err := a.RequireSkillCaptureInTx(ctx, tx, request)
	if err != nil {
		return sc.InitialSkillBindings{}, err
	}
	_, err = a.RequireSkillCaptureDiscoveryInTx(ctx, tx, request)
	requireCode(r.p.launch.t, err, f.Forbidden)
	if err = r.requirePlan(ctx, tx, plan, "skill", scope.AttemptBinding); err != nil {
		return sc.InitialSkillBindings{}, err
	}
	return sc.InitialSkillBindings{Request: request, AssignmentSequence: 1, Bindings: []sc.SkillBinding{}}, nil
}

func (r *resourceCaptureControl) ResolveExecutionToolsInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest, plan tc.ExecutionToolPlan) ([]tc.ExecutionTool, error) {
	a, t := r.p.launch.authority, r.p.launch.t
	facts, err := a.RequireToolCaptureInTx(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	if err = facts.Validate(request); err != nil {
		return nil, err
	}
	config := r.p.launch.config.Fields()
	if facts.AgentVersion != config.Core.Version || !reflect.DeepEqual(facts.Selection.AllowedToolIDs, config.AllowedToolIDs) || !reflect.DeepEqual(facts.Selection.DeniedToolIDs, r.p.launch.request.Policy.DeniedToolIDs) {
		t.Fatal("capture did not project actual Agent and original policy")
	}
	if len(facts.Selection.AllowedToolIDs) > 0 {
		original := facts.Selection.AllowedToolIDs[0]
		facts.Selection.AllowedToolIDs[0] = newTestID[i.Tool](t)
		again, e := a.RequireToolCaptureInTx(ctx, tx, request)
		if e != nil || again.Selection.AllowedToolIDs[0] != original {
			t.Fatal("returned selection changed private capture")
		}
	}
	if err = r.requirePlan(ctx, tx, plan, "tool", facts.AttemptBinding); err != nil {
		return nil, err
	}
	return []tc.ExecutionTool{}, nil
}

func TestExecutionPreparationResourceCaptureOriginalTransaction(t *testing.T) {
	r := newResourceCaptureControl(t)
	fields := r.p.launch.config.Fields()
	fields.AllowedToolIDs = []i.ToolID{newTestID[i.Tool](t)}
	var err error
	r.p.launch.config, err = ac.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	requireCode(t, r.p.driver.Run(context.Background(), r.p.execution), f.DependencyUnbound)
	if !reflect.DeepEqual(r.order, []string{"skill-discover", "tool-discover", "skill-capture", "tool-capture"}) || r.p.store.providerRefs != 0 {
		t.Fatal("full-union capture order or atomic rollback changed")
	}
	_, err = r.p.launch.authority.RequireSkillCaptureInTx(r.lastCaptureCtx, r.lastCaptureTx, r.skillRequest)
	requireCode(t, err, f.Forbidden)
	row := r.p.store.rows[r.p.execution.String()]
	if row[3] != "preparing" || row[4] != int64(2) {
		t.Fatal("partial resource capture changed Execution state")
	}
}

func TestExecutionPreparationResourcePlansRejectChangedSourceAndClaim(t *testing.T) {
	for _, change := range []string{"project", "claim", "provider-rejected"} {
		t.Run(change, func(t *testing.T) {
			r := newResourceCaptureControl(t)
			switch change {
			case "project":
				r.mutateAfterDiscovery = func() { r.p.project.Version++ }
			case "claim":
				r.mutateAfterDiscovery = func() {
					claim := r.p.store.claims[r.p.execution.String()]
					claim.fence++
					r.p.store.claims[r.p.execution.String()] = claim
				}
			case "provider-rejected":
				r.reject = "tool"
			}
			requireCode(t, r.p.driver.Run(context.Background(), r.p.execution), f.ConfirmationStale)
			if r.p.store.providerRefs != 0 {
				t.Fatal("rejected complete capture retained tentative refs")
			}
		})
	}
}

func TestExecutionPreparationResourceDiscoveryStopJoinsOriginalCall(t *testing.T) {
	r := newResourceCaptureControl(t)
	r.entered, r.release = make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- r.p.driver.Run(context.Background(), r.p.execution) }()
	<-r.entered
	r.p.driver.Stop()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(r.p.driver.Drain(cancelled), context.Canceled) || r.p.driver.Joined() {
		t.Fatal("Stop retired an original discovery that has not returned")
	}
	close(r.release)
	if err := <-done; !errors.Is(err, context.Canceled) || !r.p.driver.Joined() {
		t.Fatal("discovery cancellation/checkpoint did not actually join", err)
	}
	if r.p.captures != 0 || r.p.launch.captures != 0 || r.p.store.providerRefs != 0 {
		t.Fatal("cancelled discovery entered final capture")
	}
}
