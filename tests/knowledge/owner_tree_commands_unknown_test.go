//go:build integration

package knowledge_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func TestKnowledgeTreeCommandHTTPUnknown(t *testing.T) {
	for _, commit := range []bool{false, true} {
		name := "not_forwarded"
		if commit {
			name = "committed_ack_lost"
		}
		t.Run(name, func(t *testing.T) {
			// Original accepted complete-frame proxy: the real Store alone
			// produces Unknown. No return value or receipt is manufactured.
			x, observer, proxy := unknownPublicationFixture(t)
			v := assembleTreeCommandHTTPFixture(t, x)
			d, target := v.makeDocument(t, nil, "unknown"), v.makeDocument(t, nil, "target")
			key := treeMeta(t).IdempotencyKey
			body := treeCommandJSON(t, map[string]any{"expected_parent_id": nil, "target_parent_id": target.ID})
			identity, err := kc.CommandIdentity(v.project, kc.Move, key)
			if err != nil {
				t.Fatal(err)
			}
			hook := &treeCommandTxStore{Store: v.raw, identity: identity, result: make(chan f.CommitResult, 1)}
			hook.after = func(ctx context.Context, tx f.Tx) error {
				pid, err := treeCommandPID(ctx, v.raw, tx)
				if err != nil {
					return err
				}
				return proxy.Arm(pid)
			}
			v.bind(t, concurrentService(t, x, hook))
			before := v.facts(t)
			out, done := treeCommandRun(t, v, treeCommandRequest(v, d, key, body))
			t.Cleanup(proxy.Release)
			runtimeAwait(t, proxy.Reached())
			var original f.CommitResult
			select {
			case original = <-hook.result:
			case <-time.After(3 * time.Second):
				t.Fatal("original Store Unknown missing")
			}
			if original.State() != f.Unknown || original.AttemptID().Validate() != nil || proxy.WriterPID() <= 0 {
				t.Fatal("original Unknown identity invalid")
			}
			response := treeCommandReply(t, out, done)
			treeCommandProblem(t, response, f.CommitUnknown)
			var problem struct {
				CommitState f.CommitState `json:"commit_state"`
				Retry       string        `json:"retry_hint"`
			}
			if json.Unmarshal(response.body, &problem) != nil || problem.CommitState != f.Unknown || problem.Retry != "lookup" {
				t.Fatal("HTTP lowered original Unknown")
			}
			if commit {
				proxy.Release()
				runtimeAwait(t, proxy.Committed())
				runtimeAwait(t, proxy.HeldJoined())
			} else {
				unknownRollbackWriter(t, observer, proxy)
			}
			want := "not_observed"
			if commit {
				want = "committed"
			}
			treeCommandLookupState(t, v.lookup(t, d.ID, "move", body, key), want)
			facts := v.facts(t)
			wantCommands := before.Commands
			if commit {
				wantCommands++
			}
			if facts.Commands != wantCommands || facts.Events != before.Events || facts.Outbox != before.Outbox || facts.Audit != before.Audit {
				t.Fatal("Lookup disagrees with actual backend outcome")
			}
			if !commit && !facts.Activity.Equal(before.Activity) {
				t.Fatal("rolled-back activity")
			}
			current := v.current(t, d.ID)
			if (current.ParentDocumentID != nil) != commit || commit && *current.ParentDocumentID != target.ID {
				t.Fatal("actual canonical parent disagrees with backend outcome")
			}
			treeCommandSuccess(t, v.post(t, d.ID, "move", body, key))
			final := v.facts(t)
			if final.Commands != before.Commands+1 || final.Events != before.Events || final.Outbox != before.Outbox || final.Audit != before.Audit {
				t.Fatal("original replay did not converge once")
			}
			treeCommandLookupState(t, v.lookup(t, d.ID, "move", body, key), "committed")
		})
	}
	t.Run("original_final_rollback_exposes_in_progress_not_a_receipt", func(t *testing.T) {
		v := newTreeCommandHTTPFixture(t)
		d := v.makeDocument(t, nil, "pending title")
		key := treeMeta(t).IdempotencyKey
		body := `{"expected_version":"1","title":"after retry"}`
		var activity *titleFailActivity
		service := publicationService(t, v.ownerTreeFixture, func(deps *knowledge.Dependencies) {
			activity = &titleFailActivity{ActivityAuthority: deps.Activity, err: f.NewFault(f.DependencyUnavailable, f.NotCommitted)}
			deps.Activity = activity
		})
		v.bind(t, service)
		before := v.facts(t)
		treeCommandProblem(t, v.post(t, d.ID, "rename", body, key), f.DependencyUnavailable)
		if activity.calls != 1 {
			t.Fatal("actual final Activity not reached")
		}
		titleSameDocument(t, d, v.current(t, d.ID))
		pending := v.facts(t)
		if pending.Commands != before.Commands+1 || pending.Events != before.Events+1 || pending.Outbox != before.Outbox || pending.Audit != before.Audit || !pending.Activity.Equal(before.Activity) {
			t.Fatal("plan/final rollback split facts")
		}
		treeCommandLookupState(t, v.lookup(t, d.ID, "update", body, key), "in_progress")
		if v.facts(t) != pending {
			t.Fatal("Lookup changed pending command")
		}
		activity.err = nil
		treeCommandSuccess(t, v.post(t, d.ID, "rename", body, key))
		receipt := treeCommandLookupState(t, v.lookup(t, d.ID, "update", body, key), "committed")
		treeCommandDocument(t, receipt["document"], v.current(t, d.ID))
		final := v.facts(t)
		if activity.calls != 2 || final.Commands != pending.Commands || final.Events != pending.Events || final.Outbox != pending.Outbox+1 || final.Audit != pending.Audit || !final.Activity.After(pending.Activity) {
			t.Fatal("same original pending command retry effects")
		}
	})
}
