package skill

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type prepareTransferControlled struct {
	oc.Transfers
	status                            oc.TransferStatusView
	cancelErr, confirmErr, observeErr error
	inspect, cancel, confirm, lookups int
	applied, absent, confirmActive    bool
	original                          oc.TransferObservationRequest
}

func (c *prepareTransferControlled) InspectTransfer(context.Context, id.Actor, oc.TransferID) (oc.TransferStatusView, error) {
	c.inspect++
	return c.status, nil
}
func (c *prepareTransferControlled) CancelTransfer(context.Context, id.Actor, oc.TransferID, f.CommandMeta) (oc.TransferStatusView, error) {
	c.cancel++
	return c.status, c.cancelErr
}
func (c *prepareTransferControlled) ConfirmStopped(context.Context, id.Actor, oc.TransferID, oc.TransferEvidenceRef) (oc.TransferStatusView, error) {
	c.confirm++
	out := c.status
	out.LeaseActive = c.confirmActive
	return out, c.confirmErr
}
func (c *prepareTransferControlled) LookupTransferCommand(_ context.Context, r oc.TransferObservationRequest) (oc.TransferCommandObservation, error) {
	c.lookups++
	c.original = r
	if c.observeErr != nil {
		return oc.TransferCommandObservation{}, c.observeErr
	}
	out := oc.TransferCommandObservationDetails{Request: r, Applied: c.applied}
	if !c.absent {
		status := c.status
		if r.Details().Original.Details().Operation == oc.TransferConfirmTerminal && c.applied {
			status.LeaseActive = false
		}
		out.Status = &status
	}
	return oc.NewTransferCommandObservation(out)
}
func prepareCleanupCommandFixture(t *testing.T) (*Preparer, *preparationWitness, *prepareTransferControlled) {
	t.Helper()
	project, agent, execution := stateID[id.Project](151), stateID[id.Agent](152), stateID[id.Execution](153)
	actor, _ := id.NewAgentRun(project, agent, execution)
	w := &preparationWitness{invocation: sc.PreparationInvocation{Invocation: sc.Invocation{Actor: actor, ProjectID: project, AgentID: agent, ExecutionID: execution, OperationID: stateID[struct{}](154).String(), AttemptID: stateID[struct{}](155).String()}, RequestID: p.ID(stateID[struct{}](156).String())}, transfer: stateID[oc.Transfer](157), binding: sc.SkillBinding{SkillID: stateID[pc.Skill](158), RevisionID: stateID[sc.Revision](159), Revision: 1}, args: sc.PrepareToolArguments{MountID: stateID[id.Mount](160)}, metadata: sc.RevisionMetadata{Object: oc.ObjectMeta{ID: stateID[oc.StoredObject](161)}}}
	w.command = f.CommandMeta{RequestID: stateID[f.Request](162), IdempotencyKey: "original.prepare"}
	w.cancelCommand = f.CommandMeta{RequestID: w.command.RequestID, IdempotencyKey: "original.prepare.stop"}
	owner, _ := oc.NewObjectOwner(oc.SkillRevision, w.binding.RevisionID.String(), project.String())
	spec, _ := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: stateID[oc.Runner](163), OperationID: stateID[oc.Operation](154), Direction: oc.TransferGET, Owner: owner, ObjectID: w.metadata.Object.ID})
	manifest, _ := oc.NewTransferManifest(oc.TransferManifestDetails{MediaType: "application/zip", Length: 10, SHA256: f.Digest("sha256:" + strings.Repeat("a", 64))})
	d := oc.TransferAccessDetails{Operation: oc.TransferIssue, Actor: actor, IssueCommand: w.command, Spec: spec, ID: w.transfer, ObjectID: w.metadata.Object.ID, LeaseID: stateID[oc.Lease](164), Manifest: manifest}
	var err error
	w.issueRequest, err = oc.NewTransferAccessRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	d.Operation, d.Version, d.CancelCommand = oc.TransferCancel, 1, &w.cancelCommand
	w.cancelRequest, err = oc.NewTransferAccessRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	w.retirement = oc.TransferEvidenceRef{ID: stateID[oc.TransferEvidence](165), Kind: oc.TransferStoppedEvidence}
	d.Operation, d.CancelCommand, d.Evidence = oc.TransferConfirmTerminal, nil, &w.retirement
	w.stopRequest, err = oc.NewTransferAccessRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	expires, _ := f.NewInstant(time.Unix(2000000000, 0).UTC())
	c := &prepareTransferControlled{status: oc.TransferStatusView{ID: w.transfer, ObjectID: w.metadata.Object.ID, OperationID: spec.Details().OperationID, RunnerID: spec.Details().RunnerID, Direction: oc.TransferGET, Version: 1, ExpiresAt: expires, State: oc.TransferPending, LeaseActive: true}}
	s := &preparerState{transfers: c, observer: c}
	return &Preparer{data: func() *preparerState { return s }}, w, c
}
func TestSkillPrepareCancelUnknownOnlyObservesOriginal(t *testing.T) {
	s, w, c := prepareCleanupCommandFixture(t)
	root := errors.New("original cancellation ambiguity")
	original := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(root)
	c.cancelErr = original
	if err := s.retirePreparationTransfer(context.Background(), w); !errors.Is(err, root) {
		t.Fatal("lost original cancel", err)
	}
	for _, observationErr := range []error{context.Canceled, nil} {
		c.observeErr = observationErr
		if err := s.retirePreparationTransfer(context.Background(), w); !errors.Is(err, root) {
			t.Fatal("failed/unapplied lookup replaced original Unknown", err)
		}
		if c.cancel != 1 || c.confirm != 0 || c.inspect != 1 {
			t.Fatal("Unknown repeated a mutation or inferred retirement")
		}
	}
	c.applied = true
	if err := s.retirePreparationTransfer(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if c.cancel != 1 || c.confirm != 1 || !c.original.Details().Original.Equal(w.cancelRequest) {
		t.Fatal("confirmed cancel did not use exact original command")
	}
}
func TestSkillPrepareConfirmUnknownNeedsExactAppliedReceipt(t *testing.T) {
	s, w, c := prepareCleanupCommandFixture(t)
	w.completed = true
	root := errors.New("original retirement ambiguity")
	c.confirmErr = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(root)
	if err := s.retirePreparationTransfer(context.Background(), w); !errors.Is(err, root) {
		t.Fatal(err)
	}
	for _, observationErr := range []error{context.Canceled, nil} {
		c.observeErr = observationErr
		if err := s.retirePreparationTransfer(context.Background(), w); !errors.Is(err, root) {
			t.Fatal("retirement observation lost Unknown", err)
		}
	}
	c.applied = true
	if err := s.retirePreparationTransfer(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if c.confirm != 1 || c.cancel != 0 || c.inspect != 1 || c.original.Details().EvidenceDigest != w.retirementDigest {
		t.Fatal("retirement repeated or rebound")
	}
}
func TestSkillPrepareIssueAbsenceUsesFormalOriginalObservation(t *testing.T) {
	s, w, c := prepareCleanupCommandFixture(t)
	w.issueError = f.NewFault(f.CommitUnknown, f.Unknown)
	w.lookupNeeded = true
	c.absent = true
	if err := s.retirePreparationTransfer(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	r, err := sc.DecodePrepareUnissuedResult(w.resultBody)
	if err != nil || r.SkillID != w.binding.SkillID || r.MayHavePrepared || c.inspect+c.cancel+c.confirm != 0 || c.lookups != 1 {
		t.Fatal("settled absence invented a prepared package or mutation", err)
	}
	if _, err = sc.DecodePrepareUnknownResult(w.resultBody); err == nil {
		t.Fatal("unissued failure became uncertain remote effect")
	}
}

func TestSkillPrepareConfirmedActiveLeaseOnlyObservesOriginal(t *testing.T) {
	s, w, c := prepareCleanupCommandFixture(t)
	w.completed = true
	c.confirmActive = true
	for i := 0; i < 2; i++ {
		err := s.retirePreparationTransfer(context.Background(), w)
		var value *f.Fault
		if !errors.As(err, &value) || value.Code != f.ResourceBusy || value.CommitState == f.Unknown {
			t.Fatal("known pending retirement changed to an unknown outcome", err)
		}
	}
	c.applied = true
	if err := s.retirePreparationTransfer(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if c.inspect != 1 || c.cancel != 0 || c.confirm != 1 || c.lookups != 2 || !c.original.Details().Original.Equal(w.stopRequest) {
		t.Fatal("pending committed confirmation was repeated or rebound")
	}
}

func TestSkillPreparePostCommitStatusErrorCannotRepeatCleanup(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		s, w, c := prepareCleanupCommandFixture(t)
		root := errors.New("status read after commit")
		readError := f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(root)
		if confirm {
			w.completed = true
			c.confirmErr = readError
		} else {
			c.cancelErr = readError
		}
		err := s.retirePreparationTransfer(context.Background(), w)
		var faultValue *f.Fault
		if !errors.As(err, &faultValue) || faultValue.CommitState != f.Unknown || !errors.Is(err, root) {
			t.Fatal("post-commit read was treated as no write", err)
		}
		c.applied = true
		if err = s.retirePreparationTransfer(context.Background(), w); err != nil {
			t.Fatal(err)
		}
		if c.confirm != 1 || c.lookups != 1 || (!confirm && c.cancel != 1) || (confirm && c.cancel != 0) {
			t.Fatal("cleanup repeated after status-read failure")
		}
	}
}
