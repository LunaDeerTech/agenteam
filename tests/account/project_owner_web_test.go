//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runProjectOwnerWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	// Registered first, so the original top-level budget includes every fixture
	// cleanup and actual join. A timeout is a failure, never a detached success.
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Project browser top exceeded its 120-second budget including cleanup")
		}
	})
	f := newProjectOwnerWebFixture(t, ctx, mode)
	result := f.browser(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("Project browser evidence incomplete: %s", name)
		}
	}
	if mode == "read" {
		count, ok := result["projects"].(float64)
		if !ok || count < 26 {
			t.Fatal("Project browser did not verify the real 25-row pagination boundary")
		}
	}
	if mode == "layouts" && result["layouts"] != float64(8) {
		t.Fatal("Project browser layout matrix incomplete")
	}
	t.Logf("Project Owner production browser mode=%s; default root and formal Account/Project APIs; private static hosting only; Skills preparation and terminal lifecycle facts are not D10 or lifecycle-runtime acceptance", mode)
}

func TestAccountProjectOwnerWebReadAndNavigation(t *testing.T) {
	runProjectOwnerWeb(t, "read", "pagination", "filters", "return", "resolve_get", "non_owner", "deleting", "reused_name")
}

func TestAccountProjectOwnerWebEditAndRename(t *testing.T) {
	runProjectOwnerWeb(t, "edit", "save", "clear_description", "rename", "name_conflict", "version_conflict", "dirty_navigation", "confirmed_read_failure")
}

func TestAccountProjectOwnerWebOriginalRecovery(t *testing.T) {
	runProjectOwnerWeb(t, "recovery", "uncertain", "lookup_only", "same_original", "unique_facts", "historical", "local_abandon")
}

func TestAccountProjectOwnerWebIdentityAndOwnership(t *testing.T) {
	runProjectOwnerWeb(t, "identity", "logout", "non_owner", "checking", "new_session", "late_read", "owner", "aggregate")
}

func TestAccountProjectOwnerWebLayouts(t *testing.T) {
	runProjectOwnerWeb(t, "layouts", "keyboard", "focus", "no_overflow", "readonly", "empty", "no_debug")
}
