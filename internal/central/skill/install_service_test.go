package skill

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// The controlled transaction/transport below checks the public call's order,
// original receipt and failure behavior. It is not SQL or D05 authorization.
type installCallStore struct{ *installedReadStore }

func (s *installCallStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.installedReadStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *installCallStore) WithinTx(ctx context.Context, cause f.TransactionCause, work func(context.Context, f.Tx) error) f.CommitResult {
	before := append([]any(nil), s.installationValues...)
	result := s.installedReadStore.WithinTx(ctx, cause, work)
	if result.State() == f.NotCommitted {
		s.installationValues = before
	}
	return result
}
func (s *installCallStore) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !s.live {
		return pgconn.CommandTag{}, errors.New("SQL without original Tx")
	}
	switch {
	case strings.HasPrefix(q, "INSERT INTO agenteam_skill.installation_attempts"):
		if len(args) != 11 || args[2] != s.installed.id.String() || args[7] != stateID[oc.Process](50).String() {
			return pgconn.CommandTag{}, invalid()
		}
		if s.installed.execution == nil {
			if args[8] != string(id.Human) || args[9] != nil || args[10] != nil {
				return pgconn.CommandTag{}, invalid()
			}
		} else {
			call, _ := ctx.Value(installExecutionKey{}).(*installExecutionCall)
			if call == nil || args[8] != string(id.AgentRun) || args[9] != call.binding.AttemptID || args[10] != call.read.RequestID.String() {
				return pgconn.CommandTag{}, invalid()
			}
		}
	case strings.HasPrefix(q, "UPDATE agenteam_skill.installations SET phase='reserved'"):
		if s.installationValues[14] != "planned" || s.installationValues[15] != args[5] {
			return pgconn.CommandTag{}, invalid()
		}
		s.installationValues[14] = "reserved"
		s.installationValues[15] = s.installationValues[15].(int64) + 1
		s.installationValues[16], s.installationValues[17], s.installationValues[18] = args[2], args[3], args[4]
	case strings.HasPrefix(q, "UPDATE agenteam_skill.installations SET phase='published'"):
		if !s.skillInserted || !s.revisionInserted || s.installationValues[14] != "reserved" || s.installationValues[15] != args[2] {
			return pgconn.CommandTag{}, invalid()
		}
		s.installationValues[14] = "published"
		s.installationValues[15] = s.installationValues[15].(int64) + 1
	default:
		return s.initializationWriteStore.Exec(ctx, q, args...)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type installCallProjects struct{ *skillReaderProjects }

func (p *installCallProjects) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) (pc.ProjectAccess, error) {
	if intent != id.Read && intent != id.Mutate {
		return pc.ProjectAccess{}, invalid()
	}
	return p.skillReaderProjects.RequireOwnerInTx(ctx, tx, actor, project, id.Read)
}

type installCallObjects struct {
	ObjectPorts
	t          *testing.T
	store      *installCallStore
	actor      id.Actor
	row        installationRow
	issuer     oc.AccessIssuer
	steps      []string
	discardErr error
}

