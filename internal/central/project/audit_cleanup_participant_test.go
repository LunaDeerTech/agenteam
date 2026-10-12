package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type auditCleanerControl func(context.Context, identity.Actor, audit.LifecycleCause, identity.ProjectID, *audit.CleanupCheckpoint) (audit.CleanupReport, error)

func (call auditCleanerControl) CleanupProject(ctx context.Context, actor identity.Actor, cause audit.LifecycleCause, project identity.ProjectID, checkpoint *audit.CleanupCheckpoint) (audit.CleanupReport, error) {
	return call(ctx, actor, cause, project, checkpoint)
}

func auditCleanupControl(t *testing.T, store Store, cleaner audit.Cleaner) (c.ProjectLifecycleParticipant, identity.Actor, c.LifecycleCause, c.ScopeRef) {
	t.Helper()
	a, err := NewLifecycleAuthority(store, registryTestManifest(t, registryTestEntries()), nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewAuditLifecycleParticipant(a, cleaner)
	if err != nil {
		t.Fatal(err)
	}
	actor, project, cause := lifecycleTestActor(t)
	cause.Action = c.Delete
	return p, actor, cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
}

func TestAuditCleanupParticipantInputAndLegacy(t *testing.T) {
	store := &stopRoundGateStore{}
	cleaner := auditCleanerControl(func(context.Context, identity.Actor, audit.LifecycleCause, identity.ProjectID, *audit.CleanupCheckpoint) (audit.CleanupReport, error) {
		t.Fatal("rejected input reached Audit")
		return audit.CleanupReport{}, nil
	})
	p, actor, cause, scope := auditCleanupControl(t, store, cleaner)
	if p.Name() != c.AuditParticipant {
		t.Fatal("participant identity changed")
	}
	a := p.(*auditCleanupParticipant).authority
	for _, broken := range []audit.Cleaner{nil, auditCleanerControl(nil)} {
		if _, err := NewAuditLifecycleParticipant(a, broken); err == nil {
			t.Fatal("nil cleaner bound")
		}
	}
	if _, err := NewAuditLifecycleParticipant(nil, cleaner); err == nil {
		t.Fatal("nil authority bound")
	}
	legacy, err := NewAuditStopParticipant(a)
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

func TestAuditCleanupCheckpointAndReport(t *testing.T) {
	_, project, cause := lifecycleTestActor(t)
	cause.Action = c.Delete
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
	op, _ := f.ParseID[audit.LifecycleOperation](cause.OperationID.String())
	id := testID[audit.Record](t)
	data := audit.CleanupCheckpoint{ProjectID: project, OperationID: op, LastID: &id}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []audit.CleanupState{audit.CleanupPending, audit.CleanupCompleted} {
		report, err := auditCleanupReport(cause, scope, audit.CleanupReport{State: state, Checkpoint: data})
		if err != nil || !report.Matches(c.AuditParticipant, cause, scope) {
			t.Fatal("valid original Audit report refused", err)
		}
		got, err := decodeAuditCleanupCheckpoint(report.Details().Checkpoint, cause, scope)
		if err != nil || got == nil || got.ProjectID != project || got.OperationID != op || got.LastID == nil || *got.LastID != id || len(report.Details().RemainingRefs) != 0 {
			t.Fatal("original checkpoint identity changed", err)
		}
		*got.LastID = testID[audit.Record](t)
		if !bytes.Equal(report.Details().Checkpoint.Bytes(), raw) {
			t.Fatal("decoded diagnostic aliases checkpoint bytes")
		}
	}
	if got, err := decodeAuditCleanupCheckpoint(nil, cause, scope); err != nil || got != nil {
		t.Fatal("initial empty progress refused")
	}
	withoutLast, _ := json.Marshal(audit.CleanupCheckpoint{ProjectID: project, OperationID: op})
	for _, tc := range []struct {
		name   string
		raw    []byte
		schema f.Version
		owner  c.ParticipantName
	}{
		{"unknown-field", append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"unknown":true}`)...), 1, c.AuditParticipant},
		{"duplicate-field", append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"project_id":"`+project.String()+`"}`)...), 1, c.AuditParticipant},
		{"case-folded", bytes.Replace(raw, []byte(`"project_id"`), []byte(`"PROJECT_ID"`), 1), 1, c.AuditParticipant},
		{"explicit-null", append(append([]byte{}, withoutLast[:len(withoutLast)-1]...), []byte(`,"last_id":null}`)...), 1, c.AuditParticipant},
		{"trailing-value", append(append([]byte{}, raw...), []byte(` {}`)...), 1, c.AuditParticipant},
		{"foreign-project", bytes.Replace(raw, []byte(project.String()), []byte(testID[identity.Project](t).String()), 1), 1, c.AuditParticipant},
		{"foreign-operation", bytes.Replace(raw, []byte(op.String()), []byte(testID[audit.LifecycleOperation](t).String()), 1), 1, c.AuditParticipant},
		{"invalid-id", bytes.Replace(raw, []byte(id.String()), []byte("invalid"), 1), 1, c.AuditParticipant},
		{"other-schema", raw, 2, c.AuditParticipant},
		{"other-participant", raw, 1, c.OutboxParticipant},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkpoint, err := c.NewCleanupCheckpoint(tc.owner, cause, scope, tc.schema, tc.raw)
			if err != nil {
				t.Fatal("invalid test envelope", err)
			}
			if got, err := decodeAuditCleanupCheckpoint(&checkpoint, cause, scope); err == nil || got != nil {
				t.Fatal("invalid stored checkpoint accepted")
			}
		})
	}
	for _, observed := range []audit.CleanupReport{{State: "failed", Checkpoint: data}, {State: audit.CleanupCompleted}, {State: audit.CleanupPending, Checkpoint: audit.CleanupCheckpoint{ProjectID: testID[identity.Project](t), OperationID: op}}} {
		if report, err := auditCleanupReport(cause, scope, observed); err == nil || report.Details().State != "" {
			t.Fatal("malformed provider report became canonical progress")
		}
	}
}

