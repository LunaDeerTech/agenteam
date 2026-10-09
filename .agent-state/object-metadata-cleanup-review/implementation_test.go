package object

// Independent controlled-Store probes. Production Service/plan methods execute;
// the Store supplies explicit rows and affected counts, not PostgreSQL semantics.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type independentRows struct {
	pgx.Rows
	ids    []string
	at     int
	closed bool
}

func (r *independentRows) Next() bool { r.at++; return r.at < len(r.ids) }
func (r *independentRows) Scan(v ...any) error {
	if len(v) != 1 {
		return errors.New("scan arity")
	}
	*v[0].(*string) = r.ids[r.at]
	return nil
}
func (r *independentRows) Err() error { return nil }
func (r *independentRows) Close()     { r.closed = true }
func independentID(n int) string      { return fmt.Sprintf("01970000-0000-7000-8000-%012d", n) }

type independentMetadataStore struct {
	queries   []string
	queryArgs [][]any
	*auditTestStore
	t              *testing.T
	stage          string
	ids            []string
	rows           []*independentRows
	writes         []string
	args           [][]any
	ended, pending bool
	rowRevision    int
	affected       int64
	failAt         int
	acquisitions   int
}

func (s *independentMetadataStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !tx.Valid() || s.ended {
		return nil, errors.New("not live own Tx")
	}
	return s, nil
}
func (s *independentMetadataStore) AcquireAll(_ context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	if tx != s.tx || len(locks) == 0 {
		panic("unbound acquire")
	}
	s.acquisitions++
	return nil
}
func (s *independentMetadataStore) Query(_ context.Context, q string, args ...any) (*postgres.Rows, error) {
	if len(args) == 0 {
		panic("query omitted original identity")
	}
	s.queries = append(s.queries, q)
	s.queryArgs = append(s.queryArgs, append([]any{}, args...))
	var ids []string
	if strings.Contains(q, "FROM agenteam_object."+s.stage+" ") {
		ids = append(ids, s.ids...)
	}
	r := &independentRows{ids: ids, at: -1}
	s.rows = append(s.rows, r)
	return postgres.MetadataIndependentRows(r), nil
}
func (s *independentMetadataStore) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	s.writes = append(s.writes, q)
	s.args = append(s.args, append([]any{}, args...))
	if s.failAt == len(s.writes) {
		return pgconn.CommandTag{}, errors.New("controlled write failure")
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", s.affected)), nil
}
func (s *independentMetadataStore) assertClosed(t *testing.T) {
	t.Helper()
	for _, r := range s.rows {
		if !r.closed {
			t.Fatal("rows not closed")
		}
	}
}

type independentPlanner struct {
	deps oc.AccessDependencies
	tx   foundation.Tx
	deny bool
}

func (p *independentPlanner) Discover(context.Context, oc.AccessRequest) (oc.AccessDependencies, error) {
	return p.deps, nil
}
func (p *independentPlanner) ValidateInTx(_ context.Context, tx foundation.Tx, _ oc.AccessRequest, d oc.AccessDependencies) error {
	if tx != p.tx || !d.Equal(p.deps) {
		panic("planner identity changed")
	}
	if p.deny {
		return foundation.NewFault(foundation.Forbidden, foundation.NotCommitted)
	}
	return nil
}

type independentAuthority struct {
	oc.CleanupAuthority
	tx     foundation.Tx
	cause  oc.ObjectCleanupCause
	object oc.ObjectID
	deny   bool
	calls  int
}

func (a *independentAuthority) CheckCleanupInTx(_ context.Context, tx foundation.Tx, c oc.ObjectCleanupCause, o oc.ObjectID) error {
	a.calls++
	if tx != a.tx || (c.Details().OperationID != a.cause.Details().OperationID || c.Details().Reason != a.cause.Details().Reason || !c.Details().Owner.Equal(a.cause.Details().Owner)) || o != a.object {
		panic("current authority not same exact Tx/cause/object")
	}
	if a.deny {
		return foundation.NewFault(foundation.Forbidden, foundation.NotCommitted)
	}
	return nil
}

type independentMetadataFixture struct {
	s         *Service
	store     *independentMetadataStore
	source    *auditPublishFixture
	cause     oc.ObjectCleanupCause
	request   oc.AccessRequest
	plan      oc.AccessLockPlan
	locked    oc.LockedAccess
	authority *independentAuthority
	planner   *independentPlanner
	cleanup   string
}

