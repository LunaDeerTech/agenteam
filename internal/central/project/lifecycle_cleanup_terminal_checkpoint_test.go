package project

import (
	"bytes"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func terminalCheckpointFixture(t *testing.T) (lifecycleCleanupRound, c.ProjectTerminalOwnerIdentity, c.ProjectTerminalOwnerIdentity, c.CleanupReport) {
	t.Helper()
	_, _, round, _, _ := cleanupVariablesFixture(t)
	round.checkpoint = cleanupVariablesReady(t, round)
	report, err := variablesParentCleanupReport(round, cleanupSkillsChild(t, round, c.CleanupCompleted, nil))
	if err != nil {
		t.Fatal(err)
	}
	round.checkpoint = report.Details().Checkpoint
	a := c.ProjectTerminalOwnerIdentity{Kind: c.ProjectTerminalAgentOwner, ProcessID: round.claim.process.String(), InstanceID: testID[struct{}](t).String()}
	b := c.ProjectTerminalOwnerIdentity{Kind: c.ProjectTerminalVariablesOwner, ProcessID: round.claim.process.String(), InstanceID: testID[struct{}](t).String()}
	result, err := terminalCleanupReport(round, a, b, "12345")
	if err != nil {
		t.Fatal("invalid complete terminal receipt fixture", err)
	}
	return round, a, b, result
}

func TestTerminalCleanupCheckpointBindings(t *testing.T) {
	round, a, b, report := terminalCheckpointFixture(t)
	cp := report.Details().Checkpoint
	data, err := decodeTerminalCleanupCheckpoint(cp, round.cause, round.claim.project)
	if err != nil || !data.matchesClaim(round.claim) || data.AgentOwner != a || data.VariablesOwner != b || report.Details().State != c.CleanupCompleted || cp.Schema() != 6 {
		t.Fatal("terminal receipt lost the original transaction or pair identities", err)
	}
	if bytes.Contains(cp.Bytes(), []byte("command_id")) || bytes.Contains(cp.Bytes(), []byte("agents_checkpoint")) {
		t.Fatal("terminal receipt still needs the deleted command")
	}
	for _, mode := range []string{"project", "operation", "version", "process", "attempt", "fence", "zero-transaction", "noncanonical-transaction", "overflow-transaction", "digest", "agent-kind", "owner-process", "missing-instance", "extra-field", "duplicate-field", "wrong-schema"} {
		t.Run(mode, func(t *testing.T) {
			bad := data
			schema := f.Version(6)
			switch mode {
			case "project":
				bad.Project = testID[struct{}](t).String()
			case "operation":
				bad.Operation = testID[struct{}](t).String()
			case "version":
				bad.Version++
			case "process":
				bad.Process = "invalid"
			case "attempt":
				bad.Attempt = "invalid"
			case "fence":
				bad.Fence = 0
			case "zero-transaction":
				bad.TransactionID = "0"
			case "noncanonical-transaction":
				bad.TransactionID = "012345"
			case "overflow-transaction":
				bad.TransactionID = "18446744073709551616"
			case "agent-kind":
				bad.AgentOwner.Kind = c.ProjectTerminalVariablesOwner
			case "owner-process":
				bad.VariablesOwner.ProcessID = testID[struct{}](t).String()
			case "missing-instance":
				bad.AgentOwner.InstanceID = ""
			case "wrong-schema":
				schema = 5
			}
			raw, err := json.Marshal(bad)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "extra-field" {
				raw = append(raw[:len(raw)-1], []byte(`,"extra":true}`)...)
			} else if mode == "duplicate-field" {
				raw = append(raw[:len(raw)-1], []byte(`,"transaction_id":"12345"}`)...)
			} else if mode == "digest" {
				raw = bytes.Replace(raw, []byte(data.PreviousDigest), []byte("invalid"), 1)
			}
			changed, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, schema, raw)
			if err != nil {
				t.Fatal("public checkpoint fixture rejected before private decoder", err)
			}
			if _, err = decodeTerminalCleanupCheckpoint(&changed, round.cause, round.claim.project); err == nil {
				t.Fatal("nonexact terminal receipt accepted")
			}
		})
	}
}

func TestTerminalCleanupCheckpointRequiresCompletedPrefix(t *testing.T) {
	for _, mode := range []string{"nil", "schema-four", "variables-pending", "claim-terminal", "foreign-owner"} {
		t.Run(mode, func(t *testing.T) {
			round, a, b, _ := terminalCheckpointFixture(t)
			switch mode {
			case "nil":
				round.checkpoint = nil
			case "schema-four":
				round.checkpoint = cleanupVariablesReady(t, round)
			case "variables-pending":
				r, err := variablesParentCleanupReport(round, cleanupSkillsChild(t, round, c.CleanupPending, nil))
				if err != nil {
					t.Fatal(err)
				}
				round.checkpoint = r.Details().Checkpoint
			case "claim-terminal":
				round.claim.phase = "terminal"
			case "foreign-owner":
				a.ProcessID = testID[struct{}](t).String()
			}
			if _, err := terminalCleanupReport(round, a, b, "12345"); err == nil {
				t.Fatal("terminal receipt created before exact prefix completion")
			}
		})
	}
}
