package registry

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"testing"
)

type deniedMCPRegistry struct {
	tc.MCPRegistryAuthority
	calls int
}

func (a *deniedMCPRegistry) RequireBindingInTx(context.Context, f.Tx, any) error { return nil }
func (a *deniedMCPRegistry) RequireMCPDiscoveryInTx(context.Context, f.Tx, tc.MCPDiscovery) error {
	a.calls++
	return fail(f.Forbidden)
}
func TestMCPDiscoveryCannotPublishCallerDTOWithoutOriginalWitness(t *testing.T) {
	const key = "01900000-0000-7000-8000-000000000001"
	project, _ := f.ParseID[id.Project](key)
	store := &metadataStore{tx: f.NewTx(), held: []f.LockRequest{RegistryLock(f.Exclusive)}}
	authority := &deniedMCPRegistry{}
	r, err := New(store, Options{MCP: authority})
	if err != nil {
		t.Fatal(err)
	}
	d := tc.MCPDiscovery{ProjectID: project, ConfigID: key, ConnectionID: key, RuntimeBindingID: key, ConfigRevision: 1, Tools: []tc.MCPRegistration{}, PresentNames: []string{"invalid_but_present"}}
	if _, err = r.ReconcileMCPInTx(context.Background(), store.tx, d); err == nil || authority.calls != 1 || len(store.writes) != 0 || store.queries != 0 {
		t.Fatal("unissued discovery changed serving catalogue")
	}
	d.PresentNames = append(d.PresentNames, d.PresentNames[0])
	if _, err = r.ReconcileMCPInTx(context.Background(), store.tx, d); err == nil || authority.calls != 1 {
		t.Fatal("duplicate discovery entered original authority")
	}
	capture := &ExecutionCapture{data: func() *executionCaptureState { return &executionCaptureState{registry: r} }}
	b := tc.MCPBinding{ProjectID: project, ConfigID: key, ConnectionID: key, RuntimeBindingID: key, ConfigRevision: 1, RemoteToolName: "call"}
	execution, _ := f.ParseID[id.Execution](key)
	if err = capture.RequireMCPCaptureInTx(context.Background(), store.tx, execution, b); err == nil {
		t.Fatal("public capture DTO minted lease permission")
	}
}
