package model

import (
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"strings"
	"testing"
	"time"
)

func TestManagementReceiptClosedProjectionAndBounds(t *testing.T) {
	at, _ := f.NewInstant(time.Now())
	o := managementOperation{ID: mustID[struct{}](t).String(), Provider: mustID[struct{}](t).String(), ProviderVersion: 3, Action: "discovery", CreatedAt: at}
	for _, reason := range []string{"", "timeout", "interrupted"} {
		r := o.result(reason, 25*time.Second)
		if r.DurationMS != 20000 || r.Items == nil || len(*r.Items) != 0 {
			t.Fatal("unbounded receipt")
		}
		b, e := json.Marshal(r)
		if e != nil {
			t.Fatal(e)
		}
		if !strings.Contains(string(b), `"provider_version":"3"`) {
			t.Fatal("version must remain decimal string")
		}
		if reason == "interrupted" && r.Outcome != "unknown" {
			t.Fatal("interrupted cannot claim failure/success")
		}
	}
	if o.result("timeout", -time.Second).DurationMS != 0 {
		t.Fatal("negative duration")
	}
}
func TestManagementSecretRejectsForgedOwnerWithoutWitness(t *testing.T) {
	a := &ManagementSecretAuthority{}
	cid := mustID[sc.Credential](t)
	ref, _ := sc.NewCredentialRef(cid, id.SystemScope())
	owner, _ := sc.NewCredentialLeaseOwner(sc.ModelManagementRequestOwner, mustID[struct{}](t).String())
	reg, _ := id.RegisterService(id.SecretService)
	actor, _ := reg.Actor(owner.Details().ID, id.SystemScope())
	lease := mustID[sc.Lease](t)
	req := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.Model, LeaseOwner: owner, LeaseID: lease, Action: sc.ReadLeaseUsage}
	if req.Validate() != nil {
		t.Fatal("expected valid structural request")
	}
	_, e := a.DiscoverUsage(context.Background(), req)
	requireCode(t, e, f.Forbidden)
	_, e = a.AuthorizeLeaseInTx(context.Background(), f.Tx{}, actor, ref, owner, sc.ReadLease)
	requireCode(t, e, f.Forbidden)
}
