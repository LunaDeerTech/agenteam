//go:build integration

package work_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

type fixtureStore interface {
	project.Store
	audit.Store
	work.Store
}
type fixture struct {
	db               *pgfixture.Database
	raw              *postgres.Store
	store            fixtureStore
	accounts         *account.Authority
	projectAuthority *project.Authority
	projects         *project.Service
	authority        *work.Authority
	service          *work.Service
	reader           *work.Reader
	skills           *skillFixture
	events           *outbox.Service
	workEvents       wc.WorkEvents
	keys             cursor.Keyring
}

func newDatabase(t *testing.T) *pgfixture.Database {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	migrate(t, db)
	conn := db.Connect(t)
	var count int
	if err := conn.QueryRow(ctxFor(t), `SELECT count(*) FROM pg_tables WHERE schemaname='agenteam_work'`).Scan(&count); err != nil || count != 5 {
		t.Fatal("compiled 00021 must supply exactly five Work tables", err)
	}
	return db
}
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	return assemble(t, db, raw, raw, true)
}
func assemble(t *testing.T, db *pgfixture.Database, raw *postgres.Store, store fixtureStore, createSkillSchema bool) *fixture {
	t.Helper()
	ak, ck := keys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	pa, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if err != nil {
		t.Fatal(err)
	}
	wa, err := work.NewAuthority(store, pa)
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: pa})
	if err != nil {
		t.Fatal(err)
	}
	catalog := event.NewCatalog()
	pt, err := c.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := wc.RegisterWorkEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := fixtureProcess{id[oc.Process](t)}
	events, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{c.ProjectProducer: pa, wc.WorkProducer: wa}, Projects: pa, Sessions: accounts, System: accounts, Processes: process, Audit: aud, Cursors: ck})
	if err != nil {
		t.Fatal(err)
	}
	if createSkillSchema {
		if _, err = raw.Exec(ctxFor(t), `CREATE SCHEMA work_fixture; CREATE TABLE work_fixture.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL,protected boolean NOT NULL,published boolean NOT NULL)`); err != nil {
			t.Fatal(err)
		}
	}
	skills := &skillFixture{store: store, authority: pa, issuer: c.NewInitializationPlanIssuer()}
	projects, err := project.New(store, project.Dependencies{Authority: pa, Activity: accounts, Audit: aud, Events: events, ProjectEvents: pt, Initializer: skills, Processes: process, Cursors: ck}, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		projects.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := projects.Drain(ctx); err != nil {
			t.Error(err)
		}
	})
	f := &fixture{db: db, raw: raw, store: store, accounts: accounts, projectAuthority: pa, projects: projects, authority: wa, skills: skills, events: events, workEvents: wt, keys: ck}
	f.service = f.newService(t, events, accounts)
	f.reader, err = work.NewReader(store, wa, ck)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *fixture) newService(t *testing.T, events oc.Appender, activity work.ActivityAuthority) *work.Service {
	t.Helper()
	s, err := work.New(f.store, work.Dependencies{Authority: f.authority, Events: events, WorkEvents: f.workEvents, Activity: activity})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.Drain(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}
func (f *fixture) milestone(t *testing.T, a identity.Actor, p c.ProjectID, title string) wc.Milestone {
	t.Helper()
	r, err := f.service.CreateMilestone(ctxFor(t), a, meta(t, id[struct{}](t).String(), nil), p, wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: title})
	if err != nil || r.Milestone == nil || !r.Changed {
		t.Fatal("create milestone", err)
	}
	return *r.Milestone
}
func (f *fixture) sprint(t *testing.T, a identity.Actor, p c.ProjectID, parent wc.MilestoneID, title string) wc.Sprint {
	t.Helper()
	r, err := f.service.CreateSprint(ctxFor(t), a, meta(t, id[struct{}](t).String(), nil), p, wc.CreateSprintRequest{SprintID: id[c.Sprint](t), MilestoneID: parent, Title: title})
	if err != nil || r.Sprint == nil || !r.Changed {
		t.Fatal("create sprint", err)
	}
	return r.Sprint.Clone()
}
func (f *fixture) tx(t *testing.T, locks []foundation.LockRequest, fn func(context.Context, foundation.Tx, postgres.SQLExecutor) error) {
	t.Helper()
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		x, err := f.store.InTx(tx)
		if err != nil {
			return err
		}
		return fn(ctx, tx, x)
	})
	if result.State() != foundation.Committed {
		t.Fatal("fixture transaction", result.State(), result.Fault())
	}
}
func fixtureLocks(a identity.Actor, p c.ProjectID, extra ...foundation.LockRequest) []foundation.LockRequest {
	user, _ := foundation.UserLock(a.Details().UserID)
	projectID, _ := foundation.ProjectLock(p.String())
	return append([]foundation.LockRequest{{Key: user, Mode: foundation.Exclusive}, {Key: projectID, Mode: foundation.Exclusive}}, extra...)
}
func (f *fixture) renew(t *testing.T, a identity.Actor) identity.Actor {
	t.Helper()
	session := id[identity.Session](t)
	verifier := sha256.Sum256([]byte(session.String()))
	user, err := foundation.ParseID[identity.User](a.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := foundation.UserLock(user.String())
	f.tx(t, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}, func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if err := f.accounts.RequireCurrentSession(ctx, tx, a); err != nil {
			return err
		}
		_, err := x.Exec(ctx, `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,issued_at,last_activity_at,idle_seconds,absolute_expires_at) VALUES($1,$2,$3,'a',clock_timestamp(),clock_timestamp(),3600,clock_timestamp()+interval '1 hour')`, session.String(), user.String(), verifier[:])
		return err
	})
	v, err := identity.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (f *fixture) ageSession(t *testing.T, a identity.Actor) {
	t.Helper()
	key, _ := foundation.UserLock(a.Details().UserID)
	f.tx(t, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}, func(ctx context.Context, _ foundation.Tx, x postgres.SQLExecutor) error {
		_, err := x.Exec(ctx, `UPDATE agenteam_account.sessions SET issued_at=LEAST(issued_at,clock_timestamp()-interval '3 minutes'),last_activity_at=clock_timestamp()-interval '2 minutes' WHERE id=$1`, a.Details().SessionID)
		return err
	})
}
func (f *fixture) snapshot(t *testing.T, a identity.Actor) string {
	t.Helper()
	var raw string
	err := f.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
'milestones',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.milestones t),
'sprints',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.sprints t),
'milestone_groups',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id),'[]'::jsonb) FROM agenteam_work.milestone_order_groups t),
'sprint_groups',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY project_id,milestone_id),'[]'::jsonb) FROM agenteam_work.sprint_order_groups t),
'receipts',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.structure_commands t WHERE state='completed'),
'events',(SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events t WHERE producer='work'),
'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1))::text`, a.Details().SessionID).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
func (f *fixture) eventCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_outbox.events WHERE producer='work'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
func jsonBytes(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func equalResult(t *testing.T, a, b wc.StructureMutation) {
	t.Helper()
	if !bytes.Equal(jsonBytes(t, a), jsonBytes(t, b)) {
		t.Fatal("typed receipt changed")
	}
}
func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(8 * time.Second):
		t.Fatal("owned handshake did not arrive")
	}
}

