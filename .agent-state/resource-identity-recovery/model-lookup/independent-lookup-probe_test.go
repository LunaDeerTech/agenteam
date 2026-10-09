//go:build integration

package account_test

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const independentLookupProject = "0191ac00-4751-7234-899a-100000000001"
const independentLookupResource = "0191ac00-4751-7234-899a-100000000002"
const independentLookupCanary = "independent-private-material-sentinel"

type independentLookupCase struct{ name, operation, flag, member, inner string }

func independentLookupCases() []independentLookupCase {
	return []independentLookupCase{
		{"configuration", "lookupProjectModelConfiguration", "found", "receipt", `{"kind":"provider.create","resource_id":"` + independentLookupResource + `","version":"1","affected_references":"0"}`},
		{"credential", "lookupProjectModelCredential", "observed", "result", `{"credential_id":"` + independentLookupResource + `","purpose":"model","version":"1","deleted":false}`},
	}
}

func (c independentLookupCase) wrap(flag, inner string) []byte {
	return []byte(`{"` + c.flag + `":` + flag + `,"` + c.member + `":` + inner + `}`)
}

func independentLookupFixture(c independentLookupCase) (*projectModelsWebFixture, *projectModelsWebRequest, *http.Response) {
	f := &projectModelsWebFixture{
		projectOwnerAuditWebFixture: &projectOwnerAuditWebFixture{projectOwnerWebFixture: &projectOwnerWebFixture{}},
		registry:                    projectModelsWebRegistry{Projects: map[string]string{"main": independentLookupProject}, Targets: map[string]map[string]map[string]bool{}, Cursors: map[string]map[string]map[string]bool{}},
	}
	request := &projectModelsWebRequest{Project: "main", Operation: &projectModelsWebOperation{Operation: c.operation, Family: "lookup"}}
	response := &http.Response{StatusCode: 200, Header: http.Header{
		"Cache-Control": {"no-store"}, "Content-Type": {"application/json; charset=utf-8"},
		"X-Request-Id": {"0191ac00-4751-7234-899a-100000000003"},
	}, Request: &http.Request{Method: "POST", URL: &url.URL{Path: "/api/v1/projects/" + independentLookupProject + "/model-commands/lookup"}}}
	if c.name == "credential" {
		response.Request.URL.Path = "/api/v1/projects/" + independentLookupProject + "/model-credential-commands/lookup"
	}
	return f, request, response
}

func TestIndependentModelLookupControls(t *testing.T) {
	for _, c := range independentLookupCases() {
		t.Run(c.name, func(t *testing.T) {
			f, request, response := independentLookupFixture(c)
			historical := strings.Replace(c.inner, `"version":"1"`, `"version":"9223372036854775807"`, 1)
			if c.name == "configuration" {
				historical = strings.Replace(historical, "provider.create", "model.delete", 1)
			} else {
				historical = strings.Replace(historical, `"deleted":false`, `"deleted":true`, 1)
			}
			// Alternate found/missing calls on the same fixture. History remains an
			// observation, including deleted resources and non-create versions.
			for _, raw := range [][]byte{c.wrap("true", c.inner), c.wrap("false", "null"), c.wrap("true", historical), c.wrap("false", "null")} {
				original := bytes.Clone(raw)
				if err := f.admitResponse(request, response, raw); err != nil {
					t.Fatal("valid lookup union rejected")
				}
				if !bytes.Equal(raw, original) {
					t.Fatal("admission changed retained response bytes")
				}
				if len(f.registry.Targets) != 0 || len(f.registry.Cursors) != 0 || len(f.modelCounts) != 0 || len(f.modelOrigins) != 0 || len(f.modelLastCounts) != 0 || f.modelServer.Started != 0 || f.modelServer.Finished != 0 {
					t.Fatal("historical admission manufactured current targets or execution evidence")
				}
			}
		})
	}
}

