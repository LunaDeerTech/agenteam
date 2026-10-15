package contract

import (
	"encoding/json"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func observationFixture(t *testing.T) TransferAccessDetails {
	t.Helper()
	_, spec, d := transferFixture(t)
	s := spec.Details()
	s.Direction, s.ObjectID, s.UploadCommand, s.Manifest = TransferGET, d.ObjectID, nil, TransferManifest{}
	d.Spec, _ = NewTransferSpec(s)
	d.Operation, d.Version, d.Evidence = TransferIssue, 0, nil
	d.UploadID, d.StagingID = UploadID{}, AttemptID{}
	return d
}
func TestTransferObservationNeverInfersAbsenceFromZero(t *testing.T) {
	d := observationFixture(t)
	original, err := NewTransferAccessRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewTransferObservationRequest(TransferObservationDetails{Original: original})
	if err != nil {
		t.Fatal(err)
	}
	if (TransferCommandObservation{}).SettledFor(r) {
		t.Fatal("zero observation invented settled absence")
	}
	observed, err := NewTransferCommandObservation(TransferCommandObservationDetails{Request: r})
	if err != nil || !observed.SettledFor(r) || observed.Details().Status != nil || observed.Details().Applied {
		t.Fatal("issued settled absence", err)
	}
	d.IssueCommand.IdempotencyKey = "different.original"
	other, _ := NewTransferAccessRequest(d)
	changed, _ := NewTransferObservationRequest(TransferObservationDetails{Original: other})
	if observed.SettledFor(changed) {
		t.Fatal("observation rebound to another command")
	}
	if json.Unmarshal([]byte(`{}`), &r) == nil {
		t.Fatal("wire minted private observation request")
	}
}
func TestTransferObservationClosedOperationsAndStatusCopy(t *testing.T) {
	d := observationFixture(t)
	original, _ := NewTransferAccessRequest(d)
	r, _ := NewTransferObservationRequest(TransferObservationDetails{Original: original})
	expires, _ := f.NewInstant(time.Unix(2000000000, 0).UTC())
	status := TransferStatusView{ID: d.ID, ObjectID: d.ObjectID, RunnerID: d.Spec.Details().RunnerID, OperationID: d.Spec.Details().OperationID, Direction: TransferGET, Version: 1, ExpiresAt: expires, LeaseActive: true, State: TransferPending}
	obs, err := NewTransferCommandObservation(TransferCommandObservationDetails{Request: r, Applied: true, Status: &status})
	if err != nil {
		t.Fatal(err)
	}
	status.LeaseActive = false
	copy := obs.Details()
	copy.Status.LeaseActive = false
	if !obs.Details().Status.LeaseActive {
		t.Fatal("caller mutated observation")
	}
	for _, op := range []TransferOperation{TransferInspect, TransferMaterialize, TransferCleanup} {
		d.Operation, d.Version = op, 1
		if op == TransferCleanup {
			d.CleanupCause, _ = f.NewID[CleanupOperation]()
		}
		original, err = NewTransferAccessRequest(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = NewTransferObservationRequest(TransferObservationDetails{Original: original}); err == nil {
			t.Fatal("unsupported observation operation")
		}
	}
	d = observationFixture(t)
	d.Operation, d.Version = TransferConfirmTerminal, 1
	evidence, _ := f.NewID[TransferEvidence]()
	d.Evidence = &TransferEvidenceRef{ID: evidence, Kind: TransferStoppedEvidence}
	original, _ = NewTransferAccessRequest(d)
	if _, err = NewTransferObservationRequest(TransferObservationDetails{Original: original}); err == nil {
		t.Fatal("missing exact retirement digest")
	}
}
