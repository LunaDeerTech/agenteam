package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// CurrentProcessEnvironmentRequest selects a current command call, never a
// historical capture or caller assertion that its environment is empty.
type CurrentProcessEnvironmentRequest struct {
	ProjectID         i.ProjectID
	AgentID           i.AgentID
	ExecutionID       i.ExecutionID
	CallBindingDigest f.Digest
}

func (r CurrentProcessEnvironmentRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil || r.CallBindingDigest.Validate() != nil {
		return invalid("", "INVALID_CURRENT_ENVIRONMENT")
	}
	return nil
}
func (r CurrentProcessEnvironmentRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	return (EnvironmentCaptureRequest{ProjectID: r.ProjectID, AgentID: r.AgentID, ExecutionID: r.ExecutionID}).RequiredLocks()
}

// These facts are returned only by Execution's original, active private
// command witness after its real current Agent read in this exact transaction.
type CurrentProcessEnvironmentFacts struct {
	AgentVersion             f.Version
	AllowedSecretVariableIDs []i.ProjectVariableID
}

func (v CurrentProcessEnvironmentFacts) Validate() error {
	if v.AgentVersion.Validate() != nil || v.AllowedSecretVariableIDs == nil || len(v.AllowedSecretVariableIDs) > MaxSecretReferences {
		return invalid("", "INVALID_CURRENT_ENVIRONMENT_FACTS")
	}
	for n, id := range v.AllowedSecretVariableIDs {
		if id.Validate() != nil || n > 0 && v.AllowedSecretVariableIDs[n-1].String() >= id.String() {
			return invalid("", "INVALID_CURRENT_ENVIRONMENT_FACTS")
		}
	}
	return nil
}
func (v CurrentProcessEnvironmentFacts) Clone() CurrentProcessEnvironmentFacts {
	v.AllowedSecretVariableIDs = slices.Clone(v.AllowedSecretVariableIDs)
	return v
}

type CurrentProcessEnvironmentAuthority interface {
	RequireCurrentProcessEnvironmentInTx(context.Context, f.Tx, CurrentProcessEnvironmentRequest) (CurrentProcessEnvironmentFacts, error)
}

// This SQL-only participant owns no goroutine or transaction. Its caller must
// hold the complete union before entry and join the original call. No value,
// lease, decryption, or reusable "empty proof" is exposed.
type CurrentProcessEnvironment interface {
	RequireEmptyInTx(context.Context, f.Tx, CurrentProcessEnvironmentRequest) error
}

func (CurrentProcessEnvironmentRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "current_process_environment")
}
func (CurrentProcessEnvironmentRequest) LogValue() slog.Value {
	return slog.StringValue("current_process_environment")
}
func (CurrentProcessEnvironmentRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"current_process_environment"`), nil
}
func (CurrentProcessEnvironmentFacts) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "current_process_environment_facts")
}
func (CurrentProcessEnvironmentFacts) LogValue() slog.Value {
	return slog.StringValue("current_process_environment_facts")
}
func (CurrentProcessEnvironmentFacts) MarshalJSON() ([]byte, error) {
	return []byte(`"current_process_environment_facts"`), nil
}
