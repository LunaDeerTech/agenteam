package contract

import (
	"math"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type Deletion struct{}
type DeletionID = f.ID[Deletion]

// DeleteRequest is explicit Owner intent for one observed Meeting version.
// It neither deletes an Agent nor grants cancellation of unrelated work.
type DeleteRequest struct {
	ProjectID       i.ProjectID   `json:"project_id"`
	MeetingID       MeetingID     `json:"meeting_id"`
	ExpectedVersion f.Version     `json:"expected_version"`
	Meta            f.CommandMeta `json:"-"`
}

func (r DeleteRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.MeetingID.Validate() != nil || r.ExpectedVersion.Validate() != nil || r.ExpectedVersion == f.Version(math.MaxInt64) || r.Meta.Validate() != nil || r.Meta.ExpectedVersion != nil {
		return invalid()
	}
	return nil
}

type DeletionPhase string

const (
	DeletionCancelling DeletionPhase = "cancelling"
	DeletionCompleted  DeletionPhase = "deleted"
)

// A successful request can still be cancelling. Only deleted, with its
// committed completion time, means the original lifecycle gates and purge
// completed. Lookup observes this original intent and never resubmits it.
type DeleteReceipt struct {
	DeletionID      DeletionID    `json:"deletion_id"`
	MeetingID       MeetingID     `json:"meeting_id"`
	AcceptedVersion f.Version     `json:"accepted_version"`
	Phase           DeletionPhase `json:"phase"`
	ErrorCode       f.Code        `json:"error_code,omitempty"`
	RequestedAt     time.Time     `json:"requested_at"`
	DeletedAt       *time.Time    `json:"deleted_at,omitempty"`
	Replayed        bool          `json:"replayed,omitempty"`
}

func (r DeleteReceipt) Validate() error {
	if r.DeletionID.Validate() != nil || r.MeetingID.Validate() != nil || r.AcceptedVersion.Validate() != nil || r.RequestedAt.IsZero() || r.ErrorCode != "" && !r.ErrorCode.Known() {
		return invalid()
	}
	switch r.Phase {
	case DeletionCancelling:
		if r.DeletedAt != nil {
			return invalid()
		}
	case DeletionCompleted:
		if r.DeletedAt == nil || r.DeletedAt.Before(r.RequestedAt) || r.ErrorCode != "" {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}
