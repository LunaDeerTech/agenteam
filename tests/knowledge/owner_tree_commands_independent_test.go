//go:build integration

package knowledge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

// Independent of the command HTTP implementation. Both identities are real
// Invitation/Redeem/Login users; the original handler, Account boundary and B02
// Service remain unchanged. Only the upstream Project Owner change is an
// explicit same-Store fixture fact, not an execution of a TransferOwner API.
func TestKnowledgeTreeCommandHTTPIndependentReceiptOwner(t *testing.T) {
	v := newTreeCommandHTTPFixture(t)
	a, b := v.ownerBrowser, v.otherBrowser
	if a.actor.Details().UserID == b.actor.Details().UserID || a.actor.Details().SessionID == b.actor.Details().SessionID {
		t.Fatal("independent current Owner identities are not distinct")
	}
	d := v.makeDocument(t, nil, "independent unchanged source")
	keyA := treeMeta(t).IdempotencyKey
	titleA := "original receipt A " + treeID[struct{}](t).String()
	bodyA := treeCommandJSON(t, map[string]any{"expected_version": "1", "title": titleA})
	first := treeCommandSuccess(t, v.post(t, d.ID, "rename", bodyA, keyA))
	confirmedA := v.current(t, d.ID)
	if confirmedA.ContentVersion != 2 || confirmedA.Title != titleA || confirmedA.ObjectID != d.ObjectID {
		t.Fatal("A's actual title command did not commit the expected document")
	}
	treeCommandDocument(t, first["document"], confirmedA)
	lookupPath := treeCommandHTTPPath(v.project, "/commands/lookup")
	lookupBody := func(body string) string {
		return treeCommandJSON(t, map[string]any{"command": "update", "document_id": d.ID.String(), "request": json.RawMessage(body)})
	}
	lookup := func(browser treeCommandHTTPBrowser, body string, key f.IdempotencyKey) treeCommandHTTPResponse {
		return v.request(t, browser, "POST", lookupPath, lookupBody(body), key)
	}
	checkReceipt := func(response treeCommandHTTPResponse, want kc.DocumentRef) {
		t.Helper()
		r := treeCommandLookupState(t, treeCommandSuccess(t, response), "committed")
		if len(r) != 3 || string(r["command"]) != `"update"` || string(r["changed"]) != "true" {
			t.Fatal("original title Lookup did not return its exact receipt union")
		}
		treeCommandDocument(t, r["document"], want)
	}
	checkReceipt(lookup(a, bodyA, keyA), confirmedA)
	rowA := independentTreeCommandRow(t, v, a, keyA)
	before := v.facts(t)
	activityB := v.activity(t, b.actor)
	changed := treeCommandJSON(t, map[string]any{"expected_version": "1", "title": "changed original intent"})
	independentTreeCommandRefusal(t, lookup(a, changed, keyA), f.IdempotencyKeyReused, titleA)
	if v.facts(t) != before || !v.activity(t, b.actor).Equal(activityB) || independentTreeCommandRow(t, v, a, keyA) != rowA {
		t.Fatal("changed-intent Lookup modified the original command or facts")
	}

	independentTreeCommandOwnerFact(t, v)
	v.ownerBrowser = b
	keyB := treeMeta(t).IdempotencyKey
	if keyB == keyA {
		t.Fatal("new Owner must use a distinct positive-control key")
	}
	titleB := "current receipt B " + treeID[struct{}](t).String()
	bodyB := treeCommandJSON(t, map[string]any{"expected_version": "2", "title": titleB})
	beforeB := v.facts(t)
	second := treeCommandSuccess(t, v.post(t, d.ID, "rename", bodyB, keyB))
	confirmedB := v.current(t, d.ID)
	if confirmedB.ContentVersion != 3 || confirmedB.Title != titleB || confirmedB.ObjectID != d.ObjectID {
		t.Fatal("the current new Owner did not complete an actual positive command")
	}
	treeCommandDocument(t, second["document"], confirmedB)
	checkReceipt(lookup(b, bodyB, keyB), confirmedB)
	afterB := v.facts(t)
	if afterB.Commands != beforeB.Commands+1 || afterB.Events != beforeB.Events+1 || afterB.Outbox != beforeB.Outbox+1 || afterB.Audit != beforeB.Audit || afterB.Cleanup != beforeB.Cleanup {
		t.Fatal("new Owner positive command did not produce exactly its own facts")
	}
	rowB := independentTreeCommandRow(t, v, b, keyB)
	activityA := v.activity(t, a.actor)

	// B now has current read/write authority. The exact old key and intent
	// must reach persisted actor/digest refusal, not an early non-Owner gate.
	independentTreeCommandRefusal(t, lookup(b, bodyA, keyA), f.IdempotencyKeyReused, titleA)
	independentTreeCommandRefusal(t, lookup(a, bodyA, keyA), f.NotFound, titleA)
	independentTreeCommandRefusal(t, v.request(t, a, "POST", treeCommandHTTPPath(v.project, "/"+d.ID.String()+"/rename"), bodyA, keyA), f.NotFound, titleA)
	checkReceipt(lookup(b, bodyB, keyB), confirmedB)
	if v.facts(t) != afterB || !v.activity(t, a.actor).Equal(activityA) || independentTreeCommandRow(t, v, a, keyA) != rowA || independentTreeCommandRow(t, v, b, keyB) != rowB {
		t.Fatal("old receipt refusal changed durable receipts, facts or either Session activity")
	}
	titleSameDocument(t, confirmedB, v.current(t, d.ID))
}

