package skill

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This transactional double checks ordering/rollback, not PostgreSQL or D05
// witness semantics. Real publication remains a separate integration gate.
type initializationWriteStore struct {
	*initializationReadStore
	unknownAt                       int
	skillInserted, revisionInserted bool
	work                            map[string][]any
}

func (s *initializationWriteStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.Contains(query, "FROM agenteam_skill.work") {
		if v, ok := s.work[args[0].(string)]; ok {
			return skillRowValues{values: v}
		}
		return skillRowValues{err: pgx.ErrNoRows}
	}
	return s.initializationReadStore.QueryRow(ctx, query, args...)
}

func (s *initializationWriteStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.initializationReadStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *initializationWriteStore) WithinTx(ctx context.Context, cause f.TransactionCause, work func(context.Context, f.Tx) error) f.CommitResult {
	before := append([]any(nil), s.row.values...)
	workBefore := map[string][]any{}
	for id, v := range s.work {
		workBefore[id] = append([]any(nil), v...)
	}
	skill, revision := s.skillInserted, s.revisionInserted
	s.unknown = s.txs+1 == s.unknownAt
	r := s.initializationReadStore.WithinTx(ctx, cause, work)
	if r.State() == f.NotCommitted {
		s.work = workBefore
		s.row.values = before
		s.skillInserted = skill
		s.revisionInserted = revision
	}
	return r
}
func (s *initializationWriteStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !s.live {
		return pgconn.CommandTag{}, errors.New("SQL outside live transaction")
	}
	switch {
	case strings.HasPrefix(query, "INSERT INTO agenteam_skill.work"):
		if s.work == nil {
			s.work = map[string][]any{}
		}
		s.work[args[0].(string)] = []any{args[0], args[1], args[2], args[3], args[4], "running", int64(1), args[5], (*time.Time)(nil)}
	case strings.HasPrefix(query, "UPDATE agenteam_skill.work SET phase='joined'"):
		v := s.work[args[0].(string)]
		if len(v) != 9 || v[3] != args[1] || v[6] != args[2] {
			return pgconn.CommandTag{}, errors.New("wrong work owner")
		}
		v[5] = "joined"
		at := v[7].(time.Time)
		v[8] = &at
	case strings.HasPrefix(query, "INSERT INTO agenteam_skill.object_attempts"):
		if len(args) != 8 || args[7] != stateID[oc.Process](50).String() {
			return pgconn.CommandTag{}, errors.New("attempt process/mapping changed")
		}
	case strings.HasPrefix(query, "UPDATE agenteam_skill.initializations SET object_id"):
		if args[4] != s.row.values[15] {
			return pgconn.CommandTag{}, errors.New("wrong original version")
		}
		s.row.values[16] = args[1]
		s.row.values[17] = args[2]
		s.row.values[18] = args[3]
		s.row.values[14] = "reserved"
		s.row.values[15] = s.row.values[15].(int64) + 1
	case strings.HasPrefix(query, "INSERT INTO agenteam_skill.skills"):
		s.skillInserted = true
	case strings.HasPrefix(query, "INSERT INTO agenteam_skill.revisions"):
		s.revisionInserted = true
	case strings.HasPrefix(query, "UPDATE agenteam_skill.initializations SET phase='published'"):
		if !s.skillInserted || !s.revisionInserted {
			return pgconn.CommandTag{}, errors.New("early publication")
		}
		s.row.values[14] = "published"
		s.row.values[15] = s.row.values[15].(int64) + 1
	default:
		return pgconn.CommandTag{}, errors.New("unexpected SQL")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type initializationObjects struct {
	ObjectPorts
	t                              *testing.T
	store                          *initializationWriteStore
	projects                       *initializationReadProjects
	issuer                         oc.AccessIssuer
	row                            initializationRow
	steps                          []string
	fail                           string
	sentinel                       error
	discardStarted, discardRelease chan struct{}
}

func (o *initializationObjects) PreparePayload(_ context.Context, _ id.Actor, _ oc.ObjectOwner, media string, size int64, digest *f.Digest, body io.ReadCloser) (oc.PreparedPayload, error) {
	o.steps = append(o.steps, "prepare")
	if o.store.live {
		o.t.Fatal("physical preparation inside Tx")
	}
	data, e := io.ReadAll(body)
	closeErr := body.Close()
	if e != nil || closeErr != nil || int64(len(data)) != size || sum(data) != *digest {
		o.t.Fatal("not original actual bundle")
	}
	return oc.NewPreparedPayload(oc.PreparedDetails{ID: stateID[oc.Payload](90), MediaType: media, Length: size, SHA256: *digest})
}
func (o *initializationObjects) DiscardPrepared(oc.PreparedPayload) error {
	o.steps = append(o.steps, "discard")
	if o.discardStarted != nil {
		close(o.discardStarted)
		<-o.discardRelease
	}
	if o.fail == "discard" {
		return o.sentinel
	}
	return nil
}
func (o *initializationObjects) DiscoverAccess(_ context.Context, request oc.AccessRequest) (oc.AccessLockPlan, error) {
	if o.store.live {
		o.t.Fatal("discovery inside Tx")
	}
	locks, _ := o.row.locks(f.Exclusive, stateID[oc.StoredObject](7))
	deps, e := oc.NewAccessDependencies(o.row.semantic, locks)
	if e != nil {
		return oc.AccessLockPlan{}, e
	}
	return oc.NewAccessLockPlan(o.issuer, oc.AccessPlanDetails{Request: request, DependencyRequest: request, Dependencies: deps, DomainBinding: o.row.semantic, Objects: []oc.ObjectID{stateID[oc.StoredObject](7)}, Locks: locks})
}
func (o *initializationObjects) AcquireAccessPlansInTx(ctx context.Context, tx f.Tx, plans []oc.AccessLockPlan, extra []f.LockRequest) (oc.LockedAccess, error) {
	locks := append([]f.LockRequest(nil), extra...)
	for _, p := range plans {
		locks = append(locks, p.Details().Locks...)
	}
	locks, e := oc.NormalizeAccessLocks(locks)
	if e != nil {
		return oc.LockedAccess{}, e
	}
	if e = o.store.AcquireAll(ctx, tx, locks); e != nil {
		return oc.LockedAccess{}, e
	}
	return oc.NewLockedAccess(o.issuer, tx, plans, extra)
}
func (o *initializationObjects) ReserveUploadInTx(_ context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, command f.CommandMeta, _ oc.PreparedPayload, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.UploadAttempt, error) {
	o.steps = append(o.steps, "reserve")
	if !o.store.live || tx != o.store.tx || !locked.Matches(o.issuer, tx, plan, plan.Details().Request) || command.IdempotencyKey != o.row.request.InitializationKey || actor.Details().CauseRef != o.row.request.CreationID.String() || owner.Details().ID != o.row.revision.String() {
		o.t.Fatal("reservation identity/lock mismatch")
	}
	return oc.NewUploadAttempt(oc.AttemptDetails{ID: stateID[oc.Attempt](6), ObjectID: stateID[oc.StoredObject](7), UploadID: stateID[oc.Upload](8)})
}
func (o *initializationObjects) UploadPrepared(_ context.Context, _ id.Actor, _ oc.ObjectOwner, _ oc.PreparedPayload, a oc.UploadAttempt) (oc.UploadAttempt, error) {
	o.steps = append(o.steps, "physical")
	if o.store.live || o.store.txs != 3 || o.store.row.values[14] != "reserved" {
		o.t.Fatal("physical work before known reservation commit")
	}
	if o.fail == "physical" {
		return oc.UploadAttempt{}, o.sentinel
	}
	if o.fail == "revoke" {
		o.projects.gate = o.sentinel
	}
	return a, nil
}
func (o *initializationObjects) PublishVerifiedInTx(_ context.Context, tx f.Tx, _ id.Actor, _ oc.ObjectOwner, a oc.UploadAttempt, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.PutResult, error) {
	o.steps = append(o.steps, "publish")
	if !o.store.live || !o.store.skillInserted || !locked.Matches(o.issuer, tx, plan, plan.Details().Request) {
		o.t.Fatal("publish not atomic with existing Skill")
	}
	if o.fail == "publish" {
		return oc.PutResult{}, o.sentinel
	}
	scope, _ := id.InProject(o.row.request.ProjectID)
	result := oc.PutResult{Meta: oc.ObjectMeta{ID: a.Details().ObjectID, Scope: scope, MediaType: sc.PackageMediaType, ByteSize: o.row.bundle.size, SHA256: o.row.bundle.packageDigest, State: oc.Available, Version: 1, CreatedAt: o.row.created}}
	if o.fail == "foreign_object" {
		result.Meta.ID = stateID[oc.StoredObject](99)
	}
	if o.fail == "prospective_receipt" {
		owner, _ := o.row.owner()
		result.Receipt, _ = oc.NewUploadReceipt(oc.ReceiptDetails{ID: stateID[oc.Receipt](99), UploadID: a.Details().UploadID, ObjectID: a.Details().ObjectID, Owner: owner, CreationCause: o.row.request.CreationID.String()})
	}
	return result, nil
}
func TestInitializationWriterCommitBeforePhysicalAndAtomicPublication(t *testing.T) {
	for _, name := range []string{"success", "replay", "plan_unknown", "work_unknown", "reserve_unknown", "publish_unknown", "retirement_unknown", "physical", "revoke", "publish", "foreign_object", "prospective_receipt", "discard"} {
		t.Run(name, func(t *testing.T) {
			s, read, projects, actor, request := readFixture(t)
			store := &initializationWriteStore{initializationReadStore: read}
			s.state().authority.state().store = store
			o := &initializationObjects{t: t, store: store, projects: projects, issuer: oc.NewAccessIssuer(), row: stateRow(t), fail: name, sentinel: f.NewFault(f.DependencyUnavailable, f.Unknown)}
			s.state().objects = o
			if name != "replay" {
				read.row.values = stateValues(t)
			}
			switch name {
			case "plan_unknown":
				store.unknownAt = 1
			case "reserve_unknown":
				store.unknownAt = 3
			case "work_unknown":
				store.unknownAt = 2
			case "publish_unknown":
				store.unknownAt = 4
			case "retirement_unknown":
				store.unknownAt = 5
			}
			out, e := s.InitializeProjectSkills(context.Background(), actor, request)
			trace := strings.Join(o.steps, ",")
			switch name {
			case "success":
				if e != nil || out.State != pc.InitializationCompleted || trace != "prepare,reserve,physical,publish,discard" || read.row.values[14] != "published" {
					t.Fatal("incomplete publication", e, trace)
				}
			case "replay":
				if e != nil || out.State != pc.InitializationCompleted || trace != "" {
					t.Fatal("replayed physical work", e, trace)
				}
			case "plan_unknown", "work_unknown", "reserve_unknown", "publish_unknown", "retirement_unknown":
				if e == nil || out.State != "" {
					t.Fatal("unknown became completion")
				}
				if _, ok := UnknownAttempt(e); !ok {
					t.Fatal("original unknown lost")
				}
				want := ""
				if name == "reserve_unknown" {
					want = "prepare,reserve,discard"
				}
				if name == "publish_unknown" || name == "retirement_unknown" {
					want = "prepare,reserve,physical,publish,discard"
				}
				if trace != want {
					t.Fatal("external work after unknown", trace)
				}
			default:
				if e == nil || out.State != "" {
					t.Fatal("failed step became completion")
				}
				if name == "physical" || name == "revoke" || name == "publish" || name == "discard" {
					if e != o.sentinel {
						t.Fatal("original dependency error replaced")
					}
				}
				if name != "discard" && (store.skillInserted || store.revisionInserted) {
					t.Fatal("failed publication left visible partial row")
				}
			}
			s.Stop()
			if name == "work_unknown" || name == "retirement_unknown" {
				if s.Joined() {
					t.Fatal("durable Unknown erased")
				}
				if e = s.Drain(context.Background()); e != nil {
					t.Fatal("original serialized work could not retire", e)
				}
			}
			if !s.Joined() {
				t.Fatal("local calls not actually retired")
			}
		})
	}
}

func TestSkillInitializationWorkWaitsForDiscardAndOriginalBudget(t *testing.T) {
	s, read, projects, actor, request := readFixture(t)
	read.row.values = stateValues(t)
	store := &initializationWriteStore{initializationReadStore: read}
	s.state().authority.state().store = store
	o := &initializationObjects{t: t, store: store, projects: projects, issuer: oc.NewAccessIssuer(), row: stateRow(t), discardStarted: make(chan struct{}), discardRelease: make(chan struct{})}
	s.state().objects = o
	done := make(chan error, 1)
	go func() { _, e := s.InitializeProjectSkills(context.Background(), actor, request); done <- e }()
	select {
	case <-o.discardStarted:
	case <-time.After(time.Second):
		t.Fatal("did not reach actual discard")
	}
	s.Stop()
	if s.Joined() {
		t.Fatal("cancel called actual discard joined")
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if e := s.Drain(expired); !errors.Is(e, context.Canceled) {
		t.Fatal("original drain budget replaced", e)
	}
	close(o.discardRelease)
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled accounting tail became clean success")
		}
	case <-time.After(time.Second):
		t.Fatal("call did not finish")
	}
	if s.Joined() {
		t.Fatal("cancelled DB tail silently removed durable work")
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if e := s.Drain(ctx); e != nil || !s.Joined() {
		t.Fatal("actual returned local work could not retire", e)
	}
}
