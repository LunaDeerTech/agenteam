package workhttp

import (
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestWorkHTTPBlockerManagementNonterminalReceiptAndLookup(t *testing.T) {
	for _, sample := range commandSamples() {
		if sample.name != string(c.TaskBlockerCommandAdd) && sample.name != string(c.TaskBlockerCommandResolve) {
			continue
		}
		for _, state := range []c.TaskState{c.TaskStateBacklog, c.TaskStateTodo, c.TaskStateInProgress, c.TaskStateInReview, c.TaskStateBlocked} {
			t.Run(sample.name+"/"+string(state), func(t *testing.T) {
				for _, lookup := range []bool{false, true} {
					h, ports := commandFixture()
					sample.prepare(ports)
					ports.blockerResult.Task.State = state
					ports.blockerResult.Task.AssigneeAgentID = ptr(testID[id.Agent](811))
					path := sample.path
					if lookup {
						path = sample.lookupPath
					}
					writer := newTestWriter()
					if serveTest(h, commandRequest("POST", path, sampleBody(t, sample, lookup)), writer) || writer.Code != 200 || ports.calls != 1 {
						t.Fatal("valid assigned receipt aborted", lookup, writer.Code)
					}
					var raw json.RawMessage = writer.Body.Bytes()
					if lookup {
						var result struct {
							Receipt json.RawMessage `json:"receipt"`
						}
						if err := json.Unmarshal(raw, &result); err != nil {
							t.Fatal(err)
						}
						raw = result.Receipt
					}
					var receipt c.TaskBlockerMutation
					if err := json.Unmarshal(raw, &receipt); err != nil || receipt.Task.State != state || !sameAgent(receipt.Task.AssigneeAgentID, ports.blockerResult.Task.AssigneeAgentID) || receipt.Task.ManualRank != ports.blockerResult.Task.ManualRank {
						t.Fatal("wire projected different Task workflow", err)
					}
				}
			})
		}
	}
}

func TestWorkHTTPBlockerManagementRejectsTerminalProjectionAndReportsLastCause(t *testing.T) {
	sample := commandSamples()[10]
	for _, state := range []c.TaskState{c.TaskStateDone, c.TaskStateCancelled} {
		for _, lookup := range []bool{false, true} {
			h, ports := commandFixture()
			sample.prepare(ports)
			ports.blockerResult.Task.State = state
			path := sample.path
			if lookup {
				path = sample.lookupPath
			}
			writer := newTestWriter()
			aborted := serveTest(h, commandRequest("POST", path, sampleBody(t, sample, lookup)), writer)
			if lookup && (aborted || writer.Code != 503) || !lookup && !aborted {
				t.Fatal("invalid committed receipt fabricated a success or rollback", lookup, writer.Code)
			}
		}
	}
	h, ports := commandFixture()
	sample.prepare(ports)
	ports.err = f.NewFault(f.LastBlockerNeedsTransfer, f.NotCommitted)
	writer := newTestWriter()
	if serveTest(h, commandRequest("POST", sample.path, sampleBody(t, sample, false)), writer) || writer.Code != 409 {
		t.Fatal("last Blocker domain conflict not preserved", writer.Code)
	}
	var problem struct {
		Code        f.Code        `json:"code"`
		CommitState f.CommitState `json:"commit_state"`
	}
	if err := json.Unmarshal(writer.Body.Bytes(), &problem); err != nil || problem.Code != f.LastBlockerNeedsTransfer || problem.CommitState != f.NotCommitted {
		t.Fatal("last Blocker code or commit state changed", err)
	}
}
