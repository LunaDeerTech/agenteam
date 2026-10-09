//go:build integration

package knowledge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func knowledgeOwnerHTTPTop(t *testing.T) {
	t.Helper()
	started := time.Now()
	// Registered first, so this includes all actual service/Store fixture tails.
	t.Cleanup(func() {
		if time.Since(started) > 120*time.Second {
			t.Error("Knowledge Owner HTTP top including cleanup exceeded 120s")
		}
	})
}
func (v *knowledgeOwnerHTTPFixture) create(t *testing.T, parent *kc.DocumentID, title string) kc.DocumentRef {
	t.Helper()
	d, err := v.service.CreateDocument(knowledgeContext(t), v.ownerBrowser.actor, treeMeta(t), kc.CreateRequest{ProjectID: v.project, DocumentID: treeID[kc.Document](t), ParentDocumentID: parent, Title: title}, publicationText(t, "private-body-"+knowledgeID(t)))
	if err != nil {
		t.Fatal("real Knowledge publication", err)
	}
	return d
}
func (r knowledgeOwnerHTTPResponse) want(t *testing.T, status int) map[string]any {
	t.Helper()
	if r.aborted || r.status != status {
		t.Fatalf("HTTP status=%d aborted=%t, want=%d", r.status, r.aborted, status)
	}
	if r.header.Get("Cache-Control") != "no-store" || r.header.Get("X-Request-ID") == "" {
		t.Fatal("missing security/identity headers")
	}
	if r.header.Get("Content-Length") != strconv.Itoa(len(r.body)) {
		t.Fatal("incorrect complete representation length")
	}
	var body map[string]any
	if json.Unmarshal(r.body, &body) != nil {
		t.Fatal("invalid complete response JSON")
	}
	return body
}
func knowledgeOwnerHTTPItems(t *testing.T, body map[string]any) []any {
	t.Helper()
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatal("items not non-null array")
	}
	return items
}
func knowledgeOwnerHTTPIDs(t *testing.T, items []any) []string {
	t.Helper()
	out := make([]string, len(items))
	for i, item := range items {
		d, ok := item.(map[string]any)
		if !ok {
			t.Fatal("invalid document")
		}
		out[i], ok = d["id"].(string)
		if !ok {
			t.Fatal("missing document ID")
		}
	}
	return out
}
func knowledgeOwnerHTTPWantIDs(t *testing.T, items []any, expected ...kc.DocumentID) {
	t.Helper()
	got := knowledgeOwnerHTTPIDs(t, items)
	if len(got) != len(expected) {
		t.Fatalf("item count=%d want=%d", len(got), len(expected))
	}
	for i, id := range expected {
		if got[i] != id.String() {
			t.Fatal("wrong ordered item identity", i)
		}
	}
}
func (v *knowledgeOwnerHTTPFixture) facts(t *testing.T) string {
	t.Helper()
	var out string
	err := v.raw.QueryRow(knowledgeContext(t), `SELECT jsonb_build_object(
 'knowledge_commands',(SELECT count(*) FROM agenteam_knowledge.commands WHERE project_id=$1),
 'knowledge_events',(SELECT count(*) FROM agenteam_knowledge.command_events WHERE project_id=$1),
 'audit',(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1),
 'outbox',(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1),
 'activity',(SELECT coalesce(jsonb_agg(jsonb_build_object('id',id,'at',last_activity_at) ORDER BY id),'[]') FROM agenteam_account.sessions))::text`, v.project.String()).Scan(&out)
	if err != nil {
		t.Fatal("read-only facts snapshot", err)
	}
	return out
}

type knowledgeOwnerHTTPSchemaCase struct {
	Label  string `json:"label"`
	Schema string `json:"schema"`
	Value  any    `json:"value"`
	Valid  bool   `json:"valid"`
}

