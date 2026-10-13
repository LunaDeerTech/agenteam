package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This fixture controls the original Store's callback and actual return, not a
// PostgreSQL transaction or real ProcessGuard. Public Service calls still use
// the production admission, ledger, stop and return code on one original pair.
type knowledgeProjectPureStore struct {
	Store
	mu                            sync.Mutex
	live                          map[f.Tx]*knowledgeProjectPureTx
	ledger                        map[string]projectCallRecord
	documents                     map[string]postgres.Row
	queries, writes, transactions int
	before                        func(*knowledgeProjectPureTx)
	result                        func(*knowledgeProjectPureTx, f.CommitResult) (f.CommitResult, bool)
	tail                          func(*knowledgeProjectPureTx)
	extraQuery                    func(string, ...any) postgres.Row
}
type knowledgeProjectPureTx struct {
	postgres.SQLExecutor
	store           *knowledgeProjectPureStore
	tx              f.Tx
	ctx             context.Context
	cause           f.TransactionCause
	held            []f.LockRequest
	changes         map[string]projectCallRecord
	queries, writes int
}

func (s *knowledgeProjectPureStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	x := &knowledgeProjectPureTx{store: s, tx: f.NewTx(), ctx: ctx, cause: cause, changes: map[string]projectCallRecord{}}
	s.mu.Lock()
	s.live[x.tx] = x
	s.transactions++
	s.mu.Unlock()
	if s.before != nil {
		s.before(x)
	}
	err := fn(ctx, x.tx)
	result, apply := f.CommittedResult(), err == nil
	if err != nil {
		var problem *f.Fault
		if !errors.As(err, &problem) {
			problem = fault(f.DependencyUnavailable).WithCause(err)
		}
		result = f.NotCommittedResult(problem)
	}
	if s.result != nil {
		result, apply = s.result(x, result)
	}
	s.mu.Lock()
	if apply {
		for key, row := range x.changes {
			s.ledger[key] = row
		}
	}
	delete(s.live, x.tx)
	s.mu.Unlock()
	// The callback/physical outcome can finish while this same WithinTx has
	// not returned. Cancellation never releases this explicit return barrier.
	if s.tail != nil {
		s.tail(x)
	}
	return result
}
func (s *knowledgeProjectPureStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if x := s.live[tx]; x != nil {
		return x, nil
	}
	return nil, errors.New("foreign or ended original transaction")
}
func (s *knowledgeProjectPureStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.live[tx]
	if x == nil || x.held != nil {
		return errors.New("foreign transaction or second lock acquisition")
	}
	x.held = append([]f.LockRequest{}, locks...)
	return nil
}
func (s *knowledgeProjectPureStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.live[tx]
	if x == nil {
		return errors.New("ended transaction")
	}
	for _, want := range locks {
		found := false
		for _, got := range x.held {
			if f.CompareLockKeys(want.Key, got.Key) == 0 && (got.Mode == f.Exclusive || got.Mode == want.Mode) {
				found = true
			}
		}
		if !found {
			return errors.New("missing original lock")
		}
	}
	return nil
}
func (x *knowledgeProjectPureTx) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	s := x.store
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries++
	x.queries++
	jsonRow := func(value any) postgres.Row {
		raw, err := json.Marshal(value)
		return publicationCheckpointRow(func(dest ...any) error {
			if err != nil {
				return err
			}
			*dest[0].(*[]byte) = append([]byte(nil), raw...)
			return nil
		})
	}
	switch {
	case strings.HasPrefix(sql, "INSERT INTO agenteam_knowledge.project_calls"):
		if len(args) != 8 {
			return rejectedRow{}
		}
		var source string
		if args[2] != nil {
			source = args[2].(string)
		}
		raw, _ := json.Marshal(map[string]any{"id": args[0], "project_id": args[1], "source_project_id": source, "process_id": args[3], "instance_id": args[4], "lane": args[5], "identity": json.RawMessage(args[6].(string)), "identity_digest": args[7], "phase": "running", "registered_at": "2026-10-13T12:00:00Z", "closed_at": ""})
		var row projectCallRecord
		if json.Unmarshal(raw, &row) != nil {
			return rejectedRow{}
		}
		x.changes[row.ID.String()] = row
		x.writes++
		s.writes++
		return jsonRow(row)
	case strings.Contains(sql, "FROM agenteam_knowledge.project_calls WHERE id=$1"):
		key := args[0].(string)
		row, ok := x.changes[key]
		if !ok {
			row, ok = s.ledger[key]
		}
		if !ok {
			return publicationCheckpointRow(func(...any) error { return pgx.ErrNoRows })
		}
		return jsonRow(row)
	case strings.Contains(sql, "FROM agenteam_knowledge.project_calls WHERE project_id=$1 OR source_project_id=$1"):
		rows := make([]projectCallRecord, 0)
		for _, row := range s.ledger {
			if row.Project.String() == args[0] || row.Source == args[0] {
				rows = append(rows, row)
			}
		}
		sort.Slice(rows, func(a, b int) bool { return rows[a].ID.String() < rows[b].ID.String() })
		return jsonRow(rows)
	case strings.Contains(sql, "FROM agenteam_knowledge.documents WHERE project_id=$1 AND id=$2"):
		if row := s.documents[args[0].(string)+"/"+args[1].(string)]; row != nil {
			return row
		}
		return publicationCheckpointRow(func(...any) error { return pgx.ErrNoRows })
	default:
		if s.extraQuery != nil {
			return s.extraQuery(sql, args...)
		}
		return rejectedRow{}
	}
}
func (x *knowledgeProjectPureTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	s := x.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if !strings.HasPrefix(sql, "UPDATE agenteam_knowledge.project_calls SET phase='business_closed'") || len(args) != 9 {
		return pgconn.CommandTag{}, errors.New("unexpected write")
	}
	row, ok := s.ledger[args[0].(string)]
	if !ok || row.Phase != "running" || row.Project.String() != args[1] || row.Process.String() != args[3] || row.Instance.String() != args[4] || string(row.Lane) != args[5] || string(row.Identity) != args[6] || string(row.Digest) != args[7] || row.Registered != args[8] {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	if row.Source != "" && row.Source != args[2] || row.Source == "" && args[2] != nil {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	row.Phase, row.Closed = "business_closed", "2026-10-13T12:00:01Z"
	x.changes[row.ID.String()] = row
	x.writes++
	s.writes++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}
func (s *knowledgeProjectPureStore) counts() (int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transactions, s.queries, s.writes
}

type knowledgeProjectPureAuthority struct {
	pc.ProjectAuthority
	store    *knowledgeProjectPureStore
	actor    id.Actor
	projects map[id.ProjectID]pc.ProjectAccess
}

func (a *knowledgeProjectPureAuthority) RequireOwnerInTx(_ context.Context, tx f.Tx, actor id.Actor, p id.ProjectID, _ id.AccessIntent) (pc.ProjectAccess, error) {
	if _, err := a.store.InTx(tx); err != nil {
		return pc.ProjectAccess{}, err
	}
	grant, ok := a.projects[p]
	if !ok || !actor.Equal(a.actor) {
		return pc.ProjectAccess{}, fault(f.Forbidden)
	}
	return grant, nil
}
func (a *knowledgeProjectPureAuthority) RequireProjectCallAdmissionInTx(ctx context.Context, tx f.Tx, p id.ProjectID) error {
	return a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{knowledgeProjectLock(p, f.Shared)})
}
func (a *knowledgeProjectPureAuthority) WithProjectStop(ctx context.Context, _ id.Actor, _ pc.LifecycleCause, _ pc.ScopeRef, fn func(context.Context) error) error {
	return fn(ctx)
}
func (a *knowledgeProjectPureAuthority) RequireProjectStopInTx(ctx context.Context, tx f.Tx, _ id.Actor, _ pc.LifecycleCause, scope pc.ScopeRef) error {
	return a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{knowledgeProjectLock(scope.ProjectID, f.Exclusive)})
}
func (a *knowledgeProjectPureAuthority) RequireProjectCleanupInTx(ctx context.Context, tx f.Tx, _ id.Actor, _ pc.LifecycleCause, scope pc.ScopeRef) error {
	return a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{knowledgeProjectLock(scope.ProjectID, f.Exclusive)})
}

