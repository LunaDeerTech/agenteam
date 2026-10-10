package skill

import (
	"context"
	"errors"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type constructorObjects struct{ ObjectPorts }
type constructorProcesses struct{ oc.ProcessAuthority }

func testSkillService(t *testing.T) *Service {
	t.Helper()
	a, e := NewAuthority(&initializationGateStore{}, &initializationGateProjects{})
	if e != nil {
		t.Fatal(e)
	}
	bundle, e := AddSkills(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(Dependencies{Authority: a, Objects: &constructorObjects{}, Processes: &constructorProcesses{}, ProcessID: stateID[oc.Process](50), Bundle: bundle})
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestSkillServiceNoUnboundOrLateDependencyInstall(t *testing.T) {
	good := testSkillService(t).state()
	base := Dependencies{Authority: good.authority, Objects: good.objects, Processes: good.processes, ProcessID: good.process, Bundle: good.bundle}
	for _, edit := range []func(*Dependencies){func(d *Dependencies) { d.Authority = nil }, func(d *Dependencies) { d.Objects = (*constructorObjects)(nil) }, func(d *Dependencies) { d.Processes = (*constructorProcesses)(nil) }, func(d *Dependencies) { d.ProcessID = oc.ProcessID{} }, func(d *Dependencies) { d.Bundle = BuiltinBundle{} }} {
		d := base
		edit(&d)
		s, e := New(d)
		var fault *f.Fault
		if s != nil || !errors.As(e, &fault) || fault.Code != f.DependencyUnbound {
			t.Fatal("missing dependency accepted")
		}
	}
}
func TestSkillServiceCancellationRetainsCapacityAndActualOwner(t *testing.T) {
	s := testSkillService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make([]*serviceCall, 0, MaxInitializations)
	for range MaxInitializations {
		c, e := s.begin(ctx, true)
		if e != nil {
			t.Fatal(e)
		}
		calls = append(calls, c)
	}
	cancel()
	if c, e := s.begin(context.Background(), true); e == nil || c != nil {
		t.Fatal("cancel reclaimed an unfinished physical slot")
	}
	s.end(calls[0])
	c, e := s.begin(context.Background(), true)
	if e != nil {
		t.Fatal(e)
	}
	calls[0] = c
	s.Stop()
	if s.Joined() {
		t.Fatal("stop is not join")
	}
	expired, expire := context.WithCancel(context.Background())
	expire()
	if e := s.Drain(expired); !errors.Is(e, context.Canceled) {
		t.Fatal("lost caller deadline", e)
	}
	if c, e := s.begin(context.Background(), false); e == nil || c != nil {
		t.Fatal("late read admission")
	}
	var owners sync.WaitGroup
	for _, c := range calls {
		owners.Add(1)
		go func(c *serviceCall) {
			defer owners.Done()
			if c.ctx.Err() != context.Canceled {
				t.Error("uncancelled owner")
			}
			s.end(c)
			s.end(c)
		}(c)
	}
	owners.Wait()
	if e := s.Drain(context.Background()); e != nil || !s.Joined() {
		t.Fatal("actual joined calls did not retire", e)
	}
}
