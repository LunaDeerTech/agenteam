package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type directTextRecord struct {
	snapshot       c.DirectTextSnapshot
	round          c.DirectTextRound
	startedVersion f.Version
	startedEvent   event.EventID
	terminal       *directTextTerminal
}
type directTextTerminal struct {
	status          c.Status
	reason          string
	before, version f.Version
	through         f.Sequence
	at              f.Instant
	eventID         event.EventID
	response        *mc.ModelResponse
	responseBytes   []byte
	event           event.Event
	plan            oc.AppendPlan
}

const directTextColumns = `s.content,s.digest,s.execution_id::text,s.project_id::text,s.agent_id::text,s.id::text,s.start_id::text,s.process_id::text,s.input_digest,s.context_digest,s.created_at,s.started_version,s.started_event_id::text,r.content,r.digest,r.id::text,r.input_binding_id::text,r.call_id::text,r.input_digest,r.context_digest,r.created_at,r.transcript_through,r.terminal_status,r.terminal_version,r.terminal_reason,r.terminal_event_id::text,r.response,r.response_digest,r.finished_at`

func loadDirectText(ctx context.Context, x postgres.SQLExecutor, execution i.ExecutionID) (*directTextRecord, error) {
	var snapshotRaw, roundRaw, responseRaw []byte
	var sd, e, p, a, snapshotID, startID, process, inputDigest, contextDigest, startedEvent, rd, roundID, bindingID, callID, roundInput, roundContext string
	var snapshotAt, roundAt time.Time
	var startedVersion, through int64
	var status, reason, terminalEvent, responseDigest *string
	var terminalVersion *int64
	var finished *time.Time
	err := x.QueryRow(ctx, `SELECT `+directTextColumns+` FROM agenteam_execution.snapshots s JOIN agenteam_execution.rounds r ON (r.execution_id,r.snapshot_id,r.start_id)=(s.execution_id,s.id,s.start_id) WHERE s.execution_id=$1`, execution.String()).Scan(&snapshotRaw, &sd, &e, &p, &a, &snapshotID, &startID, &process, &inputDigest, &contextDigest, &snapshotAt, &startedVersion, &startedEvent, &roundRaw, &rd, &roundID, &bindingID, &callID, &roundInput, &roundContext, &roundAt, &through, &status, &terminalVersion, &reason, &terminalEvent, &responseRaw, &responseDigest, &finished)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	r := &directTextRecord{startedVersion: f.Version(startedVersion)}
	if r.snapshot, err = c.DecodeDirectTextSnapshot(snapshotRaw); err != nil {
		return nil, unavailable(nil)
	}
	if r.round, err = c.DecodeDirectTextRound(roundRaw); err != nil {
		return nil, unavailable(nil)
	}
	if r.startedEvent, err = f.ParseID[event.EventIdentity](startedEvent); err != nil {
		return nil, unavailable(nil)
	}
	sv, rv := r.snapshot.Fields(), r.round.Fields()
	iv := sv.Context.Input().Fields()
	digest, err := model.TextInputDigest(rv.Messages)
	if err != nil || startedVersion < 2 || sv.Context.InputDigest() != f.Digest(inputDigest) || sv.Context.Digest() != f.Digest(contextDigest) || r.snapshot.Digest() != f.Digest(sd) || sv.ID.String() != snapshotID || sv.StartID.String() != startID || sv.ProcessID.String() != process || !sv.CreatedAt.Time().Equal(snapshotAt) || e != execution.String() || iv.Request.ExecutionID != execution || iv.Request.Launch.ProjectID.String() != p || iv.Request.Launch.AgentID.String() != a || r.round.Digest() != f.Digest(rd) || rv.ID.String() != roundID || rv.InputBindingID.String() != bindingID || rv.CallID.String() != callID || rv.ExecutionID != execution || rv.SnapshotID != sv.ID || rv.StartID != sv.StartID || rv.ContextDigest != sv.Context.Digest() || roundContext != contextDigest || rv.Input.Digest != digest || string(digest) != roundInput || !rv.CreatedAt.Time().Equal(roundAt) || !rv.CreatedAt.Time().Equal(sv.CreatedAt.Time()) {
		return nil, unavailable(nil)
	}
	if status == nil {
		if terminalVersion != nil || reason != nil || terminalEvent != nil || responseRaw != nil || responseDigest != nil || finished != nil || through != 1 {
			return nil, unavailable(nil)
		}
	} else {
		if terminalVersion == nil || reason == nil || terminalEvent == nil || finished == nil || *terminalVersion <= startedVersion {
			return nil, unavailable(nil)
		}
		t := &directTextTerminal{status: c.Status(*status), reason: *reason, version: f.Version(*terminalVersion), through: f.Sequence(through)}
		if t.at, err = f.NewInstant(*finished); err != nil {
			return nil, unavailable(nil)
		}
		if t.eventID, err = f.ParseID[event.EventIdentity](*terminalEvent); err != nil {
			return nil, unavailable(nil)
		}
		if t.at.Time().Before(sv.CreatedAt.Time()) || !t.status.Terminal() {
			return nil, unavailable(nil)
		}
		if t.status == c.Succeeded {
			var response mc.ModelResponse
			if decodeDirectTextJSON(responseRaw, &response) != nil || !stableDirectTextResponse(response, rv.CallID) || responseDigest == nil || c.TriggerInputDigest(responseRaw) != f.Digest(*responseDigest) || through != 2 || t.reason != "completed" {
				return nil, unavailable(nil)
			}
			t.response = &response
			t.responseBytes = bytes.Clone(responseRaw)
		} else if responseRaw != nil || responseDigest != nil || through != 1 || !(t.status == c.Cancelled && t.reason == "cancelled" || t.status == c.Failed && (t.reason == "model_failed" || t.reason == "incomplete_response" || t.reason == "runtime_failed")) {
			return nil, unavailable(nil)
		}
		r.terminal = t
	}
	if err = checkDirectTextEntry(ctx, x, execution, rv.ID, 1, "input", rv.Messages[1], sv.CreatedAt); err != nil {
		return nil, err
	}
	if r.terminal != nil && r.terminal.response != nil {
		if err = checkDirectTextEntry(ctx, x, execution, rv.ID, 2, "assistant", r.terminal.response.Message, r.terminal.at); err != nil {
			return nil, err
		}
	}
	return r, ctx.Err()
}

func directTextJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, invalid()
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil || len(raw) > c.MaxDirectTextRoundBytes {
		return nil, invalid()
	}
	return raw, nil
}
func decodeDirectTextJSON(raw []byte, target any) error {
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || !bytes.Equal(raw, canonical) {
		return invalid()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return invalid()
	}
	return nil
}
func stableDirectTextResponse(response mc.ModelResponse, call mc.CallID) bool {
	if response.Validate() != nil || response.CallID != call || response.FinishReason != "stop" || len(response.Message.Parts) == 0 {
		return false
	}
	for _, part := range response.Message.Parts {
		if part.Text == nil {
			return false
		}
	}
	return true
}
func checkDirectTextEntry(ctx context.Context, x postgres.SQLExecutor, e i.ExecutionID, round c.RoundID, sequence int64, kind string, message mc.Message, at f.Instant) error {
	var actualRound, actualKind, digest string
	var raw []byte
	var created time.Time
	err := x.QueryRow(ctx, `SELECT round_id::text,kind,message,digest,created_at FROM agenteam_execution.transcript_entries WHERE execution_id=$1 AND sequence=$2`, e.String(), sequence).Scan(&actualRound, &actualKind, &raw, &digest, &created)
	if err != nil {
		return unavailable(err)
	}
	expected, err := directTextJSON(message)
	if err != nil || actualRound != round.String() || actualKind != kind || !bytes.Equal(raw, expected) || string(c.TriggerInputDigest(raw)) != digest || !at.Time().Equal(created) {
		return unavailable(nil)
	}
	return ctx.Err()
}
func insertDirectTextEntry(ctx context.Context, x postgres.SQLExecutor, e i.ExecutionID, round c.RoundID, sequence int64, kind string, message mc.Message, at f.Instant) error {
	raw, err := directTextJSON(message)
	if err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.transcript_entries(execution_id,sequence,round_id,kind,message,digest,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, e.String(), sequence, round.String(), kind, raw, string(c.TriggerInputDigest(raw)), at.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return nil
}
func insertDirectText(ctx context.Context, x postgres.SQLExecutor, run *directTextCall, eventID event.EventID) error {
	s, r := run.snapshot.Fields(), run.round.Fields()
	q := run.request
	if run.summary.Version == f.Version(math.MaxInt64) {
		return fault(f.InvalidState)
	}
	version := run.summary.Version + 1
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.snapshots(id,execution_id,project_id,agent_id,start_id,process_id,input_digest,context_digest,schema_version,content,digest,started_version,started_event_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$10,$11,$12,$13)`, s.ID.String(), q.ExecutionID.String(), q.Launch.ProjectID.String(), q.Launch.AgentID.String(), s.StartID.String(), s.ProcessID.String(), string(s.Context.InputDigest()), string(s.Context.Digest()), run.snapshot.CanonicalBytes(), string(run.snapshot.Digest()), int64(version), eventID.String(), s.CreatedAt.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	tag, err = x.Exec(ctx, `INSERT INTO agenteam_execution.rounds(id,execution_id,snapshot_id,start_id,input_binding_id,call_id,input_digest,context_digest,schema_version,content,digest,created_at,transcript_through) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$10,$11,1)`, r.ID.String(), q.ExecutionID.String(), s.ID.String(), s.StartID.String(), r.InputBindingID.String(), r.CallID.String(), string(r.Input.Digest), string(r.ContextDigest), run.round.CanonicalBytes(), string(run.round.Digest()), r.CreatedAt.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	if err = insertDirectTextEntry(ctx, x, q.ExecutionID, r.ID, 1, "input", r.Messages[1], r.CreatedAt); err != nil {
		return err
	}
	tag, err = x.Exec(ctx, `UPDATE agenteam_execution.executions SET status='running',version=version+1,snapshot_id=$3,started_at=$4,updated_at=GREATEST($4,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status='preparing' AND cancel_requested_at IS NULL`, q.ExecutionID.String(), int64(run.summary.Version), s.ID.String(), s.CreatedAt.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

func saveDirectTextTerminal(ctx context.Context, x postgres.SQLExecutor, run *directTextCall) error {
	t := run.terminal
	r := run.round.Fields()
	if t == nil || t.before == f.Version(math.MaxInt64) || t.version != t.before+1 {
		return fault(f.InvalidState)
	}
	var response any
	var digest any
	if t.response != nil {
		response = t.responseBytes
		digest = string(c.TriggerInputDigest(t.responseBytes))
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_execution.rounds SET terminal_status=$3,terminal_version=$4,terminal_reason=$5,terminal_event_id=$6,response=$7,response_digest=$8,finished_at=$9,transcript_through=$10 WHERE id=$1 AND execution_id=$2 AND terminal_status IS NULL`, r.ID.String(), r.ExecutionID.String(), string(t.status), int64(t.version), t.reason, t.eventID.String(), response, digest, t.at.Time(), int64(t.through))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	if t.response != nil {
		if err = insertDirectTextEntry(ctx, x, r.ExecutionID, r.ID, 2, "assistant", t.response.Message, t.at); err != nil {
			return err
		}
	}
	tag, err = x.Exec(ctx, `UPDATE agenteam_execution.executions SET status=$3,version=version+1,completed_at=$4,updated_at=GREATEST($4,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status='running' AND snapshot_id=$5 AND ($3<>'succeeded' OR cancel_requested_at IS NULL)`, r.ExecutionID.String(), int64(t.before), string(t.status), t.at.Time(), r.SnapshotID.String())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}
func directTextTerminalMatches(row *executionRecord, stored *directTextRecord, t *directTextTerminal) bool {
	if row == nil || stored == nil || stored.terminal == nil || t == nil {
		return false
	}
	s := stored.terminal
	return row.summary.Status == t.status && row.summary.Version == t.version && row.summary.CompletedAt != nil && row.summary.CompletedAt.Time().Equal(t.at.Time()) && s.status == t.status && s.version == t.version && s.reason == t.reason && s.through == t.through && s.eventID == t.eventID && s.at.Time().Equal(t.at.Time()) && bytes.Equal(s.responseBytes, t.responseBytes)
}
func directTextReceipt(row *executionRecord, stored *directTextRecord) c.DirectTextReceipt {
	if row == nil || stored == nil {
		return c.DirectTextReceipt{}
	}
	s, r := stored.snapshot.Fields(), stored.round.Fields()
	through := f.Sequence(1)
	if stored.terminal != nil {
		through = stored.terminal.through
	}
	return c.DirectTextReceipt{ExecutionID: row.summary.ID, StartID: s.StartID, SnapshotID: s.ID, RoundID: r.ID, CallID: r.CallID, Status: row.summary.Status, Version: row.summary.Version, TranscriptThrough: through}
}
