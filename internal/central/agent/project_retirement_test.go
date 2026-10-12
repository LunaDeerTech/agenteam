package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type retirementGateControl struct{ c.ProjectRetirementGate }
type retirementRefsControl struct {
	c.ProjectRetirementReferences
	kind c.ProjectRetirementReferenceKind
}

func (p *retirementRefsControl) ProjectRetirementReferenceKind() c.ProjectRetirementReferenceKind {
	return p.kind
}
func (p *retirementRefsControl) ProjectRetirementLocks(change c.ProjectRetirementChange) ([]f.LockRequest, error) {
	return change.Request.RequiredLocks(), change.Validate()
}
func retirementDependencies() ProjectRetirementDependencies {
	return ProjectRetirementDependencies{Work: &retirementGateControl{}, Execution: &retirementGateControl{}, Models: &retirementRefsControl{kind: c.ProjectRetirementModelReferences}, Tools: &retirementRefsControl{kind: c.ProjectRetirementToolReferences}, Mounts: &retirementRefsControl{kind: c.ProjectRetirementMountReferences}, Secrets: &retirementRefsControl{kind: c.ProjectRetirementSecretReferences}, Skills: &retirementRefsControl{kind: c.ProjectRetirementSkillReferences}}
}
func retirementPlan(t *testing.T) c.ProjectRetirementPlan {
	t.Helper()
	_, before := skillAssignmentRequestFixture(t)
	core := before.Fields().Core
	op := retirementPreparationID[pc.Operation](t)
	registration, _ := i.RegisterService(i.ProjectLifecycle)
	scope, _ := i.InProject(core.ProjectID)
	actor, err := registration.Actor(op.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := c.ProjectRetirementBeforeDigest(before)
	if err != nil {
		t.Fatal(err)
	}
	plan := c.ProjectRetirementPlan{Change: c.ProjectRetirementChange{Request: c.ProjectRetirementRequest{Actor: actor, ProjectID: core.ProjectID, AgentID: core.ID, Cause: pc.LifecycleCause{OperationID: op, Action: pc.Delete, ProjectVersion: 2}, ExpectedVersion: core.Version}, Before: before}, CommandID: retirementPreparationID[c.AgentCommand](t), BeforeDigest: digest}
	if err = plan.Validate(); err != nil {
		t.Fatal(err)
	}
	return plan
}
func retirementConsumer(t *testing.T, s Store) (*ProjectRetirement, *Authority) {
	t.Helper()
	projects := &project.Authority{}
	agents, err := NewAuthority(s, projects)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := NewProjectRetirement(agents, projects, retirementDependencies())
	if err != nil {
		t.Fatal(err)
	}
	return consumer, agents
}
func TestAgentProjectRetirementRequiresAllOwners(t *testing.T) {
	s := &retirementPreparationStore{}
	r, a := retirementConsumer(t, s)
	for _, tc := range []struct {
		name string
		edit func(*ProjectRetirementDependencies)
	}{
		{"work", func(d *ProjectRetirementDependencies) { d.Work = nil }}, {"execution", func(d *ProjectRetirementDependencies) { d.Execution = nil }},
		{"models", func(d *ProjectRetirementDependencies) { d.Models = nil }}, {"tools", func(d *ProjectRetirementDependencies) { d.Tools = nil }},
		{"mounts", func(d *ProjectRetirementDependencies) { d.Mounts = nil }}, {"secrets", func(d *ProjectRetirementDependencies) { d.Secrets = nil }},
		{"skills", func(d *ProjectRetirementDependencies) { d.Skills = nil }}, {"wrong-kind", func(d *ProjectRetirementDependencies) { d.Models = d.Tools }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deps := retirementDependencies()
			tc.edit(&deps)
			if _, err := NewProjectRetirement(a, r.projects, deps); err == nil {
				t.Fatal("incomplete retirement assembled")
			}
		})
	}
	if s.borrows != 0 {
		t.Fatal("constructor used storage")
	}
	locks, err := r.ProjectRetirementLocks(retirementPlan(t))
	if err != nil || len(locks) != 3 {
		t.Fatal("complete normalized mandatory union", err)
	}
	for _, lock := range locks {
		if lock.Mode != f.Exclusive {
			t.Fatal("retirement weakened mandatory locks")
		}
	}
}
func TestAgentProjectRetirementWitnessCannotBeReused(t *testing.T) {
	s := &retirementPreparationStore{}
	r, a := retirementConsumer(t, s)
	plan := retirementPlan(t)
	tx := f.NewTx()
	for _, name := range []string{"public-prepared", "public-applied", "closed-prepared", "closed-applied", "wrong-tx", "wrong-before", "recovery-not-prepared"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			change := plan.Change.Clone()
			useTx := tx
			applied := name == "public-applied" || name == "closed-applied"
			if name != "public-prepared" && name != "public-applied" {
				w := &projectRetirementWitness{open: true, consumer: r, tx: tx, plan: plan.Clone(), applied: applied}
				if name == "closed-prepared" || name == "closed-applied" {
					w.close()
				}
				if name == "wrong-tx" {
					useTx = f.NewTx()
				}
				if name == "wrong-before" {
					fields := change.Before.Fields()
					fields.Core.Description = "another full preimage"
					var err error
					change.Before, err = c.NewAgentConfig(fields)
					if err != nil {
						t.Fatal(err)
					}
				}
				if name == "recovery-not-prepared" {
					w.applied = true
					w.recovery = true
				}
				ctx = context.WithValue(ctx, projectRetirementWitnessKey{}, w)
			}
			var err error
			if applied {
				err = a.RequireProjectRetirementAppliedInTx(ctx, useTx, change)
			} else {
				err = a.RequireProjectRetirementPreparedInTx(ctx, useTx, change)
			}
			requireCode(t, err, f.Forbidden)
			if s.borrows != 0 {
				t.Fatal("invalid witness borrowed storage")
			}
		})
	}
}

