package mcp

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestOAuthRecoveryConnectionRequiresOriginalTokenPublication(t *testing.T) {
	source := oauthRecoveryTestFact(t)
	token, _ := f.NewID[sc.Credential]()
	source.phase, source.resultCredential, source.resultVersion = "complete", token.String(), 1
	request, _ := f.NewID[f.Request]()
	lease, _ := f.NewID[sc.Lease]()
	connection := oauthConnectionRecoveryFact{request: request.String(), command: source.command, project: source.project,
		config: source.config, revision: source.revision, connection: source.connection, version: source.version + 1,
		user: source.user, session: source.session, credential: source.resultCredential, lease: lease.String(), phase: "pending"}
	if !connection.matches(source) || !connection.sameOrigin(connection) {
		t.Fatal("original token publication rejected")
	}
	for _, change := range []func(*oauthConnectionRecoveryFact){
		func(v *oauthConnectionRecoveryFact) { v.command = digest([]byte("another callback command")) },
		func(v *oauthConnectionRecoveryFact) { v.project = source.config },
		func(v *oauthConnectionRecoveryFact) { v.config = source.project },
		func(v *oauthConnectionRecoveryFact) { v.revision++ },
		func(v *oauthConnectionRecoveryFact) { v.connection = source.flow },
		func(v *oauthConnectionRecoveryFact) { v.version++ },
		func(v *oauthConnectionRecoveryFact) { v.user = source.session },
		func(v *oauthConnectionRecoveryFact) { v.session = source.user },
		func(v *oauthConnectionRecoveryFact) { v.credential = source.credential },
		func(v *oauthConnectionRecoveryFact) { v.lease = "" },
		func(v *oauthConnectionRecoveryFact) { v.request = "" },
		func(v *oauthConnectionRecoveryFact) { v.phase = "unknown" },
		func(v *oauthConnectionRecoveryFact) { v.released = true },
	} {
		other := connection
		change(&other)
		if other.matches(source) {
			t.Fatal("foreign or malformed connection tail matched original publication")
		}
	}
	for _, phase := range []string{"claimed", "pending", "unknown", "failed"} {
		other := source
		other.phase = phase
		if connection.matches(other) {
			t.Fatal("unconfirmed token publication authorized SDK retirement")
		}
	}
	closed := connection
	closed.phase, closed.retired, closed.released = "abandoned", true, true
	if !closed.matches(source) || !closed.sameOrigin(connection) {
		t.Fatal("original closed tail lost its immutable binding")
	}
	closed.lease = source.lease
	if closed.sameOrigin(connection) {
		t.Fatal("another original lease replaced the captured tail")
	}
}

type oauthConnectionTailSQL struct {
	postgres.SQLExecutor
	pending       bool
	writes, reads int
	failure       error
}

func (x *oauthConnectionTailSQL) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	x.writes++
	return pgconn.NewCommandTag("UPDATE 1"), x.failure
}
func (x *oauthConnectionTailSQL) QueryRow(context.Context, string, ...any) postgres.Row {
	x.reads++
	return oauthLookupRow(func(dest ...any) error {
		*dest[0].(*bool) = x.pending
		return nil
	})
}

// This verifies retirement ordering and the explicit postcondition. Real SQL
// CAS matching, Guard admission and Secret retirement remain integration gates.
func TestOAuthRecoveryConnectionBusyPointerNeedsClosedTail(t *testing.T) {
	for _, closed := range []oauthConnectionRecoveryFact{
		{phase: "abandoned"}, {phase: "abandoned", retired: true}, {phase: "abandoned", released: true},
	} {
		x := &oauthConnectionTailSQL{}
		if err := clearStoppedOAuthConnection(context.Background(), x, closed); err == nil || x.writes != 0 || x.reads != 0 {
			t.Fatal("unfinished I/O or lease reached connection publication")
		}
	}
	closed := oauthConnectionRecoveryFact{phase: "abandoned", retired: true, released: true}
	x := &oauthConnectionTailSQL{pending: true}
	if err := clearStoppedOAuthConnection(context.Background(), x, closed); err == nil || x.writes != 1 || x.reads != 1 {
		t.Fatal("remaining original busy pointer was declared recovered")
	}
	x = &oauthConnectionTailSQL{}
	if err := clearStoppedOAuthConnection(context.Background(), x, closed); err != nil || x.writes != 1 || x.reads != 1 {
		t.Fatal("closed original tail did not verify its detached pointer", err)
	}
	closed.phase = "complete"
	x = &oauthConnectionTailSQL{}
	if err := clearStoppedOAuthConnection(context.Background(), x, closed); err != nil || x.writes != 0 || x.reads != 1 {
		t.Fatal("completed SDK result was overwritten by cold cancellation", err)
	}
	closed.phase = "abandoned"
	sentinel := errors.New("original transaction failure")
	x = &oauthConnectionTailSQL{failure: sentinel}
	if err := clearStoppedOAuthConnection(context.Background(), x, closed); err != sentinel || x.reads != 0 {
		t.Fatal("publication failure lost its original transaction result")
	}
}

func TestOAuthRecoveryConnectionBareContextCannotReleaseOrRead(t *testing.T) {
	source := oauthRecoveryTestFact(t)
	project, _ := f.ParseID[id.Project](source.project)
	scope, _ := id.InProject(project)
	registration, _ := id.RegisterService(id.SecretService)
	actor, _ := registration.Actor(source.request, scope)
	credential, _ := f.ParseID[sc.Credential](source.credential)
	ref, _ := sc.NewCredentialRef(credential, scope)
	lease, _ := f.ParseID[sc.Lease](source.lease)
	owner, _ := sc.NewCredentialLeaseOwner(sc.MCPConnectionRequestOwner, source.request)
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseID: lease, LeaseOwner: owner, Action: sc.ReleaseLeaseUsage}
	service := &Service{}
	w := &oauthConnectionRecoveryUse{recovery: &oauthRecoveryUse{service: service, original: source}, request: r}
	ctx := context.WithValue(context.Background(), oauthConnectionRecoveryKey{}, w)
	for _, use := range []sc.UsageRequest{r, func() sc.UsageRequest { copy := r; copy.Action = sc.AcquireLeaseUsage; return copy }(), func() sc.UsageRequest {
		copy := r
		copy.Action = sc.ReadLeaseUsage
		copy.RequestID = source.request
		return copy
	}()} {
		if _, err := service.DiscoverUsage(ctx, use); err == nil {
			t.Fatal("row data without actual Guard/claim acquired a usage plan")
		}
	}
	if _, err := service.AuthorizeLeaseInTx(ctx, f.NewTx(), actor, ref, owner, sc.ReleaseLease); err == nil {
		t.Fatal("bare technical actor/lease DTO granted release")
	}
}
