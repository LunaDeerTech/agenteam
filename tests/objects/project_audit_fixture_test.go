//go:build integration

package objects_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type objectAuditStore interface {
	object.Store
	audit.Store
}
type objectAuditValidation struct {
	request      oc.AccessRequest
	dependencies oc.AccessDependencies
}

// This observer never grants permissions. It delegates to the existing formal
// planner and proves actual held dependency locks before recording validation.
type objectAuditPlanner struct {
	base      oc.AccessPlanner
	store     *postgres.Store
	mu        sync.Mutex
	validated map[foundation.Tx][]objectAuditValidation
	before    func(context.Context, foundation.Tx, oc.AccessRequest) error
}

func (p *objectAuditPlanner) Discover(ctx context.Context, r oc.AccessRequest) (oc.AccessDependencies, error) {
	return p.base.Discover(ctx, r)
}
func (p *objectAuditPlanner) ValidateInTx(ctx context.Context, tx foundation.Tx, r oc.AccessRequest, d oc.AccessDependencies) error {
	p.mu.Lock()
	before := p.before
	p.mu.Unlock()
	if before != nil {
		if err := before(ctx, tx, r); err != nil {
			return err
		}
	}
	if err := p.base.ValidateInTx(ctx, tx, r, d); err != nil {
		return err
	}
	if err := p.store.RequireHeldLocks(ctx, tx, d.Locks()); err != nil {
		return err
	}
	p.mu.Lock()
	p.validated[tx] = append(p.validated[tx], objectAuditValidation{r, d})
	p.mu.Unlock()
	return nil
}
func (p *objectAuditPlanner) set(fn func(context.Context, foundation.Tx, oc.AccessRequest) error) {
	p.mu.Lock()
	p.before = fn
	p.mu.Unlock()
}

type objectAuditHook func(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) error
type objectAuditTap struct {
	inner ac.Appender
	mu    sync.Mutex
	hook  objectAuditHook
	after objectAuditHook
}

func (p *objectAuditTap) set(h objectAuditHook) { p.mu.Lock(); p.hook = h; p.mu.Unlock() }
func (p *objectAuditTap) AppendInTx(ctx context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) (ac.AppendReceipt, error) {
	p.mu.Lock()
	h, a := p.hook, p.after
	p.mu.Unlock()
	if h != nil {
		if err := h(ctx, tx, e, k); err != nil {
			return ac.AppendReceipt{}, err
		}
	}
	receipt, err := p.inner.AppendInTx(ctx, tx, e, k)
	if err == nil && a != nil {
		err = a(ctx, tx, e, k)
	}
	return receipt, err
}

// A strict test-only Project port: current SQL facts plus the real planner/
// lifecycle authority, then the production Object fact checker. It does not
// implement or stand in for the future production Project ObjectProducer map.
type objectAuditProjectPort struct {
	auditAuthority
	checker *object.ProjectAuditAuthority
	planner *objectAuditPlanner
	gate    oc.ProjectGate
	runner  *runnerAuthority
	stop    *objectStopAuthority
	process oc.ProcessID
}

