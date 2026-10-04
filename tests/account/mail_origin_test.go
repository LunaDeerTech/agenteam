//go:build integration

package account_test

import (
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func invitedMailUser(t *testing.T, f *b02Fixture, admin identity.Actor, email string) (identity.Actor, sc.SecretMaterial) {
	t.Helper()
	req, e := c.NewInvitationCreate(c.InvitationCreateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: email})
	if e != nil {
		t.Fatal(e)
	}
	inv, e := f.service.CreateInvitation(ctxFor(t), req)
	if e != nil {
		t.Fatal(e)
	}
	token := fixtureLinkToken(t, f, c.InvitationToken, inv.ID.String())
	browser, e := f.service.NewAnonymousContext(ctxFor(t))
	if e != nil {
		t.Fatal(e)
	}
	defer browser.Cookie.Destroy()
	defer browser.CSRF.Destroy()
	password := testPassword(t, "Owned invited user passphrase 485729!")
	redeem, e := c.NewInvitationRedeem(c.RedeemFields{Browser: browser.Identity, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Token: token, Username: "mail-target", DisplayName: "Mail target", Password: password, Confirmation: password})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.service.RedeemInvitation(ctxFor(t), redeem); e != nil {
		t.Fatal(e, safeFailure(e))
	}
	login, _ := loginRequest(t, f.fixture, password, email)
	response, e := f.service.Login(ctxFor(t), login)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	cookie := useCookie(t, response)
	actor, e := f.service.Authenticate(ctxFor(t), cookie)
	if e != nil {
		t.Fatal(e)
	}
	if e = response.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	return actor, password
}
func cancelMailAttempt(t *testing.T, p c.DeliveryPort, r *manualMailRuntime, job c.JobID, prepare bool) {
	t.Helper()
	a, e := p.ClaimDelivery(ctxFor(t), job)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	r.accept(a)
	var m c.DeliveryMaterials
	if prepare {
		m, e = p.PrepareDelivery(ctxFor(t), a)
		if e != nil {
			t.Fatal(e, safeFailure(e))
		}
	}
	if e = p.FinishDelivery(ctxFor(t), a, r.finish(t, a, m)); e != nil {
		t.Fatal(e, safeFailure(e))
	}
}
func TestAccountMailRetryResetOriginIsExactAndCurrentTargetNotAdmin(t *testing.T) {
	f := newB02Account(t)
	adminPassword := f.bootstrap(t)
	admin, _ := b02Login(t, f, adminPassword)
	user, password := invitedMailUser(t, f, admin, "target@example.test")
	for i := range 2 {
		if i != 0 { // Move only the fixture cooldown clock, not captured origin facts.
			if _, e := f.db.Connect(t).Exec(ctxFor(t), `UPDATE agenteam_account.password_resets SET last_delivery_at=clock_timestamp()-interval '61 seconds' WHERE user_id=$1`, user.Details().UserID); e != nil {
				t.Fatal(e)
			}
		}
		if _, e := f.service.RequestPasswordReset(ctxFor(t), resetRequest(t, f, "target@example.test")); e != nil {
			t.Fatal(e)
		}
		if _, e := f.service.Recover(ctxFor(t)); e != nil {
			t.Fatal(e, safeFailure(e))
		}
	}
	if _, e := f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	rows, e := f.store.Query(ctxFor(t), `SELECT id::text,job_id::text,link_id::text FROM agenteam_account.delivery_intents WHERE kind='password_reset' ORDER BY id`)
	if e != nil {
		t.Fatal(e)
	}
	type root struct{ intent, job, link string }
	var roots []root
	for rows.Next() {
		var r root
		if e = rows.Scan(&r.intent, &r.job, &r.link); e != nil {
			t.Fatal(e)
		}
		roots = append(roots, r)
	}
	rows.Close()
	if e = rows.Err(); e != nil || len(roots) != 2 || roots[0].link != roots[1].link {
		t.Fatal("two exact roots for same live link", len(roots), e)
	}
	p, runtime := newManualMail(t, f)
	job, _ := foundation.ParseID[c.MailJob](roots[0].job)
	cancelMailAttempt(t, p, runtime, job, true)
	status, e := f.service.GetMailJob(ctxFor(t), admin, job)
	if e != nil {
		t.Fatal(e)
	}
	q := c.MailJobRetry{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), JobID: job, ExpectedVersion: status.Version}
	bad := q
	bad.Actor = user
	if _, e = f.service.RetryMailJob(ctxFor(t), bad); !hasCode(e, foundation.Forbidden) {
		t.Fatal("non-admin retry", safeFailure(e))
	}
	first, e := f.service.RetryMailJob(ctxFor(t), q)
	if e != nil {
		t.Fatal("admin retry for other user", safeFailure(e))
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	a, e := p.ClaimDelivery(ctxFor(t), first.JobID)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	runtime.accept(a)
	m, e := p.PrepareDelivery(ctxFor(t), a)
	if e != nil {
		t.Fatal("exact original reset target", safeFailure(e))
	}
	if e = m.Use(func(v c.DeliveryMaterialFields) error {
		if v.Recipient != "target@example.test" || v.LinkID != roots[0].link {
			return c.Invalid()
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if e = p.FinishDelivery(ctxFor(t), a, runtime.finish(t, a, m)); e != nil {
		t.Fatal(e)
	}
	status, e = f.service.GetMailJob(ctxFor(t), admin, first.JobID)
	if e != nil {
		t.Fatal(e)
	}
	q2 := c.MailJobRetry{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), JobID: first.JobID, ExpectedVersion: status.Version}
	second, e := f.service.RetryMailJob(ctxFor(t), q2)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	var one, two string
	if e = f.store.QueryRow(ctxFor(t), `SELECT (SELECT origin_intent_id::text FROM agenteam_account.delivery_intents WHERE job_id=$1),(SELECT origin_intent_id::text FROM agenteam_account.delivery_intents WHERE job_id=$2)`, first.JobID.String(), second.JobID.String()).Scan(&one, &two); e != nil || one != roots[0].intent || two != one || one == roots[1].intent {
		t.Fatal("retry chose arbitrary same-Link command", e)
	}
	// Current admin Session may change without changing the command's stable
	// business identity; the original captured Session is not rewritten.
	if e = f.service.Logout(ctxFor(t), account.LogoutRequest{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String())}); e != nil {
		t.Fatal(e)
	}
	current, _ := b02Login(t, f, adminPassword)
	q.Actor = current
	if v, e := f.service.RetryMailJob(ctxFor(t), q); e != nil || v.JobID != first.JobID {
		t.Fatal("current-session receipt", safeFailure(e))
	}
	newPassword := testPassword(t, "Replacement target passphrase 742835!")
	change, e := c.NewPasswordChange(c.PasswordChangeFields{Actor: user, Key: foundation.IdempotencyKey(id[struct{}](t).String()), ExpectedVersion: 1, OldPassword: password, Password: newPassword, Confirmation: newPassword})
	if e != nil {
		t.Fatal(e)
	}
	response, e := f.service.ChangePassword(ctxFor(t), change)
	if e != nil {
		t.Fatal(e, safeFailure(e))
	}
	if e = response.Close(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	var stillThere int
	if e = f.store.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.password_resets WHERE id=$1`, roots[0].link).Scan(&stillThere); e != nil || stillThere != 1 {
		t.Fatal("normal change should retain expired-generation row", stillThere, e)
	}
	if _, e = f.service.ReconcileDeliveryIntents(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	if _, e = p.ClaimDelivery(ctxFor(t), second.JobID); e == nil {
		t.Fatal("old password version admitted")
	}
	if v, e := f.service.RetryMailJob(ctxFor(t), q); e != nil || v.JobID != first.JobID {
		t.Fatal("accepted receipt incorrectly required live reset", safeFailure(e))
	}
	status, e = f.service.GetMailJob(ctxFor(t), current, first.JobID)
	if e != nil {
		t.Fatal(e)
	}
	fresh := c.MailJobRetry{Actor: current, Key: foundation.IdempotencyKey(id[struct{}](t).String()), JobID: first.JobID, ExpectedVersion: status.Version}
	if _, e = f.service.RetryMailJob(ctxFor(t), fresh); !hasCode(e, foundation.ResourceDeleted) {
		t.Fatal("new retry revived old reset generation", safeFailure(e))
	}
}