func knowledgeOwnerHTTPValidateSchema(t *testing.T, cases []knowledgeOwnerHTTPSchemaCase) {
	t.Helper()
	python := os.Getenv("AGENTEAM_KNOWLEDGE_HTTP_SCHEMA_PYTHON")
	if python == "" {
		t.Fatal("explicit installed local Schema interpreter required")
	}
	input, err := json.Marshal(map[string]any{"cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../.agent-state/knowledge-owner-read/schema-controls.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(knowledgeContext(t), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, script)
	cmd.Stdin = bytes.NewReader(input)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("actual response local Schema: %v %s", err, raw)
	}
	t.Log(string(raw))
}

func TestKnowledgeOwnerReadHTTPMetadata(t *testing.T) {
	knowledgeOwnerHTTPTop(t)
	v := newKnowledgeOwnerHTTPFixture(t)
	a := v.create(t, nil, "A root")
	b := v.create(t, &a.ID, "B child")
	c := v.create(t, &b.ID, "C literal %_! needle")
	z := v.create(t, nil, "Z root")
	base := knowledgeOwnerHTTPPath(v.project, "")
	before := v.facts(t)
	var schemas []knowledgeOwnerHTTPSchemaCase
	get := func(t *testing.T, suffix, schema string) map[string]any {
		r := v.request(t, v.ownerBrowser, "GET", base+suffix, "", "")
		body := r.want(t, 200)
		if r.header.Get("Content-Type") != "application/json" {
			t.Fatal("wrong success representation type")
		}
		if bytes.Contains(r.body, []byte(`"object_id"`)) || bytes.Contains(r.body, []byte("private-body-")) {
			t.Fatal("internal content disclosed")
		}
		head := v.request(t, v.ownerBrowser, "HEAD", base+suffix, "", "")
		if head.aborted || head.status != 200 || len(head.body) != 0 || head.header.Get("Content-Length") != r.header.Get("Content-Length") || head.header.Get("Content-Type") != r.header.Get("Content-Type") || head.header.Get("Cache-Control") != "no-store" {
			t.Fatal("HEAD differs from authorized complete representation")
		}
		schemas = append(schemas, knowledgeOwnerHTTPSchemaCase{schema, schema, body, true})
		return body
	}
	var firstCursor string
	t.Run("five_endpoints_full_tree_and_literal_search", func(t *testing.T) {
		list := get(t, "", "DocumentPage")
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, list), a.ID, b.ID, c.ID, z.ID)
		head := get(t, "/"+c.ID.String(), "DocumentHead")
		active, ok := head["active"].(map[string]any)
		if !ok || len(active) != 12 || active["id"] != c.ID.String() || active["content_version"] != "1" {
			t.Fatal("safe canonical active metadata")
		}
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, get(t, "/children?parent_document_id=null", "DocumentPage")), a.ID, z.ID)
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, get(t, "/children?parent_document_id="+a.ID.String(), "DocumentPage")), b.ID)
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, get(t, "/"+c.ID.String()+"/ancestors", "Ancestors")), a.ID, b.ID)
		search := get(t, "/search-titles?title_query="+url.QueryEscape("%_!"), "TitlePage")
		hits := knowledgeOwnerHTTPItems(t, search)
		if len(hits) != 1 {
			t.Fatal("literal wildcard characters broadened search")
		}
		hit := hits[0].(map[string]any)
		if hit["document"].(map[string]any)["id"] != c.ID.String() {
			t.Fatal("wrong literal search document")
		}
		knowledgeOwnerHTTPWantIDs(t, hit["ancestors"].([]any), a.ID, b.ID)
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, get(t, "/"+a.ID.String()+"/ancestors", "Ancestors")))
	})
	t.Run("keyset_limit_and_query_binding", func(t *testing.T) {
		page := get(t, "?limit=2", "DocumentPage")
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, page), a.ID, b.ID)
		firstCursor, _ = page["next_cursor"].(string)
		if firstCursor == "" {
			t.Fatal("missing first cursor")
		}
		next := get(t, "?limit=200&cursor="+url.QueryEscape(firstCursor), "DocumentPage")
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, next), c.ID, z.ID)
		if _, exists := next["next_cursor"]; exists {
			t.Fatal("terminal page has cursor")
		}
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, get(t, "?source_kind=text&media_type=text%2Fplain&indexing_status=pending&title_query=B", "DocumentPage")), b.ID)
		for _, suffix := range []string{"?limit=2&title_query=A&cursor=", "/children?parent_document_id=null&limit=2&cursor=", "/search-titles?limit=2&cursor="} {
			v.request(t, v.ownerBrowser, "GET", base+suffix+url.QueryEscape(firstCursor), "", "").want(t, 400)
		}
		tampered := firstCursor[:len(firstCursor)-1] + "!"
		v.request(t, v.ownerBrowser, "GET", base+"?cursor="+url.QueryEscape(tampered), "", "").want(t, 400)
	})
	t.Run("empty_arrays_and_no_read_side_effects", func(t *testing.T) {
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, get(t, "?title_query=never-present", "DocumentPage")))
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, get(t, "/children?parent_document_id="+c.ID.String(), "DocumentPage")))
		if got := v.facts(t); got != before {
			t.Fatal("GET/HEAD changed commands/Audit/Outbox/session Activity")
		}
	})
	t.Run("canonical_delete_minimal_tombstone", func(t *testing.T) {
		preview, err := v.service.PrepareDeleteSubtree(knowledgeContext(t), v.ownerBrowser.actor, v.project, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		deleted, err := v.service.DeleteSubtree(knowledgeContext(t), v.ownerBrowser.actor, treeMeta(t), v.project, c.ID, preview.Confirmation)
		if err != nil || len(deleted.DeletedIDs) != 1 {
			t.Fatal("real subtree delete", err)
		}
		deletedBefore := v.facts(t)
		head := get(t, "/"+c.ID.String(), "DocumentHead")
		tombstone, ok := head["deleted"].(map[string]any)
		if !ok || len(head) != 1 || len(tombstone) != 4 || tombstone["id"] != c.ID.String() {
			t.Fatal("invalid minimal tombstone")
		}
		for _, suffix := range []string{"/" + c.ID.String() + "/ancestors", "/children?parent_document_id=" + c.ID.String()} {
			v.request(t, v.ownerBrowser, "GET", base+suffix, "", "").want(t, 404)
		}
		knowledgeOwnerHTTPWantIDs(t, knowledgeOwnerHTTPItems(t, get(t, "/children?parent_document_id="+b.ID.String(), "DocumentPage")))
		if got := v.facts(t); got != deletedBefore {
			t.Fatal("tombstone reads changed facts")
		}
	})
	knowledgeOwnerHTTPValidateSchema(t, schemas)
}

