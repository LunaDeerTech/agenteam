package skill

import (
	"context"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func gatedCleanupFixture(t *testing.T) (*Service, *cleanupTestStore, *cleanupTestObjects, *cleanupTestProjects, pc.ScopeRef) {
	t.Helper()
	s, store, o, p, scope := cleanupFixture(t)
	store.c = &cleanupRow{id: stateID[oc.CleanupOperation](230), project: scope.ProjectID, cause: p.cause, skill: store.r.skill, revision: store.r.revision, object: store.r.object, upload: store.r.upload, phase: cleanupGated, version: 1}
	store.serving, store.gated = false, true
	return s, store, o, p, scope
}

func TestSkillCleanupPlansRevalidateCurrentFacts(t *testing.T) {
	for _, name := range []string{"current", "permission_revoked", "weak_skill_lock", "changed_attempt", "changed_cleanup", "purge_before_completed", "physical_after_completed", "missing_parent", "foreign_tx"} {
		t.Run(name, func(t *testing.T) {
			s, store, _, p, _ := gatedCleanupFixture(t)
			cause, _ := store.c.objectCause(store.r)
			request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.CleanupObjectAccess, Cleanup: cause, ObjectID: store.r.object})
			if err != nil {
				t.Fatal(err)
			}
			deps, err := s.state().authority.Discover(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			store.live = true
			store.tx = f.NewTx()
			store.held = map[string]f.LockMode{}
			if err = store.AcquireAll(context.Background(), store.tx, deps.Locks()); err != nil {
				t.Fatal(err)
			}
			tx := store.tx
			switch name {
			case "permission_revoked":
				p.denied = f.NewFault(f.CommitUnknown, f.Unknown)
			case "weak_skill_lock":
				store.held[skillLock(store.r.skill, f.Exclusive).Key.Canonical()] = f.Shared
			case "changed_attempt":
				store.row.values[18] = stateID[oc.Attempt](231).String()
			case "changed_cleanup":
				store.c.id = stateID[oc.CleanupOperation](232)
			case "purge_before_completed":
				request, _ = oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.PurgeDeletedObjectMetadataAccess, Cleanup: cause, ObjectID: store.r.object})
			case "physical_after_completed":
				store.c.phase = cleanupCompleted
			case "missing_parent":
				store.core = false
			case "foreign_tx":
				tx = f.NewTx()
			}
			err = s.state().authority.ValidateInTx(context.Background(), tx, request, deps)
			if name == "current" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("stale authorization accepted")
			}
			if name == "permission_revoked" && err != p.denied {
				t.Fatal("current original Unknown changed", err)
			}
		})
	}
}

