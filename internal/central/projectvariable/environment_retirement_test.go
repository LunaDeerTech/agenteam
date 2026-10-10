package projectvariable

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Only the outer Execution proof and Secret persistence are controlled. The
// capture provider, immutable head/set checks and PV callback witnesses are the
// real implementation; this does not claim an Execution terminal transaction.
type retirementTestOwner struct {
	store   *environmentTestStore
	binding f.Digest
	reject  bool
}

func (o *retirementTestOwner) CheckEnvironmentRetirementPlan(_ context.Context, v c.EnvironmentCapture) error {
	actual, err := c.EnvironmentRetirementBinding(v)
	if err != nil || actual != o.binding || o.reject {
		return fault(f.Forbidden)
	}
	return nil
}
func (o *retirementTestOwner) CheckEnvironmentRetirementInTx(ctx context.Context, tx f.Tx, v c.EnvironmentCapture) error {
	if tx != o.store.tx {
		return fault(f.Forbidden)
	}
	return o.CheckEnvironmentRetirementPlan(ctx, v)
}

type retirementTestLeases struct {
	authority                 *EnvironmentRetirementAuthority
	request                   sc.ProjectVariableLeaseRetirementRequest
	planning, final, observed context.Context
	tx                        f.Tx
	writes, plans, reads      int
	retired                   bool
	after                     func()
}

func (l *retirementTestLeases) DiscoverProjectVariableLeaseRetirement(ctx context.Context, r sc.ProjectVariableLeaseRetirementRequest) (sc.ProjectVariableLeaseRetirementPlan, error) {
	if err := l.authority.CheckProjectVariableLeaseRetirementPlan(ctx, r); err != nil {
		return nil, err
	}
	l.planning, l.request = ctx, r
	l.plans++
	return environmentTestLeasePlan{r.RequiredLocks()}, nil
}
func (l *retirementTestLeases) RetireProjectVariableLeaseInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRetirementRequest, _ sc.ProjectVariableLeaseRetirementPlan) error {
	if err := l.authority.CheckProjectVariableLeaseRetirementInTx(ctx, tx, r); err != nil {
		return err
	}
	if l.authority.CheckProjectVariableLeaseRetiredInTx(ctx, tx, r) == nil {
		return errors.New("write witness authorized the observer stage")
	}
	l.final, l.tx = ctx, tx
	if !l.retired {
		l.writes++
		l.retired = true
	}
	if l.after != nil {
		l.after()
	}
	return nil
}
func (l *retirementTestLeases) ProjectVariableLeaseRetiredInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableLeaseRetirementRequest, _ sc.ProjectVariableLeaseRetirementPlan) (bool, error) {
	if err := l.authority.CheckProjectVariableLeaseRetiredInTx(ctx, tx, r); err != nil {
		return false, err
	}
	if l.authority.CheckProjectVariableLeaseRetirementInTx(ctx, tx, r) == nil {
		return false, errors.New("observer authorized retirement writes")
	}
	l.observed, l.tx = ctx, tx
	l.reads++
	return l.retired, nil
}

func retirementFixture(t *testing.T, empty bool) (*EnvironmentRetirementProvider, *environmentTestStore, *retirementTestOwner, *retirementTestLeases, c.EnvironmentCapture) {
	t.Helper()
	capture, store, original, _ := newEnvironmentTest(t)
	if empty {
		store.ids, store.index, original.allowed = []string{}, nil, []i.ProjectVariableID{}
	}
	plan, err := capture.DiscoverExecutionEnvironment(context.Background(), original.r)
	if err != nil {
		t.Fatal(err)
	}
	value, err := environmentTestFinal(capture, store, original.r, plan)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := c.EnvironmentRetirementBinding(value)
	if err != nil {
		t.Fatal(err)
	}
	owner := &retirementTestOwner{store: store, binding: binding}
	base, err := NewAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewEnvironmentRetirementAuthority(base, owner)
	if err != nil {
		t.Fatal(err)
	}
	leases := &retirementTestLeases{authority: authority}
	provider, err := NewEnvironmentRetirement(authority, leases)
	if err != nil {
		t.Fatal(err)
	}
	return provider, store, owner, leases, value
}

