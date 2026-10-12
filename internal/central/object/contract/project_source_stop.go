package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ProjectSourceStopRequest identifies one original Artifact creation and its
// issued source handle. It carries no authority; both owners check their own
// facts under the current Project driver's private synchronous loan.
type ProjectSourceStopRequest struct {
	data func() ProjectSourceStopDetails
}

type ProjectSourceStopDetails struct {
	Actor                            i.Actor
	Cause                            ProjectStopCause
	Command                          f.CommandIdentity
	Lease                            SourceLease
	SourceProjectID, TargetProjectID i.ProjectID
	ObjectID                         ObjectID
}

func NewProjectSourceStopRequest(d ProjectSourceStopDetails) (ProjectSourceStopRequest, error) {
	if d.Command.Validate() != nil || d.Lease.Validate() != nil || d.SourceProjectID.Validate() != nil || d.TargetProjectID.Validate() != nil || d.ObjectID.Validate() != nil {
		return ProjectSourceStopRequest{}, bad()
	}
	owners := d.Command.OwnerIDs()
	if d.Command.Namespace() != "artifact" || d.Command.Command() != "create" || len(owners) != 1 || owners[0] != d.TargetProjectID.String() {
		return ProjectSourceStopRequest{}, bad()
	}
	if _, err := NewProjectStopRequest(ProjectStopRequestDetails{Actor: d.Actor, Cause: d.Cause, Component: ArtifactStopComponent, Step: RequestProjectStopStep}); err != nil {
		return ProjectSourceStopRequest{}, err
	}
	p := d.Cause.Details().ProjectID
	if p != d.SourceProjectID && p != d.TargetProjectID {
		return ProjectSourceStopRequest{}, bad()
	}
	return ProjectSourceStopRequest{func() ProjectSourceStopDetails { return d }}, nil
}
func (r ProjectSourceStopRequest) Validate() error {
	if r.data == nil {
		return bad()
	}
	return nil
}
func (r ProjectSourceStopRequest) Details() ProjectSourceStopDetails {
	if r.data == nil {
		return ProjectSourceStopDetails{}
	}
	return r.data()
}
func (r ProjectSourceStopRequest) Equal(other ProjectSourceStopRequest) bool {
	if r.Validate() != nil || other.Validate() != nil {
		return false
	}
	a, b := r.data(), other.data()
	return a.Actor.Equal(b.Actor) && a.Cause.Equal(b.Cause) && a.Command.Canonical() == b.Command.Canonical() &&
		a.Lease.ID() == b.Lease.ID() && a.Lease.data().issuer.data() == b.Lease.data().issuer.data() &&
		a.SourceProjectID == b.SourceProjectID && a.TargetProjectID == b.TargetProjectID && a.ObjectID == b.ObjectID
}
func (r ProjectSourceStopRequest) RequiredLocks() ([]f.LockRequest, error) {
	if r.Validate() != nil {
		return nil, bad()
	}
	d := r.data()
	a, err := f.ProjectLock(d.SourceProjectID.String())
	if err != nil {
		return nil, err
	}
	b, err := f.ProjectLock(d.TargetProjectID.String())
	if err != nil {
		return nil, err
	}
	if a.Canonical() == b.Canonical() {
		return []f.LockRequest{{Key: a, Mode: f.Exclusive}}, nil
	}
	if f.CompareLockKeys(a, b) > 0 {
		a, b = b, a
	}
	return []f.LockRequest{{Key: a, Mode: f.Exclusive}, {Key: b, Mode: f.Exclusive}}, nil
}
func (ProjectSourceStopRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_source_stop")
}
func (ProjectSourceStopRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"project_source_stop"`), nil
}
func (*ProjectSourceStopRequest) UnmarshalJSON([]byte) error { return bad() }
func (ProjectSourceStopRequest) LogValue() slog.Value        { return slog.StringValue("project_source_stop") }

// With owns the original authorization Store return and the complete callback
// return. The callback first checks the Artifact command in its own transaction
// and waits for that Store return, then synchronously joins the original Object
// handle. Every release, join and confirmation Tx must call Require again.
type ProjectSourceStopAuthority interface {
	WithProjectSourceStop(context.Context, ProjectSourceStopRequest, func(context.Context) error) error
	RequireProjectSourceStopInTx(context.Context, f.Tx, ProjectSourceStopRequest) error
}
