//go:build integration

package skill_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

func testID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	v, e := f.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func testCause(t *testing.T) f.TransactionCause {
	t.Helper()
	v, e := f.NewRecoveryCause("skills.fixture", testID[struct{}](t).String(), "")
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func requireCommitted(t *testing.T, r f.CommitResult) {
	t.Helper()
	if r.State() != f.Committed {
		t.Fatal("fixture transaction did not commit", r.State(), r.Fault())
	}
}

// Only the registered ProjectInitialization route is exercised here. Account is
// a real Authority dependency, but these are canonical test-owned Project and
// Creation facts, not a production Project.Create or Human login acceptance.
// All Skill SQL, locks, transactions and four initializer methods are real.
// The explicitly controlled Object boundary below is not a D05 publication or
// private Audit-witness success and does not remove the Object stop limitation.
type skillPG struct {
	db       *pgfixture.Database
	store    *postgres.Store
	projects *project.Authority
}

func newSkillPG(t *testing.T) *skillPG {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	migrator, e := postgres.NewMigrator(db.Config(t, nil))
	if e != nil {
		t.Fatal(e)
	}
	if result := migrator.Migrate(testContext(t)); !result.Migrated {
		t.Fatal("continuous migration required", result.Fault)
	}
	store := openSkillStore(t, db.Config(t, nil))
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	cursors, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), cursors)
	if e != nil {
		t.Fatal(e)
	}
	downloads, e := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), cursors, secrets)
	if e != nil {
		t.Fatal(e)
	}
	accountsKey, e := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), cursors, secrets, downloads)
	if e != nil {
		t.Fatal(e)
	}
	accounts, e := account.NewAuthority(store, accountsKey)
	if e != nil {
		t.Fatal(e)
	}
	projects, e := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts})
	if e != nil {
		t.Fatal(e)
	}
	return &skillPG{db, store, projects}
}
func openSkillStore(t *testing.T, cfg postgres.Config) *postgres.Store {
	t.Helper()
	store, e := postgres.Open(testContext(t), cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		e := store.Drain(ctx)
		cancel()
		if e != nil {
			t.Error("Store Drain failed", e)
			ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			e = store.ForceClose(ctx)
			cancel()
			if e != nil {
				t.Error("Store ForceClose failed", e)
			}
			if e = store.Drain(context.Background()); e != nil {
				t.Error("Store actual final join failed", e)
			}
		}
	})
	return store
}

type skillCase struct {
	request   pc.InitializationRequest
	actor     id.Actor
	service   *skill.Service
	authority *skill.Authority
	objects   *controlledObjects
	owner     id.UserID
}

func (p *skillPG) newCase(t *testing.T) *skillCase {
	t.Helper()
	request := pc.InitializationRequest{CreationID: testID[pc.Creation](t), ProjectID: testID[id.Project](t), InitializationKey: f.IdempotencyKey("skill-" + testID[struct{}](t).String())}
	owner := testID[id.User](t)
	scope, e := id.InProject(request.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	registration, e := id.RegisterService(id.ProjectInitialization)
	if e != nil {
		t.Fatal(e)
	}
	actor, e := registration.Actor(request.CreationID.String(), scope)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	name := "Skills-" + request.ProjectID.String()
	key, _ := f.ProjectLock(request.ProjectID.String())
	r := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
		if e := p.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); e != nil {
			return e
		}
		x, e := p.store.InTx(tx)
		if e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_project.projects(id,owner_user_id,name,normalized_name,description,lifecycle,version,created_at,updated_at,creation_id) VALUES($1,$2,$3,$4,'Skills PG fixture','active',1,$5,$5,$6)`, request.ProjectID.String(), owner.String(), name, "skills-"+request.ProjectID.String(), now, request.CreationID.String()); e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO agenteam_project.creations(id,project_id,owner_user_id,command_key,semantic_digest,request_name,request_description,state,initialization_key,version,created_at,updated_at,event_id) VALUES($1,$2,$3,$4,$5,$6,'Skills PG fixture','initializing',$7,1,$8,$8,$9)`, request.CreationID.String(), request.ProjectID.String(), owner.String(), "create-"+request.CreationID.String(), "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", name, string(request.InitializationKey), now, testID[struct{}](t).String())
		return e
	})
	requireCommitted(t, r)
	authority, e := skill.NewAuthority(p.store, p.projects)
	if e != nil {
		t.Fatal(e)
	}
	attempt, e := oc.NewUploadAttempt(oc.AttemptDetails{ID: testID[oc.Attempt](t), ObjectID: testID[oc.StoredObject](t), UploadID: testID[oc.Upload](t)})
	if e != nil {
		t.Fatal(e)
	}
	o := &controlledObjects{t: t, store: p.store, authority: authority, request: request, issuer: oc.NewAccessIssuer(), attempt: attempt}
	service := p.newService(t, authority, o)
	return &skillCase{request, actor, service, authority, o, owner}
}

