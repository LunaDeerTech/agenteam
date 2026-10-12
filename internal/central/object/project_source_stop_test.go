package object

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The original handle and live Store/lock boundaries are controlled here.
// Actual source acquisition, Project loan issuance and PG rows belong to PG.
type sourceStopStore struct {
	*joinControlStore
	fixture *sourceStopFixture
	locks   map[f.Tx][]f.LockRequest
}

func (s *sourceStopStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.joinControlStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *sourceStopStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.mu.Lock()
	s.locks[tx] = append([]f.LockRequest(nil), locks...)
	s.mu.Unlock()
	return ctx.Err()
}
func (s *sourceStopStore) RequireHeldLocks(ctx context.Context, tx f.Tx, want []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, w := range want {
		found := false
		for _, got := range s.locks[tx] {
			if got.Key.Canonical() == w.Key.Canonical() && (got.Mode == f.Exclusive || got.Mode == w.Mode) {
				found = true
			}
		}
		if !found {
			return failure(f.Forbidden, nil)
		}
	}
	return ctx.Err()
}
func (s *sourceStopStore) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	v := s.fixture
	return auditTestRow(func(out ...any) error {
		if strings.Contains(q, "FROM agenteam_object.uploads WHERE object_id=$1") {
			return pgx.ErrNoRows
		}
		if strings.Contains(q, "FROM agenteam_object.object_leases WHERE id=$1") {
			if len(args) != 1 || args[0] != v.h.id.String() {
				return errors.New("wrong original source lease")
			}
			v.leaseReads++
			return auditValues(v.h.source.Details().Meta.ID.String(), v.h.id.String(), v.h.process.String(), v.leaseState).Scan(out...)
		}
		if strings.Contains(q, "FROM agenteam_object.project_work WHERE id=$1") {
			if len(args) != 1 || args[0] != v.h.work.work.id {
				return errors.New("wrong original source work")
			}
			w := v.h.work.work
			object := w.object.String()
			v.workReads++
			return auditValues(w.id, w.project.String(), w.process.String(), w.kind, w.resource, &object, w.epoch, w.epochOperation, w.revoked, w.fence, v.joined).Scan(out...)
		}
		return errors.New("unexpected source stop query")
	})
}
func (s *sourceStopStore) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	v := s.fixture
	if strings.HasPrefix(q, "UPDATE agenteam_object.object_leases ") {
		if len(args) != 3 || args[0] != v.h.id.String() || args[1] != v.h.source.Details().Meta.ID.String() || args[2] != v.h.process.String() {
			return pgconn.CommandTag{}, errors.New("wrong release tuple")
		}
		v.releases++
		v.leaseState = "released"
	} else if strings.HasPrefix(q, "UPDATE agenteam_object.project_work ") {
		if len(args) != 1 || args[0] != v.h.work.work.id {
			return pgconn.CommandTag{}, errors.New("wrong join tuple")
		}
		v.joins++
		v.joined = true
	} else {
		return pgconn.CommandTag{}, errors.New("unexpected source mutation")
	}
	return pgconn.NewCommandTag("UPDATE 1"), ctx.Err()
}

type sourceStopLoanKey struct{}
type sourceStopFixture struct {
	store                                          *sourceStopStore
	service                                        *Service
	reads                                          *SourceReads
	h                                              *sourceLeaseState
	request                                        oc.ProjectSourceStopRequest
	leaseState                                     string
	joined                                         bool
	releases, joins, leaseReads, workReads, checks int
	deny                                           bool
}

func (v *sourceStopFixture) Discover(ctx context.Context, req oc.AccessRequest) (oc.AccessDependencies, error) {
	if req.Details().Operation != oc.ReleaseReaderAccess {
		return oc.AccessDependencies{}, failure(f.Forbidden, nil)
	}
	locks, err := v.request.RequiredLocks()
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(newDigest(make([]byte, 32)), locks)
}
func (v *sourceStopFixture) ValidateInTx(ctx context.Context, tx f.Tx, req oc.AccessRequest, deps oc.AccessDependencies) error {
	if req.Details().Operation != oc.ReleaseReaderAccess || req.Details().ObjectID != v.h.source.Details().Meta.ID {
		return failure(f.Forbidden, nil)
	}
	return v.store.RequireHeldLocks(ctx, tx, deps.Locks())
}
func (v *sourceStopFixture) Resolve(context.Context, i.Actor, oc.BusinessFileRef) (oc.ResolvedSource, error) {
	return oc.ResolvedSource{}, errors.New("stop cannot resolve a new source")
}
func (v *sourceStopFixture) WithProjectSourceStop(ctx context.Context, r oc.ProjectSourceStopRequest, fn func(context.Context) error) error {
	if !r.Equal(v.request) {
		return failure(f.Forbidden, nil)
	}
	return fn(context.WithValue(ctx, sourceStopLoanKey{}, v))
}
func (v *sourceStopFixture) RequireProjectSourceStopInTx(ctx context.Context, tx f.Tx, r oc.ProjectSourceStopRequest) error {
	v.checks++
	if v.deny || ctx.Value(sourceStopLoanKey{}) != v || !r.Equal(v.request) {
		return failure(f.Forbidden, nil)
	}
	locks, err := r.RequiredLocks()
	if err != nil {
		return err
	}
	return v.store.RequireHeldLocks(ctx, tx, locks)
}

