package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestMCPOAuthProcessRequiresOriginalRootOwner(t *testing.T) {
	s := &Service{}
	if _, call, err := s.beginOAuthProcess(context.Background()); err == nil || call != nil {
		t.Fatal("OAuth admitted without root process owner")
	}
	if err := s.requireOAuthProcessInTx(context.Background(), f.Tx{}); err == nil {
		t.Fatal("DTO admitted without original operation")
	}
	s.owner = &processOwner{}
	ctx := context.WithValue(context.Background(), oauthProcessKey{}, &oauthProcessCall{service: &Service{}, requestID: "request", commandDigest: "digest"})
	if err := s.oauthProcessInTx(ctx, f.Tx{}, "request", "digest"); err == nil {
		t.Fatal("another Service operation accepted")
	}
}
