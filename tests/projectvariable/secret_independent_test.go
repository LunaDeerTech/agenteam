//go:build integration

package projectvariable_test

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// This independent supplement uses the original real Account/Project/D10/D04/
// Audit/Outbox fixture. Its persistent Skills receipt remains a disclosed
// upstream fixture. No private plan, canonical row or receipt is manufactured.
func TestSecretVariableOwnerIndependentReadIsolation(t *testing.T) {
	t.Run("real_keyset_and_generation", func(t *testing.T) {
		v := newSecretOwnerFixture(t)
		a, p := v.ownerBrowser.actor, v.project.ID
		canary := []byte("independent-secret-page-material")
		defer clear(canary)
		var records []vc.SecretVariableMutation
		// Deliberately create outside C-name order; expected order below is
		// fixed by these inputs, not computed using the production query.
		for _, name := range []string{"PAGE_C", "PAGE_A", "PAGE_B"} {
			input := secretCreateInput(t, name, canary)
			created, err := v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, "independent-create-"+name, nil), p, input)
			if err != nil || created.Validate() != nil || !created.Fields().Changed {
				t.Fatal("actual Secret page publication failed", err)
			}
			records = append(records, created)
		}
		aID, bID, cID := records[1].Fields().Variable.Fields().ID, records[2].Fields().Variable.Fields().ID, records[0].Fields().Variable.Fields().ID
		counts := v.secretCounts(t)
		ordinary := v.createVariable(t, "PAGE_AB", "ordinary page value")
		if v.secretCounts(t) != counts {
			t.Fatal("ordinary producer changed Secret generation or facts")
		}
		fresh := v.login(t, v.ownerBrowser.email).actor
		if fresh.Details().SessionID == a.Details().SessionID || fresh.Details().UserID != a.Details().UserID {
			t.Fatal("continuation requires an actual new Session for the same User")
		}
		before := independentSecretReadFacts(t, v)
		first := independentSecretPage(t, v, a, f.PageRequest{Limit: 1}, aID)
		if first.NextCursor == "" {
			t.Fatal("three real rows did not produce a continuation")
		}
		second := independentSecretPage(t, v, a, f.PageRequest{Limit: 1, Cursor: first.NextCursor}, bID)
		if second.NextCursor == "" || second.NextCursor == first.NextCursor {
			t.Fatal("original seek did not advance")
		}
		third := independentSecretPage(t, v, a, f.PageRequest{Limit: 1, Cursor: second.NextCursor}, cID)
		if third.NextCursor != "" {
			t.Fatal("final real page retained a continuation")
		}
		continued := independentSecretPage(t, v, fresh, f.PageRequest{Limit: 2, Cursor: first.NextCursor}, bID, cID)
		if continued.NextCursor != "" {
			t.Fatal("new Session or changed limit lost the exact remaining rows")
		}
		public, err := v.service.ListVariables(ctxFor(t), a, p, f.PageRequest{Limit: 10})
		if err != nil || len(public.Items) != 1 || public.Items[0].Fields().ID != ordinary.Fields().ID {
			t.Fatal("ordinary and Secret actual row sets overlapped", err)
		}
		independentSecretFactsUnchanged(t, v, before)

		// A real no-op adds only the two command receipts; the old cursor must
		// still reach exactly the original remaining rows. Replay adds nothing.
		description := records[1].Fields().Variable.Fields().Description
		patch := secretUpdateInput(t, vc.SecretVariableUpdateFields{Description: &description})
		expected := f.Version(1)
		noOpMeta := meta(t, "independent-page-noop", &expected)
		noop, err := v.owner.UpdateSecretVariable(ctxFor(t), fresh, noOpMeta, p, aID, patch)
		if err != nil || noop.Validate() != nil || noop.Fields().Changed || noop.Fields().Variable.Fields().Version != 1 {
			t.Fatal("actual metadata no-op changed its result", err)
		}
		wantCounts := counts
		wantCounts[4]++
		wantCounts[5]++
		if v.secretCounts(t) != wantCounts || independentSecretReadFacts(t, v).activity != before.activity {
			t.Fatal("no-op changed generation or mutation facts")
		}
		before = independentSecretReadFacts(t, v)
		replayed, err := v.owner.UpdateSecretVariable(ctxFor(t), a, noOpMeta, p, aID, patch)
		if err != nil {
			t.Fatal("same User original no-op did not replay", err)
		}
		sameSecretReceipt(t, noop, replayed)
		independentSecretPage(t, v, fresh, f.PageRequest{Limit: 2, Cursor: first.NextCursor}, bID, cID)
		independentSecretFactsUnchanged(t, v, before)

		// Membership/count is unchanged, but a genuine name change moves B
		// after C. The old generation must fail before returning any page.
		renamed := "PAGE_Z"
		update := secretUpdateInput(t, vc.SecretVariableUpdateFields{Name: &renamed})
		changed, err := v.owner.UpdateSecretVariable(ctxFor(t), a, meta(t, "independent-page-rename", &expected), p, bID, update)
		if err != nil || changed.Validate() != nil || !changed.Fields().Changed || changed.Fields().Variable.Fields().Name != renamed {
			t.Fatal("actual rename failed", err)
		}
		for n := range wantCounts {
			wantCounts[n]++
		}
		if v.secretCounts(t) != wantCounts {
			t.Fatal("real rename did not advance exact Secret facts once")
		}
		v.assertSecretVersions(t, bID, 2, 1)
		before = independentSecretReadFacts(t, v)
		stale, err := v.owner.ListSecretVariables(ctxFor(t), a, p, f.PageRequest{Limit: 1, Cursor: first.NextCursor})
		requireCode(t, err, f.CursorStale)
		independentSecretEmptyPage(t, stale)
		ordered := independentSecretPage(t, v, a, f.PageRequest{Limit: 3}, aID, cID, bID)
		if ordered.NextCursor != "" {
			t.Fatal("unchanged three-member set did not end")
		}
		independentSecretFactsUnchanged(t, v, before)
		v.assertNoSecretLeak(t, canary, first, second, third, continued, noop, replayed, changed, ordered)
	})
	t.Run("current_owner_receipt_and_cursor", func(t *testing.T) {
		v := newSecretOwnerFixture(t)
		a, b, p := v.ownerBrowser.actor, v.otherBrowser.actor, v.project.ID
		canary := []byte("independent-secret-owner-material")
		defer clear(canary)
		input := secretCreateInput(t, "OWNER_A", canary)
		originalMeta := meta(t, "independent-owner-original", nil)
		original, err := v.owner.CreateSecretVariable(ctxFor(t), a, originalMeta, p, input)
		if err != nil || original.Validate() != nil {
			t.Fatal("original Owner publication failed", err)
		}
		target := input.Fields().ID
		ids := []vc.VariableID{target}
		for _, name := range []string{"OWNER_B", "OWNER_C"} {
			in := secretCreateInput(t, name, canary)
			result, err := v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, "independent-owner-"+name, nil), p, in)
			if err != nil || result.Validate() != nil {
				t.Fatal("actual Owner directory publication failed", err)
			}
			ids = append(ids, in.Fields().ID)
		}
		query := secretLookup(t, p, target, vc.SecretCreateCommand, originalMeta)
		looked, err := v.owner.LookupSecretVariableCommand(ctxFor(t), a, query)
		if err != nil || looked.Receipt() == nil {
			t.Fatal("original receipt positive control failed", err)
		}
		sameSecretReceipt(t, original, *looked.Receipt())
		first := independentSecretPage(t, v, a, f.PageRequest{Limit: 1}, target)
		if first.NextCursor == "" {
			t.Fatal("Owner binding requires an actual continuation")
		}
		counts := v.secretCounts(t)
		// Real old/new User EX and Project EX, current old Owner and new
		// Session checks, then an upstream SQL fact. Not OwnerTransfer API.
		v.transferOwner(t, p, a, b)
		if v.secretCounts(t) != counts {
			t.Fatal("upstream Owner fixture changed the Secret generation")
		}
		before := independentSecretReadFacts(t, v)
		current, err := v.owner.GetSecretVariable(ctxFor(t), b, p, target)
		if err != nil || current.Validate() != nil || current.Fields().ID != target {
			t.Fatal("new current Owner lacked ordinary metadata access", err)
		}
		bFirst := independentSecretPage(t, v, b, f.PageRequest{Limit: 1}, target)
		if bFirst.NextCursor == "" {
			t.Fatal("new Owner did not obtain its own continuation")
		}
		bLast := independentSecretPage(t, v, b, f.PageRequest{Limit: 2, Cursor: bFirst.NextCursor}, ids[1], ids[2])
		if bLast.NextCursor != "" {
			t.Fatal("new Owner's own continuation did not end")
		}
		// All rejections precede B's first write. Thus the foreign cursor
		// cannot be rejected merely because a mutation changed generation.
		wrongPage, err := v.owner.ListSecretVariables(ctxFor(t), b, p, f.PageRequest{Limit: 1, Cursor: first.NextCursor})
		requireCode(t, err, f.CursorInvalid)
		independentSecretEmptyPage(t, wrongPage)
		wrongReceipt, err := v.owner.LookupSecretVariableCommand(ctxFor(t), b, query)
		requireCode(t, err, f.IdempotencyKeyReused)
		if wrongReceipt.Receipt() != nil || wrongReceipt.Validate() == nil {
			t.Fatal("current Owner inherited another User's historical receipt")
		}
		wrongWrite, err := v.owner.CreateSecretVariable(ctxFor(t), b, originalMeta, p, input)
		requireCode(t, err, f.IdempotencyKeyReused)
		if wrongWrite.Validate() == nil {
			t.Fatal("current Owner claimed the previous writer's original intent")
		}
		oldPage, err := v.owner.ListSecretVariables(ctxFor(t), a, p, f.PageRequest{Limit: 1, Cursor: first.NextCursor})
		requireCode(t, err, f.NotFound)
		independentSecretEmptyPage(t, oldPage)
		oldReceipt, err := v.owner.LookupSecretVariableCommand(ctxFor(t), a, query)
		requireCode(t, err, f.NotFound)
		if oldReceipt.Receipt() != nil || oldReceipt.Validate() == nil {
			t.Fatal("old Owner retained historical permission")
		}
		independentSecretFactsUnchanged(t, v, before)

		description := "new Owner safe description"
		patch := secretUpdateInput(t, vc.SecretVariableUpdateFields{Description: &description})
		expected := f.Version(1)
		bMeta := meta(t, "independent-current-owner-update", &expected)
		updated, err := v.owner.UpdateSecretVariable(ctxFor(t), b, bMeta, p, target, patch)
		if err != nil || updated.Validate() != nil || !updated.Fields().Changed || updated.Fields().Variable.Fields().ID != target || updated.Fields().Variable.Fields().Version != 2 {
			t.Fatal("current Owner new identity could not update the original mapping", err)
		}
		for n := range counts {
			counts[n]++
		}
		if v.secretCounts(t) != counts {
			t.Fatal("current Owner update did not create exactly one set of facts")
		}
		v.assertSecretVersions(t, target, 2, 1)
		before = independentSecretReadFacts(t, v)
		bQuery := secretLookup(t, p, target, vc.SecretUpdateCommand, bMeta)
		bReceipt, err := v.owner.LookupSecretVariableCommand(ctxFor(t), b, bQuery)
		if err != nil || bReceipt.Receipt() == nil {
			t.Fatal("new writer could not recover its own safe receipt", err)
		}
		sameSecretReceipt(t, updated, *bReceipt.Receipt())
		denied, err := v.owner.LookupSecretVariableCommand(ctxFor(t), a, bQuery)
		requireCode(t, err, f.NotFound)
		if denied.Receipt() != nil || denied.Validate() == nil {
			t.Fatal("former Owner observed the new writer's receipt")
		}
		independentSecretFactsUnchanged(t, v, before)
		v.assertNoSecretLeak(t, canary, original, looked, first, current, bFirst, updated, bReceipt)
	})
}

