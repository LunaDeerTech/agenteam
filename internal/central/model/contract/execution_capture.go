package contract

import (
	"context"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ExecutionModelCaptureRequest identifies the source, not authority. The real
// Execution owner retains its original preparing request/claim/process/fence.
type ExecutionModelCaptureRequest struct {
	ProjectID   id.ProjectID   `json:"project_id"`
	AgentID     id.AgentID     `json:"agent_id"`
	ExecutionID id.ExecutionID `json:"execution_id"`
}

func (r ExecutionModelCaptureRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil {
		return bad()
	}
	return nil
}

type ExecutionModelCaptureScope struct {
	Project        pc.ProjectRef
	AttemptBinding f.Digest
}

func (s ExecutionModelCaptureScope) Clone() ExecutionModelCaptureScope {
	s.Project = s.Project.Clone()
	return s
}

// Final facts come only after the same call's actual Agent capture. They must
// match the complete Model-owned reference pair and its owner version; the
// approval reference is checked for consistency, never resolved as Agent work.
type ExecutionModelCaptureFacts struct {
	Scope        ExecutionModelCaptureScope
	AgentVersion f.Version
	Selection    AgentModelSelection
}

func (v ExecutionModelCaptureFacts) Clone() ExecutionModelCaptureFacts {
	v.Scope = v.Scope.Clone()
	v.Selection = v.Selection.Clone()
	return v
}

type ExecutionModelCaptureAuthority interface {
	RequireModelCaptureDiscoveryInTx(context.Context, f.Tx, ExecutionModelCaptureRequest) (ExecutionModelCaptureScope, error)
	RequireModelCaptureInTx(context.Context, f.Tx, ExecutionModelCaptureRequest) (ExecutionModelCaptureFacts, error)
}

type ExecutionModelCapturePlan interface{ RequiredLocks() []f.LockRequest }

// Discovery reads actual Agent references under the preparing owner's private
// proof, then delegates to the existing Resolver. Its prepared Model intent
// can be durable; no Model snapshot, binding or Credential lease is committed
// there. Final delegates to ResolveModelInTx in the original complete capture
// Tx, after source/attempt and actual Agent facts are revalidated. The returned
// ResolvedModel is provisional until the entire outer capture commits. Missing
// other Snapshot providers must roll it and its lease back together.
//
// This slice resolves only AgentGeneration's direct primary Model using the
// existing supported profile. It grants no invocation/credential-read/retire
// authority and cannot enable tool-calling, reasoning or AgentRetry runtime.
type ExecutionModelCaptureProvider interface {
	DiscoverExecutionModel(context.Context, ExecutionModelCaptureRequest) (ExecutionModelCapturePlan, error)
	ResolveExecutionModelInTx(context.Context, f.Tx, ExecutionModelCaptureRequest, ExecutionModelCapturePlan) (ResolvedModel, error)
}

func (ExecutionModelCaptureRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("execution_model_capture_request"))
}
func (ExecutionModelCaptureRequest) LogValue() slog.Value {
	return slog.StringValue("execution_model_capture_request")
}
func (ExecutionModelCaptureScope) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("execution_model_capture_scope"))
}
func (ExecutionModelCaptureScope) LogValue() slog.Value {
	return slog.StringValue("execution_model_capture_scope")
}
func (ExecutionModelCaptureFacts) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("execution_model_capture_facts"))
}
func (ExecutionModelCaptureFacts) LogValue() slog.Value {
	return slog.StringValue("execution_model_capture_facts")
}
