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
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Controlled ports prove fixed dispatch and original return ownership. They do
// not manufacture the real pair's bound guard, canonical claim or SQL outcome.
func cleanupVariablesFixture(t *testing.T) (*LifecycleCleanupDriver, *LifecycleCleanupDriver, lifecycleCleanupRound, *cleanupSkillsPort, *cleanupSkillsPort) {
	t.Helper()
	store := &stopRoundGateStore{}
	skills, variables := &cleanupSkillsPort{}, &cleanupSkillsPort{}
	old, projects, round := cleanupSkillsFixture(t, store, skills)
	agents := &cleanupAgentsPort{}
	legacy, err := NewLifecycleCleanupDriverWithAgents(store, projects, old.state.processes, agents, skills)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := NewLifecycleCleanupDriverWithVariables(store, projects, old.state.processes, agents, skills, variables)
	if err != nil || store.transactions != 0 || driver.state.authority != old.state.authority || driver.state.skills != skills || driver.state.variables != variables || driver.state.agents != agents {
		t.Fatal("fixed construction lost original owners", err)
	}
	if _, err = NewLifecycleCleanupDriverWithVariables(store, projects, old.state.processes, agents, skills, (*cleanupSkillsPort)(nil)); err == nil {
		t.Fatal("typed nil Variables provider accepted")
	}
	return driver, legacy, round, skills, variables
}

func cleanupVariablesReady(t *testing.T, round lifecycleCleanupRound) *c.CleanupCheckpoint {
	t.Helper()
	data, _ := cleanupAdvancedAgentCheckpoint(t, round)
	data.AgentsDone, data.Skills.State = true, c.CleanupCompleted
	report, err := agentsCleanupReport(round, data)
	if err != nil {
		t.Fatal(err)
	}
	return report.Details().Checkpoint
}

func TestLifecycleCleanupVariablesFixedOrder(t *testing.T) {
	driver, legacy, round, skills, variables := cleanupVariablesFixture(t)
	for _, stage := range []string{"agent", "skills", "variables", "completed"} {
		t.Run(stage, func(t *testing.T) {
			var cp *c.CleanupCheckpoint
			wantAgent, wantVariables, wantReplay := false, false, false
			switch stage {
			case "agent":
				wantAgent = true
			case "skills":
				data, err := decodeAgentsCleanupCheckpoint(nil, round.cause, round.claim.project)
				if err != nil {
					t.Fatal(err)
				}
				data.AgentsDone = true
				report, err := agentsCleanupReport(round, data)
				if err != nil {
					t.Fatal(err)
				}
				cp = report.Details().Checkpoint
			case "variables":
				cp, wantVariables = cleanupVariablesReady(t, round), true
			case "completed":
				r := round
				r.checkpoint = cleanupVariablesReady(t, round)
				report, err := variablesParentCleanupReport(r, cleanupSkillsChild(t, round, c.CleanupCompleted, nil))
				if err != nil {
					t.Fatal(err)
				}
				cp, wantReplay = report.Details().Checkpoint, true
			}
			a, v, replay, err := driver.state.selectDomainStep(cp, round.cause, round.claim.project)
			if err != nil || a != wantAgent || v != wantVariables || replay != wantReplay {
				t.Fatal("selected a later or incorrect domain", a, v, replay, err)
			}
		})
	}
	data, err := decodeAgentsCleanupCheckpoint(nil, round.cause, round.claim.project)
	if err != nil {
		t.Fatal(err)
	}
	data.AgentsDone = true
	initial, err := agentsCleanupReport(round, data)
	if err != nil {
		t.Fatal(err)
	}
	round.checkpoint = initial.Details().Checkpoint
	skillCalls, variableCalls := 0, 0
	skills.call = func(context.Context, i.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
		skillCalls++
		return cleanupSkillsChild(t, round, c.CleanupCompleted, nil), nil
	}
	variables.call = func(_ context.Context, _ i.Actor, _ c.LifecycleCause, _ c.ScopeRef, cp *c.CleanupCheckpoint) (c.CleanupReport, error) {
		variableCalls++
		if cp != nil {
			t.Error("parent checkpoint leaked into the Variables owner")
		}
		return cleanupSkillsChild(t, round, c.CleanupCompleted, nil), nil
	}
	first, err := driver.state.invoke(context.Background(), round)
	if err != nil || skillCalls != 1 || variableCalls != 0 {
		t.Fatal("Skills return automatically entered Variables", err)
	}
	// Production start reads this only after its original checkpoint Store has
	// returned. This control provides that input; PG proves the actual commit.
	round.checkpoint = first.Details().Checkpoint
	_, round.variablesStep, _, err = driver.state.selectDomainStep(round.checkpoint, round.cause, round.claim.project)
	if err != nil || !round.variablesStep {
		t.Fatal("next explicit round could not select Variables", err)
	}
	last, err := driver.state.invoke(context.Background(), round)
	if err != nil || skillCalls != 1 || variableCalls != 1 || last.Details().State != c.CleanupPending || last.Details().Checkpoint.Schema() != 5 {
		t.Fatal("Variables completed its parent or reentered Skills", err)
	}
	if _, _, _, err = legacy.state.selectDomainStep(last.Details().Checkpoint, round.cause, round.claim.project); err == nil {
		t.Fatal("old Agent/Skills constructor consumed Variables progress")
	}
}

