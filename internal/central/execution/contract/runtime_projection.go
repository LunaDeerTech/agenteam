package contract

import (
	"context"
	"strconv"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const PublicRuntimeSchema = "execution_public_runtime_v1"
const MaxRuntimeProjectionItems = 1024
const MaxRuntimeToolSummaryBytes = 8 << 10
const MaxRuntimeProjectionTranscriptEntries = 4096

// RuntimeProjection is rebuilt from canonical Execution state and committed
// transcript entries. It includes no uncommitted Model frames or token deltas.
// TranscriptThrough is the actual durable transcript sequence read under the
// original Execution lock; it is not a Gateway notification sequence.
type RuntimeProjection struct {
	Schema            string                  `json:"schema"`
	ExecutionID       i.ExecutionID           `json:"execution_id"`
	ProjectID         i.ProjectID             `json:"project_id"`
	ExecutionVersion  f.Version               `json:"execution_version"`
	Status            Status                  `json:"status"`
	TranscriptThrough f.Progress              `json:"transcript_through"`
	Items             []RuntimeProjectionItem `json:"items"`
}

type RuntimeProjectionItem struct {
	ID                 string              `json:"id"`
	Seq                f.Sequence          `json:"seq"`
	Revision           f.Version           `json:"revision"`
	Type               string              `json:"type"`
	RoundID            RoundID             `json:"round_id,omitzero"`
	TranscriptSequence f.Progress          `json:"transcript_sequence"`
	State              *RuntimeStateData   `json:"state,omitempty"`
	Waiting            *RuntimeWaitingData `json:"waiting,omitempty"`
	Tool               *RuntimeToolData    `json:"tool,omitempty"`
	Text               *RuntimeTextData    `json:"text,omitempty"`
}

type RuntimeStateData struct {
	Status Status `json:"status"`
}
type RuntimeWaitingData struct {
	Reason    string `json:"reason"`
	Reference string `json:"reference"`
}
type RuntimeToolData struct {
	CallID          string     `json:"call_id"`
	Name            string     `json:"name"`
	ResultAvailable bool       `json:"result_available"`
	ResultSequence  f.Progress `json:"result_sequence"`
	IsError         bool       `json:"is_error"`
	ResultText      string     `json:"result_text"`
	ResultTextBytes f.Progress `json:"result_text_bytes"`
	TextTruncated   bool       `json:"text_truncated"`
	ImageParts      uint32     `json:"image_parts"`
	FileParts       uint32     `json:"file_parts"`
}

// Meeting's exact current association and private original witness are still
// mandatory. A raw Execution ID, public topic, or past subscription grants no
// access to this reader. It does not replace the terminal-only RuntimeReader.
type MeetingRuntimeProjectionReader interface {
	ReadMeetingRuntimeProjectionInTx(context.Context, f.Tx, i.ProjectID, AssociatedMeeting) (RuntimeProjection, error)
}

func (p RuntimeProjection) Validate() error {
	if p.Schema != PublicRuntimeSchema || p.ExecutionID.Validate() != nil || p.ProjectID.Validate() != nil || p.ExecutionVersion.Validate() != nil || !p.Status.Valid() || p.TranscriptThrough.Validate() != nil || p.TranscriptThrough > MaxRuntimeProjectionTranscriptEntries || len(p.Items) == 0 || len(p.Items) > MaxRuntimeProjectionItems {
		return invalid()
	}
	ids := make(map[string]bool, len(p.Items))
	previous := f.Sequence(0)
	waiting, textBytes := 0, 0
	for index, item := range p.Items {
		if len(item.ID) == 0 || len(item.ID) > 512 || !utf8.ValidString(item.ID) || ids[item.ID] || item.Seq.Validate() != nil || item.Seq <= previous || item.Revision.Validate() != nil || item.TranscriptSequence.Validate() != nil || item.TranscriptSequence > p.TranscriptThrough {
			return invalid()
		}
		ids[item.ID], previous = true, item.Seq
		parts := 0
		for _, present := range []bool{item.State != nil, item.Waiting != nil, item.Tool != nil, item.Text != nil} {
			if present {
				parts++
			}
		}
		if parts != 1 {
			return invalid()
		}
		switch item.Type {
		case "state":
			if index != 0 || item.ID != "state" || item.Seq != 1 || item.Revision != p.ExecutionVersion || item.TranscriptSequence != 0 || item.RoundID != (RoundID{}) || item.State == nil || item.State.Status != p.Status {
				return invalid()
			}
		case "waiting":
			if index != 1 || item.Seq != 2 || item.Revision != p.ExecutionVersion || p.Status != Waiting || item.RoundID != (RoundID{}) || item.TranscriptSequence != 0 || item.Waiting == nil || (item.Waiting.Reason != "approval" && item.Waiting.Reason != "decision") || !uuid(item.Waiting.Reference) || item.ID != "waiting/"+item.Waiting.Reference {
				return invalid()
			}
			waiting++
		case "text":
			if index == 0 || !item.originalPart() || item.Revision != 1 || item.Text == nil || !utf8.ValidString(item.Text.Content) {
				return invalid()
			}
			textBytes += len(item.Text.Content)
		case "tool":
			if index == 0 || !item.originalPart() || item.Tool == nil || item.Tool.validate() != nil || item.Revision != 1 && item.Revision != 2 || (item.Revision == 2) != item.Tool.ResultAvailable || item.Tool.ResultSequence > p.TranscriptThrough || item.Tool.ResultAvailable && item.Tool.ResultSequence <= item.TranscriptSequence {
				return invalid()
			}
			textBytes += len(item.Tool.ResultText)
		default:
			return invalid()
		}
		if textBytes > MaxRuntimeTextBytes {
			return invalid()
		}
	}
	if (p.Status == Waiting && waiting != 1) || (p.Status != Waiting && waiting != 0) || p.Items[0].Type != "state" {
		return invalid()
	}
	return nil
}

// Each published part retains its original round/part identity. The sparse
// sequence is derived from the durable transcript sequence and the original
// bounded Message part index, so omitted private metadata creates no fiction
// of contiguous public progress.
func (item RuntimeProjectionItem) originalPart() bool {
	if item.RoundID.Validate() != nil || item.TranscriptSequence == 0 || item.TranscriptSequence > MaxRuntimeProjectionTranscriptEntries {
		return false
	}
	prefix := "round/" + item.RoundID.String() + "/"
	if !strings.HasPrefix(item.ID, prefix) {
		return false
	}
	raw := strings.TrimPrefix(item.ID, prefix)
	part, err := strconv.Atoi(raw)
	return err == nil && part >= 0 && part < 256 && strconv.Itoa(part) == raw && item.Seq == f.Sequence(uint64(item.TranscriptSequence)*257+uint64(part)+3)
}

func (t RuntimeToolData) validate() error {
	if strings.TrimSpace(t.CallID) == "" || len(t.CallID) > 256 || strings.TrimSpace(t.Name) == "" || len(t.Name) > 256 || !utf8.ValidString(t.CallID) || !utf8.ValidString(t.Name) || !utf8.ValidString(t.ResultText) || len(t.ResultText) > MaxRuntimeToolSummaryBytes || t.ResultTextBytes.Validate() != nil || t.ResultSequence.Validate() != nil || t.ResultTextBytes < f.Progress(len(t.ResultText)) || t.TextTruncated != (t.ResultTextBytes > f.Progress(len(t.ResultText))) || uint64(t.ImageParts)+uint64(t.FileParts) > 256 {
		return invalid()
	}
	if !t.ResultAvailable && (t.ResultSequence != 0 || t.IsError || t.ResultText != "" || t.ResultTextBytes != 0 || t.TextTruncated || t.ImageParts != 0 || t.FileParts != 0) {
		return invalid()
	}
	return nil
}
