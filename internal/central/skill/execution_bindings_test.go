package skill

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// These storage and owner doubles exercise the production provider, with the
// existing real initialization implementation. They are not Execution grants,
// PostgreSQL rollback evidence, or a complete preparation/Snapshot fixture.
type captureBindingStore struct {
	*agentInitializationStore
	captured []any
	refs     [][]any
	writes   int
	failRef  error
	afterTx  func()
}

func (s *captureBindingStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.agentInitializationStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *captureBindingStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	head, refs := slices.Clone(s.captured), slices.Clone(s.refs)
	result := s.agentInitializationStore.WithinTx(ctx, cause, fn)
	if result.State() != f.Committed {
		s.captured, s.refs = head, refs
	}
	if s.afterTx != nil {
		s.afterTx()
	}
	return result
}
func (s *captureBindingStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	switch {
	case strings.HasPrefix(q, "SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.agent_assignment_heads"):
		if !strings.Contains(q, "FROM agenteam_skill.execution_binding_heads") || !strings.Contains(q, "FROM agenteam_skill.execution_bindings") {
			return skillRowValues{err: errors.New("missing execution protection")}
		}
		return skillRowValues{values: []any{s.head == nil && s.assignments == 0 && s.captured == nil && len(s.refs) == 0}}
	case strings.HasPrefix(q, "SELECT project_id::text,agent_id::text,assignment_sequence,attempt_binding,source_digest,binding_count,record"):
		if s.captured == nil {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		return skillRowValues{values: s.captured}
	case strings.HasPrefix(q, "SELECT count(*) FROM agenteam_skill.execution_bindings"):
		count := int64(len(s.refs))
		if len(args) > 1 {
			count = 0
			for _, v := range s.refs {
				tuple := []any{v[0], v[1], v[2], v[6], v[7], v[3], v[4], v[5], v[8]}
				if reflect.DeepEqual(tuple, args) {
					count++
				}
			}
		}
		return skillRowValues{values: []any{count}}
	default:
		return s.agentInitializationStore.QueryRow(ctx, q, args...)
	}
}
func (s *captureBindingStore) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !s.live {
		return pgconn.CommandTag{}, errors.New("write outside caller transaction")
	}
	switch {
	case strings.HasPrefix(q, "INSERT INTO agenteam_skill.execution_binding_heads"):
		if s.captured != nil {
			return pgconn.CommandTag{}, errors.New("duplicate head")
		}
		s.captured = []any{args[1], args[2], args[3], args[4], args[5], int64(args[6].(int)), slices.Clone(args[7].([]byte))}
	case strings.HasPrefix(q, "INSERT INTO agenteam_skill.execution_bindings"):
		if s.failRef != nil {
			return pgconn.CommandTag{}, s.failRef
		}
		s.refs = append(s.refs, slices.Clone(args))
	default:
		return pgconn.CommandTag{}, errors.New("unexpected capture write")
	}
	s.writes++
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type captureCallKey struct{}
type captureOwnerDouble struct {
	store           *captureBindingStore
	request         sc.SkillCaptureRequest
	scope           sc.SkillCaptureScope
	finalTx         f.Tx
	discover, final int
	err             error
	onCall          func(bool)
}

func (o *captureOwnerDouble) check(ctx context.Context, tx f.Tx, r sc.SkillCaptureRequest, final bool) (sc.SkillCaptureScope, error) {
	if final {
		o.final++
	} else {
		o.discover++
	}
	if o.onCall != nil {
		o.onCall(final)
	}
	if o.err != nil {
		return sc.SkillCaptureScope{}, o.err
	}
	if ctx.Value(captureCallKey{}) != o || r != o.request || !o.store.live || tx != o.store.tx || final && tx != o.finalTx {
		return sc.SkillCaptureScope{}, fault(f.Forbidden)
	}
	return o.scope, nil
}
func (o *captureOwnerDouble) RequireSkillCaptureDiscoveryInTx(ctx context.Context, tx f.Tx, r sc.SkillCaptureRequest) (sc.SkillCaptureScope, error) {
	return o.check(ctx, tx, r, false)
}
func (o *captureOwnerDouble) RequireSkillCaptureInTx(ctx context.Context, tx f.Tx, r sc.SkillCaptureRequest) (sc.SkillCaptureScope, error) {
	return o.check(ctx, tx, r, true)
}

func captureFixture(t *testing.T, enabled bool) (*ExecutionBindings, *captureBindingStore, *captureOwnerDouble, context.Context, sc.SkillCaptureRequest) {
	t.Helper()
	initializer, base, projects, creation, r := agentInitializerFixture(t)
	r.AddSkillsEnabled = enabled
	plan, err := initializer.DiscoverNewAgentInitialization(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	cause, _ := f.NewCommandsCause(r.Command)
	result := base.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
		if err := base.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		creation.witnessTx = tx
		return initializer.InitializeNewAgentInTx(ctx, tx, r, plan)
	})
	if result.State() != f.Committed {
		t.Fatal("controlled initialized source", result.Fault())
	}
	store := &captureBindingStore{agentInitializationStore: base}
	request := sc.SkillCaptureRequest{ProjectID: r.ProjectID, AgentID: r.AgentID, ExecutionID: stateID[id.Execution](230)}
	binding, _, err := captureDigest("original preparing attempt")
	if err != nil {
		t.Fatal(err)
	}
	owner := &captureOwnerDouble{store: store, request: request, scope: sc.SkillCaptureScope{Project: projects.project, AttemptBinding: binding}}
	authority, err := NewAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := NewExecutionBindings(authority, owner)
	if err != nil {
		t.Fatal(err)
	}
	return provider, store, owner, context.WithValue(context.Background(), captureCallKey{}, owner), request
}
func captureFinal(t *testing.T, s *captureBindingStore, o *captureOwnerDouble, ctx context.Context, plan sc.InitialBindingsPlan, fn func(context.Context, f.Tx) error) f.CommitResult {
	t.Helper()
	cause, err := f.NewJobCause("skill-capture-test", "final", "capture")
	if err != nil {
		t.Fatal(err)
	}
	return s.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		o.finalTx = tx
		return fn(ctx, tx)
	})
}

