package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type skillTransferRouteProbe struct {
	skillPrepareTransferSource
	ctx   context.Context
	tx    f.Tx
	err   error
	calls []string
}

func (p *skillTransferRouteProbe) record(ctx context.Context, tx f.Tx, op string) error {
	if ctx != p.ctx || tx != p.tx {
		panic("router replaced original context or transaction")
	}
	p.calls = append(p.calls, op)
	return p.err
}
func (p *skillTransferRouteProbe) Discover(ctx context.Context, _ oc.TransferAccessRequest) (oc.AccessDependencies, error) {
	return oc.AccessDependencies{}, p.record(ctx, p.tx, "discover")
}
func (p *skillTransferRouteProbe) ValidateInTx(ctx context.Context, tx f.Tx, _ oc.TransferAccessRequest, _ oc.AccessDependencies) (oc.TransferAuthorization, error) {
	return oc.TransferAuthorization{}, p.record(ctx, tx, "validate")
}
func (p *skillTransferRouteProbe) DiscoverPreparationLease(ctx context.Context, _ oc.AccessRequest) (oc.AccessDependencies, error) {
	return oc.AccessDependencies{}, p.record(ctx, p.tx, "prepare-lease")
}
func (p *skillTransferRouteProbe) ValidatePreparationLeaseInTx(ctx context.Context, tx f.Tx, _ oc.AccessRequest, _ oc.AccessDependencies) error {
	return p.record(ctx, tx, "prepare-lease-tx")
}
func (p *skillTransferRouteProbe) DiscoverMaintenanceLease(ctx context.Context, _ oc.AccessRequest) (oc.AccessDependencies, error) {
	return oc.AccessDependencies{}, p.record(ctx, p.tx, "maintenance-lease")
}
func (p *skillTransferRouteProbe) ValidateMaintenanceLeaseInTx(ctx context.Context, tx f.Tx, _ oc.AccessRequest, _ oc.AccessDependencies) error {
	return p.record(ctx, tx, "maintenance-lease-tx")
}
func (p *skillTransferRouteProbe) AuthorizeLeaseInTx(ctx context.Context, tx f.Tx, _ id.Actor, _ oc.ObjectID, _ oc.LeaseOwner, _ oc.LeaseAction) error {
	return p.record(ctx, tx, "lease-authorize")
}
func (p *skillTransferRouteProbe) DiscoverTransferObservation(ctx context.Context, _ oc.TransferObservationRequest) (oc.AccessDependencies, error) {
	return oc.AccessDependencies{}, p.record(ctx, p.tx, "observation")
}
func (p *skillTransferRouteProbe) ValidateTransferObservationInTx(ctx context.Context, tx f.Tx, _ oc.TransferObservationRequest, _ oc.AccessDependencies) (oc.TransferObservationFacts, error) {
	return oc.TransferObservationFacts{}, p.record(ctx, tx, "observation-tx")
}
func (p *skillTransferRouteProbe) DiscoverSkillTransferRetirement(ctx context.Context, _ oc.SkillTransferRetirementRequest) (oc.AccessDependencies, error) {
	return oc.AccessDependencies{}, p.record(ctx, p.tx, "retirement")
}
func (p *skillTransferRouteProbe) ValidateSkillTransferRetirementInTx(ctx context.Context, tx f.Tx, _ oc.SkillTransferRetirementRequest, _ oc.AccessDependencies) error {
	return p.record(ctx, tx, "retirement-tx")
}