func TestEnvironmentRetirementPreservesHistoryAndRequiresOriginalProof(t *testing.T) {
	for _, empty := range []bool{false, true} {
		p, s, _, leases, capture := retirementFixture(t, empty)
		before := s.writes
		plan, err := p.DiscoverEnvironmentRetirement(context.Background(), capture)
		if err != nil || plan == nil {
			t.Fatal("retirement discovery", err)
		}
		if !empty && leases.authority.CheckProjectVariableLeaseRetirementPlan(leases.planning, leases.request) == nil {
			t.Fatal("planning witness escaped its original call")
		}
		s.tx, s.locks = f.NewTx(), plan.RequiredLocks()
		observed, err := p.EnvironmentRetiredInTx(context.Background(), s.tx, capture, plan)
		if err != nil || observed != empty || leases.writes != 0 {
			t.Fatal("observer wrote or invented retirement", err)
		}
		for n := 0; n < 2; n++ {
			if err = p.RetireEnvironmentInTx(context.Background(), s.tx, capture, plan); err != nil {
				t.Fatal("retire/replay", err)
			}
		}
		observed, err = p.EnvironmentRetiredInTx(context.Background(), s.tx, capture, plan)
		wantWrites := 1
		if empty {
			wantWrites = 0
		}
		if err != nil || !observed || leases.writes != wantWrites || s.writes != before || s.head == nil || len(s.refs) != len(capture.Fields().Secrets) {
			t.Fatal("retirement deleted immutable history or replay wrote twice", err)
		}
		if !empty && (leases.authority.CheckProjectVariableLeaseRetirementInTx(leases.final, s.tx, leases.request) == nil || leases.authority.CheckProjectVariableLeaseRetiredInTx(leases.observed, s.tx, leases.request) == nil) {
			t.Fatal("final/observer witness survived original callback")
		}
		if empty {
			s.head = nil
			if observed, err = p.EnvironmentRetiredInTx(context.Background(), s.tx, capture, plan); err == nil || observed {
				t.Fatal("empty set replaced the required real head")
			}
		}
	}
	for _, mode := range []string{"owner", "held", "tx", "issuer", "refs", "head", "cancel"} {
		p, s, owner, leases, capture := retirementFixture(t, false)
		plan, err := p.DiscoverEnvironmentRetirement(context.Background(), capture)
		if err != nil {
			t.Fatal(err)
		}
		s.tx, s.locks = f.NewTx(), plan.RequiredLocks()
		tx, ctx := s.tx, context.Background()
		switch mode {
		case "owner":
			owner.reject = true
		case "held":
			s.locks = nil
		case "tx":
			tx = f.NewTx()
		case "issuer":
			p, err = NewEnvironmentRetirement(leases.authority, leases)
			if err != nil {
				t.Fatal(err)
			}
		case "refs":
			s.refs = append(s.refs, testID[i.ProjectVariable](88).String())
		case "head":
			s.head[4] = string(digest([]byte("different immutable head")))
		case "cancel":
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		if err = p.RetireEnvironmentInTx(ctx, tx, capture, plan); err == nil || leases.writes != 0 {
			t.Fatal("unproven retirement", mode, err)
		}
	}
	p, s, _, leases, capture := retirementFixture(t, false)
	s.unknown = true
	plan, err := p.DiscoverEnvironmentRetirement(context.Background(), capture)
	var faultValue *f.Fault
	if plan != nil || !errors.As(err, &faultValue) || faultValue.CommitState != f.Unknown || leases.plans != 0 || leases.writes != 0 {
		t.Fatal("unknown read transaction published a plan")
	}
	s.unknown = false
	plan, err = p.DiscoverEnvironmentRetirement(context.Background(), capture)
	if err != nil {
		t.Fatal(err)
	}
	s.tx, s.locks = f.NewTx(), plan.RequiredLocks()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leases.after = cancel
	if err = p.RetireEnvironmentInTx(ctx, s.tx, capture, plan); !errors.Is(err, context.Canceled) || leases.writes != 1 {
		t.Fatal("late callback cancellation returned success", err)
	}
	// The controlled store does not claim rollback; the caller must roll back
	// the real outer transaction after this cancellation.
}
