package object

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type maintenanceTransferStore struct {
	*auditTestStore
	held []f.LockRequest
}

func (s *maintenanceTransferStore) RequireHeldLocks(ctx context.Context, tx f.Tx, want []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	for _, w := range want {
		if !slices.ContainsFunc(s.held, func(h f.LockRequest) bool {
			return h.Key.Canonical() == w.Key.Canonical() && (h.Mode == f.Exclusive || h.Mode == w.Mode)
		}) {
			return failure(f.Forbidden, nil)
		}
	}
	return ctx.Err()
}

type maintenanceTransferGuard struct {
	process oc.ProcessID
	tx      f.Tx
	calls   int
}

func (*maintenanceTransferGuard) ConfirmStopped(context.Context, oc.ProcessID) error {
	return errors.New("retirement must not substitute a process death check")
}
func (g *maintenanceTransferGuard) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, process oc.ProcessID) error {
	g.calls++
	if tx != g.tx || process != g.process {
		return failure(f.Forbidden, nil)
	}
	return ctx.Err()
}

type maintenanceTransferFixture struct {
	store   *maintenanceTransferStore
	guard   *maintenanceTransferGuard
	a       *TransferMaintenanceAuthority
	t       *TransferService
	row     transferRow
	actor   id.Actor
	ctx     context.Context
	finish  func()
	request oc.TransferAccessRequest
	plan    oc.AccessDependencies
	lease   bool
}

func maintenanceTransferValues(r transferRow) postgres.Row {
	d, m := r.spec.Details(), r.manifest.Details()
	actor, _ := json.Marshal(r.actor.Details())
	var expected *int64
	if r.issue.ExpectedVersion != nil {
		v := int64(*r.issue.ExpectedVersion)
		expected = &v
	}
	completed := ""
	if r.completed.Validate() == nil {
		completed = r.completed.String()
	}
	return auditValues(r.id.String(), actor, r.stable, r.issue.IdempotencyKey, r.issue.RequestID.String(), expected, r.digest,
		d.Owner.Details().ProjectID, d.Owner.Details().Kind, d.Owner.Details().ID, d.RunnerID.String(), d.OperationID.String(), int64(r.runnerGeneration), int64(r.operationVersion), r.execution.String(), d.Direction, r.object.String(), "", "", "", nil, "", "", r.lease.String(), "", "", m.MediaType, m.Length, digestBytes(m.SHA256), r.key, d.ExpiresInSeconds, r.expires, r.phase, int64(r.version), r.revoked, completed, r.completedDigest, r.retirement.String(), r.retirementDigest, r.retirementKind, r.pendingRetirement.String(), r.pendingKind, r.cleanup, r.leaseActive, r.stagingCleaned)
}

func newMaintenanceTransferFixture(t *testing.T) *maintenanceTransferFixture {
	t.Helper()
	r, _, _ := observationIdentity(t)
	r.version, r.phase, r.leaseActive = 3, "complete", true
	r.expires = time.Now().UTC().Truncate(time.Second)
	r.retirement = auditID[oc.TransferEvidence](t)
	r.retirementKind = oc.TransferStoppedEvidence
	hash := sha256.Sum256([]byte("actual returned requests and closed admission"))
	r.retirementDigest = hash[:]
	r.pendingRetirement, r.pendingKind = r.retirement, r.retirementKind
	v := &maintenanceTransferFixture{store: &maintenanceTransferStore{auditTestStore: &auditTestStore{tx: f.NewTx()}}, row: r, lease: true}
	v.guard = &maintenanceTransferGuard{process: auditID[oc.Process](t), tx: v.store.tx}
	state := &serviceState{store: v.store, process: v.guard.process, auth: Authorizations{Processes: v.guard}}
	service := &Service{data: func() *serviceState { return state }}
	transfer := &transferState{objects: service}
	v.t = &TransferService{data: func() *transferState { return transfer }}
	var err error
	v.a, err = NewTransferMaintenanceAuthority(v.store)
	if err != nil {
		t.Fatal(err)
	}
	registration, _ := id.RegisterService(id.ObjectMaintenance)
	v.actor, _ = registration.Actor(r.id.String(), r.spec.Details().Owner.Scope())
	v.ctx, v.finish = v.t.transferMaintenanceContext(context.Background(), v.actor, r)
	t.Cleanup(v.finish)
	request, err := r.request(v.actor, oc.TransferCleanup, nil, nil, oc.PreparedPayload{})
	if err != nil {
		t.Fatal(err)
	}
	v.request = request.Details().Transfer
	v.store.row = func(query string, _ ...any) postgres.Row {
		switch {
		case strings.HasPrefix(query, "SELECT "+transferColumns):
			return maintenanceTransferValues(v.row)
		case strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE id="):
			return auditValues(v.lease)
		default:
			t.Fatal("unexpected maintenance query")
			return nil
		}
	}
	v.plan, err = v.a.Discover(v.ctx, v.request)
	if err != nil {
		t.Fatal(err)
	}
	v.store.held = v.plan.Locks()
	return v
}

