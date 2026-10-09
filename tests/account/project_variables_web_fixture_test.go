//go:build integration

package account_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type variableWebRequestKey struct{}
type variableWebAttempt struct {
	Index                                              int
	Method, Path, Query, Project, Target, Command, Key string
	Body, Receipt                                      []byte
	CSRF                                               [32]byte
	Status                                             int
	EOF, Closed                                        bool
	Cut                                                *variableWebCut
}
type variableWebCut struct {
	Written                     int
	Write, Flush, Hijack, Close bool
}
type variableWebHold struct {
	Path                        string
	Release                     chan struct{}
	Once                        sync.Once
	Started, Finished, Canceled bool
	Index                       int
}
type projectVariablesWebFixture struct {
	owner                    *projectOwnerWebFixture
	mode                     string
	mu                       sync.Mutex
	attempts                 []*variableWebAttempt
	initial                  map[string]any
	targets                  map[string]string
	dropCommand, dropProject string
	dropTarget               string
	dropIndex                int
	setupFacts               map[string][2]int
	setupToken               string
	hold                     *variableWebHold
	responses                int
	recording                bool
}

func variableWebMode(mode string) bool {
	switch mode {
	case "read", "crud", "recovery", "identity", "authority", "layouts":
		return true
	}
	return false
}
func newProjectVariablesWebFixture(t *testing.T, ctx context.Context, mode string) *projectVariablesWebFixture {
	t.Helper()
	if !variableWebMode(mode) {
		t.Fatal("exact Variables browser case required")
	}
	v := &projectVariablesWebFixture{mode: mode, initial: map[string]any{}, targets: map[string]string{}, setupFacts: map[string][2]int{}, setupToken: id[identity.ProjectVariable](t).String()}
	owner := newProjectOwnerWebFixtureWithVariables(t, ctx, "variables-"+mode, v)
	v.owner = owner
	owner.private("project-variables-material.json", map[string]any{"owner": owner.owner, "admin": owner.admin, "other": owner.other,
		"ids": owner.ids, "projects": owner.initial, "variables": v.initial, "targets": v.targets})
	t.Cleanup(func() {
		v.releaseRead()
		owner.stopProxy()
		v.mu.Lock()
		defer v.mu.Unlock()
		for _, a := range v.attempts {
			clear(a.Body)
			clear(a.Receipt)
			a.Key = ""
		}
		v.attempts = nil
	})
	return v
}

// Called after the original formal Account/Project preparation, before its
// disclosed terminal lifecycle fixtures. All ordinary values use real HTTP.
func (v *projectVariablesWebFixture) prepare(ctx context.Context) {
	o := v.owner
	for _, entry := range []struct{ key, name string }{{"dotted", "variables.dot-name"}, {"create", "variables-create"}, {"update", "variables-update"}, {"delete", "variables-delete"}} {
		o.ids[entry.key] = o.create(ctx, o.ownerActor, entry.name).ID.String()
	}
	for _, key := range []string{"main", "dotted", "update", "delete", "archiving", "archived"} {
		target := id[identity.ProjectVariable](o.t).String()
		v.targets[key] = target
		value := v.request(ctx, key, http.MethodPost, "", map[string]any{"request": map[string]any{"variable_id": target, "name": "CUSTOM_VALUE", "description": "普通变量准备", "value": "original ordinary value"}}, http.StatusOK)
		v.initial[key] = value["variable"]
	}
	if v.mode == "read" || v.mode == "layouts" {
		for n := range 51 {
			v.request(ctx, "dotted", http.MethodPost, "", map[string]any{"request": map[string]any{
				"variable_id": id[identity.ProjectVariable](o.t).String(), "name": fmt.Sprintf("PAGE_%03d", n), "description": "分页摘要", "value": "detail-only ordinary value"}}, http.StatusOK)
		}
	}
	// Only browser requests belong to the acceptance ledger; seed facts remain
	// real persistent facts and are counted separately by the final SQL checks.
	v.mu.Lock()
	for _, a := range v.attempts {
		clear(a.Body)
		clear(a.Receipt)
	}
	v.attempts = nil
	v.responses = 0
	v.recording = true
	v.mu.Unlock()
}
func (v *projectVariablesWebFixture) request(ctx context.Context, key, method, target string, body any, status int) map[string]any {
	project, ok := v.owner.ids[key]
	if !ok || key == "other" {
		v.owner.t.Fatal("private Variables setup target invalid")
	}
	path := "/api/v1/projects/" + project + "/variables"
	if target != "" {
		path += "/" + target
	}
	client := *v.owner.ownerClient
	client.Transport = variableWebSetupTransport{base: client.Transport, token: v.setupToken}
	result := v.owner.setup.setupRequest(ctx, &client, method, path, body, v.owner.ownerCSRF, method != http.MethodGet, status)
	if method != http.MethodGet && status == http.StatusOK {
		facts := v.setupFacts[project]
		facts[0]++
		if result["changed"] == true {
			facts[1]++
		}
		v.setupFacts[project] = facts
	}
	return result
}

