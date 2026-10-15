package app

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type mcpCleanupRouteProbe struct {
	objectOwnerAuthority
	ctx    context.Context
	tx     f.Tx
	cause  oc.ObjectCleanupCause
	object oc.ObjectID
	err    error
	calls  int
}

func (p *mcpCleanupRouteProbe) CheckCleanupInTx(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID) error {
	p.calls++
	p.ctx, p.tx, p.cause, p.object = ctx, tx, cause, object
	return p.err
}

func TestM3MCPRecoveryRoutesOriginalCleanupWithoutFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	originalTx := f.NewTx()
	owner, err := oc.NewObjectOwner(oc.MCPContent, updateRootID[oc.Upload]().String(), updateRootID[id.Project]().String())
	if err != nil {
		t.Fatal(err)
	}
	cause, err := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: updateRootID[oc.CleanupOperation](), Owner: owner, Reason: oc.AbandonedAttempt})
	if err != nil {
		t.Fatal(err)
	}
	object := updateRootID[oc.StoredObject]()
	routes := &objectAuthorities{}
	var fault *f.Fault
	if err := routes.CheckCleanupInTx(ctx, originalTx, cause, object); !errors.As(err, &fault) || fault.Code != f.DependencyUnbound {
		t.Fatal("missing MCP cleanup owner admitted", err)
	}
	denied := f.NewFault(f.CommitUnknown, f.Unknown)
	probe := &mcpCleanupRouteProbe{err: denied}
	routes.mcp = probe
	if got := routes.CheckCleanupInTx(ctx, originalTx, cause, object); got != denied || probe.calls != 1 {
		t.Fatal("MCP cleanup result was replaced", got)
	}
	got, want := probe.cause.Details(), cause.Details()
	if probe.ctx != ctx || probe.tx != originalTx || probe.object != object || got.OperationID != want.OperationID || !got.Owner.Equal(want.Owner) || got.Reason != want.Reason {
		t.Fatal("original cleanup context or tuple changed")
	}
	if err := routes.CheckCleanupInTx(ctx, originalTx, oc.ObjectCleanupCause{}, object); err == nil || probe.calls != 1 {
		t.Fatal("invalid cleanup reached the selected owner")
	}
}
