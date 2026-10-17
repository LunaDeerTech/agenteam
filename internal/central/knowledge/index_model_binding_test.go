package knowledge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func bindingActor(t *testing.T, name id.ServiceName, cause string, project id.ProjectID) id.Actor {
	t.Helper()
	registration, err := id.RegisterService(name)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := id.InProject(project)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(cause, scope)
	if err != nil {
		t.Fatal(err)
	}
	return actor
}

func modelBindingFixture(t *testing.T) (*indexOperation, mc.ConsumerRequest) {
	t.Helper()
	project := newID[id.Project](t)
	call := newID[mc.Call](t)
	key := newID[struct{}](t).String()
	actor := bindingActor(t, id.KnowledgeRuntime, key, project)
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, call.String())
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := id.InProject(project)
	ref, err := sc.NewCredentialRef(newID[sc.Credential](t), scope)
	if err != nil {
		t.Fatal(err)
	}
	lease := newID[sc.Lease](t)
	consumer := mc.Consumer{Kind: mc.KnowledgeConsumer, ProjectID: project, Purpose: mc.KnowledgeEmbedding, OperationID: key}
	resolved := mc.ResolvedModel{
		Snapshot: mc.ConfigSnapshot{
			ID:       newID[mc.Snapshot](t),
			Identity: mc.ModelIdentity{ProviderID: newID[mc.Provider](t), ModelID: newID[mc.Model](t), ProviderName: "provider", ModelName: "embedding", ProviderModelID: "upstream-model", Protocol: mc.OpenAIEmbeddings, Profile: mc.OpenAIEmbeddingsV1, ModelType: mc.EmbeddingModel, AdapterRevision: "openai-embeddings-float-v2"},
			Endpoint: "https://provider.example/v1", Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`),
			Capabilities: mc.Capabilities{Embedding: &mc.EmbeddingCapabilities{Dimensions: 2, DimensionsMode: mc.EmbeddingFixedDimensions}, InputModalities: []string{"text"}, OutputModalities: []string{"vector"}}, CredentialRef: &ref,
		},
		Consumer: consumer, LeaseOwner: owner, CredentialLease: &sc.CredentialLease{LeaseID: lease, CredentialRef: ref},
	}
	if err := resolved.Validate(); err != nil {
		t.Fatal(err)
	}
	authority := &IndexAuthority{process: newID[oc.Process](t)}
	service := &IndexService{authority: authority, owners: map[*indexOperation]struct{}{}}
	op := &indexOperation{service: service, id: key, profile: indexProfile{Project: project}, kind: "index", call: call, actor: actor, input: f.Digest("sha256:" + strings.Repeat("a", 64))}
	service.owners[op] = struct{}{}
	op.binding.remember(resolved)
	r := mc.ConsumerRequest{Action: mc.ReadCredentialConsumer, Actor: actor, Consumer: consumer, SnapshotID: resolved.Snapshot.ID, LeaseOwner: owner, LeaseID: &lease, CallID: &call,
		Attempt: &mc.AttemptIdentity{CallID: call, InvocationID: newID[mc.Invocation](t), AttemptIndex: 1, ProcessID: authority.process, Fence: 1},
		Input:   &mc.InputIdentity{OperationID: key, Digest: op.input, SchemaVersion: 1}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	return op, r
}

func bindingFinalize(t *testing.T, r mc.ConsumerRequest) mc.ConsumerRequest {
	t.Helper()
	r = r.Clone()
	r.Action = mc.FinalizeConsumer
	r.Actor = bindingActor(t, id.ModelRuntime, r.Attempt.InvocationID.String(), r.Consumer.ProjectID)
	return r
}

func bindingRetire(r mc.ConsumerRequest) mc.ConsumerRequest {
	r = r.Clone()
	r.Action, r.Attempt, r.Input = mc.RetireConsumer, nil, nil
	return r
}

func TestIndexModelBindingOwnsTechnicalFinalizationAndRetirement(t *testing.T) {
	for _, first := range []string{"credential", "startup-finalization"} {
		t.Run(first, func(t *testing.T) {
			op, read := modelBindingFixture(t)
			finalize := bindingFinalize(t, read)
			initial := read
			if first == "startup-finalization" {
				initial = finalize
			}
			ctx := context.WithValue(context.Background(), indexOperationKey{}, op)
			if _, err := op.service.authority.operation(ctx, initial); err != nil {
				t.Fatal(err)
			}
			if err := op.binding.authorize(op, initial, false); err != nil {
				t.Fatal(err)
			}
			if op.binding.attempt != nil {
				t.Fatal("discovery bound an attempt")
			}
			retire := bindingRetire(finalize)
			if err := op.binding.authorize(op, retire, false); err == nil {
				t.Fatal("unbound technical retirement accepted")
			}
			if err := op.binding.authorize(op, initial, true); err != nil {
				t.Fatal(err)
			}
			for _, r := range []mc.ConsumerRequest{read, finalize, retire} {
				if r.Validate() != nil {
					t.Fatal("invalid fixture callback")
				}
				if _, err := op.service.authority.operation(ctx, r); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := op.service.authority.operation(context.Background(), finalize); err == nil {
				t.Fatal("technical actor without private operation accepted")
			}
			version := f.Version(2)
			retire.Actor, retire.TerminalVersion = op.actor, &version
			if err := op.binding.authorize(op, retire, false); err != nil {
				t.Fatal("original domain terminal retirement rejected", err)
			}
			retire.TerminalVersion = nil
			if err := op.binding.authorize(op, retire, false); err == nil {
				t.Fatal("domain actor accepted as runtime retirement")
			}
		})
	}
}

func TestIndexModelBindingRejectsSubstitution(t *testing.T) {
	cases := map[string]func(*testing.T, *indexOperation, *mc.ConsumerRequest){
		"cause": func(t *testing.T, op *indexOperation, r *mc.ConsumerRequest) {
			r.Actor = bindingActor(t, id.ModelRuntime, newID[mc.Invocation](t).String(), op.profile.Project)
		},
		"project": func(t *testing.T, _ *indexOperation, r *mc.ConsumerRequest) {
			r.Actor = bindingActor(t, id.ModelRuntime, r.Attempt.InvocationID.String(), newID[id.Project](t))
		},
		"service": func(t *testing.T, op *indexOperation, r *mc.ConsumerRequest) {
			r.Actor = bindingActor(t, id.KnowledgeRuntime, r.Attempt.InvocationID.String(), op.profile.Project)
		},
		"process": func(t *testing.T, _ *indexOperation, r *mc.ConsumerRequest) {
			r.Attempt.ProcessID = newID[oc.Process](t)
		},
		"call":     func(t *testing.T, _ *indexOperation, r *mc.ConsumerRequest) { r.Attempt.CallID = newID[mc.Call](t) },
		"attempt":  func(_ *testing.T, _ *indexOperation, r *mc.ConsumerRequest) { r.Attempt.AttemptIndex = 2 },
		"fence":    func(_ *testing.T, _ *indexOperation, r *mc.ConsumerRequest) { r.Attempt.Fence = 2 },
		"snapshot": func(t *testing.T, _ *indexOperation, r *mc.ConsumerRequest) { r.SnapshotID = newID[mc.Snapshot](t) },
		"lease": func(t *testing.T, _ *indexOperation, r *mc.ConsumerRequest) {
			lease := newID[sc.Lease](t)
			r.LeaseID = &lease
		},
		"no-lease":      func(_ *testing.T, _ *indexOperation, r *mc.ConsumerRequest) { r.LeaseID = nil },
		"profile-owner": func(_ *testing.T, op *indexOperation, _ *mc.ConsumerRequest) { op.kind = "profile" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			op, r := modelBindingFixture(t)
			r = bindingFinalize(t, r)
			change(t, op, &r)
			if err := op.binding.authorize(op, r, true); err == nil {
				t.Fatal("substituted callback accepted")
			}
			if op.binding.attempt != nil {
				t.Fatal("rejected callback changed binding")
			}
		})
	}
	op, read := modelBindingFixture(t)
	if err := op.binding.authorize(op, read, true); err != nil {
		t.Fatal(err)
	}
	rebound := read.Clone()
	rebound.Attempt.InvocationID = newID[mc.Invocation](t)
	rebound = bindingFinalize(t, rebound)
	if err := op.binding.authorize(op, rebound, true); err == nil {
		t.Fatal("second invocation replaced original binding")
	}
	if *op.binding.attempt != *read.Attempt {
		t.Fatal("original binding changed")
	}
}
