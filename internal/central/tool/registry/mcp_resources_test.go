package registry

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func resourceTestID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	id, err := f.NewID[T]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestMCPResourceCaptureIdentityAndClosedSet(t *testing.T) {
	r := tc.ExecutionToolCaptureRequest{ProjectID: resourceTestID[id.Project](t), AgentID: resourceTestID[id.Agent](t), ExecutionID: resourceTestID[id.Execution](t)}
	a, err := resourceCaptureDigest(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := resourceCaptureDigest(r, []tc.MCPResourceConnection{})
	if err != nil || a != b {
		t.Fatal("empty capture is not canonical")
	}
	connection := tc.MCPResourceConnection{ProjectID: r.ProjectID, ConfigID: resourceTestID[struct{}](t).String(), ConnectionID: resourceTestID[struct{}](t).String(), RuntimeBindingID: resourceTestID[struct{}](t).String(), ConfigRevision: 1}
	if _, err = resourceCaptureDigest(r, []tc.MCPResourceConnection{connection, connection}); err == nil {
		t.Fatal("duplicate captured connection accepted")
	}
	connection.ProjectID = resourceTestID[id.Project](t)
	if _, err = resourceCaptureDigest(r, []tc.MCPResourceConnection{connection}); err == nil {
		t.Fatal("foreign project capture accepted")
	}
	copy := r
	copy.ExecutionID = resourceTestID[id.Execution](t)
	c, err := resourceCaptureDigest(copy, nil)
	if err != nil || c == a {
		t.Fatal("capture identity not bound to original execution")
	}
}
func TestMCPResourceCaptureRejectsPublicRequestWithoutPrivateLoan(t *testing.T) {
	c := &ExecutionCapture{}
	r := tc.ExecutionToolCaptureRequest{ProjectID: resourceTestID[id.Project](t), AgentID: resourceTestID[id.Agent](t), ExecutionID: resourceTestID[id.Execution](t)}
	if err := c.RequireMCPResourceDiscoveryInTx(context.Background(), f.Tx{}, r); err == nil {
		t.Fatal("public discovery descriptor authorized")
	}
	if err := c.RequireMCPResourceCaptureInTx(context.Background(), f.Tx{}, r, nil); err == nil {
		t.Fatal("public capture descriptor authorized")
	}
}
