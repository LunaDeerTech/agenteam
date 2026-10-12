package execution

import (
	"bytes"
	"context"
	"encoding/json"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

var _ c.RoundProjectionReader = (*RoundInputDeterminer)(nil)

// WithCommittedRoundProjection reads the immutable input selected by E/next.
// It shares Determine's actual-call owner, including the original Store return;
// neither a successful callback nor a cancellation request retires that owner.
// No current configuration/Registry lookup or write occurs in this path.
func (d *RoundInputDeterminer) WithCommittedRoundProjection(ctx context.Context, e i.ExecutionID, next c.RoundID, consume func(c.RoundProjectionSource) error) error {
	if consume == nil {
		return invalid()
	}
	ctx, leave, err := d.enter(ctx, e, next)
	if err != nil {
		return err
	}
	defer leave()
	scope, err := d.roundScope(ctx, e, next)
	if err != nil {
		return err
	}
	return d.roundReadTx(ctx, "projection", e, next, roundInputLocks(scope.summary.ProjectID, scope.summary.AgentID, e), func(ctx context.Context, tx f.Tx) error {
		x, err := d.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		// This reader also verifies consumption, the fixed prefix, every pending
		// disposition and the original seq4/control presence and canonical bytes.
		input, found, err := loadRoundInput(ctx, x, e)
		if err != nil {
			return err
		}
		if !found {
			return fault(f.NotFound)
		}
		fields := input.Fields()
		if fields.RoundID != next || fields.Initial.Request.ProjectID != scope.summary.ProjectID || fields.Initial.Request.AgentID != scope.summary.AgentID {
			return fault(f.ConfirmationStale)
		}
		source, err := loadCommittedRoundProjection(ctx, x, input)
		if err != nil {
			return err
		}
		return lendRoundProjection(ctx, source, consume)
	})
}

// Called only after loadRoundInput has verified the complete persisted input in
// the same locked transaction. Re-reading here lends original bytes without
// making 68's writer or its private source shape a public projection API.
func loadCommittedRoundProjection(ctx context.Context, x postgres.SQLExecutor, input c.RoundInput) (source c.RoundProjectionSource, err error) {
	defer func() {
		if err != nil {
			clearRoundProjectionLoan(&source)
		}
	}()
	fields := input.Fields()
	old, err := loadDirectText(ctx, x, fields.ExecutionID)
	if err != nil {
		return source, err
	}
	if old == nil || old.toolPending == nil {
		return source, fault(f.InvalidState)
	}
	defer clear(old.toolPending.original)
	defer clear(old.toolPending.canonical)
	round, ok := old.round.(toolExecutionRound)
	if !ok || round.Fields().ID != fields.PredecessorRoundID || old.snapshot.Fields().ID != fields.SnapshotID || old.snapshot.Digest() != fields.SnapshotDigest {
		return source, fault(f.ConfirmationStale)
	}
	call, err := toolBinding(old)
	if err != nil {
		return source, err
	}
	var operation, attempt, request, inputDigest, fingerprint, terminal, result string
	var resultBytes int64
	err = x.QueryRow(ctx, `SELECT operation_id::text,attempt_id::text,request_id::text,input_digest,fingerprint,terminal_digest,result_digest,result_bytes FROM agenteam_execution.tool_result_consumptions WHERE execution_id=$1`, fields.ExecutionID.String()).Scan(&operation, &attempt, &request, &inputDigest, &fingerprint, &terminal, &result, &resultBytes)
	if err != nil {
		return source, unavailable(err)
	}
	op, err := f.ParseID[tc.Operation](operation)
	if err != nil {
		return source, unavailable(nil)
	}
	a, err := f.ParseID[tc.Attempt](attempt)
	if err != nil {
		return source, unavailable(nil)
	}
	binding := sealedToolResultBinding{Call: call, OperationID: op, AttemptID: a, RequestID: p.ID(request), InputDigest: f.Digest(inputDigest), Fingerprint: f.Digest(fingerprint), TerminalDigest: f.Digest(terminal), ResultDigest: f.Digest(result), ResultBytes: f.Progress(resultBytes)}
	if call.RunnerBinding == tc.RunCommandRegistration().Binding {
		binding, err = loadCommandSealedBinding(ctx, x, binding)
		if err != nil {
			return source, err
		}
	} else {
		binding.Outcome, binding.Attempted = "success", true
	}
	if binding.validate() != nil || binding.OperationID != fields.Source.OperationID || binding.AttemptID != fields.Source.AttemptID || binding.ResultDigest != fields.Source.ResultDigest || call.CallID != fields.Source.CallID {
		return source, unavailable(nil)
	}
	source = c.RoundProjectionSource{SchemaVersion: 1, Input: input, Snapshot: old.snapshot, Predecessor: round.ToolCallRound, Arguments: bytes.Clone(old.toolPending.original)}
	switch call.RunnerBinding {
	case tc.ReadFileRegistration().Binding:
		source.Result = tc.ReadFileResultBinding(binding)
	case tc.WriteFileRegistration().Binding:
		written := tc.WriteFileResultBinding(binding)
		source.WriteResult = &written
	case tc.RunCommandRegistration().Binding:
		command := tc.RunCommandResultBinding(binding)
		source.CommandResult = &command
	default:
		return source, unavailable(nil)
	}
	rows, err := x.Query(ctx, `SELECT sequence,round_id::text,kind,message,digest FROM agenteam_execution.transcript_entries WHERE execution_id=$1 AND sequence<=3 ORDER BY sequence`, fields.ExecutionID.String())
	if err != nil {
		return source, unavailable(err)
	}
	source.Transcript, err = scanRoundProjectionEntries(ctx, rows, fields)
	if err != nil {
		return source, err
	}
	if fields.Control != nil {
		source.ControlCanonical, err = directTextJSON(fields.Control)
		if err != nil {
			return source, err
		}
	}
	return source, ctx.Err()
}

type roundProjectionRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

func scanRoundProjectionEntries(ctx context.Context, rows roundProjectionRows, fields c.RoundInputFields) (entries []c.RoundProjectionEntry, err error) {
	defer func() {
		rows.Close()
		if err == nil && rows.Err() != nil {
			err = unavailable(rows.Err())
		}
		if err != nil {
			for n := range entries {
				clear(entries[n].CanonicalMessage)
			}
			entries = nil
		}
	}()
	messages := make([]json.RawMessage, 0, 3)
	for rows.Next() {
		var sequence int64
		var id, kind, digest string
		var raw []byte
		if err = rows.Scan(&sequence, &id, &kind, &raw, &digest); err != nil {
			return entries, unavailable(err)
		}
		if sequence != int64(len(entries)+1) || sequence > 3 || id != fields.PredecessorRoundID.String() || kind != []string{"input", "assistant", "tool_result"}[sequence-1] || c.TriggerInputDigest(raw) != f.Digest(digest) {
			return entries, unavailable(nil)
		}
		var message mc.Message
		if decodeDirectTextJSON(raw, &message) != nil || message.Validate() != nil {
			return entries, unavailable(nil)
		}
		canonical, encodeErr := directTextJSON(message)
		if encodeErr != nil || !bytes.Equal(canonical, raw) {
			return entries, unavailable(nil)
		}
		loan := bytes.Clone(raw)
		entries = append(entries, c.RoundProjectionEntry{Sequence: f.Sequence(sequence), RoundID: fields.PredecessorRoundID, Kind: kind, CanonicalMessage: loan, Digest: f.Digest(digest)})
		messages = append(messages, loan)
	}
	if err = rows.Err(); err != nil {
		return entries, unavailable(err)
	}
	if len(entries) != 3 || entries[2].Digest != fields.Source.MessageDigest {
		return entries, unavailable(nil)
	}
	prefix, err := json.Marshal(messages)
	if err != nil || c.TriggerInputDigest(prefix) != fields.TranscriptPrefixDigest {
		return entries, unavailable(nil)
	}
	return entries, ctx.Err()
}

func lendRoundProjection(ctx context.Context, source c.RoundProjectionSource, consume func(c.RoundProjectionSource) error) error {
	// Keep our own entry descriptors: even a callback replacing slice elements
	// cannot prevent cleanup of the original loans. It receives no mutable alias
	// to persistent store data or the private ownership bookkeeping.
	borrowed := source
	if source.WriteResult != nil {
		copy := *source.WriteResult
		borrowed.WriteResult = &copy
	}
	borrowed.Transcript = append([]c.RoundProjectionEntry(nil), source.Transcript...)
	defer clearRoundProjectionLoan(&source)
	defer clear(borrowed.Transcript)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := consume(borrowed); err != nil {
		return err
	}
	return ctx.Err()
}

func clearRoundProjectionLoan(source *c.RoundProjectionSource) {
	clear(source.Arguments)
	clear(source.ControlCanonical)
	for n := range source.Transcript {
		clear(source.Transcript[n].CanonicalMessage)
	}
	clear(source.Transcript)
	*source = c.RoundProjectionSource{}
}
