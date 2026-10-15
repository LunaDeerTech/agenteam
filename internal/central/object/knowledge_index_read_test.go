package object

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type fixedIndexReadGrant struct {
	grant     oc.OwnerAuthorization
	gateCalls int
	gateErr   error
}

func (a *fixedIndexReadGrant) AuthorizeOwner(context.Context, id.Actor, oc.ObjectOwner, id.AccessIntent) (oc.OwnerAuthorization, error) {
	panic("readRow must use the original transaction")
}
func (a *fixedIndexReadGrant) AuthorizeOwnerInTx(context.Context, f.Tx, id.Actor, oc.ObjectOwner, id.AccessIntent) (oc.OwnerAuthorization, error) {
	return a.grant, nil
}
func (a *fixedIndexReadGrant) CheckInTx(context.Context, f.Tx, id.Actor, oc.ObjectOwner, id.AccessIntent) error {
	a.gateCalls++
	return a.gateErr
}

func TestReadRowCannotBroadenIndexGrantThroughCanonicalReference(t *testing.T) {
	project, _ := f.NewID[id.Project]()
	job, _ := f.NewID[struct{}]()
	document, _ := f.NewID[struct{}]()
	object, _ := f.NewID[oc.StoredObject]()
	other, _ := f.NewID[oc.StoredObject]()
	scope, _ := id.InProject(project)
	registration, _ := id.RegisterService(id.KnowledgeRuntime)
	actor, _ := registration.Actor(job.String(), scope)
	owner, _ := oc.NewObjectOwner(oc.Knowledge, document.String(), project.String())
	grant, err := oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: id.Read, Existence: oc.ExistingOwner, Version: 1, ReadObjectID: object})
	if err != nil {
		t.Fatal(err)
	}
	marker := errors.New("current-project-gate")
	authority := &fixedIndexReadGrant{grant: grant, gateErr: marker}
	state := &serviceState{auth: Authorizations{Resources: authority, Gate: authority}}
	service := &Service{data: func() *serviceState { return state }}
	// No Store is bound: the wrong target must be rejected before either a
	// canonical reference lookup or a protected-lease fallback could allow it.
	if _, err = service.readRow(context.Background(), f.NewTx(), actor, owner, other); !hasCode(err, f.Forbidden) || authority.gateCalls != 0 {
		t.Fatal("foreign object reached the canonical read path", err)
	}
	// The exact object must still cross the current Project gate. This is not
	// evidence of a real database read; the App indexing test covers that path.
	if _, err = service.readRow(context.Background(), f.NewTx(), actor, owner, object); !errors.Is(err, marker) || authority.gateCalls != 1 {
		t.Fatal("exact target bypassed current authorization", err)
	}
}
