package project

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type outboxCleanupControl struct {
	*outboxStopControl
	cleanup func(context.Context, identity.Actor, oc.LifecycleCause) (oc.CleanupReport, error)
}

func (p *outboxCleanupControl) Cleanup(ctx context.Context, actor identity.Actor, cause oc.LifecycleCause) (oc.CleanupReport, error) {
	return p.cleanup(ctx, actor, cause)
}

func TestOutboxCleanupConstructionAndInput(t *testing.T) {
	v := newOutboxStopFixture(t)
	authority := v.participant.(*outboxStopParticipant).authority
	participant, err := NewOutboxLifecycleParticipant(authority, v.provider)
	if err != nil || participant.Name() != c.OutboxParticipant {
		t.Fatal("fixed complete adapter", err)
	}
	if _, err = NewOutboxLifecycleParticipant(nil, v.provider); err == nil {
		t.Fatal("missing authority accepted")
	}
	var nilProvider *outboxCleanupControl
	if _, err = NewOutboxLifecycleParticipant(authority, nilProvider); err == nil {
		t.Fatal("typed nil provider accepted")
	}
	if _, err = NewOutboxLifecycleParticipant(authority, &outboxStopControl{name: "audit"}); err == nil {
		t.Fatal("foreign name accepted")
	}
	// The original preflight-controlled receipt is only an Inspect fixture.
	// Delegating it proves the complete adapter keeps that original stop path.
	stopped, err := participant.InspectStop(context.Background(), v.actor, v.cause, v.scope)
	if err != nil || !stopped.Matches(c.OutboxParticipant, v.cause, v.scope) || v.calls != 1 {
		t.Fatal("original Inspect delegation changed", err)
	}
	v.calls, v.txContext = 0, nil
	checkpoint, err := c.NewCleanupCheckpoint(c.OutboxParticipant, v.cause, v.scope, 1, []byte("opaque"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		ctx        context.Context
		actor      identity.Actor
		cause      c.LifecycleCause
		scope      c.ScopeRef
		checkpoint *c.CleanupCheckpoint
		code       f.Code
	}{
		{"nil-context", nil, v.actor, v.cause, v.scope, nil, f.InvalidArgument},
		{"human", context.Background(), testActor(t), v.cause, v.scope, nil, f.Forbidden},
		{"empty-cause", context.Background(), v.actor, c.LifecycleCause{}, v.scope, nil, f.InvalidArgument},
		{"empty-scope", context.Background(), v.actor, v.cause, c.ScopeRef{}, nil, f.InvalidArgument},
		{"foreign-project", context.Background(), v.actor, v.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: testID[identity.Project](t)}, nil, f.Forbidden},
		{"foreign-checkpoint", context.Background(), v.actor, v.cause, v.scope, &checkpoint, f.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := participant.Cleanup(tc.ctx, tc.actor, tc.cause, tc.scope, tc.checkpoint)
			hasCode(t, err, tc.code)
			if report.Details().State != "" || v.calls != 0 || v.txContext != nil {
				t.Fatal("rejected cleanup reached a dependency or exposed a report")
			}
		})
	}
	archive := v.cause
	archive.Action = c.Archive
	_, err = participant.Cleanup(context.Background(), v.actor, archive, v.scope, nil)
	hasCode(t, err, f.Forbidden)
	old, err := v.participant.Cleanup(context.Background(), v.actor, v.cause, v.scope, nil)
	hasCode(t, err, f.DependencyUnbound)
	if old.Details().State != "" || v.calls != 0 {
		t.Fatal("legacy constructor gained cleanup")
	}
}

func TestOutboxCleanupOpaqueCallPreservesFailure(t *testing.T) {
	v := newOutboxStopFixture(t)
	op, _ := f.ParseID[oc.LifecycleOperation](v.cause.OperationID.String())
	cause, _ := oc.NewLifecycleCause(oc.LifecycleDetails{ProjectID: v.scope.ProjectID, OperationID: op, Action: oc.DeleteProject, ProjectVersion: v.cause.ProjectVersion})
	sentinel := errors.New("controlled original proof failure")
	provider := &outboxCleanupControl{outboxStopControl: v.provider, cleanup: func(ctx context.Context, actor identity.Actor, got oc.LifecycleCause) (oc.CleanupReport, error) {
		if !actor.Equal(v.actor) || got.Details() != cause.Details() {
			t.Fatal("original cause changed")
		}
		return oc.CleanupReport{Removed: 1, Remaining: 1}, sentinel
	}}
	report, err := callOutboxCleanup(provider, context.Background(), v.actor, cause)
	if !errors.Is(err, sentinel) || report.Completed {
		t.Fatal("partial original error became completion", err)
	}
	provider.cleanup = func(context.Context, identity.Actor, oc.LifecycleCause) (oc.CleanupReport, error) {
		panic("opaque provider")
	}
	report, err = callOutboxCleanup(provider, context.Background(), v.actor, cause)
	hasCode(t, err, f.DependencyUnavailable)
	if report != (oc.CleanupReport{}) {
		t.Fatal("opaque panic fabricated progress")
	}
}
