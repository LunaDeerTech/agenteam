package meeting

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	hc "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// DecisionInboxSource supplies only a current, authorized projection. An open
// item never authorizes an answer: RespondDecision still validates the original
// Execution's exact waiting reference with its full source-bound reader.
type DecisionInboxSource struct {
	store     Store
	authority hc.DecisionSourceReadAuthority
}

func NewDecisionInboxSource(store Store, authority hc.DecisionSourceReadAuthority) (*DecisionInboxSource, error) {
	if nilPort(store) || nilPort(authority) {
		return nil, fault(f.DependencyUnbound)
	}
	return &DecisionInboxSource{store, authority}, nil
}

func decisionInboxSource(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, meeting c.MeetingID, decision c.DecisionID) (hc.Source, error) {
	out := hc.Source{ProjectID: project, ID: decision.String(), Context: hc.SourceContext{MeetingID: meeting.String()}, Title: "待答决议"}
	var question string
	err := x.QueryRow(ctx, `SELECT d.version,COALESCE(d.resolved_at,d.created_at),d.question,COALESCE((d.status='pending' AND NOT s.deleting AND NOT t.cancel_requested AND NOT c.cancel_requested AND c.status='running' AND c.current_execution_id=d.execution_id AND c.generation=l.generation AND c.attempt=l.attempt AND (t.status='running' OR (t.status='completed' AND (c.generation>1 OR c.attempt>1)))),false) FROM agenteam_meeting.decision_requests d JOIN agenteam_meeting.sessions s ON s.id=d.meeting_id JOIN agenteam_meeting.contributions c ON c.id=d.contribution_id AND c.meeting_id=s.id JOIN agenteam_meeting.turns t ON t.id=c.turn_id AND t.meeting_id=s.id JOIN agenteam_meeting.launch_intents l ON l.execution_id=d.execution_id AND l.meeting_id=s.id AND l.contribution_id=c.id WHERE s.project_id=$1 AND s.id=$2 AND d.id=$3`, project.String(), meeting.String(), decision.String()).Scan(&out.Version, &out.OccurredAt, &question, &out.Open)
	if errors.Is(err, pgx.ErrNoRows) {
		// Immutable identity/state facts survive purge. Historical payloads are
		// never used to reconstruct an actionable Decision or its old question.
		err = x.QueryRow(ctx, `SELECT version,occurred_at FROM agenteam_meeting.decision_events WHERE project_id=$1 AND meeting_id=$2 AND decision_id=$3 ORDER BY version DESC LIMIT 1`, project.String(), meeting.String(), decision.String()).Scan(&out.Version, &out.OccurredAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return hc.Source{}, fault(f.NotFound)
		}
		out.Open = false
	}
	if err != nil {
		return hc.Source{}, portError(err)
	}
	if out.Version.Validate() != nil || out.OccurredAt.IsZero() {
		return hc.Source{}, fault(f.InvalidState)
	}
	runes := []rune(question)
	if len(runes) > 512 {
		runes = runes[:512]
	}
	out.Summary = string(runes)
	out.Relevant = true
	return out, nil
}

func (s *DecisionInboxSource) ReadDecisionSourceInTx(ctx context.Context, tx f.Tx, project i.ProjectID, meetingID, decisionID string) (hc.Source, error) {
	if s == nil || ctx == nil || !tx.Valid() || project.Validate() != nil {
		return hc.Source{}, fault(f.InvalidArgument)
	}
	meeting, err := f.ParseID[c.Meeting](meetingID)
	if err != nil {
		return hc.Source{}, fault(f.InvalidArgument)
	}
	decision, err := f.ParseID[c.Decision](decisionID)
	if err != nil {
		return hc.Source{}, fault(f.InvalidArgument)
	}
	if err = s.authority.RequireDecisionSourceReadInTx(ctx, tx, s.store, project, meetingID, decisionID, false); err != nil {
		return hc.Source{}, portError(err)
	}
	d := decisionLock(decision)
	d.Mode = f.Shared
	if err = s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Shared), meetingLock(meeting, f.Shared), d}); err != nil {
		return hc.Source{}, portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return hc.Source{}, portError(err)
	}
	return decisionInboxSource(ctx, x, project, meeting, decision)
}

func (s *DecisionInboxSource) ScanDecisionSourcesInTx(ctx context.Context, tx f.Tx, project i.ProjectID) ([]hc.Source, error) {
	if s == nil || ctx == nil || !tx.Valid() || project.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if err := s.authority.RequireDecisionSourceReadInTx(ctx, tx, s.store, project, "", "", true); err != nil {
		return nil, portError(err)
	}
	if err := s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project, f.Exclusive)}); err != nil {
		return nil, portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	rows, err := x.Query(ctx, `SELECT s.id,d.id FROM agenteam_meeting.decision_requests d JOIN agenteam_meeting.sessions s ON s.id=d.meeting_id WHERE s.project_id=$1 UNION SELECT meeting_id,decision_id FROM agenteam_meeting.decision_events WHERE project_id=$1 ORDER BY 1,2`, project.String())
	if err != nil {
		return nil, portError(err)
	}
	type identity struct{ meeting, decision string }
	ids := []identity{}
	for rows.Next() {
		var v identity
		if err = rows.Scan(&v.meeting, &v.decision); err != nil {
			rows.Close()
			return nil, portError(err)
		}
		ids = append(ids, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, portError(err)
	}
	// Release the rows before borrowing the same transaction for each source.
	out := []hc.Source{}
	for _, id := range ids {
		meeting, em := f.ParseID[c.Meeting](id.meeting)
		decision, ed := f.ParseID[c.Decision](id.decision)
		if em != nil || ed != nil {
			return nil, fault(f.InvalidState)
		}
		v, err := decisionInboxSource(ctx, x, project, meeting, decision)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

var _ hc.DecisionSourceProvider = (*DecisionInboxSource)(nil)
