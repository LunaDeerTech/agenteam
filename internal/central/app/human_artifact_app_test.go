//go:build integration

package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// All business calls use the original default App's installed Human service.
// Store barriers preserve real callbacks/COMMITs. The ACK mask is local typed
// control, not a network COMMIT-fault experiment. Cancellation starts from a
// real prospective upload; this is neither Project cleanup nor an HTTP API.
func TestHumanArtifactDefaultApp(t *testing.T) {
	t.Run("inline-replay-audit-and-original-close", humanArtifactAppInline)
	t.Run("published-ack-unknown-and-readonly-replay", humanArtifactAppUnknown)
	t.Run("pending-creation-explicit-cancellation", humanArtifactAppCancellation)
}

type humanArtifactAppStore struct {
	*workAppStore
	mu     sync.Mutex
	hooks  []*usageAppHook
	reject *humanArtifactPublishRejection
}

type humanArtifactPublishRejection struct {
	project id.ProjectID
	key     f.IdempotencyKey
	err     *f.Fault
	used    bool
	result  f.CommitResult
}

func humanArtifactWrites(ctx context.Context, x postgres.SQLExecutor) (int64, error) {
	var n int64
	err := x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables`).Scan(&n)
	return n, err
}

func humanArtifactCommand(cause f.TransactionCause, p id.ProjectID, key f.IdempotencyKey) bool {
	d := cause.Details()
	owners := d.Primary.OwnerIDs()
	return d.Kind == f.CommandsCause && d.Primary.Namespace() == "artifact" && d.Primary.Command() == "create" && d.Primary.Key() == key && len(owners) == 1 && owners[0] == p.String()
}

func (s *humanArtifactAppStore) arm(h *usageAppHook) {
	h.observed, h.release = make(chan usageAppObservation, 1), make(chan struct{})
	s.mu.Lock()
	s.hooks = append(s.hooks, h)
	s.mu.Unlock()
}

func (s *humanArtifactAppStore) releaseAll() {
	s.mu.Lock()
	hooks := append([]*usageAppHook(nil), s.hooks...)
	for _, h := range hooks {
		h.used = true
	}
	s.mu.Unlock()
	for _, h := range hooks {
		h.open()
	}
}

func (s *humanArtifactAppStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	reject := s.reject
	checkReject := reject != nil && !reject.used && humanArtifactCommand(cause, reject.project, reject.key)
	var h *usageAppHook
	for _, candidate := range s.hooks {
		if !candidate.used && candidate.match(cause) {
			h = candidate
			break
		}
	}
	s.mu.Unlock()
	if checkReject {
		// Read only the prospective identity produced by the original creation.
		// The final publish Store/BEGIN/callback is never entered on this branch.
		var ready bool
		err := s.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.commands c
 JOIN agenteam_object.uploads u ON u.id=c.target_upload_id AND u.object_id=c.target_object_id
 JOIN agenteam_object.upload_attempts a ON a.id=c.target_attempt_id AND a.upload_id=u.id AND a.object_id=u.object_id
 WHERE c.project_id=$1::uuid AND c.command_key=$2 AND c.state='pending' AND c.path='content'
 AND u.owner_kind='artifact' AND u.owner_id=c.artifact_id AND u.project_id=c.project_id
 AND u.creation_cause=c.creation_cause AND u.current_attempt_id=a.id AND u.disposition='reserved'
 AND a.phase='verified' AND a.io_closed
 AND NOT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts b WHERE b.id=c.artifact_id))`, reject.project.String(), string(reject.key)).Scan(&ready)
		if err != nil {
			return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err))
		}
		if ready {
			s.mu.Lock()
			reject.used = true
			reject.result = f.NotCommittedResult(reject.err)
			s.mu.Unlock()
			return reject.result
		}
	}
	var before, after int64
	selected := false
	r := s.workAppStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if h == nil {
			return fn(ctx, tx)
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		before, err = humanArtifactWrites(ctx, x)
		if err != nil {
			return err
		}
		if err = fn(ctx, tx); err != nil {
			return err
		}
		after, err = humanArtifactWrites(ctx, x)
		if err != nil {
			return err
		}
		selected, err = h.selectRow(ctx, x, cause, before, after)
		return err
	})
	if !selected || r.State() != f.Committed {
		return r
	}
	s.mu.Lock()
	if h.used {
		s.mu.Unlock()
		return r
	}
	h.used = true
	s.mu.Unlock()
	returned := make(chan struct{})
	defer close(returned)
	masked := r
	if h.mask {
		attempt, err := f.NewID[f.TransactionAttempt]()
		if err != nil {
			return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
		}
		masked = f.UnknownResult(attempt, cause)
	}
	h.observed <- usageAppObservation{r, cause, before, after, returned, masked}
	<-h.release
	return masked
}