// Lifecycle seeding is test-only future input, under current owner, Project EX,
// schedule EX and target Sprint EX. It never claims Start/Complete is implemented.
func (f *fixture) lifecycle(t *testing.T, a identity.Actor, p c.ProjectID, sprint wc.Sprint, completed bool) {
	t.Helper()
	schedule, _ := foundation.ProjectScheduleLock(p.String())
	target, _ := foundation.AggregateLock(foundation.SprintAggregate, sprint.ID.String())
	f.tx(t, fixtureLocks(a, p, foundation.LockRequest{Key: schedule, Mode: foundation.Exclusive}, foundation.LockRequest{Key: target, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Mutate); err != nil {
			return err
		}
		history := jsonBytes(t, wc.ActorHistory{Kind: identity.Human, UserID: a.Details().UserID})
		var err error
		if completed {
			_, err = x.Exec(ctx, `WITH moment AS (SELECT clock_timestamp() AS at) UPDATE agenteam_work.sprints SET started_at=coalesce(started_at,moment.at),started_by=coalesce(started_by,$3::jsonb),completed_at=moment.at,completed_by=$3,updated_at=moment.at,version=version+1 FROM moment WHERE project_id=$1 AND id=$2`, p.String(), sprint.ID.String(), history)
			if err != nil {
				return err
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_project.projects SET current_sprint_id=NULL WHERE id=$1`, p.String())
		} else {
			_, err = x.Exec(ctx, `WITH moment AS (SELECT clock_timestamp() AS at) UPDATE agenteam_work.sprints SET started_at=moment.at,started_by=$3,updated_at=moment.at,version=version+1 FROM moment WHERE project_id=$1 AND id=$2`, p.String(), sprint.ID.String(), history)
			if err != nil {
				return err
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_project.projects SET current_sprint_id=$2 WHERE id=$1`, p.String(), sprint.ID.String())
		}
		return err
	})
	t.Log("test-only canonical lifecycle seed; actual Work calls still use real Authority")
}
func (f *fixture) seedLifecycle(t *testing.T, a identity.Actor, p c.ProjectID, lifecycle c.Lifecycle) {
	t.Helper()
	f.tx(t, fixtureLocks(a, p), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Read); err != nil {
			return err
		}
		switch lifecycle {
		case c.Archived:
			_, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, p.String())
			return err
		case c.Active:
			_, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='active',archived_at=NULL,current_lifecycle_operation_id=NULL,updated_at=clock_timestamp() WHERE id=$1`, p.String())
			return err
		default:
			return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
		}
	})
	t.Log("test-only consistent Project lifecycle input")
}

// This constructs only the future Project transition input needed by the Work
// gate. No lifecycle participant is registered or run and no stop is claimed.
func (f *fixture) seedProjectTransition(t *testing.T, a identity.Actor, p c.ProjectID, state c.Lifecycle) {
	t.Helper()
	if state != c.Archiving && state != c.Deleting {
		t.Fatal("unsupported test transition")
	}
	manifest, err := c.NewRequiredManifest([]c.ParticipantRegistration{
		{Name: c.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []c.ReferenceKind{"object"}},
		{Name: c.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []c.ReferenceKind{"secret"}},
		{Name: c.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", CleanupAfter: []c.ParticipantName{c.ArtifactObjectParticipant, c.SecretParticipant}},
		{Name: c.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", CleanupAfter: []c.ParticipantName{c.OutboxParticipant}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	operation := id[c.Operation](t)
	f.tx(t, fixtureLocks(a, p), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		access, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, a, p, identity.Mutate)
		if err != nil {
			return err
		}
		action := "archive"
		if state == c.Deleting {
			action = "delete"
		}
		if _, err = x.Exec(ctx, `WITH moment AS (SELECT clock_timestamp() AS at) INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) SELECT $1,$2,$3,$4,$5,'accepted',1,$6,$7,at,at FROM moment`, operation.String(), p.String(), a.Details().UserID, action, int64(access.Project().Version+1), jsonBytes(t, manifest), string(digest)); err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle=$2,version=version+1,current_lifecycle_operation_id=$3,updated_at=clock_timestamp() WHERE id=$1`, p.String(), string(state), operation.String())
		return err
	})
	t.Log("test-only admitted transition input with manifest metadata; no lifecycle participant run", state)
}

