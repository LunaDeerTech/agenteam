package project

import (
	"context"
	"errors"
	"reflect"
	"testing"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

type initializationAuditFixture struct {
	base          *convergenceFixture
	gate          audit.ProjectAuthority
	entry         audit.Entry
	key           audit.AppendKey
	calls         int
	providerError error
}

func initializationAuditEntry(t *testing.T, f *convergenceFixture, action audit.Action, change func(*audit.EntryFields, *audit.ObjectMetadataFields, *audit.AppendKeyDetails)) (audit.Entry, audit.AppendKey) {
	t.Helper()
	scope, _ := identity.InProject(f.request.ProjectID)
	object := testID[struct{}](t).String()
	resource, _ := audit.NewResource(audit.ObjectResource, object)
	k := audit.AppendKeyDetails{Producer: audit.ObjectProducer, CauseRef: testID[struct{}](t).String()}
	m := audit.ObjectMetadataFields{ObjectID: object, InitiatorKind: identity.Service, InitiatorID: f.request.CreationID.String(), MediaType: "text/plain", ByteSize: 7, Phase: audit.PublishedPhase}
	e := audit.EntryFields{Scope: scope, Action: action, Outcome: audit.Success, Resource: resource}
	if action == audit.ObjectUploadFailed {
		e.Outcome, m.Phase, m.Reason = audit.Unknown, audit.FailedPhase, audit.IntegrityMismatch
	}
	if action == audit.ObjectDelete {
		k.CauseRef, k.Ordinal, m.Phase = digest([]byte("private cleanup cause")).String(), 1, audit.DeletedPhase
	}
	e.Actor = convergenceServiceActor(t, identity.ObjectService, scope, k.CauseRef)
	if change != nil {
		change(&e, &m, &k)
	}
	metadata, err := audit.ObjectMetadata(action, m)
	if err != nil {
		t.Fatal("typed metadata construction", err)
	}
	e.Metadata = metadata
	entry, err := audit.NewEntry(e)
	if err != nil {
		t.Fatal("typed entry construction", err)
	}
	key, err := audit.NewAppendKey(k.Producer, k.CauseRef, k.Ordinal)
	if err != nil {
		t.Fatal("typed key construction", err)
	}
	return entry, key
}

func newInitializationAuditFixture(t *testing.T, action audit.Action, change func(*audit.EntryFields, *audit.ObjectMetadataFields, *audit.AppendKeyDetails)) *initializationAuditFixture {
	t.Helper()
	f := &initializationAuditFixture{base: newConvergenceFixture(t)}
	type witness struct{}
	f.base.store.ctx = context.WithValue(context.Background(), witness{}, "original private context")
	f.entry, f.key = initializationAuditEntry(t, f.base, action, change)
	var err error
	f.gate, err = NewInitializationAuditAuthority(f.base.authority, auditFactFunc(func(ctx context.Context, tx foundation.Tx, entry audit.Entry, key audit.AppendKey) error {
		f.calls++
		if ctx != f.base.store.ctx || tx != f.base.store.tx || !reflect.DeepEqual(entry.Fields().Metadata.JSON(), f.entry.Fields().Metadata.JSON()) || !entry.Fields().Actor.Equal(f.entry.Fields().Actor) || key.Details() != f.key.Details() {
			t.Fatal("provider lost original context/Tx/entry/key")
		}
		if !reflect.DeepEqual(f.base.store.order, []string{"held", "executor", "creation", "project"}) {
			t.Fatal("provider preceded Project facts", f.base.store.order)
		}
		f.base.store.order = append(f.base.store.order, "provider")
		return f.providerError
	}))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *initializationAuditFixture) check(t *testing.T, code foundation.Code, order ...string) error {
	t.Helper()
	before := f.base.snapshot()
	err := f.gate.CheckAppendInTx(f.base.store.ctx, f.base.store.tx, f.entry, f.key)
	if code == "" {
		if err != nil {
			t.Fatal("valid Project gate rejected", err)
		}
	} else {
		hasCode(t, err, code)
	}
	if !reflect.DeepEqual(f.base.store.order, order) {
		t.Fatalf("order=%v want=%v", f.base.store.order, order)
	}
	if f.base.store.unexpected != 0 || before != f.base.snapshot() {
		t.Fatal("initialization audit escaped read-only scope")
	}
	wantCalls := 0
	if len(order) > 0 && order[len(order)-1] == "provider" {
		wantCalls = 1
	}
	if f.calls != wantCalls {
		t.Fatalf("provider calls=%d want=%d", f.calls, wantCalls)
	}
	return err
}

func TestInitializationAuditAuthorityContract(t *testing.T) {
	for _, kind := range []string{"nil-authority", "zero-authority", "nil-facts", "typed-nil-facts"} {
		t.Run(kind, func(t *testing.T) {
			f := newConvergenceFixture(t)
			a := f.authority
			var provider audit.ProjectFactAuthority = auditFactFunc(func(context.Context, foundation.Tx, audit.Entry, audit.AppendKey) error {
				t.Fatal("constructor called facts")
				return nil
			})
			switch kind {
			case "nil-authority":
				a = nil
			case "zero-authority":
				a = &Authority{}
			case "nil-facts":
				provider = nil
			case "typed-nil-facts":
				var p auditFactFunc
				provider = p
			}
			got, err := NewInitializationAuditAuthority(a, provider)
			hasCode(t, err, foundation.DependencyUnbound)
			if got != nil || len(f.store.order) != 0 || f.store.unexpected != 0 {
				t.Fatal("unbound construction has effects")
			}
		})
	}
	for _, kind := range []string{"tx", "entry", "key"} {
		t.Run("invalid_"+kind, func(t *testing.T) {
			f := newInitializationAuditFixture(t, audit.ObjectUploadComplete, nil)
			switch kind {
			case "tx":
				f.base.store.tx = foundation.Tx{}
			case "entry":
				f.entry = audit.Entry{}
			case "key":
				f.key = audit.AppendKey{}
			}
			f.check(t, foundation.InvalidArgument)
		})
	}
	for _, tc := range []struct {
		name   string
		action audit.Action
		change func(*audit.EntryFields, *audit.ObjectMetadataFields, *audit.AppendKeyDetails)
	}{
		{"maintenance", audit.ObjectUploadComplete, func(e *audit.EntryFields, _ *audit.ObjectMetadataFields, k *audit.AppendKeyDetails) {
			e.Actor = convergenceServiceActor(t, identity.ObjectMaintenance, e.Scope, k.CauseRef)
		}},
		{"actor-cause", audit.ObjectUploadComplete, func(_ *audit.EntryFields, _ *audit.ObjectMetadataFields, k *audit.AppendKeyDetails) {
			k.CauseRef = testID[struct{}](t).String()
		}},
		{"upload-digest", audit.ObjectUploadComplete, func(e *audit.EntryFields, _ *audit.ObjectMetadataFields, k *audit.AppendKeyDetails) {
			k.CauseRef = digest([]byte("not upload")).String()
			e.Actor = convergenceServiceActor(t, identity.ObjectService, e.Scope, k.CauseRef)
		}},
		{"upload-ordinal", audit.ObjectUploadComplete, func(_ *audit.EntryFields, _ *audit.ObjectMetadataFields, k *audit.AppendKeyDetails) { k.Ordinal = 1 }},
		{"association", audit.ObjectUploadComplete, func(e *audit.EntryFields, _ *audit.ObjectMetadataFields, _ *audit.AppendKeyDetails) {
			e.Associations.OperationID = testID[struct{}](t).String()
		}},
		{"failed-outcome", audit.ObjectUploadFailed, func(e *audit.EntryFields, _ *audit.ObjectMetadataFields, _ *audit.AppendKeyDetails) {
			e.Outcome = audit.Failed
		}},
		{"failed-ordinal", audit.ObjectUploadFailed, func(_ *audit.EntryFields, _ *audit.ObjectMetadataFields, k *audit.AppendKeyDetails) { k.Ordinal = 2 }},
		{"failed-reason", audit.ObjectUploadFailed, func(_ *audit.EntryFields, m *audit.ObjectMetadataFields, _ *audit.AppendKeyDetails) {
			m.Reason = audit.Cancelled
		}},
		{"delete-uuid", audit.ObjectDelete, func(e *audit.EntryFields, _ *audit.ObjectMetadataFields, k *audit.AppendKeyDetails) {
			k.CauseRef = testID[struct{}](t).String()
			e.Actor = convergenceServiceActor(t, identity.ObjectService, e.Scope, k.CauseRef)
		}},
		{"delete-ordinal", audit.ObjectDelete, func(_ *audit.EntryFields, _ *audit.ObjectMetadataFields, k *audit.AppendKeyDetails) { k.Ordinal = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newInitializationAuditFixture(t, tc.action, tc.change)
			f.check(t, foundation.Forbidden)
		})
	}
}

func TestInitializationAuditAuthorityFacts(t *testing.T) {
	for _, action := range []audit.Action{audit.ObjectUploadComplete, audit.ObjectUploadFailed, audit.ObjectDelete} {
		for _, state := range []c.CreationState{c.CreationAccepted, c.CreationInitializing, c.CreationFailed, c.CreationCompleted} {
			t.Run(string(action)+"/"+string(state), func(t *testing.T) {
				f := newInitializationAuditFixture(t, action, nil)
				f.base.state(state)
				if f.key.Details().CauseRef == f.base.request.CreationID.String() {
					t.Fatal("fixture confused Object cause and Creation")
				}
				if action == audit.ObjectUploadComplete && (state == c.CreationAccepted || state == c.CreationFailed) {
					f.check(t, foundation.InvalidState, "held", "executor", "creation", "project")
				} else {
					f.check(t, "", "held", "executor", "creation", "project", "provider")
				}
			})
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*convergenceFixture)
		code   foundation.Code
		stop   string
	}{
		{"missing-creation", func(f *convergenceFixture) { f.store.creationErr = pgx.ErrNoRows }, foundation.Forbidden, "creation"},
		{"creation-read", func(f *convergenceFixture) { f.store.creationErr = errors.New("read failed") }, foundation.DependencyUnavailable, "creation"},
		{"creation-id", func(f *convergenceFixture) { f.store.creationRow[0] = testID[c.Creation](t).String() }, foundation.DependencyUnavailable, "creation"},
		{"different-project", func(f *convergenceFixture) { f.store.creationRow[1] = testID[identity.Project](t).String() }, foundation.Forbidden, "creation"},
		{"invalid-key", func(f *convergenceFixture) { f.store.creationRow[8] = "bad key" }, foundation.DependencyUnavailable, "creation"},
		{"missing-project", func(f *convergenceFixture) { f.store.projectErr = pgx.ErrNoRows }, foundation.Forbidden, "project"},
		{"project-read", func(f *convergenceFixture) { f.store.projectErr = errors.New("read failed") }, foundation.DependencyUnavailable, "project"},
		{"project-id", func(f *convergenceFixture) { f.store.projectRow[0] = testID[identity.Project](t).String() }, foundation.DependencyUnavailable, "project"},
		{"reverse-creation", func(f *convergenceFixture) { f.store.projectRow[11] = testID[c.Creation](t).String() }, foundation.DependencyUnavailable, "project"},
		{"owner", func(f *convergenceFixture) { f.store.projectRow[1] = testID[identity.User](t).String() }, foundation.DependencyUnavailable, "project"},
		{"deleting", func(f *convergenceFixture) { f.store.projectRow[5] = "deleting" }, foundation.Forbidden, "project"},
		{"premature-initialized", func(f *convergenceFixture) { f.store.projectRow[12] = true }, foundation.DependencyUnavailable, "project"},
		{"protected-skill-only", func(f *convergenceFixture) { id := testID[c.Skill](t).String(); f.store.creationRow[9] = &id }, foundation.DependencyUnavailable, "project"},
		{"protected-revision-only", func(f *convergenceFixture) { n := int64(1); f.store.creationRow[10] = &n }, foundation.DependencyUnavailable, "project"},
		{"request-name", func(f *convergenceFixture) { v := "Different"; f.store.creationRow[5] = &v }, foundation.DependencyUnavailable, "project"},
		{"request-description", func(f *convergenceFixture) { v := "Different"; f.store.creationRow[6] = &v }, foundation.DependencyUnavailable, "project"},
		{"missing-ex", func(f *convergenceFixture) { f.store.lockErr = errors.New("missing held lock") }, foundation.DependencyUnavailable, "held"},
		{"foreign-tx", func(f *convergenceFixture) { f.store.executorErr = errors.New("foreign transaction") }, foundation.DependencyUnavailable, "executor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newInitializationAuditFixture(t, audit.ObjectUploadFailed, nil)
			tc.mutate(f.base)
			order := []string{"held", "executor", "creation", "project"}
			for i, v := range order {
				if v == tc.stop {
					order = order[:i+1]
					break
				}
			}
			f.check(t, tc.code, order...)
		})
	}
	t.Run("completed-historical-result", func(t *testing.T) {
		f := newInitializationAuditFixture(t, audit.ObjectUploadComplete, nil)
		f.base.state(c.CreationCompleted)
		f.base.store.projectRow[2], f.base.store.projectRow[3], f.base.store.projectRow[4], f.base.store.projectRow[6] = "Later", "later", "later description", int64(3)
		f.check(t, "", "held", "executor", "creation", "project", "provider")
	})
	for _, kind := range []string{"fault", "error", "cancel"} {
		t.Run("provider-"+kind, func(t *testing.T) {
			f := newInitializationAuditFixture(t, audit.ObjectUploadFailed, nil)
			code := foundation.DependencyUnavailable
			switch kind {
			case "fault":
				f.providerError = fault(foundation.Forbidden)
				code = foundation.Forbidden
			case "error":
				f.providerError = errors.New("provider read failed")
			case "cancel":
				f.providerError = context.Canceled
			}
			err := f.check(t, code, "held", "executor", "creation", "project", "provider")
			if !errors.Is(err, f.providerError) {
				t.Fatal("provider error cause lost")
			}
		})
	}
	t.Run("caller-canceled", func(t *testing.T) {
		f := newInitializationAuditFixture(t, audit.ObjectUploadFailed, nil)
		ctx, cancel := context.WithCancel(f.base.store.ctx)
		cancel()
		f.base.store.ctx = ctx
		if err := f.check(t, foundation.DependencyUnavailable, "held"); !errors.Is(err, context.Canceled) {
			t.Fatal("caller cancellation lost")
		}
	})
}

