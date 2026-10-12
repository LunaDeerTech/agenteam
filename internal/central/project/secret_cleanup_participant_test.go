package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	secret "github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type secretCleanerControl func(context.Context, identity.Actor, sc.LifecycleCause, identity.ProjectID, *sc.CleanupCheckpoint) (sc.CleanupReport, error)

func (call secretCleanerControl) CleanupProject(ctx context.Context, actor identity.Actor, cause sc.LifecycleCause, project identity.ProjectID, checkpoint *sc.CleanupCheckpoint) (sc.CleanupReport, error) {
	return call(ctx, actor, cause, project, checkpoint)
}

func secretCleanupControl(t *testing.T, store Store, cleaner secretCleanupProvider) (c.ProjectLifecycleParticipant, identity.Actor, c.LifecycleCause, c.ScopeRef) {
	t.Helper()
	a, err := NewLifecycleAuthority(store, registryTestManifest(t, registryTestEntries()), nil)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := NewSecretStopParticipant(a)
	if err != nil {
		t.Fatal(err)
	}
	actor, project, cause := lifecycleTestActor(t)
	cause.Action = c.Delete
	return &secretCleanupParticipant{ProjectLifecycleParticipant: stop, authority: a, cleaner: cleaner}, actor, cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
}

func TestSecretCleanupParticipantInputAndLegacy(t *testing.T) {
	store := &stopRoundGateStore{}
	cleaner := secretCleanerControl(func(context.Context, identity.Actor, sc.LifecycleCause, identity.ProjectID, *sc.CleanupCheckpoint) (sc.CleanupReport, error) {
		t.Fatal("rejected input reached Secret")
		return sc.CleanupReport{}, nil
	})
	p, actor, cause, scope := secretCleanupControl(t, store, cleaner)
	if p.Name() != c.SecretParticipant {
		t.Fatal("participant identity changed")
	}
	a := p.(*secretCleanupParticipant).authority
	for _, broken := range []*secret.Service{nil} {
		if _, err := NewSecretLifecycleParticipant(a, broken); err == nil {
			t.Fatal("nil cleaner bound")
		}
	}
	if _, err := NewSecretLifecycleParticipant(nil, &secret.Service{}); err == nil {
		t.Fatal("nil authority bound")
	}
	legacy, err := NewSecretStopParticipant(a)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Cleanup(context.Background(), actor, cause, scope, nil)
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

func TestSecretCleanupCheckpointAndReport(t *testing.T) {
	_, project, cause := lifecycleTestActor(t)
	cause.Action = c.Delete
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
	op, _ := f.ParseID[sc.LifecycleOperation](cause.OperationID.String())
	data := sc.CleanupCheckpoint{ProjectID: project, OperationID: op}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, completed := range []bool{false, true} {
		report, err := secretCleanupReport(cause, scope, sc.CleanupReport{Completed: completed, Checkpoint: data})
		if err != nil || !report.Matches(c.SecretParticipant, cause, scope) {
			t.Fatal("valid original Secret report refused", err)
		}
		got, err := decodeSecretCleanupCheckpoint(report.Details().Checkpoint, cause, scope)
		if err != nil || got == nil || got.ProjectID != project || got.OperationID != op || len(report.Details().RemainingRefs) != 0 {
			t.Fatal("original checkpoint identity changed", err)
		}
		got.ProjectID = testID[identity.Project](t)
		if !bytes.Equal(report.Details().Checkpoint.Bytes(), raw) {
			t.Fatal("decoded diagnostic aliases checkpoint bytes")
		}
	}
	if got, err := decodeSecretCleanupCheckpoint(nil, cause, scope); err != nil || got != nil {
		t.Fatal("initial empty progress refused")
	}
	for _, tc := range []struct {
		name   string
		raw    []byte
		schema f.Version
		owner  c.ParticipantName
	}{
		{"unknown-field", append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"unknown":true}`)...), 1, c.SecretParticipant},
		{"duplicate-field", append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"project_id":"`+project.String()+`"}`)...), 1, c.SecretParticipant},
		{"case-folded", bytes.Replace(raw, []byte(`"project_id"`), []byte(`"PROJECT_ID"`), 1), 1, c.SecretParticipant},
		{"trailing-value", append(append([]byte{}, raw...), []byte(` {}`)...), 1, c.SecretParticipant},
		{"foreign-project", bytes.Replace(raw, []byte(project.String()), []byte(testID[identity.Project](t).String()), 1), 1, c.SecretParticipant},
		{"foreign-operation", bytes.Replace(raw, []byte(op.String()), []byte(testID[sc.LifecycleOperation](t).String()), 1), 1, c.SecretParticipant},
		{"other-schema", raw, 2, c.SecretParticipant},
		{"other-participant", raw, 1, c.OutboxParticipant},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkpoint, err := c.NewCleanupCheckpoint(tc.owner, cause, scope, tc.schema, tc.raw)
			if err != nil {
				t.Fatal("invalid test envelope", err)
			}
			if got, err := decodeSecretCleanupCheckpoint(&checkpoint, cause, scope); err == nil || got != nil {
				t.Fatal("invalid stored checkpoint accepted")
			}
		})
	}
	for _, observed := range []sc.CleanupReport{{Completed: true}, {Checkpoint: sc.CleanupCheckpoint{ProjectID: testID[identity.Project](t), OperationID: op}}} {
		if report, err := secretCleanupReport(cause, scope, observed); err == nil || report.Details().State != "" {
			t.Fatal("malformed provider report became canonical progress")
		}
	}
}

