//go:build integration

package knowledge_test

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func TestKnowledgeTreeCommandHTTPAuthority(t *testing.T) {
	v := newTreeCommandHTTPFixture(t)
	t.Run("actual_boundary_nonowner_and_admin_no_bypass", func(t *testing.T) {
		d := v.makeDocument(t, nil, "current Owner")
		path := treeCommandHTTPPath(v.project, "/"+d.ID.String()+"/rename")
		body := `{"expected_version":"1","title":"must reject"}`
		before := v.facts(t)
		for _, browser := range []treeCommandHTTPBrowser{v.otherBrowser, v.adminBrowser} {
			treeCommandProblem(t, v.request(t, browser, "POST", path, body, treeMeta(t).IdempotencyKey), f.NotFound)
		}
		r := treeCommandHTTPRequest(knowledgeContext(t), v.ownerBrowser, "POST", path, body, treeMeta(t).IdempotencyKey)
		r.Header.Set("Origin", "https://untrusted.example.test")
		treeCommandProblem(t, v.serve(r), f.OriginDenied)
		r = treeCommandHTTPRequest(knowledgeContext(t), v.ownerBrowser, "POST", path, body, treeMeta(t).IdempotencyKey)
		r.Header.Del("X-CSRF-Token")
		treeCommandProblem(t, v.serve(r), f.CSRFFailed)
		treeCommandProblem(t, v.request(t, treeCommandHTTPBrowser{}, "GET", path, "", ""), f.Unauthenticated)
		response := v.request(t, v.ownerBrowser, "GET", path, "", "")
		treeCommandProblem(t, response, f.MethodNotAllowed)
		if response.header.Get("Allow") != "POST" || v.facts(t) != before {
			t.Fatal("boundary rejection had domain effects")
		}
	})
	t.Run("archive_new_write_denied_but_original_receipt_readable", func(t *testing.T) {
		v.project = v.ownerTreeFixture.project(t, v.ownerBrowser.actor, true)
		d := v.makeDocument(t, nil, "archive")
		body := `{"expected_version":"1","title":"confirmed before archive"}`
		key := treeMeta(t).IdempotencyKey
		first := v.post(t, d.ID, "rename", body, key)
		treeCommandSuccess(t, first)
		v.archive(t, v.project)
		before := v.facts(t)
		replay := v.post(t, d.ID, "rename", body, key)
		treeCommandSuccess(t, replay)
		if !bytes.Equal(first.body, replay.body) {
			t.Fatal("archived original receipt changed")
		}
		treeCommandLookupState(t, v.lookup(t, d.ID, "update", body, key), "committed")
		treeCommandSuccess(t, v.post(t, d.ID, "delete-preview", `{}`, ""))
		treeCommandProblem(t, v.post(t, d.ID, "rename", `{"expected_version":"2","title":"new forbidden write"}`, treeMeta(t).IdempotencyKey), f.ProjectNotActive)
		if v.facts(t) != before {
			t.Fatal("archive read/replay/reject changed facts")
		}
	})
	t.Run("actual_logout_old_cookie_rejected_new_session_original_key", func(t *testing.T) {
		v.project = v.ownerTreeFixture.project(t, v.ownerBrowser.actor, true)
		d := v.makeDocument(t, nil, "session")
		body := `{"expected_version":"1","title":"confirmed by original Session"}`
		key := treeMeta(t).IdempotencyKey
		first := v.post(t, d.ID, "rename", body, key)
		treeCommandSuccess(t, first)
		old := v.ownerBrowser
		if err := v.core.Logout(knowledgeContext(t), account.LogoutRequest{Actor: old.actor, Key: treeMeta(t).IdempotencyKey}); err != nil {
			t.Fatal("actual Logout", err)
		}
		path := treeCommandHTTPPath(v.project, "/"+d.ID.String()+"/rename")
		before := v.facts(t)
		treeCommandProblem(t, v.request(t, old, "POST", path, body, key), f.SessionRevoked)
		if v.facts(t) != before {
			t.Fatal("revoked HTTP call changed project facts")
		}
		v.ownerBrowser = v.login(t, old.email)
		if v.ownerBrowser.actor.Details().UserID != old.actor.Details().UserID || v.ownerBrowser.actor.Details().SessionID == old.actor.Details().SessionID {
			t.Fatal("new real same-User Session")
		}
		before = v.facts(t)
		replay := v.post(t, d.ID, "rename", body, key)
		treeCommandSuccess(t, replay)
		if !bytes.Equal(first.body, replay.body) || v.facts(t) != before {
			t.Fatal("new Session original replay changed facts")
		}
		treeCommandLookupState(t, v.lookup(t, d.ID, "update", body, key), "committed")
	})
}

// The only direct mutation is an explicitly owned upstream Project fixture,
// under the same real User EX/Project EX locks. No transfer receipt is claimed.
func treeCommandChangeOwner(ctx context.Context, v *treeCommandHTTPFixture, tx f.Tx) error {
	x, err := v.raw.InTx(tx)
	if err != nil {
		return err
	}
	_, err = x.Exec(ctx, `UPDATE agenteam_project.projects SET owner_user_id=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, v.project.String(), v.otherBrowser.actor.Details().UserID)
	return err
}

func treeCommandRequest(v *treeCommandHTTPFixture, d kc.DocumentRef, key f.IdempotencyKey, body string) *http.Request {
	return treeCommandHTTPRequest(context.Background(), v.ownerBrowser, "POST", treeCommandHTTPPath(v.project, "/"+d.ID.String()+"/move"), body, key)
}
