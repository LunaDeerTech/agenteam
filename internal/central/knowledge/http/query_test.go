package knowledgehttp

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestKnowledgeHTTPStrictQueriesAndRoutes(t *testing.T) {
	prefix := projectPrefix + testID[id.Project](4).String() + "/knowledge/documents"
	for _, tc := range []struct {
		path, raw string
		kind      resource
	}{
		{"", "", documents}, {"/children", "parent_document_id=null", children}, {"/children", "parent_document_id=" + testDocument().ID.String() + "&limit=200", children}, {"/search-titles", "title_query=" + url.QueryEscape("%_<中文") + "&limit=1", searchTitles}, {"/" + testDocument().ID.String(), "", document}, {"/" + testDocument().ID.String() + "/ancestors", "", ancestors},
	} {
		r := httptest.NewRequest("GET", prefix+tc.path, nil)
		r.URL.RawQuery = tc.raw
		if resourceRoute(r.URL.Path).kind != tc.kind {
			t.Fatal("route", tc.path)
		}
		q, err := parseQuery(r, tc.kind)
		if err != nil {
			t.Fatal(tc.path, err)
		}
		if tc.kind == searchTitles && q.filter.TitleQuery != "%_<中文" {
			t.Fatal("literal query was rewritten")
		}
	}
	for _, raw := range []string{"limit=0", "limit=201", "limit=01", "limit=+1", "limit=-1", "limit=1.0", "limit=1&%6cimit=2", "limit", "limit=", "unknown=canary", "title_query=%FF", "title_query=%00", "title_query=%0A", "title_query=x;limit=2", "&limit=1", "limit=1&", "cursor=" + strings.Repeat("x", 8193), "source_kind=text&media_type=application%2Fpdf", "indexing_status=unknown", "parent_document_id=null"} {
		r := httptest.NewRequest("GET", prefix, nil)
		r.URL.RawQuery = raw
		if _, err := parseQuery(r, documents); err == nil {
			t.Fatal("bad query accepted", raw[:min(len(raw), 60)])
		}
	}
	for _, tc := range []struct {
		kind resource
		raw  string
	}{{children, ""}, {children, "parent_document_id="}, {children, "parent_document_id=ROOT"}, {searchTitles, "source_kind=text"}, {document, "limit=1"}, {ancestors, "title_query=x"}} {
		r := httptest.NewRequest("GET", prefix, nil)
		r.URL.RawQuery = tc.raw
		if _, err := parseQuery(r, tc.kind); err == nil {
			t.Fatal("wrong route query accepted", tc)
		}
	}
	r := httptest.NewRequest("GET", prefix+"?", nil)
	if _, err := parseQuery(r, documents); err == nil {
		t.Fatal("bare query accepted")
	}
	for _, path := range []string{prefix + "/", prefix + "//ancestors", prefix + "/children/ancestors", prefix + "/search-titles/ancestors", prefix + "/a/b/c", "/api/v1/projects/x/variables"} {
		if HandlesPath(path) {
			t.Fatal("unowned route claimed", path)
		}
	}
}
