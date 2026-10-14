package contract

import (
	"encoding/json"
	"strconv"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// Before-start cancellation has no invented Snapshot, Round or Model Call.
// Its explicit null identities are a closed variant of the lifecycle payload.
type lifecycleWire struct {
	ExecutionID       i.ExecutionID      `json:"execution_id"`
	AgentID           i.AgentID          `json:"agent_id"`
	StartID           *DirectTextStartID `json:"start_id"`
	SnapshotID        *SnapshotID        `json:"snapshot_id"`
	RoundID           *RoundID           `json:"round_id"`
	CallID            *mc.CallID         `json:"call_id"`
	Status            Status             `json:"status"`
	Reason            string             `json:"reason"`
	TranscriptThrough string             `json:"transcript_through"`
}

func (v DirectTextLifecycle) MarshalJSON() ([]byte, error) {
	if v.Validate() != nil {
		return nil, invalid()
	}
	w := lifecycleWire{ExecutionID: v.ExecutionID, AgentID: v.AgentID, Status: v.Status, Reason: v.Reason, TranscriptThrough: strconv.FormatInt(int64(v.TranscriptThrough), 10)}
	if v.Reason != "cancelled_before_start" {
		w.StartID, w.SnapshotID, w.RoundID, w.CallID = &v.StartID, &v.SnapshotID, &v.RoundID, &v.CallID
	}
	return json.Marshal(w)
}

func (v *DirectTextLifecycle) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid()
	}
	var w lifecycleWire
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || directTextDecode(canonical, &w) != nil {
		return invalid()
	}
	through, err := strconv.ParseInt(w.TranscriptThrough, 10, 64)
	if err != nil || strconv.FormatInt(through, 10) != w.TranscriptThrough {
		return invalid()
	}
	out := DirectTextLifecycle{ExecutionID: w.ExecutionID, AgentID: w.AgentID, Status: w.Status, Reason: w.Reason, TranscriptThrough: f.Sequence(through)}
	if w.Reason == "cancelled_before_start" {
		if w.StartID != nil || w.SnapshotID != nil || w.RoundID != nil || w.CallID != nil {
			return invalid()
		}
	} else {
		if w.StartID == nil || w.SnapshotID == nil || w.RoundID == nil || w.CallID == nil {
			return invalid()
		}
		out.StartID, out.SnapshotID, out.RoundID, out.CallID = *w.StartID, *w.SnapshotID, *w.RoundID, *w.CallID
	}
	if out.Validate() != nil {
		return invalid()
	}
	*v = out
	return nil
}
