package agent

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type ActivityAuthority interface {
	TouchActivityInTx(context.Context, f.Tx, i.Actor) error
}
type Dependencies struct {
	Authority        *Authority
	EventAuthority   *EventAuthority
	Events           oc.Appender
	Audit            ac.Appender
	Activity         ActivityAuthority
	Models           mc.AgentReferences
	Tools            c.ToolConfigurationDirectory
	ToolReferences   c.AgentToolReferences
	Mounts           c.MountConfiguration
	Secrets          vc.SecretDirectory
	SecretReferences vc.SecretReferences
	Skills           c.AgentSkillsInitializer
}
type Service struct{ state *serviceState }
type serviceState struct {
	store Store
	deps  Dependencies
	calls *callSet
}

// Construction performs no I/O and does not supply absent capabilities. All
// providers remain mandatory for the full F1 shape, including empty arrays and
// false defaults. The default application does not bind this service yet.
func New(store Store, deps Dependencies) (*Service, error) {
	if nilPort(store) || !reflect.TypeOf(store).Comparable() || deps.Authority == nil || deps.Authority.state == nil || deps.Authority.state.store != store || deps.EventAuthority == nil || deps.EventAuthority.owner == nil || deps.EventAuthority.owner.state != deps.Authority.state || !deps.EventAuthority.events.Valid() ||
		nilPort(deps.Events) || nilPort(deps.Audit) || nilPort(deps.Activity) || nilPort(deps.Models) || nilPort(deps.Tools) || nilPort(deps.ToolReferences) || nilPort(deps.Mounts) || nilPort(deps.Secrets) || nilPort(deps.SecretReferences) || nilPort(deps.Skills) {
		return nil, fault(f.DependencyUnbound)
	}
	return &Service{&serviceState{store, deps, newCalls()}}, nil
}
func (s *Service) begin(ctx context.Context) (context.Context, func(), error) {
	if s == nil || s.state == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	return s.state.calls.begin(ctx)
}
func (s *Service) Stop() {
	if s != nil && s.state != nil {
		s.state.calls.stop()
	}
}
func (s *Service) Drain(ctx context.Context) error {
	if s == nil || s.state == nil {
		return fault(f.DependencyUnbound)
	}
	return s.state.calls.drain(ctx)
}
func (s *Service) Joined() bool            { return s != nil && s.state != nil && s.state.calls.joined() }
func (Service) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_service") }
func (Service) LogValue() slog.Value       { return slog.StringValue("agent_service") }