func (p *objectAuditProjectPort) CheckAppendInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	f := entry.Fields()
	if f.Scope.Details().Kind != identity.ProjectScope || key.Details().Producer != ac.ObjectProducer || f.Actor.Details().ServiceName != identity.ObjectService || f.Actor.Details().CauseRef != key.Details().CauseRef {
		return fault(foundation.Forbidden)
	}
	e, err := p.store.InTx(tx)
	if err != nil {
		return err
	}
	oid := f.Resource.Details().ID
	if f.Resource.Details().Kind == ac.ObjectTransferResource {
		if err = e.QueryRow(ctx, `SELECT object_id::text FROM agenteam_object.object_transfers WHERE id=$1`, oid).Scan(&oid); err != nil {
			return err
		}
	}
	pk, _ := foundation.ProjectLock(f.Scope.Details().ProjectID)
	ok, _ := foundation.AggregateLock(foundation.ObjectAggregate, oid)
	if err = p.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: pk, Mode: foundation.Shared}, {Key: ok, Mode: foundation.Exclusive}}); err != nil {
		return err
	}
	var project, state string
	if err = e.QueryRow(ctx, `SELECT o.project_id::text,p.state FROM agenteam_object.objects o JOIN object_fixture.projects p ON p.id=o.project_id WHERE o.id=$1`, oid).Scan(&project, &state); err != nil {
		return err
	}
	if project != f.Scope.Details().ProjectID {
		return fault(foundation.Forbidden)
	}
	p.planner.mu.Lock()
	records := append([]objectAuditValidation(nil), p.planner.validated[tx]...)
	p.planner.mu.Unlock()
	authorized := false
	for _, v := range records {
		d := v.request.Details()
		if err = p.store.RequireHeldLocks(ctx, tx, v.dependencies.Locks()); err != nil {
			return err
		}
		switch {
		case f.Action == ac.ObjectUploadComplete && d.Operation == oc.PublishAccess && d.Attempt.Details().ObjectID.String() == oid:
			if _, err = p.authority.AuthorizeOwnerInTx(ctx, tx, d.Actor, d.Owner, identity.Mutate); err != nil {
				return err
			}
			if err = p.gate.CheckInTx(ctx, tx, d.Actor, d.Owner, identity.Mutate); err != nil {
				return err
			}
			authorized = true
		case d.Kind == oc.MaintenanceAccess && d.InstanceID == p.process && d.ObjectID.String() == oid &&
			(f.Action == ac.ObjectUploadFailed && (d.Operation == oc.FinishWriterAccess || d.Operation == oc.RecoverAttemptAccess) || f.Action == ac.ObjectDelete && d.Operation == oc.FinalizeCleanupAccess):
			// Native maintenance is restricted to this exact instance and existing
			// object under its validated SystemConfig/Project/Object lock union.
			if state != "active" && state != "archived" && state != "deleting" {
				return fault(foundation.Forbidden)
			}
			mk, _ := foundation.SystemConfigLock("object-maintenance")
			if err = p.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: mk, Mode: foundation.Shared}}); err != nil {
				return err
			}
			authorized = true
		case f.Resource.Details().Kind == ac.ObjectTransferResource && d.Kind == oc.TransferAccess && d.Transfer.Details().ID.String() == f.Resource.Details().ID:
			tr := d.Transfer.Details()
			intent := identity.Mutate
			if tr.Operation == oc.TransferCapture || tr.Operation == oc.TransferPublish || tr.Operation == oc.TransferCancel {
				intent = identity.Converge
			}
			if p.runner == nil {
				return fault(foundation.DependencyUnbound)
			}
			rd, er := p.runner.dependencies(ctx, e, d.Transfer)
			if er != nil {
				return er
			}
			if _, err = p.runner.ValidateInTx(ctx, tx, d.Transfer, rd); err != nil {
				return err
			}
			if _, err = p.authority.AuthorizeOwnerInTx(ctx, tx, tr.Actor, tr.Spec.Details().Owner, intent); err != nil {
				return err
			}
			if err = p.gate.CheckInTx(ctx, tx, tr.Actor, tr.Spec.Details().Owner, intent); err != nil {
				return err
			}
			authorized = true
		case f.Action == ac.ObjectTransferRevoke && d.Operation == oc.GateProjectAccess && d.ProjectCleanup.Details().ProjectID.String() == project:
			if err = p.authority.CheckProjectCleanupInTx(ctx, tx, d.Actor, d.ProjectCleanup); err != nil {
				return err
			}
			authorized = true
		}
	}
	if !authorized && f.Action == ac.ObjectTransferRevoke && p.stop != nil {
		p.stop.mu.Lock()
		r, found := p.stop.validated[tx]
		p.stop.mu.Unlock()
		if found && r.Details().Cause.Details().ProjectID.String() == project {
			deps, er := p.stop.dependencies(r)
			if er != nil {
				return er
			}
			grant, er := p.stop.ValidateProjectStopInTx(ctx, tx, r, deps)
			if er != nil {
				return er
			}
			authorized = grant.Mode() == oc.ContinueProjectStop
		}
	}
	if !authorized {
		return fault(foundation.Forbidden)
	}
	return p.checker.CheckProjectAuditInTx(ctx, tx, entry, key)
}

type objectAuditOptions struct {
	store    objectAuditStore
	pg       *postgres.Store
	config   *object.StorageConfig
	stop     *objectStopAuthority
	transfer *transferFixture
}
type objectAuditFixture struct {
	*fixture
	checker  *object.ProjectAuditAuthority
	tap      *objectAuditTap
	planner  *objectAuditPlanner
	transfer *transferFixture
	dbstore  objectAuditStore
}

