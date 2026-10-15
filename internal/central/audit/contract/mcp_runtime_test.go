package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

func TestMCPRuntimeAuditOnlyRefreshInOriginalProject(t *testing.T) {
	project, _ := f.NewID[id.Project]()
	scope, _ := id.InProject(project)
	request, _ := f.NewID[struct{}]()
	reg, _ := id.RegisterService(id.MCPRuntime)
	actor, _ := reg.Actor(request.String(), scope)
	metadata, err := MCPMetadata(MCPDiscoveryRefresh, MCPMetadataFields{ConfigID: request.String(), ConnectionID: request.String(), Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	resource, _ := NewResource(MCPConnectionResource, request.String())
	entry := EntryFields{Actor: actor, Scope: scope, Action: MCPDiscoveryRefresh, Resource: resource, Outcome: Success, Metadata: metadata}
	if _, err = NewEntry(entry); err != nil {
		t.Fatal(err)
	}
	for _, action := range []Action{MCPConnectionConnect, MCPConnectionEnable, MCPConnectionDisable, MCPConnectionDisconnect} {
		v := entry
		v.Action = action
		v.Metadata, _ = MCPMetadata(action, MCPMetadataFields{ConfigID: request.String(), ConnectionID: request.String(), Version: 1})
		if _, err := NewEntry(v); err == nil {
			t.Fatal("runtime impersonated Owner command", action)
		}
	}
	other, _ := f.NewID[id.Project]()
	entry.Scope, _ = id.InProject(other)
	if _, err := NewEntry(entry); err == nil {
		t.Fatal("foreign Project audit admitted")
	}
	entry.Scope = scope
	otherReg, _ := id.RegisterService(id.SecretService)
	entry.Actor, _ = otherReg.Actor(request.String(), scope)
	if _, err := NewEntry(entry); err == nil {
		t.Fatal("foreign service admitted")
	}
}