func TestKnowledgeOwnerReadHTTPCurrentAuthority(t *testing.T) {
	knowledgeOwnerHTTPTop(t)
	v := newKnowledgeOwnerHTTPFixture(t)
	a := v.create(t, nil, "A authority")
	v.create(t, nil, "B authority")
	base := knowledgeOwnerHTTPPath(v.project, "")
	first := v.request(t, v.ownerBrowser, "GET", base+"?limit=1", "", "").want(t, 200)
	cursor, ok := first["next_cursor"].(string)
	if !ok || cursor == "" {
		t.Fatal("missing authority cursor")
	}
	t.Run("foreign_admin_missing_and_current_project", func(t *testing.T) {
		for _, browser := range []knowledgeOwnerHTTPBrowser{v.otherBrowser, v.adminBrowser} {
			for _, suffix := range []string{"", "/" + a.ID.String(), "/children?parent_document_id=null", "/" + a.ID.String() + "/ancestors", "/search-titles"} {
				v.request(t, browser, "GET", base+suffix, "", "").want(t, 404)
			}
		}
		v.request(t, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(treeID[identity.Project](t), ""), "", "").want(t, 404)
		foreign := v.ownerTreeFixture.project(t, v.ownerBrowser.actor, true)
		v.request(t, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(foreign, "/"+a.ID.String()), "", "").want(t, 404)
		v.request(t, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(foreign, "?cursor="+url.QueryEscape(cursor)), "", "").want(t, 400)
		uninitialized := v.ownerTreeFixture.project(t, v.ownerBrowser.actor, false)
		v.request(t, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(uninitialized, ""), "", "").want(t, 409)
	})
	t.Run("real_boundary_safe_errors_and_head", func(t *testing.T) {
		before := v.facts(t)
		for _, tc := range []struct {
			method, suffix, body string
			status               int
		}{{"GET", "?unknown_PRIVATE_QUERY_canary=x", "", 400}, {"GET", "?limit=01", "", 400}, {"GET", "?limit=1&%6cimit=2", "", 400}, {"GET", "/children", "", 400}, {"GET", "/children?parent_document_id=", "", 400}, {"GET", "", "x", 400}, {"POST", "", "", 405}} {
			r := v.request(t, v.ownerBrowser, tc.method, base+tc.suffix, tc.body, "")
			r.want(t, tc.status)
			if bytes.Contains(r.body, []byte("PRIVATE_QUERY_canary")) {
				t.Fatal("raw query echoed")
			}
			if tc.status == 405 && r.header.Get("Allow") != "GET, HEAD" {
				t.Fatal("wrong allowed methods")
			}
		}
		unauth := v.request(t, knowledgeOwnerHTTPBrowser{}, "GET", base, "", "")
		unauth.want(t, 401)
		r := knowledgeOwnerHTTPRequest(knowledgeContext(t), v.ownerBrowser, "GET", base, "", "")
		r.Header.Set("Origin", "https://foreign.example.test")
		v.serve(r).want(t, 403)
		missing := base + "/" + treeID[kc.Document](t).String()
		get := v.request(t, v.ownerBrowser, "GET", missing, "", "")
		get.want(t, 404)
		head := v.request(t, v.ownerBrowser, "HEAD", missing, "", "")
		if head.aborted || head.status != 404 || len(head.body) != 0 || head.header.Get("Content-Length") == "0" || head.header.Get("Content-Type") != "application/problem+json" {
			t.Fatal("HEAD error representation/body")
		}
		if got := v.facts(t); got != before {
			t.Fatal("read rejection changed durable facts")
		}
	})
	t.Run("new_session_same_user_then_formal_logout_rejects_cursor", func(t *testing.T) {
		renewed := v.login(t, v.ownerBrowser.email)
		v.request(t, renewed, "GET", base+"?limit=100&cursor="+url.QueryEscape(cursor), "", "").want(t, 200)
		if err := v.core.Logout(knowledgeContext(t), account.LogoutRequest{Actor: renewed.actor, Key: f.IdempotencyKey(knowledgeID(t))}); err != nil {
			t.Fatal("formal Logout", err)
		}
		before := v.facts(t)
		v.request(t, renewed, "GET", base+"?cursor="+url.QueryEscape(cursor), "", "").want(t, 401)
		if got := v.facts(t); got != before {
			t.Fatal("revoked read changed durable facts")
		}
	})
	t.Run("archived_project_read_gate", func(t *testing.T) {
		// Explicit upstream lifecycle fact; no claim that lifecycle workers ran.
		v.archive(t, v.project)
		before := v.facts(t)
		v.request(t, v.ownerBrowser, "GET", base+"?cursor="+url.QueryEscape(cursor), "", "").want(t, 200)
		if got := v.facts(t); got != before {
			t.Fatal("archived read changed durable facts")
		}
	})
	t.Run("deleting_requires_current_read_gate", func(t *testing.T) {
		knowledgeOwnerHTTPDeletingFixture(t, v)
		before := v.facts(t)
		for _, suffix := range []string{"", "/" + a.ID.String(), "/children?parent_document_id=null", "/" + a.ID.String() + "/ancestors", "/search-titles"} {
			v.request(t, v.ownerBrowser, "GET", base+suffix, "", "").want(t, 409)
		}
		if got := v.facts(t); got != before {
			t.Fatal("Deleting read rejection changed facts")
		}
	})
	if strings.Contains(v.logs.text(), v.ownerBrowser.cookie) || strings.Contains(v.logs.text(), v.ownerBrowser.csrf) || strings.Contains(v.logs.text(), "PRIVATE_QUERY_canary") {
		t.Fatal("private request material reached safe logs")
	}
}