func independentMetadata(t *testing.T, stage string, n int) *independentMetadataFixture {
	t.Helper()
	v := newAuditPublishFixture(t)
	owner, err := oc.NewObjectOwner(oc.SkillRevision, v.u.owner.Details().ID, v.u.owner.Details().ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	v.u.owner, v.u.state, v.u.disposition, v.u.command = owner, "committed", "revoked", "original-command"
	v.o.meta.State, v.o.cleaning = oc.Deleted, true
	v.a.phase, v.a.cleaning, v.a.closed = "cleaned", true, true
	cause, err := oc.NewObjectCleanupCause(oc.CleanupDetails{Owner: owner, Reason: oc.ProjectDeleted, OperationID: auditID[oc.CleanupOperation](t)})
	if err != nil {
		t.Fatal(err)
	}
	store := &independentMetadataStore{auditTestStore: v.store, t: t, stage: stage, affected: 1}
	for i := 0; i < n; i++ {
		store.ids = append(store.ids, independentID(100+i))
	}
	cleanup := independentID(90)
	oldRow := v.store.row
	v.store.row = func(q string, args ...any) postgres.Row {
		switch {
		case strings.HasPrefix(q, "SELECT to_jsonb(r)"):
			raw, _ := json.Marshal(map[string]any{"id": args[0], "revision": store.rowRevision})
			return auditValues(raw)
		case strings.HasPrefix(q, "SELECT c.id::text"):
			if strings.Contains(q, "c.operation_id=$4") {
				if len(args) != 4 || args[0] != v.u.object.String() || args[1] != v.u.id.String() || args[2] != v.u.attempt.String() || args[3] != cause.Details().OperationID.String() {
					t.Fatal("canonical anchors changed")
				}
				return auditValues(cleanup)
			}
			if len(args) != 3 || args[1] != v.u.object.String() || args[2] != v.u.id.String() {
				t.Fatal("history left original upload/object")
			}
			return auditValues(independentID(1000 + indexOf(store.ids, args[0].(string))))
		case strings.HasPrefix(q, "SELECT\n EXISTS"):
			return auditValues(store.pending)
		default:
			return oldRow(q, args...)
		}
	}
	state := &serviceState{store: store, accessIssuer: oc.NewAccessIssuer(), accessTransactions: map[foundation.Tx]bool{}, metadataTransactions: map[foundation.Tx]bool{}, projectWork: map[string]*projectWorkHandle{}}
	s := &Service{data: func() *serviceState { return state }}
	pk, _ := foundation.ProjectLock(owner.Details().ProjectID)
	sk, _ := foundation.AggregateLock(foundation.SkillAggregate, owner.Details().ID)
	deps, err := oc.NewAccessDependencies(foundation.Digest("sha256:"+strings.Repeat("f", 64)), []foundation.LockRequest{{Key: pk, Mode: foundation.Exclusive}, {Key: sk, Mode: foundation.Exclusive}})
	if err != nil {
		t.Fatal(err)
	}
	planner := &independentPlanner{deps: deps, tx: store.tx}
	authority := &independentAuthority{tx: store.tx, cause: cause, object: v.u.object}
	state.auth.Planner, state.auth.Cleanup = planner, authority
	request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.PurgeDeletedObjectMetadataAccess, Cleanup: cause, ObjectID: v.u.object})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := s.accessFacts(context.Background(), store, request)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := oc.NewAccessLockPlan(state.accessIssuer, oc.AccessPlanDetails{Request: request, DependencyRequest: request, Dependencies: deps, DomainBinding: facts.binding, Objects: facts.objects, Locks: append(facts.locks, deps.Locks()...)})
	if err != nil {
		t.Fatal(err)
	}
	locked, err := s.AcquireAccessPlansInTx(context.Background(), store.tx, []oc.AccessLockPlan{plan}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &independentMetadataFixture{s, store, v, cause, request, plan, locked, authority, planner, cleanup}
}
func indexOf(ids []string, want string) int {
	for i, id := range ids {
		if id == want {
			return i
		}
	}
	panic("unselected ID")
}
func (v *independentMetadataFixture) purge() (oc.ObjectMetadataPurgeResult, error) {
	return v.s.PurgeDeletedObjectMetadataInTx(context.Background(), v.store.tx, v.cause, v.source.u.object, v.plan, v.locked)
}

