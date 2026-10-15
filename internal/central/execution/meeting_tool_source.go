package execution

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// ReadMeetingToolSourceInTx projects provenance from the original accepted
// Execution launch. The private plan and original input loan remain mandatory;
// an AgentSource is never permission to read or mutate its origin resource.
func (d *ToolCallDriver) ReadMeetingToolSourceInTx(ctx context.Context, tx f.Tx, binding tc.ToolCallBinding, plan tc.ToolCallPlan, lookup bool) (mc.AgentSource, error) {
	if d == nil || d.driver == nil || d.driver.state == nil || ctx == nil || binding.Validate() != nil || !mc.AgentHandler(binding.Binding) {
		return mc.AgentSource{}, invalid()
	}
	var source mc.AgentSource
	read := func(current context.Context, original tc.ToolCallBinding, _ []byte) error {
		x, err := d.driver.state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadExecution(current, x, original.ExecutionID)
		if err != nil {
			return err
		}
		source, err = meetingToolSource(row, original)
		return err
	}
	var err error
	if lookup {
		// The Runtime's original accepted-operation lookup may outlive running
		// status. This loan still checks its exact persisted call/round/input,
		// same Store/Tx/locks and current process, without granting a new call.
		err = d.WithCommittedBuiltinToolCallInTx(ctx, tx, binding, plan, read)
	} else {
		err = d.WithBuiltinToolCallInTx(ctx, tx, binding, plan, read)
	}
	if err != nil {
		return mc.AgentSource{}, err
	}
	return source, nil
}

func meetingToolSource(row *executionRecord, binding tc.ToolCallBinding) (mc.AgentSource, error) {
	if row == nil || row.summary.ID != binding.ExecutionID || row.summary.ProjectID != binding.ProjectID || row.summary.AgentID != binding.AgentID || row.launch.ProjectID != binding.ProjectID || row.launch.AgentID != binding.AgentID || row.launch.Trigger.Validate(row.launch.Purpose) != nil {
		return mc.AgentSource{}, fault(f.ConfirmationStale)
	}
	origin := mc.ProposalOrigin{Kind: row.launch.Trigger.Kind}
	switch origin.Kind {
	case "task":
		origin.ID = row.launch.Trigger.TaskID
	case "meeting":
		origin.ID = row.launch.Trigger.MeetingID
	default:
		return mc.AgentSource{}, fault(f.CapabilityUnsupported)
	}
	source := mc.AgentSource{ExecutionID: row.summary.ID, Origin: &origin}
	if err := source.Validate(); err != nil {
		return mc.AgentSource{}, err
	}
	return source, nil
}

var _ mc.AgentSourceReader = (*ToolCallDriver)(nil)