type humanArtifactApp struct {
	*workAppFixture
	tracked *humanArtifactAppStore
	bound   *projectUsageAssembly
	pair    *artifact.ProjectCallOwnership
	service *artifact.Service
	calls   []<-chan struct{}
}

func newHumanArtifactApp(t *testing.T) *humanArtifactApp {
	t.Helper()
	v := &humanArtifactApp{}
	v.workAppFixture = newWorkAppFixtureWith(t, func(root *modelRootApp, d *dependencies) {
		open := d.open
		d.open = func(ctx context.Context, cfg postgres.Config) (database, error) {
			original, err := open(ctx, cfg)
			store, ok := original.(*workAppStore)
			if err != nil || !ok {
				return original, err
			}
			v.tracked = &humanArtifactAppStore{workAppStore: store}
			return v.tracked, nil
		}
		d.observeProjectUsage = func(bound *projectUsageAssembly) { v.bound = bound }
	})
	v.assembly.mu.Lock()
	installed := v.assembly.artifacts
	v.assembly.mu.Unlock()
	if installed == nil || installed.pair == nil || v.bound == nil || v.bound.lifecycle == nil || v.tracked == nil {
		t.Fatal("default App omitted its original Artifact/Life/Store graph")
	}
	v.pair, v.service = installed.pair, installed.pair.Service()
	workAppCheck(t, "artifact-original-object-binding", v.service.RequireProjectStopBinding(v.objects.service))
	workAppCheck(t, "artifact-original-store-binding", v.objects.service.RequireProjectCleanupStore(v.tracked))
	t.Cleanup(func() {
		v.tracked.releaseAll()
		for _, done := range v.calls {
			usageAppWait(t, done, "original Human Artifact call cleanup")
		}
	})
	return v
}

func (v *humanArtifactApp) invocation(t *testing.T, p id.ProjectID) ac.Invocation {
	t.Helper()
	out, err := ac.NewInvocation(ac.InvocationDetails{Actor: v.actor, ProjectID: p, AttemptID: guardID[ac.CallAttempt](t)})
	workAppCheck(t, "human-artifact-invocation", err)
	return out
}

func humanArtifactMeta(t *testing.T) f.CommandMeta {
	t.Helper()
	return f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: f.IdempotencyKey(guardID[struct{}](t).String())}
}

func (v *humanArtifactApp) create(t *testing.T, p id.ProjectID, meta f.CommandMeta, text string) ac.Metadata {
	t.Helper()
	out, err := v.service.CreateFromContent(v.ctx, v.invocation(t, p), meta, ac.Display{Name: "human-note.txt", Description: "Synthetic inline note"}, "text/plain", text)
	workAppCheck(t, "human-artifact-create", err)
	d := out.Details()
	if out.Validate() != nil || d.Reference.ProjectID != p || d.Name != "human-note.txt" || d.Object.ByteSize != f.Progress(len(text)) || d.CreatedByUserID != v.actor.Details().UserID || d.CreatedByAgentID != "" || d.ExecutionID != "" || d.OperationID != "" {
		t.Fatal("formal Human artifact receipt differs")
	}
	return out
}

func (v *humanArtifactApp) list(t *testing.T, p id.ProjectID, expected ac.Metadata) {
	t.Helper()
	page, err := v.service.ListArtifacts(v.ctx, v.invocation(t, p), ac.ListFilter{}, f.PageRequest{Limit: 10})
	workAppCheck(t, "human-artifact-list", err)
	if len(page.Items()) != 1 || page.NextCursor() != "" || string(workAppJSON(t, page.Items()[0])) != string(workAppJSON(t, expected)) {
		t.Fatal("Human List lost exact Project metadata")
	}
}

