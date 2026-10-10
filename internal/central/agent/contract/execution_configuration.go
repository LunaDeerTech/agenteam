package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ExecutionConfigurationStage string

const (
	ExecutionConfigurationLaunch  ExecutionConfigurationStage = "launch"
	ExecutionConfigurationCapture ExecutionConfigurationStage = "capture"
	ExecutionConfigurationCurrent ExecutionConfigurationStage = "current"
)

// ExecutionConfigurationRequest identifies a caller-owned transaction. It is
// neither a launch permit nor proof that an Execution is preparing or running.
type ExecutionConfigurationRequest struct {
	Actor       i.Actor
	ProjectID   i.ProjectID
	AgentID     i.AgentID
	ExecutionID i.ExecutionID
	Stage       ExecutionConfigurationStage
}

func (r ExecutionConfigurationRequest) Validate() error {
	if r.Actor.Validate() != nil || r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil {
		return invalid("", "INVALID_EXECUTION_CONFIGURATION_REQUEST")
	}
	d := r.Actor.Details()
	switch r.Stage {
	case ExecutionConfigurationLaunch:
		if d.Kind != i.Human && d.Kind != i.Service {
			return invalid("", "INVALID_EXECUTION_CONFIGURATION_ACTOR")
		}
	case ExecutionConfigurationCapture:
		if d.Kind != i.Human && d.Kind != i.Service && d.Kind != i.AgentRun {
			return invalid("", "INVALID_EXECUTION_CONFIGURATION_ACTOR")
		}
	case ExecutionConfigurationCurrent:
		if d.Kind != i.AgentRun {
			return invalid("", "INVALID_EXECUTION_CONFIGURATION_ACTOR")
		}
	default:
		return invalid("", "INVALID_EXECUTION_CONFIGURATION_STAGE")
	}
	if d.Kind == i.AgentRun && (d.ProjectID != r.ProjectID.String() || d.AgentID != r.AgentID.String() || d.ExecutionID != r.ExecutionID.String()) {
		return invalid("", "EXECUTION_CONFIGURATION_SCOPE_MISMATCH")
	}
	return nil
}

func (r ExecutionConfigurationRequest) Clone() ExecutionConfigurationRequest { return r }

// RequiredLocks is the minimum Agent-side subset. Execution and its Trigger
// provider contribute their full plans before the caller's first AcquireAll.
func (r ExecutionConfigurationRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	locks := make([]f.LockRequest, 0, 4)
	if r.Actor.Details().Kind == i.Human {
		key, _ := f.UserLock(r.Actor.Details().UserID)
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Shared})
	}
	project, _ := f.ProjectLock(r.ProjectID.String())
	agent, _ := f.AgentLock(r.AgentID.String())
	execution, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	return append(locks, f.LockRequest{Key: project, Mode: f.Shared}, f.LockRequest{Key: agent, Mode: f.Shared}, f.LockRequest{Key: execution, Mode: f.Exclusive})
}

func (ExecutionConfigurationRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_configuration_request")
}
func (ExecutionConfigurationRequest) LogValue() slog.Value {
	return slog.StringValue("execution_configuration_request")
}

// ExecutionIdentityAuthority is implemented by the actual Execution owner.
// Launch requires its private original-Tx witness after current Project and
// Trigger validation; no created row is required yet. Capture requires the
// original preparing row, no cancellation, and its private same-Tx witness
// after original Trigger input capture. An AgentRun capture does not depend on
// the expired Session of a previously authorized Human launch. Current requires
// its real running row, no cancellation, and sealed immutable Snapshot.
// All stages bind the full Actor/Project/Agent/Execution, original live same-Store
// Tx and complete held lock union. A DTO, constructor or public context flag is
// never proof. This callback must not recurse into the Agent configuration port.
type ExecutionIdentityAuthority interface {
	RequireExecutionConfigurationInTx(context.Context, f.Tx, ExecutionConfigurationRequest) error
}

// ExecutionConfiguration returns current initialized canonical configuration;
// the Execution owner separately persists its immutable preparation/Snapshot.
type ExecutionConfiguration interface {
	ReadExecutionConfigurationInTx(context.Context, f.Tx, ExecutionConfigurationRequest) (AgentConfig, error)
}
