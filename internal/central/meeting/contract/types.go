// Package contract contains Meeting-owned durable identities and public views.
// Runtime references identify facts; they never grant Execution permissions.
package contract

import (
	"strings"
	"time"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type Meeting struct{}
type Participant struct{}
type Message struct{}
type Turn struct{}
type Contribution struct{}
type Decision struct{}
type Input struct{}
type Intent struct{}
type SummaryGeneration struct{}
type MeetingID = f.ID[Meeting]
type ParticipantID = f.ID[Participant]
type MessageID = f.ID[Message]
type TurnID = f.ID[Turn]
type ContributionID = f.ID[Contribution]
type DecisionID = f.ID[Decision]
type InputID = f.ID[Input]
type IntentID = f.ID[Intent]
type SummaryGenerationID = f.ID[SummaryGeneration]

const (
	MaxParticipants = 32
	MaxContentBytes = 128 << 10
	MaxNodes        = 1024
)

// InlineNode preserves source order. References are identities only, never
// automatically loaded external content. Draft agent mentions are accepted
// only by Start and converted to a server-created participant identity.
type InlineNode struct {
	Type          string     `json:"type"`
	Text          string     `json:"text,omitempty"`
	ParticipantID string     `json:"participant_id,omitempty"`
	AgentID       string     `json:"agent_id,omitempty"`
	Reference     *Reference `json:"reference,omitempty"`
}
type Reference struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}
type ParticipantView struct {
	ID          ParticipantID `json:"id"`
	MeetingID   MeetingID     `json:"meeting_id"`
	Kind        string        `json:"kind"`
	SourceID    string        `json:"source_id"`
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Order       int           `json:"order"`
	Active      bool          `json:"active"`
}
type RollingSummary struct {
	Version       f.Version `json:"version"`
	Goals         string    `json:"goals"`
	Decisions     string    `json:"decisions"`
	Unresolved    string    `json:"unresolved"`
	Facts         string    `json:"facts"`
	InputDigest   f.Digest  `json:"input_digest,omitempty"`
	PendingUpdate bool      `json:"pending_update"`
}
type Session struct {
	ID              MeetingID       `json:"id"`
	ProjectID       i.ProjectID     `json:"project_id"`
	Status          string          `json:"status"`
	Version         f.Version       `json:"version"`
	Title           *string         `json:"title"`
	ProposalContent *string         `json:"proposal_content,omitempty"`
	Summary         *RollingSummary `json:"summary"`
	Deleting        bool            `json:"deleting"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}
type MessageView struct {
	ID            MessageID     `json:"id"`
	ParticipantID ParticipantID `json:"participant_id"`
	Content       []InlineNode  `json:"content_nodes"`
	ReplyTo       *MessageID    `json:"reply_to_message_id,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
}
type ContributionView struct {
	ID              ContributionID        `json:"id"`
	TurnID          TurnID                `json:"turn_id"`
	ParticipantID   ParticipantID         `json:"participant_id"`
	Kind            string                `json:"kind"`
	Order           int                   `json:"order"`
	Status          string                `json:"status"`
	Generation      f.Version             `json:"generation"`
	Attempt         f.Version             `json:"attempt"`
	ExecutionID     *i.ExecutionID        `json:"execution_id"`
	WaitingOn       *i.ExecutionID        `json:"waiting_on_execution_id"`
	CancelRequested bool                  `json:"cancel_requested"`
	Message         *MessageView          `json:"message"`
	History         []ContributionHistory `json:"history"`
	StartedAt       *time.Time            `json:"started_at"`
	CompletedAt     *time.Time            `json:"completed_at"`
}

