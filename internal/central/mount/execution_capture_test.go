package mount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled owner callbacks exercise the provider protocol, not real
// Execution preparation authority or a committed environment snapshot.
type mountCaptureOwner struct {
	store            *controlledStore
	request          mc.ExecutionMountCaptureRequest
	facts            mc.ExecutionMountCaptureFacts
	err              error
	cancel           context.CancelFunc
	discovery, final int
}

func (o *mountCaptureOwner) check(ctx context.Context, tx f.Tx, r mc.ExecutionMountCaptureRequest) error {
	if tx != o.store.tx || r != o.request {
		return fault(f.Forbidden)
	}
	if err := o.store.RequireHeldLocks(ctx, tx, mountCaptureLocks(r)); err != nil {
		return err
	}
	if o.cancel != nil {
		o.cancel()
	}
	return o.err
}
func (o *mountCaptureOwner) RequireMountCaptureDiscoveryInTx(ctx context.Context, tx f.Tx, r mc.ExecutionMountCaptureRequest) (mc.ExecutionMountCaptureScope, error) {
	o.discovery++
	return o.facts.Scope, o.check(ctx, tx, r)
}
func (o *mountCaptureOwner) RequireMountCaptureInTx(ctx context.Context, tx f.Tx, r mc.ExecutionMountCaptureRequest) (mc.ExecutionMountCaptureFacts, error) {
	o.final++
	return o.facts, o.check(ctx, tx, r)
}
func mountCaptureFixture(t *testing.T) (*ExecutionCapture, *controlledStore, *mountCaptureOwner, mc.ExecutionMountCaptureRequest) {
	t.Helper()
	_, s, projects, _, change := fixture(t)
	s.head = referenceState{exists: true, version: 1, ids: []i.MountID{}}
	r := mc.ExecutionMountCaptureRequest{ProjectID: change.ProjectID, AgentID: change.AgentID, ExecutionID: testID[i.Execution](t, 50)}
	o := &mountCaptureOwner{store: s, request: r, facts: mc.ExecutionMountCaptureFacts{Scope: mc.ExecutionMountCaptureScope{Project: projects.access.Project(), AttemptBinding: f.Digest("sha256:" + strings.Repeat("a", 64))}, AgentVersion: 1, AllowedMountIDs: []i.MountID{}}}
	p, err := NewExecutionCapture(s, o)
	if err != nil {
		t.Fatal(err)
	}
	return p, s, o, r
}
func TestExecutionMountCaptureRequiresRealEmptyHead(t *testing.T) {
	p, s, o, r := mountCaptureFixture(t)
	plan, err := p.DiscoverExecutionMounts(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	locks := plan.RequiredLocks()
	locks[0].Mode = f.Exclusive
	if s.RequireHeldLocks(context.Background(), s.tx, plan.RequiredLocks()) != nil {
		t.Fatal("plan aliases caller locks")
	}
	out, err := p.CaptureExecutionMountsInTx(context.Background(), s.tx, r, plan)
	if err != nil || out.Validate() != nil || out.Request != r || out.AgentVersion != 1 || out.Mounts == nil {
		t.Fatal("real empty head rejected", err)
	}
	if o.discovery != 1 || o.final != 1 || s.acquires != 1 || s.reads != 4 || s.writes != 0 {
		t.Fatal("capture skipped source or opened a new writer")
	}
	copy := out.Clone()
	copy.Mounts = nil
	if out.Mounts == nil || copy.Validate() == nil {
		t.Fatal("nil became valid empty metadata")
	}
	p, s, _, r = mountCaptureFixture(t)
	s.head.exists = false
	plan, err = p.DiscoverExecutionMounts(context.Background(), r)
	requireCode(t, err, f.DependencyUnbound)
	if plan != nil || s.writes != 0 {
		t.Fatal("missing source was fabricated")
	}
}
func TestExecutionMountCaptureRejectsStaleAndForeignPlans(t *testing.T) {
	for _, mode := range []string{"attempt", "agent-version", "head-version", "missing-head", "nonempty-head", "nil-allowlist", "nonempty-allowlist", "foreign-tx", "missing-lock", "foreign-issuer", "missing-proof"} {
		p, s, o, r := mountCaptureFixture(t)
		plan, err := p.DiscoverExecutionMounts(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		tx := s.tx
		switch mode {
		case "attempt":
			o.facts.Scope.AttemptBinding = f.Digest("sha256:" + strings.Repeat("b", 64))
		case "agent-version":
			o.facts.AgentVersion++
		case "head-version":
			s.head.version++
		case "missing-head":
			s.head.exists = false
		case "nonempty-head":
			s.head.ids = []i.MountID{testID[i.Mount](t, 60)}
		case "nil-allowlist":
			o.facts.AllowedMountIDs = nil
		case "nonempty-allowlist":
			o.facts.AllowedMountIDs = []i.MountID{testID[i.Mount](t, 60)}
		case "foreign-tx":
			tx = f.NewTx()
		case "missing-lock":
			s.missingLocks = true
		case "foreign-issuer":
			p, _ = NewExecutionCapture(s, o)
		case "missing-proof":
			o.err = fault(f.Forbidden)
		}
		out, err := p.CaptureExecutionMountsInTx(context.Background(), tx, r, plan)
		if err == nil || out.Mounts != nil || out.Request.Validate() == nil || s.writes != 0 || s.acquires != 1 {
			t.Fatal("stale authority/source published", mode, err)
		}
	}
}
func TestExecutionMountCaptureCancellationAndPhysicalUnknown(t *testing.T) {
	p, s, o, r := mountCaptureFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	o.cancel = cancel
	plan, err := p.DiscoverExecutionMounts(ctx, r)
	if !errors.Is(err, context.Canceled) || plan != nil || s.reads != 0 {
		t.Fatal("callback cancellation published discovery", err)
	}
	p, s, o, r = mountCaptureFixture(t)
	plan, err = p.DiscoverExecutionMounts(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	o.cancel = cancel
	out, err := p.CaptureExecutionMountsInTx(ctx, s.tx, r, plan)
	if !errors.Is(err, context.Canceled) || out.Mounts != nil || s.reads != 2 || s.writes != 0 {
		t.Fatal("callback cancellation published final", err)
	}
	p, s, _, r = mountCaptureFixture(t)
	attempt := testID[f.TransactionAttempt](t, 61)
	cause, err := f.NewJobCause("mount-capture-test", r.ExecutionID.String(), attempt.String())
	if err != nil {
		t.Fatal(err)
	}
	physical := f.UnknownResult(attempt, cause)
	s.physical = &physical
	plan, err = p.DiscoverExecutionMounts(context.Background(), r)
	got, ok := UnknownAttempt(err)
	if plan != nil || !ok || got.AttemptID() != attempt || got.Cause().Validate() != nil || s.writes != 0 {
		t.Fatal("physical Unknown was replaced", err)
	}
}

// These controls observe the provider protocol and explicit SQL outputs. They
// do not substitute for the real Execution witness, PG constraints or commit.
type captureMaterialStore struct {
	*runnerMountStore
	material           *captureStoredMaterial
	refs               []executionMountReference
	transactions       int
	inTransaction      bool
	beforeTransaction  func(int)
	forbidCurrentReads bool
	materialReads      int
	referenceWrites    int
	failReference      int
}
type captureStoredMaterial struct {
	project, agent, attempt, digest string
	version                         int64
	count                           int
	raw                             []byte
}

func (s *captureMaterialStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if s.inTransaction {
		panic("nested discovery transaction")
	}
	s.transactions++
	if s.beforeTransaction != nil {
		s.beforeTransaction(s.transactions)
	}
	s.inTransaction = true
	defer func() { s.inTransaction = false }()
	return s.controlledStore.WithinTx(ctx, cause, fn)
}
func (s *captureMaterialStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, fault(f.DependencyUnavailable)
	}
	return s, nil
}
func (s *captureMaterialStore) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	switch {
	case strings.HasPrefix(sql, "SELECT project_id,agent_id,agent_version,attempt_binding,capture_digest,mount_count,material"):
		s.materialReads++
		if s.material == nil {
			return controlRow{err: pgx.ErrNoRows}
		}
		m := s.material
		return controlRow{values: []any{m.project, m.agent, m.version, m.attempt, m.digest, m.count, bytes.Clone(m.raw)}}
	case strings.HasPrefix(sql, "SELECT COALESCE(jsonb_agg(jsonb_build_object('mount_id'"):
		s.materialReads++
		refs := s.refs
		if refs == nil {
			refs = []executionMountReference{}
		}
		raw, _ := json.Marshal(refs)
		return controlRow{values: []any{raw}}
	default:
		if s.forbidCurrentReads {
			panic("historical verification read current configuration")
		}
		return s.runnerMountStore.QueryRow(ctx, sql, args...)
	}
}
func (s *captureMaterialStore) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !s.applied {
		panic("capture wrote without original owner proof")
	}
	switch {
	case strings.HasPrefix(sql, "INSERT INTO agenteam_mount.execution_captures"):
		if s.material != nil {
			panic("capture reallocated existing material")
		}
		s.writes++
		s.material = &captureStoredMaterial{project: args[1].(string), agent: args[2].(string), version: args[3].(int64), attempt: args[4].(string), digest: args[5].(string), count: args[6].(int), raw: bytes.Clone(args[7].([]byte))}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.HasPrefix(sql, "INSERT INTO agenteam_mount.execution_references"):
		s.referenceWrites++
		if s.failReference == s.referenceWrites {
			return pgconn.CommandTag{}, errors.New("CONTROLLED_PRIVATE_SQL_CANARY")
		}
		m, _ := f.ParseID[i.Mount](args[1].(string))
		w, _ := f.ParseID[mc.Workspace](args[2].(string))
		r, _ := f.ParseID[rc.Runner](args[3].(string))
		s.refs = append(s.refs, executionMountReference{m, w, r, f.Version(args[4].(int64)), f.Version(args[5].(int64))})
		s.writes++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	default:
		panic("unexpected capture write")
	}
}

