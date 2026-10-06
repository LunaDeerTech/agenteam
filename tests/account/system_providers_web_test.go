//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runSystemProvidersWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	f := newProvidersWebFixture(t, ctx, mode)
	result := f.browserProviders(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("Provider browser evidence incomplete: %s", name)
		}
	}
	largeBytes, _ := result["large_bytes"].(float64)
	if mode == "read" && (result["providers"] != float64(26) || result["models"] != float64(26) || largeBytes <= 600000) {
		t.Fatal("formal Provider/Model page scale evidence incomplete")
	}
	if mode == "navigation" && result["layouts"] != float64(8) {
		t.Fatal("Provider light/dark four-width matrix incomplete")
	}
	t.Logf("real Provider production browser group=%s completed; original 45s browser/2m top budgets, formal writes and owned response controls; configuration success makes no external connectivity claim", mode)
}
func TestAccountSystemProvidersWebLifecycle(t *testing.T) {
	runSystemProvidersWeb(t, "lifecycle", "created", "edited", "enabled", "deleted", "model_fence", "url_compatible")
}
func TestAccountSystemProvidersWebCredentialReplacement(t *testing.T) {
	runSystemProvidersWeb(t, "credential", "created", "replaced", "shared_preserved", "partial", "rebase", "credential_retained")
}
func TestAccountSystemProvidersWebOutcomeRecovery(t *testing.T) {
	runSystemProvidersWeb(t, "outcome", "credential_replayed", "provider_replayed", "historical_only", "abandoned", "confirmed_read_failure")
}
func TestAccountSystemProvidersWebReadAndPagination(t *testing.T) {
	runSystemProvidersWeb(t, "read", "canonical", "bad_cursor", "metadata_version", "metadata_missing", "large_page")
}
func TestAccountSystemProvidersWebAuthorityAndIdentity(t *testing.T) {
	runSystemProvidersWeb(t, "authority", "denied", "forbidden", "revoked", "new_session", "switched")
}
func TestAccountSystemProvidersWebNavigationAndLayouts(t *testing.T) {
	runSystemProvidersWeb(t, "navigation", "navigation", "dirty", "checking", "dialogs", "partial", "uncertain")
}
