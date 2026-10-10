package skill

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type bindingExecutionControl struct{ calls int }

func (p *bindingExecutionControl) DiscoverInstallExecution(context.Context, sc.InstallExecutionRequest) (sc.InstallExecutionPlan, error) {
	p.calls++
	return nil, fault(f.Forbidden)
}
func (p *bindingExecutionControl) RequireInstallExecutionInTx(context.Context, f.Tx, sc.InstallExecutionRequest, sc.InstallExecutionPlan) error {
	p.calls++
	return fault(f.Forbidden)
}

type incomparableBindingControl []byte

func (incomparableBindingControl) DiscoverInstallExecution(context.Context, sc.InstallExecutionRequest) (sc.InstallExecutionPlan, error) {
	panic("composition must not request authority")
}
func (incomparableBindingControl) RequireInstallExecutionInTx(context.Context, f.Tx, sc.InstallExecutionRequest, sc.InstallExecutionPlan) error {
	panic("composition must not request authority")
}

// These controls exercise composition only. No Execution, ProcessGuard or
// actual installation is manufactured by a successful identity check.
func TestSkillInstallBindingOriginalInstanceAndTransaction(t *testing.T) {
	s := testSkillService(t)
	d := s.state()
	store := d.authority.state().store.(*initializationGateStore)
	store.tx = f.NewTx()
	producer := &bindingExecutionControl{}
	d.authority.state().installExecution = producer
	if err := s.RequireInstallBindingInTx(context.Background(), store.tx, producer, d.process); err != nil {
		t.Fatal(err)
	}
	if len(store.calls) != 1 || store.calls[0] != "live" || producer.calls != 0 || len(d.calls) != 0 {
		t.Fatal("binding opened work, called authority or skipped original Tx")
	}
	if err := s.RequireInstallBindingInTx(context.Background(), f.NewTx(), producer, d.process); err == nil {
		t.Fatal("foreign transaction accepted")
	}
	store.liveErr = errors.New("ended original transaction")
	if err := s.RequireInstallBindingInTx(context.Background(), store.tx, producer, d.process); err == nil {
		t.Fatal("ended transaction accepted")
	}
}

func TestSkillInstallBindingRejectsChangedOrUnavailableOwner(t *testing.T) {
	for _, name := range []string{"zero-service", "human-only", "foreign-producer", "typed-nil", "incomparable", "process", "stopped", "cancelled", "nil-context"} {
		t.Run(name, func(t *testing.T) {
			s := testSkillService(t)
			d := s.state()
			store := d.authority.state().store.(*initializationGateStore)
			store.tx = f.NewTx()
			producer := &bindingExecutionControl{}
			d.authority.state().installExecution = producer
			var expected sc.InstallExecutionAuthority = producer
			process := d.process
			ctx := context.Background()
			switch name {
			case "zero-service":
				s = &Service{}
			case "human-only":
				d.authority.state().installExecution = nil
			case "foreign-producer":
				expected = &bindingExecutionControl{}
			case "typed-nil":
				expected = (*bindingExecutionControl)(nil)
			case "incomparable":
				expected = incomparableBindingControl{1}
				d.authority.state().installExecution = expected
			case "process":
				process = stateID[oc.Process](51)
			case "stopped":
				s.Stop()
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil-context":
				ctx = nil
			}
			err := s.RequireInstallBindingInTx(ctx, store.tx, expected, process)
			if err == nil || producer.calls != 0 || len(d.calls) != 0 {
				t.Fatal("invalid composition granted or admitted work")
			}
			if name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("original cancellation lost")
			}
		})
	}
}
