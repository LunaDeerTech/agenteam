package contract

import (
	"encoding/json"
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"strings"
	"testing"
)

func TestMCPFileClosedIdentityAndOpaqueReflection(t *testing.T) {
	const value = "01900000-0000-7000-8000-000000000001"
	project, _ := foundation.ParseID[identity.Project](value)
	details := BusinessFileDetails{Kind: MCPFile, ProjectID: project, FileID: value}
	ref, err := NewBusinessFileRef(details)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BusinessFileDetails){func(d *BusinessFileDetails) { d.FileID = "https://remote/resource" }, func(d *BusinessFileDetails) { d.Revision = 1 }, func(d *BusinessFileDetails) { d.DocumentID = value }, func(d *BusinessFileDetails) { d.ExecutionID = value }, func(d *BusinessFileDetails) { d.PayloadID = value }, func(d *BusinessFileDetails) { d.ProjectID = identity.ProjectID{} }} {
		bad := details
		mutate(&bad)
		if _, err = NewBusinessFileRef(bad); err == nil {
			t.Fatal("accepted mixed MCP source identity")
		}
	}
	raw, err := json.Marshal(ref)
	if err != nil || string(raw) != `"business_file_ref"` || strings.Contains(fmt.Sprintf("%+v", ref), value) {
		t.Fatal("MCP source leaked through generic reflection")
	}
	var parsed BusinessFileRef
	if json.Unmarshal(raw, &parsed) == nil {
		t.Fatal("serialized ref created a source capability")
	}
}
