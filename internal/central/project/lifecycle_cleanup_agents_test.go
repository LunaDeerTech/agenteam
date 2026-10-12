package project

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type cleanupAgentsPort struct {
	ac.ProjectRetirementConsumer
	discover func(context.Context, i.Actor, c.LifecycleCause, i.ProjectID) (*ac.ProjectRetirementPlan, error)
	empty    func(context.Context, f.Tx, i.Actor, c.LifecycleCause, i.ProjectID) error
}

func (p *cleanupAgentsPort) DiscoverProjectRetirement(ctx context.Context, a i.Actor, cause c.LifecycleCause, id i.ProjectID) (*ac.ProjectRetirementPlan, error) {
	return p.discover(ctx, a, cause, id)
}
func (p *cleanupAgentsPort) RequireProjectAgentsEmptyInTx(ctx context.Context, tx f.Tx, a i.Actor, cause c.LifecycleCause, id i.ProjectID) error {
	return p.empty(ctx, tx, a, cause, id)
}

func TestLifecycleCleanupAgentsConstructionAndCheckpoint(t *testing.T) {
	store := &stopRoundGateStore{}
	old, projects, round := cleanupSkillsFixture(t, store, &cleanupSkillsPort{})
	port := &cleanupAgentsPort{}
	driver, err := NewLifecycleCleanupDriverWithAgents(store, projects, old.state.processes, port, old.state.skills)
	if err != nil || driver.state.authority != old.state.authority || driver.state.agents != port || driver.state.audit != nil || driver.state.participant != nil || store.transactions != 0 {
		t.Fatal("fixed construction", err)
	}
	t.Run("missing-consumer", func(t *testing.T) {
		if _, err := NewLifecycleCleanupDriverWithAgents(store, projects, old.state.processes, (*cleanupAgentsPort)(nil), old.state.skills); err == nil {
			t.Fatal("missing consumer accepted")
		}
	})
	data, err := decodeAgentsCleanupCheckpoint(nil, round.cause, round.claim.project)
	if err != nil {
		t.Fatal(err)
	}
	data.Applied = &agentCleanupApplied{Agent: testID[i.Agent](t), Version: 1, Command: testID[ac.AgentCommand](t), Digest: f.Digest("sha256:" + string(bytes.Repeat([]byte("a"), 64)))}
	for _, done := range []bool{false, true} {
		t.Run(map[bool]string{false: "one-agent", true: "agents-empty"}[done], func(t *testing.T) {
			data.AgentsDone = done
			report, err := agentsCleanupReport(round, data)
			if err != nil {
				t.Fatal(err)
			}
			if report.Details().State != c.CleanupPending || report.Details().Checkpoint.Schema() != 3 {
				t.Fatal("retirement completed aggregate")
			}
			got, err := decodeAgentsCleanupCheckpoint(report.Details().Checkpoint, round.cause, round.claim.project)
			if err != nil || got.Applied == nil || *got.Applied != *data.Applied || got.AgentsDone != done {
				t.Fatal("lost exact applied anchor", err)
			}
			if _, _, err := old.state.skillsProgress(report.Details().Checkpoint, round.cause, round.claim.project); err == nil {
				t.Fatal("legacy Skills owner consumed Agent checkpoint")
			}
		})
	}
	data.AgentsDone = true
	initial, err := agentsCleanupReport(round, data)
	if err != nil {
		t.Fatal(err)
	}
	round.checkpoint = initial.Details().Checkpoint
	child := cleanupSkillsChild(t, round, c.CleanupCompleted, nil)
	parent, err := skillsParentCleanupReport(round, child)
	if err != nil {
		t.Fatal(err)
	}
	combined, err := agentsSkillsCleanupReport(round, parent)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeAgentsCleanupCheckpoint(combined.Details().Checkpoint, round.cause, round.claim.project)
	if err != nil || !got.AgentsDone || got.Skills.State != c.CleanupCompleted || combined.Details().State != c.CleanupPending || *got.Applied != *data.Applied {
		t.Fatal("child completion lost anchor or completed parent", err)
	}
}
func TestLifecycleCleanupAgentsRejectsCheckpointDrift(t *testing.T) {
	_, _, round := cleanupSkillsFixture(t, &stopRoundGateStore{}, &cleanupSkillsPort{})
	data, err := decodeAgentsCleanupCheckpoint(nil, round.cause, round.claim.project)
	if err != nil {
		t.Fatal(err)
	}
	report, err := agentsCleanupReport(round, data)
	if err != nil {
		t.Fatal(err)
	}
	original := report.Details().Checkpoint.Bytes()
	for _, tc := range []struct {
		name string
		edit func([]byte) []byte
	}{
		{"unknown-field", func(b []byte) []byte { return append(b[:len(b)-1], []byte(`,"retired":true}`)...) }},
		{"duplicate", func(b []byte) []byte { return append(b[:len(b)-1], []byte(`,"agents_done":false}`)...) }},
		{"null-anchor", func(b []byte) []byte { return append(b[:len(b)-1], []byte(`,"agent_applied":null}`)...) }},
		{"foreign-project", func(b []byte) []byte {
			return bytes.Replace(b, []byte(round.claim.project.String()), []byte(testID[i.Project](t).String()), 1)
		}},
		{"skills-before-agents", func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"skills_state":"pending"`), []byte(`"skills_state":"completed"`), 1)
		}},
		{"bad-anchor", func(b []byte) []byte {
			// Use valid JSON in canonical field order, with only the Agent ID
			// invalid. Zero typed IDs/versions would fail Marshal before the
			// checkpoint's typed parser was ever reached.
			anchor := struct {
				Agent   string `json:"agent_id"`
				Version string `json:"version"`
				Command string `json:"command_id"`
				Digest  string `json:"before_digest"`
			}{"invalid-agent-id", "1", testID[ac.AgentCommand](t).String(), "sha256:" + string(bytes.Repeat([]byte("a"), 64))}
			raw, err := json.Marshal(anchor)
			if err != nil {
				t.Fatal(err)
			}
			field := append([]byte(`"agent_applied":`), raw...)
			field = append(field, []byte(`,"skills_progress":`)...)
			return bytes.Replace(b, []byte(`"skills_progress":`), field, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.edit(bytes.Clone(original))
			cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, 3, raw)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeAgentsCleanupCheckpoint(&cp, round.cause, round.claim.project); err == nil {
				t.Fatal("invalid exact checkpoint accepted")
			}
		})
	}
}
func TestLifecycleCleanupAgentsOriginalDiscoveryAndScope(t *testing.T) {
	failure := errors.New("original discovery failed")
	store := &stopRoundGateStore{commit: f.NotCommittedResult(fault(f.InvalidState))}
	old, projects, round := cleanupSkillsFixture(t, store, &cleanupSkillsPort{})
	entered, release := make(chan struct{}), make(chan struct{})
	var saved context.Context
	port := &cleanupAgentsPort{discover: func(ctx context.Context, _ i.Actor, cause c.LifecycleCause, p i.ProjectID) (*ac.ProjectRetirementPlan, error) {
		saved = ctx
		if cause != round.cause || p != round.claim.project {
			t.Error("lost original scope")
		}
		close(entered)
		<-release
		return nil, failure
	}}
	driver, err := NewLifecycleCleanupDriverWithAgents(store, projects, old.state.processes, port, old.state.skills)
	if err != nil {
		t.Fatal(err)
	}
	round.agentStep = true
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan error, 1)
	joined := false
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		cancel()
		free()
		if !joined {
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("original discovery still running")
			}
		}
	})
	go func() { _, err := driver.state.runAgentStep(ctx, round); returned <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("discovery not entered")
	}
	cancel()
	select {
	case <-returned:
		joined = true
		t.Fatal("cancel substituted original return")
	default:
	}
	free()
	select {
	case err = <-returned:
		joined = true
	case <-time.After(5 * time.Second):
		t.Fatal("original return not observed")
	}
	if !errors.Is(err, failure) {
		t.Fatal("discovery failure replaced", err)
	}
	hasCode(t, driver.state.authority.requireAgentCleanupStepInTx(context.WithoutCancel(saved), f.NewTx(), round.cause, round.claim.project), f.Forbidden)
	hasCode(t, driver.state.authority.requireAgentCleanupStepInTx(context.Background(), f.NewTx(), round.cause, round.claim.project), f.Forbidden)
}
func TestLifecycleCleanupAgentsUnknownDoesNotDiscover(t *testing.T) {
	cause, _ := f.NewJobCause("agent-cleanup-test", testID[c.Operation](t).String(), testID[struct{}](t).String())
	unknown := f.UnknownResult(testID[f.TransactionAttempt](t), cause)
	store := &stopRoundGateStore{commit: unknown}
	old, projects, round := cleanupSkillsFixture(t, store, &cleanupSkillsPort{})
	port := &cleanupAgentsPort{discover: func(context.Context, i.Actor, c.LifecycleCause, i.ProjectID) (*ac.ProjectRetirementPlan, error) {
		t.Error("Unknown start discovered Agent")
		return nil, nil
	}}
	driver, err := NewLifecycleCleanupDriverWithAgents(store, projects, old.state.processes, port, old.state.skills)
	if err != nil {
		t.Fatal(err)
	}
	err = driver.Run(context.Background(), round.claim.project, round.claim.operation)
	hasCode(t, err, f.CommitUnknown)
	result, ok := UnknownAttempt(err)
	if !ok || result.AttemptID() != unknown.AttemptID() || len(driver.state.owners) != 1 || store.transactions != 1 {
		t.Fatal("original owner lost")
	}
	driver.Stop()
	if err = driver.Drain(context.Background()); err != nil || len(driver.state.owners) != 1 {
		t.Fatal("drain erased Unknown", err)
	}
}
