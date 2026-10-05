package model

import (
	"context"
	"errors"

	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type strictFallback struct {
	t       *testing.T
	request sc.UsageRequest
	calls   int
	err     error
}

func (s *strictFallback) CheckReferenceInTx(_ context.Context, _ f.Tx, a id.Actor, ref sc.CredentialRef, p sc.Purpose, owner string, retain bool) error {
	if a.Details() != s.request.Actor.Details() || !ref.Equal(s.request.Ref) || p != s.request.Purpose || owner != s.request.ReferenceOwner || retain != s.request.Retain {
		s.t.Fatal("fallback reference changed")
	}
	s.calls++
	return s.err
}
func (s *strictFallback) AuthorizeLeaseInTx(_ context.Context, _ f.Tx, a id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, _ sc.LeaseAction) (sc.UseGrant, error) {
	if a.Details() != s.request.Actor.Details() || !ref.Equal(s.request.Ref) || !owner.Equal(s.request.LeaseOwner) {
		s.t.Fatal("fallback lease changed")
	}
	s.calls++
	return sc.UseGrant{}, s.err
}
func (s *strictFallback) DiscoverUsage(_ context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	if !sameUsage(r, s.request) {
		s.t.Fatal("fallback plan changed")
	}
	s.calls++
	return sc.UsageDependencies{}, s.err
}
func (s *strictFallback) ValidateUsageInTx(_ context.Context, _ f.Tx, r sc.UsageRequest, _ sc.UsageDependencies) error {
	if !sameUsage(r, s.request) {
		s.t.Fatal("fallback validation changed")
	}
	s.calls++
	return s.err
}
func TestModelSecretRouterPreservesFallbackAndDoesNotGuessLeasePurpose(t *testing.T) {
	a := pureAuthority(t, &noIOStore{})
	actor := testActor(t)
	ref, _ := sc.NewCredentialRef(mustID[sc.Credential](t), id.SystemScope())
	owner, _ := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, mustID[struct{}](t).String())
	sentinel := errors.New("fallback sentinel")
	request := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseOwner: owner, LeaseID: mustID[sc.Lease](t), Action: sc.ReadLeaseUsage}
	fallback := &strictFallback{t: t, request: request, err: sentinel}
	router, e := NewSecretUsageRouter(a, fallback)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = router.DiscoverUsage(context.Background(), request); e != sentinel {
		t.Fatal("fallback error lost")
	}
	if e = router.ValidateUsageInTx(context.Background(), f.NewTx(), request, sc.UsageDependencies{}); e != sentinel {
		t.Fatal("fallback validation changed")
	}
	if _, e = router.AuthorizeLeaseInTx(context.Background(), f.NewTx(), actor, ref, owner, sc.ReadLease); e != sentinel {
		t.Fatal("execution lease was guessed to be Model")
	}
	request.Purpose = sc.Model
	if _, e = router.DiscoverUsage(context.Background(), request); e == nil {
		t.Fatal("Model read lease gained unbound call authority")
	}
	if fallback.calls != 3 {
		t.Fatal("unbound Model request reached fallback")
	}
	if _, e = NewSecretUsageRouter(a, nil); e == nil {
		t.Fatal("implicit fallback allowed")
	}
}

func sameUsage(a, b sc.UsageRequest) bool {
	x, e := sc.UsageBinding(a)
	if e != nil {
		return false
	}
	y, e := sc.UsageBinding(b)
	return e == nil && x == y
}
