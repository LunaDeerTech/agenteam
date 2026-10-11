package app

import (
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

// These are immutable capabilities, constructed before Audit, Secret, Usage
// and Outbox capture their consumers. No callback looks up a later binding.
type executionRuntimeAuthorities struct {
	pending       *scheduler.PendingAuthority
	execution     *execution.Authority
	agents        *agent.Authority
	model         *model.RuntimeAuthority
	events        *execution.DirectTextEventAuthority
	secretService i.ServiceRegistration
}

func createExecutionRuntimeAuthorities(db database, projects *project.Authority, guard *object.ProcessGuard) (*executionRuntimeAuthorities, error) {
	store, ok := db.(execution.Store)
	if !ok || runtimeInformationNil(store) || projects == nil || guard == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	b := &executionRuntimeAuthorities{}
	var err error
	if b.pending, err = scheduler.NewPendingAuthority(store); err != nil {
		return nil, err
	}
	access, err := project.NewSchedulerExecutionAccess(projects, b.pending)
	if err != nil {
		return nil, err
	}
	if b.execution, err = execution.NewAuthority(store, projects, access); err != nil {
		return nil, err
	}
	if b.agents, err = agent.NewAuthority(store, projects); err != nil {
		return nil, err
	}
	modelService, err := i.RegisterService(i.ModelRuntime)
	if err != nil {
		return nil, err
	}
	if b.secretService, err = i.RegisterService(i.SecretService); err != nil {
		return nil, err
	}
	outboundService, err := i.RegisterService(i.OutboundService)
	if err != nil {
		return nil, err
	}
	if b.model, err = model.NewRuntimeAuthority(store, model.RuntimeAuthorizations{
		Consumers: b.execution, Process: guard, ModelRuntime: modelService,
		SecretService: b.secretService, OutboundService: outboundService,
	}); err != nil {
		return nil, err
	}
	if b.events, err = execution.NewDirectTextEventAuthority(b.execution); err != nil {
		return nil, err
	}
	return b, nil
}

type agentRuntimeProviders struct {
	configuration ac.ExecutionConfiguration
	skills        sc.InitialBindingsProvider
	roundSkills   sc.InitialRoundBindingsReader
	tools         tc.ExecutionTools
}

// This profile reads the original Agent, Skill and Registry state. In
// particular it does not install a replacement builtin, manufacture an empty
// directory or add deny IDs. Registry still rejects every selected registered
// builtin whose actual source is unbound. The persisted execution policy alone
// may exclude it; DirectText independently rejects a nonempty captured set.
func createAgentRuntimeProviders(db database, projects *project.Authority, accounts *account.Authority, skills *skill.Authority, a *executionRuntimeAuthorities) (agentRuntimeProviders, error) {
	var out agentRuntimeProviders
	store, ok := db.(registry.Store)
	if !ok || runtimeInformationNil(store) || projects == nil || accounts == nil || skills == nil || a == nil {
		return out, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	var err error
	if out.configuration, err = agent.NewExecutionConfiguration(a.agents, a.execution); err != nil {
		return out, err
	}
	if out.skills, err = skill.NewExecutionBindings(skills, a.execution); err != nil {
		return out, err
	}
	if out.roundSkills, err = skill.NewInitialRoundBindings(skills); err != nil {
		return out, err
	}
	registered, err := registry.New(store, registry.Options{Sources: map[string]registry.BuiltinSource{}, Authorizations: registry.Authorizations{Sessions: accounts, Projects: projects}})
	if err != nil {
		return out, err
	}
	if out.tools, err = registry.NewExecutionCapture(registered, a.execution); err != nil {
		return out, err
	}
	return out, nil
}
