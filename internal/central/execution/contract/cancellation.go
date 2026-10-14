package contract

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// CancellationScope identifies canonical facts to re-read under the complete
// lock union. It is not a cancellation permit.
type CancellationScope struct {
	ProjectID   i.ProjectID
	TaskID      string
	ExecutionID i.ExecutionID
	AgentID     i.AgentID
}

func (s CancellationScope) Validate() error {
	if s.ProjectID.Validate() != nil || !uuid(s.TaskID) || s.ExecutionID.Validate() != nil || s.AgentID.Validate() != nil {
		return invalid()
	}
	return nil
}

type CancelRequest struct {
	ProjectID           i.ProjectID   `json:"project_id"`
	TaskID              string        `json:"task_id"`
	ExecutionID         i.ExecutionID `json:"execution_id"`
	ExpectedVersion     f.Version     `json:"expected_version"`
	ExpectedTaskVersion f.Version     `json:"expected_task_version"`
	Reason              *string       `json:"reason,omitempty"`
	Meta                f.CommandMeta `json:"-"`
}

func (r CancelRequest) Validate() error {
	if r.ProjectID.Validate() != nil || !uuid(r.TaskID) || r.ExecutionID.Validate() != nil || r.ExpectedVersion.Validate() != nil || r.ExpectedTaskVersion.Validate() != nil || r.Meta.Validate() != nil || r.Meta.ExpectedVersion != nil {
		return invalid()
	}
	if r.Reason != nil && (len(*r.Reason) == 0 || len(*r.Reason) > 1024 || !utf8.ValidString(*r.Reason) || strings.TrimSpace(*r.Reason) == "" || strings.ContainsRune(*r.Reason, 0)) {
		return invalid()
	}
	return nil
}
func (r CancelRequest) Clone() CancelRequest {
	if r.Reason != nil {
		v := *r.Reason
		r.Reason = &v
	}
	return r
}
func (r CancelRequest) Identity() (f.CommandIdentity, error) {
	if err := r.Validate(); err != nil {
		return f.CommandIdentity{}, err
	}
	return f.NewCommandIdentity("execution", []string{r.ProjectID.String(), r.TaskID, r.ExecutionID.String()}, "cancel", r.Meta.IdempotencyKey)
}
func (r CancelRequest) Digest(actor i.Actor) (f.Digest, error) {
	if r.Validate() != nil || actor.Validate() != nil || actor.Details().Kind != i.Human {
		return "", invalid()
	}
	raw, err := json.Marshal(struct {
		Domain  string
		User    string
		Request CancelRequest
	}{"execution.cancel.v1", actor.Details().UserID, r})
	if err != nil {
		return "", invalid()
	}
	return TriggerInputDigest(raw), nil
}

// Protection is the Work-owned accepted postimage, not a current Task read.
type TaskCancellationProtection struct {
	ID        string    `json:"id"`
	Version   f.Version `json:"version"`
	State     string    `json:"state"`
	BlockerID string    `json:"blocker_id"`
}

func (p TaskCancellationProtection) Validate() error {
	if !uuid(p.ID) || p.Version.Validate() != nil || p.State != "blocked" || !uuid(p.BlockerID) {
		return invalid()
	}
	return nil
}

type CancelReceipt struct {
	Execution TaskExecution              `json:"execution"`
	Task      TaskCancellationProtection `json:"task"`
}
type CancelResult struct {
	Receipt  CancelReceipt `json:"receipt"`
	Replayed bool          `json:"replayed"`
}
type CancelLookup struct {
	Found   bool           `json:"found"`
	Receipt *CancelReceipt `json:"receipt"`
}

// Only the original Work service recognizes its instance-bound prepared plan.
// Apply borrows the caller's transaction and must not begin or commit another.
type TaskCancellationPlan interface{ RequiredLocks() []f.LockRequest }
type TaskCancellationProvider interface {
	RequireStore(any) error
	PrepareExecutionCancellation(context.Context, i.Actor, f.CommandMeta, CancellationScope, f.Version, *string) (TaskCancellationPlan, error)
	ApplyExecutionCancellationInTx(context.Context, f.Tx, i.Actor, CancellationScope, TaskCancellationPlan) (TaskCancellationProtection, error)
	// AgentRun Task cancellation requires the actual Work transition's private
	// current-Tx witness; a public actor/scope does not establish that proof.
	RequireTaskCancellationInTx(context.Context, f.Tx, i.Actor, CancellationScope) error
}

type CancellationInTx interface {
	CancelLocks(i.Actor, CancellationScope) ([]f.LockRequest, error)
	RequestCancelInTx(context.Context, f.Tx, i.Actor, CancellationScope, *f.Version) (Summary, error)
	Notify(context.Context, CancellationScope) error
}

type TaskExecutionCancellation interface {
	Cancel(context.Context, i.Actor, CancelRequest) (CancelResult, error)
	LookupCancel(context.Context, i.Actor, CancelRequest) (CancelLookup, error)
}
