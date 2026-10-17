package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type SummaryResolver interface {
	mc.Resolver
	mc.ResolutionLookup
}
type SummaryModelDependencies struct {
	Authority  *SummaryAuthority
	Resolver   SummaryResolver
	Calls      mc.JSONCaller
	Retirement mc.ModelCallRetirement
	Stopped    mc.StoppedCallObserver
	Retention  mc.ServingSnapshotRetention
	Actor      i.ServiceRegistration
}
type SummaryModel struct {
	store       Store
	deps        SummaryModelDependencies
	mu          sync.Mutex
	stopped     bool
	calls       map[mc.CallID]*summaryModelCall
	transitions map[c.SummaryGenerationID]*summaryTransition
}
type summaryModelCall struct {
	mu                       sync.Mutex
	owner                    *summaryOwner
	cancel                   context.CancelFunc
	handle                   mc.JSONCall
	resolved                 *mc.ResolvedModel
	actor                    i.Actor
	done                     bool
	failure                  error
	candidate                *SummaryCandidate
	resolutionUnknown        bool
	returnedUnknown          bool
	retirementUnknown        bool
	dispatchUnknown          bool
	stoppedResolutionChecked bool
	stoppedSnapshotSettled   bool
	retirementPlan           mc.ModelCallRetirementPlan
	retirementRequest        *mc.ModelCallRetirementRequest
	resolve                  *mc.ResolveRequest
	pin                      *summaryPin
}

func NewSummaryModel(store Store, deps SummaryModelDependencies) (*SummaryModel, error) {
	if nilPort(store) || deps.Authority == nil || !sameStore(deps.Authority.store, store) || nilPort(deps.Resolver) || nilPort(deps.Calls) || nilPort(deps.Retirement) || nilPort(deps.Retention) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SummaryModel{store: store, deps: deps, calls: map[mc.CallID]*summaryModelCall{}, transitions: map[c.SummaryGenerationID]*summaryTransition{}}, nil
}

