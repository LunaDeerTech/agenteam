package app

import (
	"context"
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	agenthttp "github.com/LunaDeerTech/agenteam/internal/central/agent/http"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
)

// Directory construction uses the original Project Authority and Store. It
// neither constructs Agent commands nor binds the Project runtime initializer.
type agentDirectoryAssembly struct{ reader *agent.DirectoryReader }

func createAgentDirectory(cfg config.Config, db database, projects *project.Authority) (*agentDirectoryAssembly, error) {
	store, ok := db.(agent.Store)
	if !ok || runtimeInformationNil(store) || projects == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	authority, err := agent.NewAuthority(store, projects)
	if err != nil {
		return nil, err
	}
	return createAgentDirectoryWithAuthority(cfg, db, authority)
}

func createAgentDirectoryWithAuthority(cfg config.Config, db database, authority *agent.Authority) (*agentDirectoryAssembly, error) {
	store, ok := db.(agent.Store)
	if !ok || runtimeInformationNil(store) || authority == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	reader, err := agent.NewDirectoryReader(store, authority, cfg.CursorKeyring())
	if err != nil {
		return nil, err
	}
	return &agentDirectoryAssembly{reader}, nil
}
func (a *agentDirectoryAssembly) StopAdmission()                  { a.reader.Stop() }
func (a *agentDirectoryAssembly) Drain(ctx context.Context) error { return a.reader.Drain(ctx) }
func (a *agentDirectoryAssembly) Force(ctx context.Context) error { return a.Drain(ctx) }
func (a *agentDirectoryAssembly) Joined() bool                    { return a.reader.Joined() }
func agentDirectoryHandler(a *agentDirectoryAssembly, core *account.Service, origin string) (http.Handler, error) {
	if a == nil || a.reader == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return agentDirectoryReaderHandler(a.reader, core, origin)
}

// The default App borrows this fixed reader from its configuration owner;
// creating the handler creates no second accountWork or exit lifecycle.
func agentOwnedDirectoryHandler(a *agentConfigurationAssembly, core *account.Service, origin string) (http.Handler, error) {
	if a == nil || a.ownership == nil || a.service != a.ownership.Service() {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return agentDirectoryReaderHandler(a.ownership.DirectoryReader(), core, origin)
}

func agentDirectoryReaderHandler(reader *agent.DirectoryReader, core *account.Service, origin string) (http.Handler, error) {
	if reader == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return agenthttp.NewDirectoryHTTPHandler(reader, boundary)
}
func agentDirectoryRoutes(existing, reads http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if agenthttp.HandlesDirectoryPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}

var _ accountWork = (*agentDirectoryAssembly)(nil)
