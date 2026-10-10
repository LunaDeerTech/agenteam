package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/projection"
)

// ExecutionCapture captures registered Builtin metadata and fixed references.
// It does not register missing Core tools, invoke a backend, seal a Snapshot or
// grant runtime permission. Its Authority is the actual preparing-call owner.
type ExecutionCapture struct{ data func() *executionCaptureState }
type executionCaptureState struct {
	registry *Registry
	owner    tc.ExecutionToolCaptureAuthority
}

func NewExecutionCapture(r *Registry, owner tc.ExecutionToolCaptureAuthority) (*ExecutionCapture, error) {
	if r.data() == nil || nilPort(owner) {
		return nil, fail(f.DependencyUnbound)
	}
	s := &executionCaptureState{registry: r, owner: owner}
	return &ExecutionCapture{data: func() *executionCaptureState { return s }}, nil
}
func (c *ExecutionCapture) state() *executionCaptureState {
	if c == nil || c.data == nil {
		return nil
	}
	return c.data()
}

type executionToolPlanData struct {
	issuer        *executionCaptureState
	request       tc.ExecutionToolCaptureRequest
	facts         tc.ExecutionToolDiscoveryFacts
	version       f.Version
	allowed, core []id.ToolID
	entries       []currentTool
	locks         []f.LockRequest
}
type executionToolPlan struct{ data func() executionToolPlanData }

func (p executionToolPlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return slices.Clone(p.data().locks)
}
func (executionToolPlan) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "execution_tool_plan") }
func (executionToolPlan) LogValue() slog.Value         { return slog.StringValue("execution_tool_plan") }
func (executionToolPlan) MarshalJSON() ([]byte, error) { return []byte(`"execution_tool_plan"`), nil }

func captureError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return portError(err)
}
func captureContext(ctx context.Context) error {
	if ctx == nil {
		return fail(f.InvalidArgument)
	}
	return captureError(ctx.Err())
}
func captureLocks(r tc.ExecutionToolCaptureRequest) ([]f.LockRequest, error) {
	return normalizedLocks(append(r.RequiredLocks(), RegistryLock(f.Shared)))
}

func (c *ExecutionCapture) DiscoverExecutionTools(ctx context.Context, request tc.ExecutionToolCaptureRequest) (tc.ExecutionToolPlan, error) {
	if err := captureContext(ctx); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	s := c.state()
	if s == nil {
		return nil, fail(f.DependencyUnbound)
	}
	locks, err := captureLocks(request)
	if err != nil {
		return nil, err
	}
	d := executionToolPlanData{issuer: s, request: request}
	var callbackErr error
	err = s.registry.read(ctx, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) (err error) {
		defer func() { callbackErr = err }()
		if _, err = s.registry.executor(ctx, tx, locks); err != nil {
			return err
		}
		facts, err := s.owner.RequireToolDiscoveryInTx(ctx, tx, request)
		if err != nil {
			return captureError(err)
		}
		if err = facts.Validate(request); err != nil {
			return err
		}
		d.facts = facts.Clone()
		d.version, d.allowed, err = loadCaptureAgentReferences(ctx, x, request)
		if err != nil {
			return err
		}
		d.core, err = loadCaptureCoreTools(ctx, x)
		if err != nil {
			return err
		}
		d.entries, err = s.registry.captureEntries(ctx, tx, x, d.allowed, d.core, d.facts.DeniedToolIDs)
		if err != nil {
			return err
		}
		return captureContext(ctx)
	})
	if err != nil {
		var unknown *UnknownError
		if errors.As(err, &unknown) {
			return nil, unknown
		}
		if callbackErr != nil {
			return nil, captureError(callbackErr)
		}
		return nil, captureError(err)
	}
	if err = captureContext(ctx); err != nil {
		return nil, err
	}
	for _, entry := range d.entries {
		locks = append(locks, toolLock(entry.Ref.ToolID, f.Shared))
	}
	d.locks, err = normalizedLocks(locks)
	if err != nil {
		return nil, err
	}
	return executionToolPlan{data: func() executionToolPlanData { return d }}, nil
}