func TestLifecycleCleanupVariablesCheckpoint(t *testing.T) {
	_, _, round, _, _ := cleanupVariablesFixture(t)
	round.checkpoint = cleanupVariablesReady(t, round)
	original := round.checkpoint.Bytes()
	for _, state := range []c.CleanupState{c.CleanupPending, c.CleanupCompleted} {
		report, err := variablesParentCleanupReport(round, cleanupSkillsChild(t, round, state, nil))
		if err != nil {
			t.Fatal(err)
		}
		data, agents, err := decodeVariablesCleanupCheckpoint(report.Details().Checkpoint, round.cause, round.claim.project)
		if err != nil || data.State != state || !bytes.Equal(data.AgentsBytes, original) || data.AgentsSchema != 4 || agents.Applied.BeforeUpdatedAt == nil || report.Details().State != c.CleanupPending {
			t.Fatal("lost prior exact anchor or confused child and parent completion", err)
		}
	}
	base, err := variablesParentCleanupReport(round, cleanupSkillsChild(t, round, c.CleanupPending, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"wrong-project", "unknown-field", "duplicate-state", "missing-anchor", "skills-pending", "recursive-schema"} {
		t.Run(mode, func(t *testing.T) {
			raw := base.Details().Checkpoint.Bytes()
			var data variablesCleanupCheckpoint
			if err := json.Unmarshal(raw, &data); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "wrong-project":
				data.Project = testID[i.Project](t).String()
			case "missing-anchor":
				data.AgentsBytes = nil
			case "recursive-schema":
				data.AgentsSchema = 5
			case "skills-pending":
				agents, err := decodeAgentsCleanupCheckpoint(round.checkpoint, round.cause, round.claim.project)
				if err != nil {
					t.Fatal(err)
				}
				agents.Skills.State = c.CleanupPending
				data.AgentsBytes, err = json.Marshal(agents)
				if err != nil {
					t.Fatal(err)
				}
			}
			raw, err = json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "unknown-field" {
				raw = append(raw[:len(raw)-1], []byte(`,"extra":true}`)...)
			} else if mode == "duplicate-state" {
				raw = append(raw[:len(raw)-1], []byte(`,"variables_state":"pending"}`)...)
			}
			cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, 5, raw)
			if err != nil {
				t.Fatal("invalid public fixture", err)
			}
			if _, _, err = decodeVariablesCleanupCheckpoint(&cp, round.cause, round.claim.project); err == nil {
				t.Fatal("nonexact Variables progress accepted")
			}
		})
	}
	childCP, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, 1, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = variablesParentCleanupReport(round, cleanupSkillsChild(t, round, c.CleanupPending, &childCP)); err == nil {
		t.Fatal("provider introduced an unsupported cursor")
	}
}

