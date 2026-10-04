//go:build integration

package account_test

import (
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func b02Admin(t *testing.T, f *b02Fixture) identity.Actor {
	t.Helper()
	password := f.bootstrap(t)
	r, _ := loginRequest(t, f.fixture, password, "admin@mail.com")
	resp, e := f.service.Login(ctxFor(t), r)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	cookie := useCookie(t, resp)
	actor, e := f.service.Authenticate(ctxFor(t), cookie)
	if e != nil {
		t.Fatal(e)
	}
	if e = resp.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	return actor
}
func TestAccountInvitationAtomicSecretIntentEventAndRevocation(t *testing.T) {
	f := newB02Account(t)
	admin := b02Admin(t, f)
	r, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: "NEW@example.com"})
	receipt, e := f.service.CreateInvitation(ctxFor(t), r)
	if e != nil {
		t.Fatal("create", e, safeFailure(e))
	}
	var invites, intents, events, refs int
	var made, expires time.Time
	var ref string
	e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.invitations),(SELECT count(*) FROM agenteam_account.delivery_intents),(SELECT count(*) FROM agenteam_outbox.events WHERE event_type='account.delivery-requested'),created_at,expires_at,material_ref::text FROM agenteam_account.invitations WHERE id=$1`, receipt.ID.String()).Scan(&invites, &intents, &events, &made, &expires, &ref)
	if e != nil || invites != 1 || intents != 1 || events != 1 || expires.Sub(made) != 24*time.Hour {
		t.Fatal("atomic facts", invites, intents, events, expires.Sub(made), e)
	}
	e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_secret.secret_references WHERE credential_id=$1`, ref).Scan(&refs)
	if e != nil || refs != 1 {
		t.Fatal("reference", refs, e)
	}
	again, e := f.service.CreateInvitation(ctxFor(t), r)
	if e != nil || again != receipt {
		t.Fatal("replay", e, safeFailure(e))
	}
	fields := r.Fields()
	fields.Email = "different@example.com"
	different, _ := c.NewInvitationCreate(fields)
	if _, e = f.service.CreateInvitation(ctxFor(t), different); !hasCode(e, foundation.IdempotencyKeyReused) {
		t.Fatal("semantic", e)
	}
	fields = r.Fields()
	fields.Key = foundation.IdempotencyKey(id[struct{}](t).String())
	resend, _ := c.NewInvitationCreate(fields)
	if _, e = f.service.CreateInvitation(ctxFor(t), resend); !hasCode(e, foundation.RateLimited) {
		t.Fatal("resend limit", e, safeFailure(e))
	}
	if _, e = f.store.Exec(ctxFor(t), `UPDATE agenteam_account.invitations SET last_delivery_at=clock_timestamp()-interval '61 seconds' WHERE id=$1`, receipt.ID.String()); e != nil {
		t.Fatal(e)
	}
	resent, e := f.service.CreateInvitation(ctxFor(t), resend)
	if e != nil || resent.ID != receipt.ID || resent.JobID == receipt.JobID {
		t.Fatal("resend", e, safeFailure(e))
	}
	var expires2 time.Time
	var ref2 string
	if e = f.store.QueryRow(ctxFor(t), `SELECT expires_at,material_ref::text FROM agenteam_account.invitations WHERE id=$1`, receipt.ID.String()).Scan(&expires2, &ref2); e != nil || !expires.Equal(expires2) || ref != ref2 {
		t.Fatal("link renewed", e)
	}
	status, e := f.service.ReconcileDeliveryIntents(ctxFor(t))
	if e != nil || status.Advanced != 2 {
		t.Fatal("canonical", status, e, safeFailure(e))
	}
	revoke := c.InvitationRevoke{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ID: receipt.ID, ExpectedVersion: receipt.Version}
	if e = f.service.RevokeInvitation(ctxFor(t), revoke); e != nil {
		t.Fatal("revoke", e, safeFailure(e))
	}
	if e = f.service.RevokeInvitation(ctxFor(t), revoke); e != nil {
		t.Fatal("revoke replay", e)
	}
	var links, cancelled int
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_account.invitations),(SELECT count(*) FROM agenteam_account.mail_jobs WHERE phase='cancelled'),(SELECT count(*) FROM agenteam_secret.secret_references WHERE credential_id=$1)`, ref).Scan(&links, &cancelled, &refs); e != nil || links != 0 || cancelled != 2 || refs != 0 {
		t.Fatal("revocation gate", links, cancelled, refs, e)
	}
}
func TestAccountInvitationRedeemCurrentProofAndCleanup(t *testing.T) {
	f := newB02Account(t)
	admin := b02Admin(t, f)
	r, _ := c.NewInvitationCreate(c.InvitationCreateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: "invited@example.com"})
	invite, e := f.service.CreateInvitation(ctxFor(t), r)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	token := fixtureLinkToken(t, f, c.InvitationToken, invite.ID.String())
	browser, e := f.service.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer browser.Cookie.Destroy()
	defer browser.CSRF.Destroy()
	view, e := f.service.InspectInvitation(ctxFor(t), browser.Identity, token, nil)
	if e != nil || view.Email != "invited@example.com" {
		t.Fatal("inspect", e)
	}
	if _, e = f.service.InspectInvitation(ctxFor(t), browser.Identity, token, &admin); !hasCode(e, foundation.Forbidden) {
		t.Fatal("signed-in mismatch", e)
	}
	password, _ := sc.NewSecretMaterial([]byte("a newly invited strong passphrase 842!"))
	defer password.Destroy()
	redeem, _ := c.NewInvitationRedeem(c.RedeemFields{Browser: browser.Identity, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Token: token, Username: "invited-user", DisplayName: "邀请账户", Password: password, Confirmation: password})
	receipt, e := f.service.RedeemInvitation(ctxFor(t), redeem)
	if e != nil || !receipt.Completed {
		t.Fatal("redeem", e, safeFailure(e))
	}
	if _, e = f.service.RedeemInvitation(ctxFor(t), redeem); e != nil {
		t.Fatal("redeem replay", e)
	}
	other := redeem.Fields()
	other.Key = foundation.IdempotencyKey(id[struct{}](t).String())
	r2, _ := c.NewInvitationRedeem(other)
	if _, e = f.service.RedeemInvitation(ctxFor(t), r2); !hasCode(e, foundation.ResourceDeleted) {
		t.Fatal("consumed new command", e)
	}
	if _, e = f.service.InspectInvitation(ctxFor(t), browser.Identity, token, nil); !hasCode(e, foundation.ResourceDeleted) {
		t.Fatal("consumed inspect", e)
	}
	lr, _ := loginRequest(t, f.fixture, password, "invited@example.com")
	response, e := f.service.Login(ctxFor(t), lr)
	if e != nil {
		t.Fatal("new user login", e, safeFailure(e))
	}
	actor, e := f.service.Authenticate(ctxFor(t), useCookie(t, response))
	if e != nil {
		t.Fatal(e)
	}
	if e = response.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	if _, e = f.authority.AuthorizeSystem(ctxFor(t), foundation.Tx{}, actor, identity.Mutate); !hasCode(e, foundation.Forbidden) {
		t.Fatal("invited admin", e)
	}
	if _, e = f.service.Recover(ctxFor(t)); e != nil {
		t.Fatal("link cleanup", e, safeFailure(e))
	}
	var unfinished int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.material_cleanup WHERE owner_kind='invitation' AND phase<>'completed'`).Scan(&unfinished); e != nil || unfinished != 0 {
		t.Fatal("material remaining", unfinished, e)
	}
}