func TestExecutionSkillBindingsFixedRevisionAndEmptyHead(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		p, s, o, ctx, r := captureFixture(t, enabled)
		plan, err := p.DiscoverInitialBindings(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		if o.discover != 2 || s.writes != 0 || len(plan.RequiredLocks()) != 5 {
			t.Fatal("discovery bypassed real source/lock planning")
		}
		locks := plan.RequiredLocks()
		locks[0].Mode = ""
		if plan.RequiredLocks()[0].Mode == "" {
			t.Fatal("lock alias")
		}
		result := captureFinal(t, s, o, ctx, plan, func(ctx context.Context, tx f.Tx) error {
			beforeTx, beforeAcquire := s.txs, s.acquires
			got, err := p.ResolveInitialBindingsInTx(ctx, tx, r, plan)
			if err != nil {
				return err
			}
			if got.Validate() != nil || got.AssignmentSequence != 1 || got.Bindings == nil {
				t.Fatal("invalid fixed initial set")
			}
			want := 0
			if enabled {
				want = 1
			}
			if len(got.Bindings) != want || s.captured == nil || len(s.refs) != want {
				t.Fatal("head or fixed revision protection missing")
			}
			if enabled {
				b := got.Bindings[0]
				src := plan.(*initialBindingsPlan).source
				if b.RevisionID != src.RevisionID || b.PackageSHA256 != src.PackageDigest || b.EntryPath != sc.EntryPath {
					t.Fatal("unfixed package")
				}
				got.Bindings[0].Name = "changed local copy"
			}
			again, err := p.ResolveInitialBindingsInTx(ctx, tx, r, plan)
			if err != nil {
				return err
			}
			if again.Validate() != nil || enabled && again.Bindings[0].Name == "changed local copy" {
				t.Fatal("result aliases plan")
			}
			if s.writes != 1+want || s.txs != beforeTx || s.acquires != beforeAcquire {
				t.Fatal("reentry rewrote or began/acquired in caller Tx")
			}
			return nil
		})
		if result.State() != f.Committed || o.final != 2 {
			t.Fatal("controlled final failed", result.Fault())
		}
		if got, err := p.ResolveInitialBindingsInTx(ctx, s.tx, r, plan); err == nil || got.Bindings != nil {
			t.Fatal("ended transaction accepted")
		}
	}
}