func TestLifecycleCleanupVariablesScopeFamilies(t *testing.T) {
	driver, _, round, _, _ := cleanupVariablesFixture(t)
	for _, mode := range []string{"no-scope", "skills", "agent", "both", "closed"} {
		t.Run(mode, func(t *testing.T) {
			r := round
			r.agentStep, r.variablesStep = mode == "agent" || mode == "both", mode == "both" || mode == "closed"
			w := &lifecycleCleanupScope{state: driver.state, round: r, open: mode != "closed"}
			ctx := context.Background()
			if mode != "no-scope" {
				ctx = context.WithValue(ctx, lifecycleCleanupScopeKey{}, w)
			}
			hasCode(t, driver.state.authority.requireVariablesCleanupClaimInTx(ctx, f.NewTx(), round.cause, round.claim.project), f.Forbidden)
		})
	}
	round.variablesStep = true
	w := &lifecycleCleanupScope{state: driver.state, round: round, open: true}
	ctx := context.WithValue(context.Background(), lifecycleCleanupScopeKey{}, w)
	hasCode(t, driver.state.authority.requireSkillsCleanupClaimInTx(ctx, f.NewTx(), round.cause, round.claim.project), f.Forbidden)
	hasCode(t, driver.state.authority.requireAgentCleanupStepInTx(ctx, f.NewTx(), round.cause, round.claim.project), f.Forbidden)
}

func TestLifecycleCleanupVariablesOriginalReturn(t *testing.T) {
	for _, mode := range []string{"pending", "completed", "error", "unknown", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			driver, _, round, skills, variables := cleanupVariablesFixture(t)
			round.checkpoint, round.variablesStep = cleanupVariablesReady(t, round), true
			skills.call = func(context.Context, i.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
				t.Error("Variables round reentered Skills")
				return c.CleanupReport{}, errors.New("wrong provider")
			}
			entered, release := make(chan struct{}), make(chan struct{})
			failure := errors.New("original Variables error")
			cause, err := f.NewJobCause("variables-cleanup", round.claim.operation.String(), "batch")
			if err != nil {
				t.Fatal(err)
			}
			unknown := f.UnknownResult(testID[f.TransactionAttempt](t), cause)
			var saved context.Context
			variables.call = func(ctx context.Context, actor i.Actor, cause c.LifecycleCause, scope c.ScopeRef, cp *c.CleanupCheckpoint) (c.CleanupReport, error) {
				if actor.Details().CauseRef != round.claim.operation.String() || cause != round.cause || scope.ProjectID != round.claim.project || cp != nil {
					t.Error("lost original provider binding")
				}
				saved = ctx
				close(entered)
				<-release
				report := cleanupSkillsChild(t, round, c.CleanupCompleted, nil)
				switch mode {
				case "pending":
					report = cleanupSkillsChild(t, round, c.CleanupPending, nil)
				case "error":
					return report, failure
				case "unknown":
					return report, commitError(unknown)
				}
				return report, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type result struct {
				report c.CleanupReport
				err    error
			}
			done := make(chan result, 1)
			joined := false
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			t.Cleanup(func() {
				free()
				if !joined {
					<-done
				}
			})
			go func() { r, e := driver.state.invoke(ctx, round); done <- result{r, e} }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("provider not entered")
			}
			if mode == "cancelled" {
				cancel()
			}
			select {
			case <-done:
				joined = true
				t.Fatal("detached original provider return")
			default:
			}
			free()
			var got result
			select {
			case got = <-done:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("original provider did not return")
			}
			switch mode {
			case "error":
				if !errors.Is(got.err, failure) {
					t.Fatal("original error replaced", got.err)
				}
			case "unknown":
				actual, ok := UnknownAttempt(got.err)
				if !ok || actual.AttemptID() != unknown.AttemptID() {
					t.Fatal("original Unknown attempt replaced", got.err)
				}
			case "cancelled":
				if !errors.Is(got.err, context.Canceled) {
					t.Fatal("late success ignored cancellation", got.err)
				}
			default:
				if got.err != nil || got.report.Details().State != c.CleanupPending {
					t.Fatal("child completion became parent completion", got.err)
				}
			}
			if got.err != nil && got.report.Matches(c.SkillsParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}) {
				t.Fatal("failed call published progress")
			}
			hasCode(t, driver.state.authority.requireVariablesCleanupClaimInTx(context.WithoutCancel(saved), f.NewTx(), round.cause, round.claim.project), f.Forbidden)
		})
	}
}