func (v *humanArtifactApp) snapshot(t *testing.T, p id.ProjectID) string {
	t.Helper()
	var raw string
	workAppCheck(t, "human-artifact-snapshot", v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_artifact.artifacts x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY command_hash),'[]') FROM agenteam_artifact.commands x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_object.objects x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_object.uploads x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_object.upload_attempts x WHERE object_id IN (SELECT id FROM agenteam_object.objects WHERE project_id=$1::uuid)),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]') FROM agenteam_object.object_references x WHERE object_id IN (SELECT id FROM agenteam_object.objects WHERE project_id=$1::uuid)),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_object.object_leases x WHERE object_id IN (SELECT id FROM agenteam_object.objects WHERE project_id=$1::uuid)),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY id),'[]') FROM agenteam_audit.audit_records x WHERE project_id=$1::uuid AND producer='artifact'))::text`, p.String()).Scan(&raw))
	return raw
}

func (v *humanArtifactApp) audit(t *testing.T, p id.ProjectID, creates, lists, reads int64) {
	t.Helper()
	var gotCreate, gotList, gotRead, invalid int64
	workAppCheck(t, "human-artifact-audit", v.admin.QueryRow(v.ctx, `SELECT
 count(*) FILTER(WHERE action='artifact.create'),count(*) FILTER(WHERE action='artifact.list'),count(*) FILTER(WHERE action='artifact.read'),
 count(*) FILTER(WHERE actor_kind<>'human' OR user_id<>$2::uuid OR session_id<>$3::uuid OR outcome<>'success'
 OR execution_id IS NOT NULL OR operation_id IS NOT NULL OR tool_id IS NOT NULL OR tool_call_id IS NOT NULL
 OR metadata ?| ARRAY['name','description','content','text'])
 FROM agenteam_audit.audit_records WHERE project_id=$1::uuid AND producer='artifact'`, p.String(), v.actor.Details().UserID, v.actor.Details().SessionID).Scan(&gotCreate, &gotList, &gotRead, &invalid))
	if gotCreate != creates || gotList != lists || gotRead != reads || invalid != 0 {
		t.Fatal("real Human Artifact audit is missing, duplicated, or misattributed")
	}
}

func (v *humanArtifactApp) ledger(t *testing.T, p id.ProjectID, pending int64) {
	t.Helper()
	var running, bad, instances int64
	workAppCheck(t, "human-artifact-call-ledger", v.admin.QueryRow(v.ctx, `SELECT count(*) FILTER(WHERE joined_at IS NULL),
 count(*) FILTER(WHERE process_id<>$2::uuid OR instance_id IS NULL),count(DISTINCT instance_id)
 FROM agenteam_artifact.project_work WHERE project_id=$1::uuid`, p.String(), v.objects.process.String()).Scan(&running, &bad, &instances))
	if running != pending || bad != 0 || instances != 1 {
		t.Fatal("original Artifact call ledger/instance differs")
	}
}

func humanArtifactHeld(t *testing.T, h *usageAppHook, done <-chan struct{}) usageAppObservation {
	t.Helper()
	select {
	case out := <-h.observed:
		if out.result.State() != f.Committed {
			t.Fatal("original Artifact Store did not physically commit")
		}
		return out
	case <-done:
		t.Fatal("original Artifact call returned before its selected Store barrier")
	case <-time.After(3 * time.Second):
		t.Fatal("original Artifact Store barrier not reached")
	}
	return usageAppObservation{}
}

func humanArtifactNotReturned(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
		t.Fatal("original Artifact call returned while its Store remained held")
	default:
	}
}