func TestExecutionSkillBindingsRejectSourceAndAttemptDrift(t *testing.T) {
	for _, name := range []string{"issuer", "request", "foreign_tx", "held", "proof", "attempt", "source", "missing_head", "publication", "references"} {
		t.Run(name, func(t *testing.T) {
			p, s, o, ctx, r := captureFixture(t, true)
			plan, err := p.DiscoverInitialBindings(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			result := captureFinal(t, s, o, ctx, plan, func(ctx context.Context, tx f.Tx) error {
				switch name {
				case "issuer":
					p = &ExecutionBindings{store: s, executions: o}
				case "request":
					r.ExecutionID = stateID[id.Execution](231)
				case "foreign_tx":
					tx = f.NewTx()
				case "held":
					s.held = map[string]f.LockMode{}
				case "proof":
					o.finalTx = f.NewTx()
				case "attempt":
					o.scope.AttemptBinding, _, _ = captureDigest("later attempt")
				case "source":
					s.published.values[8] = int64(2)
				case "missing_head":
					s.head = nil
				case "publication":
					s.published.values[9] = false
				case "references":
					s.assignments = 2
				}
				got, err := p.ResolveInitialBindingsInTx(ctx, tx, r, plan)
				if err == nil || got.Bindings != nil || s.writes != 0 {
					t.Fatal("untrusted or stale capture wrote/returned data")
				}
				return err
			})
			if result.State() != f.NotCommitted {
				t.Fatal("rejected capture committed")
			}
		})
	}
	p, s, _, ctx, r := captureFixture(t, false)
	s.head = nil
	if plan, err := p.DiscoverInitialBindings(ctx, r); err == nil || plan != nil {
		t.Fatal("false manufactured missing initialized head")
	}
	if _, err := NewExecutionBindings(nil, nil); err == nil {
		t.Fatal("unbound constructor")
	}
}

func TestExecutionSkillBindingsKeepOriginalOutcomeAndRollback(t *testing.T) {
	p, s, o, ctx, r := captureFixture(t, true)
	plan, err := p.DiscoverInitialBindings(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("controlled ref insert failed")
	s.failRef = sentinel
	result := captureFinal(t, s, o, ctx, plan, func(ctx context.Context, tx f.Tx) error {
		got, err := p.ResolveInitialBindingsInTx(ctx, tx, r, plan)
		if err == nil || got.Bindings != nil {
			t.Fatal("partial result escaped")
		}
		return err
	})
	if result.State() != f.NotCommitted || s.captured != nil || len(s.refs) != 0 {
		t.Fatal("controlled rollback left partial protection")
	}

	// Cancellation after a read Tx physically returns must not publish a plan;
	// an actual Unknown physical outcome remains the original attempt instead.
	for _, unknown := range []bool{false, true} {
		p, s, _, ctx, r := captureFixture(t, false)
		ctx, cancel := context.WithCancel(ctx)
		s.unknown = unknown
		s.afterTx = cancel
		got, err := p.DiscoverInitialBindings(ctx, r)
		if got != nil || err == nil || s.live {
			t.Fatal("read outcome/return tail lost")
		}
		if unknown {
			if _, ok := UnknownAttempt(err); !ok {
				t.Fatal("physical unknown overwritten by cancellation")
			}
		} else if !errors.Is(err, context.Canceled) {
			t.Fatal("committed cancelled read returned plan")
		}
	}
	p, s, o, ctx, r = captureFixture(t, true)
	plan, err = p.DiscoverInitialBindings(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	o.err = fmt.Errorf("private callback material: %w", context.Canceled)
	result = captureFinal(t, s, o, ctx, plan, func(ctx context.Context, tx f.Tx) error {
		got, err := p.ResolveInitialBindingsInTx(ctx, tx, r, plan)
		if err != context.Canceled || got.Bindings != nil || strings.Contains(fmt.Sprint(err), "private") {
			t.Fatal("callback cancellation leaked or escaped")
		}
		return err
	})
	if result.State() != f.NotCommitted || s.writes != 0 {
		t.Fatal("cancelled callback wrote")
	}
}

func TestExecutionSkillBindingsProtectCleanup(t *testing.T) {
	p, s, o, ctx, r := captureFixture(t, false)
	plan, err := p.DiscoverInitialBindings(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	result := captureFinal(t, s, o, ctx, plan, func(ctx context.Context, tx f.Tx) error {
		_, e := p.ResolveInitialBindingsInTx(ctx, tx, r, plan)
		return e
	})
	if result.State() != f.Committed {
		t.Fatal(result.Fault())
	}
	// Isolate each independent reference guard in the controlled storage query.
	s.head = nil
	s.assignments = 0
	empty, err := agentAssignmentsEmpty(ctx, s, r.ProjectID)
	if err != nil || empty {
		t.Fatal("empty captured head failed to protect cleanup", err)
	}
	s.captured = nil
	s.refs = [][]any{{"retained fixed revision"}}
	empty, err = agentAssignmentsEmpty(ctx, s, r.ProjectID)
	if err != nil || empty {
		t.Fatal("fixed revision failed to protect cleanup", err)
	}
	s.refs = nil
	empty, err = agentAssignmentsEmpty(ctx, s, r.ProjectID)
	if err != nil || !empty {
		t.Fatal("fully empty scope stayed protected", err)
	}
}
