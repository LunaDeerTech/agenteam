//go:build integration

package projectvariable_test

import (
	"bytes"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This bounded chain reuses the original claim, synchronous Launch rejection,
// failure finalizer and TLS HTTP assembly. No Task state or Blocker is SQL-seeded.
// The retained technical cause means the Human resolution is not the last cause;
// this test does not claim a PG proof of the last-Human-blocker rejection.
func TestTaskBlockerManagementAssignedBlockedHTTP(t *testing.T) {
	t.Run("original-failure-human-cause-and-technical-boundary", func(t *testing.T) {
		x := newSchedulerFailureFixture(t)
		beforeFailure := x.currentTask(t)
		settled, err := x.finalizer.FinalizeLaunchFailure(ctxFor(t), x.request.ProjectID, x.dispatch)
		if err != nil {
			t.Fatal("original final failure", err)
		}
		x.requireSettled(t, settled, beforeFailure)
		v := technicalResolutionHTTPFixture(t, x.v)
		browser := x.v.base.ownerBrowser
		before := x.currentTask(t)
		readBlockers := func() []wc.TaskBlocker {
			t.Helper()
			reply := v.request(t, browser, "GET", v.taskPath()+"/blockers?status=all", "", "")
			var page f.Page[wc.TaskBlocker]
			if reply.status != 200 || json.Unmarshal(reply.body, &page) != nil || page.Items == nil || page.NextCursor != "" {
				t.Fatal("original complete Blocker read")
			}
			return page.Items
		}
		original := readBlockers()
		if before.State != wc.TaskStateBlocked || before.AssigneeAgentID == nil || len(original) != 1 || original[0].Type != wc.TaskBlockerTechnical || original[0].ResolvedAt != nil {
			t.Fatal("formal original failure did not produce assigned blocked Task")
		}
		technical := original[0].Clone()
		add := wc.TaskBlockerCreate{BlockerID: id[wc.TaskBlockerIdentity](t), Type: wc.TaskBlockerWaitingForHuman, Description: "Owner is checking the launch configuration", Metadata: wc.TaskBlockerMetadata{WaitingForHuman: &wc.TaskBlockerWaitingForHumanMetadata{}}}
		addKey := f.IdempotencyKey("blocked-human-cause-add")
		addBody := string(jsonBytes(t, map[string]any{"expected_version": before.Version, "request": add}))
		addedReply := v.request(t, browser, "POST", v.taskPath()+"/blockers", addBody, addKey)
		var added wc.TaskBlockerMutation
		if addedReply.status != 200 || json.Unmarshal(addedReply.body, &added) != nil || added.Validate() != nil || added.Blocker.ID != add.BlockerID || added.Blocker.Type != add.Type {
			t.Fatal("assigned blocked Human Add did not commit", addedReply.status)
		}
		want := before.Clone()
		want.Version, want.UpdatedAt = before.Version+1, added.Task.UpdatedAt
		if !bytes.Equal(jsonBytes(t, added.Task), jsonBytes(t, want)) || !bytes.Equal(jsonBytes(t, x.currentTask(t)), jsonBytes(t, want)) {
			t.Fatal("Add changed Task state, assignment, rank or unrelated content")
		}
		all := readBlockers()
		if len(all) != 2 {
			t.Fatal("Add lost a Blocker")
		}
		for _, blocker := range all {
			if blocker.ID == technical.ID && !bytes.Equal(jsonBytes(t, blocker), jsonBytes(t, technical)) {
				t.Fatal("Add changed the original technical cause")
			}
		}
		comment := "The independent human check is complete"
		resolve := wc.TaskBlockerResolve{BlockerID: add.BlockerID, ResolutionComment: &comment}
		resolveKey := f.IdempotencyKey("blocked-human-cause-resolve")
		resolveBody := string(jsonBytes(t, map[string]any{"expected_version": added.Task.Version, "request": resolve}))
		resolvedReply := v.request(t, browser, "POST", v.taskPath()+"/blockers/resolve", resolveBody, resolveKey)
		var resolved wc.TaskBlockerMutation
		if resolvedReply.status != 200 || json.Unmarshal(resolvedReply.body, &resolved) != nil || resolved.Validate() != nil || resolved.Blocker.ID != add.BlockerID || resolved.Blocker.ResolvedAt == nil || resolved.Blocker.ResolutionComment == nil || *resolved.Blocker.ResolutionComment != comment {
			t.Fatal("non-last Human cause did not resolve", resolvedReply.status)
		}
		want.Version, want.UpdatedAt = added.Task.Version+1, resolved.Task.UpdatedAt
		if !bytes.Equal(jsonBytes(t, resolved.Task), jsonBytes(t, want)) || !bytes.Equal(jsonBytes(t, x.currentTask(t)), jsonBytes(t, want)) {
			t.Fatal("Resolve changed Task workflow")
		}
		all = readBlockers()
		if len(all) != 2 {
			t.Fatal("Resolve lost a Blocker")
		}
		for _, blocker := range all {
			want := technical
			if blocker.ID == resolved.Blocker.ID {
				want = resolved.Blocker
			}
			if !bytes.Equal(jsonBytes(t, blocker), jsonBytes(t, want)) {
				t.Fatal("resolution rewrote creation provenance or lost history")
			}
		}
		snapshot := func() string {
			t.Helper()
			var commands string
			if err := x.v.base.raw.QueryRow(ctxFor(t), `SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb)::text FROM agenteam_work.task_blocker_commands c WHERE project_id=$1::uuid`, before.ProjectID.String()).Scan(&commands); err != nil {
				t.Fatal(err)
			}
			return x.snapshot(t) + "\n" + commands
		}
		stable := snapshot()
		lookupBody := string(jsonBytes(t, map[string]any{"command": wc.TaskBlockerCommandAdd, "expected_version": before.Version, "request": add}))
		lookupReply := v.request(t, browser, "POST", v.taskPath()+"/blocker-commands/lookup", lookupBody, addKey)
		var found wc.TaskBlockerCommandLookup
		if lookupReply.status != 200 || json.Unmarshal(lookupReply.body, &found) != nil || found.Status != wc.LookupCommitted || found.Receipt == nil || !bytes.Equal(jsonBytes(t, *found.Receipt), addedReply.body) {
			t.Fatal("later resolution replaced the original Add receipt")
		}
		replay := v.request(t, browser, "POST", v.taskPath()+"/blockers", addBody, addKey)
		if replay.status != 200 || !bytes.Equal(replay.body, addedReply.body) || snapshot() != stable {
			t.Fatal("historical Lookup/replay rewrote Task or ledger")
		}
		technicalBody := string(jsonBytes(t, map[string]any{"expected_version": resolved.Task.Version, "request": wc.TaskBlockerResolve{BlockerID: technical.ID}}))
		v.requireProblem(t, v.request(t, browser, "POST", v.taskPath()+"/blockers/resolve", technicalBody, "standalone-technical-denied"), 503, f.DependencyUnbound)
		if snapshot() != stable {
			t.Fatal("rejected technical resolution changed persistent facts")
		}
		x.requireNoExecution(t)
	})
}