// ContributionHistory contains public immutable replies and execution lineage.
// It grants no permission to act on a historical Execution.
type ContributionHistory struct {
	ExecutionID  i.ExecutionID  `json:"execution_id"`
	Generation   f.Version      `json:"generation"`
	Attempt      f.Version      `json:"attempt"`
	Status       string         `json:"status"`
	RetryOf      *i.ExecutionID `json:"retry_of_execution_id"`
	RegenerateOf *i.ExecutionID `json:"regenerate_of_execution_id"`
	Message      *MessageView   `json:"message"`
}
type TurnView struct {
	ID            TurnID             `json:"id"`
	Mode          string             `json:"mode"`
	Status        string             `json:"status"`
	Result        string             `json:"result,omitempty"`
	Version       f.Version          `json:"version"`
	CreatedAt     time.Time          `json:"created_at"`
	Contributions []ContributionView `json:"contributions"`
	SummaryError  string             `json:"summary_error,omitempty"`
}
type Detail struct {
	Session      Session           `json:"session"`
	Participants []ParticipantView `json:"participants"`
	References   []Reference       `json:"references"`
	Turns        []TurnView        `json:"turns"`
	Decisions    []DecisionView    `json:"decisions"`
}
type DecisionView struct {
	ID             DecisionID     `json:"id"`
	ContributionID ContributionID `json:"contribution_id"`
	ExecutionID    i.ExecutionID  `json:"execution_id"`
	Question       string         `json:"question"`
	Options        []string       `json:"options"`
	Status         string         `json:"status"`
	Answer         *string        `json:"answer"`
	Version        f.Version      `json:"version"`
}
type StartRequest struct {
	ProjectID i.ProjectID   `json:"project_id"`
	Agents    []i.AgentID   `json:"agents"`
	Mode      string        `json:"mode"`
	Content   []InlineNode  `json:"content_nodes"`
	Meta      f.CommandMeta `json:"-"`
}
type SendRequest struct {
	ProjectID       i.ProjectID   `json:"project_id"`
	MeetingID       MeetingID     `json:"meeting_id"`
	ExpectedVersion f.Version     `json:"expected_version"`
	Mode            string        `json:"mode"`
	Content         []InlineNode  `json:"content_nodes"`
	Interrupt       bool          `json:"interrupt"`
	ReplyTo         *MessageID    `json:"reply_to_message_id,omitempty"`
	Meta            f.CommandMeta `json:"-"`
}
type Receipt struct {
	MeetingID MeetingID `json:"meeting_id"`
	TurnID    *TurnID   `json:"turn_id,omitempty"`
	Version   f.Version `json:"version"`
	Replayed  bool      `json:"replayed"`
}

func invalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func ValidText(v string, max int) bool {
	return len(v) <= max && utf8.ValidString(v) && !strings.ContainsRune(v, 0)
}
func ValidMode(v string) bool { return v == "sequential" || v == "parallel" }
func ValidateContent(nodes []InlineNode, draft bool) error {
	if len(nodes) == 0 || len(nodes) > MaxNodes {
		return invalid()
	}
	bytes, meaningful := 0, false
	for _, n := range nodes {
		bytes += len(n.Text)
		switch n.Type {
		case "text":
			if !ValidText(n.Text, MaxContentBytes) || n.ParticipantID != "" || n.AgentID != "" || n.Reference != nil {
				return invalid()
			}
			meaningful = meaningful || strings.TrimSpace(n.Text) != ""
		case "mention":
			if n.Text != "" || n.Reference != nil || (n.ParticipantID == "") == (n.AgentID == "") || n.AgentID != "" && !draft {
				return invalid()
			}
			id := n.ParticipantID
			if id == "" {
				id = n.AgentID
			}
			if _, err := f.ParseID[Participant](id); err != nil {
				return invalid()
			}
			meaningful = true
		case "task", "knowledge", "link", "file":
			if n.Reference == nil || n.Reference.Kind != n.Type || n.Reference.ID == "" || !ValidText(n.Reference.ID, 8192) || !ValidText(n.Reference.Label, 1024) || n.Text != "" || n.ParticipantID != "" || n.AgentID != "" {
				return invalid()
			}
			meaningful = true
		default:
			return invalid()
		}
	}
	if bytes > MaxContentBytes || !meaningful {
		return invalid()
	}
	return nil
}
func (r StartRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.Meta.Validate() != nil || r.Meta.ExpectedVersion != nil || !ValidMode(r.Mode) || len(r.Agents) == 0 || len(r.Agents) > MaxParticipants {
		return invalid()
	}
	seen := map[i.AgentID]bool{}
	for _, id := range r.Agents {
		if id.Validate() != nil || seen[id] {
			return invalid()
		}
		seen[id] = true
	}
	return ValidateContent(r.Content, true)
}
func (r SendRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.MeetingID.Validate() != nil || r.ExpectedVersion.Validate() != nil || r.Meta.Validate() != nil || r.Meta.ExpectedVersion != nil || !ValidMode(r.Mode) {
		return invalid()
	}
	if r.ReplyTo != nil && r.ReplyTo.Validate() != nil {
		return invalid()
	}
	return ValidateContent(r.Content, false)
}
func TerminalContribution(s string) bool {
	return s == "completed" || s == "failed" || s == "cancelled" || s == "skipped"
}
