package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestMCPOAuthDeleteRecoveryKeepsOriginalFlowIdentity(t *testing.T) {
	v := oauthFields(t)
	scope, _ := id.InProject(v.ProjectID)
	credential, _ := f.ParseID[Credential](accountID)
	v.Ref, _ = NewCredentialRef(credential, scope)
	v.Kind, v.OwnerKind, v.ExpectedVersion = Delete, MCPOAuthFlow, 7
	v.Identity, _ = f.NewCommandIdentity("secret.mcp", []string{v.ProjectID.String(), v.ConnectionID, v.OwnerID}, "delete", "original-retire-key")
	r, err := NewMCPOAuthWriteRequest(v)
	if err != nil || ValidateMCPOAuthDeleteRecoveryRequest(r) != nil {
		t.Fatal("original flow Delete rejected")
	}
	original, _ := MCPOAuthWriteBinding(r)
	without, _ := MCPOAuthWriteBinding(r.WithoutMaterial())
	if original != without || r.Fields().Value.Use(func([]byte) error { return nil }) == nil || r.Fields().Identity.Canonical() != v.Identity.Canonical() {
		t.Fatal("recovery lost original identity or retained material")
	}
	for _, kind := range []MutationKind{Create, Update, Delete} {
		copy := v
		copy.Kind, copy.OwnerKind = kind, MCPOAuthToken
		copy.Identity, _ = f.NewCommandIdentity("secret.mcp", []string{v.ProjectID.String(), v.ConnectionID, v.OwnerID}, string(kind), "original-retire-key")
		if kind == Create {
			copy.Ref, copy.ExpectedVersion = CredentialRef{}, 0
		}
		other, err := NewMCPOAuthWriteRequest(copy)
		if err != nil {
			t.Fatal(err)
		}
		if ValidateMCPOAuthDeleteRecoveryRequest(other) == nil {
			t.Fatal("token mutation entered flow Delete recovery")
		}
	}
	if ValidateMCPOAuthDeleteRecoveryRequest(MCPOAuthWriteRequest{}) == nil {
		t.Fatal("empty request accepted")
	}
}
