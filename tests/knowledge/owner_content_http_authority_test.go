//go:build integration

package knowledge_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func TestKnowledgeOwnerContentHTTPCurrentAuthority(t *testing.T) {
	knowledgeOwnerHTTPTop(t)
	v := newContentHTTPFixture(t)
	const privateText = "private canonical content authority canary"
	doc := contentHTTPCreate(t, v, kc.PlainText, "Current authority", privateText)
	path := contentHTTPPath(v, doc.ID, "")
	t.Run("real_account_before_query_and_safe_rejection", func(t *testing.T) {
		before := v.facts(t)
		v.request(t, knowledgeOwnerHTTPBrowser{}, "POST", path+"?not_a_field=PRIVATE_CONTENT_QUERY", "", "").want(t, 401)
		request := knowledgeOwnerHTTPRequest(knowledgeContext(t), v.ownerBrowser, "GET", path, "", "")
		request.Header.Set("Origin", "https://foreign.example.test")
		v.serve(request).want(t, 403)
		for _, browser := range []knowledgeOwnerHTTPBrowser{v.otherBrowser, v.adminBrowser} {
			response := v.request(t, browser, "GET", path, "", "")
			response.want(t, 404)
			if bytes.Contains(response.body, []byte(privateText)) {
				t.Fatal("non-owner observed canonical content")
			}
		}
		for _, header := range []string{"Content-Encoding", "Range", "If-Range"} {
			r := knowledgeOwnerHTTPRequest(knowledgeContext(t), v.ownerBrowser, "GET", path, "", "")
			r.Header.Set(header, "PRIVATE_CONTENT_HEADER")
			response := v.serve(r)
			response.want(t, 400)
			if bytes.Contains(response.body, []byte("PRIVATE_CONTENT_HEADER")) {
				t.Fatal("raw rejected header escaped")
			}
		}
		v.request(t, v.ownerBrowser, "GET", path+"?not_a_field=PRIVATE_CONTENT_QUERY", "", "").want(t, 400)
		if v.facts(t) != before {
			t.Fatal("rejected content read changed business facts")
		}
	})
	t.Run("new_actual_login_then_formal_logout", func(t *testing.T) {
		renewed := v.login(t, v.ownerBrowser.email)
		contentHTTPText(t, v.request(t, renewed, "GET", path, "", ""), doc, privateText, int64(len(privateText)), false)
		if err := v.core.Logout(knowledgeContext(t), account.LogoutRequest{Actor: renewed.actor, Key: f.IdempotencyKey(knowledgeID(t))}); err != nil {
			t.Fatal("formal Logout", err)
		}
		before := v.facts(t)
		response := v.request(t, renewed, "GET", path, "", "")
		response.want(t, 401)
		if bytes.Contains(response.body, []byte(privateText)) || v.facts(t) != before {
			t.Fatal("revoked content read leaked bytes or changed facts")
		}
		contentHTTPNoReader(t, v, doc)
	})
	t.Run("uninitialized_archived_and_deleting_read_gate", func(t *testing.T) {
		uninitialized := v.ownerTreeFixture.project(t, v.ownerBrowser.actor, false)
		v.request(t, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(uninitialized, "/"+doc.ID.String()+"/content"), "", "").want(t, 409)
		// Both transitions are explicit current upstream SQL lifecycle facts.
		// This test does not claim Archive/BeginDelete providers or workers ran.
		v.archive(t, v.project)
		before := v.facts(t)
		contentHTTPText(t, v.request(t, v.ownerBrowser, "GET", path, "", ""), doc, privateText, int64(len(privateText)), false)
		if v.facts(t) != before {
			t.Fatal("archived content read changed business facts")
		}
		knowledgeOwnerHTTPDeletingFixture(t, v)
		before = v.facts(t)
		response := v.request(t, v.ownerBrowser, "GET", path, "", "")
		body := response.want(t, 409)
		if body["code"] != string(f.ProjectNotActive) || bytes.Contains(response.body, []byte(privateText)) || v.facts(t) != before {
			t.Fatal("Deleting lost its current read gate")
		}
		contentHTTPNoReader(t, v, doc)
	})
	t.Run("owner_changed_sql_fact_requires_new_authority", func(t *testing.T) {
		x := newContentHTTPFixture(t)
		d := contentHTTPCreate(t, x, kc.Markdown, "Owner change", privateText)
		// Same Store, original User/Project locks, SQL fixture only. This is not
		// a production OwnerTransfer command or API acceptance.
		result, done := knowledgeOwnerHTTPOwnerWriter(t, x, nil, nil, nil)
		knowledgeOwnerHTTPCommit(t, result, done)
		before := x.facts(t)
		old := x.request(t, x.ownerBrowser, "GET", contentHTTPPath(x, d.ID, ""), "", "")
		old.want(t, 404)
		if bytes.Contains(old.body, []byte(privateText)) {
			t.Fatal("old owner retained canonical content authority")
		}
		contentHTTPText(t, x.request(t, x.otherBrowser, "GET", contentHTTPPath(x, d.ID, ""), "", ""), d, privateText, int64(len(privateText)), false)
		contentHTTPNoReader(t, x, d)
		if x.facts(t) != before {
			t.Fatal("owner-change content reads changed business facts")
		}
	})
	for _, private := range []string{v.ownerBrowser.cookie, v.ownerBrowser.csrf, privateText, "PRIVATE_CONTENT_QUERY", "PRIVATE_CONTENT_HEADER"} {
		if strings.Contains(v.logs.text(), private) {
			t.Fatal("private content/request material reached safe logs")
		}
	}
}
