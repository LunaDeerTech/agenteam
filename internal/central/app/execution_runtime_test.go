package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestExecutionRuntimeCatalogAndRequiredBindings(t *testing.T) {
	catalog := event.NewCatalog()
	old, err := defineWorkPlanningEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	added, err := defineExecutionRuntimeEvents(catalog)
	if err != nil || !old.structure.Valid() || !old.tasks.Valid() || !old.transitions.Valid() || !added.lifecycle.Valid() || !added.claim.Valid() || !added.busy.Valid() || !added.failure.Valid() {
		t.Fatal("original Work and Execution schemas cannot share the production catalog", err)
	}
	if _, err = createExecutionRuntimeAuthorities(nil, nil, nil); err == nil {
		t.Fatal("missing original Store/Project/guard admitted")
	}
	if _, err = createAgentRuntimeProviders(nil, nil, nil, nil, nil); err == nil {
		t.Fatal("missing real capture providers admitted")
	}
	// Absence must not acquire even a wire budget or a background lifetime.
	b := &executionRuntimeAssembly{constructing: true}
	err = constructExecutionRuntime(b, config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, executionRuntimeEventSet{})
	if err == nil || len(b.owners) != 0 || len(b.runs) != 0 || b.constructing {
		t.Fatal("disabled/unbound construction started a partial runtime")
	}
	if err = b.Start(context.Background()); err == nil {
		t.Fatal("partial graph started")
	}
	if err = b.Drain(context.Background()); err != nil || !b.Joined() {
		t.Fatal("empty partial graph did not retire", err)
	}
}

type runtimeLifecycleProbe struct {
	name            string
	log             *[]string
	stopped, joined bool
	err             error
}

func (p *runtimeLifecycleProbe) Stop() { p.stopped = true }
func (p *runtimeLifecycleProbe) Drain(context.Context) error {
	*p.log = append(*p.log, p.name)
	if p.err != nil {
		return p.err
	}
	p.joined = p.stopped
	return nil
}
func (p *runtimeLifecycleProbe) Joined() bool { return p.stopped && p.joined }

type runtimeGuardProbe struct{ forced int }

func (*runtimeGuardProbe) StartMaintenance(context.Context) error { return nil }
func (*runtimeGuardProbe) Check(context.Context) error            { return nil }
func (*runtimeGuardProbe) StopAdmission()                         {}
func (*runtimeGuardProbe) Drain(context.Context) error            { return nil }
func (p *runtimeGuardProbe) Force(context.Context) error          { p.forced++; return nil }

type runtimeExternalProbe struct {
	stopped bool
	forced  int
}

func (p *runtimeExternalProbe) StopAdmission()              { p.stopped = true }
func (p *runtimeExternalProbe) Drain(context.Context) error { return nil }
func (p *runtimeExternalProbe) Force(context.Context) error { p.forced++; return nil }
func (p *runtimeExternalProbe) Joined() bool                { return p.stopped }

func TestExecutionRuntimeLifecycleRetainsOriginalOwner(t *testing.T) {
	var order []string
	original := f.NewFault(f.CommitUnknown, f.Unknown)
	provider := &runtimeLifecycleProbe{name: "model-provider", log: &order}
	driver := &runtimeLifecycleProbe{name: "original-driver", log: &order, err: original}
	manager := &runtimeLifecycleProbe{name: "manager", log: &order}
	b := &executionRuntimeAssembly{}
	b.add(runtimeWork(provider))
	b.add(runtimeWork(driver))
	b.add(runtimeWork(manager))
	shared := &runtimeExternalProbe{}
	accounts := &accountAssembly{executions: b, skills: shared}
	root := &resources{stopping: true, accountService: accounts}
	guard := &runtimeGuardProbe{}
	err := accounts.Drain(context.Background())
	if !errors.Is(err, original) || b.Joined() || accounts.Joined() || root.producersJoined() || provider.joined || !reflect.DeepEqual(order, []string{"manager", "original-driver"}) {
		t.Fatal("unobserved original Unknown released a provider or lost its cause", err, order)
	}
	if err = accounts.Force(context.Background()); !errors.Is(err, original) || shared.forced != 0 {
		t.Fatal("force retired a shared provider beneath the original retained Runtime", err)
	}
	root.forceObjects(context.Background(), guard)
	if guard.forced != 0 {
		t.Fatal("root retired guard while Execution owner was retained")
	}
	// This controlled seam tests only ownership order. It does not claim a SQL
	// observation or manufacture a successful Execution/lease retirement.
	driver.err = nil
	order = nil
	if err = accounts.Drain(context.Background()); err != nil || !root.producersJoined() || !reflect.DeepEqual(order, []string{"manager", "original-driver", "model-provider"}) {
		t.Fatal("joined original owner did not release providers in order", err, order)
	}
	root.forceObjects(context.Background(), guard)
	if guard.forced != 1 {
		t.Fatal("joint predicate did not permit the original guard retirement")
	}
}

func TestExecutionRuntimePartialConstructionAndPhysicalRunTail(t *testing.T) {
	var order []string
	b := &executionRuntimeAssembly{constructing: true}
	b.StopAdmission()
	late := &runtimeLifecycleProbe{name: "late", log: &order}
	b.add(runtimeWork(late))
	if !late.stopped || b.Joined() || b.Drain(context.Background()) == nil {
		t.Fatal("late acquisition escaped original partial construction ownership")
	}
	b.constructionDone()
	if err := b.Drain(context.Background()); err != nil || !b.Joined() {
		t.Fatal(err)
	}
	if b.startRun(func(context.Context) error { t.Error("stopped runtime dispatched"); return nil }) != nil {
		t.Fatal("stopped runtime admitted a new lifetime")
	}

	live := &executionRuntimeAssembly{}
	cancelled, returned := make(chan struct{}), make(chan struct{})
	run := live.startRun(func(ctx context.Context) error {
		<-ctx.Done()
		close(cancelled)
		<-returned
		return ctx.Err()
	})
	live.StopAdmission()
	<-cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := live.Drain(ctx); !errors.Is(err, context.Canceled) || live.Joined() {
		close(returned)
		<-run.done
		t.Fatal("context cancellation was treated as physical Run return", err)
	}
	close(returned)
	<-run.done
	if err := live.Drain(context.Background()); err != nil || !live.Joined() {
		t.Fatal("original physically returned lifetime was not joined", err)
	}
}
