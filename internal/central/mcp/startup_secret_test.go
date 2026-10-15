package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestMCPStartupSecretGrantKeepsRequestInOriginalUsageProof(t *testing.T) {
	s, original, _ := startupCandidate(t)
	original.active.Store(true)
	defer original.active.Store(false)
	use, err := s.newStartupSecret(original, sc.ReadLeaseUsage)
	if err != nil {
		t.Fatal(err)
	}
	defer use.close()
	ctx := context.WithValue(context.WithValue(context.Background(), startupUseKey{}, original), startupSecretKey{}, use)
	plan, err := s.discoverStartupSecret(ctx, use.request)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := sc.UsageBinding(use.request)
	if err != nil || use.request.RequestID != original.request || !plan.Matches(s.secretIssuer, binding, use.mapping) {
		t.Fatal("original request was removed from the private usage plan", err)
	}
	tx := f.NewTx()
	use.validated = tx
	grant, err := s.authorizeStartupSecret(ctx, tx, use.request.Actor, use.request.Ref, use.request.LeaseOwner, sc.ReadLease)
	if err != nil || !grant.Subject.Equal(use.request.Actor) || grant.Consumer != sc.MCP || grant.RequestID != "" {
		t.Fatal("explicit reader grant must not use the legacy request association", err)
	}
	if _, err = s.authorizeStartupSecret(context.Background(), tx, use.request.Actor, use.request.Ref, use.request.LeaseOwner, sc.ReadLease); err == nil {
		t.Fatal("bare usage tuple acquired a grant")
	}
	if _, err = s.authorizeStartupSecret(ctx, f.NewTx(), use.request.Actor, use.request.Ref, use.request.LeaseOwner, sc.ReadLease); err == nil {
		t.Fatal("another transaction reused the validated grant")
	}
	changed := use.request
	changed.RequestID = contentTailID[f.Request](t, "39").String()
	if _, err = s.discoverStartupSecret(ctx, changed); err == nil {
		t.Fatal("another request reused the original read proof")
	}
	use.close()
	if _, err = s.authorizeStartupSecret(ctx, tx, use.request.Actor, use.request.Ref, use.request.LeaseOwner, sc.ReadLease); err == nil {
		t.Fatal("closed private usage retained its grant")
	}
}