func (f *fixture) transferOwner(t *testing.T, previous, next identity.Actor, project c.ProjectID) {
	t.Helper()
	user, _ := foundation.UserLock(next.Details().UserID)
	f.tx(t, fixtureLocks(previous, project, foundation.LockRequest{Key: user, Mode: foundation.Exclusive}), func(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor) error {
		if _, err := f.projectAuthority.RequireOwnerInTx(ctx, tx, previous, project, identity.Mutate); err != nil {
			return err
		}
		if err := f.accounts.RequireCurrentSession(ctx, tx, next); err != nil {
			return err
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET owner_user_id=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, project.String(), next.Details().UserID)
		return err
	})
	t.Log("test-only owner transfer input; not an implemented ownership command")
}

type hookStore struct {
	fixtureStore
	mu          sync.Mutex
	after       func(context.Context, foundation.Tx, foundation.TransactionCause) error
	afterResult func(foundation.TransactionCause, foundation.CommitResult)
	beforeLocks func(context.Context, foundation.Tx, []foundation.LockRequest) error
}

func (s *hookStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.mu.Lock()
	after := s.after
	afterResult := s.afterResult
	s.mu.Unlock()
	result := s.fixtureStore.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if after != nil {
			return after(ctx, tx, cause)
		}
		return nil
	})
	if afterResult != nil {
		afterResult(cause, result)
	}
	return result
}
func (s *hookStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.mu.Lock()
	before := s.beforeLocks
	s.mu.Unlock()
	if before != nil {
		if err := before(ctx, tx, locks); err != nil {
			return err
		}
	}
	return s.fixtureStore.AcquireAll(ctx, tx, locks)
}
func (s *hookStore) setAfter(fn func(context.Context, foundation.Tx, foundation.TransactionCause) error) {
	s.mu.Lock()
	s.after = fn
	s.mu.Unlock()
}

