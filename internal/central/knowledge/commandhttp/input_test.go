package commandhttp

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestTreeCommandsHTTPStrictOriginalInput(t *testing.T) {
	tests := []struct{ name, action, body string }{
		{"missing_version", "rename", `{"title":"original"}`},
		{"missing_title", "rename", `{"expected_version":"1"}`},
		{"null_title", "rename", `{"expected_version":"1","title":null}`},
		{"null_version", "rename", `{"expected_version":null,"title":"original"}`},
		{"numeric_version", "rename", `{"expected_version":1,"title":"original"}`},
		{"leading_zero", "rename", `{"expected_version":"01","title":"original"}`},
		{"overflow", "rename", `{"expected_version":"9223372036854775808","title":"original"}`},
		{"unknown", "rename", `{"expected_version":"1","title":"original","replace_source":false}`},
		{"wrong_branch", "rename", `{"expected_version":"1","title":"original","target_parent_id":null}`},
		{"empty", "rename", ``}, {"null", "rename", `null`}, {"array", "rename", `[]`},
		{"two_values", "rename", `{"expected_version":"1","title":"original"}{}`},
		{"duplicate", "rename", `{"expected_version":"1","title":"bad","title":"original"}`},
		{"duplicate_escaped", "rename", `{"expected_version":"1","title":"bad","\u0074itle":"original"}`},
		{"surrogate_value", "rename", `{"expected_version":"1","title":"\ud800"}`},
		{"surrogate_low", "rename", `{"expected_version":"1","title":"\udfff"}`},
		{"surrogate_key", "rename", `{"expected_version":"1","title":"original","\ud800":0}`},
		{"bad_utf8", "rename", "{\"expected_version\":\"1\",\"title\":\"\xff\"}"},
		{"title_control", "rename", `{"expected_version":"1","title":"a\nb"}`},
		{"title_too_long", "rename", fmt.Sprintf(`{"expected_version":"1","title":%q}`, strings.Repeat("a", 513))},
		{"parent_missing", "move", `{"target_parent_id":null}`},
		{"parent_empty", "move", `{"expected_parent_id":null,"target_parent_id":""}`},
		{"move_version", "move", `{"expected_parent_id":null,"target_parent_id":null,"expected_version":"1"}`},
		{"move_self", "move", fmt.Sprintf(`{"expected_parent_id":null,"target_parent_id":%q}`, testID[kc.Document](3).String())},
		{"preview_extra", "delete-preview", `{"confirmation_token":"x"}`},
		{"preview_null", "delete-preview", `null`},
		{"token_invalid", "delete-subtree", `{"confirmation_token":"x"}`},
		{"token_missing", "delete-subtree", `{}`},
		{"token_null", "delete-subtree", `{"confirmation_token":null}`},
		{"lookup_digest", "lookup", fmt.Sprintf(`{"command":"update","document_id":%q,"request":{"expected_version":"1","title":"original"},"semantic_digest":"bad"}`, testID[kc.Document](3).String())},
		{"lookup_create", "lookup", fmt.Sprintf(`{"command":"create","document_id":%q,"request":{}}`, testID[kc.Document](3).String())},
		{"lookup_nested_dup", "lookup", fmt.Sprintf(`{"command":"move","document_id":%q,"request":{"expected_parent_id":null,"target_parent_id":null,"target_parent_id":null}}`, testID[kc.Document](3).String())},
		{"lookup_nested_unknown", "lookup", fmt.Sprintf(`{"command":"move","document_id":%q,"request":{"expected_parent_id":null,"target_parent_id":null,"extra":true}}`, testID[kc.Document](3).String())},
		{"lookup_null_request", "lookup", fmt.Sprintf(`{"command":"move","document_id":%q,"request":null}`, testID[kc.Document](3).String())},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h, s, _ := fixture()
			w := writer()
			if serve(h, request(test.action, test.body), w) || w.Code != 400 || s.calls != 0 {
				t.Fatalf("invalid input code=%d calls=%d body=%s", w.Code, s.calls, w.Body.String())
			}
		})
	}
	for _, title := range []string{`"\ud83d\ude80"`, `"\\ud800"`, `"�"`} {
		t.Run("valid_unicode_"+title, func(t *testing.T) {
			h, s, _ := fixture()
			if title == `"\ud83d\ude80"` {
				s.doc.Title = "🚀"
			} else if title == `"\\ud800"` {
				s.doc.Title = `\ud800`
			} else {
				s.doc.Title = "�"
			}
			requireSuccess(t, h, request("rename", `{"expected_version":"1","title":`+title+`}`))
		})
	}
	t.Run("max_version_noop", func(t *testing.T) {
		h, s, _ := fixture()
		s.doc.ContentVersion = f.Version(math.MaxInt64)
		requireSuccess(t, h, request("rename", `{"expected_version":"9223372036854775807","title":"original"}`))
	})
}