func newObjectAuditFixture(t *testing.T, prospective bool) *objectAuditFixture {
	return objectAuditOn(t, newFixture(t, prospective), objectAuditOptions{})
}
func objectAuditOn(t *testing.T, original *fixture, o objectAuditOptions) *objectAuditFixture {
	t.Helper()
	copyFixture := *original
	f := &copyFixture
	if o.pg != nil {
		f.store = o.pg
	}
	f.authority = &authority{f.store}
	var store objectAuditStore = f.store
	if o.store != nil {
		store = o.store
	}
	if o.config != nil {
		f.config = *o.config
	}
	backend, err := object.NewBackend(f.config)
	if err != nil {
		t.Fatal(err)
	}
	process := id[oc.Process](t)
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "audit-spool"), process)
	if err != nil {
		t.Fatal(err)
	}
	f.backend, f.spool = backend, spool
	checker, err := object.NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	var planner oc.AccessPlanner = f.authority
	var runner *runnerAuthority
	var gate oc.ProjectGate = f.authority
	var leases oc.LeaseAuthority = f.authority
	if o.transfer != nil {
		runner = &runnerAuthority{f.authority}
		gate = transferProjectGate{f.authority}
		leases = runner
	}
	observe := &objectAuditPlanner{base: planner, store: f.store, validated: map[foundation.Tx][]objectAuditValidation{}}
	var transferAuthority oc.RunnerTransferAuthority
	planner = observe
	if runner != nil {
		transferAuthority = &objectAuditRunner{runnerAuthority: runner, planner: observe}
		planner = object.NewTransferAccessPlanner(observe, transferAuthority)
	}
	port := &objectAuditProjectPort{auditAuthority: auditAuthority{f.authority}, checker: checker, planner: observe, gate: gate, runner: runner, stop: o.stop, process: process}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	realAudit, err := audit.New(store, keys, audit.Authorizations{Projects: port})
	if err != nil {
		t.Fatal(err)
	}
	tap := &objectAuditTap{inner: realAudit}
	auth := object.Authorizations{Planner: planner, Resources: f.authority, Read: f.authority, Gate: gate, Cleanup: f.authority, Leases: leases}
	if o.stop != nil {
		auth.ProjectStop = o.stop
	}
	service, err := object.New(store, backend, spool, tap, auth)
	if err != nil {
		t.Fatal(err)
	}
	f.service = service
	result := &objectAuditFixture{fixture: f, checker: checker, tap: tap, planner: observe, dbstore: store}
	if o.transfer != nil {
		endpoint, er := object.LoadTransferEndpoint(func(string) (string, bool) { return "", false }, f.config)
		if er != nil {
			t.Fatal(er)
		}
		transfers, er := object.NewTransferService(service, transferAuthority, endpoint)
		if er != nil {
			t.Fatal(er)
		}
		result.transfer = &transferFixture{f, service, transfers, runner, o.transfer.runner, o.transfer.operation, o.transfer.execution}
	}
	t.Cleanup(func() {
		service.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil {
			_ = service.Force(ctx)
			t.Error(err)
		}
	})
	if err = service.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	return result
}
func auditStorageConfig(t *testing.T, f *fixture, endpoint string) object.StorageConfig {
	t.Helper()
	values := map[string]string{"ENDPOINT": endpoint, "BUCKET": f.bucket, "ACCESS_KEY": f.remote.AccessKey, "SECRET_KEY": f.remote.SecretKey, "TLS_MODE": "disable"}
	if strings.HasPrefix(endpoint, "https:") {
		values["TLS_MODE"] = "verify-full"
		values["CA_FILE"] = f.remote.CAFile
	}
	c, err := object.LoadStorageConfig(func(n string) (string, bool) {
		v, ok := values[strings.TrimPrefix(n, object.EnvironmentPrefix)]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func objectAuditCount(t *testing.T, f *fixture, action ac.Action) int {
	t.Helper()
	var n int
	if err := f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE action=$1`, string(action)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Selection uses actual same-Tx Audit and canonical writes, never a count of
// commits. The proxy intercepts PostgreSQL's real COMMIT protocol exchange.
type objectAuditCommitStore struct {
	*postgres.Store
	proxy     *commitProxy
	mu        sync.Mutex
	action    ac.Action
	resource  string
	stop, hit bool
	pid       int32
	xid       string
}

func (s *objectAuditCommitStore) arm(a ac.Action, resource string, stop bool) {
	s.mu.Lock()
	s.action = a
	s.resource = resource
	s.stop = stop
	s.hit = false
	s.mu.Unlock()
}
func (s *objectAuditCommitStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		s.mu.Lock()
		action, resource, needStop, hit := s.action, s.resource, s.stop, s.hit
		s.mu.Unlock()
		if action == "" || hit {
			return nil
		}
		x, err := s.Store.InTx(tx)
		if err != nil {
			return err
		}
		var own bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_audit.audit_records WHERE action=$1 AND resource_id=$2 AND xmin::text=pg_current_xact_id_if_assigned()::text)`, string(action), resource).Scan(&own); err != nil {
			return err
		}
		if !own {
			return nil
		}
		if needStop {
			if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_transfers t JOIN agenteam_object.project_stops s ON s.project_id=t.project_id WHERE t.id=$1 AND t.revoked_at IS NOT NULL AND t.xmin::text=pg_current_xact_id_if_assigned()::text AND s.xmin=t.xmin AND s.state<>'stopped')`, resource).Scan(&own); err != nil {
				return err
			}
			if !own {
				return fault(foundation.InvalidState)
			}
		}
		var pid int32
		var xid string
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid(),pg_current_xact_id()::text`).Scan(&pid, &xid); err != nil {
			return err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.hit {
			s.pid, s.xid, s.hit = pid, xid, true
			s.proxy.armed.Store(1)
		}
		return nil
	})
}
func (s *objectAuditCommitStore) assertHit(t *testing.T, f *fixture, pending bool) {
	t.Helper()
	s.mu.Lock()
	hit, pid, xid, action := s.hit, s.pid, s.xid, s.action
	s.mu.Unlock()
	if !hit || pid == 0 || xid == "" {
		t.Fatal("semantic commit selector never matched")
	}
	if pending {
		var held bool
		if err := f.store.QueryRow(contextFor(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND backend_xid::text=$2 AND state='idle in transaction')`, pid, xid).Scan(&held); err != nil || !held {
			t.Fatal("original pending writer not actually alive", err)
		}
	}
	t.Logf("actual Audit COMMIT: action=%s pid=%d xid=%s pending=%t", action, pid, xid, pending)
}
func (s *objectAuditCommitStore) waitWriter(t *testing.T, f *fixture) {
	t.Helper()
	s.mu.Lock()
	pid, xid := s.pid, s.xid
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(contextFor(t), 3*time.Second)
	defer cancel()
	for {
		var alive bool
		if err := f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND backend_xid::text=$2)`, pid, xid).Scan(&alive); err != nil {
			t.Fatal(err)
		}
		if !alive {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("original writer remains live")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// The production transfer constructor requires its concrete paired planner.
// Observe the formal Runner Validate result inside that pairing, never wrap
// around or relax the production constructor's identity check.
type objectAuditRunner struct {
	*runnerAuthority
	planner *objectAuditPlanner
}

func (a *objectAuditRunner) ValidateInTx(ctx context.Context, tx foundation.Tx, r oc.TransferAccessRequest, d oc.AccessDependencies) (oc.TransferAuthorization, error) {
	grant, err := a.runnerAuthority.ValidateInTx(ctx, tx, r, d)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	if err = a.planner.store.RequireHeldLocks(ctx, tx, d.Locks()); err != nil {
		return oc.TransferAuthorization{}, err
	}
	request, err := oc.NewTransferAccess(r)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	a.planner.mu.Lock()
	a.planner.validated[tx] = append(a.planner.validated[tx], objectAuditValidation{request, d})
	a.planner.mu.Unlock()
	return grant, nil
}

// Count actual nonempty wire PUTs, excluding the required zero markers sent by
// post-publication cleanup. The existing proxy's candidatePUT counts both.
type objectAuditPayloadCounts struct{ puts, getBytes atomic.Int64 }

func auditPayloadProxy(t *testing.T, f *fixture, upstream string) (object.StorageConfig, *objectAuditPayloadCounts) {
	t.Helper()
	target, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{}
	count := &objectAuditPayloadCounts{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := strings.Contains(r.URL.Path, "/candidate/") || strings.Contains(r.URL.Path, "/staging/")
		size := r.ContentLength
		if raw := r.Header.Get("X-Amz-Decoded-Content-Length"); raw != "" {
			var err error
			size, err = strconv.ParseInt(raw, 10, 64)
			if err != nil {
				http.Error(w, "invalid fixture length", 400)
				return
			}
		}
		if r.Method == http.MethodPut && payload && size < 0 {
			t.Error("fixture cannot classify unknown-length candidate PUT")
			http.Error(w, "unclassified body", 400)
			return
		}
		if r.Method == http.MethodPut && payload && size > 0 {
			count.puts.Add(1)
		}
		req := r.Clone(r.Context())
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.RequestURI = ""
		response, err := transport.RoundTrip(req)
		if err != nil {
			http.Error(w, "fixture upstream unavailable", 503)
			return
		}
		defer response.Body.Close()
		for k, v := range response.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(response.StatusCode)
		n, copyErr := io.Copy(w, response.Body)
		if r.Method == http.MethodGet && payload {
			count.getBytes.Add(n)
			if copyErr != nil {
				t.Error("fixture could not account for actual GET bytes")
			}
		}
	}))
	t.Cleanup(func() { server.CloseClientConnections(); server.Close(); transport.CloseIdleConnections() })
	return auditStorageConfig(t, f, server.URL), count
}
func auditRetireTransfer(t *testing.T, tf *transferFixture, grant oc.TransferGrant) {
	t.Helper()
	evidence := tf.evidence(t, grant, oc.TransferStoppedEvidence, true)
	for range 3 {
		state, err := tf.transfers.ConfirmStopped(contextFor(t), tf.actor, grant.Status.ID, evidence)
		if err != nil {
			t.Fatal(err)
		}
		if !state.LeaseActive {
			return
		}
	}
	t.Fatal("actual independent transfer retirement did not release original lease")
}
