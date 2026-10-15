package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

// MCPAuthority is composed before the backend, then bound once to the actual
// executor. An invocation descriptor or a persisted running row cannot mint a
// dispatch permission; every loan requires the original active engine witness.
type MCPAuthority struct {
	base   *BuiltinAuthority
	mu     sync.RWMutex
	engine *BuiltinExecutor
}

func NewMCPAuthority(base *BuiltinAuthority) (*MCPAuthority, error) {
	if base == nil || nilPort(base.store) || base.guard == nil {
		return nil, fail(f.DependencyUnbound)
	}
	return &MCPAuthority{base: base}, nil
}

type mcpInvocationPlan struct {
	owner      *MCPAuthority
	witness    *builtinWitness
	invocation tc.MCPInvocation
	locks      []f.LockRequest
}

func (p *mcpInvocationPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

func (a *MCPAuthority) witness(ctx context.Context, v tc.MCPInvocation) (*builtinWitness, error) {
	if a == nil || a.base == nil || v.Validate() != nil {
		return nil, fail(f.Forbidden)
	}
	w, err := a.base.witness(ctx, v.Call.Actor, v.Call.ProjectID)
	if err != nil {
		return nil, err
	}
	a.mu.RLock()
	engine := a.engine
	a.mu.RUnlock()
	if engine == nil || w.owner != engine || w.call.lookup || !sameRunnerCallBinding(w.call.binding, v.Call) ||
		w.call.record.ID != v.OperationID || w.call.record.Attempt != v.AttemptID || string(w.call.record.Request) != v.RequestID {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

func (a *MCPAuthority) DiscoverMCPInvocation(ctx context.Context, v tc.MCPInvocation) (tc.MCPInvocationPlan, error) {
	w, err := a.witness(ctx, v)
	if err != nil {
		return nil, err
	}
	return &mcpInvocationPlan{a, w, v, slices.Clone(w.call.locks)}, nil
}

func (a *MCPAuthority) RequireMCPInvocationInTx(ctx context.Context, tx f.Tx, v tc.MCPInvocation, plan tc.MCPInvocationPlan) error {
	p, ok := plan.(*mcpInvocationPlan)
	if !ok || p == nil || p.owner != a || !sameMCPInvocation(p.invocation, v) {
		return fail(f.Forbidden)
	}
	w, err := a.witness(ctx, v)
	if err != nil {
		return err
	}
	if w != p.witness {
		return fail(f.Forbidden)
	}
	return a.base.withCurrent(ctx, tx, w, func(current context.Context, _ []byte) error {
		return w.owner.executions.(tc.MCPDispatchAuthority).RequireMCPAdmissionInTx(current, tx, w.call.binding, w.call.plan)
	})
}

func sameMCPInvocation(a, b tc.MCPInvocation) bool {
	return a.OperationID == b.OperationID && a.AttemptID == b.AttemptID && a.RequestID == b.RequestID && sameRunnerCallBinding(a.Call, b.Call)
}

// Only MCP structuredContent is an output-schema instance. Text and media
// blocks remain protocol result parts. An absent MCP output schema is valid.
func (a *MCPAuthority) ValidateMCPOutput(ctx context.Context, v tc.MCPInvocation, raw json.RawMessage) error {
	w, err := a.witness(ctx, v)
	if err != nil {
		return err
	}
	return w.owner.transaction(ctx, w.call, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		return a.base.withCurrent(ctx, tx, w, func(current context.Context, _ []byte) error {
			if err := w.owner.executions.(tc.MCPDispatchAuthority).RequireMCPAdmissionInTx(current, tx, w.call.binding, w.call.plan); err != nil {
				return err
			}
			definition, err := w.owner.registry.ReadDefinitionInTx(current, tx, v.Call.Spec)
			if err != nil {
				return schemaReadError(err)
			}
			if len(definition.OutputSchema) == 0 {
				return nil
			}
			return w.owner.schemas.validate(current, tx, v.Call.Spec, raw, true)
		})
	})
}

// MCPExecutor reuses the direct Tool engine's original Operation, Attempt,
// approval, lifecycle and canonical terminal. It adds only backend selection
// and the closed MCP result projection; there is no second execution ledger.
type MCPExecutor struct {
	core    *BuiltinExecutor
	backend *mcpEngineBackend
}

func NewMCPExecutor(authority *MCPAuthority, executions tc.BuiltinInputAuthority, reg *registry.Registry, backend tc.MCPBackend) (*MCPExecutor, error) {
	if authority == nil || authority.base == nil || nilPort(executions) || reg == nil || nilPort(backend) {
		return nil, fail(f.DependencyUnbound)
	}
	if committed, ok := executions.(tc.CommittedBuiltinInputAuthority); !ok || nilPort(committed) {
		return nil, fail(f.DependencyUnbound)
	}
	if admission, ok := executions.(tc.MCPDispatchAuthority); !ok || nilPort(admission) {
		return nil, fail(f.DependencyUnbound)
	}
	schemas, err := NewRegistrySchemaValidator(authority.base.store, reg)
	if err != nil {
		return nil, err
	}
	e := &MCPExecutor{backend: &mcpEngineBackend{backend: backend}}
	e.core = &BuiltinExecutor{authority: authority.base, executions: executions, registry: reg, schemas: schemas, backends: map[tc.BuiltinBinding]BuiltinBackend{}, calls: map[tc.ToolCallIdentity]*builtinCall{}, lookups: map[*builtinCall]struct{}{}, changed: make(chan struct{}), mcp: e}
	authority.mu.Lock()
	defer authority.mu.Unlock()
	if authority.engine != nil {
		return nil, fail(f.InvalidState)
	}
	authority.engine = e.core
	return e, nil
}

func (e *MCPExecutor) Execute(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerOutcome, error) {
	if e == nil || e.core == nil || b.MCPBinding.Validate() != nil {
		return tc.RunnerOutcome{}, fail(f.InvalidArgument)
	}
	return e.core.Execute(ctx, b)
}
func (e *MCPExecutor) Lookup(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerOutcome, bool, error) {
	if e == nil || e.core == nil || b.MCPBinding.Validate() != nil {
		return tc.RunnerOutcome{}, false, fail(f.InvalidArgument)
	}
	return e.core.Lookup(ctx, b)
}
func (e *MCPExecutor) DiscoverRunnerResult(ctx context.Context, b tc.ToolCallBinding) (tc.RunnerResultPlan, error) {
	if e == nil || e.core == nil || b.MCPBinding.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	return e.core.DiscoverRunnerResult(ctx, b)
}
func (e *MCPExecutor) WithRunnerResultInTx(ctx context.Context, tx f.Tx, plan tc.RunnerResultPlan, consume func(context.Context, tc.RunnerResultBinding, mc.ToolResultPart) error) error {
	if e == nil || e.core == nil {
		return fail(f.DependencyUnbound)
	}
	return e.core.WithRunnerResultInTx(ctx, tx, plan, consume)
}
func (e *MCPExecutor) CancelCall(ctx context.Context, b tc.ToolCallBinding) error {
	if e == nil || e.core == nil || b.MCPBinding.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	return e.core.CancelCall(ctx, b)
}
func (e *MCPExecutor) JoinCall(ctx context.Context, b tc.ToolCallBinding) error {
	if e == nil || e.core == nil || b.MCPBinding.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	return e.core.JoinCall(ctx, b)
}
func (e *MCPExecutor) Stop() {
	if e != nil && e.core != nil {
		e.core.Stop()
	}
}
func (e *MCPExecutor) Drain(ctx context.Context) error {
	if e == nil || e.core == nil {
		return fail(f.DependencyUnbound)
	}
	return e.core.Drain(ctx)
}
func (e *MCPExecutor) Joined() bool { return e != nil && e.core != nil && e.core.Joined() }

func directToolBinding(b tc.ToolCallBinding) bool {
	return b.Validate() == nil && (tc.DomainBuiltinBinding(b.Binding) || b.MCPBinding.Validate() == nil)
}
func directOperationKind(b bindingData) string {
	call, err := b.request()
	if err != nil {
		return ""
	}
	if call.MCPBinding.Validate() == nil {
		return "mcp.call"
	}
	return call.Binding.HandlerID
}
func (e *BuiltinExecutor) backendFor(call *builtinCall) BuiltinBackend {
	if e == nil || call == nil {
		return nil
	}
	if call.binding.MCPBinding.Validate() == nil {
		if e.mcp == nil {
			return nil
		}
		return e.mcp.backend
	}
	return e.backends[call.binding.Binding]
}
func (e *BuiltinExecutor) validateBackendInTx(ctx context.Context, tx f.Tx, call *builtinCall, raw []byte) error {
	backend := e.backendFor(call)
	if nilPort(backend) {
		return fail(f.DependencyUnbound)
	}
	if call.binding.MCPBinding.Validate() == nil {
		if err := backend.RequireBindingInTx(ctx, tx, e.authority.store); err != nil {
			return err
		}
	} else {
		reg := backend.Registration()
		var err error
		switch reg.Class {
		case tc.CoreTool:
			err = e.registry.RequireCurrentWorkArtifactBuiltinInTx(ctx, tx, call.binding.Spec, call.binding.Binding, reg.ScopeResolverID, reg.RiskClassifierID)
		case tc.OrdinaryTool:
			err = e.registry.RequireCurrentBuiltinInTx(ctx, tx, call.binding.Spec, call.binding.Binding, reg.ScopeResolverID, reg.RiskClassifierID)
		default:
			return fail(f.InvalidArgument)
		}
		if err != nil {
			return err
		}
	}
	return e.schemas.validate(ctx, tx, call.binding.Spec, raw, false)
}
func (e *BuiltinExecutor) backendTerminal(call *builtinCall, body []byte) (string, string, error) {
	if skillPrepareResultBinding(call.binding) {
		outcome, code, _, err := skillPrepareReturnedTerminal(body)
		return outcome, code, err
	}
	if !typedMCPResultBinding(call.binding) {
		return "success", "", nil
	}
	result, err := decodeMCPResult(call.binding, body)
	if err != nil {
		return "", "", err
	}
	if !result.OutcomeKnown {
		return "unknown", string(f.DependencyUnavailable), nil
	}
	if result.Result.IsError {
		if f.Code(result.FailureCode).Known() {
			return "failure", result.FailureCode, nil
		}
		return "failure", string(f.DependencyUnavailable), nil
	}
	return "success", "", nil
}
func (e *BuiltinExecutor) backendFailure(call *builtinCall, code string) ([]byte, error) {
	if typedMCPResultBinding(call.binding) {
		return encodeMCPResult(call.binding, tc.MCPBackendResult{Result: mc.ToolResultPart{CallID: call.binding.CallID, IsError: true, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: code}}}}, OutcomeKnown: true, FailureCode: code})
	}
	return json.Marshal(map[string]any{"error": map[string]string{"code": code}})
}
func (e *BuiltinExecutor) resultPart(call tc.ToolCallBinding, terminal builtinTerminal, body []byte) (mc.ToolResultPart, error) {
	if !typedMCPResultBinding(call) {
		return mc.ToolResultPart{CallID: call.CallID, IsError: terminal.Outcome != "success", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: string(body)}}}}, nil
	}
	result, err := decodeMCPResult(call, body)
	if err != nil {
		return mc.ToolResultPart{}, err
	}
	outcome, code, err := e.backendTerminal(&builtinCall{binding: call}, body)
	if err != nil || outcome != terminal.Outcome || code != terminal.Code {
		return mc.ToolResultPart{}, fail(f.InvalidState)
	}
	return result.Result, nil
}

