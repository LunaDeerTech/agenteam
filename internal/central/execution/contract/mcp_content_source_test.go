package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestMCPContentSourceClosedOriginalIdentity(t *testing.T) {
	const raw = "01960aa5-0286-7000-8000-000000000001"
	project, _ := f.ParseID[i.Project](raw)
	agent, _ := f.ParseID[i.Agent](raw)
	execution, _ := f.ParseID[i.Execution](raw)
	operation, _ := f.ParseID[tc.Operation](raw)
	attempt, _ := f.ParseID[tc.Attempt](raw)
	actor, _ := i.NewAgentRun(project, agent, execution)
	ref, _ := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: project, FileID: raw})
	r := MCPContentReadRequest{Actor: actor, ProjectID: project, ExecutionID: execution, Reference: ref, MaterializationID: raw, ConfigID: raw, ConnectionID: raw, RuntimeBindingID: raw, ConfigRevision: 1, OperationID: operation, AttemptID: attempt, RequestID: raw, URI: "repo://owned/private-content"}
	if r.Validate() != nil {
		t.Fatal("valid canonical source description rejected")
	}
	for name, change := range map[string]func(*MCPContentReadRequest){
		"foreign project":   func(r *MCPContentReadRequest) { r.ProjectID, _ = f.NewID[i.Project]() },
		"foreign execution": func(r *MCPContentReadRequest) { r.ExecutionID, _ = f.NewID[i.Execution]() },
		"unbound actor":     func(r *MCPContentReadRequest) { r.Actor = i.Actor{} },
		"other file kind": func(r *MCPContentReadRequest) {
			r.Reference, _ = oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ArtifactFile, ProjectID: project, FileID: raw, ArtifactID: raw})
		},
		"missing attempt":  func(r *MCPContentReadRequest) { r.AttemptID = tc.AttemptID{} },
		"missing request":  func(r *MCPContentReadRequest) { r.RequestID = "" },
		"missing runtime":  func(r *MCPContentReadRequest) { r.RuntimeBindingID = "" },
		"invalid revision": func(r *MCPContentReadRequest) { r.ConfigRevision = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := r
			change(&changed)
			if changed.Validate() == nil {
				t.Fatal("substituted source accepted")
			}
		})
	}
	wire, err := json.Marshal(r)
	if err != nil || strings.Contains(string(wire), raw) || strings.Contains(fmt.Sprintf("%+v", r), "private-content") || r.LogValue().String() != "mcp_content_source" {
		t.Fatal("source reflection disclosed private identity")
	}
	if json.Unmarshal(wire, &MCPContentReadRequest{}) == nil {
		t.Fatal("public JSON reconstructed a source request")
	}
}
