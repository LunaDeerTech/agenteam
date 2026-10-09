package projectvariablehttp

import (
	"net/http/httptest"
	"strings"
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func TestVariableHTTPStrictQueryBodyAndWholePage(t *testing.T) {
	for _, query := range []string{"?", "?limit", "?limit=", "?limit=0", "?limit=01", "?limit=101", "?limit=1&%6cimit=2", "?cursor=", "?cursor=%ff", "?cursor=%00", "?cursor=x;limit=1", "?unknown=x", "?cursor=%", "?cursor=" + strings.Repeat("x", 8193)} {
		h, _, p := testHandler()
		w := newTestWriter()
		r := httptest.NewRequest("GET", testPath("/variables")+query, nil)
		if serveTest(h, r, w) || w.Code != 400 || p.calls != 0 {
			t.Fatalf("query rejected before port: %s", query[:min(len(query), 64)])
		}
	}
	for _, bad := range []string{"late-parent", "bad-last", "duplicate", "order", "cursor-short-page"} {
		h, _, p := testHandler()
		first := p.page.Items[0]
		v := first.Fields()
		switch bad {
		case "late-parent":
			v.ProjectID = testID[id.Project](55)
		case "bad-last":
			p.page.Items = append(p.page.Items, c.VariableSummary{})
		case "duplicate":
			p.page.Items = append(p.page.Items, first)
		case "order":
			v.Name = "A"
		case "cursor-short-page":
			p.page.NextCursor = "opaque"
		}
		if bad == "late-parent" || bad == "order" {
			v.ID = testVariable().Fields().ID
			next, e := c.NewVariableSummary(v)
			if e != nil {
				t.Fatal(e)
			}
			p.page.Items = append(p.page.Items, next)
		}
		w := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath("/variables"), nil), w) || w.Code != 503 || strings.Contains(w.Body.String(), "description-canary") {
			t.Fatal("partial page", bad)
		}
	}
	h, _, p := testHandler()
	w := newTestWriter()
	r := httptest.NewRequest("HEAD", testPath("/variables"), nil)
	if serveTest(h, r, w) || w.Code != 200 || w.Body.Len() != 0 || w.Header().Get("Content-Length") == "0" || p.calls != 1 {
		t.Fatal("HEAD did not execute representation")
	}
	w.cleared(t)
	for _, suffix := range []string{"/milestones", "/variables/commands/nope", "/variables/commands/lookup/", "/variables//x", "/models", "/variables/x/y/z"} {
		if HandlesPath(testPath(suffix)) {
			t.Fatal("claimed foreign route", suffix)
		}
	}
	h, _, p = testHandler()
	w = newTestWriter()
	r = httptest.NewRequest("GET", testPath("/variables"), strings.NewReader("x"))
	r.ContentLength = 0
	if serveTest(h, r, w) || w.Code != 400 || p.calls != 0 {
		t.Fatal("actual GET body accepted")
	}
}
