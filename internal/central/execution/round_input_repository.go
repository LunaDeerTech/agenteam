package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

type roundInputSource struct {
	row      *executionRecord
	original *directTextRecord
	initial  sc.InitialRoundBindingsRequest
	receipt  c.ToolResultConsumptionReceipt
	prefix   f.Digest
}

// All reads are Execution-owned. No Tool dispatch/Model call or caller receipt
// is used to establish the original full successful result already sealed by 66.
func loadRoundInputSource(ctx context.Context, x postgres.SQLExecutor, e i.ExecutionID) (*roundInputSource, error) {
	row, err := loadExecution(ctx, x, e)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fault(f.NotFound)
	}
	old, err := loadDirectText(ctx, x, e)
	if err != nil {
		return nil, err
	}
	if old == nil || old.toolPending == nil {
		return nil, fault(f.InvalidState)
	}
	call, err := toolBinding(old)
	if err != nil {
		return nil, err
	}
	input, err := loadPreparationInput(ctx, x, e)
	if err != nil {
		return nil, err
	}
	if input == nil || input.input.Digest() != old.snapshot.Fields().Context.InputDigest() || row.summary.SnapshotID == nil || *row.summary.SnapshotID != old.snapshot.Fields().ID || !input.input.Fields().Request.Equal(c.PreparationRequest{ExecutionID: e, Launch: row.launch}) {
		return nil, fault(f.ConfirmationStale)
	}
	var op, attempt, request, inputDigest, fingerprint, terminal, result string
	var resultBytes int64
	var message []byte
	err = x.QueryRow(ctx, `SELECT q.operation_id::text,q.attempt_id::text,q.request_id::text,q.input_digest,q.fingerprint,q.terminal_digest,q.result_digest,q.result_bytes,t.message FROM agenteam_execution.tool_result_consumptions q JOIN agenteam_execution.transcript_entries t ON (t.execution_id,t.sequence,t.round_id)=(q.execution_id,q.transcript_sequence,q.round_id) WHERE q.execution_id=$1`, e.String()).Scan(&op, &attempt, &request, &inputDigest, &fingerprint, &terminal, &result, &resultBytes, &message)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fault(f.InvalidState)
	}
	if err != nil {
		return nil, unavailable(err)
	}
	operation, err := f.ParseID[tc.Operation](op)
	if err != nil {
		return nil, unavailable(nil)
	}
	attemptID, err := f.ParseID[tc.Attempt](attempt)
	if err != nil {
		return nil, unavailable(nil)
	}
	b := tc.ReadFileResultBinding{Call: call, OperationID: operation, AttemptID: attemptID, RequestID: p.ID(request), InputDigest: f.Digest(inputDigest), Fingerprint: f.Digest(fingerprint), TerminalDigest: f.Digest(terminal), Outcome: "success", Attempted: true, ResultDigest: f.Digest(result), ResultBytes: f.Progress(resultBytes)}
	binding, err := toolResultBindingJSON(b)
	if err != nil {
		return nil, unavailable(nil)
	}
	var m mc.Message
	if decodeDirectTextJSON(message, &m) != nil || m.Validate() != nil || m.Role != "tool" || len(m.Parts) != 1 || m.Parts[0].ToolResult == nil {
		return nil, unavailable(nil)
	}
	r := m.Parts[0].ToolResult
	if r.CallID != call.CallID || r.IsError || len(r.Parts) != 1 || r.Parts[0].Text == nil {
		return nil, unavailable(nil)
	}
	projection, err := agentloop.ProjectReadFileToolResult(ctx, b, []byte(r.Parts[0].Text.Text))
	if err != nil {
		return nil, portError(err)
	}
	if !bytes.Equal(message, projection.CanonicalBytes()) {
		return nil, unavailable(nil)
	}
	consumed, err := loadToolResultConsumption(ctx, x, b, binding, projection, row.summary.Version)
	if err != nil {
		return nil, err
	}
	if consumed == nil {
		return nil, fault(f.InvalidState)
	}
	prefix, err := roundTranscriptPrefix(ctx, x, e, old.round.Fields().ID)
	if err != nil {
		return nil, err
	}
	fields := input.input.Fields()
	return &roundInputSource{row: row, original: old, initial: sc.InitialRoundBindingsRequest{Initial: fields.Skills.Clone(), CaptureAttemptBinding: fields.AttemptBinding}, receipt: consumed.receipt, prefix: prefix}, ctx.Err()
}