func TestTreeCommandsHTTPHeadersRoutesAndAuthorityOrder(t *testing.T) {
	for _, name := range []string{"no_key", "duplicate_key", "case_duplicate_key", "bad_key", "query", "force_query", "encoding_empty", "encoding", "content_type", "duplicate_type", "too_large", "get", "head", "unsupported_path"} {
		t.Run(name, func(t *testing.T) {
			h, s, _ := fixture()
			r := request("rename", `{"expected_version":"1","title":"original"}`)
			code := 400
			switch name {
			case "no_key":
				r.Header.Del("Idempotency-Key")
			case "duplicate_key":
				r.Header.Add("Idempotency-Key", "second")
			case "case_duplicate_key":
				r.Header["idempotency-key"] = []string{"second"}
			case "bad_key":
				r.Header.Set("Idempotency-Key", "bad\tkey")
			case "query":
				r.URL.RawQuery = "x=1"
			case "force_query":
				r.URL.ForceQuery = true
			case "encoding_empty":
				r.Header["Content-Encoding"] = []string{""}
				code = 415
			case "encoding":
				r.Header.Set("Content-Encoding", "gzip")
				code = 415
			case "content_type":
				r.Header.Set("Content-Type", "text/plain")
				code = 415
			case "duplicate_type":
				r.Header.Add("Content-Type", "application/json")
				code = 415
			case "too_large":
				r = request("rename", strings.Repeat(" ", requestLimit+1))
				code = 413
			case "get":
				r.Method = "GET"
				code = 405
			case "head":
				r.Method = "HEAD"
				code = 405
			case "unsupported_path":
				r.URL.Path += "/"
				code = 404
			}
			w := writer()
			if serve(h, r, w) || w.Code != code || s.calls != 0 {
				t.Fatalf("code=%d calls=%d", w.Code, s.calls)
			}
			if code == 405 && w.Header().Get("Allow") != "POST" {
				t.Fatal("Allow")
			}
		})
	}
	t.Run("preview_key_rejected", func(t *testing.T) {
		h, s, _ := fixture()
		r := request("delete-preview", `{}`)
		r.Header.Set("Idempotency-Key", "original-key")
		w := writer()
		if serve(h, r, w) || w.Code != 400 || s.calls != 0 {
			t.Fatal("preview accepted command identity")
		}
	})
	t.Run("origin_before_method", func(t *testing.T) {
		h, s, b := fixture()
		b.check = func(*http.Request) error { return f.NewFault(f.OriginDenied, f.NotStarted) }
		r := request("rename", `{}`)
		r.Method = "GET"
		w := writer()
		if serve(h, r, w) || w.Code != 403 || b.auths.Load() != 0 || s.calls != 0 {
			t.Fatal("origin bypass")
		}
	})
	t.Run("invalid_actor_before_method", func(t *testing.T) {
		h, s, b := fixture()
		b.actor = id.Actor{}
		r := request("rename", `{}`)
		r.Method = "GET"
		w := writer()
		if serve(h, r, w) || w.Code != 401 || s.calls != 0 {
			t.Fatal("identity bypass")
		}
	})
	for _, path := range []string{strings.TrimSuffix(testPath("rename"), "/rename"), strings.TrimSuffix(testPath("rename"), testID[kc.Document](3).String()+"/rename") + "children", strings.TrimSuffix(testPath("rename"), "rename") + "ancestors", strings.TrimSuffix(testPath("lookup"), "commands/lookup") + "search-titles"} {
		if HandlesPath(path) {
			t.Fatal("read path captured")
		}
	}
	for _, action := range []string{"rename", "move", "delete-preview", "delete-subtree", "lookup"} {
		if !HandlesPath(testPath(action)) {
			t.Fatal("missing command path")
		}
	}
}

func TestTreeCommandsHTTPOutputLimitAndBadPreview(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), requestBudget)
	defer cancel()
	if _, err := encodeValue(ctx, strings.Repeat("a", responseLimit)); err == nil {
		t.Fatal("oversized whole JSON accepted")
	}
	for _, name := range []string{"duplicate", "wrong_project", "hidden_object", "wrong_digest"} {
		t.Run(name, func(t *testing.T) {
			h, s, _ := fixture()
			switch name {
			case "duplicate":
				s.preview.Nodes = append(s.preview.Nodes, s.preview.Nodes[0])
			case "wrong_project":
				s.preview.Nodes[0].ProjectID = testID[id.Project](99)
			case "hidden_object":
				s.preview.Nodes[0].ObjectID = oc.ObjectID{}
			case "wrong_digest":
				s.preview.ScopeDigest = f.Digest("sha256:" + strings.Repeat("0", 64))
			}
			w := writer()
			if serve(h, request("delete-preview", `{}`), w) || w.Code != 503 || strings.Contains(w.Body.String(), "confirmation_token") {
				t.Fatal("bad preview published")
			}
		})
	}
}