// Complete, FK-valid upstream accepted Delete fact under the original gates.
// No lifecycle provider is simulated and no BeginDelete execution is claimed.
func knowledgeOwnerHTTPDeletingFixture(t *testing.T, v *knowledgeOwnerHTTPFixture) {
	t.Helper()
	const domain pc.ParticipantName = "knowledge"
	manifest, e := pc.NewRequiredManifest([]pc.ParticipantRegistration{
		{Name: domain, ContractVersion: 1, OwnerModule: "knowledge"},
		{Name: pc.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", CleanupAfter: []pc.ParticipantName{domain}},
		{Name: pc.SecretParticipant, ContractVersion: 1, OwnerModule: "secret"},
		{Name: pc.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", CleanupAfter: []pc.ParticipantName{domain, pc.ArtifactObjectParticipant, pc.SecretParticipant}},
		{Name: pc.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", CleanupAfter: []pc.ParticipantName{pc.OutboxParticipant}},
	})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(manifest.Entries())
	if e != nil {
		t.Fatal(e)
	}
	digest, e := manifest.Digest()
	if e != nil {
		t.Fatal(e)
	}
	operation := treeID[pc.Operation](t)
	cause, e := f.NewRecoveryCause("knowledge.http.fixture", knowledgeID(t), "")
	if e != nil {
		t.Fatal(e)
	}
	user, e := f.UserLock(v.ownerBrowser.actor.Details().UserID)
	if e != nil {
		t.Fatal(e)
	}
	project, e := f.ProjectLock(v.project.String())
	if e != nil {
		t.Fatal(e)
	}
	result := v.raw.WithinTx(knowledgeContext(t), cause, func(ctx context.Context, tx f.Tx) error {
		if e := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Exclusive}, {Key: project, Mode: f.Exclusive}}); e != nil {
			return e
		}
		q, e := v.raw.InTx(tx)
		if e != nil {
			return e
		}
		var version int64
		if e = q.QueryRow(ctx, `SELECT version FROM agenteam_project.projects WHERE id=$1 AND owner_user_id=$2 AND lifecycle='archived' AND initialized_at IS NOT NULL`, v.project.String(), v.ownerBrowser.actor.Details().UserID).Scan(&version); e != nil {
			return e
		}
		if _, e = q.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,'delete',$4,'accepted',1,$5::jsonb,$6,clock_timestamp(),clock_timestamp())`, operation.String(), v.project.String(), v.ownerBrowser.actor.Details().UserID, version+1, raw, digest.String()); e != nil {
			return e
		}
		for _, entry := range manifest.Entries() {
			if _, e = q.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required','required',1)`, operation.String(), string(entry.Name), int64(entry.ContractVersion)); e != nil {
				return e
			}
		}
		tag, e := q.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='deleting',current_lifecycle_operation_id=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND version=$3 AND lifecycle='archived'`, v.project.String(), operation.String(), version)
		if e == nil && tag.RowsAffected() != 1 {
			return f.NewFault(f.InvalidState, f.NotCommitted)
		}
		return e
	})
	if result.State() != f.Committed {
		t.Fatal("complete upstream Delete fixture did not commit", result.Fault())
	}
}