func roundTranscriptPrefix(ctx context.Context, x postgres.SQLExecutor, e i.ExecutionID, round c.RoundID) (f.Digest, error) {
	rows, err := x.Query(ctx, `SELECT sequence,round_id::text,kind,message,digest FROM agenteam_execution.transcript_entries WHERE execution_id=$1 AND sequence<=3 ORDER BY sequence`, e.String())
	if err != nil {
		return "", unavailable(err)
	}
	defer rows.Close()
	messages := make([]json.RawMessage, 0, 3)
	for rows.Next() {
		var seq int64
		var id, kind, digest string
		var raw []byte
		if err = rows.Scan(&seq, &id, &kind, &raw, &digest); err != nil {
			return "", unavailable(err)
		}
		if seq != int64(len(messages)+1) || seq > 3 || id != round.String() || kind != []string{"input", "assistant", "tool_result"}[seq-1] || c.TriggerInputDigest(raw) != f.Digest(digest) {
			return "", unavailable(nil)
		}
		var m mc.Message
		if decodeDirectTextJSON(raw, &m) != nil || m.Validate() != nil {
			return "", unavailable(nil)
		}
		messages = append(messages, bytes.Clone(raw))
	}
	if err = rows.Err(); err != nil {
		return "", unavailable(err)
	}
	if len(messages) != 3 {
		return "", unavailable(nil)
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		return "", unavailable(err)
	}
	return c.TriggerInputDigest(raw), ctx.Err()
}

func loadRoundPending(ctx context.Context, x postgres.SQLExecutor, e i.ExecutionID, project i.ProjectID, agent i.AgentID) ([]sc.RuntimeAssignmentChange, error) {
	rows, err := x.Query(ctx, `SELECT change_id::text,project_id::text,agent_id::text,outcome,change_data,change_digest,created_at FROM agenteam_execution.skill_assignment_ingress WHERE execution_id=$1 ORDER BY change_id`, e.String())
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	out := make([]sc.RuntimeAssignmentChange, 0)
	for rows.Next() {
		var id, p, a, outcome, digest string
		var raw []byte
		var at time.Time
		if err = rows.Scan(&id, &p, &a, &outcome, &raw, &digest, &at); err != nil {
			return nil, unavailable(err)
		}
		var change sc.RuntimeAssignmentChange
		if len(raw) > maxSkillAssignmentIngressBytes || decodeDirectTextJSON(raw, &change) != nil || change.Validate() != nil || change.ID.String() != id || change.ProjectID != project || change.AgentID != agent || p != project.String() || a != agent.String() || outcome != "queued" || c.TriggerInputDigest(raw) != f.Digest(digest) || !at.Equal(change.RecordedAt.Time()) {
			return nil, unavailable(nil)
		}
		out = append(out, change)
	}
	if err = rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	slices.SortFunc(out, func(a, b sc.RuntimeAssignmentChange) int {
		if a.AssignmentSequence < b.AssignmentSequence {
			return -1
		}
		if a.AssignmentSequence > b.AssignmentSequence {
			return 1
		}
		return 0
	})
	for n := 1; n < len(out); n++ {
		if out[n-1].AssignmentSequence == out[n].AssignmentSequence {
			return nil, unavailable(nil)
		}
	}
	return out, ctx.Err()
}

