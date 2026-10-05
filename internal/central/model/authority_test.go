package model

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestModelCurrentAuthorityRequiresHeldUserAndActualSessionBeforeGrant(t *testing.T) {
	actor := testActor(t)
	store := &confirmationStore{held: true}
	order := []string{}
	a, e := NewAuthority(store, Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error {
		order = append(order, "session")
		return fault(f.SessionRevoked)
	}), System: systemFunc(func(context.Context, f.Tx, id.Actor, id.AccessIntent) (id.AccessGrant, error) {
		t.Fatal("revoked Session reached System authorization")
		return id.AccessGrant{}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	requireCode(t, a.current(context.Background(), f.NewTx(), actor, id.Mutate), f.SessionRevoked)
	if len(order) != 1 {
		t.Fatal("current Session not checked")
	}
	a, e = NewAuthority(store, Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error { return nil }), System: systemFunc(func(context.Context, f.Tx, id.Actor, id.AccessIntent) (id.AccessGrant, error) {
		return id.AccessGrant{}, nil
	})})
	if e != nil {
		t.Fatal(e)
	}
	requireCode(t, a.current(context.Background(), f.NewTx(), actor, id.Read), f.Forbidden)
	a, e = NewAuthority(store, Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error { return nil }), System: systemFunc(func(_ context.Context, _ f.Tx, _ id.Actor, i id.AccessIntent) (id.AccessGrant, error) {
		return validGrant(testActor(t), i)
	})})
	if e != nil {
		t.Fatal(e)
	}
	requireCode(t, a.current(context.Background(), f.NewTx(), actor, id.Read), f.Forbidden)
}