// Resolver's original validation is not used by Stop; this separate adapter
// keeps the two distinct ValidateInTx signatures explicit.
type sourceStopResolver struct{ oc.SourceResolver }

func newSourceStopFixture(t *testing.T, beforeAcquire ...func(*sourceStopFixture)) *sourceStopFixture {
	t.Helper()
	v := &sourceStopFixture{leaseState: "active"}
	v.store = &sourceStopStore{joinControlStore: &joinControlStore{active: map[f.Tx]bool{}}, fixture: v, locks: map[f.Tx][]f.LockRequest{}}
	r := &serviceState{store: v.store, process: auditID[oc.Process](t), accessIssuer: oc.NewAccessIssuer(), accessTransactions: map[f.Tx]bool{}, metadataTransactions: map[f.Tx]bool{}, projectWork: map[string]*projectWorkHandle{}, projectWriters: map[*projectWorkWriter]bool{}, projectJoins: map[string]*projectWorkHandle{}, closedLeases: map[oc.LeaseID]oc.ObjectID{}, operations: map[*operation]bool{}, cleanupRequests: map[*cleanupRequest]bool{}, changed: make(chan struct{}), auth: Authorizations{Planner: v}}
	v.service = &Service{data: func() *serviceState { return r }}
	var err error
	v.reads, err = NewSourceReadsWithProjectStop(v.service, sourceStopResolver{}, v)
	if err != nil {
		t.Fatal(err)
	}
	source := downloadVectorClaims(t).target.Details().Source
	actor, err := i.NewHuman(auditID[i.User](t), auditID[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	v.h = &sourceLeaseState{owner: v.reads.data(), id: auditID[oc.Lease](t), origin: f.NewTx(), actor: actor, source: source, process: r.process}
	p := source.Details().Reference.Details().ProjectID
	target := auditID[i.Project](t)
	v.h.work = &projectWorkHandle{source: v.h, work: projectWork{id: v.h.id.String(), resource: v.h.id.String(), kind: "source", project: p, process: r.process, object: source.Details().Meta.ID}, origin: v.h.origin, registered: true}
	r.projectWork[v.h.work.work.id] = v.h.work
	v.reads.data().leases[v.h.id] = v.h
	lease, err := oc.NewSourceLease(v.reads.data().issuer, v.h.id)
	if err != nil {
		t.Fatal(err)
	}
	command, err := f.NewCommandIdentity("artifact", []string{target.String()}, "create", "source-stop-command")
	if err != nil {
		t.Fatal(err)
	}
	cause, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: p, OperationID: auditID[oc.ProjectStopOperation](t), Action: oc.ProjectStopDelete, ProjectVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	reg, _ := i.RegisterService(i.ProjectLifecycle)
	scope, _ := i.InProject(p)
	serviceActor, err := reg.Actor(cause.Details().OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	v.request, err = oc.NewProjectSourceStopRequest(oc.ProjectSourceStopDetails{Actor: serviceActor, Cause: cause, Command: command, Lease: lease, SourceProjectID: p, TargetProjectID: target, ObjectID: source.Details().Meta.ID})
	if err != nil {
		t.Fatal(err)
	}
	locks, err := v.request.RequiredLocks()
	if err != nil {
		t.Fatal(err)
	}
	acquire, err := f.NewCommandsCause(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, hook := range beforeAcquire {
		hook(v)
	}
	result := v.reads.WithinProjectSourceAcquisition(context.Background(), acquire, func(ctx context.Context, tx f.Tx) error {
		v.h.origin = tx
		v.h.work.origin = tx
		if err := v.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		return v.reads.BindProjectSourceInTx(ctx, tx, lease, target, command)
	})
	if result.State() != f.Committed || !v.h.acquireReturned {
		t.Fatal("original acquisition did not return", result.Fault())
	}
	t.Cleanup(func() {
		v.store.after = nil
		v.deny = false
		v.h.mu.Lock()
		retained := v.h.retained
		v.h.mu.Unlock()
		if retained {
			if _, err := v.stop(context.Background()); err != nil {
				t.Error("original Unknown confirmation", err)
			}
		}
		if _, err := v.stop(context.Background()); err != nil {
			t.Error("original source stop cleanup", err)
		}
		if err := v.reads.ForgetProjectSource(lease); err != nil {
			t.Error("original source handle retirement", err)
		}
	})
	return v
}
func (v *sourceStopFixture) stop(ctx context.Context) (bool, error) {
	var complete bool
	err := v.WithProjectSourceStop(ctx, v.request, func(ctx context.Context) (err error) {
		complete, err = v.reads.StopProjectSource(ctx, v.request)
		return err
	})
	return complete, err
}

func TestProjectSourceStopAcquisitionReturn(t *testing.T) {
	ready := make(chan *sourceStopFixture, 1)
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() { open(); joinWait(t, returned) })
	go func() {
		defer close(returned)
		newSourceStopFixture(t, func(v *sourceStopFixture) {
			v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if cause.Kind() == f.CommandsCause {
					close(entered)
					<-release
				}
				return result
			}
			ready <- v
		})
	}()
	var v *sourceStopFixture
	select {
	case v = <-ready:
	case <-time.After(time.Second):
		t.Fatal("source fixture did not reach original acquisition")
	}
	joinWait(t, entered)
	if _, err := v.store.InTx(v.h.origin); err == nil {
		t.Fatal("barrier is still inside the original transaction callback")
	}
	if v.h.acquireReturned {
		t.Fatal("expired Tx substituted for original acquisition return")
	}
	complete, err := v.stop(context.Background())
	if err != nil || complete || v.releases != 0 || v.joins != 0 || v.checks != 0 {
		t.Fatal("held acquisition admitted stop", complete, err)
	}
	open()
	joinWait(t, returned)
	v.store.after = nil
	if !v.h.acquireReturned {
		t.Fatal("original acquisition return was not observed")
	}
	complete, err = v.stop(context.Background())
	if err != nil || !complete {
		t.Fatal("returned original handle did not converge", err)
	}
}

func TestProjectSourceStopBinding(t *testing.T) {
	for _, mode := range []string{"exact", "foreign-issuer", "foreign-object", "foreign-process", "unbound-command", "wrong-target", "origin-live", "without-loan", "current-gate-denied"} {
		t.Run(mode, func(t *testing.T) {
			v := newSourceStopFixture(t)
			original := v.request
			d := original.Details()
			oldProcess := v.h.process
			oldCommand := v.h.command
			oldTarget := v.h.target
			t.Cleanup(func() {
				v.request = original
				v.h.process = oldProcess
				v.h.command = oldCommand
				v.h.target = oldTarget
				delete(v.store.active, v.h.origin)
			})
			switch mode {
			case "foreign-issuer":
				d.Lease, _ = oc.NewSourceLease(oc.NewSourceIssuer(), d.Lease.ID())
			case "foreign-object":
				d.ObjectID = auditID[oc.StoredObject](t)
			case "foreign-process":
				v.h.process = auditID[oc.Process](t)
			case "unbound-command":
				v.h.command = f.CommandIdentity{}
			case "wrong-target":
				v.h.target = auditID[i.Project](t)
			case "origin-live":
				v.store.active[v.h.origin] = true
			case "current-gate-denied":
				v.deny = true
			}
			var err error
			v.request, err = oc.NewProjectSourceStopRequest(d)
			if err != nil {
				t.Fatal(err)
			}
			var complete bool
			if mode == "without-loan" {
				complete, err = v.reads.StopProjectSource(context.Background(), v.request)
			} else {
				complete, err = v.stop(context.Background())
			}
			if mode == "exact" {
				if err != nil || !complete || v.releases != 1 || v.joins != 1 || v.checks != 3 {
					t.Fatal("original release/join not consumed", complete, err, v.releases, v.joins, v.checks)
				}
				return
			}
			if complete || mode != "origin-live" && err == nil || mode == "origin-live" && err != nil || v.releases != 0 || v.joins != 0 {
				t.Fatal("invalid binding reached release/join", complete, err, v.releases, v.joins)
			}
		})
	}
}

func TestProjectSourceStopOriginalStoreReturn(t *testing.T) {
	for _, phase := range []string{"artifact-source-release", "artifact-source-join"} {
		t.Run(phase, func(t *testing.T) {
			v := newSourceStopFixture(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			open := func() { once.Do(func() { close(release) }) }
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				open()
				if !joined {
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("original Store did not return")
					}
				}
			})
			v.store.after = func(_ context.Context, c f.TransactionCause, r f.CommitResult) f.CommitResult {
				if c.Details().Owner == phase {
					close(entered)
					<-release
				}
				return r
			}
			go func() { _, err := v.stop(ctx); done <- err }()
			joinWait(t, entered)
			cancel()
			v.h.mu.Lock()
			active := v.h.stopActive
			v.h.mu.Unlock()
			if !active {
				t.Fatal("cancel retired original borrowed stop")
			}
			if err := v.reads.ForgetProjectSource(v.request.Details().Lease); !hasCode(err, f.ResourceBusy) {
				t.Fatal("held original Store lost handle", err)
			}
			select {
			case <-done:
				joined = true
				t.Fatal("cancel replaced original Store return")
			default:
			}
			open()
			select {
			case err := <-done:
				joined = true
				if !errors.Is(err, context.Canceled) {
					t.Fatal("cancelled tail became completion", err)
				}
			case <-time.After(time.Second):
				t.Fatal("released Store did not return")
			}
		})
	}
}