// Only fixture setup carries this private nonce. Remove it before the default
// root sees the request; setup responses never become browser observations.
type variableWebSetupTransport struct {
	base  http.RoundTripper
	token string
}

func (t variableWebSetupTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.Header.Set("X-Agenteam-Variable-Fixture", t.token)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}
func projectVariablesWebPage(raw string) bool {
	if !strings.HasSuffix(raw, "/settings/variables") {
		return false
	}
	return projectOwnerWebPage(strings.TrimSuffix(raw, "/settings/variables") + "/settings/general")
}
func variableWebEndpoint(r *http.Request) (project, target, command string, ok bool) {
	if r == nil {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/projects/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/api/v1/projects/") || len(parts) < 2 || parts[1] != "variables" {
		return
	}
	if _, err := f.ParseID[identity.Project](parts[0]); err != nil {
		return
	}
	project = parts[0]
	switch {
	case len(parts) == 2 && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		if r.Method == http.MethodPost {
			command = "project.variable.create"
		}
		ok = true
	case len(parts) == 3 && (r.Method == http.MethodGet || r.Method == http.MethodPatch || r.Method == http.MethodDelete):
		if _, err := f.ParseID[identity.ProjectVariable](parts[2]); err != nil {
			return "", "", "", false
		}
		target = parts[2]
		ok = true
		if r.Method == http.MethodPatch {
			command = "project.variable.update"
		}
		if r.Method == http.MethodDelete {
			command = "project.variable.delete"
		}
	case r.Method == http.MethodPost && len(parts) == 4 && parts[2] == "commands" && parts[3] == "lookup":
		ok = true
	}
	return
}
func (v *projectVariablesWebFixture) handles(r *http.Request) bool {
	_, _, _, ok := variableWebEndpoint(r)
	return ok
}
func (v *projectVariablesWebFixture) observeRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Agenteam-Variable-Fixture") == v.setupToken {
		r.Header.Del("X-Agenteam-Variable-Fixture")
		*r = *r.WithContext(context.WithValue(r.Context(), variableWebRequestKey{}, -1))
		return true
	}
	v.mu.Lock()
	recording := v.recording
	v.mu.Unlock()
	if !recording {
		return true
	}
	project, target, command, ok := variableWebEndpoint(r)
	if !ok {
		return true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || len(raw) > 1<<20 {
		clear(raw)
		http.Error(w, "private Variables request observation failed", http.StatusBadRequest)
		return false
	}
	if command == "project.variable.create" {
		var body struct {
			Request struct {
				ID string `json:"variable_id"`
			} `json:"request"`
		}
		if json.Unmarshal(raw, &body) == nil {
			target = body.Request.ID
		}
	}
	if strings.HasSuffix(r.URL.Path, "/commands/lookup") {
		var body struct {
			Command string `json:"command"`
			Target  string `json:"target_id"`
			Request struct {
				ID string `json:"variable_id"`
			} `json:"request"`
		}
		if json.Unmarshal(raw, &body) == nil {
			command = body.Command
			target = body.Target
			if target == "" {
				target = body.Request.ID
			}
		}
	}
	v.mu.Lock()
	if len(v.attempts) >= 192 {
		v.mu.Unlock()
		clear(raw)
		http.Error(w, "private Variables observation bound", http.StatusServiceUnavailable)
		return false
	}
	a := &variableWebAttempt{Index: len(v.attempts) + 1, Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Project: project, Target: target, Command: command, Key: r.Header.Get("Idempotency-Key"), Body: append([]byte(nil), raw...), CSRF: sha256.Sum256([]byte(r.Header.Get("X-CSRF-Token")))}
	v.attempts = append(v.attempts, a)
	if v.dropIndex == 0 && v.dropProject == project && v.dropCommand == command && v.dropTarget == target && !strings.HasSuffix(a.Path, "/commands/lookup") {
		if a.Query != "" || a.Key == "" || r.Header.Get("X-CSRF-Token") == "" {
			v.mu.Unlock()
			http.Error(w, "private Variables cut original binding invalid", http.StatusBadRequest)
			return false
		}
		v.dropIndex = a.Index
	}
	v.mu.Unlock()
	*r = *r.WithContext(context.WithValue(r.Context(), variableWebRequestKey{}, a.Index))
	r.Body = &projectOwnerWebBody{Reader: bytes.NewReader(raw), raw: raw}
	return true
}
func (v *projectVariablesWebFixture) controlResponse(response *http.Response) error {
	v.mu.Lock()
	recording := v.recording
	v.mu.Unlock()
	if !recording {
		return nil
	}
	r := response.Request
	index, ok := r.Context().Value(variableWebRequestKey{}).(int)
	if ok && index == -1 {
		return nil
	}
	if !ok {
		return errors.New("private Variables response request binding missing")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 5<<20+1))
	closed := response.Body.Close()
	if err != nil || closed != nil || len(raw) > 5<<20 || !json.Valid(raw) {
		clear(raw)
		return errors.New("private Variables upstream body incomplete")
	}
	v.mu.Lock()
	if index < 1 || index > len(v.attempts) {
		v.mu.Unlock()
		clear(raw)
		return errors.New("private Variables response index invalid")
	}
	a := v.attempts[index-1]
	a.Status = response.StatusCode
	a.EOF = true
	a.Closed = true
	a.Receipt = append([]byte(nil), raw...)
	v.responses++
	sequence := v.responses
	hold := v.hold
	if hold == nil || hold.Path != r.URL.Path || r.Method != http.MethodGet || hold.Started {
		hold = nil
	} else {
		hold.Started = true
		hold.Index = index
	}
	drop := v.dropIndex == a.Index && v.dropProject == a.Project && v.dropCommand == a.Command && v.dropTarget == a.Target && response.StatusCode == http.StatusOK
	v.mu.Unlock()
	// This is the original complete backend representation, transferred only in
	// the private runtime. It is never logged or emitted into public evidence.
	v.owner.private("project-variables-response-"+strconv.Itoa(sequence)+".json", map[string]any{"index": index, "method": a.Method, "path": a.Path, "query": a.Query, "status": response.StatusCode, "body_b64": base64.StdEncoding.EncodeToString(raw), "content_type": response.Header.Get("Content-Type"), "request_b64": base64.StdEncoding.EncodeToString(a.Body), "request_id": response.Header.Get("X-Request-ID"), "key": a.Key, "csrf_sha256": fmt.Sprintf("%x", a.CSRF)})
	response.Body = &projectOwnerWebBody{Reader: bytes.NewReader(raw), raw: raw}
	if hold != nil {
		select {
		case <-hold.Release:
		case <-r.Context().Done():
		}
		v.mu.Lock()
		hold.Canceled = errors.Is(r.Context().Err(), context.Canceled)
		v.mu.Unlock()
	}
	if !drop {
		return nil
	}
	var completed bool
	if err := v.owner.store.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND target_id=$4 AND state='completed')`, a.Project, a.Command, a.Key, a.Target).Scan(&completed); err != nil || !completed {
		return errors.New("private Variables completed command absent before cut")
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil || body["command"] != a.Command {
		return errors.New("private Variables historical receipt invalid")
	}
	v.mu.Lock()
	v.dropProject = ""
	v.dropCommand = ""
	v.dropTarget = ""
	v.dropIndex = 0
	v.mu.Unlock()
	_ = response.Body.Close()
	return &projectOwnerWebLost{header: response.Header.Clone(), length: len(raw), variableCut: func(written int, writeErr, flushErr, hijackErr, closeErr error) {
		v.mu.Lock()
		defer v.mu.Unlock()
		a.Cut = &variableWebCut{written, writeErr == nil, flushErr == nil, hijackErr == nil, hijackErr == nil && closeErr == nil}
	}}
}

// This observes the original proxy handler's actual return, after its held
// response has seen the original context cancellation or explicit release.
func (v *projectVariablesWebFixture) handlerReturned(r *http.Request) {
	index, ok := r.Context().Value(variableWebRequestKey{}).(int)
	if !ok {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.hold != nil && v.hold.Started && v.hold.Index == index {
		v.hold.Finished = true
		v.hold.Canceled = errors.Is(r.Context().Err(), context.Canceled)
	}
}
func (v *projectVariablesWebFixture) releaseRead() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.hold != nil {
		v.hold.Once.Do(func() { close(v.hold.Release) })
	}
}

type variableWebIPC struct {
	Sequence int     `json:"sequence"`
	Action   string  `json:"action"`
	Project  string  `json:"project,omitempty"`
	Target   string  `json:"target,omitempty"`
	Kind     string  `json:"kind,omitempty"`
	Value    *string `json:"value,omitempty"`
	Name     *string `json:"name,omitempty"`
}

func (v *projectVariablesWebFixture) ipc(ctx context.Context, raw []byte, previous int) int {
	var in variableWebIPC
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&in)
	var extra any
	end := decoder.Decode(&extra)
	if err != nil || end != io.EOF || in.Sequence < 1 || in.Sequence > 128 {
		v.owner.t.Fatal("private Variables IPC invalid")
	}
	if in.Sequence <= previous {
		return previous
	}
	if in.Sequence != previous+1 {
		v.owner.t.Fatal("private Variables IPC sequence invalid")
	}
	project, ok := v.owner.ids[in.Project]
	if !ok || in.Project == "other" {
		v.owner.t.Fatal("private Variables IPC project rejected")
	}
	out := map[string]any{"sequence": in.Sequence}
	switch in.Action {
	case "arm-loss":
		if in.Kind != "create" && in.Kind != "update" && in.Kind != "delete" {
			v.owner.t.Fatal("private Variables cut kind invalid")
		}
		v.mu.Lock()
		if v.dropCommand != "" {
			v.mu.Unlock()
			v.owner.t.Fatal("private Variables cut already armed")
		}
		if _, err := f.ParseID[identity.ProjectVariable](in.Target); err != nil {
			v.mu.Unlock()
			v.owner.t.Fatal("private Variables cut target invalid")
		}
		v.dropTarget = in.Target
		v.dropIndex = 0
		v.dropProject = project
		v.dropCommand = "project.variable." + in.Kind
		v.mu.Unlock()
	case "hold-read":
		if _, err := f.ParseID[identity.ProjectVariable](in.Target); err != nil {
			v.owner.t.Fatal("private Variables hold target invalid")
		}
		v.releaseRead()
		v.mu.Lock()
		v.hold = &variableWebHold{Path: "/api/v1/projects/" + project + "/variables/" + in.Target, Release: make(chan struct{})}
		v.mu.Unlock()
	case "hold-status":
		v.mu.Lock()
		out["started"] = v.hold != nil && v.hold.Started
		out["finished"] = v.hold != nil && v.hold.Finished
		out["canceled"] = v.hold != nil && v.hold.Canceled
		v.mu.Unlock()
	case "release-read":
		v.releaseRead()
	case "fail-session":
		v.owner.mu.Lock()
		v.owner.failSession = true
		v.owner.mu.Unlock()
	case "update":
		if in.Value == nil && in.Name == nil {
			v.owner.t.Fatal("private Variables update presence missing")
		}
		current := v.request(ctx, in.Project, http.MethodGet, in.Target, nil, http.StatusOK)
		request := map[string]any{}
		if in.Value != nil {
			request["value"] = *in.Value
		}
		if in.Name != nil {
			request["name"] = *in.Name
		}
		out["receipt"] = v.request(ctx, in.Project, http.MethodPatch, in.Target, map[string]any{"expected_version": current["version"], "request": request}, http.StatusOK)
	case "archive":
		v.owner.ipc(ctx, projectOwnerWebIPC{Sequence: in.Sequence, Action: "archive", Project: in.Project})
		out["fact_only"] = true
	case "observe":
		var commands, history, audits, events int
		err := v.owner.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1 AND state='completed'),(SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='projectvariable'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND producer='projectvariable')`, project).Scan(&commands, &history, &audits, &events)
		if err != nil {
			v.owner.t.Fatal("private Variables persistent observation failed")
		}
		out["commands"] = commands
		out["history"] = history
		out["audits"] = audits
		out["events"] = events
	default:
		v.owner.t.Fatal("private Variables IPC action rejected")
	}
	v.owner.private("project-variables-ack-"+strconv.Itoa(in.Sequence)+".json", out)
	return in.Sequence
}
func (v *projectVariablesWebFixture) safeFailure() {
	// The browser's raw assertion output can contain ordinary values. Only its
	// closed step projection may leave private runtime; no PW Error text is logged.
	raw, err := os.ReadFile(filepath.Join(v.owner.directory, "project-variables-failure.json"))
	if err != nil {
		return
	}
	defer clear(raw)
	var value struct {
		Phase  string `json:"phase"`
		Step   int    `json:"step"`
		Status string `json:"status"`
		Source string `json:"source"`
		Line   int    `json:"line"`
		DOM    struct {
			Observed  bool `json:"observed"`
			Variables bool `json:"variables"`
			Editor    bool `json:"editor"`
			Close     bool `json:"close"`
			History   bool `json:"history"`
			Confirmed bool `json:"confirmed"`
			Uncertain bool `json:"uncertain"`
			Dialog    bool `json:"dialog"`
		} `json:"dom"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var tail any
	if decoder.Decode(&value) != nil || decoder.Decode(&tail) != io.EOF || !variableWebMode(value.Phase) || value.Step < 0 || value.Step > 128 {
		return
	}
	v.owner.t.Logf("Variables browser safe failure case=%s step=%d", value.Phase, value.Step)
	statusOK := value.Status == "failed" || value.Status == "timedOut" || value.Status == "interrupted" || value.Status == "other"
	sourceOK := value.Source == "spec" || value.Source == "helpers" || value.Source == "unknown"
	if statusOK && sourceOK && value.Line >= 0 && value.Line <= 4000 {
		v.owner.t.Logf("Variables browser safe failure status=%s source=%s line=%d dom_observed=%t variables=%t editor=%t close=%t history=%t confirmed=%t uncertain=%t dialog=%t", value.Status, value.Source, value.Line, value.DOM.Observed, value.DOM.Variables, value.DOM.Editor, value.DOM.Close, value.DOM.History, value.DOM.Confirmed, value.DOM.Uncertain, value.DOM.Dialog)
	}
}

// Independent SQL postconditions use each actual original key and the complete
// backend receipt. Browser completion flags cannot satisfy these assertions.
func (v *projectVariablesWebFixture) verify(ctx context.Context) {
	v.owner.stopProxy() // actual handler/connection join before reading the ledger
	v.mu.Lock()
	defer v.mu.Unlock()
	t := v.owner.t
	changed := map[string]bool{}
	cutKinds := map[string]int{}
	browserFacts := map[string]map[string]bool{}
	for _, a := range v.attempts {
		if !a.EOF || !a.Closed {
			t.Fatal("Variables observed response did not finish upstream body and Close")
		}
		if a.Command == "" || a.Status != http.StatusOK {
			continue
		}
		var body map[string]any
		if json.Unmarshal(a.Receipt, &body) != nil {
			t.Fatal("Variables private receipt invalid")
		}
		lookup := strings.HasSuffix(a.Path, "/commands/lookup")
		if lookup {
			if body["status"] != "committed" {
				continue
			}
			body, _ = body["receipt"].(map[string]any)
		}
		receipt, err := json.Marshal(body)
		if err != nil {
			t.Fatal("Variables private receipt encoding failed")
		}
		input := variableWebStoredInput(a, v.owner.owner.UserID)
		var rows, history, audits, events int
		err = v.owner.store.QueryRow(ctx, `SELECT count(*), coalesce(sum((SELECT count(*) FROM agenteam_projectvariable.history h WHERE h.project_id=c.project_id AND h.operation_id=c.id AND h.variable_id=c.target_id AND h.event_id=c.event_id)),0),coalesce(sum((SELECT count(*) FROM agenteam_audit.audit_records a WHERE a.id=(c.receipt->>'audit_id')::uuid AND a.project_id=c.project_id AND a.resource_id=c.target_id AND a.action=c.command_name AND a.producer='projectvariable' AND a.outcome='success')),0),coalesce(sum((SELECT count(*) FROM agenteam_outbox.events e WHERE e.id=c.event_id AND e.project_id=c.project_id AND e.aggregate_id=c.target_id AND e.producer='projectvariable')),0) FROM agenteam_projectvariable.commands c WHERE c.project_id=$1 AND c.command_name=$2 AND c.idempotency_key=$3 AND c.target_id=$4 AND c.state='completed' AND c.receipt=$5::jsonb AND c.actor_user_id=$6 AND c.request=$7::jsonb`, a.Project, a.Command, a.Key, a.Target, string(receipt), v.owner.owner.UserID, string(input)).Scan(&rows, &history, &audits, &events)
		clear(receipt)
		clear(input)
		if err != nil {
			t.Fatal("Variables original-key persistent receipt observation failed")
		}
		want := 0
		if body["changed"] == true {
			want = 1
		}
		if rows != 1 || history != want || audits != want || events != want {
			t.Fatalf("Variables original-key facts rows=%d history=%d audit=%d events=%d", rows, history, audits, events)
		}
		if browserFacts[a.Project] == nil {
			browserFacts[a.Project] = map[string]bool{}
		}
		browserFacts[a.Project][a.Command+":"+a.Key] = want == 1
		if !lookup {
			changed[a.Command] = true
		}
		if a.Cut != nil {
			cutKinds[a.Command]++
			if a.Cut.Written != 1 || !a.Cut.Write || !a.Cut.Flush || !a.Cut.Hijack || !a.Cut.Close {
				t.Fatal("Variables actual native cut was incomplete")
			}
			writes, lookups := 0, 0
			for _, later := range v.attempts {
				if later.Key != a.Key || later.Project != a.Project || later.Command != a.Command {
					continue
				}
				if later.Target != a.Target || later.CSRF != a.CSRF || later.Query != "" || later.Status != http.StatusOK {
					t.Fatal("Variables recovery changed original identity or target")
				}
				if strings.HasSuffix(later.Path, "/commands/lookup") {
					if later.Method != http.MethodPost || later.Path != "/api/v1/projects/"+a.Project+"/variables/commands/lookup" || !variableWebOriginalLookup(a, later) {
						t.Fatal("Variables Lookup changed the original request semantics or presence")
					}
					var result struct {
						Status  string          `json:"status"`
						Receipt json.RawMessage `json:"receipt"`
					}
					if json.Unmarshal(later.Receipt, &result) != nil || result.Status != "committed" || !jsonSemanticEqual(result.Receipt, a.Receipt) {
						t.Fatal("Variables Lookup replaced the historical receipt")
					}
					lookups++
					continue
				}
				if later.Method != a.Method || later.Path != a.Path || later.Query != a.Query || !bytes.Equal(later.Body, a.Body) {
					t.Fatal("Variables explicit replay changed original request")
				}
				if !jsonSemanticEqual(later.Receipt, a.Receipt) {
					t.Fatal("Variables replay replaced historical receipt")
				}
				writes++
			}
			if writes != 2 || lookups < 1 {
				t.Fatalf("Variables expected original write/replay/Lookup counts writes=%d lookup=%d", writes, lookups)
			}
		}
	}
	for _, project := range v.owner.ids {
		expected := v.setupFacts[project]
		for _, changed := range browserFacts[project] {
			expected[0]++
			if changed {
				expected[1]++
			}
		}
		var commands, completed, history, audits, events int
		err := v.owner.store.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1),(SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1 AND state='completed'),(SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='projectvariable'),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND producer='projectvariable')`, project).Scan(&commands, &completed, &history, &audits, &events)
		if err != nil || commands != expected[0] || completed != expected[0] || history != expected[1] || audits != expected[1] || events != expected[1] {
			t.Fatal("Variables seed and distinct browser intent totals do not match persistent facts")
		}
	}

	if v.mode == "recovery" {
		for _, kind := range []string{"create", "update", "delete"} {
			if cutKinds["project.variable."+kind] != 1 {
				t.Fatal("Variables each original command needs one actual cut")
			}
		}
	}
	if v.mode == "crud" {
		for _, kind := range []string{"create", "update", "delete"} {
			if !changed["project.variable."+kind] {
				t.Fatal("Variables CRUD persistent coverage incomplete")
			}
		}
	}
	if v.mode == "identity" && (v.hold == nil || !v.hold.Started || !v.hold.Finished || !v.hold.Canceled) {
		t.Fatal("Variables held original read cancellation was not actually joined")
	}
	if v.dropCommand != "" {
		t.Fatal("Variables cut declaration was never consumed")
	}
}
func jsonSemanticEqual(left, right []byte) bool {
	var a, b any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	defer clear(x)
	defer clear(y)
	return bytes.Equal(x, y)
}

func variableWebOriginalLookup(original, lookup *variableWebAttempt) bool {
	var body map[string]json.RawMessage
	if json.Unmarshal(original.Body, &body) != nil {
		return false
	}
	expected := map[string]any{"command": original.Command}
	if original.Command != "project.variable.create" {
		expected["target_id"] = original.Target
		expected["expected_version"] = body["expected_version"]
	}
	if original.Command != "project.variable.delete" {
		expected["request"] = body["request"]
	}
	raw, err := json.Marshal(expected)
	if err != nil {
		return false
	}
	defer clear(raw)
	return jsonSemanticEqual(raw, lookup.Body)
}

func variableWebStoredInput(a *variableWebAttempt, user string) []byte {
	var body map[string]json.RawMessage
	if json.Unmarshal(a.Body, &body) != nil {
		return nil
	}
	input := map[string]any{"project_id": a.Project, "actor_user_id": user, "command": a.Command, "target_id": a.Target}
	if a.Command != "project.variable.create" {
		input["expected_version"] = body["expected_version"]
	}
	if a.Command == "project.variable.create" {
		input["create"] = body["request"]
	}
	if a.Command == "project.variable.update" {
		input["update"] = body["request"]
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	return raw
}
