package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestModelRuntimeRegistrationAddsNoGrant(t *testing.T) {
	project, _ := foundation.NewID[Project]()
	invocation, _ := foundation.NewID[struct{}]()
	scope, _ := InProject(project)
	r, e := RegisterService(ModelRuntime)
	if e != nil {
		t.Fatal(e)
	}
	a, e := r.Actor(invocation.String(), scope)
	if e != nil || a.Details().ServiceName != ModelRuntime || a.Details().ProjectID != project.String() || a.Details().CauseRef != invocation.String() {
		t.Fatal("runtime identity", e)
	}
	if (AccessGrant{}).Matches(a, scope, Read) {
		t.Fatal("registration granted Owner access")
	}
	if _, e = RegisterService("model-runtime-admin"); e == nil {
		t.Fatal("unknown role accepted")
	}
	if _, e = r.Actor("caller-claims-joined", scope); e == nil {
		t.Fatal("arbitrary cause accepted")
	}
}

func TestTrustedIdentityCannotBeDeserializedOrUsedAsBearerGrant(t *testing.T) {
	u, _ := foundation.ParseID[User]("01900000-0000-7000-8000-000000000001")
	s, _ := foundation.ParseID[Session]("01900000-0000-7000-8000-000000000002")
	a, e := NewHuman(u, s)
	if e != nil {
		t.Fatal(e)
	}
	now, _ := foundation.NewInstant(time.Now())
	g, e := NewAccessGrant(a, SystemScope(), Read, now, 1)
	if e != nil || !g.Matches(a, SystemScope(), Read) || g.Matches(a, SystemScope(), Mutate) {
		t.Fatal("grant binding lost")
	}
	s2, _ := foundation.ParseID[Session]("01900000-0000-7000-8000-000000000003")
	other, _ := NewHuman(u, s2)
	if g.Matches(other, SystemScope(), Read) {
		t.Fatal("grant survived session switch")
	}
	for _, target := range []any{new(Actor), new(Scope), new(AccessGrant)} {
		if json.Unmarshal([]byte(`{"admin":true,"owner":true}`), target) == nil {
			t.Fatal("caller supplied identity accepted")
		}
	}
	for _, v := range []any{a, g, struct {
		actor Actor
		grant AccessGrant
	}{a, g}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			if strings.Contains(fmt.Sprintf(format, v), s.String()) {
				t.Fatal("session leaked implicitly")
			}
		}
	}
	if _, e := NewHuman(UserID{}, s); e == nil {
		t.Fatal("invalid identity accepted")
	}
	if _, e := RegisterService("arbitrary-admin"); e == nil {
		t.Fatal("unknown service role accepted")
	}
	r, _ := RegisterService(SecretMaintenance)
	if _, e := r.Actor("private-secret-canary", SystemScope()); e == nil {
		t.Fatal("free-text service cause accepted")
	}
	service, e := r.Actor(u.String(), SystemScope())
	if e != nil || service.Details().ServiceName != SecretMaintenance {
		t.Fatal("registered service missing")
	}
	if (AccessGrant{}).Matches(a, SystemScope(), Read) || (Actor{}).Validate() == nil || (Scope{}).Validate() == nil {
		t.Fatal("zero identity granted access")
	}
}

func TestObjectServicesRequireFixedRegistrationAndCause(t *testing.T) {
	project, _ := foundation.ParseID[Project]("01900000-0000-7000-8000-000000000001")
	scope, _ := InProject(project)
	for _, name := range []ServiceName{ObjectService, ObjectMaintenance} {
		r, err := RegisterService(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []Scope{SystemScope(), scope} {
			a, err := r.Actor(project.String(), s)
			if err != nil || a.Details().ServiceName != name || a.Details().ProjectID != s.Details().ProjectID {
				t.Fatal("service scope changed")
			}
			if _, err = r.Actor("owner=true", s); err == nil {
				t.Fatal("untrusted cause accepted")
			}
		}
	}
	if _, err := RegisterService("object-admin"); err == nil {
		t.Fatal("object service granted an invented administrator role")
	}
}
