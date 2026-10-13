package app

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	skillhttp "github.com/LunaDeerTech/agenteam/internal/central/skill/http"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

// This fixed graph records configuration and durable ingress only. Constructing
// the real slot observer starts no execution, scheduler, model or tool worker.
// Every call remains owned by the original Skill Service's begin/end/drain.
func createSkillAssignments(db database, service *skill.Service, agents *agent.Authority, facts *skill.AssignmentAuthority, events sc.AssignmentEvents, auditor *audit.Service, journal *outbox.Service) (*skill.AssignmentCommands, error) {
	store, ok := db.(agent.Store)
	if !ok || runtimeInformationNil(store) || service == nil || agents == nil || facts == nil || !events.Valid() || auditor == nil || journal == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	owner, err := createSkillAssignmentOwner(store, agents)
	if err != nil {
		return nil, err
	}
	ingress, err := execution.NewSkillAssignmentIngress(store, facts)
	if err != nil {
		return nil, err
	}
	return skill.NewAssignmentCommands(skill.AssignmentDependencies{Service: service, Agents: owner, CurrentAgents: agents, Runtime: ingress, Audit: auditor, Outbox: journal, Events: events}, facts)
}

func createSkillAssignmentOwner(store agent.Store, agents *agent.Authority) (*agent.SkillAssignmentAuthority, error) {
	if runtimeInformationNil(store) || agents == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	tools, err := registry.NewSkillAssignmentReferences(store, agents)
	if err != nil {
		return nil, err
	}
	mounts, err := mount.NewSkillAssignmentReferences(store, agents)
	if err != nil {
		return nil, err
	}
	models, err := model.NewSkillAssignmentReferences(store, agents)
	if err != nil {
		return nil, err
	}
	secrets, err := projectvariable.NewSkillAssignmentReferences(store, agents)
	if err != nil {
		return nil, err
	}
	return agent.NewSkillAssignmentAuthority(agents, []ac.SkillAssignmentVersionReferences{tools, mounts, models, secrets})
}

func skillAssignmentHandler(commands *skill.AssignmentCommands, keys cursor.Keyring, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return skillhttp.NewAssignmentHTTPHandler(commands, keys, boundary)
}

func skillAssignmentRoutes(existing, assignments http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skillhttp.HandlesAssignmentRequest(r) {
			assignments.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}
