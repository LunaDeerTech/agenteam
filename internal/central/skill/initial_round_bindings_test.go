package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// Reuse the actual initialization/capture implementations with their explicit
// SQL controls. These tests do not supply running authority or claim real PG.
func initialRoundFixture(t *testing.T, enabled bool) (*InitialRoundBindings, *captureBindingStore, sc.InitialRoundBindingsRequest) {
	t.Helper()
	capture, store, owner, ctx, request := captureFixture(t, enabled)
	plan, err := capture.DiscoverInitialBindings(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	var initial sc.InitialSkillBindings
	result := captureFinal(t, store, owner, ctx, plan, func(ctx context.Context, tx f.Tx) error {
		initial, err = capture.ResolveInitialBindingsInTx(ctx, tx, request, plan)
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("controlled initial capture", result.Fault())
	}
	authority, err := NewAuthority(store, &agentInitializationProjects{})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewInitialRoundBindings(authority)
	if err != nil {
		t.Fatal(err)
	}
	return reader, store, sc.InitialRoundBindingsRequest{Initial: initial, CaptureAttemptBinding: owner.scope.AttemptBinding}
}

func initialRoundTx(t *testing.T, s *captureBindingStore, locks []f.LockRequest, fn func(context.Context, f.Tx) error) f.CommitResult {
	t.Helper()
	cause, err := f.NewJobCause("initial-round-test", stateID[f.TransactionAttempt](241).String(), stateID[f.TransactionAttempt](242).String())
	if err != nil {
		t.Fatal(err)
	}
	return s.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		return fn(ctx, tx)
	})
}

func TestInitialRoundSkillBindingsFixedAndEmpty(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			p, s, r := initialRoundFixture(t, enabled)
			writes := s.writes
			plan, err := p.DiscoverInitialRoundBindings(context.Background(), r)
			if err != nil || len(plan.RequiredLocks()) != 5 {
				t.Fatal("complete initialized lock plan", err)
			}
			locks := plan.RequiredLocks()
			locks[0].Mode = ""
			if plan.RequiredLocks()[0].Mode == "" {
				t.Fatal("lock plan aliases caller")
			}
			result := initialRoundTx(t, s, plan.RequiredLocks(), func(ctx context.Context, tx f.Tx) error {
				transactions, acquires := s.txs, s.acquires
				got, err := p.ReadInitialRoundBindingsInTx(ctx, tx, r, plan)
				if err != nil {
					return err
				}
				if got.Validate() != nil || !sameInitialRoundRequest(r, sc.InitialRoundBindingsRequest{Initial: got, CaptureAttemptBinding: r.CaptureAttemptBinding}) {
					t.Fatal("did not read exact persisted directory")
				}
				if enabled {
					got.Bindings[0].Name = "local-only"
				}
				again, err := p.ReadInitialRoundBindingsInTx(ctx, tx, r, plan)
				if err != nil || !slices.Equal(again.Bindings, r.Initial.Bindings) || again.Bindings == nil {
					t.Fatal("alias or nil empty catalogue", err)
				}
				if s.txs != transactions || s.acquires != acquires || s.writes != writes {
					t.Fatal("reader began, acquired late locks or wrote facts")
				}
				return nil
			})
			if result.State() != f.Committed || s.writes != writes {
				t.Fatal("controlled read", result.Fault())
			}
			if got, err := p.ReadInitialRoundBindingsInTx(context.Background(), s.tx, r, plan); err == nil || got.Bindings != nil {
				t.Fatal("ended Tx accepted")
			}
		})
	}
}

func TestInitialRoundSkillBindingsRejectDriftAndMissingFacts(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*captureBindingStore)
	}{
		{"missing-capture-head", func(s *captureBindingStore) { s.captured = nil }},
		{"missing-assignment-head", func(s *captureBindingStore) { s.head = nil }},
		{"assignment-sequence", func(s *captureBindingStore) { s.head[6] = int64(2) }},
		{"extra-assignment", func(s *captureBindingStore) { s.assignments++ }},
		{"missing-ref", func(s *captureBindingStore) { s.refs = nil }},
		{"wrong-fixed-object", func(s *captureBindingStore) { s.refs[0][8] = stateID[f.TransactionAttempt](244).String() }},
		{"resource-unavailable", func(s *captureBindingStore) { s.published.values[9] = false }},
		{"malformed-record", func(s *captureBindingStore) { s.captured[6] = []byte(`{"format":1,"unknown":true}`) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, s, r := initialRoundFixture(t, true)
			plan, err := p.DiscoverInitialRoundBindings(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			writes := s.writes
			tc.mutate(s)
			result := initialRoundTx(t, s, plan.RequiredLocks(), func(ctx context.Context, tx f.Tx) error {
				got, err := p.ReadInitialRoundBindingsInTx(ctx, tx, r, plan)
				if err == nil || got.Bindings != nil {
					t.Fatal("drift produced a partial/empty success")
				}
				return err
			})
			if result.State() != f.NotCommitted || s.writes != writes {
				t.Fatal("drift wrote or committed")
			}
			if plan, err := p.DiscoverInitialRoundBindings(context.Background(), r); err == nil || plan != nil {
				t.Fatal("discovery concealed missing or changed source")
			}
		})
	}
	// Empty is still a persisted initialized capture, not an absent row.
	p, s, r := initialRoundFixture(t, false)
	s.captured = nil
	if plan, err := p.DiscoverInitialRoundBindings(context.Background(), r); err == nil || plan != nil {
		t.Fatal("absent head became empty directory")
	}
}

func TestInitialRoundSkillBindingsCallerBoundaryAndOutcome(t *testing.T) {
	p, s, r := initialRoundFixture(t, true)
	plan, err := p.DiscoverInitialRoundBindings(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	other, _, _ := initialRoundFixture(t, true)
	changed := r.Clone()
	changed.Initial.Bindings[0].Description = "private-round-canary"
	for _, candidate := range []struct {
		reader  *InitialRoundBindings
		request sc.InitialRoundBindingsRequest
		locks   []f.LockRequest
	}{
		{other, r, plan.RequiredLocks()},
		{p, changed, plan.RequiredLocks()},
		{p, r, plan.RequiredLocks()[:4]},
	} {
		result := initialRoundTx(t, s, candidate.locks, func(ctx context.Context, tx f.Tx) error {
			got, err := candidate.reader.ReadInitialRoundBindingsInTx(ctx, tx, candidate.request, plan)
			if err == nil || got.Bindings != nil {
				t.Fatal("wrong issuer/request/held set accepted")
			}
			return err
		})
		if result.State() != f.NotCommitted {
			t.Fatal("invalid caller committed")
		}
	}
	for _, unknown := range []bool{false, true} {
		p, s, r := initialRoundFixture(t, false)
		ctx, cancel := context.WithCancel(context.Background())
		s.unknown, s.afterTx = unknown, cancel
		got, err := p.DiscoverInitialRoundBindings(ctx, r)
		if got != nil || err == nil || s.live {
			t.Fatal("returned a plan before the actual read tail")
		}
		if unknown {
			if _, ok := UnknownAttempt(err); !ok {
				t.Fatal("physical Unknown overwritten by cancellation")
			}
		} else if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled committed discovery exposed a plan")
		}
	}
	if _, err := NewInitialRoundBindings(nil); err == nil {
		t.Fatal("nil authority accepted")
	}
	raw, err := json.Marshal(changed)
	if err != nil || string(raw) != `"skill_initial_round_bindings_request"` || fmt.Sprint(changed) != "skill_initial_round_bindings_request" {
		t.Fatal("request leaked catalogue in default output")
	}
}
