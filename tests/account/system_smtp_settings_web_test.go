//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runSMTPSettingsWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newSMTPSettingsWebFixture(t, ctx, mode)
	result := f.browserSMTPSettings(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("SMTP browser evidence incomplete: %s", name)
		}
	}
	if mode == "navigation" && result["layouts"] != float64(8) {
		t.Fatal("SMTP light/dark four-width matrix incomplete")
	}
	facts := f.smtpFacts(ctx)
	if facts["delivery_unchanged"] != true || facts["forbidden_calls"] != int32(0) {
		t.Fatal("configuration browser must not create or request any delivery operation")
	}
	t.Logf("real SMTP configuration browser group=%s completed; no connection, authentication, delivery or recipient acceptance claim", mode)
}
func TestAccountSystemSMTPSettingsWebLifecycle(t *testing.T) {
	runSMTPSettingsWeb(t, "lifecycle", "strict_current", "policy", "three_modes", "no_op", "validation", "cancel", "persisted", "disabled_saved_policy", "no_delivery")
}
func TestAccountSystemSMTPSettingsWebCredentialLifecycle(t *testing.T) {
	runSMTPSettingsWeb(t, "credential", "new_material", "empty_keep", "replacement", "remove", "mismatch", "disabled_reference", "private_remount", "private_safety")
}
func TestAccountSystemSMTPSettingsWebConcurrency(t *testing.T) {
	runSMTPSettingsWeb(t, "concurrency", "two_admins", "real_conflict", "draft_and_material", "explicit_adoption", "atomic_facts")
}
func TestAccountSystemSMTPSettingsWebOutcomeRecovery(t *testing.T) {
	runSMTPSettingsWeb(t, "outcome", "accepted_put_cut", "opposite_current", "exact_original", "historical_put", "accepted_post_cut", "historical_post", "unique_facts", "read_failure", "abandon_no_rollback", "no_lookup")
}
func TestAccountSystemSMTPSettingsWebAuthorityAndIdentity(t *testing.T) {
	runSMTPSettingsWeb(t, "authority", "ordinary_denied", "get_forbidden", "put_forbidden", "replay_forbidden", "revoked", "new_session", "switched")
}
func TestAccountSystemSMTPSettingsWebNavigationAndLayouts(t *testing.T) {
	runSMTPSettingsWeb(t, "navigation", "seven_leaves", "eleventh_return", "dirty", "uncertain", "two_confirmations", "checking", "fallback", "native_focus", "drawer", "no_overflow")
}
