package model

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestModelSecretReleaseRequiresExactBeforeAfterCommandPlan(t *testing.T) {
	actor := testActor(t)
	a := pureAuthority(t, &noIOStore{})
	old, _ := sc.NewCredentialRef(mustID[sc.Credential](t), id.SystemScope())
	next, _ := sc.NewCredentialRef(mustID[sc.Credential](t), id.SystemScope())
	owner := mustID[struct{}](t).String()
	p := &preparedCommand{authority: a, actor: actor, plan: mutationPlan{Resource: owner, BeforeProvider: &providerRecord{ID: owner, Input: providerInput{CredentialID: old.Details().ID.String()}}, AfterProvider: &providerRecord{ID: owner, Input: providerInput{CredentialID: next.Details().ID.String()}}}}
	r := sc.UsageRequest{Actor: actor, Ref: old, Purpose: sc.Model, ReferenceOwner: owner, Action: sc.ReleaseReferenceUsage}
	if !referenceMatches(p, r) {
		t.Fatal("exact old reference rejected")
	}
	copy := r
	copy.Ref = next
	if referenceMatches(p, copy) {
		t.Fatal("release of newly retained reference accepted")
	}
	copy = r
	copy.ReferenceOwner = mustID[struct{}](t).String()
	if referenceMatches(p, copy) {
		t.Fatal("foreign provider accepted")
	}
	p.plan.BeforeProvider = nil
	if referenceMatches(p, r) {
		t.Fatal("current ref inequality alone authorized release")
	}
	e := a.CheckReferenceInTx(context.Background(), f.NewTx(), actor, old, sc.Model, owner, false)
	requireCode(t, e, f.ResourceBusy)
	if _, e = a.AuthorizeLeaseInTx(context.Background(), f.NewTx(), actor, old, sc.CredentialLeaseOwner{}, sc.AcquireLease); e == nil {
		t.Fatal("unbound call lease authorized")
	}
}
