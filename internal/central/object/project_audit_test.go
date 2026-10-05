package object

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type auditTestStore struct {
	Store
	tx      foundation.Tx
	queries int
	row     func(string, ...any) postgres.Row
}

func (s *auditTestStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || !tx.Valid() {
		return nil, errors.New("closed or foreign transaction")
	}
	return s, nil
}
func (s *auditTestStore) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	s.queries++
	return s.row(q, args...)
}

type auditUncomparableStore struct {
	Store
	values []string
}
type auditDynamicStore struct {
	Store
	value any
}
type auditTestRow func(...any) error

func (r auditTestRow) Scan(v ...any) error { return r(v...) }
func auditValues(values ...any) postgres.Row {
	return auditTestRow(func(out ...any) error {
		if len(out) != len(values) {
			return errors.New("wrong scan count")
		}
		for i, v := range values {
			d := reflect.ValueOf(out[i]).Elem()
			if v == nil {
				d.SetZero()
			} else {
				value := reflect.ValueOf(v)
				if !value.Type().ConvertibleTo(d.Type()) {
					return errors.New("wrong scan type")
				}
				d.Set(value.Convert(d.Type()))
			}
		}
		return nil
	})
}
func auditID[K any](t *testing.T) foundation.ID[K] {
	t.Helper()
	v, e := foundation.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func auditCode(t *testing.T, err error, want foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}
func TestObjectProjectAuditConstructionHasNoIOAndChecksDynamicIdentity(t *testing.T) {
	var typed *auditTestStore
	for _, s := range []Store{nil, typed} {
		_, err := NewProjectAuditAuthority(s)
		auditCode(t, err, foundation.DependencyUnbound)
	}
	for _, s := range []Store{auditUncomparableStore{}, auditDynamicStore{value: []string{"not-comparable"}}} {
		_, err := NewProjectAuditAuthority(s)
		auditCode(t, err, foundation.InvalidArgument)
	}
	for _, s := range []Store{&auditTestStore{}, auditDynamicStore{value: "comparable"}} {
		if _, err := NewProjectAuditAuthority(s); err != nil {
			t.Fatal(err)
		}
	}
	var zero ProjectAuditAuthority
	auditCode(t, zero.CheckProjectAuditInTx(context.Background(), foundation.Tx{}, ac.Entry{}, ac.AppendKey{}), foundation.DependencyUnbound)
	var nilChecker *ProjectAuditAuthority
	auditCode(t, nilChecker.CheckProjectAuditInTx(context.Background(), foundation.Tx{}, ac.Entry{}, ac.AppendKey{}), foundation.DependencyUnbound)
}

type auditPublishFixture struct {
	store   *auditTestStore
	checker *ProjectAuditAuthority
	w       projectAuditWitness
	u       uploadRow
	a       attemptRow
	o       objectRow
}

func newAuditPublishFixture(t *testing.T) *auditPublishFixture {
	t.Helper()
	p := auditID[identity.Project](t)
	scope, _ := identity.InProject(p)
	owner, _ := oc.NewObjectOwner(oc.Artifact, auditID[struct{}](t).String(), p.String())
	actor, _ := identity.NewHuman(auditID[identity.User](t), auditID[identity.Session](t))
	at, _ := foundation.NewInstant(time.Now())
	v := &auditPublishFixture{store: &auditTestStore{tx: foundation.NewTx()}}
	v.u = uploadRow{id: auditID[oc.Upload](t), object: auditID[oc.StoredObject](t), receipt: auditID[oc.Receipt](t), owner: owner, actor: stableActor(actor), state: "pending", disposition: "attached", existence: oc.ExistingOwner, initiator: identity.Human, initiatorID: actor.Details().UserID}
	v.a = attemptRow{id: auditID[oc.Attempt](t), upload: v.u.id, object: v.u.object, process: auditID[oc.Process](t), phase: "verified", size: 4, digest: foundation.Digest("sha256:" + strings.Repeat("a", 64)), closed: true, kind: "private_candidate"}
	v.u.attempt = v.a.id
	v.o = objectRow{meta: oc.ObjectMeta{ID: v.u.object, Scope: scope, MediaType: "text/plain; charset=utf-8", ByteSize: 4, SHA256: v.a.digest, State: oc.Pending, Version: 1, CreatedAt: at}, partition: p.String()}
	key, _ := ac.NewAppendKey(ac.ObjectProducer, v.u.id.String(), 0)
	metadata, _ := ac.ObjectMetadata(ac.ObjectUploadComplete, ac.ObjectMetadataFields{ObjectID: v.u.object.String(), InitiatorKind: identity.Human, InitiatorID: v.u.initiatorID, MediaType: "text/plain", ByteSize: 4, Phase: ac.PublishedPhase})
	entry, err := auditExpectedEntry(scope, key, ac.ObjectUploadComplete, ac.Success, ac.ObjectResource, v.u.object.String(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	pk, _ := foundation.ProjectLock(p.String())
	ok, _ := foundation.AggregateLock(foundation.ObjectAggregate, v.u.object.String())
	v.w = projectAuditWitness{stage: projectAuditStage{store: v.store, tx: v.store.tx, kind: auditPublish, actor: actor, owner: owner, object: v.u.object, upload: v.u.id, attempt: v.a.id, locks: []foundation.LockRequest{{Key: pk, Mode: foundation.Shared}, {Key: ok, Mode: foundation.Exclusive}}}, entry: entry, key: key, upload: v.u.id, meta: v.o.meta}
	v.store.row = func(q string, args ...any) postgres.Row {
		switch {
		case strings.Contains(q, "FROM agenteam_object.uploads"):
			u := v.u
			d := u.owner.Details()
			return auditValues(u.id.String(), u.object.String(), u.receipt.String(), u.attempt.String(), d.Kind, d.ID, d.ProjectID, string(u.command), u.digest, u.actor, u.state, u.disposition, u.creation, u.existence, u.initiator, u.initiatorID, u.executionID, u.expected)
		case strings.Contains(q, "FROM agenteam_object.objects"):
			o := v.o
			return auditValues(o.meta.ID.String(), "project", o.partition, o.meta.Scope.Details().ProjectID, o.meta.MediaType, int64(o.meta.ByteSize), digestBytes(o.meta.SHA256), o.meta.State, int64(o.meta.Version), o.key, o.cleaning, o.meta.CreatedAt.Time())
		case strings.Contains(q, "FROM agenteam_object.upload_attempts"):
			a := v.a
			return auditValues(a.id.String(), a.upload.String(), a.object.String(), a.process.String(), a.key, a.phase, a.ordinal, a.size, digestBytes(a.digest), a.late, a.closed, a.cleaning, a.kind, "")
		case strings.Contains(q, "FROM agenteam_object.project_work"):
			return auditValues(false)
		default:
			t.Fatalf("checker performed unexpected SQL: %s", q)
			return nil
		}
	}
	v.checker, err = NewProjectAuditAuthority(v.store)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (v *auditPublishFixture) check(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	return v.checker.CheckProjectAuditInTx(ctx, tx, entry, key)
}
func (v *auditPublishFixture) context() context.Context {
	return context.WithValue(context.Background(), projectAuditWitnessKey{}, v.w)
}
func TestObjectProjectAuditPublicationReadsCanonicalPrestate(t *testing.T) {
	for _, name := range []string{"valid-repeat", "wrong-owner", "wrong-attempt", "wrong-digest", "not-verified", "not-closed", "revoked", "cleaning", "published-already", "wrong-process-witness"} {
		t.Run(name, func(t *testing.T) {
			v := newAuditPublishFixture(t)
			switch name {
			case "wrong-owner":
				v.u.owner, _ = oc.NewObjectOwner(oc.Artifact, auditID[struct{}](t).String(), v.o.meta.Scope.Details().ProjectID)
			case "wrong-attempt":
				v.u.attempt = auditID[oc.Attempt](t)
			case "wrong-digest":
				v.a.digest = foundation.Digest("sha256:" + strings.Repeat("b", 64))
			case "not-verified":
				v.a.phase = "unknown"
			case "not-closed":
				v.a.closed = false
			case "revoked":
				v.u.disposition = "revoked"
			case "cleaning":
				v.o.cleaning = true
			case "published-already":
				v.u.state = "committed"
			case "wrong-process-witness":
				v.w.stage.kind = auditWriterFailure
			}
			err := v.check(v.context(), v.store.tx, v.w.entry, v.w.key)
			if name == "valid-repeat" {
				if err != nil {
					t.Fatal(err)
				}
				if err = v.check(v.context(), v.store.tx, v.w.entry, v.w.key); err != nil {
					t.Fatal(err)
				}
			} else {
				auditCode(t, err, foundation.Forbidden)
			}
		})
	}
}
