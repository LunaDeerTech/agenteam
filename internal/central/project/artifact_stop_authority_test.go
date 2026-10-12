package project

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func artifactStopAuthorityFixture(t *testing.T) (*LifecycleAuthority, *agentRetirementAuthorityStore, oc.ProjectStopRequest, oc.AccessDependencies) {
	t.Helper()
	projects, store, req := agentRetirementAuthorityFixture(t)
	op, err := f.ParseID[oc.ProjectStopOperation](req.Cause.OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	cause, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: req.ProjectID, OperationID: op, Action: oc.ProjectStopDelete, ProjectVersion: req.Cause.ProjectVersion})
	if err != nil {
		t.Fatal(err)
	}
	request, err := oc.NewProjectStopRequest(oc.ProjectStopRequestDetails{Actor: req.Actor, Cause: cause, Component: oc.ArtifactStopComponent, Step: oc.RequestProjectStopStep})
	if err != nil {
		t.Fatal(err)
	}
	a, err := projects.lifecycleAuthority()
	if err != nil {
		t.Fatal(err)
	}
	deps, err := a.DiscoverProjectStop(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return a, store, request, deps
}

func TestArtifactStopAuthorityOriginalTransaction(t *testing.T) {
	for _, mode := range []string{"nil-context", "cancelled", "zero-tx", "foreign-tx", "expired-tx", "nil-executor", "missing-project-lock", "wrong-component-plan", "original-scan"} {
		t.Run(mode, func(t *testing.T) {
			a, store, request, deps := artifactStopAuthorityFixture(t)
			ctx, tx := context.Background(), store.tx
			switch mode {
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "zero-tx":
				tx = f.Tx{}
			case "foreign-tx":
				tx = f.NewTx()
			case "expired-tx":
				store.live = false
			case "nil-executor":
				store.nilExecutor = true
			case "missing-project-lock":
				delete(store.held, projectLock(request.Details().Cause.Details().ProjectID, f.Shared).Key.Canonical())
			case "wrong-component-plan":
				d := request.Details()
				d.Component = oc.ObjectStopComponent
				var err error
				request, err = oc.NewProjectStopRequest(d)
				if err != nil {
					t.Fatal(err)
				}
			}
			grant, err := a.ValidateProjectStopInTx(ctx, tx, request, deps)
			seen := store.seen()
			if err == nil || grant.Validate() == nil || slices.Contains(seen, "forbidden-tx") || slices.Contains(seen, "forbidden-acquire") || slices.Contains(seen, "forbidden-write") {
				t.Fatal("rejected original transaction granted stop", err, seen)
			}
			if mode == "original-scan" {
				if !errors.Is(err, store.readErr) || !slices.Contains(seen, "row") {
					t.Fatal("original Scan lost", err, seen)
				}
			} else if slices.Contains(seen, "row") {
				t.Fatal("invalid input reached canonical SQL", seen)
			}
		})
	}
}

func TestArtifactStopAuthorityDiscoveryIsNotGrant(t *testing.T) {
	a, store, request, deps := artifactStopAuthorityFixture(t)
	want := projectLock(request.Details().Cause.Details().ProjectID, f.Shared)
	if len(store.seen()) != 0 || len(deps.Locks()) != 1 || deps.Locks()[0].Key.Canonical() != want.Key.Canonical() || deps.Locks()[0].Mode != want.Mode {
		t.Fatal("discovery consumed SQL or different Project locks", store.seen())
	}
	d := request.Details()
	d.Step = oc.InspectProjectStopStep
	inspect, err := oc.NewProjectStopRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.DiscoverProjectStop(context.Background(), inspect)
	if err != nil || other.Equal(deps) {
		t.Fatal("inspect borrowed request binding", err)
	}
	_, err = a.ValidateProjectStopInTx(context.Background(), store.tx, inspect, deps)
	hasCode(t, err, f.Forbidden)
	if len(store.seen()) != 0 {
		t.Fatal("wrong original plan reached Store")
	}
}

func TestArtifactStopAuthorityWaitsForOriginalScan(t *testing.T) {
	a, store, request, deps := artifactStopAuthorityFixture(t)
	store.readEntered, store.readRelease = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var once sync.Once
	free := func() { once.Do(func() { close(store.readRelease) }) }
	joined := false
	t.Cleanup(func() {
		cancel()
		free()
		if !joined {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("original Scan still in flight")
			}
		}
	})
	go func() { _, err := a.ValidateProjectStopInTx(ctx, store.tx, request, deps); done <- err }()
	select {
	case <-store.readEntered:
	case <-time.After(time.Second):
		t.Fatal("original Scan not entered")
	}
	cancel()
	select {
	case <-done:
		joined = true
		t.Fatal("cancel replaced original Scan return")
	default:
	}
	free()
	select {
	case err := <-done:
		joined = true
		if !errors.Is(err, store.readErr) {
			t.Fatal("original Scan error lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("released original Scan did not return")
	}
}
