//go:build integration

package account_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

type b02Fixture struct {
	*fixture
	challenges *account.Challenges
	events     *outbox.Service
	process    liveProcess
}

func newB02Account(t *testing.T) *b02Fixture {
	t.Helper()
	db, store, authority := database(t)
	return assembleB02(t, db, store, store, authority, nil)
}
func assembleB02(t *testing.T, db *pgfixture.Database, raw *postgres.Store, store accountTestStore, authority *account.Authority, transform func(oc.Appender) oc.Appender) *b02Fixture {
	t.Helper()
	_, cursor, master := keys(t)
	aud, e := audit.New(store, cursor, audit.Authorizations{Sessions: authority, System: authority, Accounts: authority})
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := secret.New(store, master, aud, secret.Authorizations{Sessions: authority, System: authority, Usage: authority, AccountWrites: authority})
	if e != nil {
		t.Fatal(e)
	}
	if e = secrets.Initialize(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "recovery.jsonl")
	sink, e := recoverylog.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	catalog := event.NewCatalog()
	revoked, e := c.DefineSessionsRevoked(catalog)
	if e != nil {
		t.Fatal(e)
	}
	delivery, e := c.DefineDeliveryRequested(catalog)
	if e != nil {
		t.Fatal(e)
	}
	process := liveProcess{id[c.Process](t)}
	events, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{c.AccountProducer: authority}, Sessions: authority, System: authority, Audit: aud, Cursors: cursor, Processes: b02OutboxProcess{process}})
	if e != nil {
		t.Fatal(e)
	}
	var appender oc.Appender = events
	if transform != nil {
		appender = transform(appender)
	}
	challenges, e := account.NewChallenges(authority, process.id)
	if e != nil {
		t.Fatal(e)
	}
	service, e := account.New(account.Dependencies{Authority: authority, Audit: aud, Secrets: secrets, Events: appender, SessionsRevoked: revoked, DeliveryRequested: delivery, Processes: process, RecoveryLog: sink, Challenges: challenges})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if e := service.Force(ctx); e != nil && !service.Joined() {
			t.Error(e)
		}
	})
	return &b02Fixture{fixture: &fixture{db: db, service: service, store: raw, authority: authority, secrets: secrets, log: path}, challenges: challenges, events: events, process: process}
}

// The fixture owns the one live process; it never fabricates a stopped proof.
type b02OutboxProcess struct{ liveProcess }

func (p b02OutboxProcess) CurrentProcess() oc.ProcessID {
	id, _ := foundation.ParseID[oc.Process](p.id.String())
	return id
}
func (p b02OutboxProcess) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	v, e := foundation.ParseID[c.Process](id.String())
	if e != nil {
		return e
	}
	return p.liveProcess.ConfirmStopped(ctx, v)
}
