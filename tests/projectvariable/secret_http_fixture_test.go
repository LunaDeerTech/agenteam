//go:build integration

package projectvariable_test

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	variablehttp "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/http"
)

// Real Account/Project/D04/Owner with the existing fixture's explicitly
// controlled Skills initialization. Recorder deadline capabilities do not
// establish native transport or default production root acceptance.
func newSecretHTTPFixture(t *testing.T) *secretOwnerFixture {
	t.Helper()
	v := newSecretOwnerFixture(t)
	h, err := variablehttp.NewSecretHTTPHandler(v.owner, v.boundary)
	if err != nil {
		t.Fatal(err)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), h)
	return v
}

func secretHTTPCreateBody(t *testing.T, target vc.VariableID, value string) string {
	t.Helper()
	// Request material is synthetic, task-owned fixture input only. Production
	// decoding owns and clears bytes instead of constructing this generic map.
	return string(jsonBytes(t, map[string]any{"request": map[string]any{
		"variable_id": target, "name": "HTTP_TOKEN", "description": "safe metadata", "value": value,
	}}))
}

func secretHTTPMutation(t *testing.T, r variableHTTPResponse) vc.SecretVariableMutation {
	t.Helper()
	requireHTTP(t, r)
	var result vc.SecretVariableMutation
	if json.Unmarshal(r.body, &result) != nil || result.Validate() != nil {
		t.Fatal("invalid safe Secret HTTP mutation")
	}
	return result
}

func secretHTTPLookup(t *testing.T, r variableHTTPResponse) vc.SecretVariableCommandLookup {
	t.Helper()
	requireHTTP(t, r)
	var result vc.SecretVariableCommandLookup
	if json.Unmarshal(r.body, &result) != nil || result.Validate() != nil {
		t.Fatal("invalid safe Secret HTTP lookup")
	}
	return result
}
