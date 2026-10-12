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
)

// The existing actual-token/held-lock store returns a canonical SQL sentinel.
// These controls do not fabricate a successful lifecycle manifest or claim.
func TestVariablesCleanupAuthorityInput(t *testing.T) {
	for _, mode := range []string{"nil-authority", "nil-context", "zero-tx", "cancelled", "human", "archive", "wrong-project", "wrong-operation"} {
		t.Run(mode, func(t *testing.T) {
			a, s, r := agentRetirementAuthorityFixture(t)
			ctx, tx := context.Background(), s.tx
			switch mode {
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
				r.Actor = testActor(t)
			case "archive":
				r.Cause.Action = c.Archive
			case "wrong-project":
				r.ProjectID = testID[i.Project](t)
			case "wrong-operation":
				r.Cause.OperationID = testID[c.Operation](t)
			}
			if err := a.RequireProjectVariablesCleanupInTx(ctx, tx, r.Actor, r.Cause, r.ProjectID); err == nil || len(s.seen()) != 0 {
				t.Fatal("invalid cleanup reached Store", err, s.seen())
			}
		})
	}
}

func TestVariablesCleanupAuthorityOriginalStoreAndLock(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "expired-tx", "nil-executor", "missing-project-lock", "shared-project", "foreign-lifecycle", "canonical-error"} {
		t.Run(mode, func(t *testing.T) {
			a, s, r := agentRetirementAuthorityFixture(t)
			tx := s.tx
			key := projectLock(r.ProjectID, f.Exclusive).Key.Canonical()
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "expired-tx":
				s.live = false
			case "nil-executor":
				s.nilExecutor = true
			case "missing-project-lock":
				delete(s.held, key)
			case "shared-project":
				s.held[key] = f.Shared
			case "foreign-lifecycle":
				a.state().lifecycle = lifecycleTestAuthority(t)
			}
			err := a.RequireProjectVariablesCleanupInTx(context.Background(), tx, r.Actor, r.Cause, r.ProjectID)
			seen := s.seen()
			if err == nil || slices.Contains(seen, "forbidden-tx") || slices.Contains(seen, "forbidden-acquire") || slices.Contains(seen, "forbidden-write") {
				t.Fatal("cleanup manufactured an original transaction or grant", err, seen)
			}
			if mode == "canonical-error" {
				if !errors.Is(err, s.readErr) || !slices.Contains(seen, "row") {
					t.Fatal("original canonical error lost", err, seen)
				}
			} else if slices.Contains(seen, "row") || slices.Contains(seen, "query") {
				t.Fatal("invalid Tx/P EX reached canonical SQL", seen)
			}
		})
	}
}

func TestVariablesCleanupAuthorityOriginalReadReturn(t *testing.T) {
	a, s, r := agentRetirementAuthorityFixture(t)
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
	go func() { done <- a.RequireProjectVariablesCleanupInTx(ctx, s.tx, r.Actor, r.Cause, r.ProjectID) }()
	select {
	case <-s.readEntered:
	case <-time.After(time.Second):
		t.Fatal("original canonical Scan never entered")
	}
	cancel()
	select {
	case <-done:
		joined = true
		t.Fatal("cancellation replaced original Scan return")
	default:
	}
	release()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, s.readErr) {
			t.Fatal("original read failure lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("original Scan did not return")
	}
}
