package project

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func TestLifecycleCleanupArtifactConstructionAndStages(t *testing.T) {
	legacy, _, a, b := terminalDriverFixture(t)
	st := legacy.state
	projects, err := NewAuthority(st.store, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, f.Tx, i.Actor) error { t.Fatal("Human authorization used"); return nil }), Lifecycle: st.authority})
	if err != nil {
		t.Fatal(err)
	}
	secret := &cleanupDriverPort{name: c.SecretParticipant}
	artifact := &cleanupDriverPort{name: c.ArtifactObjectParticipant}
	outbox := &cleanupDriverPort{name: c.OutboxParticipant}
	build := func(artifact, outbox c.ProjectLifecycleParticipant) (*LifecycleCleanupDriver, error) {
		return NewLifecycleCleanupDriverWithArtifact(st.store, projects, st.processes, st.agents, st.skills, st.variables, a, b, secret, artifact, outbox)
	}
	driver, err := build(artifact, outbox)
	if err != nil || driver.state.artifact != artifact || driver.state.participant != outbox || driver.state.authority != st.authority {
		t.Fatal("fixed original ports changed", err)
	}
	for _, bad := range []c.ProjectLifecycleParticipant{nil, (*cleanupDriverPort)(nil), &cleanupDriverPort{name: c.AuditParticipant}} {
		if _, err := build(bad, outbox); err == nil {
			t.Fatal("invalid Artifact bound")
		}
		if _, err := build(artifact, bad); err == nil {
			t.Fatal("invalid Outbox bound")
		}
	}
	for _, mode := range []string{"parent-pending", "secret-required", "artifact-required", "artifact-pending", "artifact-completed", "outbox", "premature-outbox", "audit-replay", "final-unbound"} {
		t.Run(mode, func(t *testing.T) {
			r := secretCleanupRecord(t)
			skillsCleanupSet(r, c.SecretParticipant, "stopped", "completed")
			want, replay, reject := c.ArtifactObjectParticipant, false, false
			switch mode {
			case "parent-pending":
				skillsCleanupSet(r, c.SkillsParticipant, "stopped", "pending")
				want = c.SkillsParticipant
			case "secret-required":
				skillsCleanupSet(r, c.SecretParticipant, "stopped", "required")
				want = c.SecretParticipant
			case "artifact-pending":
				skillsCleanupSet(r, c.ArtifactObjectParticipant, "stopped", "pending")
			case "artifact-completed":
				skillsCleanupSet(r, c.ArtifactObjectParticipant, "stopped", "completed")
				replay = true
			case "outbox", "premature-outbox", "audit-replay", "final-unbound":
				skillsCleanupSet(r, c.ArtifactObjectParticipant, "stopped", "completed")
				r.cleanupStage = "outbox"
				want = c.OutboxParticipant
				if mode == "premature-outbox" {
					skillsCleanupSet(r, c.ArtifactObjectParticipant, "stopped", "pending")
					reject = true
				}
				if mode == "audit-replay" {
					r.cleanupStage = "audit"
					skillsCleanupSet(r, c.OutboxParticipant, "stopped", "completed")
					replay = true
				}
				if mode == "final-unbound" {
					r.cleanupStage = "final"
					reject = true
				}
			}
			got, again, err := driver.state.cleanupProgress(r)
			if reject {
				if err == nil {
					t.Fatal("incomplete/unbound phase accepted")
				}
				return
			}
			if err != nil || got != want || again != replay {
				t.Fatal("wrong canonical single stage", got, again, err)
			}
			if mode == "artifact-required" {
				complete, err := artifactCleanupCompletesDomains(r)
				if err != nil || !complete {
					t.Fatal("all other completed domains did not permit handoff", err)
				}
				skillsCleanupSet(r, c.SecretParticipant, "stopped", "pending")
				complete, err = artifactCleanupCompletesDomains(r)
				if err != nil || complete {
					t.Fatal("incomplete sibling permitted handoff", err)
				}
				// Secret is a fixed implementation order, not an Artifact manifest edge.
				if err = artifactCleanupProgress(r); err != nil {
					t.Fatal("invented Secret dependency", err)
				}
			}
		})
	}
	if _, _, err := driver.state.cleanupProgress(nil); err == nil {
		t.Fatal("nil canonical facts accepted")
	}
	if current, next, err := cleanupStages(c.ArtifactObjectParticipant); err != nil || current != "domains" || next != "domains" {
		t.Fatal("unconditional Outbox transition")
	}
	if _, err := driver.state.cleanupPort(c.AuditParticipant); err == nil {
		t.Fatal("partial constructor bound Audit")
	}
}

