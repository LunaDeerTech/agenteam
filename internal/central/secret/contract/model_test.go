package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestModelUsageRequestIDClosedVariants(t *testing.T) {
	lease, _ := foundation.ParseID[Lease](accountID)
	for _, purpose := range []Purpose{Model, MCP, System, SMTP} {
		for _, kind := range []LeaseOwnerKind{ExecutionOwner, ModelCallOwner, AccountDeliveryOwner, AccountResponseOwner} {
			owner, _ := NewCredentialLeaseOwner(kind, accountID)
			for _, action := range []UsageAction{AcquireLeaseUsage, ReleaseLeaseUsage, ReadLeaseUsage, RetainReferenceUsage, ReleaseReferenceUsage} {
				name := string(purpose) + "/" + string(kind) + "/" + string(action)
				t.Run(name, func(t *testing.T) {
					r := UsageRequest{Actor: accountActor(t, identity.SecretService), Ref: accountRef(t), Purpose: purpose, LeaseOwner: owner, LeaseID: lease, Action: action}
					if action == RetainReferenceUsage || action == ReleaseReferenceUsage {
						r.ReferenceOwner, r.LeaseOwner, r.LeaseID, r.Retain = accountID, CredentialLeaseOwner{}, LeaseID{}, action == RetainReferenceUsage
					}
					need := purpose == Model && action == ReadLeaseUsage && (kind == ExecutionOwner || kind == ModelCallOwner)
					for _, request := range []string{"", anotherAccountID, "00000000-0000-0000-0000-000000000000", "01900000-0000-4000-8000-000000000001", "01900000-0000-7000-8000-00000000000A", "bad"} {
						r.RequestID = request
						valid := request == "" && !need || request == anotherAccountID && need
						if (r.Validate() == nil) != valid {
							t.Fatalf("request %q validity, wanted %v", request, valid)
						}
					}
				})
			}
		}
	}
}

func TestUsageBindingLegacyBytesAndInvocationIdentity(t *testing.T) {
	// Literal legacy JSON at 4cc4726, before RequestID existed. These fixtures
	// include fields deliberately encoded even when empty; do not normalize it.
	const prefix = `{"Actor":{"Kind":"service","UserID":"","SessionID":"","ProjectID":"","AgentID":"","ExecutionID":"","ServiceName":"secret","CauseRef":"01900000-0000-7000-8000-000000000001"},"Scope":{"kind":"system"},"Ref":"01900000-0000-7000-8000-000000000001",`
	lease, _ := foundation.ParseID[Lease](accountID)
	for _, tc := range []struct {
		purpose Purpose
		kind    LeaseOwnerKind
		action  UsageAction
	}{
		{System, AccountResponseOwner, ReadLeaseUsage}, {SMTP, AccountDeliveryOwner, AcquireLeaseUsage},
		{MCP, ExecutionOwner, ReadLeaseUsage}, {Model, ModelCallOwner, AcquireLeaseUsage},
		{Model, ModelCallOwner, ReleaseLeaseUsage}, {Model, "", RetainReferenceUsage}, {Model, "", ReleaseReferenceUsage},
	} {
		t.Run(string(tc.purpose)+"/"+string(tc.kind)+"/"+string(tc.action), func(t *testing.T) {
			r := UsageRequest{Actor: accountActor(t, identity.SecretService), Ref: accountRef(t), Purpose: tc.purpose, Action: tc.action}
			ownerID, leaseID, reference, retain := accountID, accountID, "", false
			if tc.kind == "" {
				ownerID, leaseID, reference, retain = "", "", accountID, tc.action == RetainReferenceUsage
				r.ReferenceOwner, r.Retain = reference, retain
			} else {
				r.LeaseOwner, _ = NewCredentialLeaseOwner(tc.kind, accountID)
				r.LeaseID = lease
			}
			golden := prefix + fmt.Sprintf(`"Purpose":%q,"ReferenceOwner":%q,"LeaseOwner":{"kind":%q,"id":%q},"LeaseID":%q,"Action":%q,"Retain":%t}`, tc.purpose, reference, tc.kind, ownerID, leaseID, tc.action, retain)
			digest := sha256.Sum256([]byte(golden))
			got, err := UsageBinding(r)
			if err != nil || string(got) != "sha256:"+hex.EncodeToString(digest[:]) {
				t.Fatalf("legacy byte digest changed: %v %s", err, got)
			}
		})
	}
	owner, _ := NewCredentialLeaseOwner(ModelCallOwner, accountID)
	r := UsageRequest{Actor: accountActor(t, identity.SecretService), Ref: accountRef(t), Purpose: Model, LeaseOwner: owner, LeaseID: lease, Action: ReadLeaseUsage, RequestID: anotherAccountID}
	a, err := UsageBinding(r)
	if err != nil {
		t.Fatal(err)
	}
	r.RequestID = accountID
	b, err := UsageBinding(r)
	if err != nil || a == b || !strings.HasPrefix(string(a), "sha256:") {
		t.Fatal("Invocation omitted from binding", err)
	}
}