type knowledgeProjectPureProof struct {
	store   *knowledgeProjectPureStore
	process oc.ProcessID
}

func (p knowledgeProjectPureProof) RequireCurrentProcessInTx(_ context.Context, tx f.Tx, process oc.ProcessID) error {
	if process != p.process {
		return errors.New("wrong process")
	}
	_, err := p.store.InTx(tx)
	return err
}
func (knowledgeProjectPureProof) ConfirmStopped(context.Context, oc.ProcessID) error {
	return fault(f.ResourceBusy)
}

type knowledgeProjectPureFixture struct {
	pair         *ProjectCallOwnership
	store        *knowledgeProjectPureStore
	gate         *knowledgeProjectPureAuthority
	actor        id.Actor
	a, b, source id.ProjectID
	document     kc.DocumentID
	object       oc.ObjectID
}

func newKnowledgeProjectPureFixture(t *testing.T) *knowledgeProjectPureFixture {
	t.Helper()
	_, actor, q := queryFixture(t)
	x := &knowledgeProjectPureFixture{actor: actor, a: q.project, b: newID[id.Project](t), source: newID[id.Project](t), document: newID[kc.Document](t), object: newID[oc.StoredObject](t)}
	x.store = &knowledgeProjectPureStore{live: map[f.Tx]*knowledgeProjectPureTx{}, ledger: map[string]projectCallRecord{}, documents: map[string]postgres.Row{}}
	x.gate = &knowledgeProjectPureAuthority{store: x.store, actor: actor, projects: map[id.ProjectID]pc.ProjectAccess{}}
	user, _ := f.ParseID[id.User](actor.Details().UserID)
	at := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	now, _ := f.NewInstant(at)
	for _, p := range []id.ProjectID{x.a, x.b, x.source} {
		grant, err := pc.NewProjectAccess(actor, pc.ProjectRef{ID: p, OwnerUserID: user, Name: "Original", NormalizedName: "original", Lifecycle: pc.Active, Version: 1, CreatedAt: now, UpdatedAt: now}, now)
		if err != nil {
			t.Fatal(err)
		}
		x.gate.projects[p] = grant
		title, kind, media, indexing := "original document", "text", kc.PlainText, "pending"
		object, upload, creator := x.object.String(), newID[oc.Upload](t).String(), user.String()
		x.store.documents[p.String()+"/"+x.document.String()] = sourceRow{values: []any{x.document.String(), p.String(), (*string)(nil), &title, int64(1), &kind, &media, &object, &upload, "active", &indexing, &creator, &at, &at, (*time.Time)(nil)}}
	}
	st := &serviceState{store: x.store, deps: Dependencies{Projects: x.gate}, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	service := &Service{data: func() *serviceState { return st }}
	process := newID[oc.Process](t)
	state := &projectCallState{store: x.store, service: service, proof: knowledgeProjectPureProof{x.store, process}, process: process, instance: newID[projectCallInstance](t), stop: x.gate, owners: map[f.ID[projectCallID]]*projectCallOwner{}, blocked: map[id.ProjectID]bool{}, stops: map[id.ProjectID]*projectStopCall{}, cleanups: map[id.ProjectID]*ownedProjectCleanup{}, changed: make(chan struct{})}
	// Empty Source state grants no source lease or Object retirement proof.
	sourceData := &sourceState{store: x.store, ownership: state}
	state.resolver = &SourceResolver{data: func() *sourceState { return sourceData }}
	st.ownership = state
	x.pair = &ProjectCallOwnership{state: state}
	return x
}
func (x *knowledgeProjectPureFixture) stopIdentity(t *testing.T, p id.ProjectID) (id.Actor, pc.LifecycleCause, pc.ScopeRef) {
	t.Helper()
	cause := pc.LifecycleCause{OperationID: newID[pc.Operation](t), Action: pc.Delete, ProjectVersion: 1}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: p}
	registration, err := id.RegisterService(id.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	actorScope, _ := id.InProject(p)
	actor, err := registration.Actor(cause.OperationID.String(), actorScope)
	if err != nil {
		t.Fatal(err)
	}
	return actor, cause, scope
}
func knowledgeProjectAssertFault(t *testing.T, err error, code f.Code) {
	t.Helper()
	var problem *f.Fault
	if !errors.As(err, &problem) || problem.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}
func knowledgeProjectAssertUnknown(t *testing.T, err error, original f.CommitResult) {
	t.Helper()
	actual, ok := projectCallRawError(err)
	a, b := actual.Cause().Details(), original.Cause().Details()
	if !ok || actual.State() != f.Unknown || actual.AttemptID() != original.AttemptID() || a.Kind != b.Kind || a.Owner != b.Owner || a.RecoveryRunID != b.RecoveryRunID || a.CheckpointRef != b.CheckpointRef {
		t.Fatal("first actual Unknown identity was replaced", err)
	}
}
func (x *knowledgeProjectPureFixture) convergeStop(t *testing.T, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) {
	t.Helper()
	for range 4 {
		report, err := x.pair.RequestProjectStop(context.Background(), actor, cause, scope)
		if err != nil {
			t.Fatal(err)
		}
		if report.Details().State == pc.Stopped {
			return
		}
	}
	t.Fatal("returned call did not converge in the declared bounded rounds")
}

func TestKnowledgeProjectReadWaitsForOriginalStoreReturn(t *testing.T) {
	for _, stage := range []string{"knowledge-call-register", "knowledge.read", "knowledge-call-return"} {
		t.Run(stage, func(t *testing.T) {
			x := newKnowledgeProjectPureFixture(t)
			entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			x.store.tail = func(tx *knowledgeProjectPureTx) {
				if w, ok := tx.ctx.Value(projectCallContextKey{}).(projectCallWitness); ok && tx.cause.Details().Owner == stage && w.owner.record.Project == x.a {
					once.Do(func() { close(entered); <-release })
				}
			}
			result := make(chan error, 1)
			go func() {
				defer close(joined)
				_, err := x.pair.Service().GetDocument(context.Background(), x.actor, x.a, x.document)
				result <- err
			}()
			defer func() {
				contentReaderRelease(release)
				contentReaderAwait(t, joined, "original GetDocument did not return")
			}()
			contentReaderAwait(t, entered, "original Store return boundary not reached")
			actor, cause, scope := x.stopIdentity(t, x.a)
			report, err := x.pair.RequestProjectStop(context.Background(), actor, cause, scope)
			if err != nil || report.Details().State != pc.StopPending {
				t.Fatal("Store callback completion became Stopped", err)
			}
			select {
			case <-result:
				t.Fatal("original public call returned through held Store tail")
			default:
			}
			if head, err := x.pair.Service().GetDocument(context.Background(), x.actor, x.b, x.document); err != nil || head.Active == nil || head.Active.ProjectID != x.b {
				t.Fatal("project stop drained unrelated original Service reads", err)
			}
			contentReaderRelease(release)
			contentReaderAwait(t, joined, "released original Store did not return")
			<-result
			x.convergeStop(t, actor, cause, scope)
			if x.pair.Joined() {
				t.Fatal("project Stop was promoted to global runtime join")
			}
		})
	}
}

func TestKnowledgeProjectDualScopeAndBorrowedTransactions(t *testing.T) {
	x := newKnowledgeProjectPureFixture(t)
	service := x.pair.Service()
	captured := false
	x.store.before = func(tx *knowledgeProjectPureTx) {
		if captured || tx.cause.Details().Owner != "knowledge-call-register" {
			return
		}
		x.pair.state.mu.Lock()
		defer x.pair.state.mu.Unlock()
		for _, owner := range x.pair.state.owners {
			if owner.record.ID.String() == tx.cause.Details().RecoveryRunID && owner.record.Project == x.a && owner.record.Source == x.source.String() && owner.references == 1 {
				captured = true
			}
		}
		if !captured {
			t.Error("target and source were not owned before the first Store callback")
		}
	}
	run, entry, err := service.beginProjectCall(context.Background(), x.actor, x.a, &x.source, projectCallCommand)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { var err error; service.projectCallDone(entry, &err) }()
	if !captured {
		t.Fatal("first project callback was not observed")
	}
	_, queries, _ := x.store.counts()
	_, err = service.ReadCurrentInTx(context.Background(), f.NewTx(), x.actor, x.a, x.document)
	knowledgeProjectAssertFault(t, err, f.DependencyUnbound)
	other := newKnowledgeProjectPureFixture(t)
	_, err = other.pair.Service().ReadCurrentInTx(run, f.NewTx(), other.actor, other.a, other.document)
	knowledgeProjectAssertFault(t, err, f.DependencyUnbound)
	if transactions, queries, _ := other.store.counts(); transactions != 0 || queries != 0 {
		t.Fatal("foreign pair borrowed another pair's private call")
	}
	_, after, _ := x.store.counts()
	if after != queries {
		t.Fatal("bare borrowed Tx reached business SQL")
	}
	actor, cause, scope := x.stopIdentity(t, x.source)
	report, err := x.pair.InspectProjectStop(context.Background(), actor, cause, scope)
	if err != nil || report.Details().State != pc.StopPending || run.Err() != nil {
		t.Fatal("Inspect cancelled dual-project original call", err)
	}
	report, err = x.pair.RequestProjectStop(context.Background(), actor, cause, scope)
	if err != nil || report.Details().State != pc.StopPending || !errors.Is(run.Err(), context.Canceled) {
		t.Fatal("source project did not own the original target call", err)
	}
	if _, rejected, err := service.beginProjectCall(context.Background(), x.actor, x.b, &x.source, projectCallCommand); err == nil || rejected != nil {
		t.Fatal("new source borrow passed closed source admission")
	}
	if _, err = service.GetDocument(context.Background(), x.actor, x.b, x.document); err != nil {
		t.Fatal("dual-project cancellation reached unrelated B", err)
	}
	var original error
	service.projectCallDone(entry, &original)
	x.convergeStop(t, actor, cause, scope)
}

func TestKnowledgeProjectCanonicalCloseOwnsOriginalReader(t *testing.T) {
	for _, failed := range []bool{false, true} {
		name := "success"
		if failed {
			name = "original_close_error"
		}
		t.Run(name, func(t *testing.T) {
			x := newKnowledgeProjectPureFixture(t)
			body := &contentReaderBody{text: strings.NewReader("a"), readStarted: make(chan struct{}), readRelease: make(chan struct{}), closeStarted: make(chan struct{}), closeRelease: make(chan struct{})}
			original := errors.New("original typed Close failure")
			if failed {
				body.closeErr = original
			}
			objects := &contentReadObjects{}
			objects.read = func(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, object oc.ObjectID, span *oc.ByteRange) (*oc.ObjectReader, error) {
				if !actor.Equal(x.actor) || owner.Details().ProjectID != x.a.String() || object != x.object || span != nil {
					return nil, errors.New("wrong original canonical reader")
				}
				body.ctx = ctx
				scope, _ := id.InProject(x.a)
				at, _ := f.NewInstant(time.Now())
				return oc.NewObjectReader(oc.ObjectMeta{ID: object, Scope: scope, MediaType: kc.PlainText, ByteSize: 1, SHA256: f.Digest("sha256:" + strings.Repeat("0", 64)), State: oc.Available, Version: 1, CreatedAt: at}, nil, body)
			}
			x.pair.Service().state().deps.Objects = objects
			reader, err := x.pair.Service().OpenCanonical(context.Background(), x.actor, x.a, x.document, nil)
			if err != nil {
				t.Fatal(err)
			}
			if raw, err := io.ReadAll(reader); err != nil || string(raw) != "a" {
				t.Fatal("canonical body unavailable", err)
			}
			joined := make(chan struct{})
			result := make(chan error, 1)
			go func() { defer close(joined); result <- reader.Close() }()
			defer func() {
				contentReaderRelease(body.closeRelease)
				contentReaderAwait(t, joined, "original typed Close did not return")
			}()
			contentReaderAwait(t, body.closeStarted, "original typed Close not called")
			actor, cause, scope := x.stopIdentity(t, x.a)
			report, err := x.pair.RequestProjectStop(context.Background(), actor, cause, scope)
			if err != nil || report.Details().State != pc.StopPending {
				t.Fatal("EOF/cancel replaced original typed Close return", err)
			}
			if _, err = x.pair.Service().GetDocument(context.Background(), x.actor, x.b, x.document); err != nil {
				t.Fatal("held typed Close blocked B", err)
			}
			contentReaderRelease(body.closeRelease)
			contentReaderAwait(t, joined, "Close release was not consumed")
			closeErr := <-result
			if failed && !errors.Is(closeErr, original) {
				t.Fatal("original typed Close cause lost", closeErr)
			}
			x.convergeStop(t, actor, cause, scope)
			if body.closes.Load() != 1 || objects.calls != 1 {
				t.Fatal("Stop replaced or repeated original reader Close")
			}
		})
	}
}

func TestKnowledgeProjectCallUnknownNeedsReturnedReadOnlyConfirmation(t *testing.T) {
	for _, phase := range []string{"knowledge-call-register", "knowledge-call-return"} {
		for _, applied := range []bool{false, true} {
			name := "rolled_back"
			if applied {
				name = "physically_applied"
			}
			t.Run(phase+"/"+name, func(t *testing.T) {
				x := newKnowledgeProjectPureFixture(t)
				var original f.CommitResult
				attempt := newID[f.TransactionAttempt](t)
				x.store.result = func(tx *knowledgeProjectPureTx, normal f.CommitResult) (f.CommitResult, bool) {
					if tx.cause.Details().Owner == phase {
						original = f.UnknownResult(attempt, tx.cause)
						return original, applied
					}
					return normal, normal.State() == f.Committed
				}
				_, err := x.pair.Service().GetDocument(context.Background(), x.actor, x.a, x.document)
				knowledgeProjectAssertUnknown(t, err, original)
				_, _, initialWrites := x.store.counts()
				actor, cause, scope := x.stopIdentity(t, x.a)
				second := newID[f.TransactionAttempt](t)
				x.store.result = func(tx *knowledgeProjectPureTx, _ f.CommitResult) (f.CommitResult, bool) {
					if tx.writes != 0 {
						t.Error("Unknown confirmation performed a write")
					}
					return f.UnknownResult(second, tx.cause), false
				}
				_, err = x.pair.InspectProjectStop(context.Background(), actor, cause, scope)
				knowledgeProjectAssertUnknown(t, err, original)
				entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
				x.store.result = nil
				x.store.tail = func(tx *knowledgeProjectPureTx) {
					if tx.writes != 0 {
						t.Error("read-only confirmation created a writer")
					}
					close(entered)
					<-release
				}
				type answer struct {
					report pc.StopReport
					err    error
				}
				result := make(chan answer, 1)
				go func() {
					defer close(joined)
					report, err := x.pair.InspectProjectStop(context.Background(), actor, cause, scope)
					result <- answer{report, err}
				}()
				defer func() {
					contentReaderRelease(release)
					contentReaderAwait(t, joined, "original confirmation did not return")
				}()
				contentReaderAwait(t, entered, "confirmation Store return not reached")
				beforeTx, _, _ := x.store.counts()
				_, err = x.pair.RequestProjectStop(context.Background(), actor, cause, scope)
				knowledgeProjectAssertFault(t, err, f.ResourceBusy)
				afterTx, _, afterWrites := x.store.counts()
				if afterTx != beforeTx || afterWrites != initialWrites {
					t.Fatal("held original confirmation admitted another transaction/write")
				}
				contentReaderRelease(release)
				contentReaderAwait(t, joined, "confirmation actual return missing")
				got := <-result
				if got.err != nil || got.report.Details().State != pc.StopPending {
					t.Fatal("read-only confirmation advanced Stop instead of a later explicit round", got.err)
				}
				x.store.tail = nil
				_, _, confirmedWrites := x.store.counts()
				if confirmedWrites != initialWrites {
					t.Fatal("successful confirmation mutated ledger")
				}
				x.convergeStop(t, actor, cause, scope)
				_, _, finalWrites := x.store.counts()
				want := initialWrites
				if phase == "knowledge-call-register" && applied || phase == "knowledge-call-return" && !applied {
					want++
				}
				if finalWrites != want {
					t.Fatal("follow-up did not close exactly the original remaining call")
				}
			})
		}
	}
}

func TestKnowledgeProjectRetirementWaitsForOriginalDone(t *testing.T) {
	x := newKnowledgeProjectPureFixture(t)
	service := x.pair.Service()
	_, entry, err := service.beginProjectCall(context.Background(), x.actor, x.a, nil, projectCallRead)
	if err != nil {
		t.Fatal(err)
	}
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	originalDone := entry.owner.originalDone
	entry.owner.originalDone = func() { close(entered); <-release; originalDone() }
	go func() { defer close(joined); var err error; service.projectCallDone(entry, &err) }()
	defer func() { contentReaderRelease(release); contentReaderAwait(t, joined, "original done did not return") }()
	contentReaderAwait(t, entered, "original done boundary not reached")
	actor, cause, scope := x.stopIdentity(t, x.a)
	report, err := x.pair.InspectProjectStop(context.Background(), actor, cause, scope)
	if err != nil || report.Details().State != pc.StopPending {
		t.Fatal("closed SQL row replaced original done return", err)
	}
	if _, err = service.GetDocument(context.Background(), x.actor, x.b, x.document); err != nil {
		t.Fatal("original A done blocked B", err)
	}
	contentReaderRelease(release)
	contentReaderAwait(t, joined, "released original done remained pending")
	x.convergeStop(t, actor, cause, scope)
}

func TestKnowledgeProjectRejectedInputKeepsActualSourceClose(t *testing.T) {
	for _, method := range []string{"create", "update"} {
		for _, invalid := range []string{"meta", "request"} {
			for _, closeFails := range []bool{false, true} {
				closeName := "close_success"
				if closeFails {
					closeName = "close_failure"
				}
				t.Run(method+"/"+invalid+"/"+closeName, func(t *testing.T) {
					x := newKnowledgeProjectPureFixture(t)
					body := &contentCloseBody{started: make(chan struct{}), release: make(chan struct{})}
					closeFailure := errors.New("original unadmitted source Close failed")
					if closeFails {
						body.err = closeFailure
					}
					source, err := kc.NewUploadSource(kc.PlainText, 0, f.Digest("sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"), body)
					if err != nil {
						t.Fatal(err)
					}
					version := f.Version(1)
					meta := f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "rejected-source-close"}
					if method == "update" {
						meta.ExpectedVersion = &version
					}
					if invalid == "meta" {
						meta.RequestID = f.ID[f.Request]{}
					}
					create := kc.CreateRequest{ProjectID: x.a, DocumentID: x.document, Title: "Original"}
					update := kc.UpdateRequest{ReplaceSource: true}
					if invalid == "request" {
						create.Title = ""
						empty := ""
						update.Title = &empty
					}
					result, joined := make(chan error, 1), make(chan struct{})
					go func() {
						defer close(joined)
						var err error
						if method == "create" {
							_, err = x.pair.Service().CreateDocument(context.Background(), x.actor, meta, create, source)
						} else {
							_, err = x.pair.Service().UpdateDocument(context.Background(), x.actor, meta, x.a, x.document, update, &source)
						}
						result <- err
					}()
					defer func() {
						contentReaderRelease(body.release)
						contentReaderAwait(t, joined, "rejected command's actual SourceInput.Close did not return")
					}()
					contentReaderAwait(t, body.started, "rejected public command did not close its original input")
					if transactions, queries, writes := x.store.counts(); transactions != 0 || queries != 0 || writes != 0 {
						t.Fatal("invalid command opened project SQL")
					}
					actor, cause, scope := x.stopIdentity(t, x.a)
					report, err := x.pair.RequestProjectStop(context.Background(), actor, cause, scope)
					if err != nil || report.Details().State != pc.StopPending {
						t.Fatal("rejected command's held Close was absent from project Stop", err)
					}
					otherActor, otherCause, otherScope := x.stopIdentity(t, x.b)
					report, err = x.pair.InspectProjectStop(context.Background(), otherActor, otherCause, otherScope)
					if err != nil || report.Details().State != pc.StopPending {
						t.Fatal("invalid input invented a trustworthy project scope", err)
					}
					participant, err := NewProjectCleanupParticipant(x.pair, x.gate)
					if err != nil {
						t.Fatal(err)
					}
					cleanup, err := participant.Cleanup(context.Background(), actor, cause, scope, nil)
					if err != nil || cleanup.Details().State != pc.CleanupPending {
						t.Fatal("cleanup bypassed an unscoped original Close", err)
					}
					if _, _, writes := x.store.counts(); writes != 0 {
						t.Fatal("rejected SourceInput manufactured a ledger or business write")
					}
					x.pair.Stop()
					cancelled, cancel := context.WithCancel(context.Background())
					cancel()
					if err := x.pair.Drain(cancelled); !errors.Is(err, context.Canceled) || x.pair.Joined() {
						t.Fatal("cancellation replaced actual unadmitted Close return", err)
					}
					select {
					case <-result:
						t.Fatal("public rejection returned before SourceInput.Close")
					default:
					}
					contentReaderRelease(body.release)
					contentReaderAwait(t, joined, "released rejected source call did not actually return")
					knowledgeProjectAssertFault(t, <-result, f.InvalidArgument)
					if body.closes != 1 || body.reads != 0 {
						t.Fatal("rejected input was read or closed more than once")
					}
					if closeFails {
						// Generic SourceInput.Close has no typed-reader join promise;
						// its cached error cannot be turned into a later success.
						if x.pair.Joined() {
							t.Fatal("failed generic input Close was declared joined")
						}
						if err := x.pair.Drain(cancelled); !errors.Is(err, context.Canceled) {
							t.Fatal("failed source cleanup was discarded", err)
						}
					} else if err := x.pair.Drain(context.Background()); err != nil || !x.pair.Joined() {
						t.Fatal("successful actual input Close did not retire its local call", err)
					}
				})
			}
		}
	}
}
