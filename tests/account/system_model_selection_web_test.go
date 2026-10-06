//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runSystemModelSelectionWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newSelectionWebFixture(t, ctx, mode)
	result := f.browserSelection(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("Selection browser evidence incomplete: %s", name)
		}
	}
	if mode == "read" && (result["providers"].(float64) < 26 || result["models"].(float64) < 26) {
		t.Fatal("formal Selection pagination scale incomplete")
	}
	if mode == "navigation" && result["layouts"] != float64(8) {
		t.Fatal("Selection light/dark four-width matrix incomplete")
	}
	t.Logf("real Selection production browser group=%s completed with 45s browser/2m top budgets; formal configuration facts only, no Provider invocation or index rebuild claim", mode)
}

func TestAccountSystemModelSelectionWebLifecycle(t *testing.T) {
	runSystemModelSelectionWeb(t, "lifecycle", "null_v1", "four_purposes", "optional_null", "no_op", "cancelled", "read_back", "no_invocation")
}
func TestAccountSystemModelSelectionWebReadAndPagination(t *testing.T) {
	runSystemModelSelectionWeb(t, "read", "four_plus_four", "canonical_pages", "independent_pages", "empty", "bad_cursor", "late_read", "no_half_pair", "invalid_bindings")
}
func TestAccountSystemModelSelectionWebConcurrencyAndReferences(t *testing.T) {
	runSystemModelSelectionWeb(t, "concurrency", "two_admins", "same_version", "atomic_rejection", "required_replacement", "optional_clear", "old_version_rejected")
}
func TestAccountSystemModelSelectionWebOutcomeRecovery(t *testing.T) {
	runSystemModelSelectionWeb(t, "outcome", "accepted_cut", "lookup_only", "old_404", "same_original", "unique_facts", "confirmed_read_failure", "abandoned")
}
func TestAccountSystemModelSelectionWebAuthorityAndIdentity(t *testing.T) {
	runSystemModelSelectionWeb(t, "authority", "denied", "get_forbidden", "put_forbidden", "lookup_forbidden", "revoked", "new_session", "switched")
}
func TestAccountSystemModelSelectionWebNavigationAndLayouts(t *testing.T) {
	runSystemModelSelectionWeb(t, "navigation", "five_leaves", "ninth_return", "dirty", "uncertain", "checking", "focus", "dialogs", "no_overflow")
}
