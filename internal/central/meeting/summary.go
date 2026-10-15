package meeting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type SummaryInput struct {
	GenerationID    c.SummaryGenerationID `json:"generation_id"`
	ProjectID       i.ProjectID           `json:"project_id"`
	MeetingID       c.MeetingID           `json:"meeting_id"`
	TurnID          c.TurnID              `json:"turn_id"`
	PreviousVersion int64                 `json:"previous_version"`
	Initial         bool                  `json:"initial"`
	InputDigest     f.Digest              `json:"input_digest"`
	Messages        []FrozenMessage       `json:"messages"`
}
type SummaryCandidate struct {
	Title      *string `json:"title,omitempty"`
	Goals      string  `json:"goals"`
	Decisions  string  `json:"decisions"`
	Unresolved string  `json:"unresolved"`
	Facts      string  `json:"facts"`
}
type SummaryGenerator interface {
	GenerateSummary(context.Context, SummaryInput) (SummaryCandidate, error)
}

func ParseSummary(raw []byte, initial bool) (SummaryCandidate, error) {
	var out SummaryCandidate
	if len(raw) == 0 || len(raw) > 64<<10 {
		return out, fault(f.InvalidArgument)
	}
	// Check field presence and shape independently of zero-value strings. Empty
	// strings are legal summary facts; missing, null and array values are not.
	fields := map[string]json.RawMessage{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return out, fault(f.InvalidArgument)
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return out, fault(f.InvalidArgument)
		}
		name, ok := key.(string)
		if !ok {
			return out, fault(f.InvalidArgument)
		}
		if _, exists := fields[name]; exists {
			return out, fault(f.InvalidArgument)
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return out, fault(f.InvalidArgument)
		}
		fields[name] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return out, fault(f.InvalidArgument)
	}
	want := map[string]bool{"goals": true, "decisions": true, "unresolved": true, "facts": true}
	if initial {
		want["title"] = true
	}
	if len(fields) != len(want) {
		return out, fault(f.InvalidArgument)
	}
	for key := range want {
		value, ok := fields[key]
		if !ok || len(value) == 0 || value[0] != '"' {
			return out, fault(f.InvalidArgument)
		}
	}
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil {
		return SummaryCandidate{}, fault(f.InvalidArgument)
	}
	if initial && (out.Title == nil || !c.ValidText(*out.Title, 256) || strings.TrimSpace(*out.Title) == "") {
		return SummaryCandidate{}, fault(f.InvalidArgument)
	}
	for _, value := range []string{out.Goals, out.Decisions, out.Unresolved, out.Facts} {
		if !c.ValidText(value, 16<<10) {
			return SummaryCandidate{}, fault(f.InvalidArgument)
		}
	}
	return out, nil
}

