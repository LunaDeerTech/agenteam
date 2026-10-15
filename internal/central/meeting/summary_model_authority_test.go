package meeting

import (
	"context"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func summaryTestID[T any](t *testing.T) f.ID[T] {
	t.Helper()
	id, err := f.NewID[T]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// This checks the private protocol witness only. The App integration test
// exercises the separate persisted mapping and actual ProcessGuard gates.
func summaryOwnerFixture(t *testing.T) (*SummaryAuthority, *summaryOwner, context.Context, mc.ConsumerRequest) {
	t.Helper()
	a := &SummaryAuthority{process: summaryTestID[oc.Process](t)}
	w := &summaryOwner{authority: a, active: true, call: summaryTestID[mc.Call](t), input: SummaryInput{
		ProjectID: summaryTestID[i.Project](t), MeetingID: summaryTestID[c.Meeting](t), GenerationID: summaryTestID[c.SummaryGeneration](t), Initial: true,
	}}
	scope, err := i.InProject(w.input.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	registration, err := i.RegisterService(i.MeetingRuntime)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(w.input.GenerationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, w.call.String())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, lease := summaryTestID[mc.Snapshot](t), summaryTestID[sc.Lease](t)
	if err = w.bindResolved(mc.ResolvedModel{Snapshot: mc.ConfigSnapshot{ID: snapshot}, LeaseOwner: owner, CredentialLease: &sc.CredentialLease{LeaseID: lease}}); err != nil {
		t.Fatal(err)
	}
	r := mc.ConsumerRequest{Action: mc.ReadCredentialConsumer, Actor: actor,
		Consumer:   mc.Consumer{Kind: mc.MeetingConsumer, ProjectID: w.input.ProjectID, MeetingID: w.input.MeetingID.String(), OperationID: w.input.GenerationID.String(), Purpose: mc.MeetingSummaryInitial},
		SnapshotID: snapshot, LeaseOwner: owner, LeaseID: &lease, CallID: &w.call,
		Input:   &mc.InputIdentity{OperationID: w.input.GenerationID.String(), Digest: f.Digest("sha256:" + strings.Repeat("1", 64)), SchemaVersion: 1},
		Attempt: &mc.AttemptIdentity{CallID: w.call, InvocationID: summaryTestID[mc.Invocation](t), AttemptIndex: 1, ProcessID: a.process, Fence: 1},
	}
	if err = r.Validate(); err != nil {
		t.Fatal(err)
	}
	return a, w, context.WithValue(context.Background(), summaryOwnerKey{}, w), r
}

func summaryTechnicalActor(t *testing.T, r mc.ConsumerRequest, invocation mc.InvocationID) i.Actor {
	t.Helper()
	registration, err := i.RegisterService(i.ModelRuntime)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := i.InProject(r.Consumer.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(invocation.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	return actor
}

func TestSummaryAuthorityFinalizationCanBindOriginalAttemptBeforeCredentialRead(t *testing.T) {
	a, w, ctx, request := summaryOwnerFixture(t)
	request.Action = mc.FinalizeConsumer
	request.Actor = summaryTechnicalActor(t, request, request.Attempt.InvocationID)
	if _, err := a.owner(ctx, request); err != nil {
		t.Fatal("original finalization actor rejected", err)
	}
	if err := w.requireCallFacts(request); err != nil {
		t.Fatal("pre-read failure could not bind original attempt", err)
	}
	retire := request.Clone()
	retire.Action, retire.Attempt, retire.Input = mc.RetireConsumer, nil, nil
	if _, err := a.owner(ctx, retire); err != nil {
		t.Fatal("original runtime retirement actor rejected", err)
	}
	if err := w.requireCallFacts(retire); err != nil {
		t.Fatal("runtime retirement required a domain terminal version", err)
	}
}

func TestSummaryAuthorityRejectsForeignTechnicalFacts(t *testing.T) {
	for _, change := range []string{"context", "actor", "invocation", "process", "lease", "snapshot", "call", "attempt", "fence", "retire-before-attempt"} {
		t.Run(change, func(t *testing.T) {
			a, w, ctx, request := summaryOwnerFixture(t)
			request.Action = mc.FinalizeConsumer
			request.Actor = summaryTechnicalActor(t, request, request.Attempt.InvocationID)
			if change != "retire-before-attempt" {
				if err := w.requireCallFacts(request); err != nil {
					t.Fatal(err)
				}
			}
			switch change {
			case "context":
				ctx = context.Background()
			case "actor":
				registration, _ := i.RegisterService(i.MeetingRuntime)
				scope, _ := i.InProject(request.Consumer.ProjectID)
				request.Actor, _ = registration.Actor(w.input.GenerationID.String(), scope)
			case "invocation":
				request.Attempt.InvocationID = summaryTestID[mc.Invocation](t)
				request.Actor = summaryTechnicalActor(t, request, request.Attempt.InvocationID)
			case "process":
				request.Attempt.ProcessID = summaryTestID[oc.Process](t)
			case "lease":
				lease := summaryTestID[sc.Lease](t)
				request.LeaseID = &lease
			case "snapshot":
				request.SnapshotID = summaryTestID[mc.Snapshot](t)
			case "call":
				request.Attempt.CallID = summaryTestID[mc.Call](t)
			case "attempt":
				request.Attempt.AttemptIndex++
			case "fence":
				request.Attempt.Fence++
			case "retire-before-attempt":
				request.Action, request.Input, request.Attempt = mc.RetireConsumer, nil, nil
			}
			if _, err := a.owner(ctx, request); err == nil {
				if err = w.requireCallFacts(request); err == nil {
					t.Fatal("foreign technical facts accepted")
				}
			}
		})
	}
}
