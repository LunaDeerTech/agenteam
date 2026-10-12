package project

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This Store accepts only the original live token and actually held lock
// modes. Its canonical SQL returns a sentinel, never a manufactured manifest.
type agentRetirementAuthorityStore struct{ *retirementPrepStore }

func (s *agentRetirementAuthorityStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.record("locks")
	if tx != s.tx || !s.live {
		return fault(f.Forbidden)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, lock := range locks {
		mode, ok := s.held[lock.Key.Canonical()]
		if !ok || mode != lock.Mode && mode != f.Exclusive {
			return fault(f.Forbidden)
		}
	}
	return nil
}

func agentRetirementAuthorityFixture(t *testing.T) (*Authority, *agentRetirementAuthorityStore, ac.ProjectRetirementRequest) {
	t.Helper()
	s := &agentRetirementAuthorityStore{&retirementPrepStore{tx: f.NewTx(), live: true, held: map[string]f.LockMode{}, readErr: errors.New("original canonical read sentinel")}}
	lifecycle, err := NewLifecycleAuthority(s, skillsCleanupRecord(t).manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAuthority(s, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, f.Tx, i.Actor) error {
		t.Fatal("Project retirement borrowed Human authorization")
		return nil
	}), Lifecycle: lifecycle})
	if err != nil {
		t.Fatal(err)
	}
	actor, p, cause := lifecycleTestActor(t)
	cause.Action = c.Delete
	req := ac.ProjectRetirementRequest{Actor: actor, ProjectID: p, AgentID: testID[i.Agent](t), Cause: cause, ExpectedVersion: 1}
	for _, lock := range req.RequiredLocks() {
		s.held[lock.Key.Canonical()] = lock.Mode
	}
	return a, s, req
}

func TestAgentRetirementAuthorityInput(t *testing.T) {
	for _, name := range []string{"nil-authority", "nil-context", "zero-tx", "cancelled", "human", "archive", "wrong-project", "wrong-cause", "zero-version", "zero-plan"} {
		t.Run(name, func(t *testing.T) {
			a, s, req := agentRetirementAuthorityFixture(t)
			ctx, tx := context.Background(), s.tx
			switch name {
			case "nil-authority":
				a = nil
			case "nil-context":
				ctx = nil
			case "zero-tx":
				tx = f.Tx{}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "human":
				req.Actor = testActor(t)
			case "archive":
				req.Cause.Action = c.Archive
			case "wrong-project":
				req.ProjectID = testID[i.Project](t)
			case "wrong-cause":
				req.Cause.OperationID = testID[c.Operation](t)
			case "zero-version":
				req.ExpectedVersion = 0
			}
			var err error
			if name == "zero-plan" {
				err = a.RequireAgentRetirementAppliedInTx(ctx, tx, ac.ProjectRetirementPlan{})
			} else {
				err = a.RequireAgentRetirementInTx(ctx, tx, req)
			}
			if err == nil || len(s.seen()) != 0 {
				t.Fatal("invalid input reached Store", err, s.seen())
			}
		})
	}
}

