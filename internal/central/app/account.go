package app

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	agentc "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	audithttp "github.com/LunaDeerTech/agenteam/internal/central/audit/http"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objectcontract "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	outboundhttp "github.com/LunaDeerTech/agenteam/internal/central/outbound/http"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/runtimeinfo"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type accountRuntime interface {
	Start(context.Context) error
	Check(context.Context) error
	StopAdmission()
	Drain(context.Context) error
	Force(context.Context) error
	Joined() bool
}

type accountStorage interface {
	accountRuntime
	Handler() http.Handler
}

type accountWork interface {
	StopAdmission()
	Drain(context.Context) error
	Force(context.Context) error
	Joined() bool
}

// accountAssembly owns the entire partial construction, including an opened
// Sink before a core or either Runtime exists. Its fields are lifecycle owners,
// never mutable domain capabilities consulted by an authorization provider.
type accountAssembly struct {
	mu              sync.Mutex
	constructing    bool
	stopped         bool
	started         bool
	forced          context.Context
	executions      *executionRuntimeAssembly
	usageCalls      *usageCallAssembly
	planning        accountWork
	agentDirectory  accountWork
	agentInstall    accountWork
	agents          *agentConfigurationAssembly
	variableCalls   *projectVariableCallAssembly
	variables       accountWork
	secretVariables accountWork
	projects        accountWork
	skills          accountWork
	knowledge       accountWork
	runners         accountWork
	sink            accountWork
	core            accountWork
	runtime         accountRuntime
	mail            accountRuntime
	handler         http.Handler
}

func (a *accountAssembly) install(ctx context.Context, f func()) bool {
	a.mu.Lock()
	f()
	stopped, forced := a.stopped, a.forced
	a.mu.Unlock()
	if stopped || ctx.Err() != nil {
		a.StopAdmission()
	}
	if forced != nil {
		_ = a.Force(forced)
	}
	return !stopped && forced == nil && ctx.Err() == nil
}

func (a *accountAssembly) constructionDone() {
	a.mu.Lock()
	a.constructing = false
	a.mu.Unlock()
}

