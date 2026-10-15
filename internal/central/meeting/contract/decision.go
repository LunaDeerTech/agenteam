package contract

import (
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// RespondDecisionRequest changes one pending decision. ExpectedVersion belongs
// to the decision, while the returned Receipt.Version belongs to the session.
// An answer never directly resumes or starts an Execution.
type RespondDecisionRequest struct {
	ProjectID       i.ProjectID   `json:"project_id"`
	MeetingID       MeetingID     `json:"meeting_id"`
	DecisionID      DecisionID    `json:"decision_id"`
	Action          string        `json:"action"`
	ExpectedVersion f.Version     `json:"expected_version"`
	Answer          *string       `json:"answer,omitempty"`
	Meta            f.CommandMeta `json:"-"`
}

func (r RespondDecisionRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.MeetingID.Validate() != nil || r.DecisionID.Validate() != nil || r.ExpectedVersion.Validate() != nil || r.Meta.Validate() != nil || r.Meta.ExpectedVersion != nil {
		return invalid()
	}
	switch r.Action {
	case "answer":
		if r.Answer == nil || !ValidText(*r.Answer, 32768) || strings.TrimSpace(*r.Answer) == "" {
			return invalid()
		}
	case "skip":
		if r.Answer != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}

type DecisionInput struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

func (v DecisionInput) Validate() error {
	if !ValidText(v.Question, 8192) || strings.TrimSpace(v.Question) == "" || len(v.Options) > 32 {
		return invalid()
	}
	seen := make(map[string]bool, len(v.Options))
	for _, option := range v.Options {
		if !ValidText(option, 1024) || strings.TrimSpace(option) == "" || seen[option] {
			return invalid()
		}
		seen[option] = true
	}
	return nil
}

// DecisionResult is the resolved input returned to the same original Tool
// operation. The immutable Tool result, not this value alone, authorizes the
// subsequent Execution transcript checkpoint.
type DecisionResult struct {
	DecisionID DecisionID `json:"decision_id"`
	Version    f.Version  `json:"version"`
	Status     string     `json:"status"`
	Answer     *string    `json:"answer,omitempty"`
}

func (v DecisionResult) Validate() error {
	if v.DecisionID.Validate() != nil || v.Version.Validate() != nil {
		return invalid()
	}
	if v.Status == "answered" && v.Answer != nil && ValidText(*v.Answer, 32768) && strings.TrimSpace(*v.Answer) != "" || v.Status == "skipped" && v.Answer == nil {
		return nil
	}
	return invalid()
}