func TestInitializationAuditAuthorityDelegation(t *testing.T) {
	for _, kind := range []identity.ActorKind{identity.Human, identity.AgentRun} {
		t.Run(string(kind), func(t *testing.T) {
			f := newInitializationAuditFixture(t, audit.ObjectUploadComplete, func(_ *audit.EntryFields, m *audit.ObjectMetadataFields, _ *audit.AppendKeyDetails) {
				m.InitiatorKind = kind
				if kind == identity.AgentRun {
					m.InitiatorExecutionID = testID[struct{}](t).String()
				}
			})
			f.check(t, foundation.DependencyUnbound)
		})
	}
	t.Run("ordinary-producer", func(t *testing.T) {
		f := newInitializationAuditFixture(t, audit.ObjectUploadComplete, func(_ *audit.EntryFields, _ *audit.ObjectMetadataFields, k *audit.AppendKeyDetails) {
			k.Producer = audit.ArtifactProducer
		})
		hasCode(t, f.base.authority.CheckAppendInTx(f.base.store.ctx, f.base.store.tx, f.entry, f.key), foundation.Forbidden)
		f.check(t, foundation.Forbidden)
	})
	t.Run("secret-context-and-current-owner", func(t *testing.T) {
		type witness struct{}
		ctx := context.WithValue(context.Background(), witness{}, "private secret witness")
		calls := 0
		sentinel := fault(foundation.Forbidden)
		var entry audit.Entry
		var key audit.AppendKey
		var f *auditGateFixture
		f = newAuditGateFixture(t, auditFactFunc(func(got context.Context, tx foundation.Tx, e audit.Entry, k audit.AppendKey) error {
			calls++
			if got != ctx || tx != f.store.tx || !e.Fields().Actor.Equal(entry.Fields().Actor) || k.Details() != key.Details() {
				t.Fatal("ordinary dispatch changed")
			}
			return sentinel
		}))
		entry, key = auditGateEntry(t, f, f.actor, audit.SecretUpdate)
		gate, err := NewInitializationAuditAuthority(f.a, auditFactFunc(func(context.Context, foundation.Tx, audit.Entry, audit.AppendKey) error {
			t.Fatal("Secret borrowed init facts")
			return nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err = gate.CheckAppendInTx(ctx, f.store.tx, entry, key); err != sentinel || calls != 1 {
			t.Fatal("ordinary error/provider changed", err, calls)
		}
		if !reflect.DeepEqual(f.order[:3], []string{"locks", "session", "executor"}) {
			t.Fatal("ordinary current-owner gate changed", f.order)
		}
	})
	t.Run("other-methods", func(t *testing.T) {
		f := newInitializationAuditFixture(t, audit.ObjectUploadComplete, nil)
		ctx, tx, actor, scope := f.base.store.ctx, f.base.store.tx, f.base.actor, f.entry.Fields().Scope
		_, want := f.base.authority.AuthorizeProject(ctx, tx, actor, f.base.request.ProjectID, identity.Read)
		_, got := f.gate.AuthorizeProject(ctx, tx, actor, f.base.request.ProjectID, identity.Read)
		var w, g *foundation.Fault
		if !errors.As(want, &w) || !errors.As(got, &g) || w.Code != g.Code {
			t.Fatal("AuthorizeProject behavior changed")
		}
		hasCode(t, f.gate.CheckServiceLookup(ctx, actor, scope, f.key), foundation.DependencyUnbound)
		hasCode(t, f.gate.CheckCleanupInTx(ctx, tx, actor, audit.LifecycleCause{}, f.base.request.ProjectID), foundation.DependencyUnbound)
		if f.calls != 0 || len(f.base.store.order) != 0 || f.base.store.unexpected != 0 {
			t.Fatal("ordinary service paths borrowed init facts")
		}
	})
}
