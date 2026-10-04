//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// This test-only adapter authorizes one explicit generic consumer of one owned
// fixture credential. It is not the future AccountDelivery/SMTP adapter.
type fixtureLinkConsumer struct {
	ref   sc.CredentialRef
	owner sc.CredentialLeaseOwner
}

func (p fixtureLinkConsumer) CheckReferenceInTx(context.Context, foundation.Tx, identity.Actor, sc.CredentialRef, sc.Purpose, string, bool) error {
	return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
}
func (p fixtureLinkConsumer) AuthorizeLeaseInTx(ctx context.Context, tx foundation.Tx, a identity.Actor, r sc.CredentialRef, o sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	if ctx.Err() != nil {
		return sc.UseGrant{}, ctx.Err()
	}
	if !tx.Valid() || !r.Equal(p.ref) || !o.Equal(p.owner) || a.Details().ServiceName != identity.SecretService || a.Details().CauseRef != o.Details().ID {
		return sc.UseGrant{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return sc.UseGrant{Subject: a, Consumer: sc.System}, nil
}
func TestAccountExpiredLinkInvalidWhileActualMaterialUserKeepsLease(t *testing.T) {
	f := newB02Account(t)
	admin := b02Admin(t, f)
	r, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: admin, Key: "protected-expiry", Email: "protected-expiry@example.com"})
	receipt, e := f.service.CreateInvitation(ctxFor(t), r)
	if e != nil {
		t.Fatal(e)
	}
	token := fixtureLinkToken(t, f, c.InvitationToken, receipt.ID.String())
	var refID string
	if e = f.store.QueryRow(ctxFor(t), `SELECT material_ref::text FROM agenteam_account.invitations WHERE id=$1`, receipt.ID.String()).Scan(&refID); e != nil {
		t.Fatal(e)
	}
	rid, e := foundation.ParseID[sc.Credential](refID)
	if e != nil {
		t.Fatal(e)
	}
	ref, e := sc.NewCredentialRef(rid, identity.SystemScope())
	if e != nil {
		t.Fatal(e)
	}
	owner, e := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, id[struct{}](t).String())
	if e != nil {
		t.Fatal(e)
	}
	reg, e := identity.RegisterService(identity.SecretService)
	if e != nil {
		t.Fatal(e)
	}
	actor, e := reg.Actor(owner.Details().ID, identity.SystemScope())
	if e != nil {
		t.Fatal(e)
	}
	_, cursor, keys := keys(t)
	aud, e := audit.New(f.store, cursor, audit.Authorizations{Sessions: f.authority, System: f.authority, Accounts: f.authority})
	if e != nil {
		t.Fatal(e)
	}
	consumer, e := secret.New(f.store, keys, aud, secret.Authorizations{Usage: fixtureLinkConsumer{ref, owner}})
	if e != nil {
		t.Fatal(e)
	}
	if e = consumer.Initialize(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	var lease sc.CredentialLease
	result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		var e error
		lease, e = consumer.AcquireCredentialLeaseInTx(ctx, tx, actor, ref, owner)
		return e
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	material, e := consumer.ReadCredentialForRequest(ctxFor(t), actor, lease.LeaseID)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	joined := make(chan error, 1)
	go func() {
		joined <- material.Use(func(raw []byte) error {
			if len(raw) == 0 {
				return foundation.NewFault(foundation.InternalError, foundation.NotStarted)
			}
			close(entered)
			<-release
			return nil
		})
	}()
	await(t, entered)
	released := false
	defer func() {
		if !released {
			close(release)
		}
		material.Destroy()
	}()
	if _, e = f.store.Exec(ctxFor(t), `WITH n AS(SELECT clock_timestamp() t) UPDATE agenteam_account.invitations SET created_at=n.t-interval '25 hours',expires_at=n.t-interval '1 hour' FROM n WHERE id=$1`, receipt.ID.String()); e != nil {
		t.Fatal(e)
	}
	status, e := f.service.Recover(ctxFor(t))
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if status.Pending < 1 {
		t.Fatal("in-use payload claimed clean")
	}
	var links, refs, payloads, active int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.invitations WHERE id=$1),(SELECT count(*) FROM agenteam_secret.secret_references WHERE credential_id=$2),(SELECT count(*) FROM agenteam_secret.secrets WHERE id=$2),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE id=$3 AND NOT released)`, receipt.ID.String(), refID, lease.LeaseID.String()).Scan(&links, &refs, &payloads, &active); e != nil || links != 0 || refs != 0 || payloads != 1 || active != 1 {
		t.Fatal("expiry vs active lease", links, refs, payloads, active, e)
	}
	b, e := f.service.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer b.Cookie.Destroy()
	defer b.CSRF.Destroy()
	if _, e = f.service.InspectInvitation(ctxFor(t), b.Identity, token, nil); !hasCode(e, foundation.ResourceDeleted) {
		t.Fatal("protected bytes kept link valid", e)
	}
	select {
	case <-joined:
		t.Fatal("fixture reader not active")
	default:
	}
	close(release)
	released = true
	select {
	case e = <-joined:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not join")
	}
	material.Destroy()
	result = f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		return consumer.ReleaseCredentialLeaseInTx(ctx, tx, actor, lease.LeaseID)
	})
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	if _, e = f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_secret.secrets WHERE id=$1),(SELECT count(*) FROM agenteam_account.material_cleanup WHERE owner_id=$2 AND phase<>'completed')`, refID, receipt.ID.String()).Scan(&payloads, &active); e != nil || payloads != 0 || active != 0 {
		t.Fatal("joined payload not cleaned", payloads, active, e)
	}
}
