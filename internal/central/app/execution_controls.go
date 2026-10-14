package app

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	executionhttp "github.com/LunaDeerTech/agenteam/internal/central/execution/http"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/governance/approval"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	approvalhttp "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/http"
)

func (b *executionRuntimeAssembly) bindApproval(store execution.Store, owner *execution.ToolCallDriver) (gc.Gate, error) {
	if b == nil || b.approval != nil || b.approvalAuthority == nil || b.approvalOperations == nil || b.approvalJournal == nil || owner == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	service, err := approval.NewService(store, approval.Dependencies{Authority: b.approvalAuthority, Executions: owner, Operations: b.approvalOperations, Events: b.approvalJournal, EventTypes: b.approvalEvents})
	if err != nil {
		return nil, err
	}
	b.approval = service
	return service, nil
}

func executionControlHandler(reader *execution.TaskExecutionReader, controls *execution.CancellationService, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return executionhttp.NewHTTPHandlerWithControls(reader, controls, boundary)
}
func approvalHandler(service *approval.Service, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return approvalhttp.NewHTTPHandler(service, boundary)
}
func approvalRoutes(existing, handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if approvalhttp.HandlesPath(r.URL.Path) {
			handler.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}
