package project

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func knowledgeCleanupRecord(t *testing.T) *lifecycleRecord {
	t.Helper()
	r := executionRetirementRecord(t)
	entries := r.manifest.Entries()
	entries = append(entries, c.ParticipantRegistration{Name: c.KnowledgeParticipant, ContractVersion: 1, OwnerModule: "knowledge"})
	for n := range entries {
		if entries[n].Name == c.OutboxParticipant {
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, c.KnowledgeParticipant)
		}
	}
	r.manifest = registryTestManifest(t, entries)
	r.participants = append(r.participants, lifecycleParticipantRecord{name: c.KnowledgeParticipant, version: 1, stop: "stopped", cleanup: "required"})
	skillsCleanupSet(r, c.ExecutionParticipant, "stopped", "completed")
	return r
}

func knowledgeCleanupTestContext(ctx context.Context, w *lifecycleCleanupScope) context.Context {
	ctx = context.WithValue(ctx, lifecycleCleanupScopeKey{}, w)
	return context.WithValue(ctx, knowledgeCleanupScopeKey{}, w)
}

func knowledgeCleanupFixture(t *testing.T) (*LifecycleCleanupDriver, *KnowledgeProjectCleanupAuthority, *artifactSourceStopStore, lifecycleCleanupRound, i.Actor) {
	t.Helper()
	_, _, s, req := knowledgeStopTest(t)
	a, err := NewLifecycleAuthority(s, knowledgeCleanupRecord(t).manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewLifecycleCleanupDriverForKnowledge(s, a, &testProcess{id: testID[oc.Process](t)}, &cleanupDriverPort{name: c.KnowledgeParticipant})
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewKnowledgeProjectCleanupAuthority(a, s)
	if err != nil {
		t.Fatal(err)
	}
	r := lifecycleCleanupRound{claim: lifecycleStopClaim{project: req.ProjectID, operation: req.Cause.OperationID, process: d.state.process, attempt: testID[struct{}](t).String(), fence: 1, phase: "running"}, cause: req.Cause, participant: c.KnowledgeParticipant, operationVersion: 4, rowVersion: 2}
	s.claim = &r.claim
	return d, gate, s, r, req.Actor
}

// This checks the frozen prerequisite predicate, not successful authorization
// or any provider's actual return. Unconfigured sibling dependencies stay free.
func TestKnowledgeCleanupFrozenPrerequisites(t *testing.T) {
	r := knowledgeCleanupRecord(t)
	skillsCleanupSet(r, c.SchedulerParticipant, "stopped", "pending")
	skillsCleanupSet(r, c.ExecutionParticipant, "stopped", "pending")
	for _, state := range []string{"required", "pending", "completed"} {
		skillsCleanupSet(r, c.KnowledgeParticipant, "stopped", state)
		name, replay, err := knowledgeDriverProgress(r)
		if err != nil || name != c.KnowledgeParticipant || replay != (state == "completed") {
			t.Fatal("Knowledge added an undeclared Scheduler prerequisite", state, err)
		}
		before, after, err := cleanupStages(name)
		if err != nil || before != "domains" || after != "domains" {
			t.Fatal("Knowledge advanced the parent stage", before, after, err)
		}
	}
	for _, mode := range []string{"archive", "wrong-parent", "stop-pending", "missing-row", "duplicate-row", "declared-sibling"} {
		t.Run(mode, func(t *testing.T) {
			r := knowledgeCleanupRecord(t)
			switch mode {
			case "archive":
				r.operation.Action = c.Archive
			case "wrong-parent":
				r.cleanupStage = "outbox"
			case "stop-pending":
				r.participants[0].stop = "pending"
			case "missing-row":
				r.participants = r.participants[1:]
			case "duplicate-row":
				r.participants = append(r.participants, r.participants[0])
			case "declared-sibling":
				entries := r.manifest.Entries()
				for n := range entries {
					if entries[n].Name == c.KnowledgeParticipant {
						entries[n].CleanupAfter = append(entries[n].CleanupAfter, c.SecretParticipant)
					}
				}
				r.manifest = registryTestManifest(t, entries)
			}
			if _, _, err := knowledgeDriverProgress(r); err == nil {
				t.Fatal("invalid frozen prerequisite accepted")
			}
		})
	}
}

func TestKnowledgeCleanupOriginalFamilyClaimAndProjectLock(t *testing.T) {
	for _, mode := range []string{"project-only", "generic-only", "foreign-scope", "stop-family", "closed", "archive", "wrong-tx", "shared-project", "changed-fence", "terminal-claim"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, r, actor := knowledgeCleanupFixture(t)
			w := &lifecycleCleanupScope{state: d.state, round: r, open: true}
			ctx, tx := knowledgeCleanupTestContext(context.Background(), w), s.tx
			actual := r.claim
			s.claim = &actual
			switch mode {
			case "generic-only":
				ctx = context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, w)
			case "foreign-scope":
				ctx = context.WithValue(ctx, knowledgeCleanupScopeKey{}, &lifecycleCleanupScope{state: d.state, round: r, open: true})
			case "stop-family":
				ctx = context.WithValue(context.Background(), knowledgeStopLoanKey{}, &knowledgeStopLoan{})
			case "closed":
				w.open = false
			case "archive":
				r.cause.Action = c.Archive
			case "wrong-tx":
				tx = f.NewTx()
			case "shared-project":
				s.held[projectLock(r.claim.project, f.Exclusive).Key.Canonical()] = f.Shared
			case "changed-fence":
				actual.fence++
			case "terminal-claim":
				actual.phase = "terminal"
			}
			err := a.RequireProjectCleanupInTx(ctx, tx, actor, r.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project})
			if err == nil || errors.Is(err, s.readErr) != (mode == "project-only") {
				t.Fatal("lost private gate or required an unrelated Schedule lock", err)
			}
			if s.transactions.Load() != 0 || slices.Contains(s.seen(), "forbidden-write") || slices.Contains(s.seen(), "acquire") {
				t.Fatal("Require created a transaction, lock or write", s.seen())
			}
		})
	}
}

func TestKnowledgeCleanupClaimReadActuallyReturns(t *testing.T) {
	d, a, s, r, actor := knowledgeCleanupFixture(t)
	// Hold the first real claim Scan, which ultimately returns the SQL sentinel.
	s.claim = nil
	s.readEntered, s.readRelease = make(chan struct{}), make(chan struct{})
	w := &lifecycleCleanupScope{state: d.state, round: r, open: true}
	ctx, cancel := context.WithCancel(knowledgeCleanupTestContext(context.Background(), w))
	done := make(chan error, 1)
	joined := false
	var once sync.Once
	release := func() { once.Do(func() { close(s.readRelease) }) }
	t.Cleanup(func() {
		cancel()
		release()
		if !joined {
			<-done
		}
	})
	go func() {
		done <- a.RequireProjectCleanupInTx(ctx, s.tx, actor, r.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project})
	}()
	retirementPrepAwait(t, s.readEntered)
	cancel()
	if w.mu.TryLock() {
		w.mu.Unlock()
		t.Fatal("scope closure bypassed original Scan")
	}
	select {
	case <-done:
		joined = true
		t.Fatal("cancel replaced original Scan return")
	default:
	}
	release()
	err := <-done
	joined = true
	if !errors.Is(err, s.readErr) {
		t.Fatal("original Scan error changed", err)
	}
}