func independentSecretPage(t *testing.T, v *secretOwnerFixture, actor i.Actor, request f.PageRequest, ids ...vc.VariableID) f.Page[vc.SecretVariable] {
	t.Helper()
	page, err := v.owner.ListSecretVariables(ctxFor(t), actor, v.project.ID, request)
	if err != nil || len(page.Items) != len(ids) {
		t.Fatal("wrong actual Secret page cardinality", err)
	}
	for n, item := range page.Items {
		if item.Validate() != nil || item.Fields().ID != ids[n] || item.Fields().ProjectID != v.project.ID || item.Fields().Type != vc.SecretVariableType {
			t.Fatal("actual Secret seek returned a duplicate, foreign or reordered item")
		}
	}
	return page
}

func independentSecretEmptyPage(t *testing.T, page f.Page[vc.SecretVariable]) {
	t.Helper()
	if len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatal("rejected read returned a partial page or cursor")
	}
}

type independentSecretFacts struct{ business, activity string }

func independentSecretReadFacts(t *testing.T, v *secretOwnerFixture) independentSecretFacts {
	t.Helper()
	var activity string
	if err := v.raw.QueryRow(ctxFor(t), `SELECT coalesce(jsonb_agg(jsonb_build_object('id',id,'at',last_activity_at) ORDER BY id),'[]'::jsonb)::text FROM agenteam_account.sessions`).Scan(&activity); err != nil {
		t.Fatal("actual Session activity observation failed", err)
	}
	return independentSecretFacts{v.secretSnapshot(t), activity}
}

func independentSecretFactsUnchanged(t *testing.T, v *secretOwnerFixture, before independentSecretFacts) {
	t.Helper()
	if independentSecretReadFacts(t, v) != before {
		t.Fatal("read/replay/refusal changed business or Session activity facts")
	}
}