type mcpEngineBackend struct{ backend tc.MCPBackend }

func (*mcpEngineBackend) Registration() tc.BuiltinRegistration { return tc.BuiltinRegistration{} }
func (b *mcpEngineBackend) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	return b.backend.RequireBindingInTx(ctx, tx, store)
}
func (b *mcpEngineBackend) Execute(ctx context.Context, in BuiltinInvocation, raw []byte) ([]byte, error) {
	v := tc.MCPInvocation{Call: in.Call, OperationID: in.OperationID, AttemptID: in.AttemptID, RequestID: string(in.RequestID)}
	if v.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	result, err := b.backend.Execute(ctx, v, raw)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeMCPResult(in.Call, result)
	if err == nil {
		return encoded, nil
	}
	// The backend already returned. A local projection/size failure must not
	// erase its effect evidence or leave a complete remote call retryable.
	fallback := tc.MCPBackendResult{OutcomeKnown: result.OutcomeKnown, MayHaveExecuted: result.MayHaveExecuted, FailureCode: "mcp_invalid_response", Result: mc.ToolResultPart{CallID: in.Call.CallID, IsError: true, Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "mcp_invalid_response"}}}}}
	if !fallback.OutcomeKnown {
		fallback.MayHaveExecuted = true
	}
	return encodeMCPResult(in.Call, fallback)
}
func (*mcpEngineBackend) Lookup(context.Context, BuiltinInvocation, []byte) ([]byte, bool, error) {
	return nil, false, nil
}