type artifactParentRead struct {
	postgres.SQLExecutor
	parent, secret *c.CleanupCheckpoint
	attempts       int64
}

func (s *artifactParentRead) QueryRow(_ context.Context, _ string, args ...any) postgres.Row {
	return rowFunc(func(out ...any) error {
		if len(args) != 2 {
			return errors.New("wrong checkpoint query")
		}
		var cp *c.CleanupCheckpoint
		attempts := int64(1)
		switch args[1] {
		case string(c.SkillsParticipant):
			cp = s.parent
		case string(c.SecretParticipant):
			cp = s.secret
			attempts = s.attempts
		default:
			return errors.New("unrelated checkpoint read")
		}
		*out[0].(*int64), *out[1].(*int64) = 7, attempts
		if cp != nil {
			schema := int64(cp.Schema())
			*out[2].(**int64) = &schema
			*out[3].(*[]byte) = cp.Bytes()
		}
		return nil
	})
}
func artifactParentFixture(t *testing.T) (*LifecycleCleanupDriver, lifecycleCleanupRound, *artifactParentRead, lifecycleStopClaim) {
	t.Helper()
	d, round, parent, old := secretParentFixture(t)
	op, err := f.ParseID[sc.LifecycleOperation](round.claim.operation.String())
	if err != nil {
		t.Fatal(err)
	}
	report, err := secretCleanupReport(round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, sc.CleanupReport{Completed: true, Checkpoint: sc.CleanupCheckpoint{ProjectID: round.claim.project, OperationID: op}})
	if err != nil {
		t.Fatal(err)
	}
	actual := old
	actual.fence++
	actual.attempt = testID[struct{}](t).String()
	round.participant = c.ArtifactObjectParticipant
	round.claim.fence = actual.fence + 1
	round.claim.attempt = testID[struct{}](t).String()
	d.state.artifact = &cleanupDriverPort{name: c.ArtifactObjectParticipant}
	return d, round, &artifactParentRead{parent: parent.cp, secret: report.Details().Checkpoint, attempts: 1}, actual
}
func TestLifecycleCleanupArtifactParentBinding(t *testing.T) {
	for _, mode := range []string{"exact", "missing-claim", "old-terminal-claim", "unstarted-secret", "missing-secret-checkpoint", "wrong-parent"} {
		t.Run(mode, func(t *testing.T) {
			d, round, x, actual := artifactParentFixture(t)
			claim := &actual
			switch mode {
			case "missing-claim":
				claim = nil
			case "old-terminal-claim":
				actual.fence--
			case "unstarted-secret":
				x.attempts = 0
			case "missing-secret-checkpoint":
				x.secret = nil
			case "wrong-parent":
				raw := bytes.Replace(x.parent.Bytes(), []byte(round.claim.project.String()), []byte(testID[i.Project](t).String()), 1)
				cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, 6, raw)
				if err != nil {
					t.Fatal(err)
				}
				x.parent = &cp
			}
			err := d.state.selectArtifactParent(context.Background(), x, &round, claim)
			if mode != "exact" {
				if err == nil {
					t.Fatal("unconfirmed parent accepted")
				}
				return
			}
			if err != nil || round.artifactParentCheckpoint == nil || !bytes.Equal(round.artifactParentCheckpoint.Bytes(), x.parent.Bytes()) {
				t.Fatal("exact original parent lost", err)
			}
		})
	}
}