func loadRoundInput(ctx context.Context, x postgres.SQLExecutor, e i.ExecutionID) (c.RoundInput, bool, error) {
	var zero c.RoundInput
	var id, project, agent, snapshot, predecessor, binding, digest string
	var initial, cutoff, through, version int64
	var raw []byte
	var at time.Time
	err := x.QueryRow(ctx, `SELECT id::text,project_id::text,agent_id::text,snapshot_id::text,predecessor_round_id::text,skill_input_binding_id::text,initial_sequence,assignment_cutoff,transcript_through,checkpoint_version,content,digest,created_at FROM agenteam_execution.round_inputs WHERE execution_id=$1`, e.String()).Scan(&id, &project, &agent, &snapshot, &predecessor, &binding, &initial, &cutoff, &through, &version, &raw, &digest, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, unavailable(err)
	}
	v, err := c.DecodeRoundInput(raw)
	if err != nil {
		return zero, false, unavailable(nil)
	}
	f := v.Fields()
	if f.ExecutionID != e || f.RoundID.String() != id || f.Initial.Request.ProjectID.String() != project || f.Initial.Request.AgentID.String() != agent || f.SnapshotID.String() != snapshot || f.PredecessorRoundID.String() != predecessor || f.SkillInputBindingID.String() != binding || int64(f.Initial.AssignmentSequence) != initial || int64(f.AssignmentCutoff) != cutoff || int64(f.TranscriptThrough) != through || int64(f.CheckpointVersion) != version || string(v.Digest()) != digest || !f.CreatedAt.Time().Equal(at) {
		return zero, false, unavailable(nil)
	}
	if err = verifyRoundInputRows(ctx, x, v); err != nil {
		return zero, false, err
	}
	return v, true, ctx.Err()
}

