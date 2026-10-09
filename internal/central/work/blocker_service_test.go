package work

import (
	"context"
	"errors"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func pureBlockerPorts(t *testing.T) (*denialStore, BlockerDependencies) {
	t.Helper()
	store, authority, reader, _ := pureTaskPorts(t)
	factory, e := c.RegisterTaskBlockerEvents(event.NewCatalog())
	if e != nil {
		t.Fatal(e)
	}
	return store, BlockerDependencies{Authority: authority, Structure: reader, BlockerEvents: factory, Events: &denialEvents{}, Activity: &denialActivity{}}
}
func TestTaskBlockerConstructorsRequireExactBindings(t *testing.T) {
	store, deps := pureBlockerPorts(t)
	if _, e := NewBlocker(store, deps); e != nil {
		t.Fatal(e)
	}
	var missingStore *denialStore
	_, e := NewBlocker(missingStore, deps)
	pureCode(t, e, f.DependencyUnbound)
	for name, change := range map[string]func(*BlockerDependencies){"authority": func(d *BlockerDependencies) { d.Authority = nil }, "structure": func(d *BlockerDependencies) { d.Structure = nil }, "events": func(d *BlockerDependencies) { d.Events = nil }, "typed-events": func(d *BlockerDependencies) { var v *denialEvents; d.Events = v }, "activity": func(d *BlockerDependencies) { d.Activity = nil }, "typed-activity": func(d *BlockerDependencies) { var v *denialActivity; d.Activity = v }, "factory": func(d *BlockerDependencies) { d.BlockerEvents = c.TaskBlockerEvents{} }} {
		t.Run(name, func(t *testing.T) {
			d := deps
			change(&d)
			_, e := NewBlocker(store, d)
			pureCode(t, e, f.DependencyUnbound)
		})
	}
	other, otherDeps := pureBlockerPorts(t)
	_, e = NewBlocker(other, deps)
	pureCode(t, e, f.DependencyUnbound)
	bad := deps
	bad.Structure = otherDeps.Structure
	_, e = NewBlocker(store, bad)
	pureCode(t, e, f.DependencyUnbound)
	second, e := NewAuthority(store, deps.Authority.state().projects)
	if e != nil {
		t.Fatal(e)
	}
	secondReader, e := NewReader(store, second, pureKeys(t))
	if e != nil {
		t.Fatal(e)
	}
	bad = deps
	bad.Structure = secondReader
	_, e = NewBlocker(store, bad)
	pureCode(t, e, f.DependencyUnbound)
	if store.touches.Load() != 0 || other.touches.Load() != 0 {
		t.Fatal("constructor performed SQL")
	}
}
func TestTaskBlockerStopDrainAndPureAdmission(t *testing.T) {
	store, deps := pureBlockerPorts(t)
	service, e := NewBlocker(store, deps)
	if e != nil {
		t.Fatal(e)
	}
	ctx, entry, done, e := service.begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	confirm, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.state().mu.Lock()
	entry.confirmations[&confirmation{cancel: cancel}] = struct{}{}
	service.state().mu.Unlock()
	service.Stop()
	service.Stop()
	if ctx.Err() != context.Canceled || confirm.Err() != context.Canceled {
		t.Fatal("Stop missed call or confirmation")
	}
	cancelled, close := context.WithCancel(context.Background())
	close()
	if e = service.Drain(cancelled); !errors.Is(e, context.Canceled) {
		t.Fatal("Drain invented join")
	}
	_, _, _, e = service.begin(context.Background())
	pureCode(t, e, f.ShuttingDown)
	done()
	done()
	if e = service.Drain(context.Background()); e != nil {
		t.Fatal(e)
	}
	if store.touches.Load() != 0 {
		t.Fatal("lifecycle closed shared store")
	}
	service, e = NewBlocker(store, deps)
	if e != nil {
		t.Fatal(e)
	}
	defer service.Stop()
	r, a, _ := pureBlockerRecord(t, false)
	_, e = service.AddTaskBlocker(nil, a, f.CommandMeta{}, r.Project, r.Input.Target, *r.Input.Add)
	pureCode(t, e, f.InvalidArgument)
	_, e = service.AddTaskBlocker(context.Background(), a, f.CommandMeta{}, r.Project, r.Input.Target, *r.Input.Add)
	pureCode(t, e, f.InvalidArgument)
	_, e = service.ListTaskBlockers(context.Background(), a, r.Project, r.Input.Target, "")
	pureCode(t, e, f.InvalidArgument)
	if store.touches.Load() != 0 {
		t.Fatal("pure rejection performed SQL")
	}
}
