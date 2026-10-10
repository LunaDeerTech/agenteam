package audit

import (
	"context"
	"errors"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type denyRunnerAuthority struct {
	called bool
	tx     f.Tx
	entry  c.Entry
	key    c.AppendKey
}

func (a *denyRunnerAuthority) CheckAppendInTx(_ context.Context, tx f.Tx, entry c.Entry, key c.AppendKey) error {
	a.called = true
	a.tx = tx
	a.entry = entry
	a.key = key
	return f.NewFault(f.Forbidden, f.NotStarted)
}
func TestRunnerAppendRequiresExactRunnerAuthority(t *testing.T) {
	target, _ := f.NewID[struct{}]()
	command, _ := f.NewID[struct{}]()
	user, _ := f.NewID[id.User]()
	session, _ := f.NewID[id.Session]()
	actor, _ := id.NewHuman(user, session)
	resource, _ := c.NewResource(c.RunnerResource, target.String())
	metadata, e := c.RunnerMetadata(c.RunnerCreate, c.RunnerMetadataFields{RunnerID: target.String(), Version: 1, CredentialGeneration: 1, ChangedFields: []string{"created"}})
	if e != nil {
		t.Fatal(e)
	}
	entry, e := c.NewEntry(c.EntryFields{Scope: id.SystemScope(), Actor: actor, Action: c.RunnerCreate, Outcome: c.Success, Resource: resource, Metadata: metadata, Associations: c.Associations{RunnerID: target.String()}})
	if e != nil {
		t.Fatal(e)
	}
	key, _ := c.NewAppendKey(c.RunnerProducer, command.String(), 0)
	tx := f.NewTx()
	svc := &Service{}
	e = svc.authorizeAppend(context.Background(), tx, entry, key)
	var ff *f.Fault
	if !errors.As(e, &ff) || ff.Code != f.DependencyUnbound {
		t.Fatal("unbound runner accepted", e)
	}
	authority := &denyRunnerAuthority{}
	svc.auth.Runners = authority
	e = svc.authorizeAppend(context.Background(), tx, entry, key)
	if !errors.As(e, &ff) || ff.Code != f.Forbidden || !authority.called || authority.tx != tx || authority.key.Details() != key.Details() || authority.entry.Fields().Action != c.RunnerCreate {
		t.Fatal("authority not original Tx/input", e)
	}
}