type capturedEvent struct {
	Event event.Event
	Plan  oc.AppendPlan
	Actor identity.Actor
}
type capturingAppender struct {
	oc.Appender
	mu         sync.Mutex
	captured   []capturedEvent
	before     func(context.Context, identity.Actor, event.Event) error
	after      func(context.Context, identity.Actor, event.Event, oc.AppendPlan) error
	failAppend bool
}

func (a *capturingAppender) PrepareAppend(ctx context.Context, actor identity.Actor, e event.Event) (oc.AppendPlan, error) {
	if a.before != nil {
		if err := a.before(ctx, actor, e); err != nil {
			return oc.AppendPlan{}, err
		}
	}
	p, err := a.Appender.PrepareAppend(ctx, actor, e)
	if err != nil {
		return p, err
	}
	a.mu.Lock()
	a.captured = append(a.captured, capturedEvent{e, p, actor})
	a.mu.Unlock()
	if a.after != nil {
		if err = a.after(ctx, actor, e, p); err != nil {
			return oc.AppendPlan{}, err
		}
	}
	return p, nil
}
func (a *capturingAppender) AppendEventInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, e event.Event, p oc.AppendPlan) (oc.AppendReceipt, error) {
	r, err := a.Appender.AppendEventInTx(ctx, tx, actor, e, p)
	if err != nil {
		return r, err
	}
	if a.failAppend {
		return oc.AppendReceipt{}, foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	return r, nil
}
func (a *capturingAppender) last(t *testing.T) capturedEvent {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.captured) == 0 {
		t.Fatal("no actual prepared event")
	}
	return a.captured[len(a.captured)-1]
}

type failActivity struct{ work.ActivityAuthority }

func (a failActivity) TouchActivityInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	if err := a.ActivityAuthority.TouchActivityInTx(ctx, tx, actor); err != nil {
		return err
	}
	return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
}

// Only exact task-owned advisory keys and database PIDs are observed. A waiter
// is established by pg_locks; a sleep is never the concurrency assertion.
// lockAttempt records the actual caller Tx immediately before the real Store
// AcquireAll. It is only an observation; no lock or CommitResult is replaced.
type lockAttempt struct {
	BackendPID int32
	Request    foundation.LockRequest
}