func (o *installCallObjects) PreparePayload(_ context.Context, a id.Actor, owner oc.ObjectOwner, media string, size int64, digest *f.Digest, body io.ReadCloser) (oc.PreparedPayload, error) {
	o.steps = append(o.steps, "prepare")
	raw, e := io.ReadAll(body)
	closeErr := body.Close()
	want, _ := o.row.owner()
	if o.store.live || !a.Equal(o.actor) || !owner.Equal(want) || e != nil || closeErr != nil || media != sc.PackageMediaType || int64(len(raw)) != size || sum(raw) != *digest {
		o.t.Fatal("preparation did not consume original package outside Tx")
	}
	return oc.NewPreparedPayload(oc.PreparedDetails{ID: stateID[oc.Payload](630), MediaType: media, Length: size, SHA256: *digest})
}
func (o *installCallObjects) DiscardPrepared(oc.PreparedPayload) error {
	o.steps = append(o.steps, "discard")
	return o.discardErr
}
func (o *installCallObjects) DiscoverAccess(_ context.Context, r oc.AccessRequest) (oc.AccessLockPlan, error) {
	if o.store.live {
		o.t.Fatal("access discovery under Tx")
	}
	locks, e := o.row.locks(f.Exclusive)
	if e != nil {
		return oc.AccessLockPlan{}, e
	}
	deps, e := oc.NewAccessDependencies(o.row.semantic, locks)
	if e != nil {
		return oc.AccessLockPlan{}, e
	}
	return oc.NewAccessLockPlan(o.issuer, oc.AccessPlanDetails{Request: r, DependencyRequest: r, Dependencies: deps, DomainBinding: o.row.semantic, Objects: []oc.ObjectID{o.row.object}, Locks: locks})
}
func (o *installCallObjects) AcquireAccessPlansInTx(ctx context.Context, tx f.Tx, plans []oc.AccessLockPlan, extra []f.LockRequest) (oc.LockedAccess, error) {
	union := append([]f.LockRequest(nil), extra...)
	for _, p := range plans {
		union = append(union, p.Details().Locks...)
	}
	locks, e := oc.NormalizeAccessLocks(union)
	if e != nil {
		return oc.LockedAccess{}, e
	}
	if e = o.store.AcquireAll(ctx, tx, locks); e != nil {
		return oc.LockedAccess{}, e
	}
	return oc.NewLockedAccess(o.issuer, tx, plans, extra)
}
func (o *installCallObjects) ReserveUploadInTx(_ context.Context, tx f.Tx, a id.Actor, owner oc.ObjectOwner, meta f.CommandMeta, _ oc.PreparedPayload, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.UploadAttempt, error) {
	o.steps = append(o.steps, "reserve")
	want, _ := o.row.owner()
	if !o.store.live || !a.Equal(o.actor) || !owner.Equal(want) || meta.IdempotencyKey != o.row.key || !locked.Matches(o.issuer, tx, plan, plan.Details().Request) {
		o.t.Fatal("reservation lost original command or locked Tx")
	}
	return oc.NewUploadAttempt(oc.AttemptDetails{ID: o.row.attempt, ObjectID: o.row.object, UploadID: o.row.upload})
}
func (o *installCallObjects) UploadPrepared(_ context.Context, _ id.Actor, _ oc.ObjectOwner, _ oc.PreparedPayload, a oc.UploadAttempt) (oc.UploadAttempt, error) {
	o.steps = append(o.steps, "upload")
	if o.store.live || o.store.installationValues[14] != "reserved" || o.store.txs != 3 {
		o.t.Fatal("upload before known reservation commit")
	}
	return a, nil
}
func (o *installCallObjects) PublishVerifiedInTx(_ context.Context, tx f.Tx, _ id.Actor, _ oc.ObjectOwner, a oc.UploadAttempt, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.PutResult, error) {
	o.steps = append(o.steps, "publish")
	if !o.store.live || !o.store.skillInserted || !locked.Matches(o.issuer, tx, plan, plan.Details().Request) {
		o.t.Fatal("canonical publication not in original Tx")
	}
	scope, _ := id.InProject(o.row.project)
	return oc.PutResult{Meta: oc.ObjectMeta{ID: a.Details().ObjectID, Scope: scope, MediaType: sc.PackageMediaType, ByteSize: o.row.pkg.size, SHA256: o.row.pkg.packageDigest, State: oc.Available, Version: 1, CreatedAt: o.row.created}}, nil
}
func publicInstallFixture(t *testing.T) (*Service, *installCallStore, *installCallObjects, id.Actor, installationRow, InstallRequest) {
	t.Helper()
	s, read, p, actor, row := installedReadFixture(t)
	store := &installCallStore{read}
	s.state().authority.state().store = store
	s.state().authority.state().projects = &installCallProjects{p}
	objects := &installCallObjects{t: t, store: store, actor: actor, row: row, issuer: oc.NewAccessIssuer()}
	s.state().objects = objects
	request, e := NewInstallRequest(context.Background(), row.skill, installTestPackage(t, "Example", "repository-private-canary"))
	if e != nil {
		t.Fatal(e)
	}
	return s, store, objects, actor, row, request
}
func TestPublicInstallOriginalCommitAndLookup(t *testing.T) {
	for _, name := range []string{"publish", "completed-replay", "reserve-unknown", "publish-unknown", "discard-failed"} {
		t.Run(name, func(t *testing.T) {
			s, store, o, actor, row, request := publicInstallFixture(t)
			if name != "completed-replay" {
				store.installationValues[14] = "planned"
				store.installationValues[16] = ""
				store.installationValues[17] = ""
				store.installationValues[18] = ""
			}
			switch name {
			case "reserve-unknown":
				store.unknownAt = 3
			case "publish-unknown":
				store.unknownAt = 4
			case "discard-failed":
				o.discardErr = errors.New("private-discard-failure")
			}
			meta := f.CommandMeta{RequestID: stateID[f.Request](631), IdempotencyKey: row.key}
			receipt, err := s.Install(context.Background(), actor, meta, row.project, request)
			switch name {
			case "publish", "completed-replay":
				if err != nil || receipt.Validate() != nil || receipt.SkillID != row.skill || receipt.ObjectID != row.object {
					t.Fatal("public committed receipt missing", err)
				}
			default:
				if err == nil || receipt != (InstallReceipt{}) {
					t.Fatal("failure exposed receipt")
				}
			}
			trace := strings.Join(o.steps, ",")
			want := "prepare,reserve,upload,publish,discard"
			if name == "completed-replay" {
				want = ""
			}
			if name == "reserve-unknown" {
				want = "prepare,reserve,discard"
			}
			if trace != want {
				t.Fatal("unexpected physical order", trace)
			}
			if name == "reserve-unknown" || name == "publish-unknown" {
				if result, ok := UnknownAttempt(err); !ok || result.AttemptID() != stateID[f.TransactionAttempt](70) {
					t.Fatal("original Unknown provenance lost")
				}
			}
			before := len(o.steps)
			observed, lookupErr := s.LookupInstall(context.Background(), actor, row.project, row.key, request)
			if name == "reserve-unknown" {
				var known *f.Fault
				if !errors.As(lookupErr, &known) || known.Code != f.ResourceBusy || observed != (InstallReceipt{}) {
					t.Fatal("reserved command became receipt or fake Unknown", lookupErr)
				}
				_, retryErr := s.Install(context.Background(), actor, meta, row.project, request)
				if !errors.As(retryErr, &known) || known.Code != f.ResourceBusy {
					t.Fatal("reserved same-key call must not resend", retryErr)
				}
			} else if lookupErr != nil || observed.Validate() != nil || observed.InstallationID != row.id {
				t.Fatal("same-key committed recovery missing", lookupErr)
			}
			if len(o.steps) != before {
				t.Fatal("lookup/replay performed new Object work")
			}
			if name == "discard-failed" {
				s.Stop()
				if s.Joined() || len(s.state().work) != 1 {
					t.Fatal("failed Discard retired work")
				}
				o.discardErr = nil
				if err = s.Drain(context.Background()); err != nil || !s.Joined() {
					t.Fatal("original retained Discard did not drain", err)
				}
			}
		})
	}
}
func TestInstallationDiscardCannotRetireWhileOriginalCallHeld(t *testing.T) {
	s, _, _, _, row, _ := publicInstallFixture(t)
	call, e := s.beginProjectWork(context.Background(), row.project, installationWork)
	if e != nil {
		t.Fatal(e)
	}
	work, e := s.newInstallationOwnedWork(row, installationWork, call)
	if e != nil {
		t.Fatal(e)
	}
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	s.state().mu.Lock()
	work.installationCallerReturned = true
	work.installationDiscard = func() error { close(started); <-release; return nil }
	s.state().mu.Unlock()
	go func() { done <- s.joinInstallationDiscard(work) }()
	<-started
	s.Stop()
	if s.Joined() {
		t.Fatal("cancellation joined held original Discard")
	}
	if err := s.joinInstallationDiscard(work); err == nil {
		t.Fatal("concurrent Discard fabricated completion")
	}
	s.state().mu.Lock()
	returned := work.returned
	s.state().mu.Unlock()
	if returned {
		t.Fatal("work returned before physical Discard")
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	s.state().mu.Lock()
	returned = work.returned
	s.state().mu.Unlock()
	if !returned {
		t.Fatal("actual Discard return not retained")
	}
	// No durable row was registered by this controlled lifetime test.
	s.forgetOwnedWork(work)
	s.end(call)
	if !s.Joined() {
		t.Fatal("controlled original call not joined")
	}
}
