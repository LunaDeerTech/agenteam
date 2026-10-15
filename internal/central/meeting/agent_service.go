package meeting

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
)

type AgentDependencies struct {
	Tools        c.AgentInvocationAuthority
	Participants c.AgentParticipantAuthority
	Events       *ProposalEvents
	Realtime     rc.Publisher
}
type AgentService struct {
	store Store
	deps  AgentDependencies
}

func NewAgentService(store Store, deps AgentDependencies) (*AgentService, error) {
	if nilPort(store) || nilPort(deps.Tools) || nilPort(deps.Participants) || deps.Events == nil || !sameStore(store, deps.Events.authority.store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &AgentService{store, deps}, nil
}
func (s *AgentService) RequireBuiltinBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if s == nil || !sameStore(s.store, store) {
		return fault(f.DependencyUnbound)
	}
	return s.deps.Tools.RequireMeetingToolBindingInTx(ctx, tx, store)
}
func ParseAgentInput(handler string, raw []byte) (c.AgentInput, error) {
	var v c.AgentInput
	if len(raw) > 64<<10 {
		return v, fault(f.InvalidArgument)
	}
	encoded, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return v, fault(f.InvalidArgument)
	}
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil || v.Validate(handler) != nil {
		return c.AgentInput{}, fault(f.InvalidArgument)
	}
	if handler == "meeting.request" && strings.TrimSpace(v.ProposalContent) == "" {
		return c.AgentInput{}, fault(f.InvalidArgument)
	}
	return v, nil
}

type agentCommand struct {
	call     c.AgentToolCall
	input    c.AgentInput
	digest   f.Digest
	meeting  c.MeetingID
	locks    []f.LockRequest
	identity f.CommandIdentity
	event    *lifecycleEventPlan
}

