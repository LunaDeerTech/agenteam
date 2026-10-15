package usage

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type deniedStoppedOwnerFacts struct {
	usageInvocationFacts
	startup, owners int
}

func (v *deniedStoppedOwnerFacts) RequireStoppedInvocationStartupInTx(context.Context, f.Tx) error {
	v.startup++
	return fault(f.Forbidden)
}
func (v *deniedStoppedOwnerFacts) DiscoverStoppedInvocationOwnerFacts(context.Context, uc.StoppedInvocationOwner) (uc.InvocationDependencies, error) {
	v.owners++
	return uc.InvocationDependencies{}, fault(f.Forbidden)
}
func (*deniedStoppedOwnerFacts) ValidateStoppedInvocationOwnerFactsInTx(context.Context, f.Tx, uc.StoppedInvocationOwner, uc.InvocationDependencies) error {
	return fault(f.Forbidden)
}

func TestStoppedInvocationOwnerRequiresPrivateStartupAndOriginalFacts(t *testing.T) {
	facts := &deniedStoppedOwnerFacts{}
	v := newUsageOwner(t, facts)
	r := stoppedUsageRequest(t, v)
	owner := uc.StoppedInvocationOwner{ProjectID: r.Identity.Consumer.ProjectID, CallID: r.Identity.Attempt.CallID, ProcessID: r.Identity.Attempt.ProcessID}
	_, err := v.pair.Service().DiscoverStoppedInvocationOwner(context.Background(), owner)
	expectFault(t, err, f.Forbidden)
	if facts.owners != 1 {
		t.Fatal("bare owner bypassed Model facts")
	}
	owner.ProcessID = v.proof.process
	_, err = v.pair.Service().DiscoverStoppedInvocationOwner(context.Background(), owner)
	expectFault(t, err, f.ResourceBusy)
	if facts.owners != 1 {
		t.Fatal("current process reached recovery discovery")
	}
	_, err = v.pair.Service().DiscoverStoppedInvocationOwners(context.Background())
	// The controlled Store wraps callback rejection in its original transaction fault.
	expectFault(t, err, f.DependencyUnavailable)
	if facts.startup != 1 {
		t.Fatal("enumeration omitted original startup witness")
	}
	if _, reads, writes := v.store.counts(); reads != 0 || writes != 0 {
		t.Fatal("unauthorized enumeration accessed original borrower rows")
	}
	_, err = newUsageOwner(t, nil).pair.Service().DiscoverStoppedInvocationOwners(context.Background())
	expectFault(t, err, f.DependencyUnbound)
}
