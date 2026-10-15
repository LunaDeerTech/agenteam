package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func fixedID[T any](t *testing.T, value string) f.ID[T] {
	t.Helper()
	v, err := f.ParseID[T](value)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func validRequest(t *testing.T) Request {
	return Request{Query: "私有查询", Scope: Scope{Kind: Knowledge, ProjectID: fixedID[id.Project](t, "018f0000-0000-7000-8000-000000000001")}, Serving: ServingIdentity{GenerationID: "018f0000-0000-7000-8000-000000000002", SnapshotID: fixedID[mc.Snapshot](t, "018f0000-0000-7000-8000-000000000003"), Dimension: 3}, Limits: Limits{LexicalK: 20, DenseK: 20, FusedK: 20, ResultK: 5, RRFConstant: 60, MaxResultBytes: 4096}}
}

func TestScopeAndFilterSeparation(t *testing.T) {
	r := validRequest(t)
	if r.Validate() != nil {
		t.Fatal("valid Knowledge request rejected")
	}
	a := fixedID[id.Agent](t, "018f0000-0000-7000-8000-000000000004")
	r.Scope.AgentID = &a
	if r.Validate() == nil {
		t.Fatal("Knowledge must not accept a Memory namespace")
	}
	r.Scope.Kind = Memory
	r.Filters = Filters{MemoryTypes: []string{"fact"}, Tags: []string{"build"}}
	if r.Validate() != nil {
		t.Fatal("valid Memory request rejected")
	}
	cloned := r.Clone()
	cloned.Filters.Tags[0] = "changed"
	*cloned.Scope.AgentID = fixedID[id.Agent](t, "018f0000-0000-7000-8000-000000000005")
	if r.Filters.Tags[0] != "build" || *r.Scope.AgentID != a {
		t.Fatal("cloned request aliases scope or filters")
	}
	r.Filters.DocumentID = "018f0000-0000-7000-8000-000000000006"
	if r.Validate() == nil {
		t.Fatal("Memory must reject a Knowledge filter")
	}
}

func TestInternalProjectionsDoNotLogText(t *testing.T) {
	r := validRequest(t)
	c := Candidate{RawText: "private-source-canary"}
	for _, v := range []any{r, c, []Candidate{c}, Result{Hits: []RankedHit{{Candidate: c}}}} {
		raw, err := json.Marshal(v)
		if err != nil || strings.Contains(string(raw), "canary") || strings.Contains(string(raw), r.Query) || strings.Contains(fmt.Sprintf("%+v", v), "canary") {
			t.Fatal("internal projection exposed query or source text")
		}
	}
}