type controlledProcesses struct{}

func (controlledProcesses) ConfirmStopped(context.Context, oc.ProcessID) error {
	return f.NewFault(f.ResourceBusy, f.NotStarted)
}
func (p *skillPG) newService(t *testing.T, a *skill.Authority, o *controlledObjects) *skill.Service {
	t.Helper()
	bundle, e := skill.AddSkills(testContext(t))
	if e != nil {
		t.Fatal(e)
	}
	s, e := skill.New(skill.Dependencies{Authority: a, Objects: o, Processes: controlledProcesses{}, ProcessID: testID[oc.Process](t), Bundle: bundle})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if e := s.Drain(ctx); e != nil {
			t.Error("Skill owner did not actually drain", e)
		}
	})
	return s
}

// These eight controlled methods model the external D05 boundary only. The
// embedded unused port deliberately has no fallback: an unexpected operation
// fails the test. Discovery and authorization still use the real Skill and
// Project authorities and the real caller's live PostgreSQL transaction.
type controlledObjects struct {
	skill.ObjectPorts
	t                 *testing.T
	store             skill.Store
	authority         *skill.Authority
	request           pc.InitializationRequest
	issuer            oc.AccessIssuer
	attempt           oc.UploadAttempt
	prepared          oc.PreparedPayload
	steps             []string
	rejectPublication bool
}

