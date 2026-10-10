package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestInitialSkillBindingsKeepFixedCatalogAndClone(t *testing.T) {
	revision := sampleRevision(t)
	agent, _ := f.ParseID[id.Agent]("01900000-0000-7000-8000-000000000010")
	execution, _ := f.ParseID[id.Execution]("01900000-0000-7000-8000-000000000011")
	assignment, _ := f.ParseID[Assignment]("01900000-0000-7000-8000-000000000012")
	request := SkillCaptureRequest{ProjectID: revision.ProjectID, AgentID: agent, ExecutionID: execution}
	b := SkillBinding{SkillID: revision.SkillID, RevisionID: revision.ID, Revision: revision.Revision, AssignmentID: assignment, AssignmentSequence: 1, Name: revision.Name, Description: revision.Description, PackageSHA256: revision.PackageSHA256, EntryPath: EntryPath}
	v := InitialSkillBindings{Request: request, AssignmentSequence: 1, Bindings: []SkillBinding{b}}
	if v.Validate() != nil {
		t.Fatal("valid fixed binding rejected")
	}
	clone := v.Clone()
	clone.Bindings[0].Name = "local mutation"
	if v.Bindings[0].Name != revision.Name {
		t.Fatal("clone aliases catalog")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var round InitialSkillBindings
	if json.Unmarshal(raw, &round) != nil || round.Validate() != nil || round.Bindings[0] != b || round.Request != request {
		t.Fatal("fixed catalog lost on explicit serialization")
	}
	empty := v.Clone()
	empty.Bindings = []SkillBinding{}
	if empty.Validate() != nil || empty.Clone().Bindings == nil {
		t.Fatal("real initialized empty set lost")
	}
	for _, edit := range []func(*InitialSkillBindings){
		func(v *InitialSkillBindings) { v.Request.ExecutionID = id.ExecutionID{} },
		func(v *InitialSkillBindings) { v.AssignmentSequence = 0 },
		func(v *InitialSkillBindings) { v.Bindings = nil },
		func(v *InitialSkillBindings) { v.Bindings[0].Revision = 0 },
		func(v *InitialSkillBindings) { v.Bindings[0].AssignmentSequence = 2 },
		func(v *InitialSkillBindings) { v.Bindings[0].PackageSHA256 = "invalid" },
		func(v *InitialSkillBindings) { v.Bindings[0].EntryPath = "../SKILL.md" },
		func(v *InitialSkillBindings) { v.Bindings = append(v.Bindings, v.Bindings[0]) },
	} {
		bad := v.Clone()
		edit(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid or incomplete fixed binding accepted")
		}
	}
	if strings.Contains(fmt.Sprintf("%+v", v), revision.Name) || strings.Contains(fmt.Sprintf("%+v", request), execution.String()) {
		t.Fatal("implicit logging exposed capture facts")
	}
}
