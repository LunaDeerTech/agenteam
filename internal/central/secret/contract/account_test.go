package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const accountID = "01900000-0000-7000-8000-000000000001"
const anotherAccountID = "01900000-0000-7000-8000-000000000002"

func accountActor(t *testing.T, name identity.ServiceName) identity.Actor {
	t.Helper()
	p, e := identity.RegisterService(name)
	if e != nil {
		t.Fatal(e)
	}
	a, e := p.Actor(accountID, identity.SystemScope())
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func accountRef(t *testing.T) CredentialRef {
	t.Helper()
	id, _ := foundation.ParseID[Credential](accountID)
	r, e := NewCredentialRef(id, identity.SystemScope())
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestAccountServiceWriteRoleMatrixAndSafePlans(t *testing.T) {
	material, e := NewSecretMaterial([]byte("unique-private-account-token"))
	if e != nil {
		t.Fatal(e)
	}
	defer material.Destroy()
	key, _ := foundation.NewCommandIdentity("secret.account", []string{accountID}, "create", "private-command-key")
	f := ServiceWriteFields{Actor: accountActor(t, identity.AccountAuth), Scope: identity.SystemScope(), Identity: key, Kind: Create, Purpose: System, OwnerKind: LoginResponseMaterial, OwnerID: accountID, Value: material}
	r, e := NewServiceWriteRequest(f)
	if e != nil {
		t.Fatal(e)
	}
	binding, e := ServiceWriteBinding(r)
	if e != nil {
		t.Fatal(e)
	}
	if copy, e := ServiceWriteBinding(r.WithoutMaterial()); e != nil || copy != binding {
		t.Fatal("dropping handle changed original semantic binding")
	}
	for _, change := range []func(*ServiceWriteFields){
		func(f *ServiceWriteFields) { f.Actor = accountActor(t, identity.AccountBootstrap) }, func(f *ServiceWriteFields) { f.Actor = accountActor(t, identity.AccountMail) }, func(f *ServiceWriteFields) { f.Actor = accountActor(t, identity.AccountMaintenance) },
		func(f *ServiceWriteFields) { f.OwnerKind = SMTPSettingsMaterial }, func(f *ServiceWriteFields) { f.Purpose = SMTP }, func(f *ServiceWriteFields) { f.OwnerID = anotherAccountID }, func(f *ServiceWriteFields) { f.Kind = Update }, func(f *ServiceWriteFields) { f.ExpectedVersion = 1 }, func(f *ServiceWriteFields) { f.Ref = accountRef(t) },
	} {
		copy := f
		change(&copy)
		if _, e := NewServiceWriteRequest(copy); e == nil {
			t.Fatal("forbidden service write accepted")
		}
	}
	issuer := NewPlanIssuer()
	lock, _ := foundation.UserLock(accountID)
	input := []foundation.LockRequest{{Key: lock, Mode: foundation.Shared}, {Key: lock, Mode: foundation.Exclusive}}
	d, e := NewWriteDependencies(issuer, binding, binding, input)
	if e != nil {
		t.Fatal(e)
	}
	input[1].Mode = foundation.Shared
	if len(d.Locks()) != 1 || d.Locks()[0].Mode != foundation.Exclusive {
		t.Fatal("strongest mode/input copy")
	}
	copy := d.Locks()
	copy[0].Mode = foundation.Shared
	if d.Locks()[0].Mode != foundation.Exclusive || d.Matches(NewPlanIssuer(), binding, binding) {
		t.Fatal("plan manufactured or modified")
	}
	var out bytes.Buffer
	for _, v := range []any{r, &r, d, &d, struct{ x any }{r}, struct{ x ServiceWriteRequest }{r}} {
		for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			fmt.Fprintf(&out, f, v)
		}
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		out.Write(b)
		slog.New(slog.NewTextHandler(&out, nil)).Info("projection", "value", v)
	}
	for _, secret := range []string{"unique-private-account-token", "private-command-key", string(binding)} {
		if strings.Contains(out.String(), secret) {
			t.Fatal("unsafe projection")
		}
	}
	if json.Unmarshal([]byte(`{}`), &r) == nil || json.Unmarshal([]byte(`null`), &d) == nil {
		t.Fatal("JSON manufactured authority")
	}
}
func TestAccountUsageClosedVariantsAndCopiedBinding(t *testing.T) {
	owner, e := NewCredentialLeaseOwner(AccountResponseOwner, accountID)
	if e != nil {
		t.Fatal(e)
	}
	lease, _ := foundation.ParseID[Lease](accountID)
	base := UsageRequest{Actor: accountActor(t, identity.SecretService), Ref: accountRef(t), Purpose: System}
	for _, action := range []UsageAction{RetainReferenceUsage, ReleaseReferenceUsage, AcquireLeaseUsage, ReleaseLeaseUsage, ReadLeaseUsage} {
		r := base
		r.Action = action
		if action == RetainReferenceUsage || action == ReleaseReferenceUsage {
			r.ReferenceOwner = accountID
			r.Retain = action == RetainReferenceUsage
		} else {
			r.LeaseOwner = owner
			r.LeaseID = lease
		}
		if e = r.Validate(); e != nil {
			t.Fatal(action, e)
		}
		binding, e := UsageBinding(r)
		if e != nil {
			t.Fatal(e)
		}
		bad := r
		if r.ReferenceOwner != "" {
			bad.LeaseID = lease
		} else {
			bad.ReferenceOwner = accountID
		}
		if bad.Validate() == nil {
			t.Fatal("extra field accepted", action)
		}
		bad = r
		bad.Retain = !r.Retain
		if bad.Validate() == nil {
			t.Fatal("conflicting retain accepted", action)
		}
		changed := r
		changed.Purpose = SMTP
		other, e := UsageBinding(changed)
		if e != nil || binding == other {
			t.Fatal("purpose omitted from binding")
		}
		issuer := NewPlanIssuer()
		key, _ := foundation.UserLock(accountID)
		plan, e := NewUsageDependencies(issuer, binding, binding, []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}})
		if e != nil {
			t.Fatal(e)
		}
		outer := NewPlanIssuer()
		wrapped, e := WrapUsageDependencies(outer, plan, plan.RequiredLocks())
		if e != nil {
			t.Fatal(e)
		}
		ls := wrapped.RequiredLocks()
		ls[0].Mode = foundation.Shared
		if wrapped.RequiredLocks()[0].Mode != foundation.Exclusive || !wrapped.Matches(outer, binding, binding) || wrapped.Matches(issuer, binding, binding) || !wrapped.ProviderPlan().Matches(issuer, binding, binding) {
			t.Fatal("issuer or copied locks")
		}
		value := CredentialLease{}
		if action == AcquireLeaseUsage {
			value = CredentialLease{LeaseID: lease, CredentialRef: r.Ref}
		}
		result, e := NewUsageResult(action, value)
		if action == ReadLeaseUsage {
			if e == nil {
				t.Fatal("read result accepted")
			}
			continue
		}
		if e != nil || result.Action() != action {
			t.Fatal("result variant", e)
		}
		_, ok := result.Lease()
		if ok != (action == AcquireLeaseUsage) {
			t.Fatal("unexpected lease in result")
		}
	}
}
