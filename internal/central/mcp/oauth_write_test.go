package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestMCPOAuthWriteRequiresOriginalPrivateOwnerAndTx(t *testing.T) {
	project, _ := f.ParseID[id.Project]("01960aa5-0286-7000-8000-000000000001")
	connection := "01960aa5-0286-7000-8000-000000000002"
	flow := "01960aa5-0286-7000-8000-000000000003"
	requestID := "01960aa5-0286-7000-8000-000000000004"
	scope, _ := id.InProject(project)
	registration, _ := id.RegisterService(id.SecretService)
	actor, _ := registration.Actor(requestID, scope)
	key, _ := f.ParseIdempotencyKey(requestID)
	identity, _ := f.NewCommandIdentity("secret.mcp", []string{project.String(), connection, flow}, string(sc.Create), key)
	material, _ := sc.NewSecretMaterial([]byte("test-only-pkce"))
	defer material.Destroy()
	request, err := sc.NewMCPOAuthWriteRequest(sc.MCPOAuthWriteFields{Actor: actor, Identity: identity, ProjectID: project, ConnectionID: connection, ConfigRevision: 1, Kind: sc.Create, OwnerKind: sc.MCPOAuthFlow, OwnerID: flow, Value: material})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{secretIssuer: sc.NewPlanIssuer()}
	if _, err = s.DiscoverMCPOAuthWrite(context.Background(), request); err == nil {
		t.Fatal("DTO and Secret actor authorized a write")
	}
	binding, _ := sc.MCPOAuthWriteBinding(request)
	w := &oauthWriteUse{service: s, request: request.WithoutMaterial(), requestID: requestID, mapping: binding, owner: &oauthControlOwner{service: s}}
	w.active.Store(true)
	ctx := context.WithValue(context.Background(), oauthWriteKey{}, w)
	plan, err := s.DiscoverMCPOAuthWrite(ctx, request.WithoutMaterial())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ValidateMCPOAuthWriteInTx(ctx, f.Tx{}, request, plan); err == nil {
		t.Fatal("private intent authorized a zero transaction")
	}
	if err = s.ValidateMCPOAuthWriteObservationInTx(ctx, f.Tx{}, request, plan); err == nil {
		t.Fatal("observer accepted zero transaction")
	}
	other := &Service{secretIssuer: sc.NewPlanIssuer()}
	if _, err = other.DiscoverMCPOAuthWrite(ctx, request); err == nil {
		t.Fatal("another Service borrowed original write")
	}
	w.active.Store(false)
	if _, err = s.DiscoverMCPOAuthWrite(ctx, request); err == nil {
		t.Fatal("returned original callback retained its grant")
	}
}