func agentCommandIdentity(call c.AgentToolCall) (f.CommandIdentity, error) {
	key, err := f.ParseIdempotencyKey(call.OperationID.String())
	if err != nil {
		return f.CommandIdentity{}, err
	}
	return f.NewCommandIdentity("meeting", []string{call.Call.ProjectID.String()}, "agent-tool", key)
}
func (s *AgentService) prepareAgent(ctx context.Context, call c.AgentToolCall, in c.AgentInput, lookup bool) (agentCommand, error) {
	var out agentCommand
	if s == nil || ctx == nil || call.Validate() != nil || in.Validate(call.Call.Binding.HandlerID) != nil {
		return out, fault(f.InvalidArgument)
	}
	// These names remain unregistered until their product payload is defined.
	if call.Call.Binding.HandlerID == "meeting.update" || call.Call.Binding.HandlerID == "meeting.comment" {
		return out, fault(f.CapabilityUnsupported)
	}
	locks, err := s.deps.Tools.DiscoverMeetingTool(ctx, call, lookup)
	if err != nil {
		return out, portError(err)
	}
	command, err := agentCommandIdentity(call)
	if err != nil {
		return out, err
	}
	raw, err := canonical(in)
	if err != nil {
		return out, err
	}
	out = agentCommand{call: call, input: in, digest: digest(raw), identity: command}
	locks = append(locks, projectLock(call.Call.ProjectID, f.Shared), commandLock(command))
	if in.MeetingID != nil {
		out.meeting = *in.MeetingID
	} else if call.Call.Binding.HandlerID == "meeting.request" && !lookup {
		out.meeting, err = f.NewID[c.Meeting]()
		if err != nil {
			return out, portError(err)
		}
	}
	if out.meeting.Validate() == nil {
		locks = append(locks, meetingLock(out.meeting, f.Exclusive))
	}
	if !lookup && (call.Call.Binding.HandlerID == "meeting.request" || call.Call.Binding.HandlerID == "meeting.add-agent" || call.Call.Binding.HandlerID == "meeting.remove-agent") {
		schedule, _ := f.ProjectScheduleLock(call.Call.ProjectID.String())
		locks = append(locks, f.LockRequest{Key: schedule, Mode: f.Exclusive}, agentLock(call.Call.AgentID))
		for _, a := range in.Agents {
			locks = append(locks, agentLock(a))
		}
		if in.AgentID != nil {
			locks = append(locks, agentLock(*in.AgentID))
		}
	}
	out.locks, err = normalize(locks)
	if err != nil {
		return out, err
	}
	if !lookup && call.Call.Binding.HandlerID == "meeting.request" {
		out.event, err = s.deps.Events.prepare(ctx, call.Call.Actor, call.Call.ProjectID, out.meeting, 1, c.ProposedName, "proposed", now(), out.locks)
		if err != nil {
			return out, err
		}
		out.locks = out.event.locks
		out.event.sourceCheck = func(ctx context.Context, tx f.Tx) error {
			return s.deps.Tools.WithMeetingToolInputInTx(ctx, tx, call, false, func(ctx context.Context, raw []byte, source c.AgentSource) error {
				input, err := ParseAgentInput(call.Call.Binding.HandlerID, raw)
				if err != nil {
					return err
				}
				body, err := canonical(input)
				if err != nil {
					return err
				}
				if digest(body) != out.digest || source.ExecutionID != call.Call.ExecutionID {
					return fault(f.Forbidden)
				}
				x, err := s.store.InTx(tx)
				if err != nil {
					return portError(err)
				}
				var exact bool
				err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.proposal_sources s JOIN agenteam_meeting.participants p ON p.id=s.created_by_participant_id AND p.meeting_id=s.meeting_id WHERE s.meeting_id=$1 AND s.execution_id=$2 AND p.kind='agent' AND p.source_id=$3)`, out.meeting.String(), source.ExecutionID.String(), call.Call.AgentID.String()).Scan(&exact)
				if err != nil {
					return portError(err)
				}
				if !exact {
					return fault(f.Forbidden)
				}
				return nil
			})
		}
	}
	return out, nil
}
func (s *AgentService) agentTx(ctx context.Context, v agentCommand, lookup bool, fn func(context.Context, f.Tx, postgres.SQLExecutor, c.AgentSource) error) error {
	cause, err := f.NewCommandsCause(v.identity)
	if err != nil {
		return err
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, v.locks); err != nil {
			return portError(err)
		}
		if err := s.deps.Tools.RequireMeetingToolBindingInTx(ctx, tx, s.store); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return s.deps.Tools.WithMeetingToolInputInTx(ctx, tx, v.call, lookup, func(current context.Context, raw []byte, source c.AgentSource) error {
			original, err := ParseAgentInput(v.call.Call.Binding.HandlerID, raw)
			if err != nil {
				return err
			}
			body, err := canonical(original)
			if err != nil {
				return err
			}
			if digest(body) != v.digest {
				return fault(f.Forbidden)
			}
			if source.Validate() != nil || source.ExecutionID != v.call.Call.ExecutionID {
				return fault(f.Forbidden)
			}
			return fn(current, tx, x, source)
		})
	})
	return commitError(result)
}
func (s *AgentService) Execute(ctx context.Context, call c.AgentToolCall, in c.AgentInput) (c.AgentResult, error) {
	v, err := s.prepareAgent(ctx, call, in, false)
	if err != nil {
		return c.AgentResult{}, err
	}
	var out c.AgentResult
	err = s.agentTx(ctx, v, false, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, source c.AgentSource) error {
		old, found, err := lookupAgentCommand(ctx, x, v)
		if err != nil {
			return err
		}
		if found {
			out = old
			return nil
		}
		switch call.Call.Binding.HandlerID {
		case "meeting.list":
			out, err = s.listAgentMeetings(ctx, x, v)
		case "meeting.request":
			out, err = s.requestMeeting(ctx, tx, x, v, source)
		case "meeting.add-agent", "meeting.remove-agent":
			out, err = s.changeAgent(ctx, tx, x, v)
		default:
			err = fault(f.CapabilityUnsupported)
		}
		if err != nil {
			return err
		}
		return saveAgentCommand(ctx, x, v, out)
	})
	if err != nil {
		return c.AgentResult{}, err
	}
	s.invalidateAgentReceipt(call, out)
	return out, nil
}
func (s *AgentService) Lookup(ctx context.Context, call c.AgentToolCall, in c.AgentInput) (c.AgentResult, bool, error) {
	v, err := s.prepareAgent(ctx, call, in, true)
	if err != nil {
		return c.AgentResult{}, false, err
	}
	var out c.AgentResult
	var found bool
	err = s.agentTx(ctx, v, true, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, _ c.AgentSource) error {
		out, found, err = lookupAgentCommand(ctx, x, v)
		return err
	})
	if err != nil {
		return c.AgentResult{}, false, err
	}
	if found {
		s.invalidateAgentReceipt(call, out)
	}
	return out, found, nil
}

func (s *AgentService) invalidateAgentReceipt(call c.AgentToolCall, out c.AgentResult) {
	switch call.Call.Binding.HandlerID {
	case "meeting.request", "meeting.add-agent", "meeting.remove-agent":
		if out.Receipt != nil {
			invalidateMeeting(s.deps.Realtime, call.Call.ProjectID, out.Receipt.MeetingID)
		}
	}
}
