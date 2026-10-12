package project

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func artifactCleanupRecord(t *testing.T) *lifecycleRecord {
	t.Helper()
	r := secretCleanupRecord(t)
	entries := r.manifest.Entries()
	for n := range entries {
		if entries[n].Name == c.ArtifactObjectParticipant {
			entries[n].ReferenceKinds = []c.ReferenceKind{"artifact", "object"}
		}
	}
	r.manifest = registryTestManifest(t, entries)
	return r
}

func TestArtifactCleanupProgressAndDomains(t *testing.T) {
	for _, name := range []string{"sibling-required", "sibling-completed", "parent-pending", "artifact-pending", "already-completed", "missing-parent-edge", "declared-extra-edge", "late-other-domain", "missing-row", "failed", "outbox"} {
		t.Run(name, func(t *testing.T) {
			r := artifactCleanupRecord(t)
			want := f.Code("")
			switch name {
			case "sibling-completed":
				skillsCleanupSet(r, c.SecretParticipant, "stopped", "completed")
			case "parent-pending":
				skillsCleanupSet(r, c.SkillsParticipant, "stopped", "pending")
				want = f.InvalidState
			case "artifact-pending":
				skillsCleanupSet(r, c.ArtifactObjectParticipant, "stopped", "pending")
			case "already-completed":
				skillsCleanupSet(r, c.ArtifactObjectParticipant, "stopped", "completed")
				want = f.InvalidState
			case "missing-parent-edge", "declared-extra-edge", "late-other-domain":
				entries := r.manifest.Entries()
				for n := range entries {
					if entries[n].Name == c.ArtifactObjectParticipant {
						if name == "missing-parent-edge" {
							entries[n].CleanupAfter = nil
						} else if name == "declared-extra-edge" {
							entries[n].CleanupAfter = append(entries[n].CleanupAfter, c.SecretParticipant)
						}
					}
					if entries[n].Name == c.OutboxParticipant && name == "late-other-domain" {
						entries[n].CleanupAfter = append(entries[n].CleanupAfter, "later-domain")
					}
				}
				if name == "late-other-domain" {
					entries = append(entries, c.ParticipantRegistration{Name: "later-domain", ContractVersion: 1, OwnerModule: "later-domain"})
					r.participants = append(r.participants, lifecycleParticipantRecord{name: "later-domain", stop: "stopped", cleanup: "pending"})
					skillsCleanupSet(r, c.SecretParticipant, "stopped", "completed")
				} else if name == "missing-parent-edge" {
					want = f.DependencyUnbound
				} else {
					want = f.InvalidState
				}
				r.manifest = registryTestManifest(t, entries)
			case "missing-row":
				r.participants = r.participants[1:]
				want = f.DependencyUnavailable
			case "failed":
				r.operation.State, r.resume = c.OperationFailed, c.CleanupPhase
				want = f.InvalidState
			case "outbox":
				r.cleanupStage = "outbox"
				want = f.InvalidState
			}
			err := artifactCleanupProgress(r)
			if want != "" {
				hasCode(t, err, want)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			complete, err := artifactCleanupCompletesDomains(r)
			if err != nil || complete != (name == "sibling-completed") || r.cleanupStage != "domains" {
				t.Fatal("domain progress fabricated", complete, err)
			}
		})
	}
}

func artifactCleanupAuthorityFixture(t *testing.T) (*ObjectCleanupAuthority, *agentRetirementAuthorityStore, i.Actor, oc.ProjectCleanupCause) {
	t.Helper()
	projects, store, req := agentRetirementAuthorityFixture(t)
	a, err := NewObjectCleanupAuthority(projects)
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.ParseID[oc.CleanupOperation](req.Cause.OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	cause, err := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: req.ProjectID, OperationID: op, Version: req.Cause.ProjectVersion})
	if err != nil {
		t.Fatal(err)
	}
	return a, store, req.Actor, cause
}

func TestArtifactCleanupAuthorityOriginalTransaction(t *testing.T) {
	for _, mode := range []string{"canonical-error", "human", "foreign-tx", "expired-tx", "nil-executor", "shared-project", "foreign-store", "wrong-actor-cause"} {
		t.Run(mode, func(t *testing.T) {
			a, store, actor, cause := artifactCleanupAuthorityFixture(t)
			tx := store.tx
			switch mode {
			case "human":
				actor = testActor(t)
			case "foreign-tx":
				tx = f.NewTx()
			case "expired-tx":
				store.live = false
			case "nil-executor":
				store.nilExecutor = true
			case "shared-project":
				store.held[projectLock(cause.Details().ProjectID, f.Exclusive).Key.Canonical()] = f.Shared
			case "foreign-store":
				a.projects.state().lifecycle = lifecycleTestAuthority(t)
			case "wrong-actor-cause":
				registration, _ := i.RegisterService(i.ObjectMaintenance)
				scope, _ := i.InProject(cause.Details().ProjectID)
				actor, _ = registration.Actor(testID[c.Operation](t).String(), scope)
			}
			err := a.CheckProjectCleanupInTx(context.Background(), tx, actor, cause)
			seen := store.seen()
			if err == nil || slices.Contains(seen, "forbidden-tx") || slices.Contains(seen, "forbidden-acquire") || slices.Contains(seen, "forbidden-write") {
				t.Fatal("manufactured transaction/permission", err, seen)
			}
			if mode == "canonical-error" {
				if !errors.Is(err, store.readErr) || !slices.Contains(seen, "row") {
					t.Fatal("original Scan error lost", err, seen)
				}
			} else if slices.Contains(seen, "row") {
				t.Fatal("invalid original authority reached SQL", seen)
			}
		})
	}
}