// Read-only snapshot of the actual committed row, never a fabricated receipt.
// Keep its private bytes in memory and report only fixed failure descriptions.
func independentTreeCommandRow(t *testing.T, v *treeCommandHTTPFixture, browser treeCommandHTTPBrowser, key f.IdempotencyKey) string {
	t.Helper()
	var raw string
	err := v.raw.QueryRow(knowledgeContext(t), `SELECT row_to_json(c)::text
 FROM agenteam_knowledge.commands c
 WHERE project_id=$1 AND command_name='update' AND command_key=$2
 AND actor_user_id=$3 AND state='completed' AND receipt IS NOT NULL`, v.project.String(), key.String(), browser.actor.Details().UserID).Scan(&raw)
	if err != nil || raw == "" {
		t.Fatal("original actor's committed command row is missing")
	}
	return raw
}

func independentTreeCommandRefusal(t *testing.T, response treeCommandHTTPResponse, code f.Code, privateTitle string) {
	t.Helper()
	treeCommandProblem(t, response, code)
	var body map[string]json.RawMessage
	if json.Unmarshal(response.body, &body) != nil || bytes.Contains(response.body, []byte(privateTitle)) {
		t.Fatal("refusal exposed original receipt material")
	}
	for _, field := range []string{"receipt", "document", "title", "object_id", "upload_id", "semantic_digest", "command_key"} {
		if _, exists := body[field]; exists {
			t.Fatal("refusal published a receipt or private field")
		}
	}
}

func independentTreeCommandOwnerFact(t *testing.T, v *treeCommandHTTPFixture) {
	t.Helper()
	userA, err := f.UserLock(v.ownerBrowser.actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	userB, err := f.UserLock(v.otherBrowser.actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	project, err := f.ProjectLock(v.project.String())
	if err != nil {
		t.Fatal(err)
	}
	locks, err := oc.NormalizeLocks([]f.LockRequest{{Key: userA, Mode: f.Exclusive}, {Key: userB, Mode: f.Exclusive}, {Key: project, Mode: f.Exclusive}})
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewRecoveryCause("knowledge.commandhttp.independent", knowledgeID(t), "")
	if err != nil {
		t.Fatal(err)
	}
	result := v.raw.WithinTx(knowledgeContext(t), cause, func(ctx context.Context, tx f.Tx) error {
		if err := v.raw.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if _, err := v.deps.Projects.RequireOwnerInTx(ctx, tx, v.ownerBrowser.actor, v.project, id.Mutate); err != nil {
			return err
		}
		if err := v.deps.Activity.(*account.Authority).RequireCurrentSession(ctx, tx, v.otherBrowser.actor); err != nil {
			return err
		}
		x, err := v.raw.InTx(tx)
		if err != nil {
			return err
		}
		n, err := x.Exec(ctx, `UPDATE agenteam_project.projects
 SET owner_user_id=$2,version=version+1,updated_at=clock_timestamp()
 WHERE id=$1 AND owner_user_id=$3 AND initialized_at IS NOT NULL AND lifecycle='active'`, v.project.String(), v.otherBrowser.actor.Details().UserID, v.ownerBrowser.actor.Details().UserID)
		if err != nil {
			return err
		}
		if n.RowsAffected() != 1 {
			return fmt.Errorf("owned upstream fixture change did not affect exactly one Project")
		}
		_, err = v.deps.Projects.RequireOwnerInTx(ctx, tx, v.otherBrowser.actor, v.project, id.Mutate)
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("same-Store upstream Owner fixture transaction did not commit", result.Fault())
	}
}