// Explicit persistence projection: BusinessFileRef's general JSON form is
// deliberately opaque. Never persist it through generic model marshaling.
type mcpResultEnvelope struct {
	Format          int             `json:"format"`
	CallID          string          `json:"call_id"`
	IsError         bool            `json:"is_error"`
	Parts           []mcpResultPart `json:"parts"`
	OutcomeKnown    bool            `json:"outcome_known"`
	MayHaveExecuted bool            `json:"may_have_executed"`
	FailureCode     string          `json:"failure_code"`
}
type mcpResultPart struct {
	Kind      string      `json:"kind"`
	Text      string      `json:"text,omitempty"`
	MediaType string      `json:"media_type,omitempty"`
	Ref       *mcpFileRef `json:"ref,omitempty"`
}
type mcpFileRef struct {
	Kind      oc.SourceKind `json:"kind"`
	ProjectID id.ProjectID  `json:"project_id"`
	FileID    string        `json:"file_id"`
}

func encodeMCPResult(call tc.ToolCallBinding, result tc.MCPBackendResult) ([]byte, error) {
	if result.Result.Validate() != nil || result.Result.CallID != call.CallID || !typedMCPResultBinding(call) ||
		!result.OutcomeKnown && (!result.MayHaveExecuted || !result.Result.IsError) || result.FailureCode != "" && !result.Result.IsError || !safeMCPFailureCode(result.FailureCode) {
		return nil, fail(f.InvalidState)
	}
	e := mcpResultEnvelope{Format: 1, CallID: result.Result.CallID, IsError: result.Result.IsError, OutcomeKnown: result.OutcomeKnown, MayHaveExecuted: result.MayHaveExecuted, FailureCode: result.FailureCode, Parts: make([]mcpResultPart, 0, len(result.Result.Parts))}
	for _, p := range result.Result.Parts {
		v := mcpResultPart{}
		if p.Text != nil {
			v.Kind, v.Text = "text", p.Text.Text
		} else {
			media := p.File
			v.Kind = "file"
			if p.Image != nil {
				media, v.Kind = p.Image, "image"
			}
			d := media.Ref.Details()
			if d.Kind != oc.MCPFile || d.ProjectID != call.ProjectID {
				return nil, fail(f.Forbidden)
			}
			v.MediaType, v.Ref = media.MediaType, &mcpFileRef{d.Kind, d.ProjectID, d.FileID}
		}
		e.Parts = append(e.Parts, v)
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return nil, fail(f.InvalidState)
	}
	if len(raw) > 1<<20 {
		return nil, fail(f.PayloadTooLarge)
	}
	return raw, nil
}