func TestLifecycleCleanupArtifactOriginalReturn(t *testing.T) {
	for _, mode := range []string{"pending", "completed", "unknown", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			d, round, _, _ := artifactParentFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			ctx, cancel := context.WithCancel(context.Background())
			type outcome struct {
				report c.CleanupReport
				err    error
			}
			done := make(chan outcome, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original provider did not return")
					}
				}
			})
			cause, e := f.NewRecoveryCause("artifact-project-cleanup", round.claim.operation.String(), "")
			if e != nil {
				t.Fatal(e)
			}
			unknown := f.UnknownResult(testID[f.TransactionAttempt](t), cause)
			var saved context.Context
			d.state.artifact = &cleanupDriverPort{name: c.ArtifactObjectParticipant, call: func(got context.Context, actor i.Actor, actual c.LifecycleCause, scope c.ScopeRef, cp *c.CleanupCheckpoint) (c.CleanupReport, error) {
				saved = got
				if actual != round.cause || scope.ProjectID != round.claim.project || cp != nil || actor.Details().CauseRef != round.claim.operation.String() {
					t.Error("original call input changed")
				}
				close(entered)
				<-release
				if mode == "unknown" {
					return c.CleanupReport{}, commitError(unknown)
				}
				op, _ := f.ParseID[oc.CleanupOperation](round.claim.operation.String())
				state := oc.CleanupCompleted
				if mode == "pending" {
					state = oc.CleanupPending
				}
				return artifactCleanupReport(actual, scope, oc.ProjectCleanupResult{State: state, ProjectID: scope.ProjectID, OperationID: op})
			}}
			go func() { r, e := d.state.invoke(ctx, round); done <- outcome{r, e} }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("provider not entered")
			}
			if mode == "cancelled" {
				cancel()
			}
			select {
			case <-done:
				joined = true
				t.Fatal("original call replaced by cancellation")
			default:
			}
			free()
			var out outcome
			select {
			case out = <-done:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("released provider not returned")
			}
			witness := saved.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
			if witness.open {
				t.Fatal("saved scope revived")
			}
			if mode == "unknown" {
				attempt, ok := UnknownAttempt(out.err)
				if !ok || attempt.AttemptID() != unknown.AttemptID() || out.report.Details().State != "" {
					t.Fatal("original Unknown lost", out.err)
				}
			} else if mode == "cancelled" {
				if out.err == nil || out.report.Details().State != "" {
					t.Fatal("cancelled report published")
				}
			} else {
				want := c.CleanupCompleted
				if mode == "pending" {
					want = c.CleanupPending
				}
				if out.err != nil || out.report.Details().State != want || out.report.Details().Checkpoint == nil {
					t.Fatal("provider report altered", out.err)
				}
			}
		})
	}
}

// This holds the original controlled Store return. The PG fixture separately
// holds the physically committed Artifact finish transaction before Outbox.
type artifactAdmissionStore struct {
	Store
	operation        c.OperationID
	entered, release chan struct{}
	held             atomic.Bool
	reads            atomic.Int32
	transactions     atomic.Int32
}

func (s *artifactAdmissionStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.reads.Add(1)
	return rowFunc(func(...any) error { return pgx.ErrNoRows })
}
func (s *artifactAdmissionStore) WithinTx(_ context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions.Add(1)
	if cause.Details().JobID == s.operation.String() && s.held.CompareAndSwap(false, true) {
		close(s.entered)
		<-s.release
	}
	return f.NotCommittedResult(fault(f.InvalidState))
}
func TestLifecycleCleanupArtifactOriginalRunAdmission(t *testing.T) {
	store := &artifactAdmissionStore{operation: testID[c.Operation](t), entered: make(chan struct{}), release: make(chan struct{})}
	first, round := cleanupDriverFixture(t, store, nil)
	second, err := NewLifecycleCleanupDriverWithAudit(store, first.state.authority, first.state.processes, first.state.participant, &cleanupDriverPort{name: c.AuditParticipant})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan error, 1)
	var once sync.Once
	free := func() { once.Do(func() { close(store.release) }) }
	joined := false
	t.Cleanup(func() {
		cancel()
		free()
		if !joined {
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("original Run not returned")
			}
		}
	})
	go func() { returned <- first.Run(ctx, round.claim.project, store.operation) }()
	select {
	case <-store.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("original Store not entered")
	}
	cancel()
	before := store.reads.Load()
	hasCode(t, second.Run(context.Background(), round.claim.project, store.operation), f.ResourceBusy)
	if store.reads.Load() != before || store.transactions.Load() != 1 {
		t.Fatal("second same-P driver reached SQL before original return")
	}
	hasCode(t, second.Run(context.Background(), testID[i.Project](t), testID[c.Operation](t)), f.InvalidState)
	if store.transactions.Load() != 2 {
		t.Fatal("unrelated Project blocked by A")
	}
	select {
	case <-returned:
		joined = true
		t.Fatal("cancel detached original call")
	default:
	}
	free()
	select {
	case err := <-returned:
		joined = true
		hasCode(t, err, f.InvalidState)
	case <-time.After(5 * time.Second):
		t.Fatal("released Store not returned")
	}
	hasCode(t, second.Run(context.Background(), round.claim.project, store.operation), f.InvalidState)
	if store.transactions.Load() != 3 {
		t.Fatal("returned Run left permanent Project admission")
	}
}
