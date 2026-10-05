package secret

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type nonComparableProjectAuditStore struct {
	Store
	values []int
}

type dynamicProjectAuditStore struct {
	Store
	identity any
}

func TestSecretProjectAuditConstructionAndTransactionWitness(t *testing.T) {
	var typedNil *projectAuditUnitStore
	for _, s := range []Store{nil, typedNil} {
		_, err := NewProjectAuditAuthority(s)
		projectAuditCode(t, err, foundation.DependencyUnbound)
	}
	for _, s := range []Store{nonComparableProjectAuditStore{}, dynamicProjectAuditStore{identity: []int{1}}} {
		_, err := NewProjectAuditAuthority(s)
		projectAuditCode(t, err, foundation.InvalidArgument)
		if sameProjectAuditStore(s, s) {
			t.Fatal("unsafe Store identity accepted")
		}
	}
	f := newProjectAuditMutation(t, sc.Create, false)
	projectAuditCode(t, new(ProjectAuditAuthority).CheckProjectAuditInTx(f.ctx, f.store.tx, f.entry, f.key), foundation.DependencyUnbound)
	projectAuditCode(t, f.checker.CheckProjectAuditInTx(context.Background(), f.store.tx, f.entry, f.key), foundation.Forbidden)
	other := &projectAuditUnitStore{tx: f.store.tx}
	checker, _ := NewProjectAuditAuthority(other)
	projectAuditCode(t, checker.CheckProjectAuditInTx(f.ctx, f.store.tx, f.entry, f.key), foundation.Forbidden)
	projectAuditCode(t, f.checker.CheckProjectAuditInTx(f.ctx, foundation.NewTx(), f.entry, f.key), foundation.Forbidden)
	old := f.store.tx
	f.store.tx = foundation.Tx{}
	projectAuditCode(t, f.checker.CheckProjectAuditInTx(f.ctx, old, f.entry, f.key), foundation.DependencyUnavailable)
	f.store.tx = old
	f.store.locks = nil
	projectAuditCode(t, f.checker.CheckProjectAuditInTx(f.ctx, old, f.entry, f.key), foundation.DependencyUnavailable)
	if f.store.queries != 0 {
		t.Fatal("rejected witness/locks read durable facts")
	}
}

func TestSecretProjectAuditCompleteEntryBinding(t *testing.T) {
	f := newProjectAuditMutation(t, sc.Update, true)
	newAssociation := projectAuditID[struct{}](t).String()
	changes := map[string]func(*ac.EntryFields){
		"session": func(x *ac.EntryFields) {
			user, _ := foundation.ParseID[identity.User](x.Actor.Details().UserID)
			x.Actor, _ = identity.NewHuman(user, projectAuditID[identity.Session](t))
		},
		"actor": func(x *ac.EntryFields) {
			x.Actor, _ = identity.NewHuman(projectAuditID[identity.User](t), projectAuditID[identity.Session](t))
		},
		"scope": func(x *ac.EntryFields) { x.Scope, _ = identity.InProject(projectAuditID[identity.Project](t)) },
		"resource": func(x *ac.EntryFields) {
			x.Resource, _ = ac.NewResource(ac.SecretResource, projectAuditID[sc.Credential](t).String())
		},
		"changed-fields": func(x *ac.EntryFields) {
			x.Metadata, _ = ac.SecretMutationMetadata(ac.SecretUpdate, 8, []ac.ChangedField{ac.ValueChanged})
		},
		"version": func(x *ac.EntryFields) {
			x.Metadata, _ = ac.SecretMutationMetadata(ac.SecretUpdate, 9, []ac.ChangedField{ac.ValueChanged, ac.PurposeChanged})
		},
		"outcome": func(x *ac.EntryFields) { x.Outcome = ac.Denied },
		"action": func(x *ac.EntryFields) {
			x.Action = ac.SecretDelete
			x.Metadata, _ = ac.SecretMutationMetadata(ac.SecretDelete, 8, nil)
		},
		"tool":        func(x *ac.EntryFields) { x.Associations.ToolID = newAssociation },
		"execution":   func(x *ac.EntryFields) { x.Associations.ExecutionID = newAssociation },
		"tool-call":   func(x *ac.EntryFields) { x.Associations.ToolCallID = newAssociation },
		"operation":   func(x *ac.EntryFields) { x.Associations.OperationID = newAssociation },
		"request":     func(x *ac.EntryFields) { x.Associations.RequestID = newAssociation },
		"approval":    func(x *ac.EntryFields) { x.Associations.ApprovalID = newAssociation },
		"runner":      func(x *ac.EntryFields) { x.Associations.RunnerID = newAssociation },
		"correlation": func(x *ac.EntryFields) { x.Associations.CorrelationID = newAssociation },
		"http-trace":  func(x *ac.EntryFields) { x.Associations.HTTPTraceID = newAssociation },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			fields := f.entry.Fields()
			change(&fields)
			entry, err := ac.NewEntry(fields)
			if err != nil {
				t.Fatal(err)
			}
			if name == "session" || name == "http-trace" {
				a, _ := audit.SemanticDigest(f.entry)
				b, _ := audit.SemanticDigest(entry)
				if a != b {
					t.Fatal("test must cover semantic digest's intentional exclusion")
				}
			}
			projectAuditCode(t, f.checker.CheckProjectAuditInTx(f.ctx, f.store.tx, entry, f.key), foundation.Forbidden)
		})
	}
	for _, k := range []ac.AppendKeyDetails{{Producer: ac.SecretProducer, CauseRef: projectAuditID[struct{}](t).String(), Ordinal: 0}, {Producer: ac.SecretProducer, CauseRef: f.key.Details().CauseRef, Ordinal: 1}, {Producer: ac.ObjectProducer, CauseRef: f.key.Details().CauseRef, Ordinal: 0}} {
		key, _ := ac.NewAppendKey(k.Producer, k.CauseRef, k.Ordinal)
		projectAuditCode(t, f.checker.CheckProjectAuditInTx(f.ctx, f.store.tx, f.entry, key), foundation.Forbidden)
	}
	if f.store.queries != 0 {
		t.Fatal("changed entry/key should reject before reading facts")
	}
}

