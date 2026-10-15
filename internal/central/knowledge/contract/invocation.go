package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type CallAttempt struct{}
type CallAttemptID = f.ID[CallAttempt]

// Invocation carries original trusted Tool provenance. Construction validates
// shape only; the domain revalidates the live execution and invocation witness
// inside each authorization transaction, including canonical Object reads.
type Invocation struct{ data func() InvocationDetails }

type InvocationDetails struct {
	Actor       id.Actor
	ProjectID   id.ProjectID
	ExecutionID string
	OperationID string
	ToolID      string
	ToolCallID  string
	AttemptID   CallAttemptID
}

func NewInvocation(d InvocationDetails) (Invocation, error) {
	a := d.Actor.Details()
	if d.Actor.Validate() != nil || a.Kind != id.AgentRun || d.ProjectID.Validate() != nil || a.ProjectID != d.ProjectID.String() || a.ExecutionID != d.ExecutionID || d.AttemptID.Validate() != nil {
		return Invocation{}, invalid()
	}
	for _, value := range []string{d.ExecutionID, d.OperationID, d.ToolID, d.ToolCallID} {
		if _, err := f.ParseID[struct{}](value); err != nil {
			return Invocation{}, invalid()
		}
	}
	return Invocation{data: func() InvocationDetails { return d }}, nil
}

func (v Invocation) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}

func (v Invocation) Details() InvocationDetails {
	if v.data == nil {
		return InvocationDetails{}
	}
	return v.data()
}

// KnowledgeInvocationAuthority is supplied by the original Tool authority.
// Discovery does not grant access. InTx methods use the complete caller-held
// lock union; they neither acquire missing locks nor start nested transactions.
type KnowledgeInvocationAuthority interface {
	Discover(context.Context, Invocation) (oc.AccessDependencies, error)
	DiscoverInTx(context.Context, f.Tx, Invocation) (oc.AccessDependencies, error)
	AuthorizeInTx(context.Context, f.Tx, Invocation, id.AccessIntent) error
}

// KnowledgeInvocationInputAuthority lends the original persisted Tool input
// only during the callback in the caller's authorization transaction. The
// domain compares its parsed request before reading any business data.
type KnowledgeInvocationInputAuthority interface {
	WithKnowledgeInputInTx(context.Context, f.Tx, Invocation, func(context.Context, []byte) error) error
}

func (Invocation) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "knowledge_invocation") }
func (Invocation) LogValue() slog.Value         { return slog.StringValue("knowledge_invocation") }
func (Invocation) MarshalJSON() ([]byte, error) { return []byte(`"knowledge_invocation"`), nil }
func (*Invocation) UnmarshalJSON([]byte) error  { return invalid() }
