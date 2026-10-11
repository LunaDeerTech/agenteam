//go:build integration

package knowledge_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

func TestKnowledgeTextHTTPPublication(t *testing.T) {
	knowledgeOwnerHTTPTop(t)
	v := newTextHTTPFixture(t)
	var parent *kc.DocumentID
	for _, media := range []string{kc.PlainText, kc.Markdown} {
		t.Run(media, func(t *testing.T) {
			id := treeID[kc.Document](t)
			text := "Original canonical 世界\n" + media
			body := treeCommandJSON(t, map[string]any{"parent_document_id": parent, "title": "HTTP " + media, "media_type": media, "text": text})
			key := treeMeta(t).IdempotencyKey
			before := v.facts(t)
			treeCommandLookupState(t, treeCommandSuccess(t, textHTTPLookup(t, v, id, "create", body, key)), "not_observed")
			if v.facts(t) != before {
				t.Fatal("unobserved text Lookup executed publication")
			}
			first := v.post(t, id, "create-text", body, key)
			document := v.current(t, id)
			if document.ContentVersion != 1 || document.SourceKind != kc.Text || document.MediaType != media || document.Title != "HTTP "+media || (document.ParentDocumentID == nil) != (parent == nil) || parent != nil && *document.ParentDocumentID != *parent {
				t.Fatal("HTTP create lost canonical document fields")
			}
			treeCommandDocument(t, treeCommandSuccess(t, first)["document"], document)
			textHTTPRead(t, v, document, text)
			committed := v.facts(t)
			replay := v.post(t, id, "create-text", body, key)
			treeCommandSuccess(t, replay)
			receipt := treeCommandLookupState(t, treeCommandSuccess(t, textHTTPLookup(t, v, id, "create", body, key)), "committed")
			treeCommandDocument(t, receipt["document"], document)
			if len(receipt) != 3 || string(receipt["command"]) != `"create"` || string(receipt["changed"]) != "true" || !bytes.Equal(first.body, replay.body) || v.facts(t) != committed {
				t.Fatal("create replay or original Lookup changed publication facts")
			}
			replacement := "Replacement canonical 🌍\n" + media
			update := treeCommandJSON(t, map[string]any{"expected_version": "1", "media_type": media, "text": replacement})
			updateKey := treeMeta(t).IdempotencyKey
			treeCommandLookupState(t, treeCommandSuccess(t, textHTTPLookup(t, v, id, "update", update, updateKey)), "not_observed")
			if v.facts(t) != committed {
				t.Fatal("update Lookup executed replacement")
			}
			changed := v.post(t, id, "replace-text", update, updateKey)
			after := v.current(t, id)
			if after.ContentVersion != 2 || after.ObjectID == document.ObjectID || after.Title != document.Title || after.MediaType != media || after.SourceKind != kc.Text {
				t.Fatal("replacement failed to publish new canonical bytes/version")
			}
			treeCommandDocument(t, treeCommandSuccess(t, changed)["document"], after)
			textHTTPRead(t, v, after, replacement)
			committed = v.facts(t)
			replay = v.post(t, id, "replace-text", update, updateKey)
			treeCommandSuccess(t, replay)
			receipt = treeCommandLookupState(t, treeCommandSuccess(t, textHTTPLookup(t, v, id, "update", update, updateKey)), "committed")
			treeCommandDocument(t, receipt["document"], after)
			if string(receipt["command"]) != `"update"` || string(receipt["changed"]) != "true" || !bytes.Equal(changed.body, replay.body) || v.facts(t) != committed {
				t.Fatal("replacement replay or Lookup changed committed facts")
			}
			// The original create receipt remains version one even after replacement.
			receipt = treeCommandLookupState(t, treeCommandSuccess(t, textHTTPLookup(t, v, id, "create", body, key)), "committed")
			treeCommandDocument(t, receipt["document"], document)
			treeCommandProblem(t, v.post(t, id, "replace-text", update, treeMeta(t).IdempotencyKey), f.VersionConflict)
			other := treeCommandJSON(t, map[string]any{"expected_version": "1", "media_type": media, "text": "different intent"})
			treeCommandProblem(t, v.post(t, id, "replace-text", other, updateKey), f.IdempotencyKeyReused)
			treeCommandProblem(t, textHTTPLookup(t, v, id, "update", other, updateKey), f.IdempotencyKeyReused)
			if v.facts(t) != committed {
				t.Fatal("rejected stale/conflicting text intent changed facts")
			}
			noop := treeCommandJSON(t, map[string]any{"expected_version": "2", "media_type": media, "text": replacement})
			noopKey := treeMeta(t).IdempotencyKey
			treeCommandDocument(t, treeCommandSuccess(t, v.post(t, id, "replace-text", noop, noopKey))["document"], after)
			receipt = treeCommandLookupState(t, treeCommandSuccess(t, textHTTPLookup(t, v, id, "update", noop, noopKey)), "committed")
			if string(receipt["changed"]) != "false" || v.current(t, id).ContentVersion != 2 {
				t.Fatal("same bytes did not preserve the original version")
			}
			parent = &id
		})
	}
}

