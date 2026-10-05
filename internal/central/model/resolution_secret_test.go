package model

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestModelCurrentResolutionCandidateCannotGrantOrRead(t *testing.T) {
	a := resolutionTestAuthority(t, &noIOStore{})
	r := resolutionTestRequest(t)
	d := resolutionTestDraft(t, r)
	identity, e := resolutionIdentity(r)
	if e != nil {
		t.Fatal(e)
	}
	c := &resolutionCandidate{authority: a, request: r, identity: identity, draft: d, version: 1, locks: resolutionBaseLocks(r, identity, d.Snapshot.ID, d)}
	usage, e := a.resolutionUsage(c)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.WithValue(context.Background(), resolutionCandidateKey{}, c)
	plan, e := a.DiscoverUsage(ctx, usage)
	if e != nil {
		t.Fatal(e)
	}
	if plan.Validate() != nil {
		t.Fatal("candidate could not be planned")
	}
	_, e = a.AuthorizeLeaseInTx(ctx, f.NewTx(), usage.Actor, usage.Ref, usage.LeaseOwner, sc.AcquireLease)
	requireCode(t, e, f.DependencyUnbound)
	e = a.ValidateUsageInTx(ctx, f.NewTx(), usage, plan)
	requireCode(t, e, f.DependencyUnbound)
	foreign := *c
	foreign.authority = resolutionTestAuthority(t, &noIOStore{})
	_, e = a.DiscoverUsage(context.WithValue(context.Background(), resolutionCandidateKey{}, &foreign), usage)
	requireCode(t, e, f.DependencyUnbound)
	tx := f.NewTx()
	w := &resolutionApply{candidate: c, tx: tx, active: true}
	ctx = context.WithValue(context.Background(), resolutionApplyKey{}, w)
	_, e = a.AuthorizeLeaseInTx(ctx, tx, usage.Actor, usage.Ref, usage.LeaseOwner, sc.ReadLease)
	requireCode(t, e, f.Forbidden)
	w.verified = true
	w.active = false
	_, e = a.AuthorizeLeaseInTx(ctx, tx, usage.Actor, usage.Ref, usage.LeaseOwner, sc.AcquireLease)
	requireCode(t, e, f.Forbidden)
	w.active = true
	_, e = a.AuthorizeLeaseInTx(ctx, f.NewTx(), usage.Actor, usage.Ref, usage.LeaseOwner, sc.AcquireLease)
	requireCode(t, e, f.Forbidden)
}
