package model

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

func stoppedPureRecord(t *testing.T) *runtimeRecord {
	t.Helper()
	r := runtimePureRequest(t)
	deadline, _ := f.NewInstant(time.Now().Add(time.Minute))
	one := f.Sequence(1)
	b := runtimeBinding{Format: 1, CallID: r.CallID, Consumer: r.Consumer, Input: r.Input, Initiator: runtimeStableActor(r.Actor), SnapshotID: r.Model.Snapshot.ID, SnapshotDigest: hash([]byte("snapshot")), PreparationID: mustID[struct{}](t).String(), LeaseID: r.Model.CredentialLease.LeaseID, Owner: r.Model.LeaseOwner.Details(), Policy: mc.RetryPolicy{Class: mc.BoundedRetry, Deadline: &deadline, MaxAttempts: &one}, Mode: "json"}
	raw, err := encoded(b)
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.NewInstant(time.Now().Add(-time.Minute))
	record := &runtimeRecord{binding: b, digest: hash(raw), value: uc.Invocation{ID: mustID[mc.Invocation](t), CallID: r.CallID, AttemptIndex: 1, Consumer: r.Consumer, SnapshotID: r.Model.Snapshot.ID, Identity: r.Model.Snapshot.Identity, ProcessID: mustID[oc.Process](t), Fence: 1, Dispatch: uc.Reserved, StartedAt: at, Usage: mc.Usage{Source: mc.UnknownUsage}}, sequence: 1, version: 1, phase: "accepted"}
	if err = record.validate(); err != nil {
		t.Fatal("fixture", err)
	}
	return record
}

func TestStoppedRuntimePreservesFactsWithoutResend(t *testing.T) {
	for _, v := range []struct {
		dispatch uc.Dispatch
		want     uc.Dispatch
		status   uc.TerminalStatus
		steps    f.Sequence
	}{{uc.Reserved, uc.NotSent, uc.Cancelled, 2}, {uc.Authorized, uc.DispatchUnknown, uc.Unknown, 2}, {uc.Sent, uc.Sent, uc.Unknown, 1}, {uc.NotSent, uc.NotSent, uc.Cancelled, 1}, {uc.DispatchUnknown, uc.DispatchUnknown, uc.Unknown, 1}} {
		t.Run(string(v.dispatch), func(t *testing.T) {
			before := stoppedPureRecord(t)
			before.value.Dispatch = v.dispatch
			if v.dispatch != uc.Reserved {
				before.phase = "running"
			}
			if v.dispatch == uc.Sent {
				at := before.value.StartedAt
				before.value.DispatchedAt = &at
				n := mc.TokenCount(7)
				before.value.Usage = mc.Usage{Source: mc.ProviderUsage, InputTokens: &n}
				before.value.ProviderRequestID = "original_request"
			}
			original := before.value.Clone()
			now, _ := f.NewInstant(time.Now())
			after, request, err := stoppedRuntimeNext(before, now)
			if err != nil {
				t.Fatal(err)
			}
			if !after.retired || after.value.Dispatch != v.want || after.value.Final.Status != v.status || after.sequence != before.sequence+v.steps || after.version != before.version+1 || after.value.ID != before.value.ID || after.value.AttemptIndex != 1 || after.value.ProcessID != before.value.ProcessID || request.Validate() != nil {
				t.Fatal("recovery invented an attempt/result")
			}
			if !reflect.DeepEqual(original, before.value) || !reflect.DeepEqual(original.Usage, after.value.Usage) || after.value.ProviderRequestID != original.ProviderRequestID {
				t.Fatal("recovery changed original evidence")
			}
		})
	}
}

func TestStoppedRuntimeKnownFinalIsImmutable(t *testing.T) {
	before := stoppedPureRecord(t)
	now, _ := f.NewInstant(time.Now())
	before.value.Dispatch = uc.NotSent
	before.value.Final = &uc.Final{Status: uc.Failed, Version: 1, FinishedAt: now, Error: &mc.ModelError{Category: "permission", Code: "original_denial"}}
	before.phase = "failed"
	after, request, err := stoppedRuntimeNext(before, now)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.value, after.value) || after.sequence != before.sequence || request.AfterSequence != request.BeforeSequence || !after.retired {
		t.Fatal("known terminal was rewritten")
	}
	before.retired = true
	after, _, err = stoppedRuntimeNext(before, now)
	if err != nil || !runtimeSameRecord(before, after) {
		t.Fatal("retired tail rewrote Model", err)
	}
}

func TestStoppedRuntimeRequiresPrivateRegisteredProof(t *testing.T) {
	a := &RuntimeAuthority{data: func() *runtimeAuthorityState { return &runtimeAuthorityState{} }}
	if _, err := a.DiscoverStoppedInvocationFacts(context.Background(), uc.StoppedInvocationRequest{}); err == nil {
		t.Fatal("bare DTO authorized recovery")
	}
	if _, err := a.stoppedCall(context.WithValue(context.Background(), stoppedRuntimeKey{}, &stoppedRuntimeCall{})); err == nil {
		t.Fatal("unregistered witness authorized recovery")
	}
}