// Only the two fixed Resource handlers share MCP's typed result persistence.
// This metadata check does not authorize execution; their actual backend still
// requires the original builtin invocation and captured connection authority.
func typedMCPResultBinding(call tc.ToolCallBinding) bool {
	if call.Validate() != nil {
		return false
	}
	if call.MCPBinding.Validate() == nil {
		return true
	}
	return call.Binding.ContractRevision == 1 && (call.Binding.HandlerID == "mcp.list-resources" || call.Binding.HandlerID == "mcp.read-resource")
}

func EncodeMCPResourceResult(in BuiltinInvocation, result tc.MCPBackendResult) ([]byte, error) {
	if in.Validate() != nil || !typedMCPResultBinding(in.Call) {
		return nil, fail(f.InvalidArgument)
	}
	return encodeMCPResult(in.Call, result)
}
func decodeMCPResult(call tc.ToolCallBinding, raw []byte) (tc.MCPBackendResult, error) {
	var e mcpResultEnvelope
	if len(raw) < 2 || len(raw) > 1<<20 {
		return tc.MCPBackendResult{}, fail(f.InvalidState)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&e) != nil || e.Format != 1 || len(e.Parts) > 256 {
		return tc.MCPBackendResult{}, fail(f.InvalidState)
	}
	result := tc.MCPBackendResult{Result: mc.ToolResultPart{CallID: e.CallID, IsError: e.IsError, Parts: make([]mc.ResultPart, 0, len(e.Parts))}, OutcomeKnown: e.OutcomeKnown, MayHaveExecuted: e.MayHaveExecuted, FailureCode: e.FailureCode}
	for _, p := range e.Parts {
		var v mc.ResultPart
		switch p.Kind {
		case "text":
			if p.Ref != nil || p.MediaType != "" {
				return tc.MCPBackendResult{}, fail(f.InvalidState)
			}
			v.Text = &mc.TextPart{Text: p.Text}
		case "file", "image":
			if p.Ref == nil || p.Ref.Kind != oc.MCPFile || p.Ref.ProjectID != call.ProjectID || p.Text != "" {
				return tc.MCPBackendResult{}, fail(f.Forbidden)
			}
			ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: p.Ref.Kind, ProjectID: p.Ref.ProjectID, FileID: p.Ref.FileID})
			if err != nil {
				return tc.MCPBackendResult{}, fail(f.InvalidState)
			}
			media := &mc.MediaPart{Ref: ref, MediaType: p.MediaType}
			if p.Kind == "image" {
				v.Image = media
			} else {
				v.File = media
			}
		default:
			return tc.MCPBackendResult{}, fail(f.InvalidState)
		}
		result.Result.Parts = append(result.Result.Parts, v)
	}
	canonical, err := encodeMCPResult(call, result)
	if err != nil || !bytes.Equal(canonical, raw) {
		return tc.MCPBackendResult{}, fail(f.InvalidState)
	}
	return result, nil
}
func safeMCPFailureCode(code string) bool {
	if code == "" || f.Code(code).Known() {
		return true
	}
	switch code {
	case "mcp_invalid_request", "mcp_invalid_response", "mcp_transport_error", "mcp_authentication_required", "mcp_protocol_version_unsupported", "mcp_protocol_error", "mcp_capability_unsupported", "mcp_content_too_large", "mcp_timeout", "mcp_cancelled", "mcp_sensitive_response", "mcp_dependency_unavailable":
		return true
	}
	return false
}

