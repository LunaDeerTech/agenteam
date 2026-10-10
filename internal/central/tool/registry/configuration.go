package registry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

// Configuration implements Agent's lower-layer consumer-owned contracts. It
// never makes Agent import ToolSpec. The owner must be Agent's real canonical
// writer authority; even an explicit empty reference set requires that port.
type Configuration struct{ data func() *configurationState }
type configurationState struct {
	registry      *Registry
	owner         ac.ToolReferenceOwnerAuthority
	installSource string
}

func NewConfiguration(r *Registry, owner ac.ToolReferenceOwnerAuthority, installSource string) (*Configuration, error) {
	if r.data() == nil || nilPort(owner) || len(r.data().sources) == 0 || nilPort(r.data().auth.Sessions) || nilPort(r.data().auth.Projects) {
		return nil, fail(f.DependencyUnbound)
	}
	if installSource != "" {
		if !tc.ValidBuiltinKey(installSource) {
			return nil, fail(f.InvalidArgument)
		}
		if _, ok := r.data().sources[installSource]; !ok {
			return nil, fail(f.DependencyUnbound)
		}
	}
	s := &configurationState{registry: r, owner: owner, installSource: installSource}
	return &Configuration{data: func() *configurationState { return s }}, nil
}
func (c *Configuration) state() *configurationState {
	if c == nil || c.data == nil {
		return nil
	}
	return c.data()
}

type currentTool struct {
	Ref        tc.SpecRef
	Key        string
	Definition []byte
	Binding    tc.BuiltinBinding
	Scope      tc.ScopeResolverID
	Risk       tc.RiskClassifierID
	Class      tc.ToolClass
}

