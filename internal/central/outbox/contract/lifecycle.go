package contract

import (
	"context"
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"io"
	"log/slog"
)

type LifecycleOperation struct{}
type LifecycleAction string

const (
	ArchiveProject LifecycleAction = "archive"
	DeleteProject  LifecycleAction = "delete"
)

type LifecycleDetails struct {
	ProjectID      identity.ProjectID
	OperationID    foundation.ID[LifecycleOperation]
	Action         LifecycleAction
	ProjectVersion foundation.Version
}
type LifecycleCause struct{ data func() LifecycleDetails }

func NewLifecycleCause(d LifecycleDetails) (LifecycleCause, error) {
	if d.ProjectID.Validate() != nil || d.OperationID.Validate() != nil || (d.Action != ArchiveProject && d.Action != DeleteProject) || d.ProjectVersion.Validate() != nil {
		return LifecycleCause{}, invalid()
	}
	return LifecycleCause{func() LifecycleDetails { return d }}, nil
}
func (c LifecycleCause) Validate() error {
	if c.data == nil {
		return invalid()
	}
	return nil
}
func (c LifecycleCause) Details() LifecycleDetails {
	if c.data == nil {
		return LifecycleDetails{}
	}
	return c.data()
}
func (c LifecycleCause) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "outbox_lifecycle_cause")
}
func (c LifecycleCause) MarshalJSON() ([]byte, error) { return []byte(`"outbox_lifecycle_cause"`), nil }
func (*LifecycleCause) UnmarshalJSON([]byte) error    { return invalid() }
func (c LifecycleCause) LogValue() slog.Value         { return slog.StringValue("outbox_lifecycle_cause") }

type StopReport struct {
	Stopped bool
	Pending foundation.Progress
}
type CleanupReport struct {
	Completed          bool
	Removed, Remaining foundation.Progress
}
type ProjectLifecycleParticipant interface {
	Name() string
	RequestStop(context.Context, identity.Actor, LifecycleCause) (StopReport, error)
	InspectStop(context.Context, identity.Actor, LifecycleCause) (StopReport, error)
	Cleanup(context.Context, identity.Actor, LifecycleCause) (CleanupReport, error)
}
