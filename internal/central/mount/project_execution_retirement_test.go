package mount

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled Prepared and SQL results exercise only the adapter protocol;
// these tests do not produce real terminal receipts or prove Runner return.
type executionRetirementWitness struct{}
type executionRetirementOwner struct {
	tx      f.Tx
	request ec.ProjectExecutionRetirementRequest
	live    bool
	applied bool
	calls   int
}

func (o *executionRetirementOwner) check(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest, applied bool) error {
	o.calls++
	if !o.live || o.applied != applied || tx != o.tx || ctx.Value(executionRetirementWitness{}) != o || r.Binding != o.request.Binding || r.Cause != o.request.Cause || !r.Actor.Equal(o.request.Actor) {
		return fault(f.Forbidden)
	}
	return nil
}
func (o *executionRetirementOwner) RequireProjectExecutionRetirementPreparedInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) error {
	return o.check(ctx, tx, r, false)
}
func (o *executionRetirementOwner) RequireProjectExecutionRetirementAppliedInTx(ctx context.Context, tx f.Tx, r ec.ProjectExecutionRetirementRequest) error {
	return o.check(ctx, tx, r, true)
}
func (*executionRetirementOwner) ProjectExecutionRetirementProofInTx(context.Context, f.Tx, ec.ProjectExecutionRetirementRequest) (ec.ProjectExecutionRetirementCommit, error) {
	return ec.ProjectExecutionRetirementCommit{}, fault(f.Forbidden)
}

type executionRetirementRow func(...any) error

func (fn executionRetirementRow) Scan(out ...any) error { return fn(out...) }

type executionRetirementWrite struct {
	table string
	args  []any
}
type executionRetirementStore struct {
	Store         // Opening another transaction or acquiring locks would panic.
	t             *testing.T
	owner         *executionRetirementOwner
	live          bool
	held          map[string]f.LockMode
	snapshot      []byte
	reads, writes int
	expected      []executionRetirementWrite
	affected      int64
	cancel        context.CancelFunc
}