func TestSkillPrepareTransferMaintenancePrivateOwner(t *testing.T) {
	v := newMaintenanceTransferFixture(t)
	if _, err := v.a.Discover(context.Background(), v.request); err == nil {
		t.Fatal("bare technical DTO authorized maintenance")
	}
	for _, operation := range []oc.TransferOperation{oc.TransferInspect, oc.TransferMaterialize, oc.TransferIssue, oc.TransferCapture} {
		var evidence *oc.TransferEvidenceRef
		if operation == oc.TransferCapture {
			evidence = &oc.TransferEvidenceRef{ID: v.row.retirement, Kind: oc.TransferCompletedEvidence}
		}
		r, err := v.row.request(v.actor, operation, evidence, nil, oc.PreparedPayload{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = v.a.Discover(v.ctx, r.Details().Transfer); err == nil {
			t.Fatal("maintenance admitted non-retirement access", operation)
		}
	}
	if _, err := v.a.ValidateInTx(v.ctx, v.store.tx, v.request, v.plan); err != nil {
		t.Fatal(err)
	}
	if v.guard.calls != 1 {
		t.Fatal("missing original current guard")
	}
	owner, _ := oc.NewLeaseOwner(oc.TransferOwner, v.row.id.String())
	if err := v.a.AuthorizeLeaseInTx(v.ctx, v.store.tx, v.actor, v.row.object, owner, oc.AcquireLease); err == nil {
		t.Fatal("maintenance acquired a new lease")
	}
	if err := v.a.AuthorizeLeaseInTx(v.ctx, v.store.tx, v.actor, v.row.object, owner, oc.ReleaseLease); err != nil {
		t.Fatal(err)
	}
	v.finish()
	if _, err := v.a.Discover(v.ctx, v.request); err == nil {
		t.Fatal("expired Recover callback still authorized")
	}
}

func TestSkillPrepareTransferMaintenanceExactCheckpoint(t *testing.T) {
	for _, name := range []string{"version", "retirement", "lease", "process", "locks", "pending"} {
		t.Run(name, func(t *testing.T) {
			v := newMaintenanceTransferFixture(t)
			evidence := &oc.TransferEvidenceRef{ID: v.row.retirement, Kind: v.row.retirementKind}
			r, err := v.row.request(v.actor, oc.TransferConfirmTerminal, evidence, nil, oc.PreparedPayload{})
			if err != nil {
				t.Fatal(err)
			}
			request := r.Details().Transfer
			plan, err := v.a.Discover(v.ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := v.a.ValidateInTx(v.ctx, v.store.tx, request, plan)
			if err != nil || proof.Details().Retirement == nil || proof.Details().Retirement.Digest != newDigest(v.row.retirementDigest) {
				t.Fatal("persisted validated proof not preserved", err)
			}
			switch name {
			case "version":
				v.row.version++
			case "retirement":
				v.row.retirementDigest = make([]byte, sha256.Size)
			case "lease":
				v.lease = false
			case "process":
				v.guard.process = auditID[oc.Process](t)
			case "locks":
				v.store.held = nil
			case "pending":
				w, _ := v.ctx.Value(transferMaintenanceKey{}).(*transferMaintenanceWitness)
				w.row.retirement, w.row.retirementDigest = oc.EvidenceID{}, nil
			}
			if _, err = v.a.ValidateInTx(v.ctx, v.store.tx, request, plan); err == nil {
				t.Fatal("changed original checkpoint accepted", name)
			}
		})
	}
}
