//go:build integration

package account_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func runProjectWorkPlanningWeb(t *testing.T, mode string, required ...string) *projectWorkPlanningWebFixture {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(func() {
		cancel()
		if time.Since(started) > 120*time.Second {
			t.Error("Work browser top exceeded original 120-second budget including cleanup")
		}
	})
	f := newProjectWorkPlanningWebFixture(t, ctx, mode)
	result := f.browser(ctx)
	for _, name := range required {
		if result[name] != true {
			t.Fatalf("Work browser evidence incomplete: %s", name)
		}
	}
	// Successful browser commands must have actual same-key committed facts.
	// A result flag or a response prefix alone never satisfies this check.
	for _, observed := range f.observations() {
		if observed.Method == "GET" || strings.HasSuffix(observed.RawPath, "/lookup") || observed.Status != 200 {
			continue
		}
		table := workCommandTable(observed.Domain)
		if table == "" {
			t.Fatal("observed Work mutation has no closed command domain")
		}
		var count int
		if err := f.store.QueryRow(ctx, `SELECT count(*) FROM `+table+` WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed'`, observed.ProjectID, observed.Command, observed.Key).Scan(&count); err != nil || count != 1 {
			t.Fatal("observed successful Work mutation lacks its unique actual completed command")
		}
	}
	return f
}
func TestAccountProjectWorkPlanningWebReadAndNavigation(t *testing.T) {
	runProjectWorkPlanningWeb(t, "read", "pagination", "deep_links", "refresh", "raw_route", "parent", "permissions", "cursor")
}
func TestAccountProjectWorkPlanningWebStructureAndTasks(t *testing.T) {
	f := runProjectWorkPlanningWeb(t, "planning", "structure", "task", "plan", "ordering", "conflict", "current_receipt")
	observed := map[string]bool{}
	for _, r := range f.observations() {
		if r.Status == 200 && !strings.HasSuffix(r.RawPath, "/lookup") {
			observed[r.Command] = true
		}
	}
	for _, command := range []string{"work.milestone.create", "work.milestone.update", "work.milestone.reorder", "work.sprint.create", "work.sprint.update", "work.sprint.reorder", "work.task.create", "work.task.update", "work.task.reorder"} {
		if !observed[command] {
			t.Fatal("missing actual successful planning capability", command)
		}
	}
}
func TestAccountProjectWorkPlanningWebBlockers(t *testing.T) {
	runProjectWorkPlanningWeb(t, "blockers", "two_types", "resolve", "cycle", "status", "cursor", "state_unchanged")
}
func TestAccountProjectWorkPlanningWebOriginalRecovery(t *testing.T) {
	f := runProjectWorkPlanningWeb(t, "recovery", "three_domains", "lookup_original", "same_replay", "history", "archive", "unique_facts")
	f.guard.Lock()
	defer f.guard.Unlock()
	if f.dropped != 3 || len(f.lost) != 3 {
		t.Fatal("three actual committed original responses were not truncated")
	}
}
func TestAccountProjectWorkPlanningWebIdentityAndOwnership(t *testing.T) {
	runProjectWorkPlanningWeb(t, "identity", "logout", "revocation", "owner", "checking", "new_session", "late_read", "confirmations")
}
func TestAccountProjectWorkPlanningWebLayouts(t *testing.T) {
	f := runProjectWorkPlanningWeb(t, "layouts", "keyboard", "focus", "no_overflow", "readonly", "empty", "error", "no_debug")
	_ = f
}
