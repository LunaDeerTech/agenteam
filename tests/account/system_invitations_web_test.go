//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runSystemInvitationsWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newInvitationsWebFixture(t, ctx, mode)
	result := f.browserInvitations(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("invitation browser evidence incomplete: %s", name)
		}
	}
	if mode == "read" && (result["pages"] != float64(4) || result["rows"] != float64(26) || len(f.expected) != 26) {
		t.Fatal("invitation four-page canonical observations incomplete")
	}
	if mode == "navigation" && result["layouts"] != float64(8) {
		t.Fatal("invitation light/dark four-width matrix incomplete")
	}
	if mode == "delivery" {
		failed, e := f.smtp.State(ctx, f.smtpFailed.ID)
		if e != nil {
			t.Fatal(e)
		}
		sent, e := f.smtp.State(ctx, f.smtpSuccess.ID)
		if e != nil {
			t.Fatal(e)
		}
		if failed.Mail != 1 || failed.Messages != 0 || sent.Messages != 1 || failed.Connections != failed.Closed || sent.Connections != sent.Closed {
			t.Fatal("owned real SMTP failure/success or actual socket joins incomplete")
		}
		var joined int
		conn, closeConnection := invitationsWebConnect(t, ctx, f.db)
		defer closeConnection()
		if e = conn.QueryRow(ctx, `SELECT count(*) FROM agenteam_account.mail_attempts WHERE channel='smtp' AND terminal AND io_joined AND result IN ('failed','sent')`).Scan(&joined); e != nil || joined != 2 {
			t.Fatal("real current attempts did not join and publish", e)
		}
	}
	t.Logf("real invitation production browser group=%s completed within original 45s browser/2m top budgets; task-owned response/clock/authority controls are fixture preparation, not production hosting or elapsed-time claims", mode)
}
func TestAccountSystemInvitationsWebLifecycle(t *testing.T) {
	runSystemInvitationsWeb(t, "lifecycle", "created", "literal", "resend", "limited", "revoked", "redeemed")
}
func TestAccountSystemInvitationsWebDeliveryRetry(t *testing.T) {
	runSystemInvitationsWeb(t, "delivery", "channels", "retried", "new_job")
}
func TestAccountSystemInvitationsWebOutcomeRecovery(t *testing.T) {
	runSystemInvitationsWeb(t, "outcome", "replayed", "abandoned", "confirmed_read_failure", "conflict")
}
func TestAccountSystemInvitationsWebReadAndPagination(t *testing.T) {
	runSystemInvitationsWeb(t, "read", "canonical", "expired", "bad_cursor")
}
func TestAccountSystemInvitationsWebAuthorityAndIdentity(t *testing.T) {
	runSystemInvitationsWeb(t, "authority", "denied", "forbidden", "revoked", "switched")
}
func TestAccountSystemInvitationsWebNavigationAndLayouts(t *testing.T) {
	runSystemInvitationsWeb(t, "navigation", "navigation", "dirty", "checking", "dialogs")
}
