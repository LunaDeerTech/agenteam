package model

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type httpNilWrites chan int

func (httpNilWrites) ExecuteWrite(context.Context, sc.WriteRequest) (sc.MutationResult, error) {
	panic("nil port used")
}
func (httpNilWrites) Metadata(context.Context, id.Actor, sc.CredentialRef) (sc.Metadata, error) {
	panic("nil port used")
}
func (httpNilWrites) LookupWriteCommand(context.Context, sc.WriteCommandLookupRequest) (sc.WriteCommandObservation, error) {
	panic("nil port used")
}

func TestSystemHTTPConstructionAndRoutesMatchOpenAPI(t *testing.T) {
	store := &noIOStore{}
	core, err := New(store, pureAuthority(t, store), testDependencies(t))
	if err != nil {
		t.Fatal(err)
	}
	var typedNil *httpWritesSpy
	for _, writes := range []sc.HumanWriteCommands{nil, typedNil, httpNilWrites(nil)} {
		handler, err := NewSystemHTTPHandler(core, &account.Service{}, writes, SystemHTTPOptions{PublicOrigin: "https://example.test"})
		requireCode(t, err, f.DependencyUnbound)
		if handler != nil {
			t.Fatal("nil dependency accepted")
		}
	}
	for _, model := range []*Service{nil, {}, {data: func() *serviceState { return nil }}} {
		if handler, err := NewSystemHTTPHandler(model, nil, &httpWritesSpy{}, SystemHTTPOptions{}); err == nil || handler != nil {
			t.Fatal("zero Model accepted")
		}
	}
	data, err := os.ReadFile("../../../api/openapi/model-system.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(data, &spec) != nil {
		t.Fatal("invalid OpenAPI JSON")
	}
	count := 0
	for _, route := range (&systemHTTP{}).routes() {
		path := "/api/v1" + route.path
		method := strings.ToLower(route.method)
		if len(spec.Paths[path][method]) == 0 {
			t.Fatalf("route absent %s %s", method, path)
		}
		delete(spec.Paths[path], method)
		count++
		if route.method == "GET" {
			if len(spec.Paths[path]["head"]) == 0 {
				t.Fatal("missing HEAD")
			}
			delete(spec.Paths[path], "head")
			count++
		}
		if strings.HasSuffix(path, "/lookup") && route.intent != id.Read {
			t.Fatal("lookup intent")
		}
	}
	if count != 29 {
		t.Fatalf("route count %d", count)
	}
	for path, methods := range spec.Paths {
		if len(methods) != 0 {
			t.Fatalf("undocumented handler mismatch %s", path)
		}
	}
}
