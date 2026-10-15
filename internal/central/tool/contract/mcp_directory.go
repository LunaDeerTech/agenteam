package contract

import (
	"context"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// MCPRegistrationView is current Project metadata, never an execution grant.
// It deliberately excludes the endpoint and every credential identifier.
type MCPRegistrationView struct {
	SpecRef        SpecRef    `json:"spec_ref"`
	Definition     Definition `json:"definition"`
	Binding        MCPBinding `json:"binding"`
	DefaultEnabled bool       `json:"default_enabled"`
	Active         bool       `json:"active"`
}

type MCPToolDirectory interface {
	ListMCPTools(context.Context, id.Actor, id.ProjectID, string) ([]MCPRegistrationView, error)
}
