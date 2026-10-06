//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runAccountSecurityWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newAccountSecurityWebFixture(t, ctx, mode)
	result := f.browserAccountSecurity(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("Account security browser evidence incomplete: %s", name)
		}
	}
	if mode == "navigation" && result["layouts"] != float64(8) {
		t.Fatal("Account security light/dark four-width matrix incomplete")
	}
	t.Logf("real Account security production browser group=%s completed within 45s browser/2m top budgets; settings only, no retroactive Session revocation or mail delivery claim", mode)
}

func TestAccountSystemAccountSecurityWebLifecycle(t *testing.T) {
	runAccountSecurityWeb(t, "lifecycle", "current_settings", "four_fields", "no_op", "cancelled", "ranges", "persisted", "old_session_unchanged", "new_session_lifetimes")
}
func TestAccountSystemAccountSecurityWebConcurrency(t *testing.T) {
	runAccountSecurityWeb(t, "concurrency", "two_sessions", "same_version", "real_conflict", "draft_preserved", "explicit_adoption", "atomic_facts")
}
func TestAccountSystemAccountSecurityWebOutcomeRecovery(t *testing.T) {
	runAccountSecurityWeb(t, "outcome", "accepted_cut", "current_not_receipt", "historical_result", "same_original", "unique_facts", "confirmed_read_failure", "abandon_no_rollback", "no_lookup")
}
func TestAccountSystemAccountSecurityWebAuthorityAndIdentity(t *testing.T) {
	runAccountSecurityWeb(t, "authority", "ordinary_denied", "get_forbidden", "put_forbidden", "replay_forbidden", "revoked", "new_session", "switched")
}
func TestAccountSystemAccountSecurityWebNavigationAndLayouts(t *testing.T) {
	runAccountSecurityWeb(t, "navigation", "six_leaves", "tenth_return", "dirty", "uncertain", "checking", "focus", "drawer", "no_overflow")
}
