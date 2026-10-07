//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runAuditWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newAuditWebFixture(t, ctx, mode)
	result := f.browserAudit(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("Audit browser evidence incomplete: %s", name)
		}
	}
	if mode == "navigation" && result["layouts"] != float64(8) {
		t.Fatal("Audit theme/width image matrix incomplete")
	}
	f.mu.Lock()
	joined := f.held == f.joined
	f.mu.Unlock()
	if !joined {
		t.Fatal("owned Audit response holds did not actually join")
	}
	if f.serverStarted.Load() != f.serverFinished.Load() {
		t.Fatal("owned Audit proxy response callbacks remain active")
	}
	t.Logf("real Audit UI group=%s complete; only formal Account/Profile/Outbound producers, no Project/Tools/external target or production hosting claim", mode)
}

func TestAccountSystemAuditWebReadAndFilters(t *testing.T) {
	runAuditWeb(t, "read", "complete_projection", "typed_families", "formal_mime", "explicit_pagination", "all_filters", "valid_empty", "zero_mutations")
}

func TestAccountSystemAuditWebAuthorityAndOwnership(t *testing.T) {
	runAuditWeb(t, "authority", "ordinary_forbidden", "both_gets", "detail_missing", "cut_reread", "list_cancel_join", "detail_cancel_join", "cross_domain", "formal_logout", "late_isolated", "zero_mutations")
}

func TestAccountSystemAuditWebNavigationAndLayouts(t *testing.T) {
	runAuditWeb(t, "navigation", "nine_leaves", "thirteenth_return", "local_dirty_leave", "inline_focus", "checking_new_page", "drawer", "no_overflow", "formal_mime", "zero_mutations")
}
