package object

import (
	"context"
	"crypto/sha256"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func skillRetirementFixture(t *testing.T) (transferRow, oc.SkillTransferRetirementRequest) {
	t.Helper()
	r, _, _ := observationIdentity(t)
	version := r.operationVersion
	r.issue.ExpectedVersion = &version
	r.issue.IdempotencyKey = f.IdempotencyKey("skill-prepare-" + r.spec.Details().OperationID.String())
	var err error
	r.digest, err = transferSemantic(r.actor, r.issue, r.spec)
	if err != nil {
		t.Fatal(err)
	}
	r.retirement, r.retirementKind = auditID[oc.TransferEvidence](t), oc.TransferStoppedEvidence
	digest := sha256.Sum256([]byte("original requests joined"))
	r.retirementDigest = digest[:]
	r.leaseActive = false
	request, err := oc.NewSkillTransferRetirementRequest(oc.SkillTransferRetirementDetails{Actor: r.actor, OldProcess: auditID[oc.Process](t), Operation: r.spec.Details().OperationID, Command: r.issue, Owner: r.spec.Details().Owner, Object: r.object, Manifest: r.manifest})
	if err != nil {
		t.Fatal(err)
	}
	return r, request
}

func TestSkillPrepareColdTransferRetirementExactOriginal(t *testing.T) {
	r, request := skillRetirementFixture(t)
	if err := requireSkillTransferRetirementIdentity(r, request.Details()); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*transferRow){
		func(r *transferRow) { r.execution = auditID[id.Execution](t) },
		func(r *transferRow) { r.issue.RequestID = auditID[f.Request](t) },
		func(r *transferRow) { r.operationVersion++ },
		func(r *transferRow) { r.object = auditID[oc.StoredObject](t) },
		func(r *transferRow) { r.digest = make([]byte, sha256.Size) },
		func(r *transferRow) {
			d := r.spec.Details()
			d.OperationID = auditID[oc.Operation](t)
			r.spec, _ = oc.NewTransferSpec(d)
		},
	} {
		changed := r
		mutate(&changed)
		if requireSkillTransferRetirementIdentity(changed, request.Details()) == nil {
			t.Fatal("changed original transfer identity accepted")
		}
	}
}

func TestSkillPrepareColdTransferRetirementNeedsStoredProofAndLease(t *testing.T) {
	r, request := skillRetirementFixture(t)
	store := &auditTestStore{}
	lease := true
	store.row = func(_ string, args ...any) postgres.Row {
		if len(args) != 3 || args[0] != r.lease.String() || args[1] != r.object.String() || args[2] != r.id.String() {
			t.Fatal("retirement lost original lease tuple")
		}
		return auditValues(lease)
	}
	result, err := observeSkillTransferRetirement(context.Background(), store, request, r)
	if err != nil || !result.ClosedFor(request) || result.Details().EvidenceDigest != newDigest(r.retirementDigest) {
		t.Fatal("original closed proof rejected", err)
	}
	for _, name := range []string{"active", "pending-only", "missing-digest", "missing-lease"} {
		t.Run(name, func(t *testing.T) {
			changed := r
			lease = true
			switch name {
			case "active":
				changed.leaseActive = true
			case "pending-only":
				changed.pendingRetirement = changed.retirement
				changed.retirement = oc.EvidenceID{}
			case "missing-digest":
				changed.retirementDigest = nil
			case "missing-lease":
				lease = false
			}
			out, err := observeSkillTransferRetirement(context.Background(), store, request, changed)
			if err == nil || out.ClosedFor(request) {
				t.Fatal("unfinished retirement reported closed")
			}
		})
	}
}
