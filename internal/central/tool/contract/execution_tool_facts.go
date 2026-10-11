package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Execution supplies current Agent facts and its complete immutable Tool
// capture. This is a metadata consistency request, never invocation authority.
// AgentVersion/AllowedToolIDs are current; Tools are the original capture.
type ExecutionToolFactsRequest struct {
	Capture        ExecutionToolCaptureRequest
	AgentVersion   f.Version
	AllowedToolIDs []id.ToolID
	Tools          []ExecutionTool
}

func (r ExecutionToolFactsRequest) Validate() error {
	if r.Capture.Validate() != nil || r.AgentVersion.Validate() != nil || r.AllowedToolIDs == nil || r.Tools == nil || len(r.AllowedToolIDs) > 128 || len(r.Tools) > 128 {
		return operationInvalid()
	}
	for n, v := range r.AllowedToolIDs {
		if v.Validate() != nil || n > 0 && r.AllowedToolIDs[n-1].String() >= v.String() {
			return operationInvalid()
		}
	}
	for n, v := range r.Tools {
		if v.Validate() != nil || n > 0 && r.Tools[n-1].ToolID.String() >= v.ToolID.String() {
			return operationInvalid()
		}
	}
	return nil
}

func (r ExecutionToolFactsRequest) Clone() ExecutionToolFactsRequest {
	r.AllowedToolIDs = slices.Clone(r.AllowedToolIDs)
	r.Tools = slices.Clone(r.Tools)
	return r
}

// RequiredLocks includes this provider's entire minimum union. The caller adds
// its own source/Execution authorization locks and acquires all before InTx.
func (r ExecutionToolFactsRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	registry, _ := f.SystemConfigLock("tool-registry")
	locks := append(r.Capture.RequiredLocks(), f.LockRequest{Key: registry, Mode: f.Shared})
	for _, v := range r.Tools {
		key, _ := f.AggregateLock(f.ToolSpecAggregate, v.ToolID.String())
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Shared})
	}
	slices.SortFunc(locks, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	return locks
}

type ExecutionToolFacts interface {
	RequireExecutionToolFactsInTx(context.Context, f.Tx, ExecutionToolFactsRequest) error
}

func (ExecutionToolFactsRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_tool_facts_request")
}
func (ExecutionToolFactsRequest) LogValue() slog.Value {
	return slog.StringValue("execution_tool_facts_request")
}
func (ExecutionToolFactsRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_tool_facts_request"`), nil
}
