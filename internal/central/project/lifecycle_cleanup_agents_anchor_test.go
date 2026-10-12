package project

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func cleanupAdvancedAgentCheckpoint(t *testing.T, round lifecycleCleanupRound) (agentsCleanupCheckpoint, *c.CleanupCheckpoint) {
	t.Helper()
	data, err := decodeAgentsCleanupCheckpoint(nil, round.cause, round.claim.project)
	if err != nil {
		t.Fatal(err)
	}
	at, err := f.NewInstant(time.Date(2026, 10, 12, 1, 2, 3, 123456000, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	data.Applied = &agentCleanupApplied{Agent: testID[i.Agent](t), Version: 3, Command: testID[ac.AgentCommand](t), Digest: f.Digest("sha256:" + string(bytes.Repeat([]byte("a"), 64))), BeforeUpdatedAt: &at}
	report, err := agentsCleanupReport(round, data)
	if err != nil || report.Details().State != c.CleanupPending || report.Details().Checkpoint.Schema() != 4 {
		t.Fatal("advanced anchor did not keep the parent pending under schema4", err)
	}
	return data, report.Details().Checkpoint
}

func TestLifecycleCleanupAgentsAdvancedAnchor(t *testing.T) {
	_, _, round := cleanupSkillsFixture(t, &stopRoundGateStore{}, &cleanupSkillsPort{})
	data, cp := cleanupAdvancedAgentCheckpoint(t, round)
	got, err := decodeAgentsCleanupCheckpoint(cp, round.cause, round.claim.project)
	if err != nil || got.Applied == nil || !sameAgentCleanupAnchor(*got.Applied, *data.Applied) || got.Applied.BeforeUpdatedAt == data.Applied.BeforeUpdatedAt {
		t.Fatal("checkpoint lost the exact time or retained a mutable caller pointer", err)
	}
	if err = validateCleanupCheckpoint(c.SkillsParticipant, cp, round.cause, round.claim.project); err != nil {
		t.Fatal("original progress validator rejected the new Agent anchor", err)
	}
	changed := *got.Applied
	at, err := f.NewInstant(changed.BeforeUpdatedAt.Time().Add(time.Microsecond))
	if err != nil {
		t.Fatal(err)
	}
	changed.BeforeUpdatedAt = &at
	if sameAgentCleanupAnchor(changed, *data.Applied) {
		t.Fatal("same public IDs and digest hid a different actual Before time")
	}
	changed.BeforeUpdatedAt = nil
	if sameAgentCleanupAnchor(changed, *data.Applied) {
		t.Fatal("advanced anchor became an old exact-command anchor")
	}
	got.AgentsDone = true
	completedAgents, err := agentsCleanupReport(round, got)
	if err != nil {
		t.Fatal(err)
	}
	round.checkpoint = completedAgents.Details().Checkpoint
	child, err := skillsParentCleanupReport(round, cleanupSkillsChild(t, round, c.CleanupCompleted, nil))
	if err != nil {
		t.Fatal(err)
	}
	combined, err := agentsSkillsCleanupReport(round, child)
	if err != nil {
		t.Fatal(err)
	}
	after, err := decodeAgentsCleanupCheckpoint(combined.Details().Checkpoint, round.cause, round.claim.project)
	if err != nil || combined.Details().State != c.CleanupPending || combined.Details().Checkpoint.Schema() != 4 || after.Skills.State != c.CleanupCompleted || !sameAgentCleanupAnchor(*after.Applied, *data.Applied) {
		t.Fatal("Skills progress discarded the exact Agent anchor or completed the parent", err)
	}
}

func TestLifecycleCleanupAgentsRejectsAdvancedAnchorDrift(t *testing.T) {
	_, _, round := cleanupSkillsFixture(t, &stopRoundGateStore{}, &cleanupSkillsPort{})
	data, original := cleanupAdvancedAgentCheckpoint(t, round)
	timeField := []byte(`,"before_updated_at":"` + data.Applied.BeforeUpdatedAt.String() + `"`)
	for _, mode := range []string{"schema3-time", "missing-time", "null-time", "noncanonical-time", "duplicate-time", "no-applied"} {
		t.Run(mode, func(t *testing.T) {
			raw, schema := original.Bytes(), f.Version(4)
			switch mode {
			case "schema3-time":
				schema = 3
			case "missing-time":
				raw = bytes.Replace(raw, timeField, nil, 1)
			case "null-time":
				raw = bytes.Replace(raw, timeField, []byte(`,"before_updated_at":null`), 1)
			case "noncanonical-time":
				raw = bytes.Replace(raw, []byte(data.Applied.BeforeUpdatedAt.String()), []byte("2026-10-12T01:02:03.123456+00:00"), 1)
			case "duplicate-time":
				raw = bytes.Replace(raw, timeField, append(bytes.Clone(timeField), timeField...), 1)
			case "no-applied":
				initial, err := decodeAgentsCleanupCheckpoint(nil, round.cause, round.claim.project)
				if err != nil {
					t.Fatal(err)
				}
				report, err := agentsCleanupReport(round, initial)
				if err != nil {
					t.Fatal(err)
				}
				raw = report.Details().Checkpoint.Bytes()
			}
			cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, schema, raw)
			if err != nil {
				t.Fatal("invalid public checkpoint test input", err)
			}
			if _, err = decodeAgentsCleanupCheckpoint(&cp, round.cause, round.claim.project); err == nil {
				t.Fatal("nonexact advanced anchor accepted")
			}
		})
	}
}

type cleanupAgentRestorePort struct {
	cleanupAgentsPort
	restore func(context.Context, i.Actor, c.LifecycleCause, i.ProjectID, i.AgentID, f.Version, ac.AgentCommandID, f.Digest, *f.Instant) (ac.ProjectRetirementPlan, error)
}

func (p *cleanupAgentRestorePort) RestoreProjectRetirement(ctx context.Context, actor i.Actor, cause c.LifecycleCause, project i.ProjectID, agent i.AgentID, version f.Version, command ac.AgentCommandID, digest f.Digest, at *f.Instant) (ac.ProjectRetirementPlan, error) {
	return p.restore(ctx, actor, cause, project, agent, version, command, digest, at)
}

func TestLifecycleCleanupAgentsRestoresOriginalAdvancedAnchor(t *testing.T) {
	store := &stopRoundGateStore{commit: f.NotCommittedResult(fault(f.InvalidState))}
	old, projects, round := cleanupSkillsFixture(t, store, &cleanupSkillsPort{})
	data, checkpoint := cleanupAdvancedAgentCheckpoint(t, round)
	round.checkpoint, round.agentStep = checkpoint, true
	original := checkpoint.Bytes()
	failure := errors.New("original restoration failed")
	calls := 0
	port := &cleanupAgentRestorePort{}
	port.restore = func(_ context.Context, actor i.Actor, cause c.LifecycleCause, project i.ProjectID, agent i.AgentID, version f.Version, command ac.AgentCommandID, digest f.Digest, at *f.Instant) (ac.ProjectRetirementPlan, error) {
		calls++
		if actor.Details().CauseRef != round.claim.operation.String() || cause != round.cause || project != round.claim.project || agent != data.Applied.Agent || version != data.Applied.Version || command != data.Applied.Command || digest != data.Applied.Digest || at == nil || *at != *data.Applied.BeforeUpdatedAt {
			t.Error("restore lost the original checkpoint tuple")
		}
		// A provider receives its own scalar copy. It cannot rewrite the
		// persisted checkpoint that authorized this original restoration.
		if at != nil {
			*at = f.Instant{}
		}
		return ac.ProjectRetirementPlan{}, failure
	}
	port.discover = func(context.Context, i.Actor, c.LifecycleCause, i.ProjectID) (*ac.ProjectRetirementPlan, error) {
		t.Error("failed restoration started new Agent discovery")
		return nil, failure
	}
	driver, err := NewLifecycleCleanupDriverWithAgents(store, projects, old.state.processes, port, old.state.skills)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = driver.state.runAgentStep(context.Background(), round); !errors.Is(err, failure) || calls != 1 || !bytes.Equal(checkpoint.Bytes(), original) {
		t.Fatal("original restoration failure or checkpoint was replaced", err)
	}
}
