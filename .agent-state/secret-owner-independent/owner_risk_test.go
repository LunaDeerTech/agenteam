//go:build integration

package projectvariable_test

import (
	"context"
	"fmt"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// Added by an independent reviewer, without changing the Owner implementation
// or the existing fixture. Build through the documented Go overlay into the
// integration package. The persistent Skills initialization fixture remains
// disclosed; these tests do not establish HTTP, root, or Agent F1 binding.
func TestSecretOwnerIndependentRiskComplements(t *testing.T) {
	t.Run("real-keyset-and-generation", func(t *testing.T) {
		v := newSecretOwnerFixture(t)
		a, p := v.ownerBrowser.actor, v.project.ID
		inputs := make([]vc.SecretVariableCreate, 3)
		for n, name := range []string{"PAGE_A", "PAGE_C", "PAGE_E"} {
			inputs[n] = secretCreateInput(t, name, []byte("independent-owned-pagination-material"))
			if _, err := v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, fmt.Sprintf("independent-page-%d", n), nil), p, inputs[n]); err != nil {
				t.Fatal("create pagination input", err)
			}
		}
		first, err := v.owner.ListSecretVariables(ctxFor(t), a, p, f.PageRequest{Limit: 1})
		if err != nil || len(first.Items) != 1 || first.Items[0].Fields().ID != inputs[0].Fields().ID || first.NextCursor == "" {
			t.Fatal("first page did not produce the real Secret cursor", err)
		}
		// Real ordinary writes change their own generation. The same Secret
		// cursor must still work with a different limit and a fresh Session.
		v.createVariable(t, "PUBLIC_A", "public")
		v.createVariable(t, "PUBLIC_B", "public")
		fresh := v.login(t, v.ownerBrowser.email).actor
		if fresh.Details().SessionID == a.Details().SessionID {
			t.Fatal("fresh login did not produce a new Session")
		}
		assertRest := func(actor i.Actor) {
			t.Helper()
			page, err := v.owner.ListSecretVariables(ctxFor(t), actor, p, f.PageRequest{Limit: 2, Cursor: first.NextCursor})
			if err != nil || len(page.Items) != 2 || page.Items[0].Fields().ID != inputs[1].Fields().ID || page.Items[1].Fields().ID != inputs[2].Fields().ID || page.NextCursor != "" {
				t.Fatal("real keyset continuation lost, duplicated, or reordered a row", err)
			}
		}
		assertRest(fresh)
		publicPage, err := v.service.ListVariables(ctxFor(t), fresh, p, f.PageRequest{Limit: 1})
		if err != nil || publicPage.NextCursor == "" {
			t.Fatal("ordinary cursor control", err)
		}
		badSecret, err := v.owner.ListSecretVariables(ctxFor(t), fresh, p, f.PageRequest{Limit: 1, Cursor: publicPage.NextCursor})
		requireCode(t, err, f.CursorInvalid)
		if len(badSecret.Items) != 0 || badSecret.NextCursor != "" {
			t.Fatal("cross-type failure returned a partial Secret page")
		}
		_, err = v.service.ListVariables(ctxFor(t), fresh, p, f.PageRequest{Limit: 1, Cursor: first.NextCursor})
		requireCode(t, err, f.CursorInvalid)

		version := f.Version(1)
		description := inputs[1].Fields().Description
		noopInput := secretUpdateInput(t, vc.SecretVariableUpdateFields{Description: &description})
		noopMeta := meta(t, "independent-page-noop", &version)
		beforeNoop := v.secretCounts(t)
		noop, err := v.owner.UpdateSecretVariable(ctxFor(t), fresh, noopMeta, p, inputs[1].Fields().ID, noopInput)
		if err != nil || noop.Fields().Changed {
			t.Fatal("no-op control", err)
		}
		expected := beforeNoop
		expected[4]++
		expected[5]++
		if got := v.secretCounts(t); got != expected {
			t.Fatal("no-op changed generation or mutation facts", got, expected)
		}
		replayed, err := v.owner.UpdateSecretVariable(ctxFor(t), fresh, noopMeta, p, inputs[1].Fields().ID, noopInput)
		if err != nil {
			t.Fatal("original no-op replay", err)
		}
		sameSecretReceipt(t, noop, replayed)
		if got := v.secretCounts(t); got != expected {
			t.Fatal("replay changed persistent facts", got, expected)
		}
		assertRest(fresh)

		// Move a row across the keyset boundary. The old cursor must fail as a
		// whole page; a fresh cursor must expose the new canonical order.
		renamed := "PAGE_0"
		update := secretUpdateInput(t, vc.SecretVariableUpdateFields{Name: &renamed})
		if _, err := v.owner.UpdateSecretVariable(ctxFor(t), fresh, meta(t, "independent-page-rename", &version), p, inputs[2].Fields().ID, update); err != nil {
			t.Fatal("rename across page boundary", err)
		}
		stale, err := v.owner.ListSecretVariables(ctxFor(t), fresh, p, f.PageRequest{Limit: 2, Cursor: first.NextCursor})
		requireCode(t, err, f.CursorStale)
		if len(stale.Items) != 0 || stale.NextCursor != "" {
			t.Fatal("stale failure returned a partial Secret page")
		}
		current, err := v.owner.ListSecretVariables(ctxFor(t), fresh, p, f.PageRequest{Limit: 3})
		if err != nil || len(current.Items) != 3 || current.Items[0].Fields().ID != inputs[2].Fields().ID || current.Items[1].Fields().ID != inputs[0].Fields().ID || current.Items[2].Fields().ID != inputs[1].Fields().ID || current.NextCursor != "" {
			t.Fatal("fresh canonical order after rename", err)
		}
	})

	t.Run("owned-reference-blocks-delete", func(t *testing.T) {
		v := newSecretOwnerFixture(t)
		a, p := v.ownerBrowser.actor, v.project.ID
		input := secretCreateInput(t, "REFERENCE_GUARD", []byte("independent-owned-reference-material"))
		target := input.Fields().ID
		if _, err := v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, "independent-reference-create", nil), p, input); err != nil {
			t.Fatal("reference control create", err)
		}
		owner := id[i.Agent](t)
		// This is an explicit row fixture in the true owned table, under the
		// same Project exclusion used by Owner mutations. It proves deletion
		// protection, not an implemented Agent retain/release authority.
		v.tx(t, ownerLocks(a, p), func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
			tag, err := x.Exec(ctx, `INSERT INTO agenteam_projectvariable.secret_references(project_id,variable_id,owner_kind,owner_id,owner_version) VALUES($1,$2,'agent',$3,1)`, p.String(), target.String(), owner.String())
			if err == nil && tag.RowsAffected() != 1 {
				return fmt.Errorf("reference fixture affected %d rows", tag.RowsAffected())
			}
			return err
		})
		before := v.secretSnapshot(t)
		version := f.Version(1)
		deleteMeta := meta(t, "independent-reference-delete", &version)
		_, err := v.owner.DeleteSecretVariable(ctxFor(t), a, deleteMeta, p, target)
		requireCode(t, err, f.ResourceBusy)
		if before != v.secretSnapshot(t) {
			t.Fatal("reference rejection changed two-domain material or facts")
		}
		lookup, err := v.owner.LookupSecretVariableCommand(ctxFor(t), a, secretLookup(t, p, target, vc.SecretDeleteCommand, deleteMeta))
		if err != nil || lookup.Status() != vc.SecretLookupNotObserved || lookup.Receipt() != nil {
			t.Fatal("rejected delete published a receipt", err)
		}
		v.tx(t, ownerLocks(a, p), func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
			tag, err := x.Exec(ctx, `DELETE FROM agenteam_projectvariable.secret_references WHERE project_id=$1 AND variable_id=$2 AND owner_kind='agent' AND owner_id=$3`, p.String(), target.String(), owner.String())
			if err == nil && tag.RowsAffected() != 1 {
				return fmt.Errorf("original reference missing before fixture release: %d", tag.RowsAffected())
			}
			return err
		})
		deleted, err := v.owner.DeleteSecretVariable(ctxFor(t), a, deleteMeta, p, target)
		if err != nil || deleted.Fields().Deleted == nil || deleted.Fields().Deleted.Version != 2 {
			t.Fatal("same command after reference release", err)
		}
		if got := v.secretCounts(t); got != [6]int64{3, 2, 2, 2, 2, 2} {
			t.Fatal("reference release must permit exactly one deletion", got)
		}
	})
}
