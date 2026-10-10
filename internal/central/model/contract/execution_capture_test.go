package contract

import (
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestExecutionModelCaptureContractCopiesAndSafeFacts(t *testing.T) {
	r := ExecutionModelCaptureRequest{ProjectID: fresh[id.Project](t), AgentID: fresh[id.Agent](t), ExecutionID: fresh[id.Execution](t)}
	must(t, r.Validate())
	badRequest := r
	badRequest.ExecutionID = id.ExecutionID{}
	reject(t, badRequest.Validate())
	effort := "capture-effort-canary"
	approval := fresh[Model](t)
	facts := ExecutionModelCaptureFacts{AgentVersion: f.Version(3), Selection: AgentModelSelection{ModelID: fresh[Model](t), ReasoningEffort: &effort, ApprovalModelID: &approval}}
	copy := facts.Clone()
	*copy.Selection.ReasoningEffort = "changed"
	*copy.Selection.ApprovalModelID = fresh[Model](t)
	if *facts.Selection.ReasoningEffort != effort || *facts.Selection.ApprovalModelID != approval {
		t.Fatal("capture facts alias returned clone")
	}
	for _, safe := range []string{fmt.Sprintf("%+v", r), fmt.Sprintf("%+v", facts), facts.LogValue().String()} {
		if strings.Contains(safe, r.ExecutionID.String()) || strings.Contains(safe, effort) {
			t.Fatal("private capture facts escaped formatting")
		}
	}
}
