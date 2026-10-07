//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runMeetingSummaryWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newMeetingSummaryWebFixture(t, ctx, mode)
	result := f.browserSummary(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("Summary browser evidence incomplete: %s", name)
		}
	}
	if mode == "read" && (result["layouts"] != float64(8) || result["providers"].(float64) < 26 || result["models"].(float64) < 26) {
		t.Fatal("Summary actual pagination/layout matrix incomplete")
	}
	t.Logf("Summary production browser mode=%s; formal bootstrap/invite/redeem/login/Logout; authority role SQL is owned fact preparation only; no generation/Invocation/production SPA claim", mode)
}
func TestAccountSystemMeetingSummaryWebLifecycle(t *testing.T) {
	runMeetingSummaryWeb(t, "lifecycle", "null_v1", "plain_chat", "independent", "disabled_reference", "no_clear", "no_invocation")
}
func TestAccountSystemMeetingSummaryWebRecovery(t *testing.T) {
	runMeetingSummaryWeb(t, "recovery", "dual_intents", "lookup_only", "same_original", "cross_key", "historical", "local_abandon", "confirmed_read_failure", "unique_facts")
}
func TestAccountSystemMeetingSummaryWebAuthorityNavigation(t *testing.T) {
	runMeetingSummaryWeb(t, "authority", "aggregate", "checking", "new_session", "current403", "logout", "owner", "focus", "no_overlay")
}
func TestAccountSystemMeetingSummaryWebReadLayouts(t *testing.T) {
	runMeetingSummaryWeb(t, "read", "pagination", "empty", "bad_cursor", "late_read", "no_half_pair", "disabled_reference", "keyboard", "no_overflow")
}
