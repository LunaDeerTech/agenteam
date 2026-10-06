//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runSMTPDeliveryWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newSMTPDeliveryWebFixture(t, ctx, mode)
	result := f.browserSMTPDelivery(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("SMTP delivery browser evidence incomplete: %s", name)
		}
	}
	if mode == "navigation" && result["layouts"] != float64(8) {
		t.Fatal("SMTP delivery eight-layout matrix incomplete")
	}
	if e := f.smtp.Verify(ctx); e != nil {
		t.Fatal("owned SMTP identity changed", e)
	}
	t.Logf("real SMTP delivery browser group=%s completed; owned protocol acceptance is not external mailbox delivery", mode)
}
func TestAccountSystemSMTPDeliveryWebReadAndPagination(t *testing.T) {
	runSMTPDeliveryWeb(t, "read", "empty", "three_kinds", "two_channels", "strict_dto", "continuous_pages", "exact_detail", "cancelled_unattempted", "compatibility_separate")
}
func TestAccountSystemSMTPDeliveryWebTestAndRetry(t *testing.T) {
	runSMTPDeliveryWeb(t, "test", "accepted", "smtp_success", "smtp_failed", "smtp_unknown", "new_cycle", "version_conflict", "root_busy", "material_invalid", "explicit_review")
}
func TestAccountSystemSMTPDeliveryWebOutcomeRecovery(t *testing.T) {
	runSMTPDeliveryWeb(t, "outcome", "test_cut", "retry_cut", "exact_original", "unique_transactions", "current_disabled", "source_changed", "new_version", "read_not_receipt", "read_failure")
}
func TestAccountSystemSMTPDeliveryWebAuthorityAndIdentity(t *testing.T) {
	runSMTPDeliveryWeb(t, "authority", "get_forbidden", "write_forbidden", "replay_forbidden", "revoked", "ordinary_denied", "new_session", "private_cleanup")
}
func TestAccountSystemSMTPDeliveryWebNavigationAndLayouts(t *testing.T) {
	runSMTPDeliveryWeb(t, "navigation", "default_no_delivery", "two_sections", "dirty", "two_confirmations", "checking", "same_session", "focus", "escape", "pointer", "tab", "drawer", "no_overflow", "safe_capture")
}
