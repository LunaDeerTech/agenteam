//go:build integration

package outbox_test

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objectcontract "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	"github.com/minio/minio-go/v7"
)

type countedStore struct {
	*postgres.Store
	acquisitions atomic.Int64
}

func (s *countedStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.acquisitions.Add(1)
	return s.Store.AcquireAll(ctx, tx, locks)
}

type objectAuthority struct {
	f     *fixture
	owner objectcontract.ObjectOwner
}

func (a *objectAuthority) authorize(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, owner objectcontract.ObjectOwner, intent identity.AccessIntent) (objectcontract.OwnerAuthorization, error) {
	if !owner.Equal(a.owner) || actor.Details().Kind != identity.Human || actor.Details().UserID != a.f.actor.Details().UserID {
		return objectcontract.OwnerAuthorization{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	var visible, active bool
	if err := x.QueryRow(ctx, `SELECT visible,active FROM outbox_fixture.authority WHERE id=$1`, owner.Details().ProjectID).Scan(&visible, &active); err != nil {
		return objectcontract.OwnerAuthorization{}, err
	}
	if !visible || !active {
		return objectcontract.OwnerAuthorization{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return objectcontract.NewOwnerAuthorization(objectcontract.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: objectcontract.ExistingOwner, Version: 1})
}
func (a *objectAuthority) AuthorizeOwner(ctx context.Context, actor identity.Actor, owner objectcontract.ObjectOwner, intent identity.AccessIntent) (objectcontract.OwnerAuthorization, error) {
	return a.authorize(ctx, a.f.store, actor, owner, intent)
}
func (a *objectAuthority) AuthorizeOwnerInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner objectcontract.ObjectOwner, intent identity.AccessIntent) (objectcontract.OwnerAuthorization, error) {
	x, e := a.f.store.InTx(tx)
	if e != nil {
		return objectcontract.OwnerAuthorization{}, e
	}
	return a.authorize(ctx, x, actor, owner, intent)
}
func (a *objectAuthority) CheckInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner objectcontract.ObjectOwner, intent identity.AccessIntent) error {
	_, e := a.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
	return e
}
func (a *objectAuthority) discover(ctx context.Context, x postgres.SQLExecutor, request objectcontract.AccessRequest) (objectcontract.AccessDependencies, error) {
	d := request.Details()
	if !d.Owner.Equal(a.owner) || !d.Actor.Equal(a.f.actor) {
		return objectcontract.AccessDependencies{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	var parent string
	if err := x.QueryRow(ctx, `SELECT parent_id::text FROM outbox_fixture.authority WHERE id=$1`, d.Owner.Details().ProjectID).Scan(&parent); err != nil {
		return objectcontract.AccessDependencies{}, err
	}
	key, _ := foundation.AggregateLock(foundation.ExecutionAggregate, parent)
	// Test-only parent mapping, from actual persisted fixture authority.
	raw, _ := json.Marshal(struct{ Parent, Owner, User string }{parent, d.Owner.Details().ID, d.Actor.Details().UserID})
	return objectcontract.NewAccessDependencies(digestBytes(raw), []foundation.LockRequest{{Key: key, Mode: foundation.Shared}})
}
func (a *objectAuthority) Discover(ctx context.Context, r objectcontract.AccessRequest) (objectcontract.AccessDependencies, error) {
	return a.discover(ctx, a.f.store, r)
}
func (a *objectAuthority) ValidateInTx(ctx context.Context, tx foundation.Tx, r objectcontract.AccessRequest, d objectcontract.AccessDependencies) error {
	x, e := a.f.store.InTx(tx)
	if e != nil {
		return e
	}
	current, e := a.discover(ctx, x, r)
	if e != nil {
		return e
	}
	if !current.Equal(d) {
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
	}
	return a.f.store.RequireHeldLocks(ctx, tx, d.Locks())
}
func digestBytes(raw []byte) foundation.Digest { return oc.DigestBytes(raw) }

func TestOutboxComposesRealObjectReservationWithOneCompleteAcquire(t *testing.T) {
	f := newFixture(t)
	remote, err := objectfixture.Load()
	if err != nil {
		t.Fatal(err)
	}
	s3, transport, err := remote.Client()
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	bucket := "d06-" + remote.Nonce[:12] + "-" + strings.ReplaceAll(id[struct{}](t).String(), "-", "")[:20]
	if err = s3.MakeBucket(ctxFor(t), bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal("owned bucket creation failed")
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	cfg, err := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := object.NewBackend(cfg)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), id[objectcontract.Process](t))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(f.store, keys, audit.Authorizations{})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := objectcontract.NewObjectOwner(objectcontract.Artifact, id[struct{}](t).String(), f.project.String())
	if err != nil {
		t.Fatal(err)
	}
	auth := &objectAuthority{f, owner}
	counted := &countedStore{Store: f.store}
	svc, err := object.New(counted, backend, spool, auditor, object.Authorizations{Planner: auth, Resources: auth, Gate: auth})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := svc.Force(ctx); err != nil {
			t.Error(err)
		}
	})
	if err = svc.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	prepared, err := svc.PreparePayload(ctxFor(t), f.actor, owner, "text/plain", 4, nil, io.NopCloser(strings.NewReader("body")))
	if err != nil {
		t.Fatal(err)
	}
	defer svc.DiscardPrepared(prepared)
	command := foundation.CommandMeta{RequestID: id[foundation.Request](t), IdempotencyKey: "outbox-composition"}
	request, err := objectcontract.NewOwnerAccess(objectcontract.AccessRequestDetails{Operation: objectcontract.ReserveAccess, Actor: f.actor, Owner: owner, Intent: identity.Mutate, Command: &command, Prepared: prepared})
	if err != nil {
		t.Fatal(err)
	}
	objectPlan, err := svc.DiscoverAccess(ctxFor(t), request)
	if err != nil {
		t.Fatal(err)
	}
	e := f.event(t, true, 1, "object-reserved")
	appendPlan, err := f.svc.PrepareAppend(ctxFor(t), f.actor, e)
	if err != nil {
		t.Fatal(err)
	}
	project, _ := foundation.ProjectLock(f.project.String())
	extra := append(appendPlan.Locks(), foundation.LockRequest{Key: project, Mode: foundation.Exclusive})
	for _, rollback := range []bool{true, false} {
		before := counted.acquisitions.Load()
		commit := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			locked, err := svc.AcquireAccessPlansInTx(ctx, tx, []objectcontract.AccessLockPlan{objectPlan}, extra)
			if err != nil {
				return err
			}
			if err = f.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: project, Mode: foundation.Exclusive}}); err != nil {
				return err
			}
			if _, err = svc.ReserveUploadInTx(ctx, tx, f.actor, owner, command, prepared, objectPlan, locked); err != nil {
				return err
			}
			if _, err = f.svc.AppendEventInTx(ctx, tx, f.actor, e, appendPlan); err != nil {
				return err
			}
			if rollback {
				return foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
			}
			return nil
		})
		want := foundation.Committed
		rows := int64(1)
		if rollback {
			want = foundation.NotCommitted
			rows = 0
		}
		state(t, commit, want)
		if counted.acquisitions.Load()-before != 1 {
			t.Fatal("composition performed a second AcquireAll")
		}
		if f.count(t, `SELECT count(*) FROM agenteam_object.uploads`) != rows || f.count(t, `SELECT count(*) FROM agenteam_outbox.events`) != rows {
			t.Fatal("Object reservation/event not atomic")
		}
	}
}
