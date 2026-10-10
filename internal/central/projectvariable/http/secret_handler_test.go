package projectvariablehttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// Only transport controls. These do not prove Account or SQL authorization.
type secretTestPort struct {
	c.SecretCommands
	c.SecretQueries
	before    func(context.Context)
	calls     int
	err       error
	value     c.SecretVariable
	page      f.Page[c.SecretVariable]
	result    c.SecretVariableMutation
	lookup    c.SecretVariableCommandLookup
	actor     id.Actor
	project   c.ProjectID
	target    c.VariableID
	meta      f.CommandMeta
	query     f.PageRequest
	lookupIn  c.SecretVariableCommandLookupRequest
	create    *c.SecretVariableCreate
	update    *c.SecretVariableUpdate
	useCreate func(c.SecretVariableCreate)
	useUpdate func(c.SecretVariableUpdate)
}

func (p *secretTestPort) enter(ctx context.Context, actor id.Actor, project c.ProjectID) {
	p.calls++
	p.actor, p.project = actor, project
	if p.before != nil {
		p.before(ctx)
	}
}
func (p *secretTestPort) GetSecretVariable(ctx context.Context, a id.Actor, project c.ProjectID, target c.VariableID) (c.SecretVariable, error) {
	p.target = target
	p.enter(ctx, a, project)
	return p.value, p.err
}
func (p *secretTestPort) ListSecretVariables(ctx context.Context, a id.Actor, project c.ProjectID, q f.PageRequest) (f.Page[c.SecretVariable], error) {
	p.query = q
	p.enter(ctx, a, project)
	return p.page, p.err
}
func (p *secretTestPort) CreateSecretVariable(ctx context.Context, a id.Actor, m f.CommandMeta, project c.ProjectID, q c.SecretVariableCreate) (c.SecretVariableMutation, error) {
	p.create, p.meta, p.target = &q, m, q.Fields().ID
	if p.useCreate != nil {
		p.useCreate(q)
	}
	p.enter(ctx, a, project)
	return p.result, p.err
}
func (p *secretTestPort) UpdateSecretVariable(ctx context.Context, a id.Actor, m f.CommandMeta, project c.ProjectID, target c.VariableID, q c.SecretVariableUpdate) (c.SecretVariableMutation, error) {
	p.update, p.meta, p.target = &q, m, target
	if p.useUpdate != nil {
		p.useUpdate(q)
	}
	p.enter(ctx, a, project)
	return p.result, p.err
}
func (p *secretTestPort) DeleteSecretVariable(ctx context.Context, a id.Actor, m f.CommandMeta, project c.ProjectID, target c.VariableID) (c.SecretVariableMutation, error) {
	p.meta, p.target = m, target
	p.enter(ctx, a, project)
	return p.result, p.err
}
func (p *secretTestPort) LookupSecretVariableCommand(ctx context.Context, a id.Actor, q c.SecretVariableCommandLookupRequest) (c.SecretVariableCommandLookup, error) {
	p.lookupIn = q
	p.enter(ctx, a, q.Fields().ProjectID)
	return p.lookup, p.err
}

