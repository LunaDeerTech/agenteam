package skillhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type assignmentHTTPControl struct {
	writes, lookups, reads int
	meta                   f.CommandMeta
	input                  skill.AssignmentRequest
	query                  skill.OwnerAssignmentQuery
	ctx                    context.Context
	receipt                sc.AssignmentMutation
	lookup                 skill.AssignmentLookup
	page                   skill.OwnerAssignmentPage
	err                    error
}

func (p *assignmentHTTPControl) ExecuteAssignment(ctx context.Context, _ i.Actor, meta f.CommandMeta, input skill.AssignmentRequest) (sc.AssignmentMutation, error) {
	p.writes++
	p.ctx, p.meta, p.input = ctx, meta, input
	return p.receipt, p.err
}
func (p *assignmentHTTPControl) LookupAssignment(ctx context.Context, _ i.Actor, meta f.CommandMeta, input skill.AssignmentRequest) (skill.AssignmentLookup, error) {
	p.lookups++
	p.ctx, p.meta, p.input = ctx, meta, input
	return p.lookup, p.err
}
func (p *assignmentHTTPControl) List(ctx context.Context, _ i.Actor, _ i.ProjectID, _ i.AgentID, q skill.OwnerAssignmentQuery) (skill.OwnerAssignmentPage, error) {
	p.reads++
	p.ctx, p.query = ctx, q
	return p.page, p.err
}

