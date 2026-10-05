package audit

import (
	"context"
	"errors"
	"reflect"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type modelAppendVerifier struct {
	t     *testing.T
	tx    f.Tx
	entry c.Entry
	key   c.AppendKey
	calls int
	err   error
}

func (v *modelAppendVerifier) CheckAppendInTx(_ context.Context, tx f.Tx, entry c.Entry, key c.AppendKey) error {
	v.t.Helper()
	if tx != v.tx || !reflect.DeepEqual(entry.Fields().Actor.Details(), v.entry.Fields().Actor.Details()) || entry.Fields().Action != v.entry.Fields().Action || key.Details() != v.key.Details() {
		v.t.Fatal("Model dispatcher changed exact request")
	}
	v.calls++
	return v.err
}
func TestModelAuditDispatchRequiresModelFactAuthority(t *testing.T) {
	user, _ := f.ParseID[id.User]("01900000-0000-7000-8000-000000000001")
	session, _ := f.ParseID[id.Session]("01900000-0000-7000-8000-000000000002")
	actor, _ := id.NewHuman(user, session)
	resource, _ := c.NewResource(c.ModelProviderResource, user.String())
	metadata, e := c.ModelMetadata(c.ProviderCreate, c.ModelMetadataFields{ProviderID: user.String(), Version: 1, ChangedFields: []string{"created"}})
	if e != nil {
		t.Fatal(e)
	}
	entry, e := c.NewEntry(c.EntryFields{Scope: id.SystemScope(), Actor: actor, Action: c.ProviderCreate, Outcome: c.Success, Resource: resource, Metadata: metadata})
	if e != nil {
		t.Fatal(e)
	}
	key, _ := c.NewAppendKey(c.ModelProducer, user.String(), 0)
	tx := f.NewTx()
	service := &Service{auth: Authorizations{Sessions: sessionPort(func(context.Context, f.Tx, id.Actor) error {
		t.Fatal("generic Session grant used instead of Model fact authority")
		return nil
	})}}
	if e = service.authorizeAppend(context.Background(), tx, entry, key); e == nil {
		t.Fatal("unbound Model fact authority accepted")
	}
	denied := f.NewFault(f.Forbidden, f.NotStarted)
	verifier := &modelAppendVerifier{t: t, tx: tx, entry: entry, key: key, err: denied}
	service.auth.Models = verifier
	if e = service.authorizeAppend(context.Background(), tx, entry, key); !errors.Is(e, denied) || verifier.calls != 1 {
		t.Fatal("Model authority error not preserved", e)
	}
	verifier.err = nil
	if e = service.authorizeAppend(context.Background(), tx, entry, key); e != nil || verifier.calls != 2 {
		t.Fatal(e)
	}
	wrong, _ := c.NewAppendKey(c.SecretProducer, user.String(), 0)
	if e = service.authorizeAppend(context.Background(), tx, entry, wrong); e == nil || verifier.calls != 2 {
		t.Fatal("cross-producer key reached verifier")
	}
}