func (s *executionRetirementStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || tx != s.owner.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *executionRetirementStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if !s.live || tx != s.owner.tx {
		return fault(f.Forbidden)
	}
	for _, l := range locks {
		if mode, ok := s.held[l.Key.Canonical()]; !ok || mode != f.Exclusive && mode != l.Mode {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (s *executionRetirementStore) authorized(ctx context.Context) bool {
	return s.owner.live && s.owner.calls > 0 && ctx.Value(executionRetirementWitness{}) == s.owner
}
func (s *executionRetirementStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	s.reads++
	if !s.authorized(ctx) || !strings.HasPrefix(query, "SELECT jsonb_build_object") || !reflect.DeepEqual(args, []any{s.owner.request.Binding.ExecutionID.String()}) {
		s.t.Fatal("SQL escaped original Prepared execution")
	}
	return executionRetirementRow(func(out ...any) error {
		if len(out) != 1 {
			return fmt.Errorf("controlled row shape")
		}
		*out[0].(*[]byte) = bytes.Clone(s.snapshot)
		return nil
	})
}
func (s *executionRetirementStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.t.Fatal("unexpected multirow reader")
	return nil, fault(f.InternalError)
}
func (s *executionRetirementStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	n := s.writes
	s.writes++
	if !s.authorized(ctx) || n >= len(s.expected) || !strings.HasPrefix(query, "DELETE FROM agenteam_mount."+s.expected[n].table+" WHERE execution_id=$1") || !reflect.DeepEqual(args, s.expected[n].args) {
		s.t.Fatal("delete escaped historical FK order", query, args)
	}
	if n+1 == len(s.expected) && s.affected == 1 {
		s.snapshot = []byte(`{"capture":null,"references":[],"use":null,"retirement":null}`)
	}
	if s.cancel != nil {
		s.cancel()
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", s.affected)), nil
}
func executionRetirementFixture(t *testing.T) (*ProjectExecutionRetirementReferences, *executionRetirementStore, context.Context, ec.ProjectExecutionRetirementRequest) {
	t.Helper()
	p, a, e := testID[i.Project](t, 1), testID[i.Agent](t, 2), testID[i.Execution](t, 3)
	op := testID[pc.Operation](t, 4)
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(p)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(op.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	r := ec.ProjectExecutionRetirementRequest{Actor: actor, Cause: pc.LifecycleCause{OperationID: op, Action: pc.Delete, ProjectVersion: 9}, Binding: ec.ProjectExecutionRetirementBinding{ProjectID: p, AgentID: a, ExecutionID: e, Version: 7, CanonicalDigest: f.Digest("sha256:" + strings.Repeat("a", 64))}}
	if err = r.Validate(); err != nil {
		t.Fatal(err)
	}
	o := &executionRetirementOwner{tx: f.NewTx(), request: r, live: true}
	s := &executionRetirementStore{t: t, owner: o, live: true, held: map[string]f.LockMode{}, affected: 1}
	pk, _ := f.ProjectLock(p.String())
	sk, _ := f.ProjectScheduleLock(p.String())
	ak, _ := f.AgentLock(a.String())
	ek, _ := f.AggregateLock(f.ExecutionAggregate, e.String())
	for _, k := range []f.LockKey{pk, sk, ak, ek} {
		s.held[k.Canonical()] = f.Exclusive
	}
	metadata := mc.ExecutionMountMetadata{ID: testID[i.Mount](t, 10), WorkspaceID: testID[mc.Workspace](t, 11), RunnerID: testID[rc.Runner](t, 12), RunnerVersion: 5, RunnerName: "Configured runner", RunnerDescription: "Metadata only", Name: "Shared input", Workspace: "test-data", Version: 2}
	captured := mc.ExecutionMountSet{Request: mc.ExecutionMountCaptureRequest{ProjectID: p, AgentID: a, ExecutionID: e}, AgentVersion: 3, Mounts: []mc.ExecutionMountMetadata{metadata}, AttemptBinding: r.Binding.CanonicalDigest}
	// Use the original material encoder to prepare valid source input, not to
	// compute the expected deletion or owner-admission result.
	material, digest, err := executionMountMaterial(captured)
	if err != nil {
		t.Fatal(err)
	}
	head := map[string]any{"execution_id": e.String(), "project_id": p.String(), "agent_id": a.String(), "agent_version": 3, "attempt_binding": captured.AttemptBinding, "capture_digest": digest, "mount_count": 1, "material_hex": hex.EncodeToString(material)}
	refs := []any{map[string]any{"execution_id": e.String(), "mount_id": metadata.ID.String(), "workspace_id": metadata.WorkspaceID.String(), "runner_id": metadata.RunnerID.String(), "mount_version": int64(metadata.Version), "runner_version": int64(metadata.RunnerVersion)}}
	use := map[string]any{"execution_id": e.String(), "project_id": p.String(), "agent_id": a.String(), "agent_version": 3, "attempt_binding": captured.AttemptBinding, "capture_digest": digest, "input_digest": r.Binding.CanonicalDigest, "snapshot_digest": r.Binding.CanonicalDigest}
	retired := map[string]any{"execution_id": e.String(), "terminal_version": 7, "terminal_event_id": testID[struct{}](t, 13).String()}
	s.snapshot, err = json.Marshal(map[string]any{"capture": head, "references": refs, "use": use, "retirement": retired})
	if err != nil {
		t.Fatal(err)
	}
	s.expected = []executionRetirementWrite{{"execution_use_retirements", []any{e.String()}}, {"execution_uses", []any{e.String(), p.String(), a.String()}}, {"execution_references", []any{e.String(), metadata.ID.String()}}, {"execution_captures", []any{e.String(), p.String(), a.String()}}}
	provider, err := NewProjectExecutionRetirementReferences(s, o)
	if err != nil {
		t.Fatal(err)
	}
	return provider, s, context.WithValue(context.Background(), executionRetirementWitness{}, o), r
}
func mutateExecutionRetirementSnapshot(t *testing.T, s *executionRetirementStore, fn func(map[string]any)) {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(s.snapshot, &raw); err != nil {
		t.Fatal(err)
	}
	fn(raw)
	var err error
	s.snapshot, err = json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
}

func TestProjectExecutionRetirementMountRequiresOriginalPrepared(t *testing.T) {
	for _, mode := range []string{"foreign-tx", "expired-store", "missing-lock", "public-request", "expired-owner", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			p, s, ctx, r := executionRetirementFixture(t)
			tx := s.owner.tx
			switch mode {
			case "foreign-tx":
				tx = f.NewTx()
			case "expired-store":
				s.live = false
			case "missing-lock":
				k, _ := f.AgentLock(r.Binding.AgentID.String())
				delete(s.held, k.Canonical())
			case "public-request":
				ctx = context.Background()
			case "expired-owner":
				s.owner.live = false
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			plan, err := p.DiscoverProjectExecutionReferencesInTx(ctx, tx, r)
			if err == nil || plan != nil || s.reads != 0 || s.writes != 0 {
				t.Fatal("receipt/public DTO substituted for original Prepared", err)
			}
		})
	}
}

func TestProjectExecutionRetirementMountReleasesOnlyHistoricalFKs(t *testing.T) {
	p, s, ctx, r := executionRetirementFixture(t)
	plan, err := p.DiscoverProjectExecutionReferencesInTx(ctx, s.owner.tx, r)
	if err != nil {
		t.Fatal(err)
	}
	if s.writes != 0 || len(plan.RequiredLocks()) != 4 || p.ProjectExecutionRetirementReferenceKind() != ec.ProjectExecutionMountReferences {
		t.Fatal("discovery mutated or omitted locks")
	}
	if plan.ProjectExecutionRetirementReferenceDigest().Validate() != nil {
		t.Fatal("complete capture shape has no digest")
	}
	locks := plan.RequiredLocks()
	locks[0].Mode = f.Shared
	if reflect.DeepEqual(locks, plan.RequiredLocks()) {
		t.Fatal("caller mutated private locks")
	}
	before := s.reads
	if err = p.RequireProjectExecutionReferencesRetiredInTx(ctx, s.owner.tx, r, plan); err == nil || s.reads != before {
		t.Fatal("Prepared substituted for Applied")
	}
	if err = p.RetireProjectExecutionReferencesInTx(ctx, s.owner.tx, r, plan); err != nil {
		t.Fatal(err)
	}
	s.owner.applied = true // Controlled consumer reaches its separate Applied phase.
	if err = p.RequireProjectExecutionReferencesRetiredInTx(ctx, s.owner.tx, r, plan); err != nil || s.writes != 4 || s.owner.calls != 4 {
		t.Fatal("retirement FK order failed", err)
	}
	before = s.reads
	if err = p.RetireProjectExecutionReferencesInTx(ctx, s.owner.tx, r, plan); err == nil || s.reads != before || s.writes != 4 {
		t.Fatal("Applied confirmation allowed a second mutation", err)
	}
	s.owner.live = false
	before = s.reads
	if err = p.RequireProjectExecutionReferencesRetiredInTx(ctx, s.owner.tx, r, plan); err == nil || s.reads != before {
		t.Fatal("empty capture substituted for owner")
	}
}

func TestProjectExecutionRetirementMountRejectsUnreturnedOrChangedSource(t *testing.T) {
	for _, mode := range []string{"live-use", "stale-terminal-version", "changed-reference", "changed-full-receipt", "foreign-plan"} {
		t.Run(mode, func(t *testing.T) {
			p, s, ctx, r := executionRetirementFixture(t)
			plan, err := p.DiscoverProjectExecutionReferencesInTx(ctx, s.owner.tx, r)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "live-use":
				mutateExecutionRetirementSnapshot(t, s, func(raw map[string]any) { raw["retirement"] = nil })
			case "stale-terminal-version":
				mutateExecutionRetirementSnapshot(t, s, func(raw map[string]any) { raw["retirement"].(map[string]any)["terminal_version"] = 8 })
			case "changed-reference":
				mutateExecutionRetirementSnapshot(t, s, func(raw map[string]any) { raw["references"].([]any)[0].(map[string]any)["runner_version"] = 6 })
			case "changed-full-receipt":
				mutateExecutionRetirementSnapshot(t, s, func(raw map[string]any) {
					raw["retirement"].(map[string]any)["terminal_event_id"] = testID[struct{}](t, 14).String()
				})
				current, err := p.DiscoverProjectExecutionReferencesInTx(ctx, s.owner.tx, r)
				if err != nil || current.ProjectExecutionRetirementReferenceDigest() == plan.ProjectExecutionRetirementReferenceDigest() {
					t.Fatal("shape digest omitted full original receipt", err)
				}
			case "foreign-plan":
				p, err = NewProjectExecutionRetirementReferences(s, s.owner)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = p.RetireProjectExecutionReferencesInTx(ctx, s.owner.tx, r, plan); err == nil || s.writes != 0 {
				t.Fatal("unknown original borrower or changed source deleted", err)
			}
		})
	}
}

func TestProjectExecutionRetirementMountPreparationOnlyAndPartialDelete(t *testing.T) {
	for _, mode := range []string{"capture-only", "zero-rows", "cancelled-after-first"} {
		t.Run(mode, func(t *testing.T) {
			p, s, ctx, r := executionRetirementFixture(t)
			if mode == "capture-only" {
				mutateExecutionRetirementSnapshot(t, s, func(raw map[string]any) { raw["use"] = nil; raw["retirement"] = nil })
				s.expected = s.expected[2:]
			}
			plan, err := p.DiscoverProjectExecutionReferencesInTx(ctx, s.owner.tx, r)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "zero-rows":
				s.affected = 0
			case "cancelled-after-first":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				defer cancel()
				s.cancel = cancel
			}
			err = p.RetireProjectExecutionReferencesInTx(ctx, s.owner.tx, r, plan)
			if mode == "capture-only" {
				if err != nil || s.writes != 2 {
					t.Fatal("preparation-only capture did not retire", err)
				}
				s.owner.applied = true
				if err = p.RequireProjectExecutionReferencesRetiredInTx(ctx, s.owner.tx, r, plan); err != nil {
					t.Fatal(err)
				}
			} else if err == nil || s.writes != 1 {
				t.Fatal("incomplete deletion continued", err)
			}
		})
	}
}
