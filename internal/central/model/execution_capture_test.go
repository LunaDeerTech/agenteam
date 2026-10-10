package model

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled SQL/owner protocol tests. Real credentials, physical commits and
// Execution-issued preparation proof are covered by the integration fixture.
type modelCaptureStore struct {
	Store
	tx                          f.Tx
	live, missingLocks          bool
	locks                       []f.LockRequest
	refs                        []any
	row                         *resolutionPreparation
	readErr                     error
	unknown                     bool
	returned                    bool
	attempt                     f.ID[f.TransactionAttempt]
	cause                       f.TransactionCause
	after                       context.CancelFunc
	reads, writes, transactions int
}

func (s *modelCaptureStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *modelCaptureStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions++
	if cause.Validate() != nil {
		panic("invalid transaction cause")
	}
	s.live = true
	err := fn(ctx, s.tx)
	s.live = false
	s.returned = true
	if s.after != nil {
		s.after()
	}
	if err != nil {
		var ff *f.Fault
		if errors.As(err, &ff) {
			return f.NotCommittedResult(ff)
		}
		return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
	}
	if s.unknown {
		s.cause = cause
		return f.UnknownResult(s.attempt, cause)
	}
	return f.CommittedResult()
}
func (s *modelCaptureStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.locks = slices.Clone(locks)
	return nil
}
func (s *modelCaptureStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	if s.missingLocks || !slices.EqualFunc(s.locks, locks, func(a, b f.LockRequest) bool { return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode }) {
		return fault(f.Forbidden)
	}
	return nil
}
func (s *modelCaptureStore) QueryRow(_ context.Context, sql string, _ ...any) postgres.Row {
	s.reads++
	return scanFunc(func(dest ...any) error {
		if s.readErr != nil {
			return s.readErr
		}
		var values []any
		switch {
		case strings.HasPrefix(sql, "SELECT array_agg(role ORDER BY role)"):
			values = s.refs
		case strings.HasPrefix(sql, "SELECT id::text,resolution_identity"):
			if s.row == nil {
				return pgx.ErrNoRows
			}
			r := s.row
			request, _ := encoded(r.Request)
			draft, _ := encoded(r.Draft)
			now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
			values = []any{r.ID, r.Identity, r.Project, r.OwnerKind, r.OwnerID, r.Semantic, request, r.SnapshotID.String(), r.Phase, r.Version, draft, now, now, (*time.Time)(nil)}
		default:
			panic("unexpected SQL: no runtime adapter or writes allowed")
		}
		if len(values) != len(dest) {
			panic("unexpected projection")
		}
		for n, v := range values {
			reflect.ValueOf(dest[n]).Elem().Set(reflect.ValueOf(v))
		}
		return nil
	})
}
func (s *modelCaptureStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.writes++
	panic("capture observation must not replay a writer")
}

type modelCaptureOwner struct {
	store            *modelCaptureStore
	request          mc.ExecutionModelCaptureRequest
	facts            mc.ExecutionModelCaptureFacts
	err              error
	cancel           context.CancelFunc
	discovery, final int
}

