package agenthttp

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"slices"
	"strings"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const configurationBodyLimit = c.MaxAgentCoreBytes

type configurationIntent struct {
	meta    f.CommandMeta
	command c.CommandName
	target  i.AgentID
	request c.AgentCreate
	update  c.AgentUpdate
}

func decodeConfigurationIntent(w http.ResponseWriter, r *http.Request, route configurationRoute) (configurationIntent, error) {
	for name := range r.Header {
		if strings.EqualFold(name, "Content-Encoding") {
			return configurationIntent{}, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
	}
	var keys []string
	for name, values := range r.Header {
		if strings.EqualFold(name, "Idempotency-Key") {
			keys = append(keys, values...)
		}
	}
	if len(keys) != 1 || f.IdempotencyKey(keys[0]).Validate() != nil {
		return configurationIntent{}, invalidInput()
	}
	var wire struct {
		Command  json.RawMessage `json:"command,omitempty"`
		Target   json.RawMessage `json:"target_id,omitempty"`
		Expected json.RawMessage `json:"expected_version,omitempty"`
		Request  json.RawMessage `json:"request"`
	}
	if err := httpapi.DecodeJSON(w, r, &wire, configurationBodyLimit); err != nil {
		return configurationIntent{}, err
	}
	intent := configurationIntent{
		meta:    f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0])},
		command: c.CreateAgentCommand,
	}
	lookup := route.kind == configurationLookup
	if lookup {
		var command c.CommandName
		if json.Unmarshal(wire.Command, &command) != nil || command.Validate() != nil {
			return configurationIntent{}, invalidInput()
		}
		intent.command = command
	} else if len(wire.Command) != 0 {
		return configurationIntent{}, invalidInput()
	} else if route.kind == configurationRead && r.Method == http.MethodPatch {
		intent.command = c.UpdateAgentCommand
	}
	var err error
	if intent.command == c.CreateAgentCommand {
		if len(wire.Target) != 0 || len(wire.Expected) != 0 {
			return configurationIntent{}, invalidInput()
		}
		intent.request, err = c.DecodeAgentCreate(wire.Request)
		intent.target = intent.request.Fields().AgentID
	} else {
		var expected f.Version
		if json.Unmarshal(wire.Expected, &expected) != nil || expected.Validate() != nil {
			return configurationIntent{}, invalidInput()
		}
		intent.meta.ExpectedVersion = &expected
		if lookup {
			if json.Unmarshal(wire.Target, &intent.target) != nil || intent.target.Validate() != nil {
				return configurationIntent{}, invalidInput()
			}
		} else {
			if len(wire.Target) != 0 {
				return configurationIntent{}, invalidInput()
			}
			intent.target, err = f.ParseID[i.Agent](route.target)
			if err != nil {
				return configurationIntent{}, invalidInput()
			}
		}
		intent.update, err = c.DecodeAgentUpdate(wire.Request)
	}
	if err != nil {
		return configurationIntent{}, err
	}
	if c.ValidateAgentCommandMeta(intent.command, intent.meta) != nil {
		return configurationIntent{}, invalidInput()
	}
	return intent, nil
}

func (v configurationIntent) value() any {
	if v.command == c.UpdateAgentCommand {
		return v.update
	}
	return v.request
}

func (v configurationIntent) matches(value c.AgentMutation, project i.ProjectID) bool {
	if v.command == c.UpdateAgentCommand {
		return configurationUpdateReceiptMatches(value, project, v.target, v.meta, v.update)
	}
	return configurationReceiptMatches(value, project, v.request)
}