func TestIndependentMetadataBoundedWritesAndFinalAnchors(t *testing.T) {
	for _, c := range []struct {
		stage     string
		n, writes int
		final     bool
	}{{"object_leases", 32, 32, false}, {"project_work", 32, 32, false}, {"upload_attempts", 16, 32, false}, {"empty", 0, 5, true}} {
		t.Run(c.stage, func(t *testing.T) {
			v := independentMetadata(t, c.stage, c.n)
			out, err := v.purge()
			if err != nil {
				t.Fatal(err)
			}
			want := oc.CleanupPending
			if c.final {
				want = oc.CleanupCompleted
			}
			if out.State != want || len(v.store.writes) != c.writes || v.authority.calls != 1 || v.store.acquisitions != 1 {
				t.Fatalf("state=%v writes=%d authority=%d acquire=%d", out.State, len(v.store.writes), v.authority.calls, v.store.acquisitions)
			}
			if c.final {
				tables := []string{"UPDATE agenteam_object.uploads SET current_attempt_id=NULL", "DELETE FROM agenteam_object.cleanup_operations", "DELETE FROM agenteam_object.upload_attempts", "DELETE FROM agenteam_object.uploads", "DELETE FROM agenteam_object.objects"}
				ids := []string{"", v.cleanup, v.source.u.attempt.String(), v.source.u.id.String(), v.source.u.object.String()}
				for i, q := range v.store.writes {
					if !strings.HasPrefix(q, tables[i]) {
						t.Fatalf("final anchor order %d", i)
					}
					if i > 0 && (len(v.store.args[i]) != 1 || v.store.args[i][0] != ids[i]) {
						t.Fatal("wrong final identity")
					}
				}
			} else {
				for i, q := range v.store.writes {
					table := c.stage
					id := ""
					if c.stage != "upload_attempts" {
						id = v.store.ids[i]
					}
					if c.stage == "upload_attempts" {
						table = "upload_attempts"
						id = v.store.ids[i/2]
						if i%2 == 0 {
							table = "cleanup_operations"
							id = independentID(1000 + i/2)
						}
					}
					if q != "DELETE FROM agenteam_object."+table+" WHERE id=$1" || len(v.store.args[i]) != 1 || v.store.args[i][0] != id {
						t.Fatal("cross-table budget or exact selected identity changed")
					}
				}
			}
			before := len(v.store.writes)
			out, err = v.purge()
			if err == nil || out.State != oc.CleanupPending || len(v.store.writes) != before {
				t.Fatal("same live Tx obtained another batch")
			}
			v.store.assertClosed(t)
		})
	}
}

func TestIndependentMetadataAuthorityAndNativeGates(t *testing.T) {
	for _, name := range []string{"current-authority-denied", "planner-denied", "mapping-drift", "ended-Tx", "live-local", "ended-local-live-origin", "not-physical", "affected-zero", "write-error", "foreign-plan"} {
		t.Run(name, func(t *testing.T) {
			v := independentMetadata(t, "object_leases", 1)
			switch name {
			case "current-authority-denied":
				v.authority.deny = true
			case "planner-denied":
				v.planner.deny = true
			case "mapping-drift":
				v.store.rowRevision++
			case "ended-Tx":
				v.store.ended = true
			case "live-local", "ended-local-live-origin":
				v.s.state().projectWork["own"] = &projectWorkHandle{work: projectWork{object: v.source.u.object}, origin: v.store.tx, ended: name == "ended-local-live-origin"}
			case "not-physical":
				v.store.pending = true
			case "affected-zero":
				v.store.affected = 0
			case "write-error":
				v.store.failAt = 1
			case "foreign-plan":
				v.plan = oc.AccessLockPlan{}
			}
			out, err := v.purge()
			if err == nil || out.State != oc.CleanupPending {
				t.Fatal("missing terminal/authority fact accepted")
			}
			want := 0
			if name == "affected-zero" || name == "write-error" {
				want = 1
			}
			if len(v.store.writes) != want {
				t.Fatalf("write before required gate: %d", len(v.store.writes))
			}
			if name == "write-error" || name == "affected-zero" || name == "not-physical" || strings.Contains(name, "local") {
				v.store.pending = false
				v.store.failAt = 0
				v.store.affected = 1
				v.s.state().projectWork = map[string]*projectWorkHandle{}
				before := len(v.store.writes)
				if _, err = v.purge(); err == nil || len(v.store.writes) != before {
					t.Fatal("failed batch reset consumed live-Tx budget")
				}
			}
			v.store.assertClosed(t)
		})
	}
}

