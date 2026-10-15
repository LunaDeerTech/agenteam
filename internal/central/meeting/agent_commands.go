package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func lookupAgentCommand(ctx context.Context, x postgres.SQLExecutor, v agentCommand) (c.AgentResult, bool, error) {
	var attempt, request, project, agent, execution, call, handler, d string
	var raw []byte
	err := x.QueryRow(ctx, `SELECT attempt_id,request_id,project_id,agent_id,execution_id,tool_call_id,handler,input_digest,receipt FROM agenteam_meeting.agent_commands WHERE operation_id=$1`, v.call.OperationID.String()).Scan(&attempt, &request, &project, &agent, &execution, &call, &handler, &d, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return c.AgentResult{}, false, nil
	}
	if err != nil {
		return c.AgentResult{}, false, portError(err)
	}
	b := v.call.Call
	if attempt != v.call.AttemptID.String() || request != string(v.call.RequestID) || project != b.ProjectID.String() || agent != b.AgentID.String() || execution != b.ExecutionID.String() || call != b.CallID || handler != b.Binding.HandlerID || d != v.digest.String() {
		return c.AgentResult{}, false, fault(f.IdempotencyKeyReused)
	}
	var out c.AgentResult
	if json.Unmarshal(raw, &out) != nil {
		return c.AgentResult{}, false, fault(f.InternalError)
	}
	return out, true, nil
}
func saveAgentCommand(ctx context.Context, x postgres.SQLExecutor, v agentCommand, out c.AgentResult) error {
	raw, err := canonical(out)
	if err != nil {
		return err
	}
	b := v.call.Call
	var meeting any
	if out.Receipt != nil {
		meeting = out.Receipt.MeetingID.String()
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.agent_commands(operation_id,attempt_id,request_id,project_id,agent_id,execution_id,tool_call_id,handler,input_digest,meeting_id,receipt,committed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, v.call.OperationID.String(), v.call.AttemptID.String(), string(v.call.RequestID), b.ProjectID.String(), b.AgentID.String(), b.ExecutionID.String(), b.CallID, b.Binding.HandlerID, v.digest.String(), meeting, raw, now())
	return portError(err)
}
func (s *AgentService) listAgentMeetings(ctx context.Context, x postgres.SQLExecutor, v agentCommand) (c.AgentResult, error) {
	limit := v.input.Limit
	if limit == 0 {
		limit = 50
	}
	status := v.input.Status
	if status == "" {
		status = "active"
	}
	// Filter and project each row in one statement. Under READ COMMITTED a
	// second per-ID query could observe a later archive/delete and return a
	// session outside the requested status or fail an otherwise valid list.
	rows, err := x.Query(ctx, `SELECT id,status,version,title,proposal_content,summary,deleting,created_at,updated_at FROM agenteam_meeting.sessions WHERE project_id=$1 AND status=$2 AND NOT deleting ORDER BY updated_at DESC,id DESC LIMIT $3`, v.call.Call.ProjectID.String(), status, limit)
	if err != nil {
		return c.AgentResult{}, portError(err)
	}
	defer rows.Close()
	out := c.AgentResult{Meetings: []c.Session{}}
	for rows.Next() {
		row := c.Session{ProjectID: v.call.Call.ProjectID}
		var id string
		var version int64
		var summary []byte
		if err = rows.Scan(&id, &row.Status, &version, &row.Title, &row.ProposalContent, &summary, &row.Deleting, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return c.AgentResult{}, portError(err)
		}
		row.ID, err = f.ParseID[c.Meeting](id)
		if err != nil {
			return c.AgentResult{}, fault(f.InternalError)
		}
		row.Version = f.Version(version)
		if len(summary) > 0 {
			var value c.RollingSummary
			if json.Unmarshal(summary, &value) != nil {
				return c.AgentResult{}, fault(f.InternalError)
			}
			row.Summary = &value
		}
		out.Meetings = append(out.Meetings, row)
	}
	if err = rows.Err(); err != nil {
		return c.AgentResult{}, portError(err)
	}
	return out, nil
}
func (s *AgentService) participant(ctx context.Context, tx f.Tx, call c.AgentToolCall, id i.AgentID) (c.AgentParticipant, error) {
	v, err := s.deps.Participants.ReadMeetingParticipantInTx(ctx, tx, call, id)
	if err != nil {
		return c.AgentParticipant{}, portError(err)
	}
	if v.ID != id || v.Name == "" || !c.ValidText(v.Name, 1024) || !c.ValidText(v.Description, 65536) {
		return c.AgentParticipant{}, fault(f.Forbidden)
	}
	return v, nil
}
func (s *AgentService) requestMeeting(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, v agentCommand, source c.AgentSource) (c.AgentResult, error) {
	ids := slices.Clone(v.input.Agents)
	if !slices.Contains(ids, v.call.Call.AgentID) {
		ids = append([]i.AgentID{v.call.Call.AgentID}, ids...)
	}
	if len(ids) > c.MaxParticipants {
		return c.AgentResult{}, fault(f.InvalidArgument)
	}
	participants := make([]c.ParticipantView, 0, len(ids))
	var creator c.ParticipantID
	for n, id := range ids {
		entry, err := s.participant(ctx, tx, v.call, id)
		if err != nil {
			return c.AgentResult{}, err
		}
		pid, err := f.NewID[c.Participant]()
		if err != nil {
			return c.AgentResult{}, portError(err)
		}
		if id == v.call.Call.AgentID {
			creator = pid
		}
		participants = append(participants, c.ParticipantView{ID: pid, MeetingID: v.meeting, Kind: "agent", SourceID: id.String(), Name: entry.Name, Description: entry.Description, Order: n + 1, Active: true})
	}
	at := v.event.event.Header().OccurredAt.Time()
	if _, err := x.Exec(ctx, `INSERT INTO agenteam_meeting.sessions(id,project_id,status,version,proposal_content,created_at,updated_at) VALUES($1,$2,'proposed',1,$3,$4,$4)`, v.meeting.String(), v.call.Call.ProjectID.String(), v.input.ProposalContent, at); err != nil {
		return c.AgentResult{}, portError(err)
	}
	for _, p := range participants {
		if err := insertParticipant(ctx, x, p, at); err != nil {
			return c.AgentResult{}, err
		}
	}
	var kind, origin any
	if source.Origin != nil {
		kind = source.Origin.Kind
		origin = source.Origin.ID
	}
	if _, err := x.Exec(ctx, `INSERT INTO agenteam_meeting.proposal_sources(meeting_id,execution_id,created_by_participant_id,origin_kind,origin_id) VALUES($1,$2,$3,$4,$5)`, v.meeting.String(), source.ExecutionID.String(), creator.String(), kind, origin); err != nil {
		return c.AgentResult{}, portError(err)
	}
	if err := s.deps.Events.appendInTx(ctx, tx, v.event); err != nil {
		return c.AgentResult{}, err
	}
	return c.AgentResult{Receipt: &c.Receipt{MeetingID: v.meeting, Version: 1}}, nil
}
func (s *AgentService) changeAgent(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, v agentCommand) (c.AgentResult, error) {
	session, err := loadSession(ctx, x, v.call.Call.ProjectID, v.meeting)
	if err != nil {
		return c.AgentResult{}, err
	}
	if session.Deleting || session.Status == "archive" {
		return c.AgentResult{}, fault(f.InvalidState)
	}
	if session.Version != *v.input.ExpectedVersion {
		return c.AgentResult{}, fault(f.ConfirmationStale)
	}
	participants, err := loadParticipants(ctx, x, v.meeting)
	if err != nil {
		return c.AgentResult{}, err
	}
	admitted := false
	target := -1
	maxOrder := 0
	count := 0
	for n, p := range participants {
		if p.Order > maxOrder {
			maxOrder = p.Order
		}
		if p.Active && p.Kind == "agent" {
			count++
			if p.SourceID == v.call.Call.AgentID.String() {
				admitted = true
			}
			if p.SourceID == v.input.AgentID.String() {
				target = n
			}
		}
	}
	if !admitted {
		return c.AgentResult{}, fault(f.Forbidden)
	}
	if v.call.Call.Binding.HandlerID == "meeting.add-agent" {
		if target >= 0 {
			return c.AgentResult{}, fault(f.InvalidState)
		}
		if count >= c.MaxParticipants {
			return c.AgentResult{}, fault(f.InvalidArgument)
		}
		entry, err := s.participant(ctx, tx, v.call, *v.input.AgentID)
		if err != nil {
			return c.AgentResult{}, err
		}
		id, err := f.NewID[c.Participant]()
		if err != nil {
			return c.AgentResult{}, portError(err)
		}
		if err = insertParticipant(ctx, x, c.ParticipantView{ID: id, MeetingID: v.meeting, Kind: "agent", SourceID: entry.ID.String(), Name: entry.Name, Description: entry.Description, Order: maxOrder + 1, Active: true}, now()); err != nil {
			return c.AgentResult{}, err
		}
	} else {
		if target < 0 {
			return c.AgentResult{}, fault(f.NotFound)
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.participants SET active=false WHERE meeting_id=$1 AND id=$2 AND active`, v.meeting.String(), participants[target].ID.String()); err != nil {
			return c.AgentResult{}, portError(err)
		}
	}
	// Roster writes affect only subsequent Turns; no turn/input/message rows are
	// changed, including when the caller removes itself during a contribution.
	version := session.Version + 1
	if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET version=$2,updated_at=$3 WHERE id=$1`, v.meeting.String(), int64(version), now()); err != nil {
		return c.AgentResult{}, portError(err)
	}
	return c.AgentResult{Receipt: &c.Receipt{MeetingID: v.meeting, Version: version}}, nil
}