func TestIndependentModelLookupClosedUnion(t *testing.T) {
	for _, c := range independentLookupCases() {
		t.Run(c.name, func(t *testing.T) {
			f, request, response := independentLookupFixture(c)
			add := func(inner, field string) string { return strings.TrimSuffix(inner, "}") + "," + field + "}" }
			other := independentLookupCases()[0]
			if c.name == "configuration" {
				other = independentLookupCases()[1]
			}
			bad := []struct {
				name string
				raw  []byte
			}{
				{"cross-union", other.wrap("true", other.inner)},
				{"true-null", c.wrap("true", "null")}, {"false-value", c.wrap("false", c.inner)},
				{"flag-string", c.wrap(`"true"`, c.inner)}, {"flag-null", c.wrap("null", c.inner)},
				{"inner-array", c.wrap("true", "[]")}, {"inner-empty", c.wrap("true", "{}")},
				{"extra-wrapper", []byte(add(string(c.wrap("false", "null")), `"value":"`+independentLookupCanary+`"`))},
				{"extra-inner", c.wrap("true", add(c.inner, `"value":"`+independentLookupCanary+`"`))},
				{"wrapper-key-in-inner", c.wrap("true", add(c.inner, `"`+c.flag+`":true`))},
				{"duplicate-wrapper", []byte(add(string(c.wrap("true", c.inner)), `"`+c.flag+`":true`))},
				{"duplicate-inner", c.wrap("true", add(c.inner, `"version":"1"`))},
				{"zero-version", c.wrap("true", strings.Replace(c.inner, `"version":"1"`, `"version":"0"`, 1))},
				{"numeric-version", c.wrap("true", strings.Replace(c.inner, `"version":"1"`, `"version":1`, 1))},
				{"overflow-version", c.wrap("true", strings.Replace(c.inner, `"version":"1"`, `"version":"9223372036854775808"`, 1))},
				{"private-id", c.wrap("true", strings.Replace(c.inner, independentLookupResource, independentLookupCanary, 1))},
				{"surrogate", c.wrap("true", strings.Replace(c.inner, independentLookupResource, `\ud800`, 1))},
				{"trailing-value", append(c.wrap("false", "null"), []byte(` {}`)...)},
				{"missing-member", []byte(`{"` + c.flag + `":false}`)},
			}
			if c.name == "configuration" {
				bad = append(bad, struct {
					name string
					raw  []byte
				}{"wrong-kind", c.wrap("true", strings.Replace(c.inner, "provider.create", "secret.create", 1))}, struct {
					name string
					raw  []byte
				}{"references", c.wrap("true", strings.Replace(c.inner, `"affected_references":"0"`, `"affected_references":"1"`, 1))})
			} else {
				bad = append(bad, struct {
					name string
					raw  []byte
				}{"wrong-purpose", c.wrap("true", strings.Replace(c.inner, `"purpose":"model"`, `"purpose":"smtp"`, 1))}, struct {
					name string
					raw  []byte
				}{"deleted-string", c.wrap("true", strings.Replace(c.inner, `"deleted":false`, `"deleted":"false"`, 1))})
			}
			for _, test := range bad {
				original := bytes.Clone(test.raw)
				err := f.admitResponse(request, response, test.raw)
				if err == nil {
					t.Errorf("%s: invalid lookup admitted", test.name)
				} else if strings.Contains(err.Error(), independentLookupCanary) {
					t.Errorf("%s: private material exposed by error", test.name)
				}
				if !bytes.Equal(test.raw, original) {
					t.Errorf("%s: admission changed rejected bytes", test.name)
				}
			}
			if len(f.registry.Targets) != 0 {
				t.Fatal("rejection registered target")
			}
		})
	}
}

func TestIndependentModelLookupResponseBinding(t *testing.T) {
	for _, c := range independentLookupCases() {
		t.Run(c.name, func(t *testing.T) {
			for _, alter := range []func(*projectModelsWebRequest, *http.Response){
				func(_ *projectModelsWebRequest, r *http.Response) { r.Header.Del("Cache-Control") },
				func(_ *projectModelsWebRequest, r *http.Response) { r.Header.Set("Cache-Control", "public") },
				func(_ *projectModelsWebRequest, r *http.Response) { r.Header.Del("X-Request-ID") },
				func(_ *projectModelsWebRequest, r *http.Response) {
					r.Header.Set("X-Request-ID", independentLookupCanary)
				},
				func(_ *projectModelsWebRequest, r *http.Response) { r.Header.Set("Content-Type", "text/html") },
				func(_ *projectModelsWebRequest, r *http.Response) { r.StatusCode = http.StatusBadGateway },
				func(r *projectModelsWebRequest, _ *http.Response) { r.Target = "0191ac00-4751-7234-899a-100000000004" },
			} {
				f, request, response := independentLookupFixture(c)
				alter(request, response)
				if err := f.admitResponse(request, response, c.wrap("true", c.inner)); err == nil {
					t.Fatal("mismatched response metadata/target admitted")
				} else if strings.Contains(err.Error(), independentLookupCanary) {
					t.Fatal("response error exposed input")
				}
			}
		})
	}
}