func secretTestValue(t *testing.T, version f.Version) c.SecretVariable {
	t.Helper()
	v, err := c.NewSecretVariable(c.SecretVariableFields{ID: testID[id.ProjectVariable](3), ProjectID: testID[id.Project](4), Type: c.SecretVariableType, Name: "NAME", Description: "public description", Version: version, CreatedAt: testAt(), UpdatedAt: testAt()})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func secretTestHandler(t *testing.T) (*secretHandler, *testBoundary, *secretTestPort) {
	t.Helper()
	b := &testBoundary{}
	v := secretTestValue(t, 1)
	p := &secretTestPort{value: v, page: f.Page[c.SecretVariable]{Items: []c.SecretVariable{v}}}
	return &secretHandler{p, b}, b, p
}
func secretTestReceipt(t *testing.T, command c.SecretCommandName, changed bool, version f.Version) c.SecretVariableMutation {
	t.Helper()
	fields := c.SecretVariableMutationFields{Command: command, Changed: changed}
	if changed {
		event, audit := testID[ec.EventIdentity](9), testID[ac.Record](10)
		fields.EventID, fields.AuditID = &event, &audit
	}
	if command == c.SecretDeleteCommand {
		fields.Deleted = &c.SecretVariableDeleted{ID: testID[id.ProjectVariable](3), ProjectID: testID[id.Project](4), Type: c.SecretVariableType, Version: version, DeletedAt: testAt()}
	} else {
		fields.Variable = secretTestValue(t, version)
	}
	v, err := c.NewSecretVariableMutation(fields)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSecretHTTPRoutesAndSafeReads(t *testing.T) {
	if _, err := NewSecretHTTPHandler(nil, nil); err == nil {
		t.Fatal("nil dependencies admitted")
	}
	for _, path := range []string{"/variables", "/secret-variables/", "/secret-variables/x/y", "/secret-variables/commands/other"} {
		if HandlesSecretPath(testPath(path)) {
			t.Fatal("unowned route admitted")
		}
	}
	for _, suffix := range []string{"/secret-variables", "/secret-variables/" + testID[id.ProjectVariable](3).String()} {
		var getSize int
		for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
			h, _, p := secretTestHandler(t)
			r := httptest.NewRequest(method, testPath(suffix), nil)
			w := newTestWriter()
			if serveTest(h, r, w) {
				t.Fatal("unexpected abort")
			}
			if method == "OPTIONS" {
				if w.Code != 405 || w.Header().Get("Allow") != secretRoute(r.URL.Path).allow() || p.calls != 0 {
					t.Fatal("method boundary")
				}
				continue
			}
			if w.Code != 200 || p.calls != 1 || p.actor.Details() != testActor().Details() || p.project != testID[id.Project](4) || !strings.Contains(r.Pattern, "{project_id}/secret-variables") {
				t.Fatal("read routing or original actor")
			}
			if method == "GET" {
				getSize = w.Body.Len()
				for _, forbidden := range []string{"\"value\"", "ciphertext", "credential", "digest", "mask"} {
					if strings.Contains(w.Body.String(), forbidden) {
						t.Fatal("unsafe read field")
					}
				}
			} else if w.Body.Len() != 0 || w.Header().Get("Content-Length") != strconv.Itoa(getSize) {
				t.Fatal("HEAD diverged from complete GET representation")
			}
			w.cleared(t)
		}
	}
}

func TestSecretHTTPWholePageAndStrictQueries(t *testing.T) {
	for _, query := range []string{"?", "?limit=01", "?limit=101", "?limit=1&limit=2", "?cursor=", "?cursor=x;limit=1", "?unknown=canary"} {
		h, _, p := secretTestHandler(t)
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath("/secret-variables")+query, nil), w) || w.Code != 400 || p.calls != 0 {
			t.Fatal("query accepted", query)
		}
	}
	for _, kind := range []string{"nil", "duplicate", "foreign", "unsorted", "bad-last", "short-cursor"} {
		h, _, p := secretTestHandler(t)
		switch kind {
		case "nil":
			p.page.Items = nil
		case "duplicate":
			p.page.Items = append(p.page.Items, p.value)
		case "foreign", "unsorted":
			fields := p.value.Fields()
			fields.ID = testID[id.ProjectVariable](99)
			if kind == "foreign" {
				fields.ProjectID = testID[id.Project](99)
			} else {
				fields.Name = "AAA"
			}
			v, err := c.NewSecretVariable(fields)
			if err != nil {
				t.Fatal(err)
			}
			p.page.Items = append(p.page.Items, v)
		case "bad-last":
			p.page.Items = append(p.page.Items, c.SecretVariable{})
		case "short-cursor":
			p.page.NextCursor = "opaque"
		}
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath("/secret-variables"), nil), w) || w.Code != 503 || strings.Contains(w.Body.String(), "public description") {
			t.Fatal("partial or invalid page escaped", kind)
		}
	}
}

func TestSecretHTTPClosedProblemProjection(t *testing.T) {
	h, _, p := secretTestHandler(t)
	problem := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("value-canary"))
	problem.SafeMessage, problem.RetryHint = "value-canary", "value-canary"
	problem.CauseID = testID[f.TransactionAttempt](42).String()
	problem.FieldErrors = []f.FieldError{{Path: "/value-canary", Code: "INVALID_FIELD"}, {Path: "/expected_version", Code: "VALUE_CANARY"}, {Path: "/expected_version", Code: "INVALID_FIELD"}}
	p.err = problem
	w := newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", testPath("/secret-variables"), nil), w) {
		t.Fatal("problem unexpectedly aborted")
	}
	var body struct {
		Code   f.Code         `json:"code"`
		State  f.CommitState  `json:"commit_state"`
		Hint   string         `json:"retry_hint"`
		Fields []f.FieldError `json:"field_errors"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Code != f.CommitUnknown || body.State != f.Unknown || body.Hint != "lookup" || len(body.Fields) != 1 || strings.Contains(strings.ToLower(w.Body.String()), "canary") {
		t.Fatal("problem was not a safe Unknown projection")
	}
	var saved *f.Fault
	if !errors.As(secretProblem(problem), &saved) || saved.CauseID != problem.CauseID || !errors.Is(saved, problem.Unwrap()) {
		t.Fatal("original cause/attempt was replaced")
	}
}