func (o *controlledObjects) PreparePayload(_ context.Context, _ id.Actor, _ oc.ObjectOwner, media string, size int64, digest *f.Digest, body io.ReadCloser) (oc.PreparedPayload, error) {
	o.steps = append(o.steps, "prepare")
	b, e := io.ReadAll(body)
	closeErr := body.Close()
	if e != nil {
		return oc.PreparedPayload{}, e
	}
	if closeErr != nil {
		return oc.PreparedPayload{}, closeErr
	}
	if digest == nil || *digest != skill.AddSkillsPackageSHA256 || media != sc.PackageMediaType || int64(len(b)) != size {
		o.t.Fatal("not original pinned package")
	}
	p, e := oc.NewPreparedPayload(oc.PreparedDetails{ID: testID[oc.Payload](o.t), MediaType: media, Length: size, SHA256: *digest})
	o.prepared = p
	return p, e
}
func (o *controlledObjects) DiscardPrepared(p oc.PreparedPayload) error {
	if p.Details() != o.prepared.Details() {
		o.t.Fatal("different prepared owner")
	}
	o.steps = append(o.steps, "discard")
	return nil
}
func (o *controlledObjects) DiscoverAccess(ctx context.Context, r oc.AccessRequest) (oc.AccessLockPlan, error) {
	deps, e := o.authority.Discover(ctx, r)
	if e != nil {
		return oc.AccessLockPlan{}, e
	}
	key, _ := f.AggregateLock(f.ObjectAggregate, o.attempt.Details().ObjectID.String())
	locks := append(deps.Locks(), f.LockRequest{Key: key, Mode: f.Exclusive})
	return oc.NewAccessLockPlan(o.issuer, oc.AccessPlanDetails{Request: r, DependencyRequest: r, Dependencies: deps, DomainBinding: deps.Mapping(), Objects: []oc.ObjectID{o.attempt.Details().ObjectID}, Locks: locks})
}
func (o *controlledObjects) AcquireAccessPlansInTx(ctx context.Context, tx f.Tx, plans []oc.AccessLockPlan, extra []f.LockRequest) (oc.LockedAccess, error) {
	locks := append([]f.LockRequest(nil), extra...)
	for _, plan := range plans {
		locks = append(locks, plan.Details().Locks...)
	}
	locks, e := oc.NormalizeAccessLocks(locks)
	if e != nil {
		return oc.LockedAccess{}, e
	}
	if e = o.store.AcquireAll(ctx, tx, locks); e != nil {
		return oc.LockedAccess{}, e
	}
	for _, plan := range plans {
		if e = o.authority.ValidateInTx(ctx, tx, plan.Details().Request, plan.Details().Dependencies); e != nil {
			return oc.LockedAccess{}, e
		}
	}
	return oc.NewLockedAccess(o.issuer, tx, plans, extra)
}
func (o *controlledObjects) ReserveUploadInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, command f.CommandMeta, p oc.PreparedPayload, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.UploadAttempt, error) {
	if !locked.Matches(o.issuer, tx, plan, plan.Details().Request) || command.IdempotencyKey != o.request.InitializationKey || p.Details() != o.prepared.Details() {
		o.t.Fatal("reservation changed original identity")
	}
	if _, e := o.authority.AuthorizeOwnerInTx(ctx, tx, actor, owner, id.Mutate); e != nil {
		return oc.UploadAttempt{}, e
	}
	o.steps = append(o.steps, "reserve")
	return o.attempt, nil
}
func (o *controlledObjects) UploadPrepared(ctx context.Context, _ id.Actor, _ oc.ObjectOwner, _ oc.PreparedPayload, a oc.UploadAttempt) (oc.UploadAttempt, error) {
	var phase, object, attempt string
	if e := o.store.QueryRow(ctx, `SELECT phase,object_id::text,current_attempt_id::text FROM agenteam_skill.initializations WHERE project_id=$1`, o.request.ProjectID.String()).Scan(&phase, &object, &attempt); e != nil {
		return oc.UploadAttempt{}, e
	}
	if phase != "reserved" || object != a.Details().ObjectID.String() || attempt != a.Details().ID.String() {
		o.t.Fatal("physical boundary before durable original reservation")
	}
	o.steps = append(o.steps, "physical")
	return a, nil
}
func (o *controlledObjects) PublishVerifiedInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, a oc.UploadAttempt, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.PutResult, error) {
	if !locked.Matches(o.issuer, tx, plan, plan.Details().Request) {
		o.t.Fatal("publication changed original transaction")
	}
	grant, e := o.authority.AuthorizeOwnerInTx(ctx, tx, actor, owner, id.Mutate)
	if e != nil {
		return oc.PutResult{}, e
	}
	if grant.Details().Existence != oc.ExistingOwner {
		o.t.Fatal("Skill not visible in original publishing transaction")
	}
	o.steps = append(o.steps, "publish")
	if o.rejectPublication {
		return oc.PutResult{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	x, e := o.store.InTx(tx)
	if e != nil {
		return oc.PutResult{}, e
	}
	var created time.Time
	if e = x.QueryRow(ctx, `SELECT created_at FROM agenteam_skill.initializations WHERE project_id=$1`, o.request.ProjectID.String()).Scan(&created); e != nil {
		return oc.PutResult{}, e
	}
	at, e := f.NewInstant(created)
	if e != nil {
		return oc.PutResult{}, e
	}
	scope, e := id.InProject(o.request.ProjectID)
	if e != nil {
		return oc.PutResult{}, e
	}
	d := o.prepared.Details()
	return oc.PutResult{Meta: oc.ObjectMeta{ID: a.Details().ObjectID, Scope: scope, MediaType: d.MediaType, ByteSize: f.Progress(d.Length), SHA256: d.SHA256, State: oc.Available, Version: 1, CreatedAt: at}}, nil
}
