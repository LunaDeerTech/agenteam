package approvalhttp

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func id[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, e := f.NewID[T]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}

type controlledBoundary struct{ actor i.Actor }

func (b controlledBoundary) CheckRequest(http.ResponseWriter, *http.Request) error { return nil }
func (b controlledBoundary) RequireHuman(*http.Request) (i.Actor, error)           { return b.actor, nil }
func (b controlledBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	(&account.HTTPBoundary{}).WriteProblem(w, r, err)
}

type controlledService struct {
	value          c.Request
	key            f.IdempotencyKey
	calls, lookups int
}

func (s *controlledService) Get(context.Context, i.Actor, i.ProjectID, c.RequestID) (c.Request, error) {
	return s.value, nil
}
func (s *controlledService) Decide(_ context.Context, _ i.Actor, _ i.ProjectID, _ c.RequestID, m f.CommandMeta, in c.Decision) (c.Receipt, error) {
	s.calls++
	s.key = m.IdempotencyKey
	return c.Receipt{}, f.NewFault(f.CommitUnknown, f.Unknown)
}
func (s *controlledService) LookupDecision(_ context.Context, _ i.Actor, _ i.ProjectID, _ c.RequestID, m f.CommandMeta, in c.Decision) (c.Lookup, error) {
	s.lookups++
	s.key = m.IdempotencyKey
	return c.Lookup{Status: "not_observed"}, nil
}
func TestApprovalHTTPUnknownUsesOriginalIntentLookup(t *testing.T) {
	user := id[i.User](t)
	actor, _ := i.NewHuman(user, id[i.Session](t))
	p, request := id[i.Project](t), id[c.RequestIdentity](t)
	at, _ := f.ParseInstant("2026-10-14T00:00:00Z")
	s := &controlledService{value: c.Request{ToolName: "run-command", ID: request, ProjectID: p, AgentID: id[i.Agent](t), ExecutionID: id[i.Execution](t), ToolID: id[i.Tool](t), ToolCallID: "call", OperationID: id[tc.Operation](t), Fingerprint: f.Digest("sha256:" + strings.Repeat("0", 64)), Status: c.Pending, Version: 1, CreatedAt: at}}
	h := httpapi.Handler(nil, &handler{s, controlledBoundary{actor}})
	server := httptest.NewServer(h)
	defer server.Close()
	path := server.URL + prefix + p.String() + "/approvals/" + request.String()
	send := func(suffix, body, key string) int {
		t.Helper()
		r, _ := http.NewRequest(http.MethodPost, path+suffix, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		v, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, v.Body)
		v.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return v.StatusCode
	}
	body := `{"decision":"approve","expected_version":"1"}`
	if status := send("/decision", body, "original-intent"); status < 500 {
		t.Fatal("unknown was presented as success", status)
	}
	if status := send("/decision/lookup", body, "original-intent"); status != 200 || s.calls != 1 || s.lookups != 1 || s.key != "original-intent" {
		t.Fatal("lookup rewrote or lost original intent", status)
	}
	for _, bad := range []string{`{"decision":"approve","decision":"reject","expected_version":"1"}`, `{"decision":"approve","expected_version":1}`, `{"decision":"approve","expected_version":"1","scope":"reusable"}`} {
		if status := send("/decision", bad, "original-intent"); status != 400 {
			t.Fatal("bad action accepted", status)
		}
	}
	if status := send("/decision", body, ""); status != 400 || s.calls != 1 {
		t.Fatal("missing key reached domain", status)
	}
}
