package app

import (
	"context"
	"errors"
	"net/http"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	audithttp "github.com/LunaDeerTech/agenteam/internal/central/audit/http"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objectcontract "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	outboundhttp "github.com/LunaDeerTech/agenteam/internal/central/outbound/http"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
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
	mu           sync.Mutex
	constructing bool
	stopped      bool
	started      bool
	forced       context.Context
	sink         accountWork
	core         accountWork
	runtime      accountRuntime
	mail         accountRuntime
	handler      http.Handler
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
	runtime, mail, stopped := a.runtime, a.mail, a.stopped
	a.mu.Unlock()
	if runtime == nil || mail == nil || stopped {
		return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	if err := runtime.Start(ctx); err != nil {
		return err
	}
	if err := mail.Start(ctx); err != nil {
		return err
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
	runtime, mail, available := a.runtime, a.mail, a.started && !a.stopped
	a.mu.Unlock()
	if !available || runtime == nil || mail == nil {
		return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotStarted)
	}
	// Both concrete checks consume this same health round. Neither probes SMTP.
	if err := runtime.Check(ctx); err != nil {
		return err
	}
	return mail.Check(ctx)
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
	a.mu.Unlock()
	a.StopAdmission()
	var result error
	// Mail gets its final completion opportunity before core retirement. Even
	// if it consumes the budget, initiate every remaining concrete cancellation
	// with the original expired context so root can actually force DB last.
	for _, work := range a.works() {
		result = errors.Join(result, work.Force(original))
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

// bindAccounts constructs every real provider before any domain initialization.
// Returned callbacks capture already-constructed concrete services; none is an
// authorization locator, and no callback performs a second construction.
func bindAccounts(ctx context.Context, cfg config.Config, db database, owned *resources, deps *dependencies) error {
	store, ok := db.(account.Store)
	if !ok {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	authority, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		return err
	}
	modelStore, ok := db.(model.Store)
	if !ok {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	modelAuthority, err := model.NewAuthority(modelStore, model.Authorizations{Sessions: authority, System: authority})
	if err != nil {
		return err
	}
	usage, err := model.NewSecretUsageRouter(modelAuthority, authority)
	if err != nil {
		return err
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
	process, err := foundation.ParseID[c.Process](objects.process.String())
	if err != nil {
		return err
	}
	processes := accountProcessAuthority{process: process, guard: objects.guard}
	auditor, err := createSecurity(cfg, db, authority, modelAuthority)
	if err != nil {
		return err
	}
	secrets, err := createSecret(cfg, db, auditor, authority, usage)
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
	journalStore, ok := db.(outbox.Store)
	if !ok {
		return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	journal, err := outbox.New(journalStore, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{c.AccountProducer: authority, model.ModelProducer: modelAuthority},
		Processes: outboxProcessAuthority{process: objects.process, guard: objects.guard},
		Sessions:  authority, System: authority, Audit: auditor, Cursors: cfg.CursorKeyring(),
	})
	if err != nil {
		return err
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
	if deps.observeAccount != nil {
		deps.observeAccount(core)
	}
	avatar, err := account.NewAvatarAuthority(core)
	if err != nil {
		return err
	}
	if err = objects.construct(cfg, db, auditor, avatar); err != nil {
		return err
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
	if !accounts.install(ctx, func() {
		accounts.handler = systemAuditRoutes(systemOutboundPolicyRoutes(systemModelRoutes(httpHandler, modelHandler), policyHandler), auditHandler)
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
		return secrets, models.Initialize(ctx)
	}
	deps.outbound = func(ctx context.Context, _ config.Config, _ database, _ *audit.Service) (egress, error) {
		if deps.outboundInitialize != nil {
			return deps.outboundInitialize(ctx, transport)
		}
		return transport, transport.policy.Reload(ctx)
	}
	deps.objects = func(ctx context.Context, _ config.Config, _ database, _ *audit.Service) (objectStorage, error) {
		return objects, objects.runtime.Initialize(ctx)
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