func TestSkillCleanupMaintenanceRetainsOriginalCandidateIdentity(t *testing.T) {
	s, store, _, projects, _ := gatedCleanupFixture(t)
	old := stateID[oc.Attempt](233)
	store.attempts[old.String()] = true
	r, err := oc.NewMaintenanceAccess(oc.AccessRequestDetails{Operation: oc.CheckpointCleanupAccess, ObjectID: store.r.object, InstanceID: stateID[oc.Process](234), AttemptID: old, CleanupID: stateID[oc.CleanupOperation](235), WorkerID: stateID[oc.CleanupOperation](236), Fence: 7})
	if err != nil {
		t.Fatal(err)
	}
	deps, err := s.state().authority.Discover(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	store.live = true
	store.tx = f.NewTx()
	store.held = map[string]f.LockMode{}
	if err = store.AcquireAll(context.Background(), store.tx, deps.Locks()); err != nil {
		t.Fatal(err)
	}
	if err = s.state().authority.ValidateInTx(context.Background(), store.tx, r, deps); err != nil {
		t.Fatal("old candidate is not current cleanup worker", err)
	}
	d := r.Details()
	d.Fence++
	other, _ := oc.NewMaintenanceAccess(d)
	if err = s.state().authority.ValidateInTx(context.Background(), store.tx, other, deps); err == nil {
		t.Fatal("checkpoint fence drift accepted")
	}
	store.c.phase = cleanupCompleted
	if _, err = s.state().authority.Discover(context.Background(), r); err == nil {
		t.Fatal("new physical maintenance after completed")
	}
	if err = s.state().authority.CheckProjectCleanupInTx(context.Background(), store.tx, projects.actor, oc.ProjectCleanupCause{}); err == nil {
		t.Fatal("revision gate authorized whole project")
	}
}

type cleanupAuditDelegate struct {
	ac.ProjectAuthority
	calls int
	err   error
}

func (d *cleanupAuditDelegate) CheckAppendInTx(context.Context, f.Tx, ac.Entry, ac.AppendKey) error {
	d.calls++
	return d.err
}

func TestSkillCleanupAuditUsesNativeWitnessAndCurrentGate(t *testing.T) {
	for _, name := range []string{"forward_original", "native_missing_witness", "current_denied", "wrong_creation", "wrong_size", "completed", "other_action"} {
		t.Run(name, func(t *testing.T) {
			s, store, _, p, _ := gatedCleanupFixture(t)
			store.live = true
			store.tx = f.NewTx()
			store.held = map[string]f.LockMode{}
			locks, _ := store.r.locks(f.Exclusive, store.r.object)
			if err := store.AcquireAll(context.Background(), store.tx, locks); err != nil {
				t.Fatal(err)
			}
			action := ac.ObjectDelete
			if name == "other_action" {
				action = ac.ObjectUploadComplete
			}
			entry, key := auditEntry(t, store.r, action, func(_ *ac.EntryFields, m *ac.ObjectMetadataFields, _ *ac.AppendKeyDetails) {
				if name == "wrong_creation" {
					m.InitiatorID = stateID[pc.Creation](237).String()
				}
				if name == "wrong_size" {
					m.ByteSize++
				}
			})
			ctx := context.WithValue(context.Background(), struct{}{}, "original-native-witness-context")
			calls := 0
			sentinel := f.NewFault(f.DependencyUnavailable, f.Unknown)
			var facts ac.ProjectFactAuthority = auditFactControl(func(c context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) error {
				calls++
				if c != ctx || tx != store.tx || !e.Fields().Actor.Equal(entry.Fields().Actor) || string(e.Fields().Metadata.JSON()) != string(entry.Fields().Metadata.JSON()) || k.Details() != key.Details() {
					t.Fatal("replaced original native inputs")
				}
				return sentinel
			})
			if name == "native_missing_witness" {
				var err error
				facts, err = object.NewProjectAuditAuthority(store)
				if err != nil {
					t.Fatal(err)
				}
			}
			if name == "current_denied" {
				p.denied = sentinel
			}
			if name == "completed" {
				store.c.phase = cleanupCompleted
			}
			delegate := &cleanupAuditDelegate{err: sentinel}
			outer, err := NewLifecycleAuditAuthority(delegate, s.state().authority, facts)
			if err != nil {
				t.Fatal(err)
			}
			err = outer.CheckAppendInTx(ctx, store.tx, entry, key)
			if name == "other_action" {
				if err != sentinel || delegate.calls != 1 || calls != 0 {
					t.Fatal("ordinary delegate changed", err)
				}
				return
			}
			if err == nil || delegate.calls != 0 {
				t.Fatal("unproven native deletion accepted", err)
			}
			if name == "forward_original" {
				if err != sentinel || calls != 1 {
					t.Fatal("native fault lost", err)
				}
			} else if calls != 0 {
				t.Fatal("denied shape reached native checker")
			}
		})
	}
}

func TestSkillCleanupCheckpointIsNotAuthority(t *testing.T) {
	_, store, _, p, scope := gatedCleanupFixture(t)
	report, err := cleanupReport(p.cause, scope, store.c, false)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := report.Details().Checkpoint
	if _, err = parseCleanupCheckpoint(checkpoint, p.cause, scope); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{string(checkpoint.Bytes()) + " ", strings.Replace(string(checkpoint.Bytes()), `"project":`, `"Project":`, 1), strings.Replace(string(checkpoint.Bytes()), `"phase":"gated"`, `"phase":"gated","phase":"gated"`, 1), `{}`} {
		candidate, err := pc.NewCleanupCheckpoint(pc.SkillsParticipant, p.cause, scope, 1, []byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = parseCleanupCheckpoint(&candidate, p.cause, scope); err == nil {
			t.Fatal("noncanonical checkpoint accepted")
		}
	}
	copy := checkpoint.Bytes()
	copy[0] = '!'
	if _, err = parseCleanupCheckpoint(checkpoint, p.cause, scope); err != nil {
		t.Fatal("checkpoint alias mutated", err)
	}
}
