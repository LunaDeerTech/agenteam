package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	artifact "github.com/LunaDeerTech/agenteam/internal/central/artifact"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type artifactCleanerControl func(context.Context, identity.Actor, oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error)

func (call artifactCleanerControl) CleanupProject(ctx context.Context, actor identity.Actor, cause oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
	return call(ctx, actor, cause)
}

func artifactCleanupControl(t *testing.T, store Store, cleaner artifactCleanupProvider) (c.ProjectLifecycleParticipant, identity.Actor, c.LifecycleCause, c.ScopeRef) {
	t.Helper()
	a, err := NewLifecycleAuthority(store, registryTestManifest(t, registryTestEntries()), nil)
	if err != nil {
		t.Fatal(err)
	}
	actor, project, cause := lifecycleTestActor(t)
	cause.Action = c.Delete
	return &artifactCleanupParticipant{authority: a, cleaner: cleaner}, actor, cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
}

func TestArtifactCleanupParticipantInputAndUnboundStop(t *testing.T) {
	store := &stopRoundGateStore{}
	cleaner := artifactCleanerControl(func(context.Context, identity.Actor, oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
		t.Fatal("rejected input reached Artifact")
		return oc.ProjectCleanupResult{}, nil
	})
	p, actor, cause, scope := artifactCleanupControl(t, store, cleaner)
	if p.Name() != c.ArtifactObjectParticipant {
		t.Fatal("participant identity changed")
	}
	a := p.(*artifactCleanupParticipant).authority
	for _, broken := range []*artifact.Service{nil} {
		if _, err := NewArtifactLifecycleParticipant(a, broken); err == nil {
			t.Fatal("nil cleaner bound")
		}
	}
	if _, err := NewArtifactLifecycleParticipant(nil, &artifact.Service{}); err == nil {
		t.Fatal("nil authority bound")
	}
	if _, err := NewArtifactLifecycleParticipant(a, &artifact.Service{}); err == nil {
		t.Fatal("unbound Object composition accepted")
	}
	_, err := p.RequestStop(context.Background(), actor, cause, scope)
	hasCode(t, err, f.DependencyUnbound)
	_, err = p.InspectStop(context.Background(), actor, cause, scope)
	hasCode(t, err, f.DependencyUnbound)
	archive := cause
	archive.Action = c.Archive
	meeting := testID[c.Meeting](t)
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		actor identity.Actor
		cause c.LifecycleCause
		scope c.ScopeRef
		code  f.Code
	}{
		{"nil-context", nil, actor, cause, scope, f.InvalidArgument},
		{"human", context.Background(), testActor(t), cause, scope, f.Forbidden},
		{"empty-cause", context.Background(), actor, c.LifecycleCause{}, scope, f.InvalidArgument},
		{"archive", context.Background(), actor, archive, scope, f.Forbidden},
		{"foreign-project", context.Background(), actor, cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: testID[identity.Project](t)}, f.Forbidden},
		{"meeting", context.Background(), actor, cause, c.ScopeRef{Kind: c.MeetingScope, ProjectID: scope.ProjectID, MeetingID: &meeting}, f.DependencyUnbound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := p.Cleanup(tc.ctx, tc.actor, tc.cause, tc.scope, nil)
			hasCode(t, err, tc.code)
			if report.Details().State != "" || store.transactions != 0 {
				t.Fatal("invalid input produced progress or started a transaction")
			}
		})
	}
}

func TestArtifactCleanupCheckpointAndReport(t *testing.T) {
	_, project, cause := lifecycleTestActor(t)
	cause.Action = c.Delete
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
	op, _ := f.ParseID[oc.CleanupOperation](cause.OperationID.String())
	data := artifactCleanupCheckpoint{ProjectID: project, OperationID: op}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []oc.CleanupState{oc.CleanupPending, oc.CleanupCompleted} {
		report, err := artifactCleanupReport(cause, scope, oc.ProjectCleanupResult{State: state, ProjectID: project, OperationID: op})
		if err != nil || !report.Matches(c.ArtifactObjectParticipant, cause, scope) {
			t.Fatal("valid original Artifact report refused", err)
		}
		got, err := decodeArtifactCleanupCheckpoint(report.Details().Checkpoint, cause, scope)
		if err != nil || got == nil || got.ProjectID != project || got.OperationID != op || len(report.Details().RemainingRefs) != 0 {
			t.Fatal("original checkpoint identity changed", err)
		}
		got.ProjectID = testID[identity.Project](t)
		if !bytes.Equal(report.Details().Checkpoint.Bytes(), raw) {
			t.Fatal("decoded diagnostic aliases checkpoint bytes")
		}
	}
	if got, err := decodeArtifactCleanupCheckpoint(nil, cause, scope); err != nil || got != nil {
		t.Fatal("initial empty progress refused")
	}
	for _, tc := range []struct {
		name   string
		raw    []byte
		schema f.Version
		owner  c.ParticipantName
	}{
		{"unknown-field", append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"unknown":true}`)...), 1, c.ArtifactObjectParticipant},
		{"duplicate-field", append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"project_id":"`+project.String()+`"}`)...), 1, c.ArtifactObjectParticipant},
		{"case-folded", bytes.Replace(raw, []byte(`"project_id"`), []byte(`"PROJECT_ID"`), 1), 1, c.ArtifactObjectParticipant},
		{"trailing-value", append(append([]byte{}, raw...), []byte(` {}`)...), 1, c.ArtifactObjectParticipant},
		{"foreign-project", bytes.Replace(raw, []byte(project.String()), []byte(testID[identity.Project](t).String()), 1), 1, c.ArtifactObjectParticipant},
		{"foreign-operation", bytes.Replace(raw, []byte(op.String()), []byte(testID[oc.CleanupOperation](t).String()), 1), 1, c.ArtifactObjectParticipant},
		{"other-schema", raw, 2, c.ArtifactObjectParticipant},
		{"other-participant", raw, 1, c.OutboxParticipant},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkpoint, err := c.NewCleanupCheckpoint(tc.owner, cause, scope, tc.schema, tc.raw)
			if err != nil {
				t.Fatal("invalid test envelope", err)
			}
			if got, err := decodeArtifactCleanupCheckpoint(&checkpoint, cause, scope); err == nil || got != nil {
				t.Fatal("invalid stored checkpoint accepted")
			}
		})
	}
	for _, observed := range []oc.ProjectCleanupResult{
		{},
		{State: oc.CleanupCompleted, ProjectID: testID[identity.Project](t), OperationID: op},
		{State: oc.CleanupCompleted, ProjectID: project, OperationID: op, Remaining: 1},
		{State: oc.CleanupPending, ProjectID: project, OperationID: op, Remaining: -1},
	} {
		if report, err := artifactCleanupReport(cause, scope, observed); err == nil || report.Details().State != "" {
			t.Fatal("malformed provider report became canonical progress")
		}
	}
}