type captureProofKey struct{}
type captureExecutionOwner struct {
	store    *captureMaterialStore
	request  mc.ExecutionMountCaptureRequest
	facts    mc.ExecutionMountCaptureFacts
	expected mc.ExecutionMountSet
	reject   bool
	cancel   context.CancelFunc
	verified int
}

func (o *captureExecutionOwner) check(ctx context.Context, tx f.Tx, r mc.ExecutionMountCaptureRequest) error {
	if o.reject || tx != o.store.tx || r != o.request {
		return fault(f.Forbidden)
	}
	return o.store.RequireHeldLocks(ctx, tx, mountCaptureLocks(r))
}
func (o *captureExecutionOwner) RequireMountCaptureDiscoveryInTx(ctx context.Context, tx f.Tx, r mc.ExecutionMountCaptureRequest) (mc.ExecutionMountCaptureScope, error) {
	return o.facts.Scope, o.check(ctx, tx, r)
}
func (o *captureExecutionOwner) RequireMountCaptureInTx(ctx context.Context, tx f.Tx, r mc.ExecutionMountCaptureRequest) (mc.ExecutionMountCaptureFacts, error) {
	if err := o.check(ctx, tx, r); err != nil {
		return mc.ExecutionMountCaptureFacts{}, err
	}
	if ctx.Value(captureProofKey{}) != o {
		return mc.ExecutionMountCaptureFacts{}, fault(f.Forbidden)
	}
	o.store.applied = true
	if o.cancel != nil {
		o.cancel()
	}
	return o.facts, nil
}
func (o *captureExecutionOwner) RequireMountMaterialInTx(ctx context.Context, tx f.Tx, value mc.ExecutionMountSet) error {
	if err := o.check(ctx, tx, value.Request); err != nil {
		return err
	}
	if ctx.Value(captureProofKey{}) != o {
		return fault(f.Forbidden)
	}
	expected, _ := json.Marshal(o.expected)
	actual, _ := json.Marshal(value)
	if !bytes.Equal(expected, actual) {
		return fault(f.Forbidden)
	}
	o.verified++
	return nil
}