func (v *humanArtifactApp) joined(t *testing.T) {
	t.Helper()
	v.ledger(t, v.a, 0)
	v.ledger(t, v.b, 0)
	v.workAppFixture.joined(t)
	if !v.pair.Joined() {
		t.Fatal("original root returned without its Artifact owner")
	}
	var active int64
	workAppCheck(t, "human-artifact-final-leases", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE process_id=$1::uuid AND state='active'`, v.objects.process.String()).Scan(&active))
	if active != 0 {
		t.Fatal("original Artifact root retained a typed Object lease")
	}
}

func humanArtifactAppInline(t *testing.T) {
	v := newHumanArtifactApp(t)
	const text = "Hello, 世界.\n"
	meta := humanArtifactMeta(t)
	a := v.create(t, v.a, meta, text)
	b := v.create(t, v.b, humanArtifactMeta(t), "Independent Project B.\n")
	before := v.snapshot(t, v.a)
	replay := v.create(t, v.a, meta, text)
	if string(workAppJSON(t, replay)) != string(workAppJSON(t, a)) || v.snapshot(t, v.a) != before {
		t.Fatal("completed inline replay changed canonical metadata, Object, or create audit")
	}
	v.list(t, v.a, a)
	v.audit(t, v.a, 1, 1, 0)
	h := &usageAppHook{match: func(c f.TransactionCause) bool {
		return c.Details().Kind == f.RecoveryCause && c.Details().Owner == "object"
	}}
	h.selectRow = func(ctx context.Context, x postgres.SQLExecutor, _ f.TransactionCause, before, after int64) (bool, error) {
		var released bool
		err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE object_id=$1::uuid AND process_id=$2::uuid AND owner_kind='reader' AND state='released' AND xmin::text=pg_current_xact_id()::text)`, a.Details().Object.ID.String(), v.objects.process.String()).Scan(&released)
		return released && after > before, err
	}
	v.tracked.arm(h)
	done := make(chan struct{})
	v.calls = append(v.calls, done)
	var read ac.ReadResult
	var readErr error
	invocation := v.invocation(t, v.a)
	go func() {
		defer close(done)
		read, readErr = v.service.ReadArtifact(v.ctx, invocation, a.Details().Reference, ac.PreviewRequest{})
	}()
	held := humanArtifactHeld(t, h, done)
	humanArtifactNotReturned(t, done)
	v.ledger(t, v.a, 1)
	v.requireGuard(t)
	v.list(t, v.b, b)
	h.open()
	usageAppWait(t, held.returned, "original typed reader Store return")
	usageAppWait(t, done, "original Human ReadArtifact return")
	workAppCheck(t, "human-artifact-read", readErr)
	d := read.Details()
	if d.Projection != ac.TextProjection || d.Text != text || d.Truncated || d.InvalidUTF8 || d.Offset != 0 || d.NextOffset != f.Progress(len(text)) || string(workAppJSON(t, d.Artifact)) != string(workAppJSON(t, a)) {
		t.Fatal("original UTF8 preview or metadata differs")
	}
	v.audit(t, v.a, 1, 1, 1)
	v.audit(t, v.b, 1, 1, 0)
	// The prior reader Close really returned. Now hold the same Service's final
	// technical Store return across ordinary SIGTERM: its joined DB row alone
	// cannot release the original App, database or process guard.
	terminal := &usageAppHook{
		match: func(c f.TransactionCause) bool {
			d := c.Details()
			return d.Kind == f.RecoveryCause && d.Owner == "artifact-call-return" && d.CheckpointRef == v.a.String()
		},
		selectRow: func(context.Context, postgres.SQLExecutor, f.TransactionCause, int64, int64) (bool, error) {
			return true, nil
		},
	}
	v.tracked.arm(terminal)
	lastDone := make(chan struct{})
	v.calls = append(v.calls, lastDone)
	var last artifact.Page
	var lastErr error
	invocation = v.invocation(t, v.a)
	go func() {
		defer close(lastDone)
		last, lastErr = v.service.ListArtifacts(v.ctx, invocation, ac.ListFilter{}, f.PageRequest{Limit: 10})
	}()
	tail := humanArtifactHeld(t, terminal, lastDone)
	v.ledger(t, v.a, 0)
	v.requestStop()
	v.root.logs.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		v.assembly.mu.Lock()
		stopped := v.assembly.stopped
		v.assembly.mu.Unlock()
		if stopped {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("root did not close original Account admission")
		case <-v.root.done:
			t.Fatal("root returned before original Account Stop")
		}
	}
	if v.pair.Joined() || v.assembly.Joined() {
		t.Fatal("joined Artifact database row replaced original Store return")
	}
	select {
	case <-v.root.done:
		t.Fatal("App returned before its original Artifact Store")
	case <-v.store.stopped:
		t.Fatal("database admission closed before its original Artifact Store")
	case <-lastDone:
		t.Fatal("Artifact public call returned before its original Store")
	default:
	}
	v.requireGuard(t)
	terminal.open()
	usageAppWait(t, tail.returned, "original Artifact technical Store return")
	usageAppWait(t, lastDone, "original Artifact call during root shutdown")
	workAppCheck(t, "original-artifact-result-after-stop", lastErr)
	if len(last.Items()) != 1 || string(workAppJSON(t, last.Items()[0])) != string(workAppJSON(t, a)) {
		t.Fatal("root shutdown changed the already-completed Artifact result")
	}
	v.audit(t, v.a, 1, 2, 1)
	v.joined(t)
	t.Log("HUMAN_ARTIFACT_APP inline=true replay=true list=true utf8_read=true real_human_audit=true original_typed_close_store_return=true project_b=true original_root_guard_db_return=true no_artifact_http=true no_agent_provenance=true")
}

