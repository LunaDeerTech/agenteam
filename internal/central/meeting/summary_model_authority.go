package meeting

import (
	"context"
	"errors"
	"sync"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type SummaryAuthority struct {
	store    Store
	projects ec.PreparationProjectGate
	issuer   mc.PlanIssuer
	guard    *object.ProcessGuard
	process  oc.ProcessID
}

func NewSummaryAuthority(store Store, projects ec.PreparationProjectGate, guard *object.ProcessGuard, process oc.ProcessID) (*SummaryAuthority, error) {
	if nilPort(store) || nilPort(projects) || guard == nil || process.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &SummaryAuthority{store: store, projects: projects, issuer: mc.NewPlanIssuer(), guard: guard, process: process}, nil
}

type summaryOwnerKey struct{}
type summaryOwner struct {
	mu            sync.Mutex
	authority     *SummaryAuthority
	input         SummaryInput
	call          mc.CallID
	active        bool
	snapshot      *mc.SnapshotID
	lease         *sc.LeaseID
	attempt       *mc.AttemptIdentity
	sourceRequest *mc.ModelRequest
	sourceClosing bool
}
type summaryCallRecord struct {
	// Set only by the transaction which inserted this call in this process.
	fresh             bool
	attempt           int
	call              mc.CallID
	input             SummaryInput
	purpose           mc.Purpose
	digest            f.Digest
	messages, format  []byte
	status            string
	snapshot          *string
	unknown           bool
	deadline          time.Time
	returned, retired *time.Time
	terminal          *int64
	preparedProcess   *string
	resolveSource     []byte
	resolveBinding    *string
}

func (a *SummaryAuthority) owner(ctx context.Context, r mc.ConsumerRequest) (*summaryOwner, error) {
	if a == nil || ctx == nil || r.Validate() != nil || r.Consumer.Kind != mc.MeetingConsumer || r.CallID == nil {
		return nil, fault(f.InvalidArgument)
	}
	w, _ := ctx.Value(summaryOwnerKey{}).(*summaryOwner)
	d := r.Actor.Details()
	if w == nil || !w.active || w.authority != a || w.call != *r.CallID || w.input.GenerationID.String() != r.Consumer.OperationID || w.input.MeetingID.String() != r.Consumer.MeetingID || w.input.ProjectID != r.Consumer.ProjectID || d.Kind != i.Service || d.ProjectID != w.input.ProjectID.String() {
		return nil, fault(f.Forbidden)
	}
	technical := r.Action == mc.FinalizeConsumer || r.Action == mc.RetireConsumer && r.TerminalVersion == nil
	if technical {
		if d.ServiceName != i.ModelRuntime || r.TerminalVersion != nil {
			return nil, fault(f.Forbidden)
		}
		w.mu.Lock()
		invocation := ""
		if r.Attempt != nil {
			invocation = r.Attempt.InvocationID.String()
		} else if w.attempt != nil {
			invocation = w.attempt.InvocationID.String()
		}
		w.mu.Unlock()
		if invocation == "" || d.CauseRef != invocation {
			return nil, fault(f.Forbidden)
		}
	} else if d.ServiceName != i.MeetingRuntime || d.CauseRef != w.input.GenerationID.String() {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}

// Only the original domain call lends this witness. These physical attempt
// facts are intentionally not cold-recovery authority: a new process without
// the original handle keeps the call Unknown instead of manufacturing them.
func (w *summaryOwner) bindResolved(model mc.ResolvedModel) error {
	if model.Snapshot.ID.Validate() != nil || model.CredentialLease == nil || model.CredentialLease.LeaseID.Validate() != nil || model.LeaseOwner.Details().Kind != sc.ModelCallOwner || model.LeaseOwner.Details().ID != w.call.String() {
		return fault(f.Forbidden)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	snapshot, lease := model.Snapshot.ID, model.CredentialLease.LeaseID
	if w.snapshot != nil && *w.snapshot != snapshot || w.lease != nil && *w.lease != lease {
		return fault(f.ConfirmationStale)
	}
	w.snapshot, w.lease = &snapshot, &lease
	return nil
}

func (w *summaryOwner) requireCallFacts(r mc.ConsumerRequest) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.snapshot == nil || w.lease == nil || r.SnapshotID != *w.snapshot || r.LeaseID == nil || *r.LeaseID != *w.lease {
		return fault(f.Forbidden)
	}
	if r.Attempt != nil {
		if r.Attempt.CallID != w.call || r.Attempt.ProcessID != w.authority.process || r.Attempt.AttemptIndex != 1 || r.Attempt.Fence != 1 || w.attempt != nil && *w.attempt != *r.Attempt {
			return fault(f.Forbidden)
		}
		if w.attempt == nil {
			if r.Action != mc.ReadCredentialConsumer && r.Action != mc.FinalizeConsumer {
				return fault(f.Forbidden)
			}
			attempt := *r.Attempt
			w.attempt = &attempt
		}
	}
	if r.Action == mc.RetireConsumer && r.TerminalVersion == nil && (w.attempt == nil || r.Actor.Details().CauseRef != w.attempt.InvocationID.String()) {
		return fault(f.Forbidden)
	}
	return nil
}
func summaryLocks(p i.ProjectID, m c.MeetingID) []f.LockRequest {
	return []f.LockRequest{projectLock(p, f.Shared), meetingLock(m, f.Exclusive)}
}
func (a *SummaryAuthority) Discover(ctx context.Context, r mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	w, err := a.owner(ctx, r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	locks, err := normalize(summaryLocks(w.input.ProjectID, w.input.MeetingID))
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.ConsumerBinding(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	cause, err := readCause("meeting.summary-model.discover")
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	var mapping f.Digest
	var deadline f.Instant
	result := a.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := a.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		row, err := a.validate(ctx, tx, r, w)
		if err != nil {
			return err
		}
		mapping = row.digest
		deadline, err = f.NewInstant(row.deadline)
		return portError(err)
	})
	if err = commitError(result); err != nil {
		return mc.ConsumerDependencies{}, err
	}
	attempts := f.Sequence(1)
	policy := mc.RetryPolicy{Class: mc.BoundedRetry, Deadline: &deadline, MaxAttempts: &attempts, Categories: nil}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: mapping, Locks: locks, RetryPolicy: &policy})
}
func (a *SummaryAuthority) ValidateInTx(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	w, err := a.owner(ctx, r)
	if err != nil {
		return err
	}
	binding, err := mc.ConsumerBinding(r)
	if err != nil {
		return err
	}
	if err = a.store.RequireHeldLocks(ctx, tx, summaryLocks(w.input.ProjectID, w.input.MeetingID)); err != nil {
		return portError(err)
	}
	row, err := a.validate(ctx, tx, r, w)
	if err != nil {
		return err
	}
	if !plan.Matches(a.issuer, binding, row.digest) {
		return fault(f.ConfirmationStale)
	}
	return nil
}
func (a *SummaryAuthority) validate(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, w *summaryOwner) (summaryCallRecord, error) {
	x, err := a.store.InTx(tx)
	if err != nil {
		return summaryCallRecord{}, portError(err)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, summaryLocks(w.input.ProjectID, w.input.MeetingID)); err != nil {
		return summaryCallRecord{}, portError(err)
	}
	if err = a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return summaryCallRecord{}, portError(err)
	}
	row, err := loadSummaryCall(ctx, x, w.call)
	if err != nil {
		return row, err
	}
	if row.input.ProjectID != r.Consumer.ProjectID || row.input.MeetingID.String() != r.Consumer.MeetingID || row.input.GenerationID.String() != r.Consumer.OperationID || row.purpose != r.Consumer.Purpose {
		return row, fault(f.Forbidden)
	}
	if r.Input != nil && (r.Input.OperationID != r.Consumer.OperationID || r.Input.Digest != row.digest || r.Input.SchemaVersion != 1) {
		return row, fault(f.ConfirmationStale)
	}
	if r.Action != mc.ResolveConsumer && (row.snapshot == nil || *row.snapshot != r.SnapshotID.String()) {
		return row, fault(f.ConfirmationStale)
	}
	if r.Action == mc.ResolveConsumer && row.snapshot != nil && *row.snapshot != r.SnapshotID.String() {
		return row, fault(f.ConfirmationStale)
	}
	if r.Action == mc.ResolveConsumer {
		if err = validateSummaryResolveSource(ctx, x, r, row); err != nil {
			return row, err
		}
	} else if err = w.requireCallFacts(r); err != nil {
		return row, err
	}
	if r.Action == mc.RetireConsumer {
		if r.TerminalVersion != nil && (row.returned == nil || row.terminal == nil || int64(*r.TerminalVersion) != *row.terminal) {
			return row, fault(f.Forbidden)
		}
		return row, nil
	}
	if r.Action == mc.FinalizeConsumer {
		return row, nil
	}
	if row.returned != nil || row.retired != nil {
		return row, fault(f.InvalidState)
	}
	if summaryResolutionLookup(ctx, w, r) {
		return row, nil
	}
	if _, err = a.projects.RequirePreparingProjectInTx(ctx, tx, row.input.ProjectID); err != nil {
		return row, portError(err)
	}
	var state string
	var deleting, cancel bool
	err = x.QueryRow(ctx, `SELECT t.status,s.deleting,t.cancel_requested FROM agenteam_meeting.summary_generations g JOIN agenteam_meeting.turns t ON t.id=g.turn_id JOIN agenteam_meeting.sessions s ON s.id=g.meeting_id WHERE g.id=$1`, row.input.GenerationID.String()).Scan(&state, &deleting, &cancel)
	if err != nil {
		return row, portError(err)
	}
	if deleting || cancel || state != "finalizing" {
		return row, fault(f.InvalidState)
	}
	return row, nil
}
func loadSummaryCall(ctx context.Context, x postgres.SQLExecutor, id mc.CallID) (summaryCallRecord, error) {
	out := summaryCallRecord{call: id}
	var project, meeting, generation, purpose, inputDigest string
	err := x.QueryRow(ctx, `SELECT project_id,meeting_id,generation_id,purpose,input_digest,attempt,messages_data,response_format_data,status,snapshot_id,resolution_unknown,deadline_at,returned_at,terminal_version,retired_at,prepared_process_id,resolve_source_data,resolve_binding FROM agenteam_meeting.summary_model_calls WHERE call_id=$1`, id.String()).Scan(&project, &meeting, &generation, &purpose, &inputDigest, &out.attempt, &out.messages, &out.format, &out.status, &out.snapshot, &out.unknown, &out.deadline, &out.returned, &out.terminal, &out.retired, &out.preparedProcess, &out.resolveSource, &out.resolveBinding)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fault(f.NotFound)
	}
	if err != nil {
		return out, portError(err)
	}
	if out.input.ProjectID, err = f.ParseID[i.Project](project); err != nil {
		return out, fault(f.InternalError)
	}
	if out.input.MeetingID, err = f.ParseID[c.Meeting](meeting); err != nil {
		return out, fault(f.InternalError)
	}
	if out.input.GenerationID, err = f.ParseID[c.SummaryGeneration](generation); err != nil {
		return out, fault(f.InternalError)
	}
	out.purpose = mc.Purpose(purpose)
	out.digest = f.Digest(inputDigest)
	return out, nil
}

var _ mc.ConsumerAuthority = (*SummaryAuthority)(nil)