func TestKnowledgeTextHTTPCurrentAuthority(t *testing.T) {
	knowledgeOwnerHTTPTop(t)
	v := newTextHTTPFixture(t)
	id := treeID[kc.Document](t)
	body := `{"parent_document_id":null,"title":"Current Owner","media_type":"text/plain","text":"Owner-only canonical text"}`
	key := treeMeta(t).IdempotencyKey
	treeCommandSuccess(t, v.post(t, id, "create-text", body, key))
	update := `{"expected_version":"1","media_type":"text/plain","text":"must reject"}`
	path := treeCommandHTTPPath(v.project, "/"+id.String()+"/replace-text")
	before := v.facts(t)
	for _, browser := range []treeCommandHTTPBrowser{v.otherBrowser, v.adminBrowser} {
		treeCommandProblem(t, v.request(t, browser, http.MethodPost, path, update, treeMeta(t).IdempotencyKey), f.NotFound)
	}
	old := v.ownerBrowser
	if err := v.core.Logout(knowledgeContext(t), account.LogoutRequest{Actor: old.actor, Key: treeMeta(t).IdempotencyKey}); err != nil {
		t.Fatal("actual Logout", err)
	}
	treeCommandProblem(t, v.request(t, old, http.MethodPost, path, update, treeMeta(t).IdempotencyKey), f.SessionRevoked)
	treeCommandProblem(t, textHTTPLookup(t, v, id, "create", body, key), f.SessionRevoked)
	if v.facts(t) != before {
		t.Fatal("unauthorized text write/Lookup changed business facts")
	}
	v.ownerBrowser = v.login(t, old.email)
	treeCommandLookupState(t, treeCommandSuccess(t, textHTTPLookup(t, v, id, "create", body, key)), "committed")
	// Real upstream locks protect this explicit Owner fixture; this is not a
	// production Owner-transfer endpoint or receipt acceptance.
	result, done := treeCommandOwnerWriter(t, v, nil, nil, nil)
	treeCommandCommit(t, result, done)
	before = v.facts(t)
	treeCommandProblem(t, v.post(t, id, "create-text", body, key), f.NotFound)
	treeCommandProblem(t, v.post(t, id, "replace-text", update, treeMeta(t).IdempotencyKey), f.NotFound)
	treeCommandProblem(t, textHTTPLookup(t, v, id, "create", body, key), f.NotFound)
	if v.facts(t) != before {
		t.Fatal("old Owner replay/Lookup bypassed current authority")
	}
	v.ownerBrowser = v.otherBrowser
	textHTTPRead(t, v, v.current(t, id), "Owner-only canonical text")
}
