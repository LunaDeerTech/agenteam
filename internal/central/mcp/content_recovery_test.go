package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type recoveryResolverReject struct{ calls int }

func (r *recoveryResolverReject) Discover(context.Context, oc.ObjectOwner, f.IdempotencyKey) (oc.MCPUploadMapping, bool, error) {
	r.calls++
	return oc.MCPUploadMapping{}, false, fail(f.Forbidden)
}
func (r *recoveryResolverReject) ResolveInTx(context.Context, f.Tx, oc.ObjectOwner, f.IdempotencyKey) (oc.MCPUploadMapping, bool, error) {
	r.calls++
	return oc.MCPUploadMapping{}, false, fail(f.Forbidden)
}

func TestMCPContentRecoveryRequiresOriginalPrivateClaim(t *testing.T) {
	resolver := &recoveryResolverReject{}
	service := &Service{owner: &processOwner{}}
	a := &ContentAuthority{direct: service, recovery: resolver}
	w := &contentRecoveryUse{authority: a, call: &processCall{owner: service.owner}, stopped: true}
	w.active.Store(true)
	for name, ctx := range map[string]context.Context{
		"bare DTO":          context.Background(),
		"wrong type":        context.WithValue(context.Background(), contentRecoveryKey{}, struct{}{}),
		"no original claim": context.WithValue(context.Background(), contentRecoveryKey{}, &contentRecoveryUse{authority: a}),
		"foreign authority": context.WithValue(context.Background(), contentRecoveryKey{}, &contentRecoveryUse{authority: &ContentAuthority{}, call: w.call, stopped: true}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := a.recoveryUse(ctx); err == nil {
				t.Fatal("unissued recovery accepted")
			}
		})
	}
	ctx := context.WithValue(context.Background(), contentRecoveryKey{}, w)
	w.stopped = false
	if _, err := a.recoveryUse(ctx); err == nil {
		t.Fatal("missing stopped proof accepted")
	}
	w.stopped = true
	w.active.Store(false)
	if _, err := a.recoveryUse(ctx); err == nil {
		t.Fatal("expired witness accepted")
	}
	w.active.Store(true)
	w.call = &processCall{owner: &processOwner{}}
	if _, err := a.recoveryUse(ctx); err == nil {
		t.Fatal("foreign process owner accepted")
	}
	if err := a.CheckCleanupInTx(context.Background(), f.NewTx(), oc.ObjectCleanupCause{}, oc.ObjectID{}); err == nil || resolver.calls != 0 {
		t.Fatal("bare cleanup DTO reached resolver")
	}
}

func TestMCPContentRecoveryNeverAuthorizesNewContentRead(t *testing.T) {
	project, _ := f.NewID[id.Project]()
	materialization, _ := f.NewID[oc.Upload]()
	object, _ := f.NewID[oc.StoredObject]()
	process, _ := f.NewID[oc.Process]()
	agent, _ := f.NewID[id.Agent]()
	execution, _ := f.NewID[id.Execution]()
	owner, _ := oc.NewObjectOwner(oc.MCPContent, materialization.String(), project.String())
	actor, _ := id.NewAgentRun(project, agent, execution)
	resolver := &recoveryResolverReject{}
	service := &Service{owner: &processOwner{process: process}}
	a := &ContentAuthority{direct: service, recovery: resolver}
	w := &contentRecoveryUse{authority: a, call: &processCall{owner: service.owner}, stopped: true, found: true, mapping: oc.MCPUploadMapping{ObjectID: object}}
	w.active.Store(true)
	ctx := context.WithValue(context.Background(), contentRecoveryKey{}, w)
	read, err := oc.NewObjectReadAccess(oc.AccessRequestDetails{Operation: oc.ReadAccess, Actor: actor, Owner: owner, ObjectID: object, Intent: id.Read})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Discover(ctx, read); err == nil {
		t.Fatal("recovery became a new read grant")
	}
	for _, operation := range []oc.AccessOperation{oc.ReleaseReaderAccess, oc.FinishWriterAccess, oc.RecoverAttemptAccess, oc.ReleaseProcessAccess} {
		// Raw fields must not bypass the maintenance constructor's closed variants.
		d := oc.AccessRequestDetails{Operation: operation, InstanceID: process, ObjectID: object}
		switch operation {
		case oc.ReleaseReaderAccess:
			d.LeaseID, _ = f.NewID[oc.Lease]()
		case oc.FinishWriterAccess, oc.RecoverAttemptAccess:
			d.AttemptID, _ = f.NewID[oc.Attempt]()
		case oc.ReleaseProcessAccess:
			d.ProcessID, _ = f.NewID[oc.Process]()
		}
		request, err := oc.NewMaintenanceAccess(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = a.Discover(ctx, request); err == nil {
			t.Fatal("unrelated cleanup operation allowed", operation)
		}
	}
	if resolver.calls != 0 {
		t.Fatal("new read or unrelated tail queried original upload")
	}
}

func TestMCPContentRecoveryUnboundStartupDoesNotAdmit(t *testing.T) {
	for _, store := range []*ContentStore{nil, {}, {authority: &ContentAuthority{direct: &Service{}}}} {
		if err := store.StartRecovery(context.Background()); err == nil {
			t.Fatal("unbound recovery admitted")
		}
	}
}