type retirementReadStore struct {
	Store
	entered, release chan struct{}
	result           f.CommitResult
}

func (s *retirementReadStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
	return s.result
}
func TestAgentProjectRetirementReadNeedsActualReturn(t *testing.T) {
	for _, mode := range []string{"discovery", "restore"} {
		t.Run(mode, func(t *testing.T) {
			plan := retirementPlan(t)
			req := plan.Change.Request
			cause, _ := f.NewRecoveryCause("retirement-control", req.Cause.OperationID.String(), req.ProjectID.String())
			unknown := f.UnknownResult(retirementPreparationID[f.TransactionAttempt](t), cause)
			store := &retirementReadStore{entered: make(chan struct{}), release: make(chan struct{}), result: unknown}
			r, _ := retirementConsumer(t, store)
			ctx, cancel := context.WithCancel(context.Background())
			returned := make(chan error, 1)
			joined := false
			var once sync.Once
			free := func() { once.Do(func() { close(store.release) }) }
			t.Cleanup(func() {
				cancel()
				free()
				if !joined {
					select {
					case <-returned:
					case <-time.After(5 * time.Second):
						t.Error("original read still running")
					}
				}
			})
			go func() {
				if mode == "discovery" {
					value, err := r.DiscoverProjectRetirement(ctx, req.Actor, req.Cause, req.ProjectID)
					if value != nil {
						t.Error("Unknown read returned a plan")
					}
					returned <- err
				} else {
					value, err := r.RestoreProjectRetirement(ctx, req.Actor, req.Cause, req.ProjectID, req.AgentID, req.ExpectedVersion, plan.CommandID, plan.BeforeDigest)
					if value.Validate() == nil {
						t.Error("Unknown restoration returned a plan")
					}
					returned <- err
				}
			}()
			select {
			case <-store.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("read not entered")
			}
			cancel()
			select {
			case <-returned:
				joined = true
				t.Fatal("cancel faked original return")
			default:
			}
			free()
			var err error
			select {
			case err = <-returned:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("read not returned")
			}
			result, ok := UnknownAttempt(err)
			if !ok || result.AttemptID() != unknown.AttemptID() {
				t.Fatal("lost original Unknown", err)
			}
		})
	}
}
func TestAgentProjectRetirementReadRejectsFalseCommit(t *testing.T) {
	plan := retirementPlan(t)
	req := plan.Change.Request
	store := &retirementReadStore{result: f.CommittedResult()}
	r, _ := retirementConsumer(t, store)
	value, err := r.DiscoverProjectRetirement(context.Background(), req.Actor, req.Cause, req.ProjectID)
	requireCode(t, err, f.DependencyUnavailable)
	if value != nil {
		t.Fatal("skipped callback returned plan")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.DiscoverProjectRetirement(ctx, req.Actor, req.Cause, req.ProjectID)
	if err == nil {
		t.Fatal("cancelled read succeeded")
	}
}
