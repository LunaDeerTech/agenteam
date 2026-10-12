package project

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func secretCleanupRecord(t *testing.T) *lifecycleRecord {
	t.Helper()
	entries := registryTestEntries()
	entries = append(entries, c.ParticipantRegistration{Name: c.SkillsParticipant, ContractVersion: 1, OwnerModule: "agent-skills-variables"})
	for n := range entries {
		if entries[n].Name == c.SecretParticipant || entries[n].Name == c.ArtifactObjectParticipant {
			entries[n].CleanupAfter = []c.ParticipantName{c.SkillsParticipant}
		}
		if entries[n].Name == c.OutboxParticipant {
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, c.SkillsParticipant)
		}
	}
	r := &lifecycleRecord{operation: c.LifecycleOperation{Action: c.Delete, State: c.OperationCleaning}, cleanupStage: "domains", manifest: registryTestManifest(t, entries)}
	for _, entry := range r.manifest.Entries() {
		state := "required"
		if entry.Name == c.SkillsParticipant {
			state = "completed"
		}
		r.participants = append(r.participants, lifecycleParticipantRecord{name: entry.Name, version: 1, stop: "stopped", cleanup: state})
	}
	return r
}

// These are dependency predicates, not real terminal receipts or cleanup grants.
func TestSecretCleanupProgressBoundaries(t *testing.T) {
	for _, state := range []string{"required", "pending"} {
		t.Run(state, func(t *testing.T) {
			r := secretCleanupRecord(t)
			skillsCleanupSet(r, c.SecretParticipant, "stopped", state)
			if err := secretCleanupProgress(r); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*lifecycleRecord)
		code f.Code
	}{
		{"archive", func(r *lifecycleRecord) { r.operation.Action = c.Archive }, f.InvalidState},
		{"stopping", func(r *lifecycleRecord) { r.operation.State = c.OperationStopping }, f.InvalidState},
		{"failed-cleanup", func(r *lifecycleRecord) { r.operation.State, r.resume = c.OperationFailed, c.CleanupPhase }, f.InvalidState},
		{"outbox", func(r *lifecycleRecord) { r.cleanupStage = "outbox" }, f.InvalidState},
		{"unfinished-stop", func(r *lifecycleRecord) { skillsCleanupSet(r, c.ArtifactObjectParticipant, "pending", "required") }, f.DependencyUnavailable},
		{"parent-pending", func(r *lifecycleRecord) { skillsCleanupSet(r, c.SkillsParticipant, "stopped", "pending") }, f.InvalidState},
		{"already-completed", func(r *lifecycleRecord) { skillsCleanupSet(r, c.SecretParticipant, "stopped", "completed") }, f.InvalidState},
		{"failed", func(r *lifecycleRecord) { skillsCleanupSet(r, c.SecretParticipant, "stopped", "failed") }, f.InvalidState},
		{"missing-row", func(r *lifecycleRecord) { r.participants = r.participants[1:] }, f.DependencyUnavailable},
		{"duplicate-row", func(r *lifecycleRecord) { r.participants = append(r.participants, r.participants[0]) }, f.DependencyUnavailable},
		{"foreign-row", func(r *lifecycleRecord) { r.participants[0].name = "foreign" }, f.DependencyUnavailable},
		{"no-business-dependency", func(r *lifecycleRecord) {
			entries := r.manifest.Entries()
			for n := range entries {
				if entries[n].Name == c.SecretParticipant {
					entries[n].CleanupAfter = nil
				}
			}
			r.manifest = registryTestManifest(t, entries)
		}, f.DependencyUnbound},
		{"additional-dependency-pending", func(r *lifecycleRecord) {
			entries := r.manifest.Entries()
			for n := range entries {
				if entries[n].Name == c.SecretParticipant {
					entries[n].CleanupAfter = append(entries[n].CleanupAfter, c.ArtifactObjectParticipant)
				}
			}
			r.manifest = registryTestManifest(t, entries)
		}, f.InvalidState},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := secretCleanupRecord(t)
			tc.edit(r)
			hasCode(t, secretCleanupProgress(r), tc.code)
		})
	}
	hasCode(t, secretCleanupProgress(nil), f.DependencyUnavailable)
}

