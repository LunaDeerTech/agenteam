package object

import (
	"context"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func observationIdentity(t *testing.T) (transferRow, oc.TransferAccessDetails, oc.TransferObservationFacts) {
	t.Helper()
	project, _ := f.NewID[id.Project]()
	agent, _ := f.NewID[id.Agent]()
	execution, _ := f.NewID[id.Execution]()
	actor, _ := id.NewAgentRun(project, agent, execution)
	object, _ := f.NewID[oc.StoredObject]()
	revision, _ := f.NewID[struct{}]()
	owner, _ := oc.NewObjectOwner(oc.SkillRevision, revision.String(), project.String())
	runner, _ := f.NewID[oc.Runner]()
	operation, _ := f.NewID[oc.Operation]()
	spec, _ := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: runner, OperationID: operation, Direction: oc.TransferGET, Owner: owner, ObjectID: object})
	request, _ := f.NewID[f.Request]()
	transfer, _ := f.NewID[oc.Transfer]()
	lease, _ := f.NewID[oc.Lease]()
	command := f.CommandMeta{RequestID: request, IdempotencyKey: "original.prepare"}
	manifest, _ := oc.NewTransferManifest(oc.TransferManifestDetails{MediaType: "application/zip", Length: 10, SHA256: f.Digest("sha256:" + strings.Repeat("a", 64))})
	d := oc.TransferAccessDetails{Operation: oc.TransferIssue, Actor: actor, IssueCommand: command, Spec: spec, ID: transfer, ObjectID: object, LeaseID: lease, Manifest: manifest}
	semantic, err := transferSemantic(actor, command, spec)
	if err != nil {
		t.Fatal(err)
	}
	r := transferRow{id: transfer, object: object, actor: actor, stable: stableActor(actor), issue: command, spec: spec, manifest: manifest, digest: semantic, runnerGeneration: 2, operationVersion: 2, execution: execution}
	r.lease, _ = f.NewID[oc.Lease]() // GET provider determines its actual lease inside Issue Tx.
	return r, d, oc.TransferObservationFacts{RunnerGeneration: 2, OperationVersion: 2, ExecutionID: execution}
}
func TestTransferObservationOriginalIdentityAndActualLease(t *testing.T) {
	r, d, facts := observationIdentity(t)
	if err := requireObservedTransferIdentity(r, d, facts); err != nil {
		t.Fatal("preallocated Issue lease confused with actual lease", err)
	}
	for _, mutate := range []func(*transferRow){func(r *transferRow) { r.runnerGeneration++ }, func(r *transferRow) { r.operationVersion++ }, func(r *transferRow) { r.execution, _ = f.NewID[id.Execution]() }, func(r *transferRow) { r.id, _ = f.NewID[oc.Transfer]() }, func(r *transferRow) { r.object, _ = f.NewID[oc.StoredObject]() }, func(r *transferRow) { r.issue.RequestID, _ = f.NewID[f.Request]() }, func(r *transferRow) { r.digest = []byte("changed") }} {
		changed := r
		mutate(&changed)
		if requireObservedTransferIdentity(changed, d, facts) == nil {
			t.Fatal("changed durable original tuple accepted")
		}
	}
	d.Operation = oc.TransferCancel
	if requireObservedTransferIdentity(r, d, facts) == nil {
		t.Fatal("post-Issue command accepted another actual lease")
	}
	d.LeaseID = r.lease
	if err := requireObservedTransferIdentity(r, d, facts); err != nil {
		t.Fatal(err)
	}
}
func TestTransferObservationMissingRowRequiresLeaseAndTransferAbsence(t *testing.T) {
	_, d, facts := observationIdentity(t)
	original, _ := oc.NewTransferAccessRequest(d)
	request, _ := oc.NewTransferObservationRequest(oc.TransferObservationDetails{Original: original})
	for _, exists := range []bool{false, true} {
		store := &auditTestStore{}
		store.row = func(query string, args ...any) postgres.Row {
			if strings.HasPrefix(query, "SELECT "+transferColumns) {
				return auditTestRow(func(...any) error { return pgx.ErrNoRows })
			}
			if strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_object.object_transfers") && len(args) == 1 && args[0] == d.ID.String() {
				return auditValues(exists)
			}
			t.Fatal("unexpected query")
			return nil
		}
		out, err := observeTransferCommandInTx(context.Background(), store, request, facts)
		if exists {
			if err == nil || out.SettledFor(request) {
				t.Fatal("orphaned owner/transfer invented absence")
			}
		} else if err != nil || !out.SettledFor(request) || out.Details().Status != nil {
			t.Fatal("validated same-lock absence rejected", err)
		}
	}
}
