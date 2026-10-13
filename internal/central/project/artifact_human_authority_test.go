package project

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestArtifactHumanCancellationCurrentGate(t *testing.T) {
	for _, mode := range []string{"active", "archived", "deleting", "initializing", "revoked", "archived-mutation"} {
		t.Run(mode, func(t *testing.T) {
			v := newAuditGateFixture(t, nil)
			authority, err := NewArtifactAuthority(v.a)
			if err != nil {
				t.Fatal(err)
			}
			intent, want := i.Converge, f.Code("")
			switch mode {
			case "archived":
				v.lifecycle = c.Archived
			case "deleting":
				v.lifecycle, want = c.Deleting, f.ProjectNotActive
			case "initializing":
				v.initialized, want = false, f.ProjectNotActive
			case "revoked":
				v.sessionErr, want = fault(f.SessionRevoked), f.SessionRevoked
			case "archived-mutation":
				v.lifecycle, intent, want = c.Archived, i.Mutate, f.ProjectNotActive
			}
			grant, err := authority.AuthorizeInTx(context.Background(), v.store.tx, ac.AccessSubject{Actor: v.actor, ProjectID: v.project}, intent)
			if want != "" {
				hasCode(t, err, want)
				return
			}
			scope, _ := i.InProject(v.project)
			if err != nil || !grant.Matches(v.actor, scope, i.Converge) || grant.Matches(v.actor, scope, i.Mutate) {
				t.Fatal("current cancellation became a mutation grant or lost ownership", err)
			}
		})
	}
}

func TestArtifactHumanBindingDoesNotGrantProvenanceOrMaintenance(t *testing.T) {
	for _, mode := range []string{"execution", "operation", "tool", "tool-call", "maintenance-label"} {
		t.Run(mode, func(t *testing.T) {
			v := newAuditGateFixture(t, nil)
			authority, err := NewArtifactAuthority(v.a)
			if err != nil {
				t.Fatal(err)
			}
			subject := ac.AccessSubject{Actor: v.actor, ProjectID: v.project}
			id := testID[struct{}](t).String()
			intent, want := i.Read, f.DependencyUnbound
			switch mode {
			case "execution":
				subject.ExecutionID = id
			case "operation":
				subject.ExecutionID, subject.OperationID = id, testID[struct{}](t).String()
			case "tool":
				subject.ToolID = id
			case "tool-call":
				subject.ToolID, subject.ToolCallID = id, testID[struct{}](t).String()
			case "maintenance-label":
				scope, _ := i.InProject(v.project)
				registration, _ := i.RegisterService(i.ObjectMaintenance)
				subject.Actor, err = registration.Actor(id, scope)
				if err != nil {
					t.Fatal(err)
				}
				intent, want = i.Converge, f.Forbidden
			}
			_, err = authority.AuthorizeInTx(context.Background(), v.store.tx, subject, intent)
			hasCode(t, err, want)
			for _, step := range v.order {
				if step == "query" || step == "session" {
					t.Fatal("unbound provenance or a service label reached current business facts", v.order)
				}
			}
		})
	}
}

func TestArtifactMaintenancePlansExclusiveWithoutGrant(t *testing.T) {
	v := newAuditGateFixture(t, nil)
	authority, err := NewArtifactAuthority(v.a)
	if err != nil {
		t.Fatal(err)
	}
	subject := ac.AccessSubject{ProjectID: v.project, Maintenance: true}
	deps, err := authority.Discover(context.Background(), subject)
	if err != nil || len(deps.Locks()) != 1 || f.CompareLockKeys(deps.Locks()[0].Key, projectLock(v.project, f.Exclusive).Key) != 0 || deps.Locks()[0].Mode != f.Exclusive {
		t.Fatal("maintenance did not plan its original Converge lock before Tx", err)
	}
	if _, err = authority.AuthorizeInTx(context.Background(), v.store.tx, subject, i.Converge); err == nil {
		t.Fatal("discovery-only request issued a grant")
	}
}

func TestArtifactHumanCollectionUsesCurrentOwnerAndOriginalFacts(t *testing.T) {
	for _, mode := range []string{"active", "archived", "deleting", "revoked", "missing-facts", "original-facts-error"} {
		t.Run(mode, func(t *testing.T) {
			v := newAuditGateFixture(t, nil)
			entry, key := artifactAuditGateEntry(t, v, au.ArtifactList)
			type witnessKey struct{}
			ctx := context.WithValue(context.Background(), witnessKey{}, new(int))
			calls := 0
			original := fault(f.ResourceBusy)
			v.a.state().auditFacts[au.ArtifactProducer] = auditFactFunc(func(got context.Context, tx f.Tx, actual au.Entry, appendKey au.AppendKey) error {
				calls++
				if got != ctx || tx != v.store.tx || actual.Fields().Resource.Details() != entry.Fields().Resource.Details() || appendKey.Details() != key.Details() {
					t.Fatal("original page witness/Tx/key changed")
				}
				if mode == "original-facts-error" {
					return original
				}
				return nil
			})
			want := f.Code("")
			switch mode {
			case "archived":
				v.lifecycle = c.Archived
			case "deleting":
				v.lifecycle, want = c.Deleting, f.ProjectNotActive
			case "revoked":
				v.sessionErr, want = fault(f.SessionRevoked), f.SessionRevoked
			case "missing-facts":
				delete(v.a.state().auditFacts, au.ArtifactProducer)
				want = f.DependencyUnbound
			case "original-facts-error":
				want = f.ResourceBusy
			}
			err := v.a.CheckAppendInTx(ctx, v.store.tx, entry, key)
			if want == "" {
				if err != nil || calls != 1 {
					t.Fatal("current collection dispatch failed", err, calls)
				}
			} else {
				hasCode(t, err, want)
				if mode == "original-facts-error" {
					if calls != 1 || err != original {
						t.Fatal("fact error changed", err)
					}
				} else if calls != 0 {
					t.Fatal("rejected current owner reached facts")
				}
			}
		})
	}
}
