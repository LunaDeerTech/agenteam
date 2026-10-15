package model

import (
	"context"
	"errors"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func stoppedResolutionPureRequest(t *testing.T) mc.StoppedResolutionRequest {
	t.Helper()
	r := meetingResolutionTestRequest(t)
	call, err := f.ParseID[mc.Call](r.LeaseOwner.Details().ID)
	if err != nil {
		t.Fatal(err)
	}
	return mc.StoppedResolutionRequest{Resolve: r, CallID: call, Input: mc.InputIdentity{OperationID: r.Consumer.OperationID, Digest: hash([]byte("original input")), SchemaVersion: 1}, PreparedProcessID: mustID[oc.Process](t)}
}

type stoppedResolutionPureAuthority struct {
	mc.StoppedCallAuthority
	discover func(context.Context, mc.StoppedResolutionRequest) (mc.ConsumerDependencies, error)
}

func (a stoppedResolutionPureAuthority) DiscoverStoppedResolution(ctx context.Context, r mc.StoppedResolutionRequest) (mc.ConsumerDependencies, error) {
	return a.discover(ctx, r)
}
func (stoppedResolutionPureAuthority) ValidateStoppedResolutionInTx(context.Context, f.Tx, mc.StoppedResolutionRequest, mc.ConsumerDependencies) error {
	panic("unexpected observation transaction")
}

func TestStoppedResolutionRejectsUnboundOrUnrelatedPlan(t *testing.T) {
	request := stoppedResolutionPureRequest(t)
	a := runtimePureAuthority(t, &noIOStore{}, runtimeConsumerFunc(func(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
		panic("ordinary Resolve authority must not be used")
	}))
	r := runtimePureService(a)
	_, err := r.LookupStoppedResolution(context.Background(), request)
	requireCode(t, err, f.DependencyUnbound)
	a.state().auth.StoppedConsumers = stoppedResolutionPureAuthority{discover: func(context.Context, mc.StoppedResolutionRequest) (mc.ConsumerDependencies, error) {
		return mc.NewConsumerDependencies(mc.NewPlanIssuer(), mc.ConsumerDependencyDetails{Binding: hash([]byte("other request")), Mapping: hash([]byte("private mapping")), Locks: []f.LockRequest{projectLock(request.Resolve.Consumer.ProjectID.String())}})
	}}
	_, err = r.LookupStoppedResolution(context.Background(), request)
	requireCode(t, err, f.Forbidden)
	r.StopAdmission()
	if err := r.Drain(context.Background()); err != nil || !r.Joined() {
		t.Fatal("rejected observation retained admission", err)
	}
}

func TestStoppedResolutionDrainWaitsForOriginalObserver(t *testing.T) {
	request := stoppedResolutionPureRequest(t)
	a := runtimePureAuthority(t, &noIOStore{}, runtimeConsumerFunc(func(context.Context, mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
		panic("ordinary Resolve authority must not be used")
	}))
	r := runtimePureService(a)
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	a.state().auth.StoppedConsumers = stoppedResolutionPureAuthority{discover: func(ctx context.Context, _ mc.StoppedResolutionRequest) (mc.ConsumerDependencies, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return mc.ConsumerDependencies{}, ctx.Err()
	}}
	observed := make(chan error, 1)
	go func() { _, err := r.LookupStoppedResolution(context.Background(), request); observed <- err }()
	<-entered
	r.StopAdmission()
	<-cancelled
	if r.Joined() {
		t.Fatal("observer abandoned before actual return")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Drain(ctx); !errors.Is(err, context.Canceled) || r.Joined() {
		t.Fatal("Drain hid unfinished observation", err)
	}
	close(release)
	if err := <-observed; !errors.Is(err, context.Canceled) {
		t.Fatal("observation lost original cancellation", err)
	}
	if err := r.Drain(context.Background()); err != nil || !r.Joined() {
		t.Fatal("observer did not join", err)
	}
}

func TestStoppedResolutionChecksExactOriginalSourceAndCall(t *testing.T) {
	request := stoppedResolutionPureRequest(t)
	model := mustID[mc.Model](t)
	draftRequest := request.Resolve.Clone()
	draftRequest.ModelRef = &model
	draft := resolutionTestDraft(t, draftRequest)
	p := &resolutionPreparation{ID: mustID[struct{}](t).String(), Request: resolutionRequest(request.Resolve), Semantic: resolutionSemantic(request.Resolve), SnapshotID: draft.Snapshot.ID, Phase: "committed", Draft: draft}
	if err := stoppedResolutionMatches(request, p, nil); err != nil {
		t.Fatal(err)
	}
	changed := request.Clone()
	version := f.Version(1)
	changed.Resolve.Selection.Version = &version
	if err := stoppedResolutionMatches(changed, p, nil); err == nil {
		t.Fatal("returned/current selector version replaced original nil")
	}
	call := stoppedPureRecord(t)
	call.binding.CallID, call.binding.Consumer, call.binding.Input = request.CallID, request.Resolve.Consumer.Clone(), request.Input.Clone()
	call.binding.Initiator, call.binding.Owner, call.binding.PreparationID = p.Request.Actor, request.Resolve.LeaseOwner.Details(), p.ID
	call.binding.SnapshotID = draft.Snapshot.ID
	raw, _ := encoded(draft.Snapshot)
	call.binding.SnapshotDigest = hash(raw)
	resolved, err := stoppedResolvedModel(request.Resolve, p)
	if err != nil {
		t.Fatal(err)
	}
	call.binding.LeaseID = resolved.CredentialLease.LeaseID
	raw, _ = encoded(call.binding)
	call.digest = hash(raw)
	call.value.CallID, call.value.Consumer, call.value.SnapshotID, call.value.Identity, call.value.ProcessID = request.CallID, request.Resolve.Consumer.Clone(), draft.Snapshot.ID, draft.Snapshot.Identity, request.PreparedProcessID
	if err := stoppedResolutionMatches(request, p, call); err != nil {
		t.Fatal("exact original call rejected", err)
	}
	for _, change := range []func(*mc.StoppedResolutionRequest){
		func(r *mc.StoppedResolutionRequest) { r.PreparedProcessID = mustID[oc.Process](t) },
		func(r *mc.StoppedResolutionRequest) { r.Input.Digest = hash([]byte("other input")) },
	} {
		changed := request.Clone()
		change(&changed)
		if err := stoppedResolutionMatches(changed, p, call); err == nil {
			t.Fatal("foreign original tuple authorized observation")
		}
	}
	if err := stoppedResolutionMatches(request, nil, call); err == nil {
		t.Fatal("present call became settled resolution absence")
	}
	p.Phase = "prepared"
	if err := stoppedResolutionMatches(request, p, call); err == nil {
		t.Fatal("uncommitted preparation supplied an admitted call")
	}
	// A known final does not waive the exact original process/input checks.
	now, _ := f.NewInstant(time.Now())
	retired, _, err := stoppedRuntimeNext(call, now)
	if err != nil {
		t.Fatal(err)
	}
	p.Phase = "committed"
	changed = request.Clone()
	changed.PreparedProcessID = mustID[oc.Process](t)
	if err := stoppedResolutionMatches(changed, p, retired); err == nil {
		t.Fatal("retirement substituted for original ownership")
	}
}