func TestArtifactCleanupAuthorityPlanningAndPrivateFamily(t *testing.T) {
	a, store, actor, cause := artifactCleanupAuthorityFixture(t)
	request, err := oc.NewProjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.GateProjectAccess, Actor: actor, ProjectCleanup: cause})
	if err != nil {
		t.Fatal(err)
	}
	deps, err := a.Discover(context.Background(), request)
	if err != nil || len(store.seen()) != 0 || len(deps.Locks()) != 1 || deps.Locks()[0].Mode != f.Exclusive {
		t.Fatal("discovery granted/read data", err, store.seen())
	}
	changed, err := oc.NewProjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.FinishProjectAccess, Actor: actor, ProjectCleanup: cause, Objects: []oc.ObjectID{testID[oc.StoredObject](t)}})
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, a.ValidateInTx(context.Background(), store.tx, changed, deps), f.Forbidden)
	if len(store.seen()) != 0 {
		t.Fatal("unbound plan reached SQL")
	}
	for _, mode := range []string{"no-scope", "closed", "secret", "agent", "variables", "terminal", "original-claim"} {
		t.Run(mode, func(t *testing.T) {
			st := &agentRetirementAuthorityStore{&retirementPrepStore{tx: f.NewTx(), live: true, held: map[string]f.LockMode{}, readErr: errors.New("original claim")}}
			d, _, round := cleanupSkillsFixture(t, st, &cleanupSkillsPort{})
			d.state.artifact = &secretCleanupUncalledPort{}
			round.participant = c.ArtifactObjectParticipant
			st.held[projectLock(round.claim.project, f.Exclusive).Key.Canonical()] = f.Exclusive
			w := &lifecycleCleanupScope{state: d.state, round: round, open: true}
			ctx := context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, w)
			switch mode {
			case "no-scope":
				ctx = context.Background()
			case "closed":
				w.open = false
			case "secret":
				w.round.participant = c.SecretParticipant
			case "agent":
				w.round.agentStep = true
			case "variables":
				w.round.variablesStep = true
			case "terminal":
				w.round.terminalStep = true
			}
			err := d.state.authority.requireArtifactCleanupClaimInTx(ctx, st.tx, round.cause, round.claim.project)
			if mode == "original-claim" {
				if !errors.Is(err, st.readErr) {
					t.Fatal("original claim error lost", err)
				}
			} else {
				hasCode(t, err, f.Forbidden)
				if slices.Contains(st.seen(), "row") {
					t.Fatal("cross-family SQL")
				}
			}
		})
	}
}

func TestArtifactAuthorityCurrentHumanAndServiceGate(t *testing.T) {
	actor, project, creation := testActor(t), testID[i.Project](t), testID[c.Creation](t)
	now, order := time.Now().UTC().Truncate(time.Microsecond), []string{}
	store := &authorityStore{tx: f.NewTx(), order: &order}
	var sessionErr error
	projects, err := NewAuthority(store, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, f.Tx, i.Actor) error { order = append(order, "session"); return sessionErr })})
	if err != nil {
		t.Fatal(err)
	}
	store.row = func(q string, _ ...any) postgres.Row {
		if q == "SELECT clock_timestamp()" {
			return valuesRow(now)
		}
		return valuesRow(project.String(), actor.Details().UserID, "Demo", "demo", "body", string(c.Active), int64(1), nil, now, now, nil, creation.String(), true, nil)
	}
	a, err := NewArtifactAuthority(projects)
	if err != nil {
		t.Fatal(err)
	}
	subject := ac.AccessSubject{Actor: actor, ProjectID: project}
	grant, err := a.AuthorizeInTx(context.Background(), store.tx, subject, i.Mutate)
	scope, _ := i.InProject(project)
	if err != nil || !grant.Matches(actor, scope, i.Mutate) {
		t.Fatal("real current owner path denied", err)
	}
	order, sessionErr = nil, fault(f.SessionRevoked)
	_, err = a.AuthorizeInTx(context.Background(), store.tx, subject, i.Mutate)
	hasCode(t, err, f.SessionRevoked)
	if slices.Contains(order, "query") {
		t.Fatal("revoked session read canonical")
	}
	registration, _ := i.RegisterService(i.ObjectMaintenance)
	subject.Actor, _ = registration.Actor(testID[c.Operation](t).String(), scope)
	order = nil
	_, err = a.AuthorizeInTx(context.Background(), store.tx, subject, i.Converge)
	hasCode(t, err, f.Forbidden)
	if slices.Contains(order, "query") || slices.Contains(order, "session") {
		t.Fatal("service label borrowed permission")
	}
}

func TestArtifactCleanupAuthorityWaitsForOriginalScan(t *testing.T) {
	a, store, actor, cause := artifactCleanupAuthorityFixture(t)
	store.readEntered, store.readRelease = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var once sync.Once
	free := func() { once.Do(func() { close(store.readRelease) }) }
	joined := false
	t.Cleanup(func() {
		cancel()
		free()
		if !joined {
			<-done
		}
	})
	go func() { done <- a.CheckProjectCleanupInTx(ctx, store.tx, actor, cause) }()
	select {
	case <-store.readEntered:
	case <-time.After(time.Second):
		t.Fatal("original Scan not entered")
	}
	cancel()
	select {
	case <-done:
		joined = true
		t.Fatal("cancel replaced original return")
	default:
	}
	free()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, store.readErr) {
			t.Fatal("original Scan error lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("original Scan did not return")
	}
}
