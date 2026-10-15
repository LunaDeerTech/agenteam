package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"log/slog"
	"strings"
	"testing"
)

func oauthFields(t *testing.T) MCPOAuthWriteFields {
	t.Helper()
	project, _ := f.ParseID[id.Project](accountID)
	scope, _ := id.InProject(project)
	reg, _ := id.RegisterService(id.SecretService)
	actor, _ := reg.Actor(anotherAccountID, scope)
	cmd, _ := f.NewCommandIdentity("secret.mcp", []string{accountID, anotherAccountID, anotherAccountID}, "create", "private-oauth-command")
	material, e := NewSecretMaterial([]byte("private-oauth-token-pkce-client-secret"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(material.Destroy)
	return MCPOAuthWriteFields{Actor: actor, Identity: cmd, ProjectID: project, ConnectionID: anotherAccountID, ConfigRevision: 1, Kind: Create, OwnerKind: MCPOAuthToken, OwnerID: anotherAccountID, Value: material}
}
func TestMCPOAuthWriteRequestClosedIdentityAndBinding(t *testing.T) {
	v := oauthFields(t)
	r, e := NewMCPOAuthWriteRequest(v)
	if e != nil {
		t.Fatal(e)
	}
	binding, _ := MCPOAuthWriteBinding(r)
	without := r.WithoutMaterial()
	copyBinding, e := MCPOAuthWriteBinding(without)
	if e != nil || binding != copyBinding {
		t.Fatal("materialless request lost original binding")
	}
	if without.Fields().Value.Use(func([]byte) error { return nil }) == nil {
		t.Fatal("material retained")
	}
	for name, change := range map[string]func(*MCPOAuthWriteFields){
		"wrong-service": func(v *MCPOAuthWriteFields) {
			scope, _ := id.InProject(v.ProjectID)
			reg, _ := id.RegisterService(id.ModelRuntime)
			v.Actor, _ = reg.Actor(anotherAccountID, scope)
		},
		"wrong-project": func(v *MCPOAuthWriteFields) { v.ProjectID, _ = f.ParseID[id.Project](anotherAccountID) },
		"unknown-owner": func(v *MCPOAuthWriteFields) { v.OwnerKind = "account" },
		"token-owner":   func(v *MCPOAuthWriteFields) { v.OwnerID = accountID },
		"revision":      func(v *MCPOAuthWriteFields) { v.ConfigRevision = 0 },
		"mutation":      func(v *MCPOAuthWriteFields) { v.Kind = "refresh" },
		"command-owner": func(v *MCPOAuthWriteFields) {
			v.Identity, _ = f.NewCommandIdentity("secret.mcp", []string{accountID}, "create", "private-oauth-command")
		},
		"namespace": func(v *MCPOAuthWriteFields) {
			v.Identity, _ = f.NewCommandIdentity("secret.account", []string{accountID, anotherAccountID, anotherAccountID}, "create", "private-oauth-command")
		},
		"create-version": func(v *MCPOAuthWriteFields) { v.ExpectedVersion = 1 },
		"create-ref": func(v *MCPOAuthWriteFields) {
			scope, _ := id.InProject(v.ProjectID)
			cid, _ := f.ParseID[Credential](accountID)
			v.Ref, _ = NewCredentialRef(cid, scope)
		},
		"missing-value": func(v *MCPOAuthWriteFields) { v.Value = SecretMaterial{} },
	} {
		t.Run(name, func(t *testing.T) {
			copy := v
			change(&copy)
			if _, e := NewMCPOAuthWriteRequest(copy); e == nil {
				t.Fatal("invalid intent accepted")
			}
		})
	}
	for name, change := range map[string]func(*MCPOAuthWriteFields){
		"revision": func(v *MCPOAuthWriteFields) { v.ConfigRevision++ },
		"cause": func(v *MCPOAuthWriteFields) {
			scope, _ := id.InProject(v.ProjectID)
			reg, _ := id.RegisterService(id.SecretService)
			v.Actor, _ = reg.Actor(accountID, scope)
		},
		"kind": func(v *MCPOAuthWriteFields) { v.OwnerKind = MCPOAuthFlow },
		"value": func(v *MCPOAuthWriteFields) {
			v.Value, _ = NewSecretMaterial([]byte("different-private-token"))
			t.Cleanup(v.Value.Destroy)
		},
	} {
		t.Run("binding/"+name, func(t *testing.T) {
			copy := v
			change(&copy)
			r, e := NewMCPOAuthWriteRequest(copy)
			if e != nil {
				t.Fatal(e)
			}
			b, _ := MCPOAuthWriteBinding(r)
			if b == binding {
				t.Fatal("semantic identity change ignored")
			}
		})
	}
	var out bytes.Buffer
	for _, value := range []any{r, &r, v, &v, without, struct{ Private MCPOAuthWriteRequest }{r}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			fmt.Fprintf(&out, format, value)
		}
		raw, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		out.Write(raw)
		slog.New(slog.NewTextHandler(&out, nil)).Info("test", "value", value)
	}
	for _, secret := range []string{"private-oauth-token-pkce-client-secret", "private-oauth-command", string(binding)} {
		if strings.Contains(out.String(), secret) {
			t.Fatal("sensitive request projection")
		}
	}
}
func TestMCPOAuthUpdateDeleteRequireProjectCAS(t *testing.T) {
	v := oauthFields(t)
	scope, _ := id.InProject(v.ProjectID)
	cid, _ := f.ParseID[Credential](accountID)
	v.Ref, _ = NewCredentialRef(cid, scope)
	v.ExpectedVersion = 3
	for _, kind := range []MutationKind{Update, Delete} {
		v.Kind = kind
		v.Identity, _ = f.NewCommandIdentity("secret.mcp", []string{accountID, anotherAccountID, anotherAccountID}, string(kind), "request")
		if _, e := NewMCPOAuthWriteRequest(v); e != nil {
			t.Fatal(e)
		}
		wrong := v
		wrong.ExpectedVersion = 0
		if _, e := NewMCPOAuthWriteRequest(wrong); e == nil {
			t.Fatal("missing CAS accepted")
		}
		wrong = v
		wrong.Ref, _ = NewCredentialRef(cid, id.SystemScope())
		if _, e := NewMCPOAuthWriteRequest(wrong); e == nil {
			t.Fatal("system ref accepted")
		}
	}
}
