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

func schedulerCleanupRecord(t *testing.T) *lifecycleRecord {
	t.Helper()
	entries := registryTestEntries()
	entries = append(entries, c.ParticipantRegistration{Name: c.SchedulerParticipant, ContractVersion: 1, OwnerModule: "scheduler"})
	for n := range entries {
		if entries[n].Name == c.OutboxParticipant {
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, c.SchedulerParticipant)
		}
	}
	r := &lifecycleRecord{operation: c.LifecycleOperation{Action: c.Delete, State: c.OperationCleaning}, cleanupStage: "domains", manifest: registryTestManifest(t, entries)}
	for _, entry := range r.manifest.Entries() {
		r.participants = append(r.participants, lifecycleParticipantRecord{name: entry.Name, version: 1, stop: "stopped", cleanup: "required"})
	}
	return r
}

// Predicate checks do not issue a private scope or manufacture a completed
// Scheduler/Execution owner. In particular, uncleaned siblings are legitimate.
func TestSchedulerCleanupProgressBoundaries(t *testing.T) {
	for _, state := range []string{"required", "pending", "completed"} {
		t.Run(state, func(t *testing.T) {
			r := schedulerCleanupRecord(t)
			skillsCleanupSet(r, c.SchedulerParticipant, "stopped", state)
			name, replay, err := schedulerDriverProgress(r)
			if err != nil || name != c.SchedulerParticipant || replay != (state == "completed") {
				t.Fatal("fixed progress changed", name, replay, err)
			}
			before, after, err := cleanupStages(name)
			if err != nil || before != "domains" || after != "domains" {
				t.Fatal("Scheduler advanced the parent to Outbox", before, after, err)
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*lifecycleRecord)
	}{
		{"archive", func(r *lifecycleRecord) { r.operation.Action = c.Archive }},
		{"stopping", func(r *lifecycleRecord) { r.operation.State = c.OperationStopping }},
		{"outbox", func(r *lifecycleRecord) { r.cleanupStage = "outbox" }},
		{"stop-pending", func(r *lifecycleRecord) { r.participants[0].stop = "pending" }},
		{"missing-row", func(r *lifecycleRecord) { r.participants = r.participants[1:] }},
		{"duplicate-row", func(r *lifecycleRecord) { r.participants = append(r.participants, r.participants[0]) }},
		{"foreign-row", func(r *lifecycleRecord) { r.participants[0].name = "foreign" }},
		{"cleanup-failed", func(r *lifecycleRecord) { skillsCleanupSet(r, c.SchedulerParticipant, "stopped", "failed") }},
		{"declared-predecessor", func(r *lifecycleRecord) {
			entries := r.manifest.Entries()
			for n := range entries {
				if entries[n].Name == c.SchedulerParticipant {
					entries[n].CleanupAfter = []c.ParticipantName{c.SecretParticipant}
				}
			}
			r.manifest = registryTestManifest(t, entries)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := schedulerCleanupRecord(t)
			tc.edit(r)
			if _, _, err := schedulerDriverProgress(r); err == nil {
				t.Fatal("invalid prerequisite accepted")
			}
		})
	}
}

func schedulerCleanupFixture(t *testing.T) (*LifecycleCleanupDriver, *SchedulerProjectCleanupAuthority, *artifactSourceStopStore, lifecycleCleanupRound, i.Actor) {
	t.Helper()
	_, _, s, req := schedulerProjectStopTest(t)
	a, err := NewLifecycleAuthority(s, schedulerCleanupRecord(t).manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewLifecycleCleanupDriverForScheduler(s, a, &testProcess{id: testID[oc.Process](t)}, &cleanupDriverPort{name: c.SchedulerParticipant})
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewSchedulerProjectCleanupAuthority(a, s)
	if err != nil {
		t.Fatal(err)
	}
	r := lifecycleCleanupRound{claim: lifecycleStopClaim{project: req.ProjectID, operation: req.Cause.OperationID, process: d.state.process, attempt: testID[struct{}](t).String(), fence: 1, phase: "running"}, cause: req.Cause, participant: c.SchedulerParticipant, operationVersion: 4, rowVersion: 2}
	s.claim = &r.claim
	return d, gate, s, r, req.Actor
}

func TestSchedulerCleanupRequiresOriginalFamilyAndTransaction(t *testing.T) {
	for _, mode := range []string{"public-context", "closed", "stop-family", "other-cleanup", "agent", "variables", "terminal", "foreign-authority", "wrong-cause", "archive", "wrong-tx", "expired-tx", "nil-executor", "missing-project-lock", "missing-schedule-lock", "shared-schedule-lock", "missing-claim", "other-fence", "terminal-claim", "original-scan"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, r, actor := schedulerCleanupFixture(t)
			w := &lifecycleCleanupScope{state: d.state, round: r, open: true}
			ctx := context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, w)
			tx := s.tx
			actual := r.claim
			s.claim = &actual
			switch mode {
			case "public-context":
				ctx = context.Background()
			case "closed":
				w.open = false
			case "stop-family":
				ctx = context.WithValue(context.Background(), schedulerStopLoanKey{}, &schedulerStopLoan{})
			case "other-cleanup":
				w.round.participant = c.ArtifactObjectParticipant
			case "agent":
				w.round.agentStep = true
			case "variables":
				w.round.variablesStep = true
			case "terminal":
				w.round.terminalStep = true
			case "foreign-authority":
				_, a, _, _, _ = schedulerCleanupFixture(t)
			case "wrong-cause":
				w.round.cause.ProjectVersion++
			case "archive":
				r.cause.Action = c.Archive
			case "wrong-tx":
				tx = f.NewTx()
			case "expired-tx":
				s.live = false
			case "nil-executor":
				s.nilExecutor = true
			case "missing-project-lock":
				delete(s.held, projectLock(r.claim.project, f.Exclusive).Key.Canonical())
			case "missing-schedule-lock":
				delete(s.held, schedulerLock(r.claim.project).Key.Canonical())
			case "shared-schedule-lock":
				s.held[schedulerLock(r.claim.project).Key.Canonical()] = f.Shared
			case "missing-claim":
				s.claimMissing = true
			case "other-fence":
				actual.fence++
			case "terminal-claim":
				actual.phase = "terminal"
			}
			err := a.RequireProjectCleanupInTx(ctx, tx, actor, r.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: r.claim.project})
			if err == nil || (mode == "original-scan") != errors.Is(err, s.readErr) {
				t.Fatal("invalid gate or lost canonical Scan", mode, err)
			}
			if s.transactions.Load() != 0 || slices.Contains(s.seen(), "forbidden-write") || slices.Contains(s.seen(), "acquire") {
				t.Fatal("Require manufactured transaction or locks", s.seen())
			}
		})
	}
}

func TestSchedulerCleanupClaimScanOriginalReturn(t *testing.T) {
	d, a, s, r, actor := schedulerCleanupFixture(t)
	// Hold the actual first claim Scan, not a fabricated complete lifecycle.
	s.claim = nil
	s.readEntered, s.readRelease = make(chan struct{}), make(chan struct{})
	w := &lifecycleCleanupScope{state: d.state, round: r, open: true}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, w))
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
		t.Fatal("scope closure could bypass original Scan")
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