func TestProjectSourceStopUnknownReadonlyConfirmation(t *testing.T) {
	for _, phase := range []string{"artifact-source-release", "artifact-source-join"} {
		t.Run(phase, func(t *testing.T) {
			v := newSourceStopFixture(t)
			attempt := auditID[f.TransactionAttempt](t)
			var original f.TransactionCause
			masked := false
			v.store.after = func(_ context.Context, c f.TransactionCause, r f.CommitResult) f.CommitResult {
				if !masked && c.Details().Owner == phase && r.State() == f.Committed {
					masked = true
					original = c
					return f.UnknownResult(attempt, c)
				}
				return r
			}
			complete, err := v.stop(context.Background())
			if complete || !masked {
				t.Fatal("original Unknown not reached", complete, err)
			}
			assertCleanupUnknown(t, err, attempt)
			gotCause, wantCause := v.h.original.Cause().Details(), original.Details()
			if gotCause.Kind != wantCause.Kind || gotCause.Owner != wantCause.Owner || gotCause.RecoveryRunID != wantCause.RecoveryRunID || gotCause.CheckpointRef != wantCause.CheckpointRef {
				t.Fatal("original cause replaced")
			}
			writes := [2]int{v.releases, v.joins}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			open := func() { once.Do(func() { close(release) }) }
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				open()
				if !joined {
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("original confirmation did not return")
					}
				}
			})
			v.store.after = func(_ context.Context, c f.TransactionCause, r f.CommitResult) f.CommitResult {
				if c.Details().Owner == "artifact-source-confirm" {
					close(entered)
					<-release
				}
				return r
			}
			go func() {
				complete, err := v.stop(context.Background())
				if complete && err == nil {
					err = errors.New("confirmation advanced original release")
				}
				done <- err
			}()
			joinWait(t, entered)
			v.h.mu.Lock()
			retained, got := v.h.retained, v.h.original
			v.h.mu.Unlock()
			if !retained || got.AttemptID() != attempt || writes != [2]int{v.releases, v.joins} {
				t.Fatal("confirmation lost owner or wrote before actual return")
			}
			open()
			select {
			case err := <-done:
				joined = true
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("confirmation did not return")
			}
			if v.h.retained || writes != [2]int{v.releases, v.joins} {
				t.Fatal("readonly confirmation mutated native state")
			}
			v.store.after = nil
			complete, err = v.stop(context.Background())
			if err != nil || !complete {
				t.Fatal("next explicit original stop did not converge", err)
			}
		})
	}
}

