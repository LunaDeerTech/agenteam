//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runOutboundPolicyWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newOutboundPolicyWebFixture(t, ctx, mode)
	result := fixture.browserOutboundPolicy(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("outbound policy browser evidence incomplete: %s", name)
		}
	}
	if mode == "navigation" && result["layouts"] != float64(8) {
		t.Fatal("outbound light/dark four-width matrix incomplete")
	}
	fixture.mu.Lock()
	joined := fixture.held == fixture.holdJoined
	fixture.mu.Unlock()
	if !joined {
		t.Fatal("owned formal response holds did not actually finish")
	}
	t.Logf("real outbound policy production browser group=%s completed within 45s browser/2m top; no target probes, SMTP sockets, external mailbox, production hosting or Runtime claim", mode)
}
func TestAccountSystemOutboundPolicyWebRulesAndRead(t *testing.T) {
	runOutboundPolicyWeb(t, "read", "initial_empty", "maximum_distinct", "complete_dto", "structured_rules", "explicit_all", "invalid_zero_put", "failed_read_retained")
}
func TestAccountSystemOutboundPolicyWebMutationAndRecovery(t *testing.T) {
	runOutboundPolicyWeb(t, "mutation", "complete_save", "two_administrators", "same_expected_conflict", "draft_retained", "explicit_review", "accepted_cut", "current_not_receipt", "historical_receipt", "same_original", "unique_command_audit", "same_key_rejected", "confirmed_read_failure")
}
func TestAccountSystemOutboundPolicyWebAuthorityAndOwnership(t *testing.T) {
	runOutboundPolicyWeb(t, "authority", "ordinary_denied", "current_read_denied", "current_write_denied", "current_replay_denied", "valid_csrf", "invalid_csrf", "material_cleared", "new_identity", "actual_cancelled_read")
}
func TestAccountSystemOutboundPolicyWebNavigationAndLayouts(t *testing.T) {
	runOutboundPolicyWeb(t, "navigation", "eight_leaves", "twelfth_return", "dirty", "unknown", "checking", "current_focus", "cancelled_read", "clean_leave", "drawer", "no_overflow")
}