func TestSecretProjectAuditResolutionEvidence(t *testing.T) {
	f := newProjectAuditMutation(t, sc.Create, false)
	ref := f.p.ref
	id := projectAuditID[sc.Lease](t)
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, projectAuditID[struct{}](t).String())
	if err != nil {
		t.Fatal(err)
	}
	registration, _ := identity.RegisterService(identity.SecretService)
	caller, _ := registration.Actor(owner.Details().ID, ref.Details().Scope)
	resolution := projectAuditID[struct{}](t).String()
	subject, _ := registration.Actor(resolution, ref.Details().Scope)
	lease := leaseRecord{ref: ref, owner: owner, consumer: sc.Model}
	grant := sc.UseGrant{Subject: caller, Consumer: sc.Model, RequestID: projectAuditID[struct{}](t).String(), OperationID: projectAuditID[struct{}](t).String(), ToolID: projectAuditID[struct{}](t).String(), RunnerID: projectAuditID[struct{}](t).String()}
	metadata, _ := ac.SecretResolveMetadata(id.String(), ac.Consumer(sc.Model), "")
	resource, _ := ac.NewResource(ac.SecretResource, ref.Details().ID.String())
	entry, err := ac.NewEntry(ac.EntryFields{Scope: ref.Details().Scope, Actor: subject, Action: ac.SecretResolve, Outcome: ac.Success, Resource: resource, Metadata: metadata, Associations: ac.Associations{RequestID: grant.RequestID, OperationID: grant.OperationID, ToolID: grant.ToolID, RunnerID: grant.RunnerID}})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := ac.NewAppendKey(ac.SecretProducer, resolution, 0)
	ctx := resolutionAuditContext(context.Background(), f.store, f.store.tx, resolution, caller, id, lease, f.result.Metadata, f.p.value.id, grant, entry, key)
	read := f.store.read
	f.store.read = func(sql string, args []any) postgres.Row {
		if strings.Contains(sql, "FROM agenteam_secret.secret_leases") {
			return projectAuditUnitRow{values: []any{ref.Details().ID.String(), "project", ref.Details().Scope.Details().ProjectID, string(owner.Details().Kind), owner.Details().ID, string(sc.Model), false}}
		}
		return read(sql, args)
	}
	if err := f.checker.CheckProjectAuditInTx(ctx, f.store.tx, entry, key); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"resolution", "caller", "lease-owner", "grant-consumer", "grant-association", "metadata-version", "payload", "released"} {
		t.Run(name, func(t *testing.T) {
			w := ctx.Value(projectAuditWitnessKey{}).(projectAuditWitness)
			r := *w.resolution
			w.resolution = &r
			switch name {
			case "resolution":
				r.resolution = projectAuditID[struct{}](t).String()
			case "caller":
				r.caller, _ = registration.Actor(projectAuditID[struct{}](t).String(), ref.Details().Scope)
			case "lease-owner":
				r.lease.owner, _ = sc.NewCredentialLeaseOwner(owner.Details().Kind, projectAuditID[struct{}](t).String())
			case "grant-consumer":
				r.grant.Consumer = sc.MCP
			case "grant-association":
				r.grant.RequestID = projectAuditID[struct{}](t).String()
			case "metadata-version":
				r.metadata.Version++
			case "payload":
				r.payload = projectAuditID[payloadMarker](t)
			case "released":
				r.lease.released = true
			}
			badCtx := context.WithValue(ctx, projectAuditWitnessKey{}, w)
			if err := f.checker.CheckProjectAuditInTx(badCtx, f.store.tx, entry, key); err == nil {
				t.Fatal("unproved resolution accepted")
			}
		})
	}
}

func TestSecretProjectAuditWitnessRetainsNoMaterialAndSkipsSystem(t *testing.T) {
	f := newProjectAuditMutation(t, sc.Create, false)
	f.p.value.ciphertext = []byte("forbidden-sealed-value")
	f.p.receipt.ciphertext = []byte("forbidden-sealed-receipt")
	ctx := mutationAuditContext(context.Background(), f.store, f.store.tx, f.p, f.prior, f.result, f.entry, f.key)
	w := ctx.Value(projectAuditWitnessKey{}).(projectAuditWitness)
	for _, format := range []string{"%v", "%+v", "%#v"} {
		if got := fmt.Sprintf(format, w); got != "secret_project_audit_witness" {
			t.Fatal("unsafe witness formatting")
		}
	}
	f.p.scope = identity.SystemScope()
	base := context.Background()
	if mutationAuditContext(base, f.store, f.store.tx, f.p, f.prior, f.result, f.entry, f.key) != base {
		t.Fatal("System path changed")
	}
}
