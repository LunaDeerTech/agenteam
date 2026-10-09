package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const MaxAgentRefBytes = 16 << 10

// AgentRef is a projection of a current fact in one live transaction. A valid
// DTO is not proof that an Agent exists, is initialized, is idle or authorized.
// A serialized Ref must not be reused as a grant in another transaction.
type AgentRef struct {
	ProjectID     identity.ProjectID `json:"project_id"`
	AgentID       identity.AgentID   `json:"agent_id"`
	ConfigVersion foundation.Version `json:"config_version"`
}

func (v AgentRef) Validate() error {
	if v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil || v.ConfigVersion.Validate() != nil {
		return invalid("", "INVALID_AGENT_REF")
	}
	return nil
}
func (v AgentRef) Clone() AgentRef { return v }
func (v AgentRef) MarshalJSON() ([]byte, error) {
	type wire AgentRef
	return marshalChecked(wire(v), v.Validate(), MaxAgentRefBytes)
}
func (v *AgentRef) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire AgentRef
	w, err := decodeObject[wire](raw, MaxAgentRefBytes, []string{"project_id", "agent_id", "config_version"}, nil)
	if err != nil {
		return err
	}
	next := AgentRef(w)
	if err := next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}

// DecodeAgentRef bounds the complete supplied raw, including outer whitespace.
func DecodeAgentRef(raw []byte) (AgentRef, error) {
	var v AgentRef
	err := v.UnmarshalJSON(raw)
	return v, err
}
func (AgentRef) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_ref") }
func (AgentRef) LogValue() slog.Value       { return slog.StringValue("agent_ref") }

// WorkReferences supplies current Agent facts, never execution authority.
//
// After pure argument/context/Actor checks, an implementation must prove a live
// caller transaction from its Store, then require User SH, Project SH,
// ProjectSchedule EX and Agent SH locks before current Human Owner Read and its
// first Agent SQL. EX satisfies SH. It must not begin/commit a transaction, add
// locks, touch Activity, access networks or read Work-owned tables.
//
// Return only same-Project, initialized, active Agent facts. Missing/cross-Project
// Agents return NOT_FOUND; deleting returns INVALID_STATE/AGENT_NOT_CURRENT.
// Owner/Session errors precede Agent existence. Invalid Actor is UNAUTHENTICATED,
// AgentRun is DEPENDENCY_UNBOUND, Service is FORBIDDEN. Every failure returns a
// zero Ref. No default or fake implementation is provided here.
//
// Work mutation callers separately require Project Mutate, locking all old/new
// Agents before Task aggregate locks. Future Agent deletion takes Schedule EX
// and Agent EX and checks Work's current assignee references, including terminal
// Tasks, through Work's own port. Absence of Work references does not prove other
// domains have no references or executions; missing providers must fail.
type WorkReferences interface {
	RequireCurrentInTx(context.Context, foundation.Tx, identity.Actor,
		identity.ProjectID, identity.AgentID) (AgentRef, error)
}
