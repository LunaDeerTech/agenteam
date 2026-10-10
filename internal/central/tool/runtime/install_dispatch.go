package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/authorization"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// InstallSchemaValidator must validate against the original immutable Registry
// schemas with the standard declared JSON-Schema dialect. The fixed typed
// parser is not an implementation of this interface. Unsupported schema or an
// absent standard validator must fail before an Attempt is dispatched.
// Metadata access uses the original Tx and pre-collected Registry/ToolSpec
// locks. There is no external network, nested transaction, or lock extension.
type InstallSchemaValidator interface {
	ValidateInstallInputInTx(context.Context, f.Tx, tc.SpecRef, []byte) error
	ValidateInstallOutputInTx(context.Context, f.Tx, tc.SpecRef, json.RawMessage) error
}

type InstallExecutor struct{ data func() installExecutorData }
type installExecutorData struct {
	authority *InstallAuthority
	schemas   InstallSchemaValidator
	adapter   *builtin.SkillInstallAdapter
}

func NewInstallExecutor(authority *InstallAuthority, service builtin.SkillInstallService, schemas InstallSchemaValidator) (*InstallExecutor, error) {
	if authority == nil || authority.data == nil || nilPort(service) || nilPort(schemas) {
		return nil, fail(f.DependencyUnbound)
	}
	adapter, err := builtin.NewSkillInstallAdapter(service, authority)
	if err != nil {
		return nil, err
	}
	d := installExecutorData{authority, schemas, adapter}
	return &InstallExecutor{data: func() installExecutorData { return d }}, nil
}

// InstallOutcome is the durable observed terminal projection. It is not a
// Transcript delivery receipt; only the actual Execution writer may append it.
// Explicit methods return copies. Default recursive logs never include output.
type InstallOutcome struct{ data func() installTerminal }

func (v InstallOutcome) ModelOutput() json.RawMessage {
	if v.data == nil {
		return nil
	}
	return bytes.Clone(v.data().Output)
}
func (v InstallOutcome) Receipt() (skill.InstallReceipt, bool) {
	if v.data == nil || v.data().Receipt == nil {
		return skill.InstallReceipt{}, false
	}
	r := *v.data().Receipt
	return r.receipt(), true
}
func (v InstallOutcome) Status() (string, string, bool) {
	if v.data == nil {
		return "", "", false
	}
	d := v.data()
	return d.Status, d.Category, d.Known
}

// ExecuteInstall consumes a previously observed created Operation, revalidates
// its original bytes and current authority, persists one Attempt, then calls
// the real adapter exactly once. It never retries, regenerates the target/key,
// or starts a detached worker. A pending/terminal operation is not redispatched.
func (e *InstallExecutor) ExecuteInstall(ctx context.Context, prepared PreparedOperation, rawArguments []byte) (out InstallOutcome, err error) {
	if err = contextError(ctx); err != nil {
		return out, err
	}
	if e == nil || e.data == nil || prepared.data == nil {
		return out, fail(f.DependencyUnbound)
	}
	r := prepared.data()
	if err = r.validate(); err != nil {
		return out, err
	}
	d := e.data()
	a := d.authority.data()
	callCtx, cancel := context.WithCancel(ctx)
	h := &installHandoff{issuer: a, record: r, cancel: cancel, ctx: callCtx, done: make(chan struct{})}
	a.mu.Lock()
	if a.stopped || a.active[r.ID] != nil {
		stopped := a.stopped
		a.mu.Unlock()
		cancel()
		if stopped {
			return out, fail(f.ShuttingDown)
		}
		return out, fail(f.ResourceBusy)
	}
	a.active[r.ID] = h
	a.mu.Unlock()
	// Admission covers every dependency call, including discovery. Before the
	// first transaction there is no durable Attempt to retire, but Stop/Drain
	// must still cancel and join this original call.
	retired := true
	defer func() {
		cancel()
		h.mu.Lock()
		h.live = false
		if !retired && h.retirement == nil {
			h.retirement = fail(f.DependencyUnavailable)
		}
		h.mu.Unlock()
		a.mu.Lock()
		if retired {
			delete(a.active, r.ID)
		}
		a.mu.Unlock()
		close(h.done)
	}()
	b, err := r.Input.Binding.request()
	if err != nil {
		return out, err
	}
	if len(rawArguments) > builtin.MaxSkillInstallArguments {
		return out, fail(f.PayloadTooLarge)
	}
	raw := bytes.Clone(rawArguments)
	defer clear(raw)
	input, err := prepareInput(callCtx, b, raw)
	if err != nil {
		return out, err
	}
	encoded, err := encode(input)
	if err != nil || digest(encoded) != r.InputDigest {
		return out, fail(f.IdempotencyKeyReused)
	}
	pkg, _, err := parseInstall(callCtx, raw)
	if err != nil {
		return out, err
	}
	call, err := builtin.NewSkillInstallCall(callCtx, b.Actor, b.Spec, r.ID.String(), r.SkillID, pkg)
	if err != nil {
		return out, err
	}
	permissionInput := authorization.InstallInput{Binding: b, Call: call, Fingerprint: r.Fingerprint}
	permissionPlan, err := a.permission.DiscoverInstall(callCtx, permissionInput)
	if err != nil {
		return out, portError(err)
	}
	cause, err := inputCause(b)
	if err != nil {
		return out, err
	}
	operationLock, _ := f.AggregateLock(f.OperationAggregate, r.ID.String())
	locks, err := baseLocks(b, cause, append(permissionPlan.RequiredLocks(), f.LockRequest{Key: operationLock, Mode: f.Exclusive}))
	if err != nil {
		return out, err
	}
	attempt, err := f.NewID[tc.Attempt]()
	if err != nil {
		return out, portError(err)
	}
	request, err := f.NewID[f.Request]()
	if err != nil {
		return out, portError(err)
	}
	h.binding, h.call, h.input, h.permission = b, call, permissionInput, permissionPlan
	h.locks, h.attempt, h.request, h.cause = locks, attempt, request, cause
	// From this point a crash/panic cannot manufacture durable retirement.
	// Unknown retains ownership until recovery confirms this exact attempt.
	retired = false
	result := startInstallAttempt(callCtx, a, h, d.schemas, raw)
	if err = commitError(result); err != nil {
		// A known rollback created no physical Attempt. Unknown is retained
		// with its exact transaction cause; it must never start the adapter.
		retired = result.State() == f.NotCommitted
		if !retired {
			h.retirement = err
		}
		return out, err
	}
	h.mu.Lock()
	h.live = true
	h.mu.Unlock()
	witnessCtx := context.WithValue(callCtx, installHandoffKey{}, installHandoffContext{a, h})
	backendResult, backendErr := d.adapter.Execute(witnessCtx, call, request)
	// The original synchronous adapter, Service and its owned physical cleanup
	// have actually returned. Stop cannot convert this event into an earlier join.
	h.mu.Lock()
	h.live = false
	h.mu.Unlock()
	terminal := terminalInstall(h, backendResult, backendErr)
	h.mu.Lock()
	h.terminal = &terminal
	h.mu.Unlock()
	// Durable accounting must preserve a known receipt after late cancellation.
	// This one bounded final Tx is joined synchronously; no retry or background
	// recovery is started when the commit result is unknown.
	finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer finalCancel()
	result = finishInstallAttempt(finalCtx, a, h, d.schemas, &terminal)
	if err = commitError(result); err != nil {
		h.mu.Lock()
		h.retirement = err
		h.mu.Unlock()
		return InstallOutcome{}, err
	}
	retired = true
	copy := terminal.clone()
	return InstallOutcome{data: func() installTerminal { return copy.clone() }}, nil
}

