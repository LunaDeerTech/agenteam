package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

// InstallSource fixes one actual Skill Service and its adapter. Active means
// this backend and the compiled scope/risk functions are bound; it never means
// that a particular Execution or ToolCall is authorized. No Registry or
// authorization callback is used while checking this composition.
type InstallSource struct{ data func() *installSourceState }
type installSourceState struct {
	authority *InstallAuthority
	service   *skill.Service
	adapter   *builtin.SkillInstallAdapter
}

var _ registry.BuiltinSource = (*InstallSource)(nil)

// NewInstallSource can be composed before Registry. It does not publish an
// Active registration: Describe/CheckBinding verify the original caller Tx,
// the exact Skill producer and the guard's actual bound process each time.
func NewInstallSource(authority *InstallAuthority, service *skill.Service) (*InstallSource, error) {
	if authority == nil || authority.data == nil || authority.data() == nil || service == nil {
		return nil, fail(f.DependencyUnbound)
	}
	a := authority.data()
	if nilPort(a.store) || a.guard == nil || a.process.Validate() != nil {
		return nil, fail(f.DependencyUnbound)
	}
	adapter, err := builtin.NewSkillInstallAdapter(service, authority)
	if err != nil {
		return nil, err
	}
	d := &installSourceState{authority: authority, service: service, adapter: adapter}
	return &InstallSource{data: func() *installSourceState { return d }}, nil
}

func sameInstallInstance(a, b any) bool {
	if nilPort(a) || nilPort(b) {
		return false
	}
	x, y := reflect.ValueOf(a), reflect.ValueOf(b)
	return x.Type() == y.Type() && x.Comparable() && y.Comparable() && x.Interface() == y.Interface()
}

func installRegistration() tc.BuiltinRegistration {
	return tc.BuiltinRegistration{
		Definition:      builtin.SkillInstallDefinition(),
		Binding:         tc.BuiltinBinding{HandlerID: builtin.SkillInstallHandlerID, ContractRevision: 1},
		ScopeResolverID: builtin.SkillInstallScopeID, RiskClassifierID: builtin.SkillInstallRiskID,
		Class: tc.OrdinaryTool, Active: true,
	}
}

func (s *InstallSource) requireBinding(ctx context.Context, tx f.Tx) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.data == nil || s.data() == nil {
		return fail(f.DependencyUnbound)
	}
	d := s.data()
	if d.authority == nil || d.authority.data == nil || d.service == nil || d.adapter == nil {
		return fail(f.DependencyUnbound)
	}
	a := d.authority.data()
	if _, err := a.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{registry.RegistryLock(f.Shared)}); err != nil {
		return portError(err)
	}
	a.mu.Lock()
	stopped := a.stopped
	a.mu.Unlock()
	if stopped {
		return fail(f.ShuttingDown)
	}
	process, err := a.guard.CurrentProcess()
	if err != nil || process != a.process {
		return fail(f.DependencyUnavailable)
	}
	if err = d.service.RequireInstallBindingInTx(ctx, tx, d.authority, a.process); err != nil {
		return portError(err)
	}
	return contextError(ctx)
}

func (s *InstallSource) DescribeInTx(ctx context.Context, tx f.Tx) (tc.BuiltinRegistration, error) {
	if err := s.requireBinding(ctx, tx); err != nil {
		return tc.BuiltinRegistration{}, err
	}
	return installRegistration(), nil
}

func (s *InstallSource) CheckBindingInTx(ctx context.Context, tx f.Tx, got tc.BuiltinRegistration) error {
	if err := s.requireBinding(ctx, tx); err != nil {
		return err
	}
	return exactInstallRegistration(ctx, got)
}

func exactInstallRegistration(ctx context.Context, got tc.BuiltinRegistration) error {
	want := installRegistration()
	if !got.Active || got.Binding != want.Binding || got.ScopeResolverID != want.ScopeResolverID || got.RiskClassifierID != want.RiskClassifierID || got.Class != want.Class {
		return fail(f.InvalidState)
	}
	actual, err := tc.CanonicalDefinition(ctx, got.Definition)
	if err != nil {
		return err
	}
	expected, err := tc.CanonicalDefinition(ctx, want.Definition)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return fail(f.InvalidState)
	}
	return contextError(ctx)
}

func (InstallSource) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_install_source") }
func (InstallSource) MarshalJSON() ([]byte, error) { return []byte(`"skill_install_source"`), nil }
func (*InstallSource) UnmarshalJSON([]byte) error  { return fail(f.InvalidArgument) }
func (InstallSource) LogValue() slog.Value         { return slog.StringValue("skill_install_source") }
