package runnerhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func pureID[K any]() f.ID[K] {
	v, e := f.ParseID[K]("01900000-0000-7000-8000-000000000001")
	if e != nil {
		panic(e)
	}
	return v
}
func pureActor() id.Actor {
	v, e := id.NewHuman(pureID[id.User](), pureID[id.Session]())
	if e != nil {
		panic(e)
	}
	return v
}
func pureReceipt(command c.CommandName, target c.RunnerID) c.Receipt {
	at, _ := f.NewInstant(time.Now())
	return c.Receipt{CommandID: pureID[c.Command](), Command: command, Changed: true, Runner: c.Snapshot{ID: target, Name: "runner", Tags: []string{}, RootPath: "/srv", Version: 1, CredentialGeneration: 1, Status: c.Offline, CreatedAt: at, UpdatedAt: at}}
}

type pureCommands struct {
	calls, lookups int
	key            f.IdempotencyKey
	in             c.Intent
	material       bool
	invalid        bool
}

func (s *pureCommands) Execute(_ context.Context, _ id.Actor, key f.IdempotencyKey, in c.Intent) (c.Mutation, error) {
	s.calls++
	s.key = key
	s.in = in
	out := c.Mutation{Receipt: pureReceipt(in.Command(), in.Target())}
	if s.invalid {
		out.Receipt.Runner.Name = ""
	}
	if s.material {
		token, e := p.NewEnrollmentToken()
		if e != nil {
			return c.Mutation{}, e
		}
		at, _ := f.NewInstant(time.Now().Add(time.Minute))
		out.Material = &c.EnrollmentMaterial{Token: token, ExpiresAt: at}
	}
	return out, nil
}
func (s *pureCommands) Lookup(_ context.Context, _ id.Actor, key f.IdempotencyKey, in c.Intent) (c.Lookup, error) {
	s.lookups++
	s.key = key
	s.in = in
	out := pureReceipt(in.Command(), in.Target())
	return c.Lookup{Receipt: &out}, nil
}
func TestRunnerAdminPureIntentReplayAndOneTimeMaterial(t *testing.T) {
	commands := &pureCommands{material: true}
	h := adminHandler{commands: commands}
	target := pureID[c.Runner]()
	original := `{"runner_id":"` + target.String() + `","name":"runner","description":"raw request canary","tags":[],"root_path":"/srv"}`
	req := httptest.NewRequest("POST", adminPrefix, strings.NewReader(original))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "original-key")
	body, e := h.mutate(httptest.NewRecorder(), req, pureActor(), route{kind: "collection"})
	if e != nil {
		t.Fatal(e)
	}
	var first map[string]json.RawMessage
	if json.Unmarshal(body, &first) != nil || string(first["token_available"]) != "true" || len(first["enrollment_token"]) == 0 {
		t.Fatal("known first material")
	}
	initial, _ := commands.in.Digest(pureActor())
	request, _ := commands.in.RequestJSON()
	lookup := `{"command":"runner.create","request":` + original + `}`
	req = httptest.NewRequest("POST", adminPrefix+"/"+target.String()+"/commands/lookup", strings.NewReader(lookup))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "original-key")
	body, e = h.mutate(httptest.NewRecorder(), req, pureActor(), route{"lookup", target})
	if e != nil {
		t.Fatal(e)
	}
	var replay map[string]json.RawMessage
	if json.Unmarshal(body, &replay) != nil || string(replay["token_available"]) != "false" || len(replay["enrollment_token"]) != 0 || string(replay["state"]) != `"committed"` {
		t.Fatal("lookup recreated secret")
	}
	recovered, _ := commands.in.Digest(pureActor())
	round, _ := commands.in.RequestJSON()
	if recovered != initial || string(round) != string(request) || commands.calls != 1 || commands.lookups != 1 || commands.key != "original-key" {
		t.Fatal("lookup changed original intent")
	}
}
func TestRunnerAdminPureRejectsWireBeforeCommands(t *testing.T) {
	target := pureID[c.Runner]()
	good := `{"expected_version":"2","description":""}`
	cases := []struct {
		body   string
		header map[string][]string
		query  string
	}{
		{`{"expected_version":"2","description":null}`, nil, ""},
		{`{"expected_version":"2","description":"x","descrip\u0074ion":"y"}`, nil, ""},
		{`{"expected_version":"2","description":"\ud800"}`, nil, ""},
		{good, map[string][]string{"Content-Encoding": {""}}, ""},
		{good, map[string][]string{"idempotency-key": {"other"}}, ""},
		{good, nil, "?"},
		{good + strings.Repeat(" ", c.MaxRequestBytes), nil, ""},
	}
	for n, tc := range cases {
		commands := &pureCommands{}
		h := adminHandler{commands: commands}
		req := httptest.NewRequest("PATCH", adminPrefix+"/"+target.String()+tc.query, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "key")
		for k, v := range tc.header {
			req.Header[k] = v
		}
		if _, e := h.mutate(httptest.NewRecorder(), req, pureActor(), route{"runner", target}); e == nil || commands.calls != 0 {
			t.Fatalf("invalid case %d reached command", n)
		}
	}
}
func TestRunnerAdminPureCommittedProjectionAborts(t *testing.T) {
	commands := &pureCommands{invalid: true}
	h := adminHandler{commands: commands}
	target := pureID[c.Runner]()
	req := httptest.NewRequest("PATCH", adminPrefix+"/"+target.String(), strings.NewReader(`{"expected_version":"2","description":"changed"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "key")
	defer func() {
		if value, ok := recover().(error); !ok || !errors.Is(value, http.ErrAbortHandler) {
			t.Fatal("committed bad projection did not abort")
		}
		if commands.calls != 1 {
			t.Fatal("command not called once")
		}
	}()
	_, _ = h.mutate(httptest.NewRecorder(), req, pureActor(), route{"runner", target})
}