type captureRunnerProvider struct {
	store  *captureMaterialStore
	rows   []rc.ExecutionMetadata
	reject bool
	calls  int
}

func (*captureRunnerProvider) ExecutionMetadataLocks(ids []rc.RunnerID) ([]f.LockRequest, error) {
	return rc.ConfigurationCandidateLockRequests(ids)
}
func (r *captureRunnerProvider) ReadExecutionMetadataInTx(ctx context.Context, tx f.Tx, request rc.ExecutionMetadataRequest) ([]rc.ExecutionMetadata, error) {
	r.calls++
	if r.reject {
		return nil, fault(f.Forbidden)
	}
	if request.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	locks, err := r.ExecutionMetadataLocks(request.RunnerIDs)
	if err != nil {
		return nil, err
	}
	if err = r.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, err
	}
	return slices.Clone(r.rows), nil
}
func nonemptyMountCaptureFixture(t *testing.T) (*ExecutionCapture, *captureMaterialStore, *captureExecutionOwner, *captureRunnerProvider, mc.ExecutionMountCaptureRequest, context.Context) {
	t.Helper()
	_, base, old, r := mountCaptureFixture(t)
	s := &captureMaterialStore{runnerMountStore: &runnerMountStore{controlledStore: base, mappings: map[i.MountID]workspaceMapping{}}}
	runner := testID[rc.Runner](t, 5)
	for n := 0; n < 2; n++ {
		mid := testID[i.Mount](t, 60+n)
		wid := testID[mc.Workspace](t, 70+n)
		workspace := strings.Repeat("w", 255)
		if n == 1 {
			workspace = "source"
		}
		description := "owned Mount metadata"
		base.definitions[mid] = definition{ID: mid, Project: r.ProjectID, Agent: r.AgentID, Runner: runner, Name: mid.String(), Description: &description, Workspace: workspace, Lifecycle: "active", Version: 1}
		s.mappings[mid] = workspaceMapping{project: r.ProjectID, agent: r.AgentID, binding: ac.RunnerMountBinding{MountID: mid, WorkspaceID: wid, RunnerID: runner, Workspace: workspace}}
		base.head.ids = append(base.head.ids, mid)
	}
	o := &captureExecutionOwner{store: s, request: r, facts: old.facts}
	o.facts.AllowedMountIDs = slices.Clone(base.head.ids)
	runners := &captureRunnerProvider{store: s, rows: []rc.ExecutionMetadata{{ID: runner, Name: "Captured Runner", Description: "Safe metadata", Version: 9007199254740993}}}
	p, err := NewExecutionCapture(s, o, runners)
	if err != nil {
		t.Fatal(err)
	}
	return p, s, o, runners, r, context.WithValue(context.Background(), captureProofKey{}, o)
}
func TestExecutionMountNonemptyCapturesCompleteStableMaterial(t *testing.T) {
	p, s, o, runners, r, ctx := nonemptyMountCaptureFixture(t)
	plan, err := p.DiscoverExecutionMounts(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if s.transactions != 2 || s.acquires != 2 || s.writes != 0 || runners.calls != 1 {
		t.Fatal("discovery did not retire first Tx before complete union")
	}
	locks := plan.RequiredLocks()
	locks[0].Mode = f.Exclusive
	if s.RequireHeldLocks(ctx, s.tx, plan.RequiredLocks()) != nil {
		t.Fatal("plan aliases caller lock storage")
	}
	out, err := p.CaptureExecutionMountsInTx(ctx, s.tx, r, plan)
	if err != nil || out.Validate() != nil || len(out.Mounts) != 2 || out.AttemptBinding != o.facts.Scope.AttemptBinding || s.writes != 3 || len(s.refs) != 2 {
		t.Fatal("incomplete capture", err)
	}
	if out.Mounts[0].Workspace != strings.Repeat("w", 255) || out.Mounts[0].WorkspaceID != s.mappings[out.Mounts[0].ID].binding.WorkspaceID || out.Mounts[0].RunnerVersion != 9007199254740993 {
		t.Fatal("stable mapping or exact metadata lost")
	}
	if s.transactions != 2 || s.acquires != 2 || runners.calls != 2 {
		t.Fatal("capture opened Tx, acquired locks or skipped Runner recheck")
	}
	originalRaw := bytes.Clone(s.material.raw)
	originalRefs := slices.Clone(s.refs)
	again, err := p.CaptureExecutionMountsInTx(ctx, s.tx, r, plan)
	a, _ := json.Marshal(out)
	b, _ := json.Marshal(again)
	if err != nil || !bytes.Equal(a, b) || s.writes != 3 || !bytes.Equal(originalRaw, s.material.raw) || !slices.Equal(originalRefs, s.refs) {
		t.Fatal("same capture changed stable material", err)
	}
	*out.Mounts[0].Description = "caller mutation"
	if bytes.Contains(s.material.raw, []byte("caller mutation")) {
		t.Fatal("output aliases material")
	}
}
func TestExecutionMountDiscoveryAndFinalRejectChangedFacts(t *testing.T) {
	for _, mode := range []string{"legacy-mapping", "ambiguous-tuple", "no-runner-port", "discovery-change", "missing-union", "missing-witness", "foreign-tx", "runner-version", "runner-reject", "agent-version", "definition-change", "mapping-change", "reference-set", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			p, s, o, runners, r, ctx := nonemptyMountCaptureFixture(t)
			first := s.head.ids[0]
			switch mode {
			case "legacy-mapping":
				delete(s.mappings, first)
			case "ambiguous-tuple":
				d := s.definitions[first]
				d.ID = testID[i.Mount](t, 80)
				s.definitions[d.ID] = d
			case "no-runner-port":
				p, _ = NewExecutionCapture(s, o)
			case "discovery-change":
				s.beforeTransaction = func(n int) {
					if n == 2 {
						d := s.definitions[first]
						d.Name = "changed"
						s.definitions[first] = d
					}
				}
			}
			plan, err := p.DiscoverExecutionMounts(ctx, r)
			if mode == "legacy-mapping" || mode == "ambiguous-tuple" || mode == "no-runner-port" || mode == "discovery-change" {
				if err == nil || plan != nil || s.writes != 0 {
					t.Fatal("invalid discovery published a plan")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tx := s.tx
			switch mode {
			case "missing-union":
				s.locks = mountCaptureLocks(r)
			case "missing-witness":
				ctx = context.Background()
			case "foreign-tx":
				tx = f.NewTx()
			case "runner-version":
				runners.rows[0].Version++
			case "runner-reject":
				runners.reject = true
			case "agent-version":
				o.facts.AgentVersion++
			case "definition-change":
				d := s.definitions[first]
				d.Name = "changed"
				s.definitions[first] = d
			case "mapping-change":
				m := s.mappings[first]
				m.binding.WorkspaceID = testID[mc.Workspace](t, 81)
				s.mappings[first] = m
			case "reference-set":
				s.head.ids = s.head.ids[:1]
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				o.cancel = cancel
			}
			out, err := p.CaptureExecutionMountsInTx(ctx, tx, r, plan)
			if err == nil || out.Mounts != nil || s.writes != 0 {
				t.Fatal("rejected final capture published or wrote", mode)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost")
			}
		})
	}
}
func TestExecutionMountHistoricalVerificationUsesOriginalMaterial(t *testing.T) {
	p, s, o, runners, r, ctx := nonemptyMountCaptureFixture(t)
	plan, err := p.DiscoverExecutionMounts(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.CaptureExecutionMountsInTx(ctx, s.tx, r, plan)
	if err != nil {
		t.Fatal(err)
	}
	o.expected = out.Clone()
	// Current configuration can evolve. Historical verification must not use
	// current definitions, refs or Runner version to manufacture old material.
	s.head.version++
	s.head.ids = []i.MountID{}
	s.definitions = map[i.MountID]definition{}
	s.mappings = map[i.MountID]workspaceMapping{}
	runners.rows[0].Name = "later runner"
	runners.rows[0].Version++
	s.forbidCurrentReads = true
	calls, writes := runners.calls, s.writes
	if err = p.VerifyExecutionMountMaterialInTx(ctx, s.tx, out); err != nil {
		t.Fatal("original material rejected after current configuration changed", err)
	}
	if o.verified != 1 || runners.calls != calls || s.writes != writes {
		t.Fatal("history consulted current Runner or rewrote capture")
	}
	for _, mode := range []string{"missing-material", "missing-ref", "extra-ref", "changed-ref", "changed-bytes", "changed-header", "missing-proof", "foreign-tx"} {
		t.Run(mode, func(t *testing.T) {
			material := *s.material
			material.raw = bytes.Clone(s.material.raw)
			refs := slices.Clone(s.refs)
			defer func() { s.material = &material; s.refs = refs }()
			tx := s.tx
			readctx := ctx
			switch mode {
			case "missing-material":
				s.material = nil
			case "missing-ref":
				s.refs = s.refs[:1]
			case "extra-ref":
				s.refs = append(s.refs, s.refs[0])
			case "changed-ref":
				s.refs[0].WorkspaceID = testID[mc.Workspace](t, 99)
			case "changed-bytes":
				s.material.raw = append(s.material.raw, ' ')
			case "changed-header":
				s.material.attempt = "sha256:" + strings.Repeat("b", 64)
			case "missing-proof":
				readctx = context.Background()
			case "foreign-tx":
				tx = f.NewTx()
			}
			if err = p.VerifyExecutionMountMaterialInTx(readctx, tx, out); err == nil {
				t.Fatal("invalid historical material accepted")
			}
			if s.writes != writes || runners.calls != calls {
				t.Fatal("historical rejection rewrote or recaptured")
			}
		})
	}
}
func TestExecutionMountReferenceWriteFailureReturnsNoCapture(t *testing.T) {
	p, s, _, _, r, ctx := nonemptyMountCaptureFixture(t)
	plan, err := p.DiscoverExecutionMounts(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	s.failReference = 2
	out, err := p.CaptureExecutionMountsInTx(ctx, s.tx, r, plan)
	if err == nil || out.Mounts != nil || strings.Contains(err.Error(), "CONTROLLED_PRIVATE_SQL_CANARY") {
		t.Fatal("partial writer published success or leaked SQL cause")
	}
	// This controlled Store deliberately does not claim rollback. The real PG
	// integration must prove that the caller's rollback removes all these writes.
	if s.referenceWrites != 2 || s.material == nil {
		t.Fatal("failure did not reach second reference")
	}
}