func observeLock(s *hookStore, key foundation.LockKey, active func() bool) <-chan lockAttempt {
	observed := make(chan lockAttempt, 1)
	var once atomic.Bool
	s.mu.Lock()
	s.beforeLocks = func(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
		if active != nil && !active() {
			return nil
		}
		for _, request := range locks {
			if foundation.CompareLockKeys(request.Key, key) != 0 || !once.CompareAndSwap(false, true) {
				continue
			}
			x, err := s.InTx(tx)
			if err != nil {
				return err
			}
			var pid int32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			observed <- lockAttempt{BackendPID: pid, Request: request}
			break
		}
		return nil
	}
	s.mu.Unlock()
	return observed
}
func awaitLockAttempt(t *testing.T, observed <-chan lockAttempt) lockAttempt {
	t.Helper()
	select {
	case attempt := <-observed:
		if attempt.BackendPID <= 0 || attempt.Request.Mode != foundation.Exclusive {
			t.Fatal("caller did not request a real ExclusiveLock", attempt)
		}
		return attempt
	case <-time.After(5 * time.Second):
		t.Fatal("actual caller Tx did not reach AcquireAll")
	}
	return lockAttempt{}
}
func waitExactLock(t *testing.T, db *pgfixture.Database, attempt lockAttempt, granted bool, blocker int32) {
	t.Helper()
	if attempt.BackendPID <= 0 || attempt.Request.Mode != foundation.Exclusive {
		t.Fatal("invalid exact lock observation")
	}
	conn := db.Connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n := uint64(attempt.Request.Key.AdvisoryKey())
	for {
		var matched bool
		err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.datname=$1 AND l.locktype='advisory' AND l.classid::bigint=$2 AND l.objid::bigint=$3 AND l.objsubid=1 AND l.pid=$4 AND l.mode='ExclusiveLock' AND l.granted=$5 AND ($6::int=0 OR $6=ANY(pg_blocking_pids(l.pid))))`, db.Name, int64(n>>32), int64(n&0xffffffff), attempt.BackendPID, granted, blocker).Scan(&matched)
		if err != nil {
			t.Fatal(err)
		}
		if matched {
			t.Log("exact actual Tx lock", attempt.BackendPID, attempt.Request.Key.AdvisoryKey(), "ExclusiveLock", "granted", granted, "blocker", blocker)
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("bound caller PID/key/ExclusiveLock state absent", attempt.BackendPID, granted, blocker)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
func observedFixture(t *testing.T, base *fixture) (*fixture, *hookStore) {
	t.Helper()
	raw := openStore(t, base.db.Config(t, map[string]string{"LOCK_TIMEOUT": "5s"}))
	store := &hookStore{fixtureStore: raw}
	return assemble(t, base.db, raw, store, false), store
}
func proxyFixture(t *testing.T, base *fixture, commit bool) (*fixture, *hookStore, *commitProxy) {
	t.Helper()
	proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", base.db.Fixture.Port), commit)
	u, err := url.Parse(base.db.Fixture.URL(base.db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = proxy.listener.Addr().String()
	raw := openStore(t, base.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "LOCK_TIMEOUT": "5s"}))
	store := &hookStore{fixtureStore: raw}
	return assemble(t, base.db, raw, store, false), store, proxy
}

// The positive Human/initializer subset is reused from accepted B02. No Object
// Runtime, ProcessGuard, stopped-process proof or MinIO helper is included.
func ctxFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func id[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	v, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func meta(t *testing.T, key string, version *foundation.Version) foundation.CommandMeta {
	return foundation.CommandMeta{RequestID: id[foundation.Request](t), IdempotencyKey: foundation.IdempotencyKey(key), ExpectedVersion: version}
}
func requireCode(t *testing.T, e error, want foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(e, &f) || f.Code != want {
		t.Fatalf("error=%v want=%s", e, want)
	}
}
func cause(t *testing.T) foundation.TransactionCause {
	v, e := foundation.NewRecoveryCause("work.fixture", id[struct{}](t).String(), "")
	if e != nil {
		t.Fatal(e)
	}
	return v
}

type fixtureProcess struct{ id oc.ProcessID }

func (p fixtureProcess) CurrentProcess() oc.ProcessID { return p.id }
func (p fixtureProcess) ConfirmStopped(context.Context, oc.ProcessID) error {
	return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
}

func keys(t *testing.T) (account.Keyring, cursor.Keyring) {
	t.Helper()
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	cursor, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if e != nil {
		t.Fatal(e)
	}
	secret, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), cursor)
	if e != nil {
		t.Fatal(e)
	}
	download, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), cursor, secret)
	if e != nil {
		t.Fatal(e)
	}
	account, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), cursor, secret, download)
	if e != nil {
		t.Fatal(e)
	}
	return account, cursor
}
func openStore(t *testing.T, cfg postgres.Config) *postgres.Store {
	t.Helper()
	s, e := postgres.Open(ctxFor(t), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e = s.ForceClose(ctx); e != nil {
			t.Error(e)
		}
	})
	return s
}

type skillFixture struct {
	store     project.Store
	authority *project.Authority
	issuer    c.InitializationPlanIssuer
	mu        sync.Mutex
	mode      string
}

func (s *skillFixture) setMode(mode string) { s.mu.Lock(); s.mode = mode; s.mu.Unlock() }
func (s *skillFixture) InspectProjectSkills(ctx context.Context, actor identity.Actor, r c.InitializationRequest) (c.InitializationResult, error) {
	if actor.Details().ServiceName != identity.ProjectInitialization || actor.Details().CauseRef != r.CreationID.String() || actor.Details().ProjectID != r.ProjectID.String() {
		return c.InitializationResult{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	var project, key, skill string
	var revision int64
	e := s.store.QueryRow(ctx, `SELECT project_id::text,init_key,skill_id::text,revision FROM work_fixture.skills WHERE creation_id=$1`, r.CreationID.String()).Scan(&project, &key, &skill, &revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return c.InitializationResult{State: c.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: c.ReasonWorkPending}, nil
	}
	if e != nil {
		return c.InitializationResult{}, e
	}
	if project != r.ProjectID.String() || key != string(r.InitializationKey) {
		return c.InitializationResult{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	skillID, e := foundation.ParseID[c.Skill](skill)
	if e != nil {
		return c.InitializationResult{}, e
	}
	rev := foundation.Revision(revision)
	return c.InitializationResult{State: c.InitializationCompleted, CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: &skillID, Revision: &rev}, nil
}
func (s *skillFixture) InitializeProjectSkills(ctx context.Context, actor identity.Actor, r c.InitializationRequest) (c.InitializationResult, error) {
	s.mu.Lock()
	mode := s.mode
	s.mu.Unlock()
	if mode == "pending" {
		return c.InitializationResult{State: c.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: c.ReasonWorkPending}, nil
	}
	projectKey, _ := foundation.ProjectLock(r.ProjectID.String())
	command, _ := foundation.NewRecoveryCause("project.fixture-skill", r.CreationID.String(), "")
	result := s.store.WithinTx(ctx, command, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: projectKey, Mode: foundation.Exclusive}}); e != nil {
			return e
		}
		if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
			return e
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return e
		}
		skill, e := foundation.NewID[c.Skill]()
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO work_fixture.skills(creation_id,project_id,init_key,skill_id,revision,protected,published) VALUES($1,$2,$3,$4,1,true,true) ON CONFLICT(creation_id) DO NOTHING`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), skill.String())
		return e
	})
	if result.State() != foundation.Committed {
		return c.InitializationResult{}, foundation.NewFault(foundation.DependencyUnavailable, result.State())
	}
	return s.InspectProjectSkills(ctx, actor, r)
}
func (s *skillFixture) DiscoverConfirmation(ctx context.Context, actor identity.Actor, r c.InitializationRequest) (c.InitializationConfirmationPlan, error) {
	result, e := s.InspectProjectSkills(ctx, actor, r)
	if e != nil {
		return c.InitializationConfirmationPlan{}, e
	}
	if result.State != c.InitializationCompleted {
		return c.InitializationConfirmationPlan{}, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	key, _ := foundation.ProjectLock(r.ProjectID.String())
	locks := []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}
	return s.issuer.Plan(actor, r, c.InitializationReceipt{CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: *result.AddSkillsID, Revision: *result.Revision}, locks)
}
func (s *skillFixture) ConfirmInitializedInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, r c.InitializationRequest, plan c.InitializationConfirmationPlan) (c.InitializationReceipt, error) {
	if !s.issuer.Matches(plan, actor, r) {
		return c.InitializationReceipt{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	locks := plan.RequiredLocks()
	if e := s.store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return c.InitializationReceipt{}, e
	}
	if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
		return c.InitializationReceipt{}, e
	}
	x, e := s.store.InTx(tx)
	if e != nil {
		return c.InitializationReceipt{}, e
	}
	receipt := plan.ProposedReceipt()
	var valid bool
	e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM work_fixture.skills WHERE creation_id=$1 AND project_id=$2 AND init_key=$3 AND skill_id=$4 AND revision=$5 AND protected AND published)`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), receipt.AddSkillsID.String(), int64(receipt.Revision)).Scan(&valid)
	if e != nil {
		return c.InitializationReceipt{}, e
	}
	if !valid {
		return c.InitializationReceipt{}, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	return receipt, nil
}

func (f *fixture) human(t *testing.T, username, role string) identity.Actor {
	t.Helper()
	user, session := id[identity.User](t), id[identity.Session](t)
	// Test-owned Account facts are consumed through the real Account authority;
	// no fake Session/Owner provider is installed in the production service.
	if _, e := f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_account.users(id,email,username,display_name,role,password_phc,password_version,version,auth_sequence,initial_password_suggestion,theme) VALUES($1,$2,$3,'fixture',$4,'fixture-not-a-login-hash',1,1,1,false,'system')`, user.String(), username+"@example.test", username, role); e != nil {
		t.Fatal(e)
	}
	if _, e := f.raw.Exec(ctxFor(t), `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,issued_at,last_activity_at,idle_seconds,absolute_expires_at) VALUES($1,$2,$3,'a',clock_timestamp()-interval '2 minutes',clock_timestamp()-interval '2 minutes',3600,clock_timestamp()+interval '1 hour')`, session.String(), user.String(), bytes.Repeat([]byte(username), 32)[:32]); e != nil {
		t.Fatal(e)
	}
	actor, e := identity.NewHuman(user, session)
	if e != nil {
		t.Fatal(e)
	}
	return actor
}
func (f *fixture) create(t *testing.T, actor identity.Actor, name string) (c.ProjectRef, foundation.CommandMeta, c.CreateProjectRequest) {
	t.Helper()
	request := c.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: name, Description: "private body"}
	m := meta(t, id[struct{}](t).String(), nil)
	result, e := f.projects.CreateProject(ctxFor(t), actor, m, request)
	if e != nil || result.State != c.CreationReady {
		t.Fatal("create ready", result.State, e)
	}
	return *result.Project, m, request
}