func (a *accountAssembly) Start(ctx context.Context) error {
	a.mu.Lock()
	runtime, mail, executions, agents, stopped := a.runtime, a.mail, a.executions, a.agents, a.stopped
	a.mu.Unlock()
	if runtime == nil || mail == nil || stopped {
		return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	if agents != nil {
		if err := agents.Initialize(ctx); err != nil {
			return err
		}
	}
	if err := runtime.Start(ctx); err != nil {
		return err
	}
	if err := mail.Start(ctx); err != nil {
		return err
	}
	if executions != nil {
		if err := executions.Start(ctx); err != nil {
			return err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || ctx.Err() != nil {
		return foundation.NewFault(foundation.ShuttingDown, foundation.NotStarted)
	}
	a.started = true
	return nil
}

func (a *accountAssembly) Check(ctx context.Context) error {
	a.mu.Lock()
	runtime, mail, executions, available := a.runtime, a.mail, a.executions, a.started && !a.stopped
	a.mu.Unlock()
	if !available || runtime == nil || mail == nil {
		return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	// Both concrete checks consume this same health round. Neither probes SMTP.
	if err := runtime.Check(ctx); err != nil {
		return err
	}
	if err := mail.Check(ctx); err != nil {
		return err
	}
	if executions != nil {
		return executions.Check(ctx)
	}
	return nil
}

func (a *accountAssembly) Handler() http.Handler {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.started || a.stopped {
		return nil
	}
	return a.handler
}

func (a *accountAssembly) works() []accountWork {
	a.mu.Lock()
	defer a.mu.Unlock()
	var work []accountWork
	// Runtime joins its Scheduler, drivers, Model and wire owners before any
	// shared Work/Skill/Account provider or the root ProcessGuard may retire.
	if a.executions != nil {
		work = append(work, a.executions)
	}
	// Model actual return precedes Usage technical closure; Usage also owns
	// direct HTTP reads through this same pair. Neither may release providers early.
	if a.usageCalls != nil {
		work = append(work, a.usageCalls)
	}
	// Runner owns hijacked sockets and generation/Audit transactions; HTTP
	// shutdown alone cannot retire them. Account and DB must remain until join.
	if a.runners != nil {
		work = append(work, a.runners)
	}
	// Variables, Work and Project calls release Account Activity before core retires.
	if a.variableCalls != nil {
		work = append(work, a.variableCalls)
	} else {
		// Preserve independently constructed legacy views. The default pair
		// owns both lanes once, even though both views remain available.
		if a.variables != nil {
			work = append(work, a.variables)
		}
		if a.secretVariables != nil {
			work = append(work, a.secretVariables)
		}
	}
	if a.planning != nil {
		work = append(work, a.planning)
	}
	if a.agentDirectory != nil {
		// Retained for independently constructed legacy readers. The default
		// App's owned directory belongs only to a.agents below.
		work = append(work, a.agentDirectory)
	}
	if a.agents != nil {
		work = append(work, a.agents)
	}
	if a.agentInstall != nil {
		work = append(work, a.agentInstall)
	}
	if a.projects != nil {
		work = append(work, a.projects)
	}
	// Project creation is a caller of Skills. Both content domains must retire
	// their original Object/Store work before Account and the shared guard.
	if a.skills != nil {
		work = append(work, a.skills)
	}
	if a.knowledge != nil {
		work = append(work, a.knowledge)
	}
	// Mail finishes its durable completion through core after protocol I/O.
	// Retiring core first would reject that last cleanup transaction.
	if a.mail != nil {
		work = append(work, a.mail)
	}
	if a.runtime != nil {
		work = append(work, a.runtime)
	} else if a.core != nil {
		work = append(work, a.core)
	}
	if a.runtime == nil && a.core == nil && a.mail == nil && a.sink != nil {
		work = append(work, a.sink)
	}
	return work
}

func (a *accountAssembly) StopAdmission() {
	a.mu.Lock()
	a.stopped = true
	a.mu.Unlock()
	for _, work := range a.works() {
		work.StopAdmission()
	}
}

func (a *accountAssembly) Drain(ctx context.Context) error {
	a.StopAdmission()
	// Stop every admission before waiting, then honor the provider dependency.
	// All waits consume this same remaining shutdown context. An unjoined Mail
	// must leave core's cleanup admission open until the later Force phase.
	for _, work := range a.works() {
		if err := work.Drain(ctx); err != nil {
			return err
		}
		if !work.Joined() {
			return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
		}
	}
	if !a.Joined() {
		return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	return nil
}

func (a *accountAssembly) Force(ctx context.Context) error {
	a.mu.Lock()
	if a.forced == nil {
		a.forced = ctx
	}
	original := a.forced
	agents, install := a.agents, a.agentInstall
	a.mu.Unlock()
	a.StopAdmission()
	var result error
	// Mail gets its final completion opportunity before core retirement. Every
	// force uses the original remaining budget; an unjoined Execution bundle
	// additionally prevents shared-provider retirement below.
	for _, work := range a.works() {
		result = errors.Join(result, work.Force(original))
		// Inspect the actual work entry so a late-installed pair is also gated.
		variableCalls, ownsVariables := work.(*projectVariableCallAssembly)
		projectCalls, ownsProjects := work.(*projectCommandWork)
		planning, ownsPlanning := work.(*workPlanningAssembly)
		executions, ownsExecutions := work.(*executionRuntimeAssembly)
		usage, ownsUsage := work.(*usageCallAssembly)
		// Runtime terminal/lease cleanup and Agent/Variable/Work calls use the
		// shared providers. Expiring a force budget does not make them disposable.
		// The root retains its separate bounded Store.ForceClose policy.
		if ownsUsage && !usage.Joined() || ownsExecutions && !executions.Joined() || agents != nil && work == agents && !agents.Joined() || install != nil && work == install && !install.Joined() || ownsVariables && !variableCalls.Joined() || ownsProjects && !projectCalls.Joined() || ownsPlanning && planning.ownership != nil && !planning.Joined() {
			return errors.Join(result, foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted))
		}
	}
	return result
}

func (a *accountAssembly) Joined() bool {
	a.mu.Lock()
	constructing, stopped, sink := a.constructing, a.stopped, a.sink
	a.mu.Unlock()
	if constructing || !stopped || sink != nil && !sink.Joined() {
		return false
	}
	for _, work := range a.works() {
		if !work.Joined() {
			return false
		}
	}
	return true
}

// Each adapter holds the actual fixed ID and guard. It cannot call a locator
// that is filled later or infer process death from an empty local registry.
type accountProcessAuthority struct {
	process c.ProcessID
	guard   *object.ProcessGuard
}

func (a accountProcessAuthority) CurrentProcess() c.ProcessID { return a.process }
func (a accountProcessAuthority) ConfirmStopped(ctx context.Context, id c.ProcessID) error {
	original, err := foundation.ParseID[objectcontract.Process](id.String())
	if err != nil {
		return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	return a.guard.ConfirmStopped(ctx, original)
}

// bindAccounts fixes the original provider graph before domain initialization.
// Execution's owned graph is constructed once after the original Object guard
// has bound. Its callback captures these same providers, never a later locator.
func bindAccounts(ctx context.Context, cfg config.Config, db database, owned *resources, deps *dependencies) error {
	store, ok := db.(account.Store)
	if !ok {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	runtimeStore, ok := db.(runtimeinfo.Store)
	if !ok || runtimeInformationNil(runtimeStore) {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	authority, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		return err
	}
	_, runtimeEnabled := cfg.ExecutionRuntime()
	lifecycle, err := createProjectLifecycleAuthority(db)
	if err != nil {
		return err
	}
	projectUsage, err := createProjectUsageAuthorityWithLifecycle(db, authority, lifecycle)
	if err != nil {
		return err
	}
	agentAuthority, err := createAgentConfigurationAuthority(db, projectUsage.projects)
	if err != nil {
		return err
	}
	workAuthority, err := createWorkPlanningAuthority(db, projectUsage.projects)
	if err != nil {
		return err
	}
	runnerAuthority, err := createRunnerAuthority(db, authority)
	if err != nil {
		return err
	}
	modelStore, ok := db.(model.Store)
	if !ok {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	accounts := &accountAssembly{constructing: true}
	if !owned.addAccounts(ctx, accounts) {
		accounts.constructionDone()
		return context.Canceled
	}
	defer accounts.constructionDone()
	objects, err := openObjectAssembly(ctx, cfg, owned)
	if err != nil {
		return err
	}
	install, err := createAgentInstall(db, objects)
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.agentInstall = install }) {
		return context.Canceled
	}
	contentAuthorities, err := createKnowledgeSkillAuthoritiesWithInstall(db, projectUsage, install.authority)
	if err != nil {
		return err
	}
	var executionAuthorities *executionRuntimeAuthorities
	modelAuth := model.Authorizations{Sessions: authority, System: authority, Projects: projectUsage.projects}
	if runtimeEnabled {
		executionAuthorities, err = createExecutionRuntimeAuthorities(db, projectUsage.projects, objects.guard, agentAuthority)
		if err != nil {
			return err
		}
		modelAuth.Resolution = &model.ResolutionAuthorizations{Consumers: executionAuthorities.execution, SecretService: executionAuthorities.secretService}
		facts, e := skill.NewInitializationAuditFacts(contentAuthorities.skills, projectUsage.objectFacts)
		if e != nil {
			return e
		}
		contentAuthorities.audit, err = project.NewRuntimeInitializationAuditAuthority(projectUsage.projects, facts, executionAuthorities.model)
		if err != nil {
			return err
		}
	}
	var invocationFacts uc.InvocationFacts
	if executionAuthorities != nil {
		invocationFacts = executionAuthorities.model
	}
	boundUsage, bindErr := bindOwnedProjectUsage(cfg, db, authority, projectUsage, invocationFacts, objects)
	if boundUsage != nil && boundUsage.usageCalls != nil {
		projectUsage = boundUsage
		if !accounts.install(ctx, func() { accounts.usageCalls = boundUsage.usageCalls }) {
			return context.Canceled
		}
	}
	if bindErr != nil {
		return bindErr
	}
	modelAuthority, err := model.NewAuthority(modelStore, modelAuth)
	if err != nil {
		return err
	}
	var usage *model.SecretUsageRouter
	if runtimeEnabled {
		usage, err = model.NewRuntimeSecretUsageRouter(modelAuthority, executionAuthorities.model, authority)
	} else {
		usage, err = model.NewSecretUsageRouter(modelAuthority, authority)
	}
	if err != nil {
		return err
	}
	process, err := foundation.ParseID[c.Process](objects.process.String())
	if err != nil {
		return err
	}
	processes := accountProcessAuthority{process: process, guard: objects.guard}
	auditor, err := createSecurityWithRunners(cfg, db, authority, modelAuthority, contentAuthorities.audit, runnerAuthority)
	if err != nil {
		return err
	}
	runners, err := createRunnerControl(runnerAuthority, auditor)
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.runners = runners }) {
		return context.Canceled
	}
	secretWrites, err := createProjectSecretWriteAuthority(db, projectUsage.projects)
	if err != nil {
		return err
	}
	secrets, err := createSecret(cfg, db, auditor, authority, usage, projectUsage.projects, secretWrites)
	if err != nil {
		return err
	}
	transport, err := createOutbound(cfg, db, auditor, authority)
	if err != nil {
		return err
	}
	if deps.outboundConstruct != nil {
		if err = deps.outboundConstruct(ctx, cfg, transport); err != nil {
			owned.addOutbound(ctx, transport)
			return err
		}
	}
	if !owned.addOutbound(ctx, transport) {
		return context.Canceled
	}
	sink, err := recoverylog.Open(cfg.AccountRecoveryLog())
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.sink = sink }) {
		return context.Canceled
	}
	catalog := event.NewCatalog()
	revoked, err := c.DefineSessionsRevoked(catalog)
	if err != nil {
		return err
	}
	delivery, err := c.DefineDeliveryRequested(catalog)
	if err != nil {
		return err
	}
	modelEvents, err := model.DefineEvents(catalog)
	if err != nil {
		return err
	}
	projectEvents, err := pc.RegisterProjectEvents(catalog)
	if err != nil {
		return err
	}
	workEvents, err := defineWorkPlanningEvents(catalog)
	if err != nil {
		return err
	}
	journalStore, ok := db.(outbox.Store)
	if !ok {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	variableEvents, err := vc.RegisterVariableEvents(catalog)
	if err != nil {
		return err
	}
	secretVariableEvents, err := vc.RegisterSecretVariableEvents(catalog)
	if err != nil {
		return err
	}
	knowledgeEvents, err := kc.RegisterKnowledgeEvents(catalog)
	if err != nil {
		return err
	}
	agentEvents, err := agentc.RegisterAgentEvents(catalog)
	if err != nil {
		return err
	}
	agentProducer, err := agent.NewEventAuthority(agentAuthority, agentEvents)
	if err != nil {
		return err
	}
	producers := map[event.StableName]oc.ProducerAuthority{agentc.AgentProducer: agentProducer, c.AccountProducer: authority, model.ModelProducer: modelAuthority, pc.ProjectProducer: projectUsage.projects, wc.WorkProducer: workAuthority, vc.VariableProducer: projectUsage.variables, kc.KnowledgeProducer: contentAuthorities.knowledge}
	var executionEvents executionRuntimeEventSet
	if runtimeEnabled {
		executionEvents, err = defineExecutionRuntimeEvents(catalog)
		if err != nil {
			return err
		}
		producers[ec.ExecutionProducer] = executionAuthorities.events
	}
	journal, err := outbox.New(journalStore, catalog, outbox.Authorizations{
		Producers: producers,
		Processes: outboxProcessAuthority{process: objects.process, guard: objects.guard},
		Sessions:  authority, System: authority, Audit: auditor, Cursors: cfg.CursorKeyring(), Projects: projectUsage.projects,
	})
	if err != nil {
		return err
	}
	var planning *workPlanningAssembly
	if runtimeEnabled {
		planning, err = createOwnedWorkPlanningWithProviders(cfg, db, workAuthority, authority, journal, workEvents, executionAuthorities.pending, agentAuthority, projectUsage.projects, projectUsage.lifecycle, objects)
	} else {
		planning, err = createOwnedWorkPlanningWithAgents(cfg, db, workAuthority, authority, journal, workEvents, agentAuthority, projectUsage.projects, projectUsage.lifecycle, objects)
	}
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.planning = planning }) {
		return context.Canceled
	}
	models, err := model.New(modelStore, modelAuthority, model.Dependencies{Secret: secrets, Audit: auditor, Events: journal, ConfigurationEvents: modelEvents, Cursors: cfg.CursorKeyring()})
	if err != nil {
		return err
	}
	challenges, err := account.NewChallenges(authority, process)
	if err != nil {
		return err
	}
	core, err := account.New(account.Dependencies{Authority: authority, Audit: auditor, Secrets: secrets, Events: journal, SessionsRevoked: revoked, DeliveryRequested: delivery, Processes: processes, RecoveryLog: sink, Challenges: challenges})
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.core = core }) {
		return context.Canceled
	}
	variableCalls, err := createOwnedProjectVariables(cfg, db, projectUsage.variables, secretWrites, secrets, projectUsage.projects, authority, auditor, journal, variableEvents, secretVariableEvents, objects)
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() {
		accounts.variableCalls = variableCalls
		accounts.variables = variableCalls.variables
		accounts.secretVariables = variableCalls.secretVariables
	}) {
		return context.Canceled
	}
	variables, secretVariables := variableCalls.variables, variableCalls.secretVariables
	projectReads, err := createProjectRead(cfg, db, projectUsage.projects)
	if err != nil {
		return err
	}
	deps.runtimeInformation = runtimeInformationHandlerFactory(runtimeStore, authority, core, cfg.PublicOrigin())
	if deps.observeAccount != nil {
		deps.observeAccount(core)
	}
	avatar, err := account.NewAvatarAuthority(core)
	if err != nil {
		return err
	}
	objectStore, ok := db.(object.Store)
	if !ok {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	objectAuthorities, err := newObjectAuthorities(objectStore, avatar, contentAuthorities.knowledge, contentAuthorities.skills)
	if err != nil {
		return err
	}
	if err = objects.construct(cfg, db, auditor, objectAuthorities); err != nil {
		return err
	}
	documents, err := createKnowledge(cfg, db, contentAuthorities.knowledge, projectUsage.projects, authority, objects, auditor, journal, knowledgeEvents)
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.knowledge = &knowledgeWork{service: documents} }) {
		return context.Canceled
	}
	skills, err := createSkills(ctx, contentAuthorities.skills, objects)
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.skills = &skillWork{service: skills} }) {
		return context.Canceled
	}
	// Skills uses the original Agent authority, not the Agent service. All
	// providers are now fixed, so publish both Agent lanes as one lifecycle owner.
	agentConfiguration, err := createOwnedAgentConfiguration(db, projectUsage.projects, authority, agentAuthority, agentProducer, modelAuthority, skills, contentAuthorities.skills, install, auditor, journal, objects, cfg.CursorKeyring())
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.agents = agentConfiguration }) {
		return context.Canceled
	}
	projectCommands, err := createProjectCommands(cfg, db, projectUsage.projects, authority, auditor, journal, projectEvents, processes, skills)
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.projects = &projectCommandWork{service: projectCommands} }) {
		return context.Canceled
	}
	profiles, err := account.NewProfileService(core, objects.service)
	if err != nil {
		return err
	}
	accountRuntime, err := account.NewRuntime(core, profiles)
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.runtime = accountRuntime }) {
		return context.Canceled
	}
	registry, err := accountmail.NewWorkRegistry(processes)
	if err != nil {
		return err
	}
	port, err := account.NewDeliveryPort(core, registry)
	if err != nil {
		return err
	}
	worker, err := accountmail.New(accountmail.Dependencies{Port: port, Registry: registry, Outbound: transport.client, Trust: cfg.OutboundTrust(), RecoveryLog: sink, PublicOrigin: cfg.PublicOrigin()})
	if err != nil {
		return err
	}
	mailRuntime, err := accountmail.NewRuntime(core, worker)
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() { accounts.mail = mailRuntime }) {
		return context.Canceled
	}
	handler, err := core.MailHandler()
	if err != nil {
		return err
	}
	deliveries, err := outbox.NewRuntime(journal, []oc.HandlerDefinition{handler}, outbox.Options{})
	if err != nil {
		return err
	}
	if !owned.addOutbox(ctx, deliveries) {
		return context.Canceled
	}
	system, err := account.NewSystemHTTPFacade(core, cfg.CursorKeyring())
	if err != nil {
		return err
	}
	httpHandler, err := account.NewHTTPHandler(core, profiles, account.HTTPOptions{PublicOrigin: cfg.PublicOrigin(), System: system})
	if err != nil {
		return err
	}
	modelHandler, err := model.NewSystemHTTPHandler(models, core, secrets, model.SystemHTTPOptions{PublicOrigin: cfg.PublicOrigin()})
	if err != nil {
		return err
	}
	policyHandler, err := outboundhttp.NewSystemHTTPHandler(transport.policy, core, outboundhttp.SystemHTTPOptions{PublicOrigin: cfg.PublicOrigin()})
	if err != nil {
		return err
	}
	auditHandler, err := audithttp.NewSystemHTTPHandler(auditor, core, audithttp.SystemHTTPOptions{PublicOrigin: cfg.PublicOrigin()})
	if err != nil {
		return err
	}
	usageHandler, err := projectUsage.handler(core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	projectHandler, err := projectReadHandler(projectReads, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	updateHandler, err := projectUpdateHandler(projectCommands, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	projectModelHandler, err := projectModelsHandler(models, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	credentialHandler, err := projectCredentialsHandler(secrets, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	projectAudit, err := projectAuditHandler(auditor, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	planningHandler, err := workPlanningHandler(planning, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	directoryHandler, err := agentOwnedDirectoryHandler(agentConfiguration, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	agentHandler, err := agentConfigurationHandler(agentConfiguration, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	variableHandler, err := projectVariablesHandler(variables, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	secretVariableHandler, err := projectSecretVariablesHandler(secretVariables, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	runnerAdmin, runnerDevice, err := runnerControlHandlers(runners, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	knowledgeReads, knowledgeCommands, knowledgeContent, skillReads, err := knowledgeSkillHandlers(documents, skills, core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	skillManagement, err := skillManagementHandler(skills, cfg.CursorKeyring(), core, cfg.PublicOrigin())
	if err != nil {
		return err
	}
	if !accounts.install(ctx, func() {
		accounts.handler = projectAuditRoutes(projectCredentialsRoutes(projectModelsRoutes(projectUpdateRoutes(projectReadRoutes(projectUsageRoutes(systemAuditRoutes(systemOutboundPolicyRoutes(systemModelRoutes(httpHandler, modelHandler), policyHandler), auditHandler), usageHandler), projectHandler), updateHandler), projectModelHandler), credentialHandler), projectAudit)
		accounts.handler = workPlanningRoutes(accounts.handler, planningHandler)
		accounts.handler = agentDirectoryRoutes(accounts.handler, directoryHandler)
		accounts.handler = agentConfigurationRoutes(accounts.handler, agentHandler)
		accounts.handler = projectVariablesRoutes(accounts.handler, variableHandler)
		accounts.handler = projectSecretVariablesRoutes(accounts.handler, secretVariableHandler)
		accounts.handler = runnerControlRoutes(accounts.handler, runnerAdmin, runnerDevice)
		accounts.handler = knowledgeSkillRoutes(accounts.handler, knowledgeReads, knowledgeCommands, knowledgeContent, skillReads)
		accounts.handler = skillManagementRoutes(accounts.handler, skillManagement)
	}) {
		return context.Canceled
	}
	deps.security = func(ctx context.Context, _ config.Config, _ database) (*audit.Service, error) {
		if err := authority.Initialize(ctx); err != nil {
			return auditor, err
		}
		return auditor, auditor.CheckStorage(ctx)
	}
	deps.secret = func(ctx context.Context, _ config.Config, _ database, _ *audit.Service) (maintenance, error) {
		var err error
		if deps.secretInitialize != nil {
			err = deps.secretInitialize(ctx, secrets)
		} else {
			err = secrets.Initialize(ctx)
		}
		if err != nil {
			return secrets, err
		}
		if err = ctx.Err(); err != nil {
			return secrets, err
		}
		return secrets, initializeModelsAndUsage(ctx, models.Initialize, models.InitializeMeetingSummarySelection, projectUsage.reader.Initialize)
	}
	deps.outbound = func(ctx context.Context, _ config.Config, _ database, _ *audit.Service) (egress, error) {
		if deps.outboundInitialize != nil {
			return deps.outboundInitialize(ctx, transport)
		}
		return transport, transport.policy.Reload(ctx)
	}
	deps.objects = func(ctx context.Context, _ config.Config, _ database, _ *audit.Service) (objectStorage, error) {
		if err := objects.runtime.Initialize(ctx); err != nil {
			return objects, err
		}
		if err := ctx.Err(); err != nil {
			return objects, err
		}
		if runtimeEnabled {
			// The four-owner constructor requires this guard's actual binding.
			// Earlier startup failures never install an empty constructing owner.
			// No Run or Launch is reachable until Account.Start below this phase.
			executions := &executionRuntimeAssembly{constructing: true}
			if !accounts.install(ctx, func() { accounts.executions = executions }) {
				executions.constructionDone()
				return objects, context.Canceled
			}
			if err := constructExecutionRuntime(executions, cfg, db, authority, projectUsage, contentAuthorities.skills, workAuthority, executionAuthorities, objects, models, secrets, transport, journal, executionEvents, agentConfiguration.registry); err != nil {
				return objects, err
			}
		}
		return objects, ctx.Err()
	}
	deps.outbox = func(config.Config, database, *audit.Service, objectStorage) (outboxStorage, error) {
		return deliveries, nil
	}
	return ctx.Err()
}

var _ accountWork = (*recoverylog.Sink)(nil)
var _ accountWork = (*account.Service)(nil)
var _ accountRuntime = (*account.Runtime)(nil)
var _ accountRuntime = (*accountmail.Runtime)(nil)
var _ maintenance = (*secret.Service)(nil)