func TestArtifactCleanupPreflightMustActuallyConfirm(t *testing.T) {
	for _, kind := range []string{"unknown", "rollback", "committed-without-callback"} {
		t.Run(kind, func(t *testing.T) {
			attempt := testID[f.TransactionAttempt](t)
			store := &barrierControlStore{within: func(_ context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
				switch kind {
				case "unknown":
					return f.UnknownResult(attempt, cause)
				case "rollback":
					return f.NotCommittedResult(fault(f.InvalidState))
				default:
					return f.CommittedResult()
				}
			}}
			p, actor, cause, scope := artifactCleanupControl(t, store, artifactCleanerControl(func(context.Context, identity.Actor, oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
				t.Fatal("unconfirmed preflight called real cleaner")
				return oc.ProjectCleanupResult{}, nil
			}))
			report, err := p.Cleanup(context.Background(), actor, cause, scope, nil)
			if err == nil || report.Details().State != "" {
				t.Fatal("unconfirmed preflight published a report")
			}
			switch kind {
			case "unknown":
				hasCode(t, err, f.CommitUnknown)
				original, ok := UnknownAttempt(err)
				if !ok || original.AttemptID() != attempt || original.Cause().Details().Owner != "project.artifact-cleanup-preflight" {
					t.Fatal("original Unknown identity lost")
				}
			case "rollback":
				hasCode(t, err, f.InvalidState)
			default:
				hasCode(t, err, f.DependencyUnavailable)
			}
		})
	}
}

func TestArtifactCleanupWaitsForOriginalReturns(t *testing.T) {
	for _, boundary := range []string{"preflight-store", "cleaner-call"} {
		t.Run(boundary, func(t *testing.T) {
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			var once sync.Once
			open := func() { once.Do(func() { close(release) }) }
			joined := false
			t.Cleanup(func() {
				open()
				if !joined {
					select {
					case <-returned:
					case <-time.After(5 * time.Second):
						t.Error("original controlled call did not return")
					}
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("original cleaner error")
			cleaner := artifactCleanerControl(func(context.Context, identity.Actor, oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
				if boundary != "cleaner-call" {
					t.Error("unconfirmed preflight entered cleaner")
					return oc.ProjectCleanupResult{}, sentinel
				}
				close(entered)
				<-release
				return oc.ProjectCleanupResult{State: oc.CleanupCompleted}, sentinel
			})
			store := &stopRoundGateStore{commit: f.NotCommittedResult(fault(f.InvalidState)), entered: entered, release: release}
			p, actor, cause, scope := artifactCleanupControl(t, store, cleaner)
			go func() {
				if boundary == "preflight-store" {
					_, err := p.Cleanup(ctx, actor, cause, scope, nil)
					returned <- err
					return
				}
				op, _ := f.ParseID[oc.CleanupOperation](cause.OperationID.String())
				downstream, err := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: scope.ProjectID, OperationID: op, Version: cause.ProjectVersion})
				if err != nil {
					returned <- err
					return
				}
				_, err = callArtifactCleanup(cleaner, ctx, actor, downstream)
				returned <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("original controlled call not entered")
			}
			cancel()
			select {
			case <-returned:
				joined = true
				t.Fatal("cancellation replaced the original return")
			default:
			}
			open()
			select {
			case err := <-returned:
				joined = true
				if boundary == "cleaner-call" && !errors.Is(err, sentinel) || err == nil {
					t.Fatal("original failure became success", err)
				}
			case <-time.After(time.Second):
				t.Fatal("released original call did not return")
			}
		})
	}
	panicCleaner := artifactCleanerControl(func(context.Context, identity.Actor, oc.ProjectCleanupCause) (oc.ProjectCleanupResult, error) {
		panic("opaque provider")
	})
	_, actor, cause, scope := artifactCleanupControl(t, &stopRoundGateStore{}, panicCleaner)
	op, _ := f.ParseID[oc.CleanupOperation](cause.OperationID.String())
	downstream, err := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: scope.ProjectID, OperationID: op, Version: cause.ProjectVersion})
	if err != nil {
		t.Fatal(err)
	}
	report, err := callArtifactCleanup(panicCleaner, context.Background(), actor, downstream)
	hasCode(t, err, f.DependencyUnavailable)
	if report != (oc.ProjectCleanupResult{}) {
		t.Fatal("provider panic fabricated completion")
	}
}