func migrationFiles(t *testing.T, through string) fstest.MapFS {
	t.Helper()
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations.SQL, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name[:5] > through {
			continue
		}
		raw, err := fs.ReadFile(migrations.SQL, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	return files
}
func migrate(t *testing.T, db *pgfixture.Database, sources ...postgres.Source) {
	t.Helper()
	m, err := postgres.NewMigrator(db.Config(t, nil), sources...)
	if err != nil {
		t.Fatal(err)
	}
	if result := m.Migrate(ctxFor(t)); !result.Migrated {
		t.Fatal("project migration", result.Fault)
	}
}
func migrationPrefix(t *testing.T, through string) postgres.Source {
	t.Helper()
	source, err := postgres.NewSource(migrationFiles(t, through), nil)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

// Adapted owned real-COMMIT frame proxy: backend PID binds the exact writer.
type commitProxy struct {
	listener                          net.Listener
	upstream                          string
	reached, release, completed, quit chan struct{}
	targetPID                         atomic.Int32
	backendPID                        atomic.Int32
	once                              sync.Once
	wg                                sync.WaitGroup
	mu                                sync.Mutex
	connections                       map[net.Conn]bool
	commit                            bool
}

func newCommitProxy(t *testing.T, upstream string, commit bool) *commitProxy {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &commitProxy{listener: listener, upstream: upstream, reached: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool), commit: commit}
	p.targetPID.Store(0)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, e := listener.Accept()
			if e != nil {
				return
			}
			p.mu.Lock()
			select {
			case <-p.quit:
				p.mu.Unlock()
				_ = client.Close()
				return
			default:
				p.connections[client] = true
			}
			p.mu.Unlock()
			p.wg.Add(1)
			go p.serve(client)
		}
	}()
	t.Cleanup(func() {
		p.once.Do(func() {
			close(p.quit)
			_ = p.listener.Close()
			p.mu.Lock()
			for c := range p.connections {
				_ = c.Close()
			}
			p.mu.Unlock()
			p.wg.Wait()
		})
	})
	return p
}
func (p *commitProxy) serve(client net.Conn) {
	defer p.wg.Done()
	defer func() { _ = client.Close(); p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	server, e := net.DialTimeout("tcp", p.upstream, 3*time.Second)
	if e != nil {
		return
	}
	defer func() { _ = server.Close(); p.mu.Lock(); delete(p.connections, server); p.mu.Unlock() }()
	p.mu.Lock()
	select {
	case <-p.quit:
		p.mu.Unlock()
		return
	default:
		p.connections[server] = true
	}
	p.mu.Unlock()
	var header [4]byte
	if _, e = io.ReadFull(client, header[:]); e != nil {
		return
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 1<<20 {
		return
	}
	startup := make([]byte, int(size))
	copy(startup, header[:])
	if _, e = io.ReadFull(client, startup[4:]); e != nil {
		return
	}
	if _, e = server.Write(startup); e != nil {
		return
	}
	var held atomic.Bool
	var backend atomic.Int32
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := readPGFrame(client)
			if e != nil {
				return
			}
			if frame[0] == 'Q' && strings.EqualFold(strings.Trim(string(frame[5:]), "\x00; \t\r\n"), "COMMIT") && backend.Load() != 0 && p.targetPID.CompareAndSwap(backend.Load(), 0) {
				held.Store(true)
				p.backendPID.Store(backend.Load())
				_ = client.Close()
				close(p.reached)
				select {
				case <-p.release:
				case <-p.quit:
					return
				}
				if !p.commit {
					_ = server.Close()
					close(p.completed)
					return
				}
				if _, e = server.Write(frame); e != nil {
					return
				}
				select {
				case <-p.completed:
				case <-p.quit:
				}
				return
			}
			if _, e = server.Write(frame); e != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := readPGFrame(server)
			if e != nil {
				return
			}
			if frame[0] == 'K' && len(frame) >= 9 {
				backend.Store(int32(binary.BigEndian.Uint32(frame[5:9])))
			}
			if held.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" {
				ready, e := readPGFrame(server)
				if e == nil && ready[0] == 'Z' && len(ready) == 6 && ready[5] == 'I' {
					close(p.completed)
				}
				return
			}
			if _, e = client.Write(frame); e != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}

func readPGFrame(reader io.Reader) ([]byte, error) {
	var header [5]byte
	if _, e := io.ReadFull(reader, header[:]); e != nil {
		return nil, e
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size < 4 || size > 1<<20 {
		return nil, errors.New("invalid fixture frame")
	}
	frame := make([]byte, int(size)+1)
	copy(frame, header[:])
	_, e := io.ReadFull(reader, frame[5:])
	return frame, e
}