func TestStoppedRuntimeDrainJoinsAdmittedObservation(t *testing.T) {
	s := &runtimeState{ready: true, calls: make(map[mc.CallID]*runtimeCall), admissions: make(map[*runtimeAdmission]struct{}), recoveries: make(map[mc.CallID]*stoppedRuntimeCall)}
	r := &Runtime{data: func() *runtimeState { return s }}
	lookup, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	var cancellations atomic.Int32
	admission := &runtimeAdmission{cancel: func() {
		cancel()
		if cancellations.Add(1) == 2 {
			close(entered)
		}
	}, done: make(chan struct{})}
	s.admissions[admission] = struct{}{}
	w := &stoppedRuntimeCall{runtime: s, before: stoppedPureRecord(t), active: true, stopped: true}
	s.recoveries[w.before.binding.CallID] = w
	// Lookup releases its original observation witness before admission.done.
	returned := make(chan struct{})
	release := make(chan struct{})
	go func() {
		<-lookup.Done()
		<-release
		w.release()
		s.mu.Lock()
		delete(s.admissions, admission)
		close(admission.done)
		s.mu.Unlock()
		close(returned)
	}()
	r.StopAdmission()
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	result := make(chan error, 1)
	go func() { result <- r.Drain(ctx) }()
	select {
	case err := <-result:
		close(release)
		<-returned
		t.Fatalf("Drain returned before cancelling and joining the admitted lookup: %v", err)
	case <-entered:
	case <-ctx.Done():
		close(release)
		<-returned
		<-result
		t.Fatal("Drain never entered the original admitted lookup lifetime")
	}
	if r.Joined() {
		t.Fatal("lookup witness was abandoned before return")
	}
	close(release)
	<-returned
	if err := <-result; err != nil || !r.Joined() {
		t.Fatal("original observation did not join", err)
	}
}

func TestStoppedRuntimeOwnerRequiresPrivateStartupAndOriginalRow(t *testing.T) {
	a := &RuntimeAuthority{data: func() *runtimeAuthorityState { return &runtimeAuthorityState{} }}
	if err := a.RequireStoppedInvocationStartupInTx(context.Background(), f.Tx{}); err == nil {
		t.Fatal("bare startup authorized enumeration")
	}
	if _, err := a.DiscoverStoppedInvocationOwnerFacts(context.Background(), uc.StoppedInvocationOwner{}); err == nil {
		t.Fatal("bare DTO authorized borrower cleanup")
	}
	old := stoppedPureRecord(t)
	owner := &stoppedRuntimeOwner{request: uc.StoppedInvocationOwner{ProjectID: old.binding.Consumer.ProjectID, CallID: old.binding.CallID, ProcessID: old.value.ProcessID}}
	if err := owner.allowedRecord(old); err == nil {
		t.Fatal("unretired Model call authorized tail-only closure")
	}
	at, _ := f.NewInstant(time.Now())
	retired, _, err := stoppedRuntimeNext(old, at)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.allowedRecord(retired); err != nil {
		t.Fatal(err)
	}
	retired.value.ProcessID = mustID[oc.Process](t)
	if err = owner.allowedRecord(retired); err == nil {
		t.Fatal("different original process authorized tail closure")
	}
}

type stoppedOwnerFailedObservationStore struct {
	Store
	reads int
}

func (s *stoppedOwnerFailedObservationStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	s.reads++
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func TestStoppedRuntimeOwnerFailedObservationKeepsOriginalUnknown(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "read-failed", true: "cancelled"}[cancelled], func(t *testing.T) {
			store := &stoppedOwnerFailedObservationStore{}
			s := &runtimeState{store: store, stoppedOwners: make(map[mc.CallID]*stoppedRuntimeOwner)}
			original := f.NewFault(f.CommitUnknown, f.Unknown)
			w := &stoppedRuntimeOwner{runtime: s, active: true, original: original, request: uc.StoppedInvocationOwner{CallID: mustID[mc.Call](t)}}
			s.stoppedOwners[w.request.CallID] = w
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelled {
				cancel()
			}
			if err := w.confirm(ctx); err != original || !w.active || s.stoppedOwners[w.request.CallID] != w {
				t.Fatal("failed confirmation replaced original Unknown or abandoned its owner", err)
			}
			want := 1
			if cancelled {
				want = 0
			}
			if store.reads != want {
				t.Fatal("confirmation performed unexpected work", store.reads)
			}
		})
	}
}

func TestStoppedRuntimeOwnerMappingRepresentsAbsentCall(t *testing.T) {
	binding, process := hash([]byte("original borrower")), mustID[oc.Process](t)
	absent, err := stoppedOwnerRecordMapping(binding, process, nil)
	if err != nil || absent.Validate() != nil {
		t.Fatal("original pre-admission borrower cannot represent call absence", err)
	}
	again, err := stoppedOwnerRecordMapping(binding, process, nil)
	if err != nil || again != absent {
		t.Fatal("same absence proof changed its mapping", err)
	}
	record := stoppedPureRecord(t)
	present, err := stoppedOwnerRecordMapping(binding, process, record)
	if err != nil || present == absent {
		t.Fatal("absence aliased a persisted call", err)
	}
	record.version = 0
	if _, err = stoppedOwnerRecordMapping(binding, process, record); err == nil {
		t.Fatal("invalid persisted version became an absence proof")
	}
}
