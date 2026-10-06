//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runSystemModelsWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newModelsWebFixture(t, ctx, mode)
	result := f.browserModels(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("Model browser evidence incomplete: %s", name)
		}
	}
	if mode == "read" {
		providers, _ := result["providers"].(float64)
		models, _ := result["models"].(float64)
		large, _ := result["large_bytes"].(float64)
		if providers < 26 || models < 26 || large <= 600000 {
			t.Fatal("formal Model page scale evidence incomplete")
		}
	}
	if mode == "navigation" && result["layouts"] != float64(8) {
		t.Fatal("Model light/dark four-width matrix incomplete")
	}
	t.Logf("real Model production browser group=%s completed; 45s browser/2m top budgets, formal commands/selectors and exact owned controls; no Provider invocation or foreign adapter claim", mode)
}
func TestAccountSystemModelsWebLifecycle(t *testing.T) {
	runSystemModelsWeb(t, "lifecycle", "four_types", "edited", "enabled", "deleted", "disabled_provider", "no_invocation")
}
func TestAccountSystemModelsWebDeletionAndReplacement(t *testing.T) {
	runSystemModelsWeb(t, "deletion", "required", "optional", "candidate_rules", "current_references", "candidate_changed", "version_conflict", "unbound_ui", "unbound_http", "atomic")
}
func TestAccountSystemModelsWebOutcomeRecovery(t *testing.T) {
	runSystemModelsWeb(t, "outcome", "create_replayed", "update_replayed", "delete_replayed", "historical_only", "confirmed_read_failure", "abandoned", "unique_facts")
}
func TestAccountSystemModelsWebReadAndPagination(t *testing.T) {
	runSystemModelsWeb(t, "read", "canonical", "independent_pages", "empty", "failure", "bad_cursor", "large_page")
}
func TestAccountSystemModelsWebAuthorityAndIdentity(t *testing.T) {
	runSystemModelsWeb(t, "authority", "denied", "write_forbidden", "impact_forbidden", "lookup_forbidden", "revoked", "new_session", "switched")
}
func TestAccountSystemModelsWebNavigationAndLayouts(t *testing.T) {
	runSystemModelsWeb(t, "navigation", "navigation", "dirty", "checking", "selection", "uncertain", "dialogs", "focus")
}