func assignmentHTTPFixture() (*assignmentHandler, *testBoundary, *assignmentHTTPControl) {
	at, _ := f.NewInstant(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	a := sc.AgentSkillAssignment{ID: testID[sc.Assignment](61), ProjectID: testMetadata().ProjectID, AgentID: testID[i.Agent](62), SkillID: testMetadata().ID, Enabled: true, Sequence: 2, CreatedAt: at}
	change := testID[sc.AssignmentChange](63)
	p := &assignmentHTTPControl{receipt: sc.AssignmentMutation{ProjectID: a.ProjectID, AgentID: a.AgentID, AgentVersion: 2, AssignmentSequence: 2, ChangeID: change, Changed: true, Assignment: &a, Runtime: &sc.AssignmentIngressReceipt{ChangeID: change}}, page: skill.OwnerAssignmentPage{AgentVersion: 2, Sequence: 2, Items: []sc.AgentSkillAssignment{a}}}
	p.lookup = skill.AssignmentLookup{State: "committed", Receipt: &p.receipt}
	b := &testBoundary{}
	return &assignmentHandler{p, p, b}, b, p
}
func assignmentHTTPPath(suffix string) string {
	return "/agents/" + testID[i.Agent](62).String() + "/skills" + suffix
}
func assignmentHTTPBody(command string) string {
	extra := ""
	if command != "" {
		extra = `,"command":"` + command + `"`
	}
	return `{"expected_version":"1","request":{"skill_id":"` + testMetadata().ID.String() + `","enabled":true}` + extra + `}`
}

func TestSkillAssignmentHTTPDispatchAndHistoricalLookup(t *testing.T) {
	for _, mode := range []string{"assign", "lookup", "not-observed", "remove"} {
		t.Run(mode, func(t *testing.T) {
			h, _, p := assignmentHTTPFixture()
			suffix, body := "/assign", assignmentHTTPBody("")
			if mode == "lookup" || mode == "not-observed" {
				suffix, body = "/commands/lookup", assignmentHTTPBody("skill.assign")
			}
			if mode == "not-observed" {
				p.lookup = skill.AssignmentLookup{State: "not_observed"}
			}
			if mode == "remove" {
				suffix = "/remove"
				body = `{"expected_version":"1","request":{"assignment_id":"` + p.receipt.Assignment.ID.String() + `"}}`
				p.receipt.Assignment, p.receipt.Runtime = nil, nil
			}
			w := newTestWriter()
			if serveTest(h, installationRequest(assignmentHTTPPath(suffix), body), w) || w.Code != 200 {
				t.Fatal("assignment route failed", w.Code, w.Body.String())
			}
			if p.meta.ExpectedVersion == nil || *p.meta.ExpectedVersion != 1 || p.meta.IdempotencyKey != "skill-install-original" || p.meta.RequestID.Validate() != nil || p.input.AgentID != testID[i.Agent](62) {
				t.Fatal("original command changed")
			}
			if strings.Contains(suffix, "lookup") {
				if p.lookups != 1 || p.writes != 0 {
					t.Fatal("lookup resubmitted mutation")
				}
			} else if p.writes != 1 || p.lookups != 0 {
				t.Fatal("wrong writer")
			}
			out := wireObject(t, w.Body.Bytes())
			if mode == "not-observed" {
				if out["state"] != "not_observed" || out["receipt"] != nil {
					t.Fatal("absence fabricated outcome")
				}
			} else if mode == "lookup" {
				if out["state"] != "committed" || out["receipt"] == nil {
					t.Fatal("historical result lost")
				}
			} else if len(out) != 8 {
				t.Fatal("mutation shape changed")
			}
			if p.ctx.Err() == nil {
				t.Fatal("request not retired")
			}
			w.cleared(t)
		})
	}
}

func TestSkillAssignmentHTTPRejectsAmbiguousIntentBeforeDomain(t *testing.T) {
	valid := assignmentHTTPBody("")
	for _, body := range []string{
		strings.Replace(valid, `"enabled":true`, `"enabled":null`, 1),
		strings.Replace(valid, `"enabled":true`, `"enabled":true,"assignment_id":null`, 1),
		strings.Replace(valid, `"enabled":true`, `"enabled":true,"enabled":false`, 1),
		strings.Replace(valid, `"expected_version":"1"`, `"expected_version":1`, 1),
		strings.TrimSuffix(valid, "}") + `,"command":null}`,
		strings.TrimSuffix(valid, "}") + `,"actor":"private"}`,
		strings.Replace(valid, `"skill_id"`, `"Skill_id"`, 1),
		strings.Repeat(" ", assignmentBodyLimit) + valid,
	} {
		h, _, p := assignmentHTTPFixture()
		w := newTestWriter()
		if serveTest(h, installationRequest(assignmentHTTPPath("/assign"), body), w) || w.Code < 400 || p.writes+p.lookups+p.reads != 0 {
			t.Fatal("ambiguous intent reached domain", w.Code)
		}
	}
	for _, tc := range []struct{ suffix, body string }{{"/commands/lookup", valid}, {"/remove", valid}, {"/assign", assignmentHTTPBody("skill.assign")}} {
		h, _, p := assignmentHTTPFixture()
		w := newTestWriter()
		if serveTest(h, installationRequest(assignmentHTTPPath(tc.suffix), tc.body), w) || w.Code < 400 || p.writes+p.lookups != 0 {
			t.Fatal("route disagreed with command union")
		}
	}
}

func TestSkillAssignmentHTTPPagesHeadAndProjection(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		t.Run(method, func(t *testing.T) {
			h, _, p := assignmentHTTPFixture()
			w := newTestWriter()
			if serveTest(h, httptest.NewRequest(method, testPath(assignmentHTTPPath("")), nil), w) || w.Code != 200 || p.reads != 1 || p.query.Limit != 25 {
				t.Fatal("directory transport mismatch")
			}
			if method == "HEAD" {
				if w.Body.Len() != 0 || w.Header().Get("Content-Length") == "0" {
					t.Fatal("HEAD omitted real representation validation")
				}
				return
			}
			out := wireObject(t, w.Body.Bytes())
			if len(out) != 5 || out["agent_version"] != "2" || out["next_cursor"] != nil {
				t.Fatal("last page invented continuation")
			}
		})
	}
	h, b, p := assignmentHTTPFixture()
	b.auth = func(*http.Request) error { return f.NewFault(f.SessionRevoked, f.NotStarted) }
	w := newTestWriter()
	serveTest(h, installationRequest(assignmentHTTPPath("/assign"), assignmentHTTPBody("")), w)
	if w.Code != 401 || p.writes != 0 {
		t.Fatal("revoked Session reached writer")
	}
	h, _, p = assignmentHTTPFixture()
	p.receipt.Runtime = nil
	w = newTestWriter()
	serveTest(h, installationRequest(assignmentHTTPPath("/assign"), assignmentHTTPBody("")), w)
	var problem struct {
		Code        string `json:"code"`
		CommitState string `json:"commit_state"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &problem)
	if w.Code < 500 || problem.CommitState != "unknown" {
		t.Fatal("missing real ingress presented as mutation success", w.Body.String())
	}
}
