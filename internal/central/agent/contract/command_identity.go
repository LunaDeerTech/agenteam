package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type AgentCommand struct{}
type AgentCommandID = f.ID[AgentCommand]
type CommandName string

const (
	CreateAgentCommand CommandName = "agent.create"
	UpdateAgentCommand CommandName = "agent.update"
)

func (c CommandName) Validate() error {
	if c != CreateAgentCommand && c != UpdateAgentCommand {
		return invalid("/command", "INVALID_AGENT_COMMAND")
	}
	return nil
}

func AgentCommandIdentity(project i.ProjectID, command CommandName, key f.IdempotencyKey) (f.CommandIdentity, error) {
	if project.Validate() != nil || command.Validate() != nil || key.Validate() != nil {
		return f.CommandIdentity{}, invalid("", "INVALID_AGENT_COMMAND")
	}
	return f.NewCommandIdentity("project", []string{project.String()}, string(command), key)
}

func ValidateAgentCommandMeta(command CommandName, meta f.CommandMeta) error {
	if command.Validate() != nil || meta.Validate() != nil || (command == UpdateAgentCommand) != (meta.ExpectedVersion != nil) {
		return invalid("", "INVALID_COMMAND_META")
	}
	return nil
}
