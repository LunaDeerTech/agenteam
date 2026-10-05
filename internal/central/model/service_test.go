package model

import (
	"context"
	"errors"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Embedded nil methods panic if a supposedly pure constructor performs I/O.
type noIOStore struct{ Store }
type noIOSecret struct{ sc.UsageOperations }
type noIOAudit struct{ ac.Appender }
type noIOEvents struct{ oc.Appender }
type sessionFunc func(context.Context, f.Tx, id.Actor) error

func (fn sessionFunc) RequireCurrentSession(ctx context.Context, tx f.Tx, a id.Actor) error {
	return fn(ctx, tx, a)
}

type systemFunc func(context.Context, f.Tx, id.Actor, id.AccessIntent) (id.AccessGrant, error)

func (fn systemFunc) AuthorizeSystem(ctx context.Context, tx f.Tx, a id.Actor, i id.AccessIntent) (id.AccessGrant, error) {
	return fn(ctx, tx, a, i)
}
func mustID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, e := f.NewID[T]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func testActor(t *testing.T) id.Actor {
	t.Helper()
	v, e := id.NewHuman(mustID[id.User](t), mustID[id.Session](t))
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func testKeys(t *testing.T) cursor.Keyring {
	t.Helper()
	k, e := cursor.LoadKeyring(`{"format":1,"current_kid":"model-test","keys":[{"kid":"model-test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if e != nil {
		t.Fatal(e)
	}
	return k
}
func requireCode(t *testing.T, e error, code f.Code) {
	t.Helper()
	var ff *f.Fault
	if !errors.As(e, &ff) || ff.Code != code {
		t.Fatalf("code got %v, want %s", e, code)
	}
}
func pureAuthority(t *testing.T, store Store) *Authority {
	t.Helper()
	a, e := NewAuthority(store, Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error { panic("unexpected Session I/O") }), System: systemFunc(func(context.Context, f.Tx, id.Actor, id.AccessIntent) (id.AccessGrant, error) {
		panic("unexpected System I/O")
	})})
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func testDependencies(t *testing.T) Dependencies {
	t.Helper()
	events, e := DefineEvents(ec.NewCatalog())
	if e != nil {
		t.Fatal(e)
	}
	return Dependencies{Secret: &noIOSecret{}, Audit: &noIOAudit{}, Events: &noIOEvents{}, ConfigurationEvents: events, Cursors: testKeys(t)}
}

func TestModelConstructorsArePureAndRejectMissingOwnership(t *testing.T) {
	store := &noIOStore{}
	a := pureAuthority(t, store)
	d := testDependencies(t)
	if _, e := New(store, a, d); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*Dependencies){func(d *Dependencies) { d.Secret = nil }, func(d *Dependencies) { d.Audit = nil }, func(d *Dependencies) { d.Events = nil }, func(d *Dependencies) { d.ConfigurationEvents = ModelEvents{} }, func(d *Dependencies) { d.Cursors = cursor.Keyring{} }} {
		copy := d
		change(&copy)
		if _, e := New(store, a, copy); e == nil {
			t.Fatal("missing dependency accepted")
		}
	}
	if _, e := New(&noIOStore{}, a, d); e == nil {
		t.Fatal("different Store accepted")
	}
	if _, e := New(store, &Authority{}, d); e == nil {
		t.Fatal("zero authority accepted")
	}
	var typedNil *noIOSecret
	d.Secret = typedNil
	if _, e := New(store, a, d); e == nil {
		t.Fatal("typed nil accepted")
	}
	if _, e := NewAuthority(store, Authorizations{}); e == nil {
		t.Fatal("missing identity binding accepted")
	}
}

func validGrant(actor id.Actor, intent id.AccessIntent) (id.AccessGrant, error) {
	now, _ := f.NewInstant(time.Now())
	return id.NewAccessGrant(actor, id.SystemScope(), intent, now, 1)
}