func skillTransferRouteRequest(t *testing.T, maintenance bool, operation oc.TransferOperation, kind oc.OwnerKind) oc.TransferAccessRequest {
	t.Helper()
	project := updateRootID[id.Project]()
	owner, err := oc.NewObjectOwner(kind, updateRootID[struct{}]().String(), project.String())
	if err != nil {
		t.Fatal(err)
	}
	objectID := updateRootID[oc.StoredObject]()
	spec, err := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: updateRootID[oc.Runner](), OperationID: updateRootID[oc.Operation](), Direction: oc.TransferGET, Owner: owner, ObjectID: objectID})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := id.NewAgentRun(project, updateRootID[id.Agent](), updateRootID[id.Execution]())
	if err != nil {
		t.Fatal(err)
	}
	transfer := updateRootID[oc.Transfer]()
	if maintenance {
		registration, e := id.RegisterService(id.ObjectMaintenance)
		if e != nil {
			t.Fatal(e)
		}
		actor, err = registration.Actor(transfer.String(), owner.Scope())
		if err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := oc.NewTransferManifest(oc.TransferManifestDetails{MediaType: "application/zip", Length: 1, SHA256: f.Digest("sha256:" + strings.Repeat("a", 64))})
	if err != nil {
		t.Fatal(err)
	}
	d := oc.TransferAccessDetails{Actor: actor, Operation: operation, ID: transfer, ObjectID: objectID, Spec: spec, Manifest: manifest, LeaseID: updateRootID[oc.Lease](), IssueCommand: f.CommandMeta{RequestID: updateRootID[f.Request](), IdempotencyKey: "original-prepare"}}
	if operation != oc.TransferIssue {
		d.Version = 2
	}
	if operation == oc.TransferCleanup {
		d.CleanupCause = updateRootID[oc.CleanupOperation]()
	}
	if operation == oc.TransferConfirmTerminal {
		d.Evidence = &oc.TransferEvidenceRef{ID: updateRootID[oc.TransferEvidence](), Kind: oc.TransferStoppedEvidence}
	}
	r, err := oc.NewTransferAccessRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSkillPrepareTransferRouterKeepsProviderFailureAndPrivateProof(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "original")
	tx := f.NewTx()
	denied := errors.New("selected provider refusal")
	prep := &skillTransferRouteProbe{ctx: ctx, tx: tx, err: denied}
	maintenance := &skillTransferRouteProbe{ctx: ctx, tx: tx, err: denied}
	a := &skillPrepareTransferAuthority{preparation: prep, maintenance: maintenance}
	for _, op := range []oc.TransferOperation{oc.TransferCleanup, oc.TransferConfirmTerminal} {
		r := skillTransferRouteRequest(t, true, op, oc.SkillRevision)
		if _, err := a.Discover(ctx, r); err != denied {
			t.Fatal("selected failure replaced", err)
		}
		if _, err := a.ValidateInTx(ctx, tx, r, oc.AccessDependencies{}); err != denied {
			t.Fatal("selected validation replaced", err)
		}
	}
	if len(prep.calls) != 0 || len(maintenance.calls) != 4 {
		t.Fatal("maintenance fell back to preparation")
	}
	for _, r := range []oc.TransferAccessRequest{skillTransferRouteRequest(t, true, oc.TransferIssue, oc.SkillRevision), skillTransferRouteRequest(t, true, oc.TransferInspect, oc.SkillRevision), skillTransferRouteRequest(t, true, oc.TransferCleanup, oc.Knowledge)} {
		if _, err := a.Discover(ctx, r); err == nil {
			t.Fatal("technical identity widened transfer scope")
		}
	}
	if len(prep.calls) != 0 || len(maintenance.calls) != 4 {
		t.Fatal("unsupported scope reached a provider")
	}
	if _, err := a.Discover(ctx, skillTransferRouteRequest(t, false, oc.TransferIssue, oc.SkillRevision)); err != denied || len(prep.calls) != 1 {
		t.Fatal("ordinary preparation changed", err)
	}
	real, err := object.NewTransferMaintenanceAuthority(&rootDispatchStore{})
	if err != nil {
		t.Fatal(err)
	}
	a.maintenance = real
	if _, err := a.Discover(ctx, skillTransferRouteRequest(t, true, oc.TransferCleanup, oc.SkillRevision)); err == nil || len(prep.calls) != 1 {
		t.Fatal("public technical DTO authorized recovery or fell back")
	}
}

func TestSkillPrepareTransferRouterLeaseAndReadOnlyPorts(t *testing.T) {
	ctx := context.Background()
	tx := f.NewTx()
	denied := errors.New("original refusal")
	prep := &skillTransferRouteProbe{ctx: ctx, tx: tx, err: denied}
	maintenance := &skillTransferRouteProbe{ctx: ctx, tx: tx, err: denied}
	a := &skillPrepareTransferAuthority{preparation: prep, maintenance: maintenance}
	r := skillTransferRouteRequest(t, true, oc.TransferCleanup, oc.SkillRevision)
	owner, err := oc.NewLeaseOwner(oc.TransferOwner, r.Details().ID.String())
	if err != nil {
		t.Fatal(err)
	}
	lease, err := oc.NewLeaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseUseAccess, Actor: r.Details().Actor, ObjectID: r.Details().ObjectID, LeaseOwner: owner})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.DiscoverPreparationLease(ctx, lease); err != denied {
		t.Fatal(err)
	}
	if err = a.ValidatePreparationLeaseInTx(ctx, tx, lease, oc.AccessDependencies{}); err != denied {
		t.Fatal(err)
	}
	if err = a.AuthorizeLeaseInTx(ctx, tx, r.Details().Actor, r.Details().ObjectID, owner, oc.ReleaseLease); err != denied {
		t.Fatal(err)
	}
	if len(prep.calls) != 0 || len(maintenance.calls) != 3 {
		t.Fatal("lease changed provider")
	}
	if _, err = a.DiscoverTransferObservation(ctx, oc.TransferObservationRequest{}); err != denied {
		t.Fatal(err)
	}
	if _, err = a.ValidateTransferObservationInTx(ctx, tx, oc.TransferObservationRequest{}, oc.AccessDependencies{}); err != denied {
		t.Fatal(err)
	}
	if _, err = a.DiscoverSkillTransferRetirement(ctx, oc.SkillTransferRetirementRequest{}); err != denied {
		t.Fatal(err)
	}
	if err = a.ValidateSkillTransferRetirementInTx(ctx, tx, oc.SkillTransferRetirementRequest{}, oc.AccessDependencies{}); err != denied {
		t.Fatal(err)
	}
	if len(prep.calls) != 4 || len(maintenance.calls) != 3 {
		t.Fatal("read-only port changed authority")
	}
}
