package project

import (
	"bytes"
	"context"
	"errors"
	"testing"

	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	knowledge "github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	object "github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

var deliveryProducers = []au.Producer{au.SecretProducer, au.ProjectVariableProducer, au.KnowledgeProducer, au.ObjectProducer}

func deliveryVariableEntry(t *testing.T, x *auditGateFixture) (au.Entry, au.AppendKey) {
	t.Helper()
	scope, _ := i.InProject(x.project)
	id := testID[i.ProjectVariable](t)
	resource, _ := au.NewResource(au.ProjectVariableResource, id.String())
	metadata, err := au.ProjectVariableMetadata(au.ProjectVariableUpdate, au.ProjectVariableMetadataFields{VariableID: id.String(), Version: 2, ChangedFields: []string{"name"}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := au.NewEntry(au.EntryFields{Scope: scope, Actor: x.actor, Action: au.ProjectVariableUpdate, Outcome: au.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	key, err := au.NewAppendKey(au.ProjectVariableProducer, digest([]byte("original variable command")).String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return entry, key
}

func TestIndependentKnowledgeDeliveryFourAuditProviders(t *testing.T) {
	for _, selected := range deliveryProducers {
		t.Run(string(selected), func(t *testing.T) {
			x := newAuditGateFixture(t, nil)
			var entry au.Entry
			var key au.AppendKey
			switch selected {
			case au.SecretProducer:
				entry, key = auditGateEntry(t, x, x.actor, au.SecretUpdate)
			case au.ProjectVariableProducer:
				entry, key = deliveryVariableEntry(t, x)
			case au.KnowledgeProducer:
				entry, key = knowledgeAuditEntry(t, x)
			case au.ObjectProducer:
				entry, key = objectAuditEntry(t, x, au.ObjectUploadComplete, 0)
			}
			type witness struct{}
			ctx := context.WithValue(context.Background(), witness{}, "independent private marker")
			cause := errors.New("domain-owned cause")
			original := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
			calls := map[au.Producer]int{}
			providers := map[au.Producer]au.ProjectFactAuthority{}
			for _, producer := range deliveryProducers {
				providers[producer] = auditFactFunc(func(got context.Context, tx f.Tx, actual au.Entry, actualKey au.AppendKey) error {
					calls[producer]++
					if producer != selected || got != ctx || tx != x.store.tx || actualKey.Details() != key.Details() || actual.Fields().Action != entry.Fields().Action || !actual.Fields().Actor.Equal(entry.Fields().Actor) || !actual.Fields().Scope.Equal(entry.Fields().Scope) || actual.Fields().Resource.Details() != entry.Fields().Resource.Details() || !bytes.Equal(actual.Fields().Metadata.JSON(), entry.Fields().Metadata.JSON()) {
						t.Fatal("wrong provider or original context/Tx/facts changed")
					}
					return original
				})
			}
			a, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: providers})
			if err != nil {
				t.Fatal(err)
			}
			// Initialization wrapping must not intercept any ordinary producer,
			// including Object metadata whose initiator is a Human.
			wrapped, err := NewInitializationAuditAuthority(a, auditFactFunc(func(context.Context, f.Tx, au.Entry, au.AppendKey) error {
				t.Fatal("ordinary fact borrowed initialization proof")
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if got := wrapped.CheckAppendInTx(ctx, x.store.tx, entry, key); got != original || !errors.Is(got, cause) || calls[selected] != 1 {
				t.Fatal("exact domain Unknown not preserved")
			}
			for _, producer := range deliveryProducers {
				if producer == selected {
					continue
				}
				wrong, err := au.NewAppendKey(producer, digest([]byte("wrong producer binding")).String(), 0)
				if err != nil {
					t.Fatal(err)
				}
				hasCode(t, wrapped.CheckAppendInTx(ctx, x.store.tx, entry, wrong), f.Forbidden)
			}
			x.initialized = false
			hasCode(t, wrapped.CheckAppendInTx(ctx, x.store.tx, entry, key), f.ProjectNotActive)
			x.initialized, x.lifecycle = true, pc.Archived
			hasCode(t, wrapped.CheckAppendInTx(ctx, x.store.tx, entry, key), f.ProjectNotActive)
			if len(calls) != 1 || calls[selected] != 1 {
				t.Fatal("denied gate dispatched or fell through")
			}
		})
	}
}

func TestIndependentKnowledgeDeliveryPlansNeverCrossDomains(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	requests := []oc.ProjectRequest{modelProjectRequest(t, x), blockerProjectRequest(t, x), variableProjectRequest(t, x), knowledgeProjectRequest(t, x, "knowledge.content_changed")}
	plans := make([]oc.Dependencies, len(requests))
	for n, request := range requests {
		var err error
		plans[n], err = x.a.Discover(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(x.order) != 0 {
		t.Fatal("discovery performed authority I/O")
	}
	for n, request := range requests {
		t.Run(string(request.Details().Event.Producer), func(t *testing.T) {
			for _, stage := range []oc.Stage{oc.CurrentAccess, oc.NewFact} {
				d := request.Details()
				d.Stage = stage
				current, err := oc.NewProjectRequest(d)
				if err != nil {
					t.Fatal(err)
				}
				for other, plan := range plans {
					x.order = nil
					err = x.a.ValidateInTx(context.Background(), x.store.tx, current, plan)
					if n == other {
						if err != nil {
							t.Fatal("own plan rejected", err)
						}
					} else if err == nil || len(x.order) != 0 {
						t.Fatal("cross-domain plan reached authority")
					}
				}
			}
			d := request.Details()
			d.Event.Producer = "knowledge"
			if n == 3 {
				d.Event.Producer = "projectvariable"
			}
			wrong, err := oc.NewProjectRequest(d)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = x.a.Discover(context.Background(), wrong); err == nil {
				t.Fatal("producer-only rewrite admitted foreign event triple")
			}
		})
	}
}

func TestIndependentKnowledgeDeliveryRealFactsStillNeedPrivateProof(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	knowledgeFacts, err := knowledge.NewProjectAuditAuthority(x.store)
	if err != nil {
		t.Fatal(err)
	}
	objectFacts, err := object.NewProjectAuditAuthority(x.store)
	if err != nil {
		t.Fatal(err)
	}
	called := map[au.Producer]int{}
	providers := map[au.Producer]au.ProjectFactAuthority{}
	for _, producer := range deliveryProducers {
		providers[producer] = auditFactFunc(func(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey) error {
			called[producer]++
			switch producer {
			case au.KnowledgeProducer:
				return knowledgeFacts.CheckProjectAuditInTx(ctx, tx, entry, key)
			case au.ObjectProducer:
				return objectFacts.CheckProjectAuditInTx(ctx, tx, entry, key)
			default:
				t.Fatal("missing private proof fell back to adjacent producer")
				return nil
			}
		})
	}
	a, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: providers})
	if err != nil {
		t.Fatal(err)
	}
	entry, key := knowledgeAuditEntry(t, x)
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.Forbidden)
	entry, key = objectAuditEntry(t, x, au.ObjectUploadComplete, 0)
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.Forbidden)
	if called[au.KnowledgeProducer] != 1 || called[au.ObjectProducer] != 1 || len(called) != 2 {
		t.Fatal("real fact checker not reached exactly")
	}
}

func TestIndependentKnowledgeDeliveryInitializationDoesNotBorrowOrdinaryProof(t *testing.T) {
	for _, kind := range []i.ActorKind{i.Human, i.AgentRun} {
		t.Run(string(kind), func(t *testing.T) {
			x := newAuditGateFixture(t, nil)
			entry, key := objectAuditEntry(t, x, au.ObjectUploadComplete, 0)
			fields := entry.Fields()
			metadata := au.ObjectMetadataFields{ObjectID: fields.Resource.Details().ID, InitiatorKind: kind, InitiatorID: x.actor.Details().UserID, MediaType: "text/plain", ByteSize: 3, Phase: au.PublishedPhase}
			if kind == i.AgentRun {
				metadata.InitiatorExecutionID = testID[struct{}](t).String()
			}
			var err error
			fields.Metadata, err = au.ObjectMetadata(fields.Action, metadata)
			if err != nil {
				t.Fatal(err)
			}
			entry, err = au.NewEntry(fields)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			a := withObjectFacts(t, x, auditFactFunc(func(context.Context, f.Tx, au.Entry, au.AppendKey) error {
				calls++
				return f.NewFault(f.Forbidden, f.NotStarted)
			}))
			wrapped, err := NewInitializationAuditAuthority(a, auditFactFunc(func(context.Context, f.Tx, au.Entry, au.AppendKey) error {
				t.Fatal("non-Service initiator reached initialization facts")
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			x.initialized = false
			hasCode(t, wrapped.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.ProjectNotActive)
			if calls != 0 || len(x.store.locks) != 1 || x.store.locks[0].Mode != f.Shared {
				t.Fatal("ordinary pending path bypassed its SH/initialization gate")
			}
			x.initialized = true
			hasCode(t, wrapped.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.Forbidden)
			if calls != 1 {
				t.Fatal("ordinary current gate did not reach exact Object facts")
			}
		})
	}
	t.Run("service_initialization_keeps_original_exclusive_path", func(t *testing.T) {
		x := newInitializationAuditFixture(t, au.ObjectUploadComplete, nil)
		for _, producer := range deliveryProducers {
			x.base.authority.state().auditFacts[producer] = auditFactFunc(func(context.Context, f.Tx, au.Entry, au.AppendKey) error {
				t.Fatal("initialization borrowed ordinary fact provider")
				return nil
			})
		}
		x.check(t, "", "held", "executor", "creation", "project", "provider")
	})
}
