package agenthttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	e "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// All ports in this file are controlled transport observations, not current
// Owner grants, SQL initialization or evidence that default references exist.
func configurationCreateJSON() string {
	return `{"agent_id":"` + directoryID[i.Agent](3).String() + `","name":"Agent-One","model_ref":"01900000-0000-7000-8000-000000000020","inject_agents_md":true,"approval_policy":"default","allowed_tool_ids":[],"allowed_mount_ids":[],"allowed_secret_variable_ids":[]}`
}
func configurationValue(t *testing.T, request c.AgentCreate) c.AgentConfig {
	t.Helper()
	v := request.Fields()
	at := directorySample().CreatedAt
	tools := v.AllowedToolIDs
	if *v.InstallSkillEnabled && len(tools) == 0 {
		tools = []i.ToolID{directoryID[i.Tool](30)}
	}
	out, err := c.NewAgentConfig(c.AgentConfigFields{
		Core:           c.AgentCore{ID: v.AgentID, ProjectID: directoryID[i.Project](4), Name: v.Name, NormalizedName: strings.ToLower(v.Name), DisplayName: v.DisplayName, TagColor: v.TagColor, Description: v.Description, Instructions: v.Instructions, InjectAgentsMD: v.InjectAgentsMD, ModelRef: v.ModelRef, ReasoningEffort: v.ReasoningEffort, ApprovalPolicy: v.ApprovalPolicy, ApprovalModelRef: v.ApprovalModelRef, Lifecycle: c.AgentActive, Version: 1, CreatedAt: at, UpdatedAt: at},
		AllowedToolIDs: tools, AllowedMountIDs: v.AllowedMountIDs, AllowedSecretVariableIDs: v.AllowedSecretVariableIDs,
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func configurationMutation(t *testing.T, config c.AgentConfig) c.AgentMutation {
	t.Helper()
	out, err := c.NewAgentMutation(c.AgentMutationFields{Agent: config, Changed: true, EventIDs: []e.EventID{directoryID[e.EventIdentity](40)}})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type configurationTestCommands struct {
	t                               *testing.T
	creates, updates, gets, lookups int
	request                         c.AgentCreate
	update                          c.AgentUpdate
	target                          i.AgentID
	meta                            f.CommandMeta
	actor                           i.Actor
	project                         i.ProjectID
	digest                          f.Digest
	key                             f.IdempotencyKey
	command                         c.CommandName
	config                          c.AgentConfig
	mutation                        *c.AgentMutation
	status                          c.AgentLookupStatus
	err                             error
	before                          func(context.Context)
}

func (p *configurationTestCommands) CreateAgent(ctx context.Context, actor i.Actor, meta f.CommandMeta, project i.ProjectID, request c.AgentCreate) (c.AgentMutation, error) {
	p.creates++
	p.request, p.meta, p.actor, p.project = request.Clone(), meta, actor, project
	if p.before != nil {
		p.before(ctx)
	}
	if p.err != nil {
		return c.AgentMutation{}, p.err
	}
	if p.mutation != nil {
		return p.mutation.Clone(), nil
	}
	p.config = configurationValue(p.t, request)
	return configurationMutation(p.t, p.config), nil
}
func (p *configurationTestCommands) GetAgent(_ context.Context, _ i.Actor, project i.ProjectID, target i.AgentID) (c.AgentConfig, error) {
	p.gets++
	if project != directoryID[i.Project](4) || target != directoryID[i.Agent](3) {
		p.t.Fatal("read scope changed")
	}
	return p.config.Clone(), p.err
}
func (p *configurationTestCommands) UpdateAgent(ctx context.Context, actor i.Actor, meta f.CommandMeta, project i.ProjectID, target i.AgentID, request c.AgentUpdate) (c.AgentMutation, error) {
	p.updates++
	p.update, p.meta, p.actor, p.project, p.target = request.Clone(), meta, actor, project, target
	if p.before != nil {
		p.before(ctx)
	}
	if p.err != nil {
		return c.AgentMutation{}, p.err
	}
	if p.mutation == nil {
		p.t.Fatal("update transport test requires an explicit controlled receipt")
	}
	return p.mutation.Clone(), nil
}
func (p *configurationTestCommands) LookupAgentCommand(_ context.Context, actor i.Actor, project i.ProjectID, command c.CommandName, key f.IdempotencyKey, digest f.Digest) (c.AgentCommandLookup, error) {
	p.lookups++
	p.actor, p.project, p.command, p.key, p.digest = actor, project, command, key, digest
	if p.err != nil {
		return c.AgentCommandLookup{}, p.err
	}
	var receipt *c.AgentMutation
	if p.status == c.AgentLookupCommitted {
		v := configurationMutation(p.t, p.config)
		if p.mutation != nil {
			v = p.mutation.Clone()
		}
		receipt = &v
	}
	return c.NewAgentCommandLookup(p.status, receipt)
}

const configurationUpdateJSON = `{"description":"updated","display_name":null,"inject_agents_md":false,"allowed_tool_ids":[]}`

func configurationUpdateHTTPRequest(lookup bool, expected, request string) *http.Request {
	path := directoryPath() + "/" + directoryID[i.Agent](3).String() + "/configuration"
	method, prefix := http.MethodPatch, ""
	if lookup {
		path = directoryPrefix + directoryID[i.Project](4).String() + "/agent-commands/lookup"
		method, prefix = http.MethodPost, `"command":"agent.update","target_id":"`+directoryID[i.Agent](3).String()+`",`
	}
	r := httptest.NewRequest(method, path, strings.NewReader(`{`+prefix+`"expected_version":`+expected+`,"request":`+request+`}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-agent-update")
	return r
}

func configurationUpdateMutation(t *testing.T, version f.Version, changed bool) c.AgentMutation {
	t.Helper()
	request, err := c.DecodeAgentCreate([]byte(configurationCreateJSON()))
	if err != nil {
		t.Fatal(err)
	}
	fields := configurationValue(t, request).Fields()
	fields.Core.Version = version
	fields.Core.Description, fields.Core.DisplayName, fields.Core.InjectAgentsMD = "updated", nil, false
	fields.AllowedToolIDs = []i.ToolID{}
	config, err := c.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	events := []e.EventID{}
	if changed {
		events = append(events, directoryID[e.EventIdentity](40))
	}
	mutation, err := c.NewAgentMutation(c.AgentMutationFields{Agent: config, Changed: changed, EventIDs: events})
	if err != nil {
		t.Fatal(err)
	}
	return mutation
}

func TestAgentConfigurationHTTPUpdateAndHistoricalLookup(t *testing.T) {
	h, _, port := configurationFixture(t)
	changed := configurationUpdateMutation(t, 2, true)
	port.mutation = &changed
	r := configurationUpdateHTTPRequest(false, `"1"`, configurationUpdateJSON)
	if !HandlesConfigurationRequest(r) {
		t.Fatal("PATCH configuration dispatch missing")
	}
	port.before = func(ctx context.Context) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > configurationMutationBudget || time.Until(deadline) <= readBudget {
			t.Fatal("PATCH did not retain the mutation budget")
		}
	}
	w, aborted := directoryServe(h, r)
	if aborted || w.Code != 200 || port.updates != 1 || port.creates != 0 || port.gets != 0 || port.lookups != 0 || port.meta.ExpectedVersion == nil || *port.meta.ExpectedVersion != 1 || port.target != directoryID[i.Agent](3) {
		t.Fatal("PATCH did not call exactly the original update with caller identity")
	}
	value, err := c.DecodeAgentMutation(w.Body.Bytes())
	if err != nil || mustConfigurationJSON(t, value) != mustConfigurationJSON(t, changed) || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("changed update receipt/framing", err)
	}
	originalMeta, originalPatch := port.meta, port.update.Clone()
	originalDigest, err := c.AgentCommandDigest(port.actor, originalMeta, port.project, port.target, c.UpdateAgentCommand, originalPatch)
	if err != nil {
		t.Fatal(err)
	}
	port.before = nil
	// Current config deliberately differs. Lookup must consume only the exact
	// historical receipt, without reading it or substituting a current version.
	port.config = configurationUpdateMutation(t, 9, true).Fields().Agent
	for _, status := range []c.AgentLookupStatus{c.AgentLookupNotObserved, c.AgentLookupInProgress, c.AgentLookupCommitted} {
		port.status = status
		w, aborted = directoryServe(h, configurationUpdateHTTPRequest(true, `"1"`, configurationUpdateJSON))
		lookup, err := c.DecodeAgentCommandLookup(w.Body.Bytes())
		if aborted || w.Code != 200 || err != nil || lookup.Status() != status || port.digest != originalDigest || port.command != c.UpdateAgentCommand || port.key != originalMeta.IdempotencyKey || port.updates != 1 || port.gets != 0 {
			t.Fatal("update lookup changed original identity or caused another operation", err)
		}
		if status == c.AgentLookupCommitted && mustConfigurationJSON(t, lookup.Receipt()) != mustConfigurationJSON(t, changed) {
			t.Fatal("historical receipt was replaced")
		}
	}
	noop := configurationUpdateMutation(t, 7, false)
	port.mutation = &noop
	w, aborted = directoryServe(h, configurationUpdateHTTPRequest(false, `"7"`, configurationUpdateJSON))
	value, err = c.DecodeAgentMutation(w.Body.Bytes())
	if aborted || w.Code != 200 || err != nil || value.Fields().Changed || value.Fields().Agent.Fields().Core.Version != 7 || len(value.Fields().EventIDs) != 0 || port.updates != 2 {
		t.Fatal("no-op was made into a change", err)
	}
	port.status = c.AgentLookupCommitted
	w, aborted = directoryServe(h, configurationUpdateHTTPRequest(true, `"7"`, configurationUpdateJSON))
	lookup, err := c.DecodeAgentCommandLookup(w.Body.Bytes())
	if aborted || w.Code != 200 || err != nil || lookup.Receipt() == nil || lookup.Receipt().Fields().Changed || port.updates != 2 {
		t.Fatal("no-op lookup lost its receipt", err)
	}
	otherTarget := configurationUpdateHTTPRequest(true, `"7"`, configurationUpdateJSON)
	otherBody := `{"command":"agent.update","target_id":"` + directoryID[i.Agent](5).String() + `","expected_version":"7","request":` + configurationUpdateJSON + `}`
	otherTarget.Body, otherTarget.ContentLength = configurationTestBody(otherBody), int64(len(otherBody))
	for _, request := range []*http.Request{
		configurationUpdateHTTPRequest(true, `"8"`, configurationUpdateJSON),
		configurationUpdateHTTPRequest(true, `"7"`, strings.Replace(configurationUpdateJSON, `"display_name":null,`, "", 1)),
		configurationUpdateHTTPRequest(true, `"7"`, strings.Replace(configurationUpdateJSON, `"updated"`, `"different"`, 1)),
		otherTarget,
	} {
		port.status = c.AgentLookupNotObserved
		w, aborted = directoryServe(h, request)
		want, e := c.AgentCommandDigest(port.actor, port.meta, port.project, port.target, c.UpdateAgentCommand, port.update)
		if aborted || w.Code != 200 || e != nil || port.digest == want || port.updates != 2 || port.gets != 0 {
			t.Fatal("lookup ignored target/expected/presence/value changes")
		}
	}
}

func TestAgentConfigurationHTTPUpdateBoundaryAndReceipt(t *testing.T) {
	h, boundary, port := configurationFixture(t)
	boundary.checkErr = f.NewFault(f.CSRFFailed, f.NotStarted)
	w, abort := directoryServe(h, configurationUpdateHTTPRequest(false, `"1"`, configurationUpdateJSON))
	if abort || w.Code != 403 || port.updates != 0 {
		t.Fatal("PATCH bypassed CSRF")
	}
	boundary.checkErr = nil
	boundary.authErr = f.NewFault(f.SessionRevoked, f.NotStarted)
	w, abort = directoryServe(h, configurationUpdateHTTPRequest(true, `"1"`, configurationUpdateJSON))
	if abort || w.Code != 401 || port.lookups != 0 {
		t.Fatal("update lookup bypassed current Session")
	}
	boundary.authErr = nil
	for _, code := range []f.Code{f.NotFound, f.VersionConflict, f.IdempotencyKeyReused} {
		port.err = f.NewFault(code, f.NotStarted)
		w, abort = directoryServe(h, configurationUpdateHTTPRequest(false, `"1"`, configurationUpdateJSON))
		if abort || !strings.Contains(w.Body.String(), `"code":"`+string(code)+`"`) || port.gets != 0 {
			t.Fatal("current Owner/version/intent fault changed")
		}
	}
	port.err = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("private-update-cause"))
	before := port.updates
	w, abort = directoryServe(h, configurationUpdateHTTPRequest(false, `"1"`, configurationUpdateJSON))
	if abort || w.Code != 503 || port.updates != before+1 || port.lookups != 0 || !strings.Contains(w.Body.String(), `"commit_state":"unknown"`) || strings.Contains(w.Body.String(), "private-update-cause") {
		t.Fatal("Unknown was hidden, leaked or retried")
	}
	port.err = nil
	for _, invalid := range []c.AgentMutation{
		configurationUpdateMutation(t, 3, true),
		configurationUpdateMutation(t, 2, false),
	} {
		port.mutation = &invalid
		w, abort = directoryServe(h, configurationUpdateHTTPRequest(false, `"1"`, configurationUpdateJSON))
		if !abort || w.Body.Len() != 0 {
			t.Fatal("invalid successful update was published/relabelled")
		}
		port.status = c.AgentLookupCommitted
		w, abort = directoryServe(h, configurationUpdateHTTPRequest(true, `"1"`, configurationUpdateJSON))
		if abort || w.Code != 503 || strings.Contains(w.Body.String(), "updated") {
			t.Fatal("Lookup published invalid historical version/change")
		}
	}
	valid := configurationUpdateMutation(t, 2, true)
	port.mutation = &valid
	for _, patch := range []string{`{"description":"different"}`, `{"allowed_tool_ids":["` + directoryID[i.Tool](30).String() + `"]}`, `{"inject_agents_md":true}`} {
		w, abort = directoryServe(h, configurationUpdateHTTPRequest(false, `"1"`, patch))
		if !abort || w.Body.Len() != 0 {
			t.Fatal("explicit patch was not checked against original receipt")
		}
	}
	path := directoryPath() + "/" + directoryID[i.Agent](3).String() + "/configuration"
	w, abort = directoryServe(h, httptest.NewRequest(http.MethodPost, path, nil))
	if abort || w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD, PATCH" {
		t.Fatal("configuration method set changed")
	}
	maximum := configurationUpdateMutation(t, 9223372036854775807, false)
	port.mutation = &maximum
	w, abort = directoryServe(h, configurationUpdateHTTPRequest(false, `"9223372036854775807"`, configurationUpdateJSON))
	if abort || w.Code != 200 {
		t.Fatal("maximum-version no-op was rejected")
	}
	maximum = configurationUpdateMutation(t, 9223372036854775807, true)
	w, abort = directoryServe(h, configurationUpdateHTTPRequest(false, `"9223372036854775807"`, configurationUpdateJSON))
	if !abort || w.Body.Len() != 0 {
		t.Fatal("changed receipt overflowed original expected version")
	}
	port.mutation = &valid
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	port.before = func(context.Context) { entered <- struct{}{}; <-release }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := configurationUpdateHTTPRequest(false, `"1"`, configurationUpdateJSON).WithContext(ctx)
	body := &directoryBody{Reader: r.Body}
	r.Body = body
	done := make(chan bool, 1)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	go func() { _, aborted := directoryServe(h, r); done <- aborted }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("original update did not enter")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("PATCH returned before its original service call")
	default:
	}
	if body.closes.Load() != 0 {
		t.Fatal("PATCH body retired before original call")
	}
	once.Do(func() { close(release) })
	select {
	case aborted := <-done:
		if !aborted || body.closes.Load() != 1 {
			t.Fatal("canceled PATCH published a result or failed to close its body")
		}
	case <-time.After(time.Second):
		t.Fatal("original update did not return")
	}
}
func configurationFixture(t *testing.T) (*configurationHandler, *directoryTestBoundary, *configurationTestCommands) {
	t.Helper()
	request, err := c.DecodeAgentCreate([]byte(configurationCreateJSON()))
	if err != nil {
		t.Fatal(err)
	}
	boundary := &directoryTestBoundary{}
	port := &configurationTestCommands{t: t, config: configurationValue(t, request), status: c.AgentLookupCommitted}
	return &configurationHandler{port, boundary}, boundary, port
}
func configurationHTTPRequest(lookup bool, request string) *http.Request {
	path := directoryPath()
	body := `{"request":` + request + `}`
	if lookup {
		path = directoryPrefix + directoryID[i.Project](4).String() + "/agent-commands/lookup"
		body = `{"command":"agent.create","request":` + request + `}`
	}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "original-agent-create")
	return r
}

func TestAgentConfigurationHTTPCreateReadAndLookup(t *testing.T) {
	h, _, port := configurationFixture(t)
	if _, err := NewConfigurationHTTPHandler(nil, nil); err == nil {
		t.Fatal("missing actual dependencies accepted")
	}
	r := configurationHTTPRequest(false, configurationCreateJSON())
	if !HandlesConfigurationRequest(r) {
		t.Fatal("create dispatch missing")
	}
	for _, method := range []string{"GET", "HEAD", "PATCH", "DELETE"} {
		if HandlesConfigurationRequest(httptest.NewRequest(method, directoryPath(), nil)) {
			t.Fatal("directory collection was captured")
		}
	}
	if HandlesConfigurationRequest(httptest.NewRequest("GET", directoryPath()+"/"+directoryID[i.Agent](3).String(), nil)) {
		t.Fatal("directory detail was captured")
	}
	w, aborted := directoryServe(h, r)
	if aborted || w.Code != 200 || port.creates != 1 {
		t.Fatal("create response")
	}
	mutation, err := c.DecodeAgentMutation(w.Body.Bytes())
	if err != nil || !mutation.Fields().Changed || mutation.Fields().Agent.Fields().Core.Version != 1 || len(mutation.Fields().Agent.Fields().AllowedToolIDs) != 1 {
		t.Fatal("compound default receipt", err)
	}
	if !*port.request.Fields().AddSkillsEnabled || !*port.request.Fields().InstallSkillEnabled || port.meta.ExpectedVersion != nil {
		t.Fatal("create defaults/meta changed")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
		t.Fatal("response framing")
	}
	path := directoryPath() + "/" + directoryID[i.Agent](3).String() + "/configuration"
	get, abort := directoryServe(h, httptest.NewRequest("GET", path, nil))
	head, headAbort := directoryServe(h, httptest.NewRequest("HEAD", path, nil))
	if abort || headAbort || get.Code != 200 || head.Code != 200 || port.gets != 2 || head.Body.Len() != 0 || head.Header().Get("Content-Length") != get.Header().Get("Content-Length") {
		t.Fatal("config GET/HEAD")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(get.Body.Bytes(), &fields) != nil || len(fields) != 20 || fields["instructions"] == nil || fields["allowed_secret_variable_ids"] == nil || fields["add_skills_enabled"] != nil {
		t.Fatal("configuration replaced by directory/default DTO")
	}
	originalMeta := port.meta
	for _, status := range []c.AgentLookupStatus{c.AgentLookupNotObserved, c.AgentLookupInProgress, c.AgentLookupCommitted} {
		port.status = status
		w, aborted = directoryServe(h, configurationHTTPRequest(true, configurationCreateJSON()))
		lookup, err := c.DecodeAgentCommandLookup(w.Body.Bytes())
		if aborted || w.Code != 200 || err != nil || lookup.Status() != status || port.creates != 1 {
			t.Fatal("lookup union or writer replay", err)
		}
		want, err := c.AgentCommandDigest(port.actor, originalMeta, port.project, port.request.Fields().AgentID, c.CreateAgentCommand, port.request)
		if err != nil || port.digest != want || port.command != c.CreateAgentCommand || port.key != originalMeta.IdempotencyKey {
			t.Fatal("lookup did not bind original intent")
		}
	}
}

func TestAgentConfigurationHTTPBoundaryAndOriginalCall(t *testing.T) {
	h, boundary, port := configurationFixture(t)
	boundary.checkErr = f.NewFault(f.CSRFFailed, f.NotStarted)
	w, aborted := directoryServe(h, configurationHTTPRequest(false, configurationCreateJSON()))
	if aborted || w.Code != 403 || port.creates != 0 {
		t.Fatal("CSRF boundary bypassed")
	}
	boundary.checkErr = nil
	boundary.authErr = f.NewFault(f.SessionRevoked, f.NotStarted)
	w, aborted = directoryServe(h, configurationHTTPRequest(true, configurationCreateJSON()))
	if aborted || w.Code != 401 || port.lookups != 0 {
		t.Fatal("lookup bypassed current Session")
	}
	boundary.authErr = nil
	unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("private-agent-configuration"))
	port.err = unknown
	w, aborted = directoryServe(h, configurationHTTPRequest(false, configurationCreateJSON()))
	if aborted || w.Code != 503 || port.creates != 1 || port.lookups != 0 || !strings.Contains(w.Body.String(), `"commit_state":"unknown"`) || strings.Contains(w.Body.String(), "private-agent-configuration") {
		t.Fatal("Unknown was exposed or retried")
	}
	port.err = nil
	zero := c.AgentMutation{}
	port.mutation = &zero
	w, aborted = directoryServe(h, configurationHTTPRequest(false, configurationCreateJSON()))
	if !aborted || w.Body.Len() != 0 {
		t.Fatal("invalid successful writer became a known failure")
	}
	port.mutation = nil
	entered, release := make(chan context.Context, 1), make(chan struct{})
	var once sync.Once
	port.before = func(ctx context.Context) { entered <- ctx; <-release }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := configurationHTTPRequest(false, configurationCreateJSON()).WithContext(ctx)
	body := &directoryBody{Reader: r.Body}
	r.Body = body
	done := make(chan bool, 1)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	go func() { _, abort := directoryServe(h, r); done <- abort }()
	select {
	case original := <-entered:
		if deadline, ok := original.Deadline(); !ok || time.Until(deadline) > configurationMutationBudget {
			t.Fatal("natural mutation deadline absent")
		}
	case <-time.After(time.Second):
		t.Fatal("original call did not enter")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("returned before original call")
	default:
	}
	if body.closes.Load() != 0 {
		t.Fatal("body closed before original call")
	}
	once.Do(func() { close(release) })
	select {
	case abort := <-done:
		if !abort || body.closes.Load() != 1 {
			t.Fatal("canceled response or body retirement")
		}
	case <-time.After(time.Second):
		t.Fatal("original call did not return")
	}
}