func secretCleanupAuthorityFixture(t *testing.T) (*SecretAuthority, *agentRetirementAuthorityStore, i.Actor, sc.LifecycleCause, i.ProjectID) {
	t.Helper()
	a, store, req := agentRetirementAuthorityFixture(t)
	gate, err := NewSecretAuthority(a)
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.ParseID[sc.LifecycleOperation](req.Cause.OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	cause, err := sc.NewLifecycleCause(op, req.Cause.ProjectVersion, true)
	if err != nil {
		t.Fatal(err)
	}
	return gate, store, req.Actor, cause, req.ProjectID
}

func TestSecretCleanupAuthorityOriginalTransaction(t *testing.T) {
	for _, mode := range []string{"nil-context", "cancelled", "human", "wrong-project", "not-deleting", "foreign-tx", "expired-tx", "nil-executor", "missing-project", "shared-project", "foreign-store", "canonical-error"} {
		t.Run(mode, func(t *testing.T) {
			a, store, actor, cause, p := secretCleanupAuthorityFixture(t)
			ctx, tx := context.Background(), store.tx
			switch mode {
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "human":
				actor = testActor(t)
			case "wrong-project":
				p = testID[i.Project](t)
			case "not-deleting":
				cause, _ = sc.NewLifecycleCause(cause.Details().OperationID, cause.Details().ProjectVersion, false)
			case "foreign-tx":
				tx = f.NewTx()
			case "expired-tx":
				store.live = false
			case "nil-executor":
				store.nilExecutor = true
			case "missing-project":
				delete(store.held, projectLock(p, f.Exclusive).Key.Canonical())
			case "shared-project":
				store.held[projectLock(p, f.Exclusive).Key.Canonical()] = f.Shared
			case "foreign-store":
				a.authority.state().lifecycle = lifecycleTestAuthority(t)
			}
			err := a.CheckCleanupInTx(ctx, tx, actor, cause, p)
			seen := store.seen()
			if err == nil || slices.Contains(seen, "forbidden-tx") || slices.Contains(seen, "forbidden-acquire") || slices.Contains(seen, "forbidden-write") {
				t.Fatal("cleanup manufactured Tx/permission", err, seen)
			}
			if mode == "canonical-error" {
				if !errors.Is(err, store.readErr) || !slices.Contains(seen, "row") {
					t.Fatal("original read error lost", err, seen)
				}
			} else if slices.Contains(seen, "row") || slices.Contains(seen, "query") {
				t.Fatal("invalid original context/Tx reached SQL", seen)
			}
		})
	}
}

// No embedded participant method runs: the real Store stops at original claim SQL.
type secretCleanupUncalledPort struct{ c.ProjectLifecycleParticipant }

func TestSecretCleanupAuthorityPrivateClaim(t *testing.T) {
	for _, mode := range []string{"public-context", "closed", "other-participant", "agent-family", "variables-family", "terminal-family", "wrong-cause", "other-authority", "original-claim-read"} {
		t.Run(mode, func(t *testing.T) {
			store := &agentRetirementAuthorityStore{&retirementPrepStore{tx: f.NewTx(), live: true, held: map[string]f.LockMode{}, readErr: errors.New("original claim sentinel")}}
			d, _, round := cleanupSkillsFixture(t, store, &cleanupSkillsPort{})
			d.state.secret = &secretCleanupUncalledPort{}
			round.participant = c.SecretParticipant
			store.held[projectLock(round.claim.project, f.Exclusive).Key.Canonical()] = f.Exclusive
			w := &lifecycleCleanupScope{state: d.state, round: round, open: true}
			ctx := context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, w)
			switch mode {
			case "public-context":
				ctx = context.Background()
			case "closed":
				w.open = false
			case "other-participant":
				w.round.participant = c.OutboxParticipant
			case "agent-family":
				w.round.agentStep = true
			case "variables-family":
				w.round.variablesStep = true
			case "terminal-family":
				w.round.terminalStep = true
			case "wrong-cause":
				w.round.cause.OperationID = testID[c.Operation](t)
			case "other-authority":
				w.state.authority = lifecycleTestAuthority(t)
			}
			a := d.state.authority
			if mode == "other-authority" {
				a, _ = NewLifecycleAuthority(store, secretCleanupRecord(t).manifest, nil)
			}
			err := a.requireSecretCleanupClaimInTx(ctx, store.tx, round.cause, round.claim.project)
			if mode == "original-claim-read" {
				if !errors.Is(err, store.readErr) {
					t.Fatal("original exact claim read lost", err)
				}
			} else {
				hasCode(t, err, f.Forbidden)
				if slices.Contains(store.seen(), "row") {
					t.Fatal("other private family reached SQL")
				}
			}
		})
	}
}

func TestSecretCleanupAuthorityOriginalReturn(t *testing.T) {
	a, store, actor, cause, p := secretCleanupAuthorityFixture(t)
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
	go func() { done <- a.CheckCleanupInTx(ctx, store.tx, actor, cause, p) }()
	select {
	case <-store.readEntered:
	case <-time.After(time.Second):
		t.Fatal("original canonical read not reached")
	}
	cancel()
	select {
	case <-done:
		joined = true
		t.Fatal("cancellation replaced original return")
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
		t.Fatal("original read did not return")
	}
}