// ReserveApprovalOperation writes only the canonical created Operation. The
// original input callback grants no dispatch permission and creates no Attempt.
func (e *MCPExecutor) ReserveApprovalOperation(ctx context.Context, b tc.ToolCallBinding) (tc.ApprovalOperation, error) {
	if e == nil || e.core == nil || b.MCPBinding.Validate() != nil {
		return tc.ApprovalOperation{}, fail(f.InvalidArgument)
	}
	call, done, err := e.core.beginLookup(ctx, b)
	if err != nil {
		return tc.ApprovalOperation{}, err
	}
	defer done()
	// This is a current unattempted request, not a committed-result lookup.
	call.lookup = false
	if err = e.core.discover(call.ctx, call); err != nil {
		return tc.ApprovalOperation{}, err
	}
	var old *builtinRecord
	err = e.core.transaction(call.ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return e.core.input(ctx, tx, call, func(current context.Context, _ []byte) error {
			var err error
			old, err = loadBuiltinRecord(current, x, b)
			return err
		})
	})
	if err != nil {
		return tc.ApprovalOperation{}, err
	}
	operation, err := f.NewID[tc.Operation]()
	if err != nil {
		return tc.ApprovalOperation{}, portError(err)
	}
	if old != nil {
		operation = old.ID
	}
	lock, err := builtinOperationLock(operation)
	if err != nil {
		return tc.ApprovalOperation{}, err
	}
	call.locks, err = unionLocks(append(call.locks, lock))
	if err != nil {
		return tc.ApprovalOperation{}, err
	}
	err = e.core.transaction(call.ctx, call, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		return e.core.input(ctx, tx, call, func(current context.Context, raw []byte) error {
			currentRecord, err := loadBuiltinRecord(current, x, b)
			if err != nil {
				return err
			}
			if currentRecord != nil {
				if currentRecord.ID != operation || currentRecord.Phase != "created" || currentRecord.Version != 1 || currentRecord.Attempt != (tc.AttemptID{}) {
					return fail(f.ConfirmationStale)
				}
				call.record = *currentRecord
				return nil
			}
			if old != nil {
				return fail(f.ConfirmationStale)
			}
			if err = e.core.validateBackendInTx(current, tx, call, raw); err != nil {
				return err
			}
			in, encoded, err := builtinInput(current, b, raw)
			if err != nil {
				return err
			}
			call.record = builtinRecord{ID: operation, Input: in, InputDigest: digest(encoded), Fingerprint: digest(encoded), Phase: "created", Version: 1}
			return insertMCPReservation(current, x, call.record)
		})
	})
	if err != nil {
		return tc.ApprovalOperation{}, err
	}
	return tc.ApprovalOperation{Binding: b, OperationID: call.record.ID, Fingerprint: call.record.Fingerprint}, nil
}

