package app

import (
	"context"
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/humaninbox"
	inboxhttp "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/http"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	mc "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	meetinghttp "github.com/LunaDeerTech/agenteam/internal/central/meeting/http"
	meetingtools "github.com/LunaDeerTech/agenteam/internal/central/meeting/tools"
	objectcontract "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
	tr "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

// The original guard stays owned by App until the Outbox callback joins.
func (a outboxProcessAuthority) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, expected oc.ProcessID) error {
	if a.guard == nil || expected.Validate() != nil || expected.String() != a.process.String() {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	id, err := f.ParseID[objectcontract.Process](expected.String())
	if err != nil {
		return err
	}
	return a.guard.RequireCurrentProcessInTx(ctx, tx, id)
}

// The producer is fixed before outbox.New. The remaining domain constructors
// consume the original journal; no later setter grants append authority.
type meetingProposalEventAuthorities struct {
	types         mc.Events
	decisionTypes mc.DecisionEvents
	producer      *meeting.EventAuthority
}

func prepareMeetingProposalEvents(db database, projects pc.ProjectAuthority, catalog *event.Catalog) (meetingProposalEventAuthorities, error) {
	store, ok := db.(meeting.Store)
	if !ok || catalog == nil {
		return meetingProposalEventAuthorities{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	types, err := mc.RegisterEvents(catalog)
	if err != nil {
		return meetingProposalEventAuthorities{}, err
	}
	decisionTypes, err := mc.RegisterDecisionEvents(catalog)
	if err != nil {
		return meetingProposalEventAuthorities{}, err
	}
	producer, err := meeting.NewEventAuthority(store, projects)
	if err != nil {
		return meetingProposalEventAuthorities{}, err
	}
	return meetingProposalEventAuthorities{types: types, decisionTypes: decisionTypes, producer: producer}, nil
}

type meetingProposalAssembly struct {
	events          *meeting.ProposalEvents
	decisionEvents  *meeting.DecisionEvents
	inbox           *humaninbox.Service
	handler         oc.HandlerDefinition
	decisionHandler oc.HandlerDefinition
}

// Inbox's canonical source reader does not depend on a Human Meeting service.
// It can therefore register its handler before the original Outbox runtime,
// while Meeting's runtime/service are assembled at their existing later phase.
func constructMeetingProposalProjection(db database, projects pc.ProjectAuthority, authorities meetingProposalEventAuthorities, journal oc.Appender) (*meetingProposalAssembly, error) {
	store, ok := db.(meeting.Store)
	if !ok {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	events, err := meeting.NewProposalEvents(authorities.producer, authorities.types, journal)
	if err != nil {
		return nil, err
	}
	decisionEvents, err := meeting.NewDecisionEvents(authorities.producer, authorities.decisionTypes, journal)
	if err != nil {
		return nil, err
	}
	authority, err := humaninbox.NewAuthority(store, projects)
	if err != nil {
		return nil, err
	}
	sources, err := meeting.NewProposalInboxSource(store, authority)
	if err != nil {
		return nil, err
	}
	decisions, err := meeting.NewDecisionInboxSource(store, authority)
	if err != nil {
		return nil, err
	}
	inbox, err := humaninbox.NewServiceWithDecisions(authority, sources, authorities.types, decisions, authorities.decisionTypes)
	if err != nil {
		return nil, err
	}
	handler, err := inbox.Handler()
	if err != nil {
		return nil, err
	}
	decisionHandler, err := inbox.DecisionHandler()
	if err != nil {
		return nil, err
	}
	return &meetingProposalAssembly{events: events, decisionEvents: decisionEvents, inbox: inbox, handler: handler, decisionHandler: decisionHandler}, nil
}

func (a *meetingProposalAssembly) agentBackends(db database, agents *agent.Authority, tools *tr.BuiltinAuthority, publisher rc.Publisher) ([]tr.BuiltinBackend, error) {
	store, ok := db.(meeting.Store)
	if !ok || a == nil || a.events == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	participants, err := agent.NewMeetingParticipantReader(agents, tools)
	if err != nil {
		return nil, err
	}
	service, err := meeting.NewAgentService(store, meeting.AgentDependencies{Tools: tools, Participants: participants, Events: a.events, Realtime: publisher})
	if err != nil {
		return nil, err
	}
	return meetingtools.NewAgentBackends(service)
}

func (a *meetingProposalAssembly) owner(db database, domain *meeting.Service) (*meeting.ProposalService, error) {
	store, ok := db.(meeting.Store)
	if !ok || a == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return meeting.NewProposalService(store, domain, a.events)
}

func createMeetingInboxHTTP(domain *humaninbox.Service, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return inboxhttp.NewHTTPHandler(domain, boundary)
}

func meetingInboxRoutes(previous, inbox http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inboxhttp.HandlesPath(r.URL.Path) {
			inbox.ServeHTTP(w, r)
			return
		}
		previous.ServeHTTP(w, r)
	})
}

func createMeetingProposalHTTP(domain *meeting.ProposalService, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return meetinghttp.NewProposalHTTPHandler(domain, boundary)
}

func meetingProposalRoutes(previous, proposals http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if meetinghttp.HandlesProposalPath(r.URL.Path) {
			proposals.ServeHTTP(w, r)
			return
		}
		previous.ServeHTTP(w, r)
	})
}