func (r *Runtime) prepareSummary(ctx context.Context, p i.ProjectID, m c.MeetingID, t c.TurnID) (SummaryInput, bool, error) {
	var out SummaryInput
	ready := false
	err := r.transaction(ctx, p, m, "summary-prepare", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		s, err := loadSession(ctx, x, p, m)
		if err != nil {
			return err
		}
		var status string
		var cancel bool
		err = x.QueryRow(ctx, `SELECT status,cancel_requested FROM agenteam_meeting.turns WHERE meeting_id=$1 AND id=$2`, m.String(), t.String()).Scan(&status, &cancel)
		if err != nil {
			return portError(err)
		}
		if status != "finalizing" || cancel || s.Deleting {
			return fault(f.InvalidState)
		}
		frozen, err := captureContext(ctx, x, p, m, t)
		if err != nil {
			return err
		}
		raw, err := canonical(frozen.Messages)
		if err != nil {
			return err
		}
		inputDigest := digest(raw)
		version := int64(0)
		if s.Summary != nil {
			version = int64(s.Summary.Version)
		}
		var id, state string
		var stored []byte
		err = x.QueryRow(ctx, `SELECT id,status,input_data FROM agenteam_meeting.summary_generations WHERE turn_id=$1 AND input_digest=$2 AND expected_summary_version=$3`, t.String(), inputDigest.String(), version).Scan(&id, &state, &stored)
		if err == nil {
			generation, err := f.ParseID[c.SummaryGeneration](id)
			if err != nil {
				return fault(f.InternalError)
			}
			var messages []FrozenMessage
			if json.Unmarshal(stored, &messages) != nil {
				return fault(f.InternalError)
			}
			out = SummaryInput{generation, p, m, t, version, s.Title == nil, inputDigest, messages}
			ready = state == "prepared" || state == "generating"
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return portError(err)
		}
		generation, err := f.NewID[c.SummaryGeneration]()
		if err != nil {
			return portError(err)
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.summary_generations(id,meeting_id,turn_id,expected_summary_version,input_data,input_digest,initial,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,'prepared',$8)`, generation.String(), m.String(), t.String(), version, raw, inputDigest.String(), s.Title == nil, now()); err != nil {
			return portError(err)
		}
		out = SummaryInput{generation, p, m, t, version, s.Title == nil, inputDigest, frozen.Messages}
		ready = true
		return nil
	})
	return out, ready, err
}
func (r *Runtime) finalize(ctx context.Context, p i.ProjectID, m c.MeetingID, t c.TurnID) error {
	if nilPort(r.deps.Summaries) {
		return fault(f.DependencyUnbound)
	}
	input, ready, err := r.prepareSummary(ctx, p, m, t)
	if err != nil || !ready {
		return err
	}
	if err = r.transaction(ctx, p, m, "summary-start", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var state string
		if err := x.QueryRow(ctx, `SELECT status FROM agenteam_meeting.summary_generations WHERE id=$1`, input.GenerationID.String()).Scan(&state); err != nil {
			return portError(err)
		}
		if state == "generating" {
			return nil
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_generations SET status='generating',attempt=attempt+1 WHERE id=$1 AND status='prepared' AND attempt<3`, input.GenerationID.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ResourceBusy)
		}
		return nil
	}); err != nil {
		return err
	}
	candidate, err := r.deps.Summaries.GenerateSummary(ctx, input)
	var cancelled bool
	if observeErr := r.transaction(ctx, p, m, "summary-cancel-intent", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var e error
		cancelled, e = summaryCancellationRequested(ctx, x, input)
		return e
	}); observeErr != nil {
		return observeErr
	}
	if cancelled {
		return r.cancelSummary(ctx, input)
	}
	if err != nil {
		// A failed call leaves the Turn finalizing and the queue blocked. It
		// never substitutes the previous summary or silently chooses a model.
		code := string(f.DependencyUnavailable)
		var known *f.Fault
		if errors.As(err, &known) {
			code = string(known.Code)
		}
		// Unknown retains the original call/generation for read-only recovery;
		// it must never become a fresh attempt through a generic retry button.
		retain := known != nil && (known.CommitState == f.Unknown || known.Code == f.ResourceBusy)
		record := func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
			if _, e := x.Exec(ctx, `UPDATE agenteam_meeting.summary_generations SET status=CASE WHEN $3 THEN status ELSE 'failed' END,error_code=$2 WHERE id=$1 AND status='generating'`, input.GenerationID.String(), code, retain); e != nil {
				return portError(e)
			}
			_, e := x.Exec(ctx, `UPDATE agenteam_meeting.turns SET summary_error=$2 WHERE id=$1 AND status='finalizing'`, t.String(), code)
			return portError(e)
		}
		var recordErr error
		if retain {
			recordErr = r.transaction(ctx, p, m, "summary-unknown", record)
		} else {
			recordErr = r.summaryTransaction(ctx, input, "failed", record)
		}
		if recordErr != nil {
			return recordErr
		}
		return err
	}
	return r.publishSummary(ctx, input, candidate)
}
func (r *Runtime) publishSummary(ctx context.Context, input SummaryInput, candidate SummaryCandidate) error {
	rawCandidate, err := json.Marshal(candidate)
	if err != nil {
		return portError(err)
	}
	if _, err = ParseSummary(rawCandidate, input.Initial); err != nil {
		return err
	}
	return r.summaryTransaction(ctx, input, "published", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		s, err := loadSession(ctx, x, input.ProjectID, input.MeetingID)
		if err != nil {
			return err
		}
		var status, generationStatus string
		var cancel bool
		err = x.QueryRow(ctx, `SELECT t.status,t.cancel_requested,g.status FROM agenteam_meeting.turns t JOIN agenteam_meeting.summary_generations g ON g.turn_id=t.id WHERE t.id=$1 AND g.id=$2`, input.TurnID.String(), input.GenerationID.String()).Scan(&status, &cancel, &generationStatus)
		if err != nil {
			return portError(err)
		}
		if generationStatus == "published" && status == "completed" {
			return nil
		}
		if status != "finalizing" || cancel || s.Deleting || generationStatus != "generating" {
			return fault(f.InvalidState)
		}
		version := int64(0)
		if s.Summary != nil {
			version = int64(s.Summary.Version)
		}
		if version != input.PreviousVersion || (s.Title == nil) != input.Initial {
			return fault(f.VersionConflict)
		}
		frozen, err := captureContext(ctx, x, input.ProjectID, input.MeetingID, input.TurnID)
		if err != nil {
			return err
		}
		raw, err := canonical(frozen.Messages)
		if err != nil {
			return err
		}
		if digest(raw) != input.InputDigest {
			return fault(f.ConfirmationStale)
		}
		summary := c.RollingSummary{Version: f.Version(version + 1), Goals: candidate.Goals, Decisions: candidate.Decisions, Unresolved: candidate.Unresolved, Facts: candidate.Facts, InputDigest: input.InputDigest}
		summaryRaw, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET title=COALESCE(title,$2),summary=$3,summary_version=$4,summary_input_digest=$5,version=version+1,updated_at=$6 WHERE id=$1`, input.MeetingID.String(), candidate.Title, summaryRaw, version+1, input.InputDigest.String(), now()); err != nil {
			return portError(err)
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.summary_generations SET status='published',candidate=$2,error_code='' WHERE id=$1`, input.GenerationID.String(), rawCandidate); err != nil {
			return portError(err)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_meeting.turns SET status='completed',version=version+1,summary_error='',completed_at=$2 WHERE id=$1`, input.TurnID.String(), now())
		return portError(err)
	})
}