func insertMCPReservation(ctx context.Context, x postgres.SQLExecutor, r builtinRecord) error {
	if r.validate() != nil || r.Phase != "created" || directOperationKind(r.Input) != "mcp.call" {
		return fail(f.InvalidState)
	}
	raw, err := encode(r.Input)
	if err != nil {
		return err
	}
	b := r.Input
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.operations(operation_id,project_id,agent_id,execution_id,logical_call_id,invocation_id,call_id,tool_id,spec_revision,input_data,input_digest,skill_id,backend_key,fingerprint,phase,version,operation_kind) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULL,NULL,$12,'created',1,'mcp.call')`, r.ID.String(), b.Project.String(), b.Agent.String(), b.Execution.String(), b.LogicalCall.String(), b.Invocation.String(), b.Call, b.Spec.ToolID.String(), int64(b.Spec.SpecRevision), raw, string(r.InputDigest), string(r.Fingerprint))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	return nil
}

// Called inside the original input loan and complete lock plan. A prior
// approval reservation is reused exactly; a later invocation cannot create a
// second attempt or resurrect a denial or an uncertain running Operation.
func (e *BuiltinExecutor) startMCPAttempt(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, call *builtinCall, old *builtinRecord) error {
	if e == nil || e.mcp == nil || call == nil || call.binding.MCPBinding.Validate() != nil {
		return fail(f.Forbidden)
	}
	admission, ok := e.executions.(tc.MCPDispatchAuthority)
	if !ok || nilPort(admission) {
		return fail(f.DependencyUnbound)
	}
	if err := admission.RequireMCPAdmissionInTx(ctx, tx, call.binding, call.plan); err != nil {
		return err
	}
	if old == nil {
		return startBuiltinRecord(ctx, x, call.record, e.authority.process)
	}
	r := call.record
	if old.validate() != nil || old.Phase != "created" || old.Version != 1 || old.Attempt != (tc.AttemptID{}) || old.Request != "" || old.ID != r.ID || old.InputDigest != r.InputDigest || old.Fingerprint != r.Fingerprint || r.Attempt.Validate() != nil || !r.Request.Valid() {
		return fail(f.ConfirmationStale)
	}
	want, err := encode(r.Input)
	if err != nil {
		return err
	}
	prior, err := encode(old.Input)
	if err != nil || !bytes.Equal(want, prior) {
		return fail(f.Forbidden)
	}
	b := r.Input
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_tool.attempts(attempt_id,operation_id,execution_id,project_id,agent_id,ordinal,process_id,fence,backend_request_id,phase) VALUES($1,$2,$3,$4,$5,1,$6,1,$7,'running')`, r.Attempt.String(), r.ID.String(), b.Execution.String(), b.Project.String(), b.Agent.String(), e.authority.process.String(), string(r.Request))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.InvalidState)
	}
	tag, err = x.Exec(ctx, `UPDATE agenteam_tool.operations SET phase='running',version=2,active_attempt_id=$2 WHERE operation_id=$1 AND phase='created' AND version=1 AND active_attempt_id IS NULL`, r.ID.String(), r.Attempt.String())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.ConfirmationStale)
	}
	return nil
}