func summaryMessages(input SummaryInput) ([]mc.Message, mc.ResponseFormat, error) {
	// Only current immutable message content is supplied to the summary model.
	// Previous summaries, Turn/Decision/Approval objects are never inputs.
	type entry struct {
		Author  string `json:"author_participant_id"`
		Content any    `json:"content_nodes"`
	}
	messages := make([]entry, len(input.Messages))
	for n, m := range input.Messages {
		messages[n] = entry{m.Message.ParticipantID.String(), m.Message.Content}
	}
	raw, err := canonical(messages)
	if err != nil {
		return nil, mc.ResponseFormat{}, err
	}
	instruction := "Summarize the complete current meeting messages. Return exactly one JSON object with goals, decisions, unresolved and facts as four string fields. Use empty strings for absent facts. Do not add other fields, markdown or surrounding text. Preserve disagreement and uncertainty; never treat a conversational claim as a real approval or completed tool action. Do not invent facts."
	if input.Initial {
		instruction += " Also produce a concise title string for this meeting."
	}
	text := func(role, value string) mc.Message {
		return mc.Message{Role: role, Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: value}}}}
	}
	// D24's JSON envelope is parsed by Meeting; it does not require structured
	// output capability from the independently selected System chat model.
	attachments, err := frozenAttachments(input.ProjectID, input.Messages)
	if err != nil {
		return nil, mc.ResponseFormat{}, err
	}
	user := text("user", string(raw))
	for _, attachment := range attachments {
		part, err := attachment.MessagePart()
		if err != nil {
			return nil, mc.ResponseFormat{}, err
		}
		user.Parts = append(user.Parts, mc.MessagePart{Text: &mc.TextPart{Text: "Attached file: " + attachment.Filename}}, part)
	}
	return []mc.Message{text("system", instruction), user}, mc.ResponseFormat{Kind: "text"}, nil
}
func (m *SummaryModel) tx(ctx context.Context, input SummaryInput, action string, extra []f.LockRequest, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	locks, err := normalize(append(summaryLocks(input.ProjectID, input.MeetingID), extra...))
	if err != nil {
		return err
	}
	if owner := originalDeletionRuntime(ctx, input.ProjectID, input.MeetingID); owner != nil {
		if !sameStore(owner.runtime.store, m.store) {
			return fault(f.Forbidden)
		}
		locks, err = owner.runtime.runtimeLocks(ctx, input.ProjectID, input.MeetingID, locks)
	}
	if err != nil {
		return err
	}
	cause, err := readCause("meeting.summary-model." + action)
	if err != nil {
		return err
	}
	return commitError(m.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := m.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := m.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, tx, x)
	}))
}
func (m *SummaryModel) prepare(ctx context.Context, input SummaryInput) (summaryCallRecord, error) {
	var out summaryCallRecord
	messages, format, err := summaryMessages(input)
	if err != nil {
		return out, err
	}
	inputDigest, err := model.StructuredInputDigest(messages, format)
	if err != nil {
		return out, err
	}
	messageRaw, err := mc.EncodeCanonicalMessages(messages)
	if err != nil {
		return out, err
	}
	formatRaw, err := json.Marshal(format)
	if err != nil {
		return out, err
	}
	err = m.tx(ctx, input, "prepare", nil, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if _, err := m.deps.Authority.projects.RequirePreparingProjectInTx(ctx, tx, input.ProjectID); err != nil {
			return portError(err)
		}
		var state, storedDigest string
		var attempt int
		var cancel, deleting bool
		err := x.QueryRow(ctx, `SELECT g.status,g.input_digest,g.attempt,t.cancel_requested,s.deleting FROM agenteam_meeting.summary_generations g JOIN agenteam_meeting.turns t ON t.id=g.turn_id JOIN agenteam_meeting.sessions s ON s.id=g.meeting_id WHERE g.id=$1 AND g.meeting_id=$2 AND t.id=$3 AND t.status='finalizing'`, input.GenerationID.String(), input.MeetingID.String(), input.TurnID.String()).Scan(&state, &storedDigest, &attempt, &cancel, &deleting)
		if err != nil {
			return portError(err)
		}
		if state != "generating" || storedDigest != input.InputDigest.String() || attempt < 1 || attempt > 3 || cancel || deleting {
			return fault(f.ConfirmationStale)
		}
		var callID string
		err = x.QueryRow(ctx, `SELECT call_id FROM agenteam_meeting.summary_model_calls WHERE generation_id=$1 AND attempt=$2`, input.GenerationID.String(), attempt).Scan(&callID)
		if err == nil {
			id, err := f.ParseID[mc.Call](callID)
			if err != nil {
				return fault(f.InternalError)
			}
			out, err = loadSummaryCall(ctx, x, id)
			if err != nil {
				return err
			}
			if out.digest != inputDigest {
				return fault(f.ConfirmationStale)
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return portError(err)
		}
		if err = m.deps.Authority.guard.RequireCurrentProcessInTx(ctx, tx, m.deps.Authority.process); err != nil {
			return portError(err)
		}
		id, err := f.NewID[mc.Call]()
		if err != nil {
			return portError(err)
		}
		at := now()
		deadline := at.Add(60 * time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		purpose := mc.MeetingSummaryUpdate
		if input.Initial {
			purpose = mc.MeetingSummaryInitial
		}
		process := m.deps.Authority.process.String()
		out = summaryCallRecord{fresh: true, attempt: attempt, call: id, input: input, purpose: purpose, digest: inputDigest, messages: messageRaw, format: formatRaw, status: "prepared", deadline: deadline, preparedProcess: &process}
		if err = m.captureSummaryResolve(ctx, x, &out); err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.summary_model_calls(call_id,generation_id,project_id,meeting_id,attempt,purpose,input_digest,messages_data,response_format_data,status,deadline_at,created_at,prepared_process_id,resolve_source_data,resolve_binding) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'prepared',$10,$11,$12,$13,$14)`, id.String(), input.GenerationID.String(), input.ProjectID.String(), input.MeetingID.String(), attempt, string(purpose), inputDigest.String(), messageRaw, formatRaw, deadline, at, process, out.resolveSource, *out.resolveBinding); err != nil {
			return portError(err)
		}
		return nil
	})
	return out, err
}
func (m *SummaryModel) GenerateSummary(ctx context.Context, input SummaryInput) (candidateOut SummaryCandidate, returnedErr error) {
	var zero SummaryCandidate
	if m == nil || ctx == nil || input.GenerationID.Validate() != nil || input.InputDigest.Validate() != nil {
		return zero, fault(f.InvalidArgument)
	}
	m.mu.Lock()
	stopped := m.stopped
	m.mu.Unlock()
	if stopped {
		return zero, fault(f.InvalidState)
	}
	row, err := m.prepare(ctx, input)
	if err != nil {
		return zero, err
	}
	m.mu.Lock()
	retained := m.calls[row.call]
	m.mu.Unlock()
	if retained != nil {
		return m.recoverCall(ctx, retained)
	}
	if row.retired != nil {
		return m.completedCandidate(ctx, input, row)
	}
	scope, err := i.InProject(input.ProjectID)
	if err != nil {
		return zero, err
	}
	actor, err := m.deps.Actor.Actor(input.GenerationID.String(), scope)
	if err != nil {
		return zero, err
	}
	runCtx, cancel := context.WithDeadline(ctx, row.deadline)
	owner := &summaryOwner{authority: m.deps.Authority, input: input, call: row.call, active: true}
	call := &summaryModelCall{owner: owner, cancel: cancel, actor: actor, dispatchUnknown: !row.mayBegin()}
	call.mu.Lock()
	defer call.mu.Unlock()
	m.mu.Lock()
	if m.stopped || m.calls[row.call] != nil {
		m.mu.Unlock()
		cancel()
		return zero, fault(f.ResourceBusy)
	}
	m.calls[row.call] = call
	m.mu.Unlock()
	defer func() {
		cancel()
		if !call.done && returnedErr != nil {
			returnedErr = errors.Join(unknownSummary(), returnedErr)
		}
		m.mu.Lock()
		if call.done {
			delete(m.calls, row.call)
		}
		m.mu.Unlock()
	}()
	if call.dispatchUnknown {
		return m.recoverStoppedCall(ctx, call, row)
	}
	stopCancellationWatch := m.watchSummaryCancellation(runCtx, call)
	defer stopCancellationWatch()
	runCtx = context.WithValue(runCtx, summaryOwnerKey{}, owner)
	resolve, err := m.resolutionRequest(runCtx, call, row)
	if err != nil {
		call.failure = err
		cleanup, done := context.WithTimeout(context.WithoutCancel(runCtx), 10*time.Second)
		defer done()
		if settle := m.finishCall(cleanup, call, false); settle != nil {
			return zero, settle
		}
		return zero, err
	}
	call.resolve = &resolve
	consumer := resolve.Consumer
	var resolved mc.ResolvedModel
	if row.status == "prepared" && !row.unknown {
		if err = m.tx(runCtx, input, "resolving", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
			_, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET status='resolving',resolution_unknown=true WHERE call_id=$1 AND status='prepared'`, row.call.String())
			return portError(err)
		}); err != nil {
			return zero, err
		}
		resolved, err = m.deps.Resolver.ResolveModel(runCtx, resolve)
	} else if row.status == "resolving" || row.status == "resolved" {
		var found bool
		resolved, found, err = m.deps.Resolver.LookupResolution(runCtx, resolve)
		if err == nil && !found {
			err = f.NewFault(f.CommitUnknown, f.Unknown)
		}
	} else {
		err = f.NewFault(f.CommitUnknown, f.Unknown)
	}
	if err != nil {
		call.failure = err
		var known *f.Fault
		if errors.As(err, &known) && (known.CommitState == f.NotStarted || known.CommitState == f.NotCommitted) {
			cleanup, done := context.WithTimeout(context.WithoutCancel(runCtx), 10*time.Second)
			defer done()
			if settle := m.finishCall(cleanup, call, false); settle != nil {
				return zero, settle
			}
		} else {
			call.resolutionUnknown = true
		}
		return zero, err
	}
	call.resolved = &resolved
	if err = owner.bindResolved(resolved); err != nil {
		return zero, err
	}
	if err = m.tx(runCtx, input, "resolved", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		_, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET status='resolved',snapshot_id=$2,resolution_unknown=false WHERE call_id=$1 AND status IN('resolving','resolved')`, row.call.String(), resolved.Snapshot.ID.String())
		return portError(err)
	}); err != nil {
		call.failure = err
		return zero, err
	}
	if err = m.pinSummarySnapshot(runCtx, call); err != nil {
		call.failure = err
		if isUnknown(err) {
			return zero, err
		}
		cleanup, done := context.WithTimeout(context.WithoutCancel(runCtx), 10*time.Second)
		defer done()
		if settle := m.finishCall(cleanup, call, false); settle != nil {
			return zero, settle
		}
		return zero, err
	}
	messages, decodeErr := mc.DecodeCanonicalMessages(row.messages)
	var format mc.ResponseFormat
	if decodeErr != nil || json.Unmarshal(row.format, &format) != nil {
		return zero, fault(f.InternalError)
	}
	request := mc.ModelRequest{Actor: actor, CallID: row.call, Consumer: consumer, Model: resolved, Input: mc.InputIdentity{OperationID: input.GenerationID.String(), Digest: row.digest, SchemaVersion: 1}, Messages: messages, Tools: []mc.Tool{}, ToolChoice: mc.ToolChoice{Kind: "none"}, ResponseFormat: format, RetryClass: mc.BoundedRetry}
	if err = request.Validate(); err != nil {
		return zero, err
	}
	if err = m.tx(runCtx, input, "running", nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		_, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_model_calls SET status='running' WHERE call_id=$1 AND status='resolved'`, row.call.String())
		return portError(err)
	}); err != nil {
		return zero, err
	}
	owner.mu.Lock()
	originalRequest := request.Clone()
	owner.sourceRequest = &originalRequest
	owner.mu.Unlock()
	call.handle, err = m.deps.Calls.BeginChat(runCtx, request)
	var response mc.ModelResponse
	if err == nil {
		response, err = call.handle.Result(runCtx)
	}
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(runCtx), 10*time.Second)
	defer cleanupCancel()
	owner.mu.Lock()
	owner.sourceClosing = true
	owner.mu.Unlock()
	if call.handle != nil {
		closeErr := call.handle.Close(cleanupCtx)
		if !call.handle.Joined() {
			call.failure = errors.Join(err, closeErr, f.NewFault(f.CommitUnknown, f.Unknown))
			return zero, call.failure
		}
		if err != nil {
			if confirmed, e := call.handle.Result(cleanupCtx); e == nil {
				response, err = confirmed, nil
			}
		}
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}
	var candidate SummaryCandidate
	if err == nil {
		var text strings.Builder
		if response.FinishReason != "stop" {
			err = fault(f.InvalidState)
		} else {
			for _, part := range response.Message.Parts {
				if part.Text == nil {
					err = fault(f.InvalidState)
					break
				}
				text.WriteString(part.Text.Text)
			}
			if err == nil {
				candidate, err = ParseSummary([]byte(text.String()), input.Initial)
			}
		}
	}
	call.failure = err
	if err == nil {
		call.candidate = &candidate
	}
	if finishErr := m.finishCall(cleanupCtx, call, err == nil); finishErr != nil {
		return zero, finishErr
	}
	if err != nil {
		return zero, err
	}
	return candidate, nil
}
func (m *SummaryModel) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.stopped = true
	for _, call := range m.calls {
		call.cancel()
	}
	m.mu.Unlock()
}
func (m *SummaryModel) Joined() bool {
	if m == nil {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped && len(m.calls) == 0 && len(m.transitions) == 0
}
func (m *SummaryModel) Drain(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if m.Joined() {
			return nil
		}
		m.mu.Lock()
		calls := make([]*summaryModelCall, 0, len(m.calls))
		for _, call := range m.calls {
			calls = append(calls, call)
		}
		transitions := make([]*summaryTransition, 0, len(m.transitions))
		for _, transition := range m.transitions {
			transitions = append(transitions, transition)
		}
		m.mu.Unlock()
		for _, transition := range transitions {
			if transition.mu.TryLock() {
				_ = m.observeSummaryTransition(ctx, transition)
				transition.mu.Unlock()
			}
		}
		for _, call := range calls {
			_, _ = m.recoverCall(ctx, call)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

var _ SummaryGenerator = (*SummaryModel)(nil)
