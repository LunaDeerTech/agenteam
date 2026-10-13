package project

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestArtifactHumanCancellationCurrentGate(t *testing.T) {
	for _, mode := range []string{"active", "archived", "archiving", "deleting", "initializing", "revoked", "archived-mutation"} {
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
			case "archiving":
				v.lifecycle, want = c.Archiving, f.ProjectNotActive
				if _, err := authority.AuthorizeInTx(context.Background(), v.store.tx, ac.AccessSubject{Actor: v.actor, ProjectID: v.project}, i.Read); err != nil {
					t.Fatal("ordinary current-owner Read must remain available while archiving", err)
				}
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

func TestArtifactCancelledMaintenanceCurrentGate(t *testing.T) {
	for _, state := range []c.Lifecycle{c.Active, c.Archived, c.Archiving} {
		t.Run(string(state), func(t *testing.T) {
			v := newAuditGateFixture(t, nil)
			v.lifecycle = state
			authority, err := NewArtifactAuthority(v.a)
			if err != nil {
				t.Fatal(err)
			}
			ownerID, fileID := testID[ac.ArtifactEntity](t), testID[ac.File](t)
			objectID, uploadID := testID[oc.StoredObject](t), testID[oc.Upload](t)
			attempt, cause := testID[oc.Attempt](t), testID[struct{}](t)
			owner, err := oc.NewObjectOwner(oc.Artifact, ownerID.String(), v.project.String())
			if err != nil {
				t.Fatal(err)
			}
			registration, _ := i.RegisterService(i.ObjectMaintenance)
			actor, err := registration.Actor(uploadID.String(), owner.Scope())
			if err != nil {
				t.Fatal(err)
			}
			key := f.IdempotencyKey("cancelled-maintenance-gate")
			identity, err := f.NewCommandIdentity("artifact", []string{v.project.String()}, "create", key)
			if err != nil {
				t.Fatal(err)
			}
			commandHash := sha256.Sum256([]byte(identity.Canonical()))
			requestDigest, contentDigest := sha256.Sum256([]byte("original-command")), sha256.Sum256([]byte("body"))
			stableActor := "human:" + v.actor.Details().UserID
			created := time.Now().UTC()
			originalRow := v.store.row
			commandReads, projectReads := 0, 0
			// Only durable rows are controlled. The real Artifact owner issues its
			// private proof, and the real Project adapter consumes it in this Tx.
			v.store.row = func(query string, args ...any) postgres.Row {
				switch {
				case strings.HasPrefix(query, "SELECT id,project_id,creation_cause"):
					if len(args) != 1 || args[0] != ownerID.String() {
						t.Fatal("wrong original owner lookup")
					}
					return valuesRow(ownerID.String(), v.project.String(), cause.String(), stableActor, objectID.String(), uploadID.String(), string(key), "cancel_requested", "prospective", int64(1))
				case strings.Contains(query, "FROM agenteam_artifact.commands WHERE command_hash="):
					if len(args) != 1 {
						t.Fatal("wrong original command lookup")
					}
					hash, ok := args[0].([]byte)
					if !ok || !bytes.Equal(hash, commandHash[:]) {
						t.Fatal("original command identity changed")
					}
					commandReads++
					return valuesRow(string(key), v.project.String(), ownerID.String(), fileID.String(), stableActor, requestDigest[:], cause.String(), "content", string(ac.Generated), "cancel_requested", "Original", "", "text/plain", int64(4), contentDigest[:], []byte(nil), "", objectID.String(), uploadID.String(), attempt.String(), string(i.Human), v.actor.Details().UserID, "", "", "", "", created)
				case strings.Contains(query, "FROM agenteam_project.projects"):
					projectReads++
					return originalRow(query, args...)
				case query == "SELECT clock_timestamp()":
					return originalRow(query, args...)
				default:
					t.Fatal("unexpected maintenance query")
					return nil
				}
			}
			provider, err := artifact.NewOwnerProvider(v.store, authority)
			if err != nil {
				t.Fatal(err)
			}
			grant, err := provider.AuthorizeOwnerInTx(context.Background(), v.store.tx, actor, owner, i.Converge)
			if commandReads != 2 || projectReads != 1 {
				t.Fatal("maintenance did not reach the current gate after exact private proof", commandReads, projectReads, err)
			}
			if state == c.Archiving {
				hasCode(t, err, f.ProjectNotActive)
			} else if err != nil || grant.Details().Intent != i.Converge || !grant.Details().Actor.Equal(actor) {
				t.Fatal("valid original cancellation convergence rejected", err)
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