func (e *MCPExecutor) RequireApprovalOperationInTx(ctx context.Context, tx f.Tx, v tc.ApprovalOperation) error {
	if e == nil || e.core == nil || ctx == nil || v.Validate() != nil || v.Binding.MCPBinding.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	x, err := e.core.authority.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = e.core.authority.store.RequireHeldLocks(ctx, tx, v.RequiredLocks()); err != nil {
		return portError(err)
	}
	r, err := loadBuiltinRecord(ctx, x, v.Binding)
	if err != nil {
		return err
	}
	if r == nil || r.ID != v.OperationID || r.Fingerprint != v.Fingerprint || r.Phase != "created" && r.Phase != "running" {
		return fail(f.ConfirmationStale)
	}
	return ctx.Err()
}
func (e *MCPExecutor) RequireUnattemptedApprovalOperationInTx(ctx context.Context, tx f.Tx, v tc.ApprovalOperation) error {
	if err := e.RequireApprovalOperationInTx(ctx, tx, v); err != nil {
		return err
	}
	x, err := e.core.authority.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	var unattempted bool
	err = x.QueryRow(ctx, `SELECT o.phase='created' AND o.version=1 AND o.active_attempt_id IS NULL AND NOT EXISTS(SELECT 1 FROM agenteam_tool.attempts a WHERE a.operation_id=o.operation_id) FROM agenteam_tool.operations o WHERE o.operation_id=$1`, v.OperationID.String()).Scan(&unattempted)
	if err != nil {
		return portError(err)
	}
	if !unattempted {
		return fail(f.ConfirmationStale)
	}
	return ctx.Err()
}
func (e *MCPExecutor) RejectApprovalOperationInTx(ctx context.Context, tx f.Tx, v tc.ApprovalOperation, request string) error {
	if _, err := f.ParseID[struct{}](request); err != nil {
		return fail(f.InvalidArgument)
	}
	if err := e.RequireUnattemptedApprovalOperationInTx(ctx, tx, v); err != nil {
		return err
	}
	x, err := e.core.authority.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_tool.approval_denials(operation_id,request_id,fingerprint) VALUES($1,$2,$3)`, v.OperationID.String(), request, string(v.Fingerprint))
	if err != nil {
		return portError(err)
	}
	raw, _ := json.Marshal(struct {
		Kind      string `json:"kind"`
		Request   string `json:"request_id"`
		Available bool   `json:"report_available"`
	}{"approval_rejected", request, false})
	tag, err := x.Exec(ctx, `UPDATE agenteam_tool.operations SET phase='failed',version=2,outcome='error',completed_at=GREATEST(clock_timestamp(),created_at),terminal_data=$2 WHERE operation_id=$1 AND phase='created' AND version=1 AND active_attempt_id IS NULL`, v.OperationID.String(), raw)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fail(f.ConfirmationStale)
	}
	return ctx.Err()
}
func (e *MCPExecutor) RequireRejectedOperationInTx(ctx context.Context, tx f.Tx, v tc.ApprovalOperation, request string) error {
	if e == nil || e.core == nil || ctx == nil || v.Validate() != nil || v.Binding.MCPBinding.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	if _, err := f.ParseID[struct{}](request); err != nil {
		return fail(f.InvalidArgument)
	}
	x, err := e.core.authority.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = e.core.authority.store.RequireHeldLocks(ctx, tx, v.RequiredLocks()); err != nil {
		return portError(err)
	}
	r, err := loadBuiltinRecord(ctx, x, v.Binding)
	if err != nil {
		return err
	}
	if r == nil || r.ID != v.OperationID || r.Fingerprint != v.Fingerprint || r.Phase != "failed" || r.Version != 2 || r.Attempt != (tc.AttemptID{}) || r.Request != "" {
		return fail(f.ConfirmationStale)
	}
	var found, fingerprint string
	var raw []byte
	var count int64
	err = x.QueryRow(ctx, `SELECT d.request_id,d.fingerprint,o.terminal_data,(SELECT count(*) FROM agenteam_tool.attempts a WHERE a.operation_id=o.operation_id) FROM agenteam_tool.approval_denials d JOIN agenteam_tool.operations o ON o.operation_id=d.operation_id WHERE d.operation_id=$1`, r.ID.String()).Scan(&found, &fingerprint, &raw, &count)
	if err != nil {
		return portError(err)
	}
	expected, _ := json.Marshal(struct {
		Kind      string `json:"kind"`
		Request   string `json:"request_id"`
		Available bool   `json:"report_available"`
	}{"approval_rejected", request, false})
	if found != request || fingerprint != string(v.Fingerprint) || count != 0 || !bytes.Equal(raw, expected) {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}

var _ tc.ApprovalOperations = (*MCPExecutor)(nil)
var _ tc.MCPInvocationAuthority = (*MCPAuthority)(nil)
var _ tc.RunnerDispatcher = (*MCPExecutor)(nil)
var _ tc.RunnerResultAuthority = (*MCPExecutor)(nil)
var _ tc.RunnerCallLifecycle = (*MCPExecutor)(nil)
