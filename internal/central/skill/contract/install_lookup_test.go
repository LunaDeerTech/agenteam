package contract

import (
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestInstallLookupRequestIsReadOnlyAndExact(t *testing.T) {
	project, _ := f.NewID[id.Project]()
	agent, _ := f.NewID[id.Agent]()
	execution, _ := f.NewID[id.Execution]()
	request, _ := f.NewID[f.Request]()
	skill, _ := f.NewID[pc.Skill]()
	actor, _ := id.NewAgentRun(project, agent, execution)
	command, err := f.NewCommandIdentity("project", []string{project.String()}, "skill.install", "original-key")
	if err != nil {
		t.Fatal(err)
	}
	digest := f.Digest("sha256:" + strings.Repeat("1", 64))
	r := InstallLookupRequest{InstallExecutionRequest{Actor: actor, ProjectID: project, Command: command, RequestID: request, Intent: id.Read, SkillID: skill, NormalizedName: "checklist", PackageSHA256: digest, ManifestSHA256: digest, ByteSize: 512}}
	if r.Validate() != nil || !r.Equal(r) {
		t.Fatal("original read request rejected")
	}
	mutate := r
	mutate.Original.Intent = id.Mutate
	if mutate.Validate() == nil || r.Equal(mutate) {
		t.Fatal("lookup admitted mutation")
	}
	changed := r
	changed.Original.RequestID, _ = f.NewID[f.Request]()
	if r.Equal(changed) {
		t.Fatal("lookup lost the physical request identity")
	}
}