func verifyRoundInputRows(ctx context.Context, x postgres.SQLExecutor, v c.RoundInput) error {
	fields := v.Fields()
	source, err := loadRoundInputSource(ctx, x, fields.ExecutionID)
	if err != nil {
		return err
	}
	initial, err := directTextJSON(source.initial.Initial)
	if err != nil {
		return err
	}
	expected, _ := directTextJSON(fields.Initial)
	if source.receipt != fields.Source || source.prefix != fields.TranscriptPrefixDigest || source.original.snapshot.Digest() != fields.SnapshotDigest || source.original.snapshot.Fields().ID != fields.SnapshotID || !bytes.Equal(initial, expected) || source.row.summary.Version < fields.CheckpointVersion {
		return fault(f.ConfirmationStale)
	}
	pending, err := loadRoundPending(ctx, x, fields.ExecutionID, fields.Initial.Request.ProjectID, fields.Initial.Request.AgentID)
	if err != nil {
		return err
	}
	byID := make(map[sc.AssignmentChangeID]sc.RuntimeAssignmentChange)
	for _, p := range pending {
		if p.AssignmentSequence <= fields.AssignmentCutoff {
			byID[p.ID] = p
		}
	}
	if len(byID) != len(fields.Dispositions) {
		return unavailable(nil)
	}
	rows, err := x.Query(ctx, `SELECT change_id::text,round_input_id::text,assignment_sequence,state,reason FROM agenteam_execution.skill_assignment_dispositions WHERE execution_id=$1 ORDER BY assignment_sequence`, fields.ExecutionID.String())
	if err != nil {
		return unavailable(err)
	}
	n := 0
	for rows.Next() {
		var id, r, state, reason string
		var seq int64
		if err = rows.Scan(&id, &r, &seq, &state, &reason); err != nil {
			rows.Close()
			return unavailable(err)
		}
		if n >= len(fields.Dispositions) {
			rows.Close()
			return unavailable(nil)
		}
		d := fields.Dispositions[n]
		n++
		if d.ChangeID.String() != id || r != fields.RoundID.String() || int64(d.Sequence) != seq || d.State != state || d.Reason != reason || byID[d.ChangeID].AssignmentSequence != d.Sequence {
			rows.Close()
			return unavailable(nil)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return unavailable(err)
	}
	if n != len(fields.Dispositions) {
		return unavailable(nil)
	}
	if fields.Control != nil {
		for _, a := range fields.Control.Payload {
			if byID[a.ChangeID].Binding != a.Binding {
				return unavailable(nil)
			}
		}
	}
	var control, message []byte
	var controlID, roundID, digest, messageDigest string
	var at time.Time
	err = x.QueryRow(ctx, `SELECT c.id::text,c.round_input_id::text,c.content,c.digest,t.message,t.digest,t.created_at FROM agenteam_execution.control_inputs c JOIN agenteam_execution.transcript_entries t ON (t.execution_id,t.sequence,t.round_input_id)=(c.execution_id,c.transcript_sequence,c.round_input_id) WHERE c.execution_id=$1`, fields.ExecutionID.String()).Scan(&controlID, &roundID, &control, &digest, &message, &messageDigest, &at)
	if fields.Control == nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			if err != nil {
				return unavailable(err)
			}
			return unavailable(nil)
		}
		var count int64
		err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_execution.transcript_entries WHERE execution_id=$1 AND sequence=4`, fields.ExecutionID.String()).Scan(&count)
		if err != nil || count != 0 {
			return unavailable(err)
		}
		return ctx.Err()
	}
	if err != nil {
		return unavailable(err)
	}
	want, err := directTextJSON(fields.Control)
	if err != nil {
		return err
	}
	if controlID != fields.Control.ID.String() || roundID != fields.RoundID.String() || !bytes.Equal(control, want) || !bytes.Equal(message, want) || digest != string(c.TriggerInputDigest(want)) || messageDigest != digest || !at.Equal(fields.CreatedAt.Time()) {
		return unavailable(nil)
	}
	return ctx.Err()
}

func insertRoundInput(ctx context.Context, x postgres.SQLExecutor, v c.RoundInput, before f.Version) error {
	q := v.Fields()
	if v.Validate() != nil || q.CheckpointVersion != before+1 {
		return invalid()
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.round_inputs(id,execution_id,project_id,agent_id,snapshot_id,predecessor_round_id,skill_input_binding_id,initial_sequence,assignment_cutoff,transcript_through,checkpoint_version,content,digest,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, q.RoundID.String(), q.ExecutionID.String(), q.Initial.Request.ProjectID.String(), q.Initial.Request.AgentID.String(), q.SnapshotID.String(), q.PredecessorRoundID.String(), q.SkillInputBindingID.String(), int64(q.Initial.AssignmentSequence), int64(q.AssignmentCutoff), int64(q.TranscriptThrough), int64(q.CheckpointVersion), v.CanonicalBytes(), string(v.Digest()), q.CreatedAt.Time())
	if err != nil || tag.RowsAffected() != 1 {
		return unavailable(err)
	}
	for _, d := range q.Dispositions {
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_execution.skill_assignment_dispositions(execution_id,change_id,round_input_id,assignment_sequence,state,reason) VALUES($1,$2,$3,$4,$5,$6)`, q.ExecutionID.String(), d.ChangeID.String(), q.RoundID.String(), int64(d.Sequence), d.State, d.Reason)
		if err != nil || tag.RowsAffected() != 1 {
			return unavailable(err)
		}
	}
	if q.Control != nil {
		raw, err := directTextJSON(q.Control)
		if err != nil {
			return err
		}
		digest := string(c.TriggerInputDigest(raw))
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_execution.control_inputs(id,execution_id,round_input_id,kind,transcript_sequence,content,digest) VALUES($1,$2,$3,'skills_added',4,$4,$5)`, q.Control.ID.String(), q.ExecutionID.String(), q.RoundID.String(), raw, digest)
		if err != nil || tag.RowsAffected() != 1 {
			return unavailable(err)
		}
		tag, err = x.Exec(ctx, `INSERT INTO agenteam_execution.transcript_entries(execution_id,sequence,round_id,round_input_id,kind,message,digest,created_at) VALUES($1,4,NULL,$2,'control',$3,$4,$5)`, q.ExecutionID.String(), q.RoundID.String(), raw, digest, q.CreatedAt.Time())
		if err != nil || tag.RowsAffected() != 1 {
			return unavailable(err)
		}
	}
	tag, err = x.Exec(ctx, `UPDATE agenteam_execution.executions SET version=version+1,updated_at=GREATEST($3,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status='running' AND cancel_requested_at IS NULL`, q.ExecutionID.String(), int64(before), q.CreatedAt.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}
