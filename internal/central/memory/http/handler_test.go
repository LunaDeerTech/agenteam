package memoryhttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/memory"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
)

func fresh[K any](t *testing.T) f.ID[K] {
	t.Helper()
	id, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type testBoundary struct {
	actor i.Actor
	deny  bool
}

func (b *testBoundary) CheckRequest(http.ResponseWriter, *http.Request) error {
	if b.deny {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (b *testBoundary) RequireHuman(*http.Request) (i.Actor, error) { return b.actor, nil }
func (*testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.WriteProblem(w, r, err)
}

type managementSpy struct {
	namespace c.Namespace
	deletes   int
	revision  f.Version
}

func (s *managementSpy) List(_ context.Context, _ i.Actor, n c.Namespace, q memory.ListQuery) (memory.Page, error) {
	s.namespace = n
	return memory.Page{Memories: []c.Record{}}, nil
}
func (s *managementSpy) History(context.Context, i.Actor, c.Namespace, c.ID, string, int) (memory.HistoryPage, error) {
	return memory.HistoryPage{Revisions: []c.Revision{}}, nil
}
func (s *managementSpy) Delete(_ context.Context, _ i.Actor, n c.Namespace, id c.ID, meta f.CommandMeta) (memory.DeleteReceipt, error) {
	s.namespace = n
	s.deletes++
	s.revision = *meta.ExpectedVersion
	return memory.DeleteReceipt{MemoryID: id, Revision: *meta.ExpectedVersion, Deleted: true}, nil
}
func (*managementSpy) LookupDelete(context.Context, i.Actor, c.Namespace, c.ID, f.CommandMeta) (memory.DeleteReceipt, bool, error) {
	return memory.DeleteReceipt{}, false, nil
}
func testServer(t *testing.T) (*httptest.Server, *managementSpy, *testBoundary, string) {
	t.Helper()
	actor, _ := i.NewHuman(fresh[i.User](t), fresh[i.Session](t))
	spy := &managementSpy{}
	boundary := &testBoundary{actor: actor}
	h := &handler{spy, boundary}
	server := httptest.NewServer(httpapi.WithRequestID(slog.New(slog.NewTextHandler(io.Discard, nil)), h))
	t.Cleanup(server.Close)
	base := "/api/v1/projects/" + fresh[i.Project](t).String() + "/agents/" + fresh[i.Agent](t).String() + "/memory"
	return server, spy, boundary, base
}
func TestManagementHTTPUsesBoundedNamespaceAndNativeResponse(t *testing.T) {
	server, spy, _, base := testServer(t)
	response, err := server.Client().Get(server.URL + base + "?limit=10&type=procedure")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err != nil || closeErr != nil || response.StatusCode != 200 || string(raw) != `{"memories":[]}` || spy.namespace.Validate() != nil {
		t.Fatal(response.StatusCode, string(raw), err, closeErr)
	}
	response, err = server.Client().Get(server.URL + base + "?project_id=other")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatal(response.StatusCode)
	}
}
func TestDeleteRequiresCanonicalRevisionAndOriginalCommandKey(t *testing.T) {
	server, spy, _, base := testServer(t)
	path := server.URL + base + "/" + fresh[c.Memory](t).String() + "/delete"
	for n, body := range []string{`{"expected_revision":"3","expected_revision":"4"}`, `{"expected_revision":3}`, `{"expected_revision":"3"}`} {
		request, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "delete-original")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		raw, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if n < 2 && response.StatusCode != 400 || n == 2 && response.StatusCode != 200 {
			t.Fatal(response.StatusCode, string(raw))
		}
	}
	if spy.deletes != 1 || spy.revision != 3 {
		t.Fatal(spy.deletes, spy.revision)
	}
}
func TestAuthenticationPrecedesInvalidNamespaceAndNoDomainCall(t *testing.T) {
	server, spy, boundary, _ := testServer(t)
	boundary.deny = true
	response, err := server.Client().Get(server.URL + "/api/v1/projects/invalid/agents/invalid/memory")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var problem httpapi.Problem
	if json.NewDecoder(response.Body).Decode(&problem) != nil || response.StatusCode != 403 || spy.namespace.Validate() == nil {
		t.Fatal(response.StatusCode)
	}
}
