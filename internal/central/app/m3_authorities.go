package app

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	"github.com/LunaDeerTech/agenteam/internal/central/memory"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
)

type m3Authorities struct {
	knowledge    *knowledge.IndexAuthority
	memory       *memory.ModelAuthority
	meeting      *meeting.Authority
	summary      *meeting.SummaryAuthority
	meetingActor i.ServiceRegistration
}

func createM3Authorities(db database, projects *project.Authority, objects *objectAssembly) (*m3Authorities, error) {
	store, ok := db.(knowledge.Store)
	if !ok || runtimeInformationNil(store) || projects == nil || objects == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	a := &m3Authorities{}
	kr, err := i.RegisterService(i.KnowledgeRuntime)
	if err != nil {
		return nil, err
	}
	if a.knowledge, err = knowledge.NewIndexAuthority(store, objects.guard, kr, objects.process); err != nil {
		return nil, err
	}
	mr, err := i.RegisterService(i.MemoryRuntime)
	if err != nil {
		return nil, err
	}
	if a.memory, err = memory.NewProcessModelAuthority(store, objects.guard, mr, objects.process); err != nil {
		return nil, err
	}
	if a.meeting, err = meeting.NewAuthority(store); err != nil {
		return nil, err
	}
	if a.summary, err = meeting.NewSummaryAuthority(store, projects, objects.guard, objects.process); err != nil {
		return nil, err
	}
	if a.meetingActor, err = i.RegisterService(i.MeetingRuntime); err != nil {
		return nil, err
	}
	return a, nil
}

type executionServiceAccess struct{ scheduler, meeting ec.ServiceProjectAccess }

func (a executionServiceAccess) RequireExecutionProjectInTx(ctx context.Context, tx f.Tx, actor i.Actor, p i.ProjectID, agent i.AgentID, intent i.AccessIntent) error {
	if actor.Validate() != nil {
		return f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	var owner ec.ServiceProjectAccess
	switch actor.Details().ServiceName {
	case i.Scheduler:
		owner = a.scheduler
	case i.MeetingRuntime:
		owner = a.meeting
	default:
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	if runtimeInformationNil(owner) {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return owner.RequireExecutionProjectInTx(ctx, tx, actor, p, agent, intent)
}
