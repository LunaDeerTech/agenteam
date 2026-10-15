package memory

import (
	"context"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestModelAuthorityRequiresDedicatedProjectScopedRegistration(t *testing.T) {
	registration, err := i.RegisterService(i.MemoryRuntime)
	if err != nil {
		t.Fatal(err)
	}
	// A domain consumer registration is deliberately unavailable in System
	// scope. Construction must probe its supported Project scope instead.
	if _, err := registration.Actor(testID[struct{}](t).String(), i.SystemScope()); err == nil {
		t.Fatal("memory service accepted system scope")
	}
	if authority, err := NewModelAuthority(&Service{}, registration); err != nil || authority == nil {
		t.Fatal("valid memory registration rejected", err)
	}
	model, err := i.RegisterService(i.ModelRuntime)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []i.ServiceRegistration{{}, model} {
		if a, err := NewModelAuthority(&Service{}, r); a != nil {
			t.Fatal("foreign service registration accepted")
		} else {
			requireFault(t, err, f.DependencyUnbound)
		}
	}
	if a, err := NewModelAuthority(nil, registration); a != nil {
		t.Fatal("missing domain accepted")
	} else {
		requireFault(t, err, f.DependencyUnbound)
	}
}

// This pure gate test supplies a private owner deliberately. It verifies the
// callback actor/tuple boundary, not SQL facts, ProcessGuard or Model Runtime.
func TestModelAuthorityFirstFinalizeAndExactRetirement(t *testing.T) {
	project, agent := testID[i.Project](t), testID[i.Agent](t)
	scope, _ := i.InProject(project)
	registration, _ := i.RegisterService(i.MemoryRuntime)
	operation := testID[struct{}](t).String()
	actor, _ := registration.Actor(operation, scope)
	a, _ := NewModelAuthority(&Service{}, registration)
	a.process = testID[oc.Process](t)
	entry := &call{service: a.domain}
	callID := testID[mc.Call](t)
	owner, _ := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, callID.String())
	snapshot, lease := testID[mc.Snapshot](t), testID[sc.Lease](t)
	inv := &modelInvocation{authority: a, entry: entry, id: callID, actor: actor, consumer: mc.Consumer{Kind: mc.MemoryConsumer, ProjectID: project, AgentID: &agent, OperationID: operation, Purpose: mc.MemoryEmbedding}, input: mc.InputIdentity{OperationID: operation, Digest: digest([]byte("input")), SchemaVersion: 1}, owner: owner, resolved: &mc.ResolvedModel{Snapshot: mc.ConfigSnapshot{ID: snapshot}, CredentialLease: &sc.CredentialLease{LeaseID: lease}}}
	a.calls[inv] = struct{}{}
	ctx := context.WithValue(context.Background(), modelInvocationKey{}, inv)
	attempt := mc.AttemptIdentity{CallID: callID, InvocationID: testID[mc.Invocation](t), AttemptIndex: 1, ProcessID: a.process, Fence: 1}
	modelReg, _ := i.RegisterService(i.ModelRuntime)
	technical, _ := modelReg.Actor(attempt.InvocationID.String(), scope)
	request := mc.ConsumerRequest{Action: mc.FinalizeConsumer, Actor: technical, Consumer: inv.consumer, SnapshotID: snapshot, LeaseOwner: owner, LeaseID: &lease, CallID: &callID, Input: &inv.input, Attempt: &attempt}
	if _, err := a.witness(ctx, request); err != nil {
		t.Fatal("first finalize must pass private gate before credential read", err)
	}
	for name, change := range map[string]func(*mc.ConsumerRequest){
		"wrong actor cause": func(r *mc.ConsumerRequest) { r.Actor, _ = modelReg.Actor(testID[struct{}](t).String(), scope) },
		"foreign process":   func(r *mc.ConsumerRequest) { r.Attempt.ProcessID = testID[oc.Process](t) },
		"foreign lease":     func(r *mc.ConsumerRequest) { v := testID[sc.Lease](t); r.LeaseID = &v },
		"foreign snapshot":  func(r *mc.ConsumerRequest) { r.SnapshotID = testID[mc.Snapshot](t) },
		"foreign input":     func(r *mc.ConsumerRequest) { r.Input.Digest = digest([]byte("other")) },
		"later attempt":     func(r *mc.ConsumerRequest) { r.Attempt.AttemptIndex = 2 },
		"foreign fence":     func(r *mc.ConsumerRequest) { r.Attempt.Fence = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			r := request.Clone()
			change(&r)
			if _, err := a.witness(ctx, r); err == nil {
				t.Fatal("changed original tuple accepted")
			}
		})
	}
	retire := request.Clone()
	retire.Action = mc.RetireConsumer
	retire.Attempt = nil
	retire.Input = nil
	if _, err := a.witness(ctx, retire); err == nil {
		t.Fatal("runtime retirement without bound original attempt")
	}
	inv.attempt = &attempt
	if _, err := a.witness(ctx, retire); err != nil {
		t.Fatal("original runtime retirement rejected", err)
	}
	changed := request.Clone()
	changed.Attempt.InvocationID = testID[mc.Invocation](t)
	changed.Actor, _ = modelReg.Actor(changed.Attempt.InvocationID.String(), scope)
	if _, err := a.witness(ctx, changed); err == nil {
		t.Fatal("second invocation replaced bound original")
	}
	if _, err := a.witness(context.Background(), request); err == nil {
		t.Fatal("DTO alone grants callback")
	}
	delete(a.calls, inv)
	if _, err := a.witness(ctx, request); err == nil {
		t.Fatal("retired private owner grants callback")
	}
}
