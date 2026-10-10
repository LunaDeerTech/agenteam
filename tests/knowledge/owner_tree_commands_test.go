//go:build integration

package knowledge_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func treeCommandJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal("test input encoding", err)
	}
	return string(raw)
}

func treeCommandSuccess(t *testing.T, response treeCommandHTTPResponse) map[string]json.RawMessage {
	t.Helper()
	if response.aborted || response.status != http.StatusOK || response.header.Get("Content-Type") != "application/json" || response.header.Get("Cache-Control") != "no-store" || response.header.Get("Content-Length") != strconv.Itoa(len(response.body)) {
		t.Fatalf("original HTTP response: status=%d aborted=%v bytes=%d", response.status, response.aborted, len(response.body))
	}
	for _, forbidden := range []string{`"object_id"`, `"upload_id"`, `"semantic_digest"`, `"command_key"`, `"session_id"`} {
		if bytes.Contains(response.body, []byte(forbidden)) {
			t.Fatal("private field exposed")
		}
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(response.body, &out); err != nil || out == nil {
		t.Fatal("invalid complete success JSON")
	}
	return out
}

func treeCommandProblem(t *testing.T, response treeCommandHTTPResponse, code f.Code) {
	t.Helper()
	var problem struct {
		Code        f.Code        `json:"code"`
		CommitState f.CommitState `json:"commit_state"`
	}
	if response.aborted || response.status < 400 || response.header.Get("Content-Type") != "application/problem+json" || json.Unmarshal(response.body, &problem) != nil || problem.Code != code {
		t.Fatalf("safe Problem mismatch: status=%d aborted=%v want=%s", response.status, response.aborted, code)
	}
}

// Independent safe projection from the actual Service value; it never calls
// commandhttp's serializer and explicitly excludes the validated Object ID.
func treeCommandDocument(t *testing.T, raw json.RawMessage, d kc.DocumentRef) {
	t.Helper()
	if err := d.Validate(); err != nil {
		t.Fatal("actual service document invalid", err)
	}
	var actual map[string]any
	if json.Unmarshal(raw, &actual) != nil || len(actual) != 12 {
		t.Fatal("safe document closed shape")
	}
	var parent any
	if d.ParentDocumentID != nil {
		parent = d.ParentDocumentID.String()
	}
	c := d.CreatedBy.Details()
	creator := map[string]any{"kind": string(c.Kind)}
	switch c.Kind {
	case id.Human:
		creator["user_id"] = c.UserID.String()
	case id.AgentRun:
		creator["project_id"], creator["agent_id"], creator["execution_id"] = c.ProjectID.String(), c.AgentID.String(), c.ExecutionID.String()
	default:
		t.Fatal("invalid creator")
	}
	want := map[string]any{
		"id": d.ID.String(), "project_id": d.ProjectID.String(), "parent_document_id": parent,
		"title": d.Title, "content_version": fmt.Sprint(d.ContentVersion), "source_kind": string(d.SourceKind),
		"media_type": d.MediaType, "status": string(d.Status), "indexing_status": string(d.IndexingStatus),
		"created_by": creator, "created_at": d.CreatedAt.String(), "updated_at": d.UpdatedAt.String(),
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatal("safe document differs from actual Service value")
	}
}

type treeCommandFacts struct {
	Commands, Events, Audit, Outbox, Cleanup int
	Activity                                 time.Time
}

func (v *treeCommandHTTPFixture) facts(t *testing.T) treeCommandFacts {
	t.Helper()
	var out treeCommandFacts
	err := v.raw.QueryRow(knowledgeContext(t), `SELECT
 (SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_knowledge.command_events WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_knowledge.object_cleanup WHERE project_id=$1)`, v.project.String()).Scan(&out.Commands, &out.Events, &out.Audit, &out.Outbox, &out.Cleanup)
	if err != nil {
		t.Fatal("private fact counts", err)
	}
	out.Activity = v.activity(t, v.ownerBrowser.actor)
	return out
}

func (v *treeCommandHTTPFixture) makeDocument(t *testing.T, parent *kc.DocumentID, title string) kc.DocumentRef {
	t.Helper()
	body := "actual source for " + title
	d, err := v.service.CreateDocument(knowledgeContext(t), v.ownerBrowser.actor, treeMeta(t), kc.CreateRequest{ProjectID: v.project, DocumentID: treeID[kc.Document](t), ParentDocumentID: parent, Title: title}, publicationText(t, body))
	if err != nil {
		t.Fatal("actual B02 Create", err)
	}
	publicationFacts(t, v.ownerTreeFixture, d, []byte(body))
	return d
}

func (v *treeCommandHTTPFixture) current(t *testing.T, id kc.DocumentID) kc.DocumentRef {
	t.Helper()
	head, err := v.service.GetDocument(knowledgeContext(t), v.ownerBrowser.actor, v.project, id)
	if err != nil || head.Active == nil {
		t.Fatal("actual current Document", err)
	}
	return *head.Active
}

func (v *treeCommandHTTPFixture) post(t *testing.T, id kc.DocumentID, action, body string, key f.IdempotencyKey) treeCommandHTTPResponse {
	t.Helper()
	return v.request(t, v.ownerBrowser, "POST", treeCommandHTTPPath(v.project, "/"+id.String()+"/"+action), body, key)
}

func (v *treeCommandHTTPFixture) lookup(t *testing.T, id kc.DocumentID, command string, body string, key f.IdempotencyKey) map[string]json.RawMessage {
	t.Helper()
	input := treeCommandJSON(t, map[string]any{"command": command, "document_id": id.String(), "request": json.RawMessage(body)})
	return treeCommandSuccess(t, v.request(t, v.ownerBrowser, "POST", treeCommandHTTPPath(v.project, "/commands/lookup"), input, key))
}

func treeCommandLookupState(t *testing.T, got map[string]json.RawMessage, want string) map[string]json.RawMessage {
	t.Helper()
	var state string
	if len(got) != 2 || json.Unmarshal(got["state"], &state) != nil || state != want {
		t.Fatal("Lookup state/shape")
	}
	var receipt map[string]json.RawMessage
	if json.Unmarshal(got["receipt"], &receipt) != nil || (receipt != nil) != (want == "committed") {
		t.Fatal("Lookup receipt union")
	}
	return receipt
}

func TestKnowledgeTreeCommandHTTPMutations(t *testing.T) {
	v := newTreeCommandHTTPFixture(t)
	t.Run("rename_replay_noop_original_lookup", func(t *testing.T) {
		d := v.makeDocument(t, nil, "original")
		key := treeMeta(t).IdempotencyKey
		body := `{"expected_version":"1","title":"renamed 世界"}`
		before := v.facts(t)
		treeCommandLookupState(t, v.lookup(t, d.ID, "update", body, key), "not_observed")
		if v.facts(t) != before {
			t.Fatal("unobserved lookup mutated facts")
		}
		first := v.post(t, d.ID, "rename", body, key)
		after := v.current(t, d.ID)
		if after.ContentVersion != 2 || after.Title != "renamed 世界" || after.ObjectID != d.ObjectID {
			t.Fatal("title-only mutation")
		}
		treeCommandDocument(t, treeCommandSuccess(t, first)["document"], after)
		facts := v.facts(t)
		if facts.Commands != before.Commands+1 || facts.Events != before.Events+1 || facts.Outbox != before.Outbox+1 || facts.Cleanup != before.Cleanup {
			t.Fatal("rename exact new facts")
		}
		replay := v.post(t, d.ID, "rename", body, key)
		treeCommandSuccess(t, replay)
		if !bytes.Equal(first.body, replay.body) || v.facts(t) != facts {
			t.Fatal("replay changed receipt or facts")
		}
		receipt := treeCommandLookupState(t, v.lookup(t, d.ID, "update", body, key), "committed")
		treeCommandDocument(t, receipt["document"], after)
		if string(receipt["changed"]) != "true" {
			t.Fatal("changed lookup receipt")
		}
		treeCommandProblem(t, v.post(t, d.ID, "rename", `{"expected_version":"1","title":"other"}`, key), f.IdempotencyKeyReused)
		treeCommandProblem(t, v.post(t, d.ID, "rename", body, treeMeta(t).IdempotencyKey), f.VersionConflict)
		if v.facts(t) != facts {
			t.Fatal("rejected input changed facts")
		}
		noopKey := treeMeta(t).IdempotencyKey
		noopBody := `{"expected_version":"2","title":"renamed 世界"}`
		treeCommandDocument(t, treeCommandSuccess(t, v.post(t, d.ID, "rename", noopBody, noopKey))["document"], after)
		noop := v.facts(t)
		if noop.Commands != facts.Commands+1 || noop.Events != facts.Events || noop.Audit != facts.Audit || noop.Outbox != facts.Outbox || !noop.Activity.Equal(facts.Activity) {
			t.Fatal("no-op must persist receipt without new effects")
		}
		if string(treeCommandLookupState(t, v.lookup(t, d.ID, "update", noopBody, noopKey), "committed")["changed"]) != "false" {
			t.Fatal("no-op lookup")
		}
	})
	t.Run("move_root_expected_parent_cycle_and_replay", func(t *testing.T) {
		root, destination := v.makeDocument(t, nil, "move root"), v.makeDocument(t, nil, "destination")
		child := v.makeDocument(t, &root.ID, "move child")
		key := treeMeta(t).IdempotencyKey
		body := treeCommandJSON(t, map[string]any{"expected_parent_id": root.ID, "target_parent_id": destination.ID})
		before := v.facts(t)
		response := v.post(t, child.ID, "move", body, key)
		current := v.current(t, child.ID)
		if current.ContentVersion != child.ContentVersion || current.ParentDocumentID == nil || *current.ParentDocumentID != destination.ID {
			t.Fatal("move must preserve content version")
		}
		out := treeCommandSuccess(t, response)
		treeCommandDocument(t, out["document"], current)
		if string(out["changed"]) != "true" {
			t.Fatal("actual parent change")
		}
		facts := v.facts(t)
		if facts.Commands != before.Commands+1 || facts.Events != before.Events || facts.Outbox != before.Outbox || facts.Audit != before.Audit {
			t.Fatal("move exact facts")
		}
		if got := v.post(t, child.ID, "move", body, key); !bytes.Equal(got.body, response.body) {
			t.Fatal("move stable replay")
		}
		treeCommandDocument(t, treeCommandLookupState(t, v.lookup(t, child.ID, "move", body, key), "committed")["document"], current)
		treeCommandProblem(t, v.post(t, child.ID, "move", body, treeMeta(t).IdempotencyKey), f.VersionConflict)
		cycle := treeCommandJSON(t, map[string]any{"expected_parent_id": nil, "target_parent_id": child.ID})
		treeCommandProblem(t, v.post(t, destination.ID, "move", cycle, treeMeta(t).IdempotencyKey), f.InvalidArgument)
		if v.facts(t) != facts {
			t.Fatal("move replay or rejection effects")
		}
		rootBody := treeCommandJSON(t, map[string]any{"expected_parent_id": destination.ID, "target_parent_id": nil})
		treeCommandSuccess(t, v.post(t, child.ID, "move", rootBody, treeMeta(t).IdempotencyKey))
		if v.current(t, child.ID).ParentDocumentID != nil {
			t.Fatal("explicit null root move")
		}
	})
	t.Run("preview_stale_scope_delete_and_rotated_key_replay", func(t *testing.T) {
		root := v.makeDocument(t, nil, "delete root")
		child := v.makeDocument(t, &root.ID, "delete child")
		outside := v.makeDocument(t, nil, "outside")
		beforePreview := v.facts(t)
		preview := treeCommandSuccess(t, v.post(t, root.ID, "delete-preview", `{}`, ""))
		if v.facts(t) != beforePreview {
			t.Fatal("preview changed facts")
		}
		var token string
		var nodes []json.RawMessage
		if json.Unmarshal(preview["confirmation_token"], &token) != nil || token == "" || json.Unmarshal(preview["nodes"], &nodes) != nil || len(nodes) != 2 {
			t.Fatal("explicit Human preview")
		}
		staleBody := treeCommandJSON(t, map[string]any{"confirmation_token": token})
		treeCommandSuccess(t, v.post(t, child.ID, "rename", `{"expected_version":"1","title":"changed child"}`, treeMeta(t).IdempotencyKey))
		facts := v.facts(t)
		treeCommandProblem(t, v.post(t, root.ID, "delete-subtree", staleBody, treeMeta(t).IdempotencyKey), f.VersionConflict)
		if v.facts(t) != facts {
			t.Fatal("stale confirmation changed facts")
		}
		preview = treeCommandSuccess(t, v.post(t, root.ID, "delete-preview", `{}`, ""))
		if json.Unmarshal(preview["confirmation_token"], &token) != nil {
			t.Fatal("fresh preview token")
		}
		body := treeCommandJSON(t, map[string]any{"confirmation_token": token})
		key := treeMeta(t).IdempotencyKey
		response := v.post(t, root.ID, "delete-subtree", body, key)
		out := treeCommandSuccess(t, response)
		var deleted []kc.DocumentID
		wantDeleted := []kc.DocumentID{root.ID, child.ID}
		if wantDeleted[0].String() > wantDeleted[1].String() {
			wantDeleted[0], wantDeleted[1] = wantDeleted[1], wantDeleted[0]
		}
		if json.Unmarshal(out["deleted_ids"], &deleted) != nil || !reflect.DeepEqual(deleted, wantDeleted) || string(out["cleanup_pending"]) != "true" {
			t.Fatal("exact deleted subtree")
		}
		for _, id := range []kc.DocumentID{root.ID, child.ID} {
			head, err := v.service.GetDocument(knowledgeContext(t), v.ownerBrowser.actor, v.project, id)
			if err != nil || head.Deleted == nil || head.Active != nil {
				t.Fatal("actual tombstone", err)
			}
		}
		if v.current(t, outside.ID).ID != outside.ID {
			t.Fatal("outside subtree affected")
		}
		facts = v.facts(t)
		if a, _, pending := recoveryDeleteFacts(t, v.ownerTreeFixture, v.project); a != 1 || pending != 2 {
			t.Fatal("exact deletion audit and cleanup rows")
		}
		// Change only signing keys in a new real Service; the already completed
		// command must be replayed before old-token signature validation.
		rotated := publicationService(t, v.ownerTreeFixture, func(deps *knowledge.Dependencies) {
			var err error
			deps.Confirmations, err = kc.LoadConfirmationKeys(`{"format":1,"current_kid":"rotated","keys":[{"kid":"rotated","key_b64":"` + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, 32)) + `"}]}`)
			if err != nil {
				t.Fatal(err)
			}
		})
		v.bind(t, rotated)
		replay := v.post(t, root.ID, "delete-subtree", body, key)
		treeCommandSuccess(t, replay)
		if !bytes.Equal(replay.body, response.body) || v.facts(t) != facts {
			t.Fatal("rotated-key completed replay")
		}
		receipt := treeCommandLookupState(t, v.lookup(t, root.ID, "delete-subtree", body, key), "committed")
		if !bytes.Equal(receipt["deleted_ids"], out["deleted_ids"]) {
			t.Fatal("historical delete receipt")
		}
	})
	for _, material := range []string{v.ownerBrowser.cookie, v.ownerBrowser.csrf, "confirmation_token", "actual source for"} {
		if material != "" && strings.Contains(v.logs.text(), material) {
			t.Fatal("private material in HTTP logs")
		}
	}
}
