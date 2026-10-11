package contract

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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

const MaxExecutionMounts = 128

// Metadata is immutable preparation input, not permission to dispatch an
// operation or evidence that the logical directory physically exists.
type ExecutionMountMetadata struct {
	ID                id.MountID  `json:"id"`
	WorkspaceID       WorkspaceID `json:"workspace_id"`
	RunnerID          rc.RunnerID `json:"runner_id"`
	RunnerVersion     f.Version   `json:"runner_version"`
	RunnerName        string      `json:"runner_name"`
	RunnerDescription string      `json:"runner_description"`
	Name              string      `json:"name"`
	Description       *string     `json:"description"`
	Workspace         string      `json:"workspace"`
	Version           f.Version   `json:"version"`
}
type ExecutionMountSet struct {
	Request        ExecutionMountCaptureRequest `json:"request"`
	AgentVersion   f.Version                    `json:"agent_version"`
	Mounts         []ExecutionMountMetadata     `json:"mounts"`
	AttemptBinding f.Digest                     `json:"attempt_binding,omitempty"`
}

func (v ExecutionMountMetadata) Validate() error {
	if v.ID.Validate() != nil || v.WorkspaceID.Validate() != nil || v.RunnerID.Validate() != nil || v.Version.Validate() != nil ||
		(rc.ExecutionMetadata{ID: v.RunnerID, Name: v.RunnerName, Description: v.RunnerDescription, Version: v.RunnerVersion}).Validate() != nil ||
		len(v.Name) == 0 || len(v.Name) > 128 || !utf8.ValidString(v.Name) || strings.IndexFunc(v.Name, unicode.IsControl) >= 0 ||
		(v.Description != nil && (len(*v.Description) > 4096 || !utf8.ValidString(*v.Description) || strings.ContainsRune(*v.Description, 0))) || !ValidWorkspaceName(v.Workspace) {
		return invalidCapture
	}
	return nil
}

// The historical empty-set profile keeps its exact encoding and has no
// material binding. Nonempty captures bind the original preparing attempt.
func (v ExecutionMountSet) Validate() error {
	if v.Request.Validate() != nil || v.AgentVersion.Validate() != nil || v.Mounts == nil || len(v.Mounts) > MaxExecutionMounts {
		return invalidCapture
	}
	if len(v.Mounts) == 0 {
		if v.AttemptBinding != "" {
			return invalidCapture
		}
		return nil
	}
	if v.AttemptBinding.Validate() != nil {
		return invalidCapture
	}
	workspaces := make(map[WorkspaceID]struct{}, len(v.Mounts))
	tuples := make(map[struct {
		Runner    rc.RunnerID
		Workspace string
	}]struct{}, len(v.Mounts))
	for n, m := range v.Mounts {
		if m.Validate() != nil || n > 0 && v.Mounts[n-1].ID.String() >= m.ID.String() {
			return invalidCapture
		}
		if _, duplicate := workspaces[m.WorkspaceID]; duplicate {
			return invalidCapture
		}
		workspaces[m.WorkspaceID] = struct{}{}
		tuple := struct {
			Runner    rc.RunnerID
			Workspace string
		}{m.RunnerID, m.Workspace}
		if _, duplicate := tuples[tuple]; duplicate {
			return invalidCapture
		}
		tuples[tuple] = struct{}{}
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

// Material verification is a separate historical read. Its Execution owner
// must prove the exact expected input and original same-Tx preparing claim;
// neither this port nor a stored capture grants runtime operation authority.
type ExecutionMountMaterialVerifier interface {
	VerifyExecutionMountMaterialInTx(context.Context, f.Tx, ExecutionMountSet) error
}
type ExecutionMountMaterialAuthority interface {
	RequireMountMaterialInTx(context.Context, f.Tx, ExecutionMountSet) error
}

func (ExecutionMountSet) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("execution_mount_set")) }
func (ExecutionMountSet) LogValue() slog.Value       { return slog.StringValue("execution_mount_set") }
