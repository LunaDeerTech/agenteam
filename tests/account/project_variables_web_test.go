//go:build integration

package account_test

import (
	"context"
	"testing"
	"time"
)

func runProjectVariablesWeb(t *testing.T, mode string, required ...string) {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Variables browser top exceeded original 120-second budget including cleanup")
		}
	})
	fixture := newProjectVariablesWebFixture(t, ctx, mode)
	// This limited postcondition also runs when the browser calls Fatal. It
	// cannot turn a failed browser or unexecuted matrix into a successful test.
	defer fixture.verifyAuthorityRefusal(ctx)
	result := fixture.owner.browser(ctx)
	for _, key := range required {
		if result[key] != true {
			t.Fatalf("Variables browser evidence incomplete: %s", key)
		}
	}
	fixture.verify(ctx)
	t.Logf("Variables browser mode=%s; real default root/Account/Project and ordinary variables; private static hosting, test Skills prerequisite and lifecycle fact-only input", mode)
}
func TestAccountProjectVariablesWebReadAndPagination(t *testing.T) {
	runProjectVariablesWeb(t, "read", "paging", "deep_link", "refresh", "summary", "cursor_stale", "invalid_route")
}
func TestAccountProjectVariablesWebCRUDAndHistory(t *testing.T) {
	runProjectVariablesWeb(t, "crud", "crud", "empty", "presence", "noop", "same_name_new_id", "historical")
}
func TestAccountProjectVariablesWebOriginalRecovery(t *testing.T) {
	runProjectVariablesWeb(t, "recovery", "three_cuts", "original_lookup", "original_replay", "historical", "unique_facts")
}
func TestAccountProjectVariablesWebIdentityAndCancellation(t *testing.T) {
	runProjectVariablesWeb(t, "identity", "checking", "cancel_logout", "new_session", "held_cancel", "joined")
}
func TestAccountProjectVariablesWebAuthorityAndLifecycle(t *testing.T) {
	runProjectVariablesWeb(t, "authority", "non_owner", "administrator", "archived", "new_write_refused", "history_replay")
}
func TestAccountProjectVariablesWebLayouts(t *testing.T) {
	runProjectVariablesWeb(t, "layouts", "themes", "narrow", "keyboard", "focus", "readonly", "no_overflow")
}