// Only explicit known failures are classified as known. A plain cancelled
// error after starting a Backend is not proof that no publication occurred.
func terminalInstall(h *installHandoff, result builtin.SkillInstallResult, err error) installTerminal {
	v := installTerminal{Format: 1, Operation: h.record.ID, Attempt: h.attempt, Status: "error", Category: "unknown_outcome", Known: false}
	v.Unknown = installUnknown(err)
	if err == nil {
		receipt := result.Receipt()
		output, projectionErr := builtin.ProjectSkillInstallReceipt(context.Background(), h.call, receipt)
		if projectionErr == nil && bytes.Equal(output, result.ModelOutput()) {
			value := projectInstallReceipt(receipt)
			v.Receipt, v.Output = &value, bytes.Clone(output)
			v.Status, v.Category, v.Known = "success", "", true
			return v
		}
		v.Category = "backend_contract_violation"
		return v
	}
	if errors.Is(err, builtin.ErrSkillInstallReceipt) {
		v.Category = "backend_contract_violation"
		return v
	}
	var fault *f.Fault
	if errors.As(err, &fault) && (fault.CommitState == f.NotStarted || fault.CommitState == f.NotCommitted) {
		v.Category, v.Known = "business_rule_violation", true
		switch fault.Code {
		case f.Forbidden, f.Unauthenticated, f.SessionRevoked:
			v.Category = "authorization_denied"
		case f.CapabilityUnsupported, f.DependencyUnbound:
			v.Category = "capability_unsupported"
		case f.NotFound:
			v.Category = "not_found"
		case f.VersionConflict, f.IdempotencyKeyReused, f.ResourceBusy:
			v.Category = "conflict"
		case f.InvalidArgument, f.PayloadTooLarge:
			v.Category = "invalid_arguments"
		case f.DependencyUnavailable, f.InternalError:
			v.Category = "backend_unavailable"
		}
	}
	return v
}

func (InstallExecutor) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "tool_install_executor") }
func (InstallExecutor) LogValue() slog.Value         { return slog.StringValue("tool_install_executor") }
func (InstallExecutor) MarshalJSON() ([]byte, error) { return []byte(`"tool_install_executor"`), nil }
func (InstallOutcome) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "tool_install_outcome") }
func (InstallOutcome) LogValue() slog.Value          { return slog.StringValue("tool_install_outcome") }
func (InstallOutcome) MarshalJSON() ([]byte, error)  { return []byte(`"tool_install_outcome"`), nil }