func TestSecretCleanupPreflightMustActuallyConfirm(t *testing.T) {
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
			p, actor, cause, scope := secretCleanupControl(t, store, secretCleanerControl(func(context.Context, identity.Actor, sc.LifecycleCause, identity.ProjectID, *sc.CleanupCheckpoint) (sc.CleanupReport, error) {
				t.Fatal("unconfirmed preflight called real cleaner")
				return sc.CleanupReport{}, nil
			}))
			report, err := p.Cleanup(context.Background(), actor, cause, scope, nil)
			if err == nil || report.Details().State != "" {
				t.Fatal("unconfirmed preflight published a report")
			}
			switch kind {
			case "unknown":
				hasCode(t, err, f.CommitUnknown)
				original, ok := UnknownAttempt(err)
				if !ok || original.AttemptID() != attempt || original.Cause().Details().Owner != "project.secret-cleanup-preflight" {
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

func TestSecretCleanupWaitsForOriginalReturns(t *testing.T) {
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
			cleaner := secretCleanerControl(func(context.Context, identity.Actor, sc.LifecycleCause, identity.ProjectID, *sc.CleanupCheckpoint) (sc.CleanupReport, error) {
				if boundary != "cleaner-call" {
					t.Error("unconfirmed preflight entered cleaner")
					return sc.CleanupReport{}, sentinel
				}
				close(entered)
				<-release
				return sc.CleanupReport{Completed: true}, sentinel
			})
			store := &stopRoundGateStore{commit: f.NotCommittedResult(fault(f.InvalidState)), entered: entered, release: release}
			p, actor, cause, scope := secretCleanupControl(t, store, cleaner)
			go func() {
				if boundary == "preflight-store" {
					_, err := p.Cleanup(ctx, actor, cause, scope, nil)
					returned <- err
					return
				}
				op, _ := f.ParseID[sc.LifecycleOperation](cause.OperationID.String())
				downstream, _ := sc.NewLifecycleCause(op, cause.ProjectVersion, true)
				_, err := callSecretCleanup(cleaner, ctx, actor, downstream, scope.ProjectID, nil)
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
	panicCleaner := secretCleanerControl(func(context.Context, identity.Actor, sc.LifecycleCause, identity.ProjectID, *sc.CleanupCheckpoint) (sc.CleanupReport, error) {
		panic("opaque provider")
	})
	_, actor, cause, scope := secretCleanupControl(t, &stopRoundGateStore{}, panicCleaner)
	op, _ := f.ParseID[sc.LifecycleOperation](cause.OperationID.String())
	downstream, _ := sc.NewLifecycleCause(op, cause.ProjectVersion, true)
	report, err := callSecretCleanup(panicCleaner, context.Background(), actor, downstream, scope.ProjectID, nil)
	hasCode(t, err, f.DependencyUnavailable)
	if report != (sc.CleanupReport{}) {
		t.Fatal("provider panic fabricated completion")
	}
}
