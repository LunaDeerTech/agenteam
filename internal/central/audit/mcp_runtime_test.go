package audit

import (
	c "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

func TestMCPRuntimeAuditProducerRequiresExactOriginalCause(t *testing.T) {
	project, _ := f.NewID[id.Project]()
	scope, _ := id.InProject(project)
	cause, _ := f.NewID[struct{}]()
	registration, _ := id.RegisterService(id.MCPRuntime)
	actor, _ := registration.Actor(cause.String(), scope)
	key, _ := c.NewAppendKey(c.MCPProducer, cause.String(), 0)
	if !serviceOwns(actor, scope, key) {
		t.Fatal("exact project runtime producer rejected")
	}
	other, _ := f.NewID[struct{}]()
	changed, _ := c.NewAppendKey(c.MCPProducer, other.String(), 0)
	if serviceOwns(actor, scope, changed) {
		t.Fatal("foreign original request admitted")
	}
	foreign, _ := c.NewAppendKey(c.SecretProducer, cause.String(), 0)
	if serviceOwns(actor, scope, foreign) || serviceOwns(actor, id.SystemScope(), key) {
		t.Fatal("foreign producer or system scope admitted")
	}
}