func (o *modelCaptureOwner) check(ctx context.Context, tx f.Tx, r mc.ExecutionModelCaptureRequest) error {
	if _, err := o.store.InTx(tx); err != nil {
		return err
	}
	if r != o.request {
		return fault(f.Forbidden)
	}
	if o.cancel != nil {
		o.cancel()
	}
	return o.err
}
func (o *modelCaptureOwner) RequireModelCaptureDiscoveryInTx(ctx context.Context, tx f.Tx, r mc.ExecutionModelCaptureRequest) (mc.ExecutionModelCaptureScope, error) {
	o.discovery++
	return o.facts.Scope, o.check(ctx, tx, r)
}
func (o *modelCaptureOwner) RequireModelCaptureInTx(ctx context.Context, tx f.Tx, r mc.ExecutionModelCaptureRequest) (mc.ExecutionModelCaptureFacts, error) {
	o.final++
	return o.facts.Clone(), o.check(ctx, tx, r)
}
func modelCaptureFixture(t *testing.T) (*ExecutionCapture, *modelCaptureStore, *modelCaptureOwner, mc.ExecutionModelCaptureRequest) {
	t.Helper()
	resolve := resolutionTestRequest(t)
	r := mc.ExecutionModelCaptureRequest{ProjectID: resolve.Consumer.ProjectID, AgentID: *resolve.Consumer.AgentID, ExecutionID: *resolve.Consumer.ExecutionID}
	s := &modelCaptureStore{tx: f.NewTx(), attempt: mustID[f.TransactionAttempt](t)}
	approval := mustID[mc.Model](t)
	s.refs = []any{[]string{"agent_model", "approval_model"}, []string{r.ProjectID.String(), r.ProjectID.String()}, []string{resolve.ModelRef.String(), approval.String()}, []int64{3, 3}, []string{"", ""}}
	instant, _ := f.NewInstant(time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))
	project := pc.ProjectRef{ID: r.ProjectID, OwnerUserID: mustID[id.User](t), Name: "model-capture", NormalizedName: "model-capture", Lifecycle: pc.Active, Version: 1, CreatedAt: instant, UpdatedAt: instant}
	o := &modelCaptureOwner{store: s, request: r, facts: mc.ExecutionModelCaptureFacts{Scope: mc.ExecutionModelCaptureScope{Project: project, AttemptBinding: hash([]byte("attempt"))}, AgentVersion: 3, Selection: mc.AgentModelSelection{ModelID: *resolve.ModelRef, ApprovalModelID: &approval}}}
	svc, err := New(s, resolutionTestAuthority(t, s), testDependencies(t))
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewExecutionCapture(svc, o)
	if err != nil {
		t.Fatal(err)
	}
	return p, s, o, r
}
func TestExecutionModelCaptureCurrentSourceAndClosedProfile(t *testing.T) {
	p, s, o, r := modelCaptureFixture(t)
	source, err := readExecutionModelSource(context.Background(), s, r)
	if err != nil || source.Version != 3 || !source.Selection.Equal(o.facts.Selection) {
		t.Fatal("complete Model-owned source rejected", err)
	}
	request, err := executionModelResolveRequest(r, source)
	if err != nil || request.Validate() != nil || request.Purpose != mc.AgentGeneration || request.Selection.Kind != "direct" || request.Consumer.Kind != mc.AgentConsumer || request.LeaseOwner.Details().ID != r.ExecutionID.String() || *request.ModelRef != source.Selection.ModelID {
		t.Fatal("primary resolution source changed", err)
	}
	for _, mode := range []string{"missing", "foreign", "version", "approval-effort", "unknown-role", "duplicate"} {
		_, x, _, r := modelCaptureFixture(t)
		switch mode {
		case "missing":
			x.refs = []any{[]string{}, []string{}, []string{}, []int64{}, []string{}}
		case "foreign":
			x.refs[1].([]string)[0] = mustID[id.Project](t).String()
		case "version":
			x.refs[3].([]int64)[1]++
		case "approval-effort":
			x.refs[4].([]string)[1] = "high"
		case "unknown-role":
			x.refs[0].([]string)[1] = "other"
		case "duplicate":
			x.refs[0].([]string)[1] = "agent_model"
		}
		out, err := readExecutionModelSource(context.Background(), x, r)
		if err == nil || out.Version != 0 {
			t.Fatal("bad source returned partially", mode, err)
		}
	}
	// The actual original Resolver rejects effort before any capability/network
	// lookup; this adapter must not turn Agent metadata into profile support.
	s.refs[4].([]string)[0] = "high"
	plan, err := p.DiscoverExecutionModel(context.Background(), r)
	requireCode(t, err, f.CapabilityUnsupported)
	if plan != nil || s.transactions != 1 || !s.returned || o.discovery != 1 || s.writes != 0 {
		t.Fatal("closed profile bypassed")
	}
}
func TestExecutionModelCaptureRejectsStaleOriginalTransaction(t *testing.T) {
	for _, mode := range []string{"attempt", "version", "primary", "approval", "stored-source", "foreign-tx", "missing-lock", "issuer", "denied", "cancel"} {
		p, s, o, r := modelCaptureFixture(t)
		source, err := readExecutionModelSource(context.Background(), s, r)
		if err != nil {
			t.Fatal(err)
		}
		request, err := executionModelResolveRequest(r, source)
		if err != nil {
			t.Fatal(err)
		}
		// Same-package plan scaffold exercises rejection before delegating to
		// Resolver. It is not a consumer-issued successful resolution plan.
		plan := &executionModelPlan{owner: p, request: r, attempt: o.facts.Scope.AttemptBinding, source: source, resolve: request, locks: executionModelLocks(r)}
		s.locks = plan.RequiredLocks()
		s.live = true
		tx := s.tx
		ctx, cancel := context.WithCancel(context.Background())
		switch mode {
		case "attempt":
			o.facts.Scope.AttemptBinding = hash([]byte("another attempt"))
		case "version":
			o.facts.AgentVersion++
		case "primary":
			o.facts.Selection.ModelID = mustID[mc.Model](t)
		case "approval":
			o.facts.Selection.ApprovalModelID = nil
		case "stored-source":
			s.refs[3] = []int64{4, 4}
		case "foreign-tx":
			tx = f.NewTx()
		case "missing-lock":
			s.missingLocks = true
		case "issuer":
			p, _ = NewExecutionCapture(p.models, o)
		case "denied":
			o.err = fault(f.Forbidden)
		case "cancel":
			o.cancel = cancel
		}
		out, err := p.ResolveExecutionModelInTx(ctx, tx, r, plan)
		cancel()
		if err == nil || out.Validate() == nil || s.writes != 0 || s.transactions != 0 {
			t.Fatal("invalid final source published or opened Tx", mode, err)
		}
	}
}
func TestExecutionModelCaptureDiscoveryUnknownObservation(t *testing.T) {
	p, s, o, r := modelCaptureFixture(t)
	s.unknown = true
	ctx, cancel := context.WithCancel(context.Background())
	s.after = cancel
	plan, original := p.DiscoverExecutionModel(ctx, r)
	var physical *UnknownCommandError
	if plan != nil || !errors.As(original, &physical) || physical.AttemptID() != s.attempt || !s.returned || s.writes != 0 {
		t.Fatal("physical Unknown lost to cancellation", original)
	}
	s.unknown = false
	s.after = nil
	before := s.reads
	state, err := p.ObserveExecutionModelDiscovery(context.Background(), r, original)
	if err != nil || state != mc.ModelCaptureReadOnly || s.reads != before || o.discovery != 2 || s.writes != 0 {
		t.Fatal("read-only Unknown replayed source or writer", state, err)
	}
	state, err = p.ObserveExecutionModelDiscovery(context.Background(), r, physical)
	if err == nil || state != mc.ModelCapturePending {
		t.Fatal("public Unknown forged private observation")
	}
	o.facts.Scope.AttemptBinding = hash([]byte("changed attempt"))
	state, err = p.ObserveExecutionModelDiscovery(context.Background(), r, original)
	if err == nil || state != mc.ModelCapturePending {
		t.Fatal("new attempt retired old owner")
	}
	if strings.Contains(fmt.Sprintf("%+v", original), r.ExecutionID.String()) {
		t.Fatal("Unknown leaked identifiers")
	}
	p, s, o, r = modelCaptureFixture(t)
	ctx, cancel = context.WithCancel(context.Background())
	o.cancel = cancel
	plan, err = p.DiscoverExecutionModel(ctx, r)
	if plan != nil || !errors.Is(err, context.Canceled) || s.reads != 0 || s.writes != 0 {
		t.Fatal("canceled proof published discovery", err)
	}
}
func TestExecutionModelCapturePreparedObservationIsExactReadOnly(t *testing.T) {
	p, s, o, r := modelCaptureFixture(t)
	source, err := readExecutionModelSource(context.Background(), s, r)
	if err != nil {
		t.Fatal(err)
	}
	request, err := executionModelResolveRequest(r, source)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := resolutionIdentity(request)
	if err != nil {
		t.Fatal(err)
	}
	draft := resolutionTestDraft(t, request)
	row := &resolutionPreparation{ID: mustID[struct{}](t).String(), Identity: identity.Canonical(), Project: r.ProjectID.String(), OwnerKind: string(request.LeaseOwner.Details().Kind), OwnerID: r.ExecutionID.String(), Semantic: resolutionSemantic(request), Request: resolutionRequest(request), SnapshotID: draft.Snapshot.ID, Phase: "prepared", Version: 1, Draft: draft}
	c := &resolutionCandidate{authority: p.models.state().authority, request: request, identity: identity, row: row, version: 1, draft: draft, locks: executionModelLocks(r)}
	cause, err := f.NewCommandsCause(identity)
	if err != nil {
		t.Fatal(err)
	}
	// Controlled physical result tests the private observer's exact durable
	// matching. Only DiscoverResolve calls this wrapper in production.
	original := resolutionDiscoveryOutcome(f.UnknownResult(s.attempt, cause), c, false)
	var discovery *resolutionDiscoveryUnknown
	if !errors.As(original, &discovery) {
		t.Fatal("private original source missing")
	}
	capture := &executionCaptureUnknown{owner: p, request: r, attempt: o.facts.Scope.AttemptBinding, source: source, original: original, discovery: discovery, locks: executionModelLocks(r)}
	state, err := p.ObserveExecutionModelDiscovery(context.Background(), r, capture)
	if err != nil || state != mc.ModelCapturePending {
		t.Fatal("absence guessed rollback", err)
	}
	stored := *row
	s.row = &stored
	state, err = p.ObserveExecutionModelDiscovery(context.Background(), r, capture)
	if err != nil || state != mc.ModelCapturePrepared || s.writes != 0 {
		t.Fatal("exact original intent not observed", state, err)
	}
	stored.Version++
	state, err = p.ObserveExecutionModelDiscovery(context.Background(), r, capture)
	if err != nil || state != mc.ModelCapturePending {
		t.Fatal("later plan retired original Unknown", state, err)
	}
	s.readErr = errors.New("private SQL canary")
	state, err = p.ObserveExecutionModelDiscovery(context.Background(), r, capture)
	if err == nil || state != mc.ModelCapturePending || strings.Contains(err.Error(), "canary") || s.writes != 0 {
		t.Fatal("read error published observation", state, err)
	}
}
