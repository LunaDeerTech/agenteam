package project

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The controlled Runtime owns an exact context marker. These tests prove the
// Project adapter's checks, not a production Execution/Operation witness.
type artifactRunTestKey struct{}
type artifactRunTestAuthority struct {
	tx      f.Tx
	subject ac.AccessSubject
	marker  *int
	deny    error
	checks  int
}

func (a *artifactRunTestAuthority) match(ctx context.Context, s ac.AccessSubject) error {
	if ctx.Value(artifactRunTestKey{}) != a.marker || !s.Actor.Equal(a.subject.Actor) || s.ProjectID != a.subject.ProjectID || s.ExecutionID != a.subject.ExecutionID {
		return fault(f.Forbidden)
	}
	if s.OperationID != "" || s.ToolID != "" || s.ToolCallID != "" {
		if s.OperationID != a.subject.OperationID || s.ToolID != a.subject.ToolID || s.ToolCallID != a.subject.ToolCallID {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (a *artifactRunTestAuthority) DiscoverArtifactRun(ctx context.Context, s ac.AccessSubject) (oc.AccessDependencies, error) {
	if err := a.match(ctx, s); err != nil {
		return oc.AccessDependencies{}, err
	}
	key, _ := f.AggregateLock(f.ExecutionAggregate, s.ExecutionID)
	return oc.NewAccessDependencies(digest([]byte("actual-run-dependencies")), []f.LockRequest{{Key: key, Mode: f.Shared}})
}
func (a *artifactRunTestAuthority) DiscoverArtifactRunInTx(ctx context.Context, tx f.Tx, s ac.AccessSubject) (oc.AccessDependencies, error) {
	if tx != a.tx {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	return a.DiscoverArtifactRun(ctx, s)
}
func (a *artifactRunTestAuthority) RequireArtifactRunInTx(ctx context.Context, tx f.Tx, s ac.AccessSubject, intent i.AccessIntent) error {
	a.checks++
	if tx != a.tx || intent != i.Read && intent != i.Mutate {
		return fault(f.Forbidden)
	}
	if err := a.match(ctx, s); err != nil {
		return err
	}
	return a.deny
}

func TestArtifactAgentCurrentAuthorityAndProjectGate(t *testing.T) {
	for _, mode := range []string{"current", "object-owner-subject", "no-witness", "changed-operation", "current-denied", "archived", "uninitialized", "converge", "unbound"} {
		t.Run(mode, func(t *testing.T) {
			v := newAuditGateFixture(t, nil)
			execution := testID[i.Execution](t)
			actor, _ := i.NewAgentRun(v.project, testID[i.Agent](t), execution)
			subject := ac.AccessSubject{Actor: actor, ProjectID: v.project, ExecutionID: execution.String(), OperationID: testID[struct{}](t).String(), ToolID: testID[struct{}](t).String(), ToolCallID: testID[struct{}](t).String()}
			runs := &artifactRunTestAuthority{tx: v.store.tx, subject: subject, marker: new(int)}
			a, err := NewArtifactAuthority(v.a, runs)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), artifactRunTestKey{}, runs.marker)
			intent, want := i.Read, f.Code("")
			switch mode {
			case "object-owner-subject":
				subject.OperationID, subject.ToolID, subject.ToolCallID = "", "", ""
			case "no-witness":
				ctx = context.Background()
				want = f.Forbidden
			case "changed-operation":
				subject.OperationID = testID[struct{}](t).String()
				want = f.Forbidden
			case "current-denied":
				runs.deny = fault(f.ConfirmationStale)
				want = f.ConfirmationStale
			case "archived":
				v.lifecycle = c.Archived
				want = f.ProjectNotActive
			case "uninitialized":
				v.initialized = false
				want = f.ProjectNotActive
			case "converge":
				intent = i.Converge
				want = f.Forbidden
			case "unbound":
				a, _ = NewArtifactAuthority(v.a)
				want = f.DependencyUnbound
			}
			grant, err := a.AuthorizeInTx(ctx, v.store.tx, subject, intent)
			if want != "" {
				hasCode(t, err, want)
				return
			}
			scope, _ := i.InProject(v.project)
			if err != nil || !grant.Matches(actor, scope, intent) || runs.checks != 1 {
				t.Fatal("current AgentRun gate failed", err, runs.checks)
			}
			deps, err := a.Discover(ctx, subject)
			if err != nil || len(deps.Locks()) != 2 {
				t.Fatal("Project and Execution lock union missing", err)
			}
			for _, step := range v.order {
				if step == "session" {
					t.Fatal("AgentRun inherited Human Session authority")
				}
			}
		})
	}
}
