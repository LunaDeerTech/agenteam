package artifacthttp

import (
	"net/http/httptest"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestArtifactHTTPQueryControls(t *testing.T) {
	for _, query := range []string{"?", "?limit=0", "?limit=01", "?limit=+1", "?limit=1&%6cimit=2", "?limit=1&", "?limit", "?limit=", "?limit=1;offset=0", "?unknown=1", "?%ff=1", "?limit=%ff", "?limit=%00", "?limit=%zz", "?limit=9223372036854775808", "?limit=1.0", "?limit=1%0a"} {
		r := httptest.NewRequest("GET", testPath(false), nil)
		r.URL.RawQuery = strings.TrimPrefix(query, "?")
		r.URL.ForceQuery = true
		if _, _, err := listQuery(r); err == nil {
			t.Errorf("list accepted %q", query)
		}
		if _, err := readQuery(r); err == nil {
			t.Errorf("read accepted %q", query)
		}
	}
	for _, query := range []string{"limit=201", "kind=unknown", "name_query=" + strings.Repeat("a", 257), "name_query=%00", "media_type=Text%2FPlain", "execution_id=" + strings.ToUpper(testID[id.Execution](0xab).String()), "cursor=" + strings.Repeat("a", 8193), "offset=0"} {
		r := httptest.NewRequest("GET", testPath(false)+"?"+query, nil)
		if _, _, err := listQuery(r); err == nil {
			t.Errorf("list accepted %q", query)
		}
	}
	for _, query := range []string{"limit=65537", "offset=-1", "offset=1073741825", "name_query=x"} {
		r := httptest.NewRequest("GET", testPath(true)+"?"+query, nil)
		if _, err := readQuery(r); err == nil {
			t.Errorf("read accepted %q", query)
		}
	}
	r := httptest.NewRequest("GET", testPath(false)+"?name_query=%252F%25_%5C&limit=200&kind=generated&media_type=text%2Fplain&execution_id="+testID[id.Execution](10).String(), nil)
	filter, page, err := listQuery(r)
	if err != nil || page.Limit != 200 || filter.NameQuery != "%2F%_\\" || filter.Kind != ac.Generated || filter.MediaType != "text/plain" {
		t.Fatal("single decode literal filter changed")
	}
	preview, err := readQuery(httptest.NewRequest("GET", testPath(true)+"?offset=1073741824&limit=65536", nil))
	if err != nil || preview.Offset != 1073741824 || preview.Limit != 65536 {
		t.Fatal("closed preview bounds")
	}
	r.URL.RawQuery = strings.Repeat("a", maxQueryBytes+1)
	if _, _, err := listQuery(r); err == nil {
		t.Fatal("unbounded original query")
	}
}
