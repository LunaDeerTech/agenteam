package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Embedded nil ports panic if an unbound constructor accidentally starts I/O.
type unopenedStore struct{ Store }
type unopenedObjects struct{ oc.Objects }
type unopenedInvocations struct{ tc.MCPInvocationAuthority }
type unopenedResources struct {
	tc.MCPResourceInvocationAuthority
}

func TestMCPStaticContentAndResourceBindingFailsClosed(t *testing.T) {
	binding, err := NewAuthority(&unopenedStore{})
	if err != nil {
		t.Fatal("static authority construction")
	}
	content, err := NewContentAuthorityWithAuthority(binding, &unopenedInvocations{}, nil, &unopenedResources{})
	if err != nil {
		t.Fatal("static content authority construction")
	}
	store, err := NewContentStore(content, &unopenedObjects{})
	if err != nil {
		t.Fatal("static content store construction")
	}
	backend, err := NewResourceBackendWithAuthority(binding, &unopenedResources{}, store)
	if err != nil {
		t.Fatal("static resource backend construction")
	}
	if content.service() != nil || backend.service() != nil {
		t.Fatal("construction bound a service")
	}
	if err = backend.RequireBindingInTx(context.Background(), f.Tx{}, &unopenedStore{}); err == nil {
		t.Fatal("unbound resource backend authorized a Registry store")
	}
	if _, err = content.AuthorizeOwner(context.Background(), id.Actor{}, oc.ObjectOwner{}, id.Read); err == nil {
		t.Fatal("unbound content authority authorized an Object owner")
	}
	if _, err = store.Resolve(context.Background(), id.Actor{}, oc.BusinessFileRef{}); err == nil {
		t.Fatal("unbound source resolver returned a file")
	}
}