func (r *Registry) captureEntries(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, allowed, core, denied []id.ToolID) ([]currentTool, error) {
	ids := sortedTools(append(slices.Clone(allowed), core...))
	ids = slices.Compact(ids)
	entries := make([]currentTool, 0, len(ids))
	for _, tool := range ids {
		if slices.Contains(denied, tool) {
			continue
		}
		entry, exists, err := r.currentCaptureTool(ctx, tx, x, tool)
		if err != nil {
			return nil, err
		}
		// Current registrations intersect the capability set. Unregistered
		// identities remain retained in Agent configuration but are not exposed.
		if !exists {
			continue
		}
		if entry.Class == tc.CoreTool && !slices.Contains(core, tool) {
			return nil, fail(f.InvalidState)
		}
		entries = append(entries, entry)
		if len(entries) > projection.MaxTools {
			return nil, fail(f.PayloadTooLarge)
		}
	}
	return entries, captureContext(ctx)
}

func (c *ExecutionCapture) ResolveExecutionToolsInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest, plan tc.ExecutionToolPlan) ([]tc.ExecutionTool, error) {
	if err := captureContext(ctx); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	s := c.state()
	if s == nil {
		return nil, fail(f.DependencyUnbound)
	}
	p, ok := plan.(executionToolPlan)
	if !ok || p.data == nil {
		return nil, fail(f.InvalidArgument)
	}
	d := p.data()
	if d.issuer != s || d.request != request {
		return nil, fail(f.InvalidArgument)
	}
	x, err := s.registry.executor(ctx, tx, d.locks)
	if err != nil {
		return nil, captureError(err)
	}
	facts, err := s.owner.RequireToolCaptureInTx(ctx, tx, request)
	if err != nil {
		return nil, captureError(err)
	}
	if err = facts.Validate(request); err != nil {
		return nil, err
	}
	if facts.AttemptBinding != d.facts.AttemptBinding || !reflect.DeepEqual(facts.Project, d.facts.Project) || facts.AgentVersion != d.version || !slices.Equal(facts.Selection.AllowedToolIDs, d.allowed) || !slices.Equal(facts.Selection.DeniedToolIDs, d.facts.DeniedToolIDs) {
		return nil, fail(f.VersionConflict)
	}
	version, allowed, err := loadCaptureAgentReferences(ctx, x, request)
	if err != nil {
		return nil, err
	}
	core, err := loadCaptureCoreTools(ctx, x)
	if err != nil {
		return nil, err
	}
	if version != d.version || !slices.Equal(allowed, d.allowed) || !slices.Equal(core, d.core) {
		return nil, fail(f.VersionConflict)
	}
	entries, err := s.registry.captureEntries(ctx, tx, x, allowed, core, d.facts.DeniedToolIDs)
	if err != nil {
		return nil, err
	}
	if len(entries) != len(d.entries) {
		return nil, fail(f.VersionConflict)
	}
	refs := make([]tc.SpecRef, 0, len(entries))
	byID := make(map[id.ToolID]currentTool, len(entries))
	for n, entry := range entries {
		if !sameCurrent(entry, d.entries[n]) {
			return nil, fail(f.VersionConflict)
		}
		refs = append(refs, entry.Ref)
		byID[entry.Ref.ToolID] = entry
	}
	names, err := projection.BuildNameTable(ctx, refs)
	if err != nil {
		return nil, captureError(err)
	}
	mappings, err := names.Entries(ctx)
	if err != nil {
		return nil, captureError(err)
	}
	tools := make([]tc.ExecutionTool, 0, len(mappings))
	for _, mapping := range mappings {
		entry := byID[mapping.Reference.ToolID]
		v := tc.ExecutionTool{ToolID: entry.Ref.ToolID, SpecRevision: entry.Ref.SpecRevision, ModelVisibleName: mapping.ModelVisibleName, BindingSnapshot: tc.BuiltinExecutionBinding{Binding: entry.Binding, ScopeResolverID: entry.Scope, RiskClassifierID: entry.Risk, Class: entry.Class}}
		if err = v.Validate(); err != nil {
			return nil, fail(f.InvalidState)
		}
		tools = append(tools, v)
	}
	if err = captureContext(ctx); err != nil {
		return nil, err
	}
	if err = writeExecutionToolReferences(ctx, x, request, facts, tools); err != nil {
		return nil, err
	}
	if err = captureContext(ctx); err != nil {
		return nil, err
	}
	return slices.Clone(tools), nil
}

var _ tc.ExecutionTools = (*ExecutionCapture)(nil)
