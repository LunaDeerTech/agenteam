package mcp

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type unopenedRegistry struct{ Registry }
type unopenedSessions struct{ id.SessionAuthority }
type unopenedSystem struct{ id.SystemAuthority }
type unopenedProjects struct{ ProjectAuthority }
type unopenedAudit struct{ AuditAppender }

func TestMCPControlAuthorityWithoutExecutionCapture(t *testing.T) {
	store := &unopenedStore{}
	authority, err := NewAuthority(store)
	if err != nil {
		t.Fatal("static authority construction")
	}
	options := Options{Authority: authority, Registry: &unopenedRegistry{}, Sessions: &unopenedSessions{}, System: &unopenedSystem{}, Projects: &unopenedProjects{}, Audit: &unopenedAudit{}, Protocol: &protocol.Runtime{}}
	service, err := New(store, options)
	if err != nil {
		t.Fatal("control-only construction required an Execution capture provider")
	}
	bound, err := authority.service()
	if err != nil || bound != service || service.options.Capture != nil {
		t.Fatal("control-only construction changed its exact owner or invented capture")
	}
	request := retirementRequest(t)
	capture := tc.ExecutionToolCaptureRequest{ProjectID: request.ProjectID, AgentID: request.AgentID, ExecutionID: request.ExecutionID}
	binding := tc.MCPBinding{ProjectID: capture.ProjectID, ConfigID: "01960aa5-0286-7000-8000-000000000004", ConnectionID: "01960aa5-0286-7000-8000-000000000005", RuntimeBindingID: "01960aa5-0286-7000-8000-000000000006", ConfigRevision: 1, RemoteToolName: "echo"}
	if err := binding.Validate(); err != nil {
		t.Fatal("invalid test binding")
	}
	ctx := context.Background()
	_, err = authority.DiscoverMCPRetention(ctx, capture, []tc.MCPBinding{binding})
	requireUnbound(t, err)
	// Even a same-domain shaped plan cannot turn an absent runtime into a grant.
	plan := &retentionPlan{owner: service, request: capture, bindings: []tc.MCPBinding{binding}}
	requireUnbound(t, authority.RetainMCPRuntimeInTx(ctx, f.Tx{}, capture.ExecutionID, binding, plan))
	_, err = authority.DiscoverMCPResources(ctx, capture, nil)
	requireUnbound(t, err)
	if err = authority.RetainMCPResourcesInTx(ctx, f.Tx{}, capture, nil); err == nil {
		t.Fatal("resource capture accepted no original private plan")
	}
	if _, err = New(store, options); err == nil {
		t.Fatal("control-only authority rebound to a second service")
	}
	foreign, _ := NewAuthority(&unopenedStore{})
	if err = foreign.bind(service); err == nil {
		t.Fatal("control-only authority accepted a foreign Store")
	}
	unregistered, _ := NewAuthority(store)
	if err = unregistered.bind(&Service{store: store}); err == nil {
		t.Fatal("control-only authority bound without the actual Registry")
	}
}

func requireUnbound(t *testing.T, err error) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound {
		t.Fatal("missing runtime capability did not fail closed")
	}
}