func TestAuditCleanupPreflightMustActuallyConfirm(t *testing.T) {
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
			p, actor, cause, scope := auditCleanupControl(t, store, auditCleanerControl(func(context.Context, identity.Actor, audit.LifecycleCause, identity.ProjectID, *audit.CleanupCheckpoint) (audit.CleanupReport, error) {
				t.Fatal("unconfirmed preflight called real cleaner")
				return audit.CleanupReport{}, nil
			}))
			report, err := p.Cleanup(context.Background(), actor, cause, scope, nil)
			if err == nil || report.Details().State != "" {
				t.Fatal("unconfirmed preflight published a report")
			}
			switch kind {
			case "unknown":
				hasCode(t, err, f.CommitUnknown)
				original, ok := UnknownAttempt(err)
				if !ok || original.AttemptID() != attempt || original.Cause().Details().Owner != "project.audit-cleanup-preflight" {
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

func TestAuditCleanupWaitsForOriginalReturns(t *testing.T) {
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
			cleaner := auditCleanerControl(func(context.Context, identity.Actor, audit.LifecycleCause, identity.ProjectID, *audit.CleanupCheckpoint) (audit.CleanupReport, error) {
				if boundary != "cleaner-call" {
					t.Error("unconfirmed preflight entered cleaner")
					return audit.CleanupReport{}, sentinel
				}
				close(entered)
				<-release
				return audit.CleanupReport{State: audit.CleanupCompleted}, sentinel
			})
			store := &stopRoundGateStore{commit: f.NotCommittedResult(fault(f.InvalidState)), entered: entered, release: release}
			p, actor, cause, scope := auditCleanupControl(t, store, cleaner)
			go func() {
				if boundary == "preflight-store" {
					_, err := p.Cleanup(ctx, actor, cause, scope, nil)
					returned <- err
					return
				}
				op, _ := f.ParseID[audit.LifecycleOperation](cause.OperationID.String())
				downstream, _ := audit.NewLifecycleCause(op, audit.Delete, cause.ProjectVersion)
				_, err := callAuditCleanup(cleaner, ctx, actor, downstream, scope.ProjectID, nil)
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
	panicCleaner := auditCleanerControl(func(context.Context, identity.Actor, audit.LifecycleCause, identity.ProjectID, *audit.CleanupCheckpoint) (audit.CleanupReport, error) {
		panic("opaque provider")
	})
	_, actor, cause, scope := auditCleanupControl(t, &stopRoundGateStore{}, panicCleaner)
	op, _ := f.ParseID[audit.LifecycleOperation](cause.OperationID.String())
	downstream, _ := audit.NewLifecycleCause(op, audit.Delete, cause.ProjectVersion)
	report, err := callAuditCleanup(panicCleaner, context.Background(), actor, downstream, scope.ProjectID, nil)
	hasCode(t, err, f.DependencyUnavailable)
	if report.State != "" || report.Checkpoint.LastID != nil {
		t.Fatal("provider panic fabricated completion")
	}
}
