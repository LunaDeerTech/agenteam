package workhttp

import (
	"net/http/httptest"
	"strings"
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestWorkHTTPStrictListQueries(t *testing.T) {
	for _, bad := range []string{"limit=0", "limit=201", "limit=01", "limit=+1", "limit=1e1", "limit=1&limit=2", "limit=1&%6cimit=2", "limit", "=1", "cursor=", "cursor=%zz", "cursor=%ff", "cursor=%00", "limit=1;cursor=x", "limit=1&", "&limit=1", "owner=x", "text=", "state=BACKLOG", "priority=urgent", "type=wrong", "assignee_agent_id=none", "assignee_agent_id=", "milestone_id=wrong", "text=%20%20", strings.Repeat("x", maxQueryBytes+1)} {
		if _, err := parseQuery(httptest.NewRequest("GET", testPath("/tasks?"+bad), nil), tasks); err == nil {
			t.Fatal("invalid query accepted", bad[:min(len(bad), 80)])
		}
	}
	for _, kind := range []resource{milestones, sprints, tasks, blockers, milestone, sprint, task} {
		r := httptest.NewRequest("GET", testPath("/tasks?"), nil)
		if _, e := parseQuery(r, kind); e == nil {
			t.Fatal("bare query", kind)
		}
	}
	q, e := parseQuery(httptest.NewRequest("GET", testPath("/tasks?state=backlog&priority=medium&type=task&assignee_agent_id=null&text=canary&limit=200&cursor=opaque"), nil), tasks)
	if e != nil || q.page.Limit != 200 || q.page.Cursor != "opaque" || !q.filter.AssigneeAgentID.Present || q.filter.AssigneeAgentID.AgentID != nil || q.filter.Text == nil || *q.filter.Text != "canary" {
		t.Fatal("filter semantics", e)
	}
	q, e = parseQuery(httptest.NewRequest("GET", testPath("/tasks?assignee_agent_id="+testID[id.Agent](42).String()), nil), tasks)
	if e != nil || q.filter.AssigneeAgentID.AgentID == nil {
		t.Fatal("assigned filter", e)
	}
	for _, v := range []string{"resolved", "unresolved", "all"} {
		q, e = parseQuery(httptest.NewRequest("GET", testPath("/tasks/x/blockers?status="+v), nil), blockers)
		if e != nil || q.status != c.TaskBlockerStatus(v) {
			t.Fatal("status", e)
		}
	}
}
func TestWorkHTTPReadOrderFilterAndFullState(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		edit        func(*testPorts)
	}{
		{"priority filter", "?priority=high", func(*testPorts) {}},
		{"description predicate", "?text=description", func(*testPorts) {}},
		{"plan predicate", "?text=plan", func(*testPorts) {}},
		{"assigned", "?assignee_agent_id=null", func(p *testPorts) { p.tl.Items[0].AssigneeAgentID = ptr(testID[id.Agent](44)) }},
		{"order", "", func(p *testPorts) {
			other := p.tl.Items[0]
			other.ID = testID[c.Task](2)
			p.tl.Items = append(p.tl.Items, other)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, p := testHandler()
			tc.edit(p)
			w := newTestWriter()
			aborted := serveTest(h, httptest.NewRequest("GET", testPath("/tasks"+tc.query), nil), w)
			want := 503
			if tc.name == "description predicate" || tc.name == "plan predicate" {
				want = 200
			}
			if aborted || w.Code != want {
				t.Fatal("filter/order projection", w.Code)
			}
		})
	}
	h, _, p := testHandler()
	p.t.State = c.TaskStateInProgress
	p.t.AssigneeAgentID = ptr(testID[id.Agent](44))
	w := newTestWriter()
	if serveTest(h, httptest.NewRequest("GET", testPath("/tasks/"+p.t.ID.String()), nil), w) || w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"in_progress"`) {
		t.Fatal("read rewrote state")
	}
}

func TestWorkHTTPQueryKeysAreExact(t *testing.T) {
	for _, key := range []string{"state%7Cpriority", "priority%7Ctype", "milestone_id%7Csprint_id"} {
		if _, err := parseQuery(httptest.NewRequest("GET", testPath("/tasks?"+key+"=ignored"), nil), tasks); err == nil {
			t.Fatal("compound unknown key accepted")
		}
	}
}
