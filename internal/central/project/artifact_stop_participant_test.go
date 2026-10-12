package project

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type artifactStopControl func(context.Context, identity.Actor, oc.ProjectStopCause, bool) (oc.ObjectStopReport, error)

func (p artifactStopControl) RequestProjectStop(ctx context.Context, a identity.Actor, cause oc.ProjectStopCause) (oc.ObjectStopReport, error) {
	return p(ctx, a, cause, false)
}
func (p artifactStopControl) InspectProjectStop(ctx context.Context, a identity.Actor, cause oc.ProjectStopCause) (oc.ObjectStopReport, error) {
	return p(ctx, a, cause, true)
}
func artifactStopCause(t *testing.T, cause c.LifecycleCause, scope c.ScopeRef) oc.ProjectStopCause {
	t.Helper()
	op, err := f.ParseID[oc.ProjectStopOperation](cause.OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	value, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: scope.ProjectID, OperationID: op, Action: oc.ProjectStopAction(cause.Action), ProjectVersion: cause.ProjectVersion})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func artifactStopReport(t *testing.T, component oc.ProjectStopComponent, cause oc.ProjectStopCause, details oc.ObjectStopDetails) oc.ObjectStopReport {
	t.Helper()
	report, err := oc.NewObjectStopReport(component, cause, details)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// The inherited fixture uses a strict canonical deletion receipt for Inspect.
// It is a controlled original-transaction boundary, not a real stopping claim.
func artifactStopFixture(t *testing.T) (*artifactStopParticipant, *outboxStopFixture, *[2]int) {
	t.Helper()
	v := newOutboxStopFixture(t)
	calls := new([2]int)
	makePort := func(n int, component oc.ProjectStopComponent) artifactStopControl {
		return func(ctx context.Context, actor identity.Actor, cause oc.ProjectStopCause, inspect bool) (oc.ObjectStopReport, error) {
			calls[n]++
			if !v.returned || ctx != v.txContext || !inspect || !actor.Equal(v.actor) || !cause.Equal(artifactStopCause(t, v.cause, v.scope)) {
				t.Error("lost preflight actual return/original input")
			}
			if n == 1 && calls[0] != 1 {
				t.Error("Object entered before original Artifact return")
			}
			return artifactStopReport(t, component, cause, oc.ObjectStopDetails{State: oc.ProjectStopped}), nil
		}
	}
	return &artifactStopParticipant{authority: v.participant.(*outboxStopParticipant).authority, artifacts: makePort(0, oc.ArtifactStopComponent), objects: makePort(1, oc.ObjectStopComponent)}, v, calls
}
func TestArtifactObjectStopConstructionAndInput(t *testing.T) {
	p, v, calls := artifactStopFixture(t)
	for _, deps := range []struct {
		a        *LifecycleAuthority
		artifact *artifact.Service
		object   *object.Service
	}{
		{nil, &artifact.Service{}, &object.Service{}}, {p.authority, nil, &object.Service{}}, {p.authority, &artifact.Service{}, nil}, {p.authority, &artifact.Service{}, &object.Service{}},
	} {
		if _, err := NewArtifactObjectLifecycleParticipant(deps.a, deps.artifact, deps.object); err == nil {
			t.Fatal("unbound concrete original owners accepted")
		}
	}
	meeting := testID[c.Meeting](t)
	for _, tc := range []struct {
		name  string
		ctx   context.Context
		actor identity.Actor
		cause c.LifecycleCause
		scope c.ScopeRef
		code  f.Code
	}{
		{"nil-context", nil, v.actor, v.cause, v.scope, f.InvalidArgument},
		{"human", context.Background(), testActor(t), v.cause, v.scope, f.Forbidden},
		{"empty-cause", context.Background(), v.actor, c.LifecycleCause{}, v.scope, f.InvalidArgument},
		{"foreign-project", context.Background(), v.actor, v.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: testID[identity.Project](t)}, f.Forbidden},
		{"meeting", context.Background(), v.actor, v.cause, c.ScopeRef{Kind: c.MeetingScope, ProjectID: v.scope.ProjectID, MeetingID: &meeting}, f.DependencyUnbound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := p.RequestStop(tc.ctx, tc.actor, tc.cause, tc.scope)
			hasCode(t, err, tc.code)
			if report.Details().State != "" || *calls != [2]int{} || v.txContext != nil {
				t.Fatal("invalid input reached provider/SQL")
			}
		})
	}
	if p.Name() != c.ArtifactObjectParticipant {
		t.Fatal("wrong participant")
	}
}
func TestArtifactObjectStopReports(t *testing.T) {
	_, v, _ := artifactStopFixture(t)
	cause := artifactStopCause(t, v.cause, v.scope)
	for _, mode := range []string{"stopped", "artifact-pending", "object-pending", "pending-unbound", "unknown-over-failed", "failed", "wrong-component", "foreign-cause", "empty-report"} {
		t.Run(mode, func(t *testing.T) {
			ad, od := oc.ObjectStopDetails{State: oc.ProjectStopped}, oc.ObjectStopDetails{State: oc.ProjectStopped}
			want, reason := c.Stopped, c.SafeReason("")
			switch mode {
			case "artifact-pending":
				ad.State = oc.ProjectStopPending
				want, reason = c.StopPending, c.ReasonWorkPending
			case "object-pending":
				od.State = oc.ProjectStopPending
				od.ActiveRefs = []oc.ProjectStopRef{{Kind: oc.StopObjectLease, ID: testID[oc.ProjectStopResource](t)}}
				want, reason = c.StopPending, c.ReasonWorkPending
			case "pending-unbound":
				ad.State, ad.SafeReason = oc.ProjectStopPending, oc.ProjectStopDependencyUnbound
				want, reason = c.StopPending, c.ReasonDependencyUnbound
			case "unknown-over-failed":
				ad.State, ad.SafeReason = oc.ProjectStopFailed, oc.ProjectStopDependencyUnavailable
				od.State = oc.ProjectStopPending
				od.UnknownRefs = []oc.ProjectStopRef{{Kind: oc.StopObjectAttempt, ID: testID[oc.ProjectStopResource](t)}}
				want, reason = c.StopPending, c.ReasonOutcomeUnknown
			case "failed":
				ad.State, ad.SafeReason = oc.ProjectStopFailed, oc.ProjectStopOperationFailed
				want, reason = c.StopFailed, c.ReasonOperationFailed
			}
			reports := [2]oc.ObjectStopReport{artifactStopReport(t, oc.ArtifactStopComponent, cause, ad), artifactStopReport(t, oc.ObjectStopComponent, cause, od)}
			reject := false
			switch mode {
			case "wrong-component":
				reports[0] = reports[1]
				reject = true
			case "foreign-cause":
				d := cause.Details()
				d.ProjectVersion++
				foreign, err := oc.NewProjectStopCause(d)
				if err != nil {
					t.Fatal(err)
				}
				reports[1] = artifactStopReport(t, oc.ObjectStopComponent, foreign, od)
				reject = true
			case "empty-report":
				reports[1] = oc.ObjectStopReport{}
				reject = true
			}
			got, err := artifactObjectStopReport(v.cause, v.scope, cause, reports)
			if reject {
				if err == nil || got.Details().State != "" {
					t.Fatal("unbound child report accepted")
				}
				return
			}
			if err != nil || !got.Matches(c.ArtifactObjectParticipant, v.cause, v.scope) || got.Details().State != want || got.Details().SafeReason != reason || len(got.Details().ActiveRefs)+len(got.Details().UnknownRefs) != 0 {
				t.Fatal("incorrect safe combined state", got.Details(), err)
			}
		})
	}
}
func TestArtifactObjectStopPreflightAndSequence(t *testing.T) {
	for _, mode := range []string{"inspect-success", "preflight-unknown", "rollback", "committed-without-callback", "request-rejects-deleted-receipt"} {
		t.Run(mode, func(t *testing.T) {
			p, v, calls := artifactStopFixture(t)
			attempt := testID[f.TransactionAttempt](t)
			if mode != "inspect-success" && mode != "request-rejects-deleted-receipt" {
				v.store.within = func(_ context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
					switch mode {
					case "preflight-unknown":
						return f.UnknownResult(attempt, cause)
					case "rollback":
						return f.NotCommittedResult(fault(f.InvalidState))
					default:
						return f.CommittedResult()
					}
				}
			}
			var report c.StopReport
			var err error
			if mode == "request-rejects-deleted-receipt" {
				report, err = p.RequestStop(context.Background(), v.actor, v.cause, v.scope)
			} else {
				report, err = p.InspectStop(context.Background(), v.actor, v.cause, v.scope)
			}
			if mode == "inspect-success" {
				if err != nil || report.Details().State != c.Stopped || *calls != [2]int{1, 1} {
					t.Fatal("both original owners not consumed", *calls, err)
				}
				return
			}
			if err == nil || report.Details().State != "" || *calls != [2]int{} {
				t.Fatal("unconfirmed preflight reached original owners", err)
			}
			if mode == "preflight-unknown" {
				saved, ok := UnknownAttempt(err)
				if !ok || saved.AttemptID() != attempt || saved.Cause().Details().Owner != "project.artifact-object-stop-preflight" {
					t.Fatal("original preflight Unknown lost")
				}
			}
		})
	}
}
func TestArtifactObjectStopOriginalReturns(t *testing.T) {
	for _, mode := range []string{"artifact-return", "object-return", "completed-with-error", "provider-panic"} {
		t.Run(mode, func(t *testing.T) {
			_, v, _ := artifactStopFixture(t)
			cause := artifactStopCause(t, v.cause, v.scope)
			sentinel := errors.New("original stop failure")
			calls := [2]int{}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			ctx, cancel := context.WithCancel(context.Background())
			type outcome struct {
				reports [2]oc.ObjectStopReport
				err     error
			}
			done := make(chan outcome, 1)
			joined := false
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("original stop not returned")
					}
				}
			})
			port := func(n int, component oc.ProjectStopComponent) artifactStopControl {
				return func(_ context.Context, _ identity.Actor, got oc.ProjectStopCause, inspect bool) (oc.ObjectStopReport, error) {
					calls[n]++
					if inspect || !got.Equal(cause) {
						t.Error("RequestStop input changed")
					}
					if n == 0 && mode == "provider-panic" {
						panic("controlled provider")
					}
					if n == 0 && mode == "completed-with-error" {
						return artifactStopReport(t, component, got, oc.ObjectStopDetails{State: oc.ProjectStopped}), sentinel
					}
					if n == 0 && mode == "artifact-return" || n == 1 && mode == "object-return" {
						close(entered)
						<-release
					}
					return artifactStopReport(t, component, got, oc.ObjectStopDetails{State: oc.ProjectStopped}), nil
				}
			}
			go func() {
				reports, err := callArtifactObjectStop(ctx, v.actor, cause, false, port(0, oc.ArtifactStopComponent), port(1, oc.ObjectStopComponent))
				done <- outcome{reports, err}
			}()
			if mode == "artifact-return" || mode == "object-return" {
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("original component not entered")
				}
				cancel()
				select {
				case <-done:
					joined = true
					t.Fatal("cancel replaced original component return")
				default:
				}
				free()
			}
			var out outcome
			select {
			case out = <-done:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("original component did not return")
			}
			if out.err == nil || out.reports[0].Validate() == nil || out.reports[1].Validate() == nil {
				t.Fatal("failed/cancelled call published successful reports")
			}
			if mode == "completed-with-error" && !errors.Is(out.err, sentinel) {
				t.Fatal("original error lost", out.err)
			}
			if mode != "object-return" && calls[1] != 0 {
				t.Fatal("second owner started after failed/cancelled first")
			}
			if mode == "object-return" && calls != [2]int{1, 1} {
				t.Fatal("wrong original owner sequence")
			}
		})
	}
}