func TestIndependentMetadataDeletedHistoryCannotUsePhysicalException(t *testing.T) {
	v := independentMetadata(t, "empty", 0)
	op := &operation{service: v.s}
	// Even this exact call's returned I/O/checkpoint does not grant metadata the
	// finalize-only self-worker exception while its original operation is live.
	claim := cleanupClaim{id: independentID(10), worker: independentID(11), fence: 3}
	h := &projectWorkHandle{operation: op, work: projectWork{object: v.source.u.object, resource: claim.id, fence: 3}, origin: foundation.NewTx()}
	v.s.state().projectWork[claim.worker] = h
	call := &boundedCleanupCall{service: v.s, object: v.source.u.object, operation: op, completed: map[string]cleanupClaim{claim.worker: claim}}
	ctx := context.WithValue(context.WithValue(context.Background(), operationKey{}, op), boundedCleanupKey{}, call)
	workers, err := v.s.completedCleanupWorkers(ctx, v.source.u.object)
	if err != nil || !reflect.DeepEqual(workers, []string{claim.worker}) {
		t.Fatal("physical private-call setup", err)
	}
	if err = v.s.metadataPhysicalCompleted(ctx, v.store, v.source.u.object); err == nil {
		t.Fatal("metadata borrowed finalize self-worker exception")
	}
	h.ended = true
	if err = v.s.metadataPhysicalCompleted(ctx, v.store, v.source.u.object); err != nil {
		t.Fatal("actual returned, closed-origin work rejected", err)
	}
}

func TestIndependentMetadataFinalWriteErrorsStayPending(t *testing.T) {
	for fail := 1; fail <= 5; fail++ {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			v := independentMetadata(t, "empty", 0)
			v.store.failAt = fail
			out, err := v.purge()
			if err == nil || out.State != oc.CleanupPending || len(v.store.writes) != fail {
				t.Fatal("partial final transaction reported completion", out.State, err)
			}
			// These substituted writes are NOT a rollback proof. The caller must roll
			// back; a real two-domain transaction is an outstanding integration gate.
			before := len(v.store.writes)
			v.store.failAt = 0
			if _, err = v.purge(); err == nil || len(v.store.writes) != before {
				t.Fatal("retry bypassed one-live-Tx consumption")
			}
		})
	}
}

func TestIndependentMetadataEmptyStopLanesDoNotEstablishCompletion(t *testing.T) {
	for lane := 0; lane < 5; lane++ {
		t.Run(fmt.Sprint(lane), func(t *testing.T) {
			v := independentMetadata(t, "empty", 0)
			project, err := foundation.ParseID[identity.Project](v.source.u.owner.Details().ProjectID)
			if err != nil {
				t.Fatal(err)
			}
			cause, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: project, OperationID: auditID[oc.ProjectStopOperation](t), Action: oc.ProjectStopDelete, ProjectVersion: 2})
			if err != nil {
				t.Fatal(err)
			}
			v.store.queries = nil
			v.store.queryArgs = nil
			after := independentID(999)
			facts, err := v.s.projectStopFacts(context.Background(), v.store, cause, lane, after)
			if err != nil || len(facts.ids) != 0 || len(facts.objects) != 0 || len(v.store.queries) != 1 || len(facts.locks) != 1 || facts.binding.Validate() != nil {
				t.Fatal("empty lane expanded history", err)
			}
			q, args := v.store.queries[0], v.store.queryArgs[0]
			if len(args) != 3 || args[0] != project.String() || args[1] != after || args[2] != true || !strings.Contains(q, "LIMIT 32") {
				t.Fatal("original primary cursor/budget changed")
			}
			predicates := []string{"r.joined_at IS NULL", "r.disposition='reserved'", "r.state='active'", "NOT r.revoked", "retirement_evidence IS NULL"}
			if !strings.Contains(q, predicates[lane]) {
				t.Fatal("terminal history became candidate")
			}
			if lane == 4 && (!strings.Contains(q, "UNION") || strings.Count(q, "LIMIT 32") != 3) {
				t.Fatal("transfer pending union is not independently bounded")
			}
			v.store.pending = true
			pending, err := projectStopPending(context.Background(), v.store, cause)
			if err != nil || !pending || len(v.store.writes) != 0 {
				t.Fatal("bounded empty lane/cursor substituted for complete predicate", err)
			}
			v.store.assertClosed(t)
		})
	}
}
