package contract

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

var invalidUse = errors.New("INVALID_EXECUTION_MOUNT_USE")

// ExecutionMountUse binds the original nonempty preparation material to one
// sealed input and Snapshot. It grants no filesystem or Runner operation.
type ExecutionMountUse struct {
	Mounts         ExecutionMountSet `json:"mounts"`
	InputDigest    f.Digest          `json:"input_digest"`
	SnapshotDigest f.Digest          `json:"snapshot_digest"`
}

func (v ExecutionMountUse) Validate() error {
	if v.Mounts.Validate() != nil || len(v.Mounts.Mounts) == 0 || v.InputDigest.Validate() != nil || v.SnapshotDigest.Validate() != nil {
		return invalidUse
	}
	return nil
}
func (v ExecutionMountUse) Clone() ExecutionMountUse {
	v.Mounts = v.Mounts.Clone()
	return v
}
func (ExecutionMountUse) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("execution_mount_use")) }
func (ExecutionMountUse) LogValue() slog.Value       { return slog.StringValue("execution_mount_use") }

// Retirement identifies the actual terminal transition after the Execution
// owner has joined its runtime consumers. Cancellation alone is not proof.
type ExecutionMountRetirement struct {
	Version f.Version     `json:"version"`
	EventID event.EventID `json:"event_id"`
}

func (r ExecutionMountRetirement) Validate() error {
	if r.Version.Validate() != nil || r.EventID.Validate() != nil {
		return invalidUse
	}
	return nil
}

type ExecutionMountUseAuthority interface {
	ExecutionMountMaterialAuthority
	RequireExecutionMountUseAcquireInTx(context.Context, f.Tx, ExecutionMountUse) error
	RequireExecutionMountUseVerifyInTx(context.Context, f.Tx, ExecutionMountUse, *ExecutionMountRetirement) error
	RequireExecutionMountUseRetireInTx(context.Context, f.Tx, ExecutionMountUse, ExecutionMountRetirement) error
}

// All operations use the caller's original Store transaction and complete held
// lock union. Nil retirement means live; non-nil means that exact retirement.
// Replay never revives a retired use or changes its historical material.
type ExecutionMountUseProvider interface {
	ExecutionMountMaterialVerifier
	AcquireExecutionMountUseInTx(context.Context, f.Tx, ExecutionMountUse) error
	VerifyExecutionMountUseInTx(context.Context, f.Tx, ExecutionMountUse, *ExecutionMountRetirement) error
	RetireExecutionMountUseInTx(context.Context, f.Tx, ExecutionMountUse, ExecutionMountRetirement) error
}