func TestAgentRetirementAuthorityOriginalTransactionAndLocks(t *testing.T) {
	for _, name := range []string{"foreign-tx", "expired-tx", "nil-executor", "project-lock", "schedule-lock", "agent-lock", "shared-project", "missing-lifecycle", "canonical-error"} {
		t.Run(name, func(t *testing.T) {
			a, s, req := agentRetirementAuthorityFixture(t)
			tx := s.tx
			switch name {
			case "foreign-tx":
				tx = f.NewTx()
			case "expired-tx":
				s.live = false
			case "nil-executor":
				s.nilExecutor = true
			case "project-lock", "schedule-lock", "agent-lock":
				n := map[string]int{"project-lock": 0, "schedule-lock": 1, "agent-lock": 2}[name]
				delete(s.held, req.RequiredLocks()[n].Key.Canonical())
			case "shared-project":
				s.held[req.RequiredLocks()[0].Key.Canonical()] = f.Shared
			case "missing-lifecycle":
				a.state().lifecycle = nil
			}
			err := a.RequireAgentRetirementInTx(context.Background(), tx, req)
			seen := s.seen()
			if err == nil || slices.Contains(seen, "forbidden-tx") || slices.Contains(seen, "forbidden-acquire") || slices.Contains(seen, "forbidden-write") {
				t.Fatal("authority acquired ownership or manufactured permission", err, seen)
			}
			if name == "canonical-error" {
				if !errors.Is(err, s.readErr) || !slices.Contains(seen, "row") {
					t.Fatal("lost original canonical read error", err, seen)
				}
			} else if slices.Contains(seen, "row") || slices.Contains(seen, "query") {
				t.Fatal("invalid original transaction/locks reached SQL", seen)
			}
		})
	}
}

type agentRetirementUncalledConsumer struct{ ac.ProjectRetirementConsumer }

func TestAgentRetirementAuthorityPrivateFamilies(t *testing.T) {
	for _, name := range []string{"missing", "skills-step", "closed", "wrong-project", "wrong-operation", "foreign-authority", "agent-step"} {
		t.Run(name, func(t *testing.T) {
			store := &agentRetirementAuthorityStore{&retirementPrepStore{tx: f.NewTx(), live: true, held: map[string]f.LockMode{}}}
			d, _, round := cleanupSkillsFixture(t, store, &cleanupSkillsPort{})
			d.state.agents = &agentRetirementUncalledConsumer{}
			round.agentStep = true
			witness := &lifecycleCleanupScope{state: d.state, round: round, open: true}
			store.held[projectLock(round.claim.project, f.Exclusive).Key.Canonical()] = f.Exclusive
			store.claim = &round.claim
			ctx := context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, witness)
			a := d.state.authority
			p, cause := round.claim.project, round.cause
			switch name {
			case "missing":
				ctx = context.Background()
			case "skills-step":
				witness.round.agentStep = false
			case "closed":
				witness.open = false
				ctx = context.WithoutCancel(ctx)
			case "wrong-project":
				p = testID[i.Project](t)
			case "wrong-operation":
				cause.OperationID = testID[c.Operation](t)
			case "foreign-authority":
				a = lifecycleTestAuthority(t)
			}
			err := a.requireAgentCleanupStepInTx(ctx, store.tx, cause, p)
			if name == "agent-step" {
				if err != nil || store.rows != 1 {
					t.Fatal("matching private claim did not reach original claim read", err)
				}
				// Only the private claim is controlled here, not canonical phase
				// authority. Its sibling Skills entry must reject the Agent step.
				hasCode(t, a.requireSkillsCleanupClaimInTx(ctx, store.tx, cause, p), f.Forbidden)
				if store.rows != 1 {
					t.Fatal("Agent scope reached sibling Skills SQL")
				}
			} else if err == nil || store.rows != 0 {
				t.Fatal("invalid private family reached claim SQL", err)
			}
		})
	}
}

func TestAgentRetirementAuthorityWaitsForOriginalRead(t *testing.T) {
	a, s, req := agentRetirementAuthorityFixture(t)
	s.readEntered, s.readRelease = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var once sync.Once
	release := func() { once.Do(func() { close(s.readRelease) }) }
	joined := false
	t.Cleanup(func() {
		cancel()
		release()
		if !joined {
			<-done
		}
	})
	go func() { done <- a.RequireAgentRetirementInTx(ctx, s.tx, req) }()
	select {
	case <-s.readEntered:
	case <-time.After(time.Second):
		t.Fatal("original canonical read did not enter")
	}
	cancel()
	select {
	case <-done:
		joined = true
		t.Fatal("cancellation replaced original SQL return")
	default:
	}
	release()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, s.readErr) {
			t.Fatal("lost original read failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("original SQL did not return")
	}
}
