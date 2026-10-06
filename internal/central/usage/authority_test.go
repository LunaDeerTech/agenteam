package usage

import (
	"context"
	"errors"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

type pureStore struct{ Store }
type pureSessions struct{ id.SessionAuthority }
type pureProjects struct{ ProjectAuthority }
type nilFacts chan struct{}

func (nilFacts) Discover(context.Context, uc.InvocationRequest) (uc.InvocationDependencies, error) {
	panic("nil facts used")
}
func (nilFacts) ValidateInTx(context.Context, f.Tx, uc.InvocationRequest, uc.InvocationDependencies) (uc.InvocationFact, error) {
	panic("nil facts used")
}
func testCursor(t *testing.T) cursor.Keyring {
	t.Helper()
	k, e := cursor.LoadKeyring(`{"format":1,"current_kid":"usage","keys":[{"kid":"usage","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if e != nil {
		t.Fatal(e)
	}
	return k
}
func expectFault(t *testing.T, e error, c f.Code) {
	t.Helper()
	var ff *f.Fault
	if !errors.As(e, &ff) || ff.Code != c {
		t.Fatalf("got %v; expected %s", e, c)
	}
}
func TestUsageConstructionHasNoIO(t *testing.T) {
	store := &pureStore{}
	auth := Authorizations{Sessions: &pureSessions{}, Projects: &pureProjects{}}
	a, e := NewAuthority(store, auth)
	if e != nil {
		t.Fatal(e)
	}
	s, e := New(store, a, Dependencies{testCursor(t)})
	if e != nil {
		t.Fatal(e)
	}
	_, e = New(&pureStore{}, a, Dependencies{testCursor(t)})
	expectFault(t, e, f.DependencyUnbound)
	for _, v := range []Authorizations{{Projects: auth.Projects}, {Sessions: auth.Sessions}, {Sessions: auth.Sessions, Projects: auth.Projects, Invocations: nilFacts(nil)}} {
		_, e = NewAuthority(store, v)
		expectFault(t, e, f.DependencyUnbound)
	}
	_, e = s.DiscoverInvocation(context.Background(), uc.InvocationRequest{})
	expectFault(t, e, f.DependencyUnbound)
	var zero *Service
	_, e = zero.List(context.Background(), id.Actor{}, uc.Query{})
	expectFault(t, e, f.DependencyUnbound)
	e = zero.Initialize(nil)
	expectFault(t, e, f.InvalidArgument)
}