func (r *Registry) currentTool(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, tool id.ToolID) (currentTool, error) {
	var v currentTool
	var revision int64
	var contract int64
	var raw []byte
	err := x.QueryRow(ctx, `SELECT i.stable_key,r.spec_revision,s.definition,r.handler_id,r.contract_revision,r.scope_resolver_id,r.risk_classifier_id,r.class FROM agenteam_tool.identities i JOIN agenteam_tool.registrations r ON r.tool_id=i.tool_id AND r.spec_revision=i.latest_revision JOIN agenteam_tool.spec_revisions s ON s.tool_id=r.tool_id AND s.spec_revision=r.spec_revision WHERE i.tool_id=$1`, tool.String()).Scan(&v.Key, &revision, &raw, &v.Binding.HandlerID, &contract, &v.Scope, &v.Risk, &v.Class)
	if errors.Is(err, pgx.ErrNoRows) {
		return currentTool{}, fail(f.NotFound)
	}
	if err != nil {
		return currentTool{}, portError(err)
	}
	v.Ref = tc.SpecRef{ToolID: tool, SpecRevision: f.Version(revision)}
	v.Binding.ContractRevision = f.Version(contract)
	if v.Ref.Validate() != nil {
		return currentTool{}, fail(f.InvalidState)
	}
	v.Definition, err = decodeDefinition(ctx, raw, v.Key)
	if err != nil {
		return currentTool{}, err
	}
	source, canonical, err := r.source(ctx, tx, v.Key)
	if err != nil {
		return currentTool{}, err
	}
	if !source.Active {
		return currentTool{}, fail(f.NotFound)
	}
	if !bytes.Equal(canonical, v.Definition) || source.Binding != v.Binding || source.ScopeResolverID != v.Scope || source.RiskClassifierID != v.Risk || source.Class != v.Class {
		return currentTool{}, fail(f.InvalidState)
	}
	if v.Class != tc.OrdinaryTool {
		return currentTool{}, fail(f.Forbidden)
	}
	return v, nil
}
func sameCurrent(a, b currentTool) bool {
	return a.Ref == b.Ref && a.Key == b.Key && bytes.Equal(a.Definition, b.Definition) && a.Binding == b.Binding && a.Scope == b.Scope && a.Risk == b.Risk && a.Class == b.Class
}
func baseLocks(actor id.Actor, project id.ProjectID, agent id.AgentID, agentMode f.LockMode) []f.LockRequest {
	u, _ := f.UserLock(actor.Details().UserID)
	p, _ := f.ProjectLock(project.String())
	a, _ := f.AgentLock(agent.String())
	return []f.LockRequest{RegistryLock(f.Shared), {Key: u, Mode: f.Shared}, {Key: p, Mode: f.Shared}, {Key: a, Mode: agentMode}}
}
func normalizedLocks(locks []f.LockRequest) ([]f.LockRequest, error) {
	out := slices.Clone(locks)
	for _, v := range out {
		if v.Key.Validate() != nil || !v.Mode.Valid() {
			return nil, fail(f.InvalidArgument)
		}
	}
	slices.SortFunc(out, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	n := 0
	for _, v := range out {
		if n > 0 && f.CompareLockKeys(out[n-1].Key, v.Key) == 0 {
			if v.Mode == f.Exclusive {
				out[n-1].Mode = f.Exclusive
			}
			continue
		}
		out[n] = v
		n++
	}
	return out[:n], nil
}

type directoryPlanData struct {
	issuer   *configurationState
	request  ac.ToolConfigurationRequest
	resolved []id.ToolID
	locks    []f.LockRequest
	entries  []currentTool
}
type directoryPlan struct{ data func() directoryPlanData }

func (p directoryPlan) RequiredLocks() []f.LockRequest {
	if p.data == nil {
		return nil
	}
	return slices.Clone(p.data().locks)
}
func (p directoryPlan) ResolvedToolIDs() []id.ToolID {
	if p.data == nil {
		return nil
	}
	return slices.Clone(p.data().resolved)
}
func (directoryPlan) Format(s fmt.State, _ rune)   { _, _ = io.WriteString(s, "tool_configuration_plan") }
func (directoryPlan) MarshalJSON() ([]byte, error) { return []byte(`"tool_configuration_plan"`), nil }
func (directoryPlan) LogValue() slog.Value         { return slog.StringValue("tool_configuration_plan") }
func sameRequest(a, b ac.ToolConfigurationRequest) bool {
	return a.Actor.Details() == b.Actor.Details() && a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.Command.Canonical() == b.Command.Canonical() && slices.Equal(a.RequestedToolIDs, b.RequestedToolIDs) && (a.InstallSkillEnabled == nil && b.InstallSkillEnabled == nil || a.InstallSkillEnabled != nil && b.InstallSkillEnabled != nil && *a.InstallSkillEnabled == *b.InstallSkillEnabled)
}
func (c *Configuration) DiscoverConfigurationTools(ctx context.Context, request ac.ToolConfigurationRequest) (ac.ToolConfigurationPlan, error) {
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	s := c.state()
	if s == nil {
		return nil, fail(f.DependencyUnbound)
	}
	request = request.Clone()
	locks, err := normalizedLocks(baseLocks(request.Actor, request.ProjectID, request.AgentID, f.Shared))
	if err != nil {
		return nil, err
	}
	var entries []currentTool
	var defaultTool *currentTool
	resolved := slices.Clone(request.RequestedToolIDs)
	err = s.registry.read(ctx, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := s.registry.currentProject(ctx, tx, request.Actor, request.ProjectID); err != nil {
			return err
		}
		if request.InstallSkillEnabled != nil {
			// False is not permission to skip the required directory. Resolving the
			// real ID is also necessary to reject an explicit contradictory ID.
			if s.installSource == "" {
				return fail(f.DependencyUnbound)
			}
			definition, _, err := s.registry.source(ctx, tx, s.installSource)
			if err != nil {
				return err
			}
			if !definition.Active {
				return fail(f.DependencyUnbound)
			}
			row, err := loadIdentity(ctx, x, s.installSource)
			if err != nil {
				return err
			}
			if row == nil {
				return fail(f.DependencyUnbound)
			}
			current, err := s.registry.currentTool(ctx, tx, x, row.ID)
			if err != nil {
				return err
			}
			defaultTool = &current
			present := slices.Contains(resolved, row.ID)
			if !*request.InstallSkillEnabled && present {
				return fail(f.InvalidArgument)
			}
			if *request.InstallSkillEnabled && !present {
				resolved = append(resolved, row.ID)
			}
		}
		if len(resolved) > 128 {
			return fail(f.PayloadTooLarge)
		}
		slices.SortFunc(resolved, func(a, b id.ToolID) int { return bytes.Compare([]byte(a.String()), []byte(b.String())) })
		for _, tool := range resolved {
			v, err := s.registry.currentTool(ctx, tx, x, tool)
			if err != nil {
				return err
			}
			entries = append(entries, v)
		}
		if defaultTool != nil && !slices.Contains(resolved, defaultTool.Ref.ToolID) {
			entries = append(entries, *defaultTool)
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	locks = baseLocks(request.Actor, request.ProjectID, request.AgentID, f.Exclusive)
	for _, entry := range entries {
		locks = append(locks, toolLock(entry.Ref.ToolID, f.Shared))
	}
	locks, err = normalizedLocks(locks)
	if err != nil {
		return nil, err
	}
	d := directoryPlanData{issuer: s, request: request, resolved: resolved, locks: locks, entries: entries}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	return directoryPlan{data: func() directoryPlanData { return d }}, nil
}
func (c *Configuration) RequireConfigurationToolsInTx(ctx context.Context, tx f.Tx, request ac.ToolConfigurationRequest, plan ac.ToolConfigurationPlan) error {
	if err := request.Validate(); err != nil {
		return err
	}
	s := c.state()
	if s == nil {
		return fail(f.DependencyUnbound)
	}
	p, ok := plan.(directoryPlan)
	if !ok || p.data == nil {
		return fail(f.InvalidArgument)
	}
	d := p.data()
	if d.issuer != s || !sameRequest(d.request, request) {
		return fail(f.InvalidArgument)
	}
	x, err := s.registry.executor(ctx, tx, d.locks)
	if err != nil {
		return err
	}
	if err = s.registry.currentProject(ctx, tx, request.Actor, request.ProjectID); err != nil {
		return err
	}
	for _, old := range d.entries {
		now, err := s.registry.currentTool(ctx, tx, x, old.Ref.ToolID)
		if err != nil {
			return err
		}
		if !sameCurrent(old, now) {
			return fail(f.VersionConflict)
		}
	}
	return portError(ctx.Err())
}

type UnknownError struct {
	cause   f.TransactionCause
	attempt f.ID[f.TransactionAttempt]
}

func (e *UnknownError) Error() string                         { return string(f.CommitUnknown) }
func (e *UnknownError) Unwrap() error                         { return f.NewFault(f.CommitUnknown, f.Unknown) }
func (e *UnknownError) Cause() f.TransactionCause             { return e.cause }
func (e *UnknownError) AttemptID() f.ID[f.TransactionAttempt] { return e.attempt }
func (*UnknownError) Format(s fmt.State, _ rune)              { _, _ = io.WriteString(s, "COMMIT_UNKNOWN") }
func (*UnknownError) MarshalJSON() ([]byte, error) {
	return []byte(`{"code":"COMMIT_UNKNOWN","commit_state":"unknown"}`), nil
}
func (*UnknownError) LogValue() slog.Value { return slog.StringValue("COMMIT_UNKNOWN") }
func (r *Registry) read(ctx context.Context, locks []f.LockRequest, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	nonce, err := f.NewID[struct{}]()
	if err != nil {
		return portError(err)
	}
	cause, err := f.NewRecoveryCause("tool.registry-read", nonce.String(), "")
	if err != nil {
		return portError(err)
	}
	s := r.data()
	if s == nil {
		return fail(f.DependencyUnbound)
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, tx, x)
	})
	switch result.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		return result.Fault()
	default:
		return &UnknownError{cause: result.Cause(), attempt: result.AttemptID()}
	}
}