func configurationUpdateReceiptMatches(value c.AgentMutation, project i.ProjectID, target i.AgentID, meta f.CommandMeta, request c.AgentUpdate) bool {
	if value.Validate() != nil || c.ValidateAgentCommandMeta(c.UpdateAgentCommand, meta) != nil || request.Validate() != nil {
		return false
	}
	receipt := value.Fields()
	if !configurationMatches(receipt.Agent, project, target) {
		return false
	}
	expected := *meta.ExpectedVersion
	if receipt.Changed {
		if expected == f.Version(math.MaxInt64) || receipt.Agent.Fields().Core.Version != expected+1 || len(receipt.EventIDs) != 1 {
			return false
		}
	} else if receipt.Agent.Fields().Core.Version != expected || len(receipt.EventIDs) != 0 {
		return false
	}
	// Apply presence to the original receipt itself: every explicit field must
	// already match, including null/false/empty arrays. Omitted fields remain
	// historical facts; no current read or creation default fills them in.
	var proposed c.AgentConfig
	var err error
	if (request.Fields().RunnerMounts == nil) != (receipt.RunnerMounts == nil) {
		return false
	}
	if receipt.RunnerMounts != nil {
		proposed, err = request.ProposedWithRunnerMounts(receipt.Agent, *receipt.RunnerMounts)
	} else {
		proposed, err = request.Proposed(receipt.Agent)
	}
	if err != nil {
		return false
	}
	wanted, err := json.Marshal(proposed)
	if err != nil {
		return false
	}
	actual, err := json.Marshal(receipt.Agent)
	return err == nil && bytes.Equal(wanted, actual)
}

func configurationMatches(value c.AgentConfig, project i.ProjectID, target i.AgentID) bool {
	if value.Validate() != nil {
		return false
	}
	core := value.Fields().Core
	return core.ProjectID == project && core.ID == target && core.Lifecycle == c.AgentActive
}

func configurationSame[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func configurationReceiptMatches(value c.AgentMutation, project i.ProjectID, request c.AgentCreate) bool {
	if value.Validate() != nil || request.Validate() != nil {
		return false
	}
	receipt, wanted := value.Fields(), request.Fields()
	if !receipt.Changed || len(receipt.EventIDs) != 1 || !configurationMatches(receipt.Agent, project, wanted.AgentID) {
		return false
	}
	if (wanted.RunnerMounts == nil) != (receipt.RunnerMounts == nil) {
		return false
	}
	got := receipt.Agent.Fields()
	if wanted.RunnerMounts != nil {
		if c.MatchRunnerMountBindings(*wanted.RunnerMounts, *receipt.RunnerMounts, got.AllowedMountIDs) != nil {
			return false
		}
		wanted.AllowedMountIDs = got.AllowedMountIDs
	}
	core := got.Core
	if core.Version != 1 || core.CreatedAt != core.UpdatedAt || core.Name != wanted.Name ||
		!configurationSame(core.DisplayName, wanted.DisplayName) || !configurationSame(core.TagColor, wanted.TagColor) ||
		core.Description != wanted.Description || core.Instructions != wanted.Instructions || core.InjectAgentsMD != wanted.InjectAgentsMD ||
		core.ModelRef != wanted.ModelRef || !configurationSame(core.ReasoningEffort, wanted.ReasoningEffort) ||
		core.ApprovalPolicy != wanted.ApprovalPolicy || !configurationSame(core.ApprovalModelRef, wanted.ApprovalModelRef) ||
		!slices.Equal(got.AllowedMountIDs, wanted.AllowedMountIDs) || !slices.Equal(got.AllowedSecretVariableIDs, wanted.AllowedSecretVariableIDs) {
		return false
	}
	if !*wanted.InstallSkillEnabled {
		return slices.Equal(got.AllowedToolIDs, wanted.AllowedToolIDs)
	}
	// The original real Registry may add its single default install-skill Tool.
	// HTTP does not discover an ID, manufacture a default or grant its capability.
	if len(got.AllowedToolIDs) == 0 || len(got.AllowedToolIDs) < len(wanted.AllowedToolIDs) || len(got.AllowedToolIDs) > len(wanted.AllowedToolIDs)+1 {
		return false
	}
	for _, id := range wanted.AllowedToolIDs {
		if !slices.Contains(got.AllowedToolIDs, id) {
			return false
		}
	}
	return true
}
