package model

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type callRetirementNoSecret struct{}

func (*callRetirementNoSecret) DiscoverUsage(context.Context, sc.UsageRequest) (sc.UsageDependencies, error) {
	panic("unproven retirement cannot read Secret")
}
func (*callRetirementNoSecret) ApplyUsageInTx(context.Context, f.Tx, sc.UsageRequest, sc.UsageDependencies) (sc.UsageResult, error) {
	panic("unproven retirement cannot write Secret")
}
func (*callRetirementNoSecret) ModelCallLeaseReleasedInTx(context.Context, f.Tx, sc.UsageRequest, sc.UsageDependencies) (bool, error) {
	panic("unproven retirement cannot observe Secret")
}

func TestModelCallRetirementRequiresCurrentConsumerAndOwnPlan(t *testing.T) {
	r := runtimePureRequest(t)
	request := mc.ModelCallRetirementRequest{Actor: r.Actor, Model: r.Model, TerminalVersion: 4}
	if err := request.Validate(); err != nil {
		t.Fatal("controlled request shape", err)
	}
	want := f.NewFault(f.Forbidden, f.NotStarted)
	calls := 0
	a := runtimePureAuthority(t, &noIOStore{}, runtimeConsumerFunc(func(_ context.Context, actual mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
		calls++
		if actual.Action != mc.RetireConsumer || actual.CallID == nil || *actual.CallID != r.CallID || actual.TerminalVersion == nil || *actual.TerminalVersion != request.TerminalVersion || !actual.LeaseOwner.Equal(r.Model.LeaseOwner) {
			t.Fatal("Model-call retirement lost its original call")
		}
		return mc.ConsumerDependencies{}, want
	}))
	p, err := NewModelCallLeaseRetirement(a, &callRetirementNoSecret{})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.DiscoverModelCallRetirement(context.Background(), request)
	if !errors.Is(err, want) || plan != nil || calls != 1 {
		t.Fatal("unproven current owner obtained a retirement plan")
	}
	if err = p.ReleaseModelCallInTx(context.Background(), f.NewTx(), request, &modelCallRetirementPlan{}); err == nil {
		t.Fatal("foreign plan authorized release")
	}
	if yes, err := p.ModelCallRetiredInTx(context.Background(), f.NewTx(), request, &modelCallRetirementPlan{}); err == nil || yes {
		t.Fatal("foreign plan authorized an observation")
	}
	var missing *callRetirementNoSecret
	if _, err := NewModelCallLeaseRetirement(a, missing); err == nil {
		t.Fatal("nil dependency accepted")
	}
}

func TestModelCallRetirementWitnessIsScopedAndNoMaterial(t *testing.T) {
	r := runtimePureRequest(t)
	a := runtimePureAuthority(t, &noIOStore{}, runtimeConsumerFunc(func(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
		panic("unissued witness must not reach authority")
	}))
	reg := a.state().auth.SecretService
	actor, _ := reg.Actor(r.Model.LeaseOwner.Details().ID, r.Model.CredentialLease.CredentialRef.Details().Scope)
	usage := sc.UsageRequest{Actor: actor, Ref: r.Model.CredentialLease.CredentialRef, Purpose: sc.Model, LeaseOwner: r.Model.LeaseOwner, LeaseID: r.Model.CredentialLease.LeaseID, Action: sc.ReleaseLeaseUsage}
	if _, err := a.discoverModelCallRetirement(context.Background(), usage); err == nil {
		t.Fatal("public release DTO supplied a private witness")
	}
	// This private witness is a negative protocol control, not terminal proof.
	w := &modelCallRetirementWitness{authority: a, usage: usage, active: true}
	ctx := context.WithValue(context.Background(), modelCallRetirementKey{}, w)
	w.retire()
	if _, err := a.modelCallRetirementWitness(ctx, usage); err == nil {
		t.Fatal("retired callback witness was reused")
	}
	copy := mc.ModelCallRetirementRequest{Actor: r.Actor, Model: r.Model, TerminalVersion: 4}.Clone()
	copy.Model.Snapshot.Parameters[0] = '['
	if string(r.Model.Snapshot.Parameters) != "{}" {
		t.Fatal("retirement request retained caller aliases")
	}
}
