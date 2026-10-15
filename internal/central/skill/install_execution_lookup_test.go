package skill

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type installLookupProviderControl struct {
	*installExecutionControl
	lookupReads int
	rejectLive  bool
}

func (p *installLookupProviderControl) DiscoverInstallLookup(ctx context.Context, r sc.InstallLookupRequest) (sc.InstallExecutionPlan, error) {
	if r.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	return p.DiscoverInstallExecution(ctx, r.Original)
}
func (p *installLookupProviderControl) RequireInstallLookupInTx(ctx context.Context, tx f.Tx, r sc.InstallLookupRequest, plan sc.InstallExecutionPlan) error {
	if r.Validate() != nil {
		return fault(f.Forbidden)
	}
	p.lookupReads++
	return p.installExecutionControl.RequireInstallExecutionInTx(ctx, tx, r.Original, plan)
}
func (p *installLookupProviderControl) RequireInstallExecutionInTx(ctx context.Context, tx f.Tx, r sc.InstallExecutionRequest, plan sc.InstallExecutionPlan) error {
	if p.rejectLive {
		return fault(f.Forbidden)
	}
	return p.installExecutionControl.RequireInstallExecutionInTx(ctx, tx, r, plan)
}

func TestInstallLookupBindingCannotGrantMutation(t *testing.T) {
	s, store, _, original, ctx, actor, row, request, meta := executionInstallFixture(t)
	provider := &installLookupProviderControl{installExecutionControl: original}
	a, err := NewAuthorityWithInstallExecution(store, s.state().authority.state().projects, provider)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := a.bindInstallLookup(ctx, actor, meta, row.project, request)
	if err != nil {
		t.Fatal(err)
	}
	call, err := a.installCall(bound, actor, row.project)
	if err != nil || call.mutatePlan != nil || len(call.mutateLocks) != 0 {
		t.Fatal("lookup created a mutation grant")
	}
	if handled, err := a.requireInstallLookup(bound, f.Tx{}, actor, row.project, id.Mutate); !handled || err == nil {
		t.Fatal("private lookup accepted mutation")
	}
	if handled, err := a.requireInstallLookup(context.Background(), f.Tx{}, actor, row.project, id.Read); handled || err != nil {
		t.Fatal("bare context was interpreted as a lookup proof")
	}
	changed := meta
	changed.RequestID = stateID[f.Request](999)
	if _, err = a.bindInstallLookup(ctx, actor, changed, row.project, request); err == nil {
		t.Fatal("lookup changed the original physical request")
	}
}

func TestInstallLookupServiceUsesOnlyPrivateReadBinding(t *testing.T) {
	for _, phase := range []string{"published", "reserved"} {
		t.Run(phase, func(t *testing.T) {
			s, store, objects, original, ctx, actor, row, request, meta := executionInstallFixture(t)
			provider := &installLookupProviderControl{installExecutionControl: original, rejectLive: true}
			a, err := NewAuthorityWithInstallExecution(store, s.state().authority.state().projects, provider)
			if err != nil {
				t.Fatal(err)
			}
			s.state().authority = a
			store.installationValues[14] = phase
			receipt, err := s.LookupInstallExecution(ctx, actor, meta, row.project, request)
			if phase == "published" {
				if err != nil || receipt.Validate() != nil || receipt.SkillID != row.skill || receipt.ObjectID != row.object {
					t.Fatal("original published receipt was not read", err)
				}
			} else if err == nil || receipt != (InstallReceipt{}) {
				t.Fatal("reserved publication became a receipt")
			}
			if provider.lookupReads != 1 || original.writes != 0 || len(objects.steps) != 0 || store.skillInserted {
				t.Fatal("lookup used live mutation authority or physical installation")
			}
		})
	}
}
