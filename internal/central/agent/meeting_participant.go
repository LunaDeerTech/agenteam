package agent

import (
	"context"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"slices"
)

// MeetingParticipantReader is a separate AgentRun port. It does not relax the
// Human-only directory or WorkReferences authorization contracts.
type MeetingParticipantReader struct {
	authority *Authority
	tools     mc.AgentInvocationAuthority
}

func NewMeetingParticipantReader(a *Authority, tools mc.AgentInvocationAuthority) (*MeetingParticipantReader, error) {
	if a == nil || a.state == nil || nilPort(tools) {
		return nil, fault(f.DependencyUnbound)
	}
	return &MeetingParticipantReader{a, tools}, nil
}
func (r *MeetingParticipantReader) ReadMeetingParticipantInTx(ctx context.Context, tx f.Tx, call mc.AgentToolCall, target i.AgentID) (mc.AgentParticipant, error) {
	if r == nil || r.authority == nil || ctx == nil || call.Validate() != nil || target.Validate() != nil {
		return mc.AgentParticipant{}, fault(f.Forbidden)
	}
	if call.Call.Binding.HandlerID != "meeting.request" && call.Call.Binding.HandlerID != "meeting.add-agent" {
		return mc.AgentParticipant{}, fault(f.Forbidden)
	}
	store := r.authority.state.store
	if err := r.tools.RequireMeetingToolBindingInTx(ctx, tx, store); err != nil {
		return mc.AgentParticipant{}, portError(err)
	}
	schedule, _ := f.ProjectScheduleLock(call.Call.ProjectID.String())
	if err := store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(call.Call.ProjectID, f.Shared), {Key: schedule, Mode: f.Exclusive}, agentLock(target, f.Shared)}); err != nil {
		return mc.AgentParticipant{}, portError(err)
	}
	var out mc.AgentParticipant
	err := r.tools.WithMeetingToolInputInTx(ctx, tx, call, false, func(ctx context.Context, raw []byte, _ mc.AgentSource) error {
		var input mc.AgentInput
		if json.Unmarshal(raw, &input) != nil || input.Validate(call.Call.Binding.HandlerID) != nil {
			return fault(f.Forbidden)
		}
		allowed := call.Call.Binding.HandlerID == "meeting.request" && (target == call.Call.AgentID || slices.Contains(input.Agents, target)) || call.Call.Binding.HandlerID == "meeting.add-agent" && input.AgentID != nil && *input.AgentID == target
		if !allowed {
			return fault(f.Forbidden)
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		value, err := initializedAgent(ctx, x, call.Call.ProjectID, target)
		if err != nil {
			return err
		}
		core := value.Fields().Core
		name := core.Name
		if core.DisplayName != nil {
			name = *core.DisplayName
		}
		out = mc.AgentParticipant{ID: target, Name: name, Description: core.Description}
		return nil
	})
	if err != nil {
		return mc.AgentParticipant{}, err
	}
	return out, nil
}

var _ mc.AgentParticipantAuthority = (*MeetingParticipantReader)(nil)
