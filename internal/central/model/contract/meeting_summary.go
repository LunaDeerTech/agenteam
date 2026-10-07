package contract

import (
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MeetingSummarySelection is the independent system selection. A nil Model
// represents an initialized selection that has never been configured.
// Its shape does not establish current authorization or model availability.
type MeetingSummarySelection struct {
	ID      string    `json:"id"`
	Version f.Version `json:"version"`
	Model   *ModelID  `json:"model"`
}

func (s MeetingSummarySelection) Validate() error {
	if !uuid(s.ID) || s.Version.Validate() != nil || s.Model != nil && s.Model.Validate() != nil {
		return bad()
	}
	return nil
}

func (s MeetingSummarySelection) Clone() MeetingSummarySelection {
	s.Model = copyPtr(s.Model)
	return s
}

// UpdateMeetingSummarySelectionRequest always selects a model; it cannot clear
// the required selection. The service must verify current administrator access
// and the selected model's enabled System chat configuration in its transaction.
type UpdateMeetingSummarySelectionRequest struct {
	CommandMeta
	SelectionID     string    `json:"selection_id"`
	ExpectedVersion f.Version `json:"expected_version"`
	Model           ModelID   `json:"model"`
}

func (r UpdateMeetingSummarySelectionRequest) Validate() error {
	if r.CommandMeta.Validate() != nil || r.Scope.Details().Kind != id.System || !uuid(r.SelectionID) || r.ExpectedVersion.Validate() != nil || r.Model.Validate() != nil {
		return bad()
	}
	return nil
}

func (MeetingSummarySelection) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_meeting_summary_selection"))
}

func (MeetingSummarySelection) LogValue() slog.Value {
	return slog.StringValue("model_meeting_summary_selection")
}

func (UpdateMeetingSummarySelectionRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_update_meeting_summary_selection_request"))
}

func (UpdateMeetingSummarySelectionRequest) LogValue() slog.Value {
	return slog.StringValue("model_update_meeting_summary_selection_request")
}
