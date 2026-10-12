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
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Only Identity is reachable: the original Store stops at its canonical read
// sentinel. No embedded owner method supplies a fake cleanup authorization.
type terminalAuthorityIdentity struct {
	ac.ProjectTerminalCleanupDomain
	identity c.ProjectTerminalOwnerIdentity
}

func (p *terminalAuthorityIdentity) ProjectTerminalCleanupIdentity() c.ProjectTerminalOwnerIdentity {
	return p.identity
}

func terminalAuthorityFixture(t *testing.T) (*Authority, *agentRetirementAuthorityStore, c.ProjectTerminalCleanupRequest, *lifecycleCleanupScope, context.Context) {
	t.Helper()
	a, store, req := agentRetirementAuthorityFixture(t)
	owner := c.ProjectTerminalOwnerIdentity{Kind: c.ProjectTerminalAgentOwner, ProcessID: testID[struct{}](t).String(), InstanceID: testID[struct{}](t).String()}
	pv := owner
	pv.Kind, pv.InstanceID = c.ProjectTerminalVariablesOwner, testID[struct{}](t).String()
	st := &lifecycleCleanupState{store: store, authority: a.state().lifecycle.(*LifecycleAuthority), terminalAgents: &terminalAuthorityIdentity{identity: owner}, terminalVariables: &terminalAuthorityIdentity{identity: pv}}
	w := &lifecycleCleanupScope{state: st, open: true, round: lifecycleCleanupRound{claim: lifecycleStopClaim{project: req.ProjectID, operation: req.Cause.OperationID}, cause: req.Cause, participant: c.SkillsParticipant, terminalStep: true}}
	r := c.ProjectTerminalCleanupRequest{Actor: req.Actor, Cause: req.Cause, ProjectID: req.ProjectID, Owner: owner}
	if err := r.Validate(); err != nil {
		t.Fatal("invalid complete fixture", err)
	}
	return a, store, r, w, context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, w)
}

func TestTerminalCleanupAuthorityStoreAndScope(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "expired-tx", "nil-executor", "missing-project", "shared-project", "wrong-store", "no-scope", "closed", "agent-family", "variables-family", "confirmation", "not-applied", "canonical-error"} {
		t.Run(mode, func(t *testing.T) {
			a, s, req, w, ctx := terminalAuthorityFixture(t)
			tx := s.tx
			key := req.RequiredLocks()[0].Key.Canonical()
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "expired-tx":
				s.live = false
			case "nil-executor":
				s.nilExecutor = true
			case "missing-project":
				delete(s.held, key)
			case "shared-project":
				s.held[key] = f.Shared
			case "wrong-store":
				a.state().lifecycle = lifecycleTestAuthority(t)
			case "no-scope":
				ctx = context.Background()
			case "closed":
				w.open = false
			case "agent-family":
				w.round.agentStep = true
			case "variables-family":
				w.round.variablesStep = true
			case "confirmation":
				req.Confirmation = true
			case "not-applied":
				req.Confirmation, req.NotApplied = true, true
			}
			err := a.RequireProjectTerminalCleanupInTx(ctx, tx, req)
			seen := s.seen()
			if err == nil || slices.Contains(seen, "forbidden-tx") || slices.Contains(seen, "forbidden-acquire") || slices.Contains(seen, "forbidden-write") {
				t.Fatal("terminal gate manufactured a transaction or grant", err, seen)
			}
			if mode == "canonical-error" {
				if !errors.Is(err, s.readErr) || !slices.Contains(seen, "row") {
					t.Fatal("original canonical error lost", err, seen)
				}
			} else if slices.Contains(seen, "row") || slices.Contains(seen, "query") {
				t.Fatal("invalid original scope reached SQL", seen)
			}
		})
	}
}

func TestTerminalCleanupAuthorityOriginalReturn(t *testing.T) {
	a, s, req, w, ctx := terminalAuthorityFixture(t)
	s.readEntered, s.readRelease = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	var once sync.Once
	free := func() { once.Do(func() { close(s.readRelease) }) }
	joined := false
	t.Cleanup(func() {
		cancel()
		free()
		if !joined {
			<-done
		}
	})
	go func() { done <- a.RequireProjectTerminalCleanupInTx(ctx, s.tx, req) }()
	select {
	case <-s.readEntered:
	case <-time.After(time.Second):
		t.Fatal("original canonical Scan not reached")
	}
	cancel()
	select {
	case <-done:
		joined = true
		t.Fatal("cancellation replaced original Scan return")
	default:
	}
	free()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, s.readErr) {
			t.Fatal("original Scan error replaced", err)
		}
	case <-time.After(time.Second):
		t.Fatal("original Scan did not return")
	}
	w.mu.Lock()
	w.open = false
	w.mu.Unlock()
	hasCode(t, a.RequireProjectTerminalCleanupInTx(context.WithoutCancel(ctx), s.tx, req), f.Forbidden)
}
