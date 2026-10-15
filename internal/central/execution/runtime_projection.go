package execution

import (
	"context"
	"strconv"
	"unicode/utf8"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// ReadMeetingRuntimeProjectionInTx retains the existing reader's exact
// private source witness and full lock union. Only Execution-owned committed
// state/transcript is read; no Model or Tool private table is consulted.
func (r *MeetingExecutionReader) ReadMeetingRuntimeProjectionInTx(ctx context.Context, tx f.Tx, project i.ProjectID, association ec.AssociatedMeeting) (ec.RuntimeProjection, error) {
	result, err := r.ReadMeetingExecutionInTx(ctx, tx, project, association)
	if err != nil {
		return ec.RuntimeProjection{}, err
	}
	b, err := newRuntimeProjection(result)
	if err != nil {
		return ec.RuntimeProjection{}, err
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return ec.RuntimeProjection{}, portError(err)
	}
	// Input/control bodies are never materialized for this public reader. Their
	// durable sequence still participates in the truthful included-through value.
	rows, err := x.Query(ctx, `SELECT t.sequence,t.round_id::text,t.kind,CASE WHEN t.kind IN ('assistant','tool_result') THEN t.message ELSE NULL END,t.digest,r.snapshot_id::text FROM agenteam_execution.transcript_entries t LEFT JOIN agenteam_execution.rounds r ON r.execution_id=t.execution_id AND r.id=t.round_id WHERE t.execution_id=$1 ORDER BY t.sequence LIMIT $2`, association.ExecutionID.String(), ec.MaxRuntimeProjectionTranscriptEntries+1)
	if err != nil {
		return ec.RuntimeProjection{}, portError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var sequence int64
		var round, snapshot *string
		var kind, digest string
		var raw []byte
		if err := rows.Scan(&sequence, &round, &kind, &raw, &digest, &snapshot); err != nil {
			return ec.RuntimeProjection{}, portError(err)
		}
		err := b.accept(sequence, round, kind, raw, digest, snapshot)
		clear(raw)
		if err != nil {
			return ec.RuntimeProjection{}, err
		}
	}
	if err := rows.Err(); err != nil {
		return ec.RuntimeProjection{}, portError(err)
	}
	if err := ctx.Err(); err != nil {
		return ec.RuntimeProjection{}, err
	}
	return b.finish()
}

type runtimeProjectionBuilder struct {
	source      ec.MeetingExecutionResult
	view        ec.RuntimeProjection
	tools       map[string]int
	publicBytes int
}

func newRuntimeProjection(result ec.MeetingExecutionResult) (*runtimeProjectionBuilder, error) {
	s := result.Summary
	b := &runtimeProjectionBuilder{source: result, tools: make(map[string]int), view: ec.RuntimeProjection{Schema: ec.PublicRuntimeSchema, ExecutionID: s.ID, ProjectID: s.ProjectID, ExecutionVersion: s.Version, Status: s.Status, Items: []ec.RuntimeProjectionItem{{ID: "state", Seq: 1, Revision: s.Version, Type: "state", State: &ec.RuntimeStateData{Status: s.Status}}}}}
	if s.Status == ec.Waiting {
		if result.WaitingReason == nil || result.WaitingReference == nil {
			return nil, unavailable(nil)
		}
		b.view.Items = append(b.view.Items, ec.RuntimeProjectionItem{ID: "waiting/" + *result.WaitingReference, Seq: 2, Revision: s.Version, Type: "waiting", Waiting: &ec.RuntimeWaitingData{Reason: *result.WaitingReason, Reference: *result.WaitingReference}})
	} else if result.WaitingReason != nil || result.WaitingReference != nil {
		return nil, unavailable(nil)
	}
	if err := b.view.Validate(); err != nil {
		return nil, unavailable(err)
	}
	return b, nil
}

func (b *runtimeProjectionBuilder) accept(sequence int64, roundID *string, kind string, raw []byte, digest string, snapshotID *string) error {
	if sequence != int64(b.view.TranscriptThrough)+1 || sequence > ec.MaxRuntimeProjectionTranscriptEntries {
		return unavailable(nil)
	}
	b.view.TranscriptThrough = f.Progress(sequence)
	if kind == "input" || kind == "control" {
		if raw != nil {
			return unavailable(nil)
		}
		return nil
	}
	if kind != "assistant" && kind != "tool_result" || roundID == nil || snapshotID == nil || b.source.Summary.SnapshotID == nil || *snapshotID != b.source.Summary.SnapshotID.String() || len(raw) == 0 || ec.TriggerInputDigest(raw) != f.Digest(digest) {
		return unavailable(nil)
	}
	b.publicBytes += len(raw)
	if b.publicBytes > ec.MaxRuntimeViewBytes {
		return fault(f.PayloadTooLarge)
	}
	round, err := f.ParseID[ec.Round](*roundID)
	if err != nil {
		return unavailable(err)
	}
	var message mc.Message
	if decodeDirectTextJSON(raw, &message) != nil || message.Validate() != nil || (kind == "assistant" && message.Role != "assistant") || (kind == "tool_result" && message.Role != "tool") {
		return unavailable(nil)
	}
	if kind == "tool_result" {
		if len(message.Parts) != 1 || message.Parts[0].ToolResult == nil {
			return unavailable(nil)
		}
		result := message.Parts[0].ToolResult
		index, ok := b.tools[round.String()+"/"+result.CallID]
		if !ok {
			return unavailable(nil)
		}
		item := &b.view.Items[index]
		if item.Tool == nil || item.Tool.ResultAvailable {
			return unavailable(nil)
		}
		item.Revision = 2
		item.Tool.ResultAvailable, item.Tool.ResultSequence, item.Tool.IsError = true, f.Progress(sequence), result.IsError
		for _, part := range result.Parts {
			if part.Text != nil {
				item.Tool.ResultTextBytes += f.Progress(len(part.Text.Text))
				remaining := ec.MaxRuntimeToolSummaryBytes - len(item.Tool.ResultText)
				if remaining > 0 && !item.Tool.TextTruncated {
					text := part.Text.Text
					if len(text) > remaining {
						text = text[:remaining]
						for !utf8.ValidString(text) {
							text = text[:len(text)-1]
						}
					}
					item.Tool.ResultText += text
				}
				item.Tool.TextTruncated = item.Tool.ResultTextBytes > f.Progress(len(item.Tool.ResultText))
			} else if part.Image != nil {
				item.Tool.ImageParts++
			} else if part.File != nil {
				item.Tool.FileParts++
			} else {
				return unavailable(nil)
			}
		}
		return nil
	}
	for index, part := range message.Parts {
		item := ec.RuntimeProjectionItem{ID: "round/" + round.String() + "/" + strconv.Itoa(index), Seq: f.Sequence(sequence*257 + int64(index) + 3), Revision: 1, RoundID: round, TranscriptSequence: f.Progress(sequence)}
		switch {
		case part.Text != nil:
			item.Type, item.Text = "text", &ec.RuntimeTextData{Content: part.Text.Text}
		case part.ToolCall != nil:
			key := round.String() + "/" + part.ToolCall.ID
			if _, exists := b.tools[key]; exists {
				return unavailable(nil)
			}
			item.Type, item.Tool = "tool", &ec.RuntimeToolData{CallID: part.ToolCall.ID, Name: part.ToolCall.Name}
			b.tools[key] = len(b.view.Items)
		case part.Metadata != nil:
			continue // Provider-private metadata is never a public runtime item.
		default:
			return unavailable(nil)
		}
		if len(b.view.Items) >= ec.MaxRuntimeProjectionItems {
			return fault(f.PayloadTooLarge)
		}
		b.view.Items = append(b.view.Items, item)
	}
	return nil
}

func (b *runtimeProjectionBuilder) finish() (ec.RuntimeProjection, error) {
	if (b.source.Summary.SnapshotID == nil) != (b.view.TranscriptThrough == 0) {
		return ec.RuntimeProjection{}, unavailable(nil)
	}
	if err := b.view.Validate(); err != nil {
		return ec.RuntimeProjection{}, unavailable(err)
	}
	return b.view, nil
}

var _ ec.MeetingRuntimeProjectionReader = (*MeetingExecutionReader)(nil)
