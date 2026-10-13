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

func workCleanupRecord(t *testing.T) *lifecycleRecord {
	t.Helper()
	r := executionRetirementRecord(t)
	entries := r.manifest.Entries()
	entries = append(entries, c.ParticipantRegistration{Name: c.WorkParticipant, ContractVersion: 1, OwnerModule: "work", CleanupAfter: []c.ParticipantName{c.SchedulerParticipant, c.ExecutionParticipant}})
	for n := range entries {
		if entries[n].Name == c.OutboxParticipant {
			entries[n].CleanupAfter = append(entries[n].CleanupAfter, c.WorkParticipant)
		}
	}
	r.manifest = registryTestManifest(t, entries)
	r.participants = append(r.participants, lifecycleParticipantRecord{name: c.WorkParticipant, version: 1, stop: "stopped", cleanup: "required"})
	skillsCleanupSet(r, c.ExecutionParticipant, "stopped", "completed")
	return r
}

func workCleanupTestContext(ctx context.Context, w *lifecycleCleanupScope) context.Context {
	ctx = context.WithValue(ctx, lifecycleCleanupScopeKey{}, w)
	return context.WithValue(ctx, workCleanupScopeKey{}, w)
}

// Predicate checks do not issue a private scope or manufacture a completed
// Work/Execution owner. In particular, uncleaned siblings are legitimate.
func TestWorkCleanupProgressBoundaries(t *testing.T) {
	for _, state := range []string{"required", "pending", "completed"} {
		t.Run(state, func(t *testing.T) {
			r := workCleanupRecord(t)
			skillsCleanupSet(r, c.WorkParticipant, "stopped", state)
			name, replay, err := workDriverProgress(r)
			if err != nil || name != c.WorkParticipant || replay != (state == "completed") {
				t.Fatal("fixed progress changed", name, replay, err)
			}
			before, after, err := cleanupStages(name)
			if err != nil || before != "domains" || after != "domains" {
				t.Fatal("Work advanced the parent to Outbox", before, after, err)
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
		{"cleanup-failed", func(r *lifecycleRecord) { skillsCleanupSet(r, c.WorkParticipant, "stopped", "failed") }},
		{"scheduler-pending", func(r *lifecycleRecord) { skillsCleanupSet(r, c.SchedulerParticipant, "stopped", "pending") }},
		{"execution-pending", func(r *lifecycleRecord) { skillsCleanupSet(r, c.ExecutionParticipant, "stopped", "pending") }},
		{"declared-predecessor", func(r *lifecycleRecord) {
			entries := r.manifest.Entries()
			for n := range entries {
				if entries[n].Name == c.WorkParticipant {
					entries[n].CleanupAfter = append(entries[n].CleanupAfter, c.SecretParticipant)
				}
			}
			r.manifest = registryTestManifest(t, entries)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := workCleanupRecord(t)
			tc.edit(r)
			if _, _, err := workDriverProgress(r); err == nil {
				t.Fatal("invalid prerequisite accepted")
			}
		})
	}
	for _, missing := range []c.ParticipantName{c.SchedulerParticipant, c.ExecutionParticipant} {
		t.Run("undeclared-"+string(missing), func(t *testing.T) {
			r := workCleanupRecord(t)
			entries := r.manifest.Entries()
			for n := range entries {
				if entries[n].Name == c.WorkParticipant {
					entries[n].CleanupAfter = slices.DeleteFunc(entries[n].CleanupAfter, func(name c.ParticipantName) bool { return name == missing })
				}
			}
			r.manifest = registryTestManifest(t, entries)
			_, _, err := workDriverProgress(r)
			hasCode(t, err, f.DependencyUnbound)
		})
	}
}

func workCleanupFixture(t *testing.T) (*LifecycleCleanupDriver, *WorkProjectCleanupAuthority, *artifactSourceStopStore, lifecycleCleanupRound, i.Actor) {
	t.Helper()
	_, _, s, req := schedulerProjectStopTest(t)
	a, err := NewLifecycleAuthority(s, workCleanupRecord(t).manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewLifecycleCleanupDriverForWork(s, a, &testProcess{id: testID[oc.Process](t)}, &cleanupDriverPort{name: c.WorkParticipant})
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewWorkProjectCleanupAuthority(a, s)
	if err != nil {
		t.Fatal(err)
	}
	r := lifecycleCleanupRound{claim: lifecycleStopClaim{project: req.ProjectID, operation: req.Cause.OperationID, process: d.state.process, attempt: testID[struct{}](t).String(), fence: 1, phase: "running"}, cause: req.Cause, participant: c.WorkParticipant, operationVersion: 4, rowVersion: 2}
	s.claim = &r.claim
	return d, gate, s, r, req.Actor
}

func TestWorkCleanupRequiresOriginalFamilyAndTransaction(t *testing.T) {
	for _, mode := range []string{"public-context", "closed", "stop-family", "work-stop-family", "execution-family", "generic-only", "different-scope", "other-cleanup", "agent", "variables", "terminal", "secret-parent", "unbound-work", "foreign-authority", "wrong-cause", "archive", "wrong-tx", "expired-tx", "nil-executor", "missing-project-lock", "missing-schedule-lock", "shared-schedule-lock", "missing-claim", "other-fence", "terminal-claim", "original-scan"} {
		t.Run(mode, func(t *testing.T) {
			d, a, s, r, actor := workCleanupFixture(t)
			w := &lifecycleCleanupScope{state: d.state, round: r, open: true}
			ctx := workCleanupTestContext(context.Background(), w)
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
			case "work-stop-family":
				ctx = context.WithValue(context.Background(), workStopLoanKey{}, &workStopLoan{})
			case "execution-family":
				ctx = context.WithValue(context.Background(), executionRetirementScopeKey{}, &executionRetirementScope{})
			case "generic-only":
				ctx = context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, w)
			case "different-scope":
				ctx = context.WithValue(ctx, workCleanupScopeKey{}, &lifecycleCleanupScope{state: d.state, round: r, open: true})
			case "other-cleanup":
				w.round.participant = c.ArtifactObjectParticipant
			case "agent":
				w.round.agentStep = true
			case "variables":
				w.round.variablesStep = true
			case "terminal":
				w.round.terminalStep = true
			case "secret-parent":
				w.round.secretParentStep = true
			case "unbound-work":
				d.state.work = nil
			case "foreign-authority":
				_, a, _, _, _ = workCleanupFixture(t)
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

func TestWorkCleanupClaimScanOriginalReturn(t *testing.T) {
	d, a, s, r, actor := workCleanupFixture(t)
	// Hold the actual first claim Scan, not a fabricated complete lifecycle.
	s.claim = nil
	s.readEntered, s.readRelease = make(chan struct{}), make(chan struct{})
	w := &lifecycleCleanupScope{state: d.state, round: r, open: true}
	ctx, cancel := context.WithCancel(workCleanupTestContext(context.Background(), w))
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
