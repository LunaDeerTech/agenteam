package contract

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

var invalidCapture = errors.New("INVALID_EXECUTION_MOUNT_CAPTURE")

type ExecutionMountCaptureRequest struct {
	ProjectID   id.ProjectID   `json:"project_id"`
	AgentID     id.AgentID     `json:"agent_id"`
	ExecutionID id.ExecutionID `json:"execution_id"`
}

func (r ExecutionMountCaptureRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil {
		return invalidCapture
	}
	return nil
}

type ExecutionMountCaptureScope struct {
	Project        pc.ProjectRef
	AttemptBinding f.Digest
}
type ExecutionMountCaptureFacts struct {
	Scope           ExecutionMountCaptureScope
	AgentVersion    f.Version
	AllowedMountIDs []id.MountID
}
type ExecutionMountCaptureAuthority interface {
	RequireMountCaptureDiscoveryInTx(context.Context, f.Tx, ExecutionMountCaptureRequest) (ExecutionMountCaptureScope, error)
	RequireMountCaptureInTx(context.Context, f.Tx, ExecutionMountCaptureRequest) (ExecutionMountCaptureFacts, error)
}

// Metadata has no secret/physical host path. This slice cannot issue a nonempty
// set until real runtime reference protection and environment providers exist.
type ExecutionMountMetadata struct {
	ID          id.MountID  `json:"id"`
	RunnerID    rc.RunnerID `json:"runner_id"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	Workspace   string      `json:"workspace"`
	Version     f.Version   `json:"version"`
}
type ExecutionMountSet struct {
	Request      ExecutionMountCaptureRequest `json:"request"`
	AgentVersion f.Version                    `json:"agent_version"`
	Mounts       []ExecutionMountMetadata     `json:"mounts"`
}

// Validate intentionally accepts only the implemented empty-set profile. An
// empty set still requires the actual initialized head and version in storage.
func (v ExecutionMountSet) Validate() error {
	if v.Request.Validate() != nil || v.AgentVersion.Validate() != nil || v.Mounts == nil || len(v.Mounts) != 0 {
		return invalidCapture
	}
	return nil
}
func (v ExecutionMountSet) Clone() ExecutionMountSet {
	v.Mounts = slices.Clone(v.Mounts)
	for n := range v.Mounts {
		if v.Mounts[n].Description != nil {
			s := *v.Mounts[n].Description
			v.Mounts[n].Description = &s
		}
	}
	return v
}

type ExecutionMountCapturePlan interface{ RequiredLocks() []f.LockRequest }
type ExecutionMountCaptureProvider interface {
	DiscoverExecutionMounts(context.Context, ExecutionMountCaptureRequest) (ExecutionMountCapturePlan, error)
	CaptureExecutionMountsInTx(context.Context, f.Tx, ExecutionMountCaptureRequest, ExecutionMountCapturePlan) (ExecutionMountSet, error)
}

func (ExecutionMountSet) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("execution_mount_set")) }
func (ExecutionMountSet) LogValue() slog.Value       { return slog.StringValue("execution_mount_set") }
