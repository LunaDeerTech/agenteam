package model

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type managementUnreadBody struct{ t *testing.T }

func (b managementUnreadBody) Read([]byte) (int, error) {
	b.t.Fatal("management read scanned request body")
	return 0, io.EOF
}
func (managementUnreadBody) Close() error { return nil }

func TestSystemManagementHTTPClosedMetadataAndHEAD(t *testing.T) {
	ref, _ := sc.NewCredentialRef(mustID[sc.Credential](t), id.SystemScope())
	for _, branch := range []string{"valid", "wrong-ref", "project", "purpose", "version", "failure"} {
		t.Run(branch, func(t *testing.T) {
			calls := 0
			h := &systemHTTP{writes: &httpWritesSpy{metadata: func(got sc.CredentialRef) (sc.Metadata, error) {
				calls++
				if !got.Equal(ref) {
					t.Fatal("changed requested ref")
				}
				v := sc.Metadata{CredentialRef: ref, Purpose: sc.Model, Version: 9007199254740993}
				switch branch {
				case "wrong-ref":
					v.CredentialRef, _ = sc.NewCredentialRef(mustID[sc.Credential](t), id.SystemScope())
				case "project":
					scope, _ := id.InProject(mustID[id.Project](t))
					v.CredentialRef, _ = sc.NewCredentialRef(ref.Details().ID, scope)
				case "purpose":
					v.Purpose = sc.SMTP
				case "version":
					v.Version = 0
				case "failure":
					return v, fault(f.DependencyUnavailable)
				}
				return v, nil
			}}}
			for _, method := range []string{"GET", "HEAD"} {
				r := httptest.NewRequest(method, "https://example.test/", nil)
				r.SetPathValue("id", ref.Details().ID.String())
				out, err := h.getCredentialMetadata(httptest.NewRecorder(), r, mc.CommandMeta{Actor: testActor(t), Scope: id.SystemScope()})
				if branch != "valid" {
					if err == nil || out != nil {
						t.Fatal("invalid metadata escaped")
					}
					code := f.NotFound
					if branch == "version" || branch == "failure" {
						code = f.DependencyUnavailable
					}
					requireCode(t, err, code)
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(out)
				if err != nil {
					t.Fatal(err)
				}
				var wire map[string]any
				if json.Unmarshal(raw, &wire) != nil || len(wire) != 3 || wire["credential_id"] != ref.Details().ID.String() || wire["purpose"] != "model" || wire["version"] != "9007199254740993" {
					t.Fatal("metadata wire is not closed")
				}
				w := httptest.NewRecorder()
				if err = httpapi.WriteJSON(w, r, http.StatusOK, out); err != nil {
					t.Fatal(err)
				}
				if method == "HEAD" && w.Body.Len() != 0 {
					t.Fatal("HEAD body")
				}
			}
			if calls != 2 {
				t.Fatal("HEAD skipped metadata or retried")
			}
		})
	}
}

func TestSystemManagementHTTPRejectsInputWithoutReadingBody(t *testing.T) {
	h := &systemHTTP{writes: &httpWritesSpy{metadata: func(sc.CredentialRef) (sc.Metadata, error) {
		t.Fatal("called metadata for invalid input")
		return sc.Metadata{}, nil
	}}}
	for _, branch := range []string{"query", "empty-query", "length", "chunked", "unknown-length", "bad-id"} {
		t.Run(branch, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://example.test/", nil)
			r.Body = managementUnreadBody{t}
			r.SetPathValue("id", mustID[sc.Credential](t).String())
			switch branch {
			case "query":
				r.URL.RawQuery = "limit=1"
			case "empty-query":
				r.URL.ForceQuery = true
			case "length":
				r.ContentLength = 1
			case "chunked":
				r.TransferEncoding = []string{"chunked"}
			case "unknown-length":
				r.ContentLength = -1
			case "bad-id":
				r.SetPathValue("id", "not-an-id")
			}
			_, err := h.getCredentialMetadata(httptest.NewRecorder(), r, mc.CommandMeta{})
			requireCode(t, err, f.InvalidArgument)
			_, err = h.getModelDeletionImpact(httptest.NewRecorder(), r, mc.CommandMeta{})
			requireCode(t, err, f.InvalidArgument)
		})
	}
}

func TestSystemManagementHTTPBudgetOnlyNewReads(t *testing.T) {
	newCount := 0
	for _, route := range (&systemHTTP{}).routes() {
		for _, method := range []string{route.method, http.MethodHead} {
			r := httptest.NewRequest(method, "https://example.test/", nil)
			out, cancel := managementReadRequest(r, route)
			deadline, ok := out.Context().Deadline()
			if managementReadRoute(route) {
				newCount++
				if !ok || time.Until(deadline) > 3*time.Second || time.Until(deadline) < 2*time.Second {
					t.Fatal("new route budget")
				}
			} else if ok || out != r {
				t.Fatal("old route changed budget")
			}
			cancel()
			ctx, done := context.WithTimeout(context.Background(), time.Second)
			r = r.WithContext(ctx)
			out, cancel = managementReadRequest(r, route)
			parent, _ := ctx.Deadline()
			derived, _ := out.Context().Deadline()
			if !derived.Equal(parent) {
				t.Fatal("earlier caller budget extended")
			}
			cancel()
			done()
		}
	}
	if newCount != 4 {
		t.Fatal("GET/HEAD budget coverage")
	}
	for _, route := range (&systemHTTP{}).routes() {
		if managementReadRoute(route) && (route.intent != id.Read || route.list || strings.HasSuffix(route.path, "lookup")) {
			t.Fatal("read became command")
		}
	}
}