type sourceStopClose struct{ close func() error }

func (sourceStopClose) Read([]byte) (int, error) { return 0, io.EOF }
func (s sourceStopClose) Close() error           { return s.close() }

func TestProjectSourceStopCachedReaderUnknown(t *testing.T) {
	v := newSourceStopFixture(t)
	// Use the real reader's cached Close error. The original release callback
	// runs once; confirming it must not require that callback to run again.
	payload := bytes.Repeat([]byte{'x'}, oc.StreamBufferSize+1)
	digest := sha256.Sum256(payload)
	d := v.h.source.Details()
	d.Meta.ByteSize = f.Progress(len(payload))
	d.Meta.SHA256 = newDigest(digest[:])
	var err error
	v.h.source, err = oc.NewResolvedSource(d)
	if err != nil {
		t.Fatal(err)
	}
	releaseCalls := 0
	body, err := newIntegrityReader(context.Background(), io.NopCloser(bytes.NewReader(payload)), int64(len(payload)), d.Meta.SHA256, func() error {
		releaseCalls++
		cleanup, finish := v.reads.sourceCleanupContext(v.h)
		err := v.reads.releaseSource(cleanup, v.h)
		finish()
		v.h.mu.Lock()
		v.h.result, v.h.joined, v.h.released, v.h.busy = err, true, err == nil, false
		v.h.mu.Unlock()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = body.Close() })
	v.h.reader, err = oc.NewObjectReader(d.Meta, nil, body)
	if err != nil {
		t.Fatal(err)
	}
	v.h.busy, v.h.started = true, true
	attempt := auditID[f.TransactionAttempt](t)
	masked := false
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "artifact-source-release" && !masked && result.State() == f.Committed {
			masked = true
			return f.UnknownResult(attempt, cause)
		}
		return result
	}
	complete, err := v.stop(context.Background())
	assertCleanupUnknown(t, err, attempt)
	if complete || !masked || !v.h.retained || v.h.released || releaseCalls != 1 || v.releases != 1 || v.joins != 0 {
		t.Fatal("reader did not retain its first actual release", complete, releaseCalls, v.releases, v.joins)
	}
	assertCleanupUnknown(t, body.Close(), attempt)
	v.store.after = nil
	complete, err = v.stop(context.Background())
	if err != nil || complete || v.h.retained || v.h.released || releaseCalls != 1 || v.releases != 1 || v.joins != 0 {
		t.Fatal("reader confirmation was not readonly", complete, err)
	}
	// The immutable reader result is still Unknown, but its original Close has
	// returned. Only this next explicit loan may release and join the handle.
	assertCleanupUnknown(t, body.Close(), attempt)
	complete, err = v.stop(context.Background())
	if err != nil || !complete || !v.h.released || v.h.retained || releaseCalls != 1 || v.releases != 2 || v.joins != 1 {
		t.Fatal("cached reader error blocked the next original release", complete, err, releaseCalls, v.releases, v.joins)
	}
	v.service.state().mu.Lock()
	cleanupOwners := len(v.service.state().cleanupRequests)
	v.service.state().mu.Unlock()
	if cleanupOwners != 0 {
		t.Fatal("original Close cleanup registration remained active", cleanupOwners)
	}
}