func humanArtifactPublishHook(p id.ProjectID, key f.IdempotencyKey, mask, readOnly bool) *usageAppHook {
	h := &usageAppHook{mask: mask, match: func(c f.TransactionCause) bool { return humanArtifactCommand(c, p, key) }}
	h.selectRow = func(ctx context.Context, x postgres.SQLExecutor, _ f.TransactionCause, before, after int64) (bool, error) {
		var completed bool
		err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.commands c JOIN agenteam_artifact.artifacts a ON a.id=c.artifact_id AND a.object_id=c.target_object_id WHERE c.project_id=$1::uuid AND c.command_key=$2 AND c.state='completed')`, p.String(), string(key)).Scan(&completed)
		return completed && (readOnly && after == before || !readOnly && after > before), err
	}
	return h
}

func humanArtifactAppUnknown(t *testing.T) {
	v := newHumanArtifactApp(t)
	b := v.create(t, v.b, humanArtifactMeta(t), "Independent B.\n")
	meta := humanArtifactMeta(t)
	const text = "Original acknowledgement.\n"
	display := ac.Display{Name: "human-note.txt", Description: "Synthetic inline note"}
	h := humanArtifactPublishHook(v.a, meta.IdempotencyKey, true, false)
	v.tracked.arm(h)
	done := make(chan struct{})
	v.calls = append(v.calls, done)
	var first ac.Metadata
	var firstErr error
	invocation := v.invocation(t, v.a)
	go func() {
		defer close(done)
		first, firstErr = v.service.CreateFromContent(v.ctx, invocation, meta, display, "text/plain", text)
	}()
	held := humanArtifactHeld(t, h, done)
	if held.masked.State() != f.Unknown || held.masked.AttemptID().Validate() != nil || !humanArtifactCommand(held.masked.Cause(), v.a, meta.IdempotencyKey) {
		t.Fatal("controlled ACK lost its original cause/attempt")
	}
	humanArtifactNotReturned(t, done)
	v.ledger(t, v.a, 1)
	v.list(t, v.b, b)
	h.open()
	usageAppWait(t, held.returned, "original published ACK Store return")
	usageAppWait(t, done, "original published Unknown call return")
	var fault *f.Fault
	if !errors.As(firstErr, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || fault.CauseID != held.masked.AttemptID().String() || first.Validate() == nil {
		t.Fatal("original creation did not preserve its first typed Unknown")
	}
	v.ledger(t, v.a, 0)
	before := v.snapshot(t, v.a)
	confirm := humanArtifactPublishHook(v.a, meta.IdempotencyKey, false, true)
	v.tracked.arm(confirm)
	confirmed := make(chan struct{})
	v.calls = append(v.calls, confirmed)
	var replay ac.Metadata
	var replayErr error
	invocation = v.invocation(t, v.a)
	go func() {
		defer close(confirmed)
		replay, replayErr = v.service.CreateFromContent(v.ctx, invocation, meta, display, "text/plain", text)
	}()
	observed := humanArtifactHeld(t, confirm, confirmed)
	if observed.after != observed.before || v.snapshot(t, v.a) != before {
		t.Fatal("original completed replay confirmation wrote business facts")
	}
	humanArtifactNotReturned(t, confirmed)
	v.ledger(t, v.a, 1)
	confirm.open()
	usageAppWait(t, observed.returned, "original readonly replay Store return")
	usageAppWait(t, confirmed, "original replay actual return")
	workAppCheck(t, "human-artifact-replay-after-unknown", replayErr)
	if replay.Validate() != nil || replay.Details().Reference.ProjectID != v.a || replay.Details().Object.ByteSize != f.Progress(len(text)) || v.snapshot(t, v.a) != before {
		t.Fatal("original Unknown replay changed the committed result")
	}
	v.audit(t, v.a, 1, 0, 0)
	v.joined(t)
	t.Log("HUMAN_ARTIFACT_APP_UNKNOWN original_physical_commit=true local_typed_ack_mask=true original_cause_attempt=true readonly_replay_same_tx_zero_writes=true no_duplicate_audit=true original_store_and_call_return=true project_b=true no_network_fault=true")
}

func humanArtifactAppCancellation(t *testing.T) {
	v := newHumanArtifactApp(t)
	b := v.create(t, v.b, humanArtifactMeta(t), "Preserved B.\n")
	bBefore := v.snapshot(t, v.b)
	meta := humanArtifactMeta(t)
	refusal := errors.New("controlled final publication refusal")
	rejection := &humanArtifactPublishRejection{project: v.a, key: meta.IdempotencyKey, err: f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(refusal)}
	v.tracked.mu.Lock()
	v.tracked.reject = rejection
	v.tracked.mu.Unlock()
	out, err := v.service.CreateFromContent(v.ctx, v.invocation(t, v.a), meta, ac.Display{Name: "cancel-note.txt"}, "text/plain", "Prospective content.\n")
	v.tracked.mu.Lock()
	used, result := rejection.used, rejection.result
	v.tracked.mu.Unlock()
	if !used || result.State() != f.NotCommitted || !errors.Is(err, refusal) || out.Validate() == nil {
		t.Fatal("formal creation did not stop before final publication")
	}
	v.ledger(t, v.a, 0)
	var pending, verified, noCanonical, causeBound bool
	var objectID, uploadID, attemptID, artifactID string
	workAppCheck(t, "original-pending-cancellation-tuple", v.admin.QueryRow(v.ctx, `SELECT c.state='pending',a.phase='verified' AND a.io_closed,
 NOT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts b WHERE b.id=c.artifact_id),
 u.owner_kind='artifact' AND u.owner_id=c.artifact_id AND u.project_id=c.project_id AND u.creation_cause=c.creation_cause AND u.current_attempt_id=a.id AND u.disposition='reserved',
 c.target_object_id::text,c.target_upload_id::text,c.target_attempt_id::text,c.artifact_id::text
 FROM agenteam_artifact.commands c JOIN agenteam_object.uploads u ON u.id=c.target_upload_id AND u.object_id=c.target_object_id
 JOIN agenteam_object.upload_attempts a ON a.id=c.target_attempt_id AND a.upload_id=u.id AND a.object_id=u.object_id
 WHERE c.project_id=$1::uuid AND c.command_key=$2`, v.a.String(), string(meta.IdempotencyKey)).Scan(&pending, &verified, &noCanonical, &causeBound, &objectID, &uploadID, &attemptID, &artifactID))
	if !pending || !verified || !noCanonical || !causeBound {
		t.Fatal("cancellation did not start from the real original pending upload")
	}
	state, err := v.service.CancelCreation(v.ctx, v.invocation(t, v.a), meta.IdempotencyKey)
	workAppCheck(t, "human-artifact-explicit-cancel", err)
	if state != oc.CleanupCompleted {
		t.Fatal("original explicit creation cancellation did not complete")
	}
	var cancelled, retired, writerGone bool
	workAppCheck(t, "original-cancellation-return-facts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT state='cancelled' AND source_lease_id IS NULL FROM agenteam_artifact.commands WHERE project_id=$1::uuid AND command_key=$2),
 EXISTS(SELECT 1 FROM agenteam_object.uploads u JOIN agenteam_object.objects o ON o.id=u.object_id JOIN agenteam_object.upload_attempts a ON a.id=u.current_attempt_id
 WHERE u.id=$3::uuid AND u.object_id=$4::uuid AND u.owner_id=$5::uuid AND a.id=$6::uuid AND u.disposition='revoked' AND o.state='deleted' AND a.phase='cleaned'),
 NOT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE object_id=$4::uuid AND state='active')`, v.a.String(), string(meta.IdempotencyKey), uploadID, objectID, artifactID, attemptID).Scan(&cancelled, &retired, &writerGone))
	if !cancelled || !retired || !writerGone || v.snapshot(t, v.b) != bBefore {
		t.Fatal("formal cancellation lost its exact original Object/owner or changed B")
	}
	v.audit(t, v.a, 0, 0, 0)
	v.list(t, v.b, b)
	v.joined(t)
	t.Log("HUMAN_ARTIFACT_APP_CANCEL original_creation_uploaded=true final_publish_not_entered=true real_pending_tuple=true explicit_cancel_creation=true original_object_retired=true original_owner_return=true project_b=true no_sql_business_seed=true no_project_cleanup=true")
}