func TestProjectSourceStopLegacyUnknown(t *testing.T) {
	v := newSourceStopFixture(t)
	legacy, err := NewSourceReads(v.service, sourceStopResolver{})
	if err != nil {
		t.Fatal(err)
	}
	// Legacy construction does not opt in to typed Project ownership. The
	// shared result helper must leave its original public Unknown untouched.
	h := &sourceLeaseState{owner: legacy.data()}
	cause := recoveryCause()
	result := f.UnknownResult(auditID[f.TransactionAttempt](t), cause)
	err = sourceCommitError(h, result)
	var retained *projectCleanupFailure
	if !hasCode(err, f.CommitUnknown) || errors.As(err, &retained) || h.retained || h.originalError != nil {
		t.Fatal("legacy source acquired typed retained ownership", err)
	}
	if sourceCommitError(h, f.CommittedResult()) != nil {
		t.Fatal("legacy result was pinned to the earlier Unknown")
	}
}

func TestProjectSourceStopRolledBackAcquisition(t *testing.T) {
	v := newSourceStopFixture(t)
	h := &sourceLeaseState{owner: v.reads.data(), id: auditID[oc.Lease](t), source: v.h.source, actor: v.h.actor, process: v.h.process}
	h.work = &projectWorkHandle{source: h, work: projectWork{id: h.id.String(), resource: h.id.String(), kind: "source", project: workProject(h.source.Details().Owner), process: h.process, object: h.source.Details().Meta.ID}, registered: true}
	v.reads.data().leases[h.id] = h
	v.service.state().projectWork[h.id.String()] = h.work
	lease, err := oc.NewSourceLease(v.reads.data().issuer, h.id)
	if err != nil {
		t.Fatal(err)
	}
	command := v.request.Details().Command
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		t.Fatal(err)
	}
	locks, err := v.request.RequiredLocks()
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	done := make(chan f.CommitResult, 1)
	returned := false
	t.Cleanup(func() {
		open()
		if !returned {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("original rejected acquisition did not return")
				return
			}
		}
		v.store.after = nil
		if err := v.reads.ForgetProjectSource(lease); err != nil {
			t.Error("rejected local acquisition did not retire", err)
		}
	})
	v.store.after = func(_ context.Context, original f.TransactionCause, result f.CommitResult) f.CommitResult {
		if original.Details().Kind == f.CommandsCause {
			close(entered)
			<-release
		}
		return result
	}
	go func() {
		done <- v.reads.WithinProjectSourceAcquisition(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
			h.origin, h.work.origin = tx, tx
			if err := v.store.AcquireAll(ctx, tx, locks); err != nil {
				return err
			}
			if err := v.reads.BindProjectSourceInTx(ctx, tx, lease, v.h.target, command); err != nil {
				return err
			}
			return f.NewFault(f.Forbidden, f.NotCommitted)
		})
	}()
	joinWait(t, entered)
	if h.acquireReturned || v.reads.ForgetProjectSource(lease) == nil {
		t.Fatal("expired callback substituted for rejected acquisition return")
	}
	open()
	select {
	case result := <-done:
		returned = true
		if result.State() != f.NotCommitted || !h.acquireReturned || h.acquisition.State() != f.NotCommitted {
			t.Fatal("original rejected acquisition result was lost")
		}
	case <-time.After(time.Second):
		t.Fatal("rejected acquisition did not return")
	}
	v.store.after = nil
	calls := v.store.calls.Load()
	if err := v.reads.ForgetProjectSource(lease); err != nil {
		t.Fatal(err)
	}
	if v.reads.data().leases[h.id] != nil || v.service.state().projectWork[h.id.String()] != nil || !h.work.joinRetired || v.store.calls.Load() != calls || v.releases != 0 || v.joins != 0 || v.leaseReads != 0 || v.workReads != 0 {
		t.Fatal("known rollback cleanup issued SQL or retained local ownership")
	}
}

func TestProjectSourceStopWaitsForOriginalClose(t *testing.T) {
	v := newSourceStopFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	joined := false
	t.Cleanup(func() {
		cancel()
		open()
		if !joined {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("original reader.Close did not return")
			}
		}
	})
	reader, err := oc.NewObjectReader(v.h.source.Details().Meta, nil, sourceStopClose{close: func() error {
		// Execute the original release/join and cleanup registration, then hold
		// the enclosing reader.Close tail after that original work has returned.
		cleanup, finish := v.reads.sourceCleanupContext(v.h)
		err := v.reads.releaseSource(cleanup, v.h)
		finish()
		v.h.mu.Lock()
		v.h.released = err == nil
		v.h.busy = false
		v.h.mu.Unlock()
		close(entered)
		<-release
		return err
	}})
	if err != nil {
		t.Fatal(err)
	}
	v.h.reader = reader
	v.h.busy = true
	go func() { _, err := v.stop(ctx); done <- err }()
	joinWait(t, entered)
	v.service.state().mu.Lock()
	cleanupOwners := len(v.service.state().cleanupRequests)
	v.service.state().mu.Unlock()
	if cleanupOwners != 1 || v.releases != 1 || v.joins != 1 {
		t.Fatal("outer Close must still own its cleanup registration after release/join", cleanupOwners, v.releases, v.joins)
	}
	cancel()
	if err = v.reads.ForgetProjectSource(v.request.Details().Lease); !hasCode(err, f.ResourceBusy) {
		t.Fatal("outer Close tail lost its handle", err)
	}
	select {
	case <-done:
		joined = true
		t.Fatal("cancel substituted for original Close")
	default:
	}
	open()
	select {
	case err = <-done:
		joined = true
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("released original Close did not return")
	}
	v.service.state().mu.Lock()
	cleanupOwners = len(v.service.state().cleanupRequests)
	v.service.state().mu.Unlock()
	if cleanupOwners != 0 {
		t.Fatal("actual outer Close return did not retire its registration", cleanupOwners)
	}
	v.h.reader = nil
}
