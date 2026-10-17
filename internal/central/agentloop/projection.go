package agentloop

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
)

const textProjectionLimit = 16 << 20

// These are explicit model-visible projections, not copies of Environment,
// ResolvedModel, AgentConfig or ExecutionContext storage encodings.
type projectInformation struct {
	ID          id.ProjectID `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Version     f.Version    `json:"version"`
}
type environmentInformation struct {
	Project  projectInformation `json:"project"`
	Policy   ec.Policy          `json:"execution_policy"`
	AgentsMD bool               `json:"agents_md_injected"`
	Mounts   []mountInformation `json:"mounts"`
	Tools    []string           `json:"tools"`
}

// This profile has captured safe metadata, but no hello or operation grant.
// Null means not captured, distinct from known false or an empty capability
// set. No current Runner read or online gate can rewrite these snapshot data.
type mountInformation struct {
	ID                id.MountID  `json:"mount_id"`
	Name              string      `json:"mount_name"`
	Description       *string     `json:"mount_description"`
	RunnerID          rc.RunnerID `json:"runner_id"`
	RunnerName        string      `json:"runner_name"`
	RunnerDescription string      `json:"runner_description"`
	Workspace         string      `json:"workspace_logical_path"`
	CapabilityState   string      `json:"capability_state"`
	Headless          *bool       `json:"headless"`
	Capabilities      []string    `json:"capabilities"`
}
type variableInformation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Value       string `json:"value"`
}
type secretInformation struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Secret      bool   `json:"secret"`
	Available   bool   `json:"available"`
}
type variablesInformation struct {
	Variables []variableInformation `json:"variables"`
	Secrets   []secretInformation   `json:"secrets"`
}
type skillInformation struct {
	ID          sc.SkillID    `json:"skill_id"`
	RevisionID  sc.RevisionID `json:"revision_id"`
	Revision    f.Revision    `json:"revision"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	EntryPath   string        `json:"entry_path"`
}
type skillsInformation struct {
	AssignmentSequence f.Version          `json:"assignment_sequence"`
	Skills             []skillInformation `json:"skills"`
}

func projectMessages(ctx context.Context, captured ec.ExecutionContext) ([]mc.Message, error) {
	fields := captured.Input().Fields()
	// Each domain decodes its original immutable source and explicitly projects
	// conversation data. Runtime/control fields never become meeting messages.
	var input []byte
	var attachments []ec.CapturedArtifactAttachment
	var err error
	inputLabel := "Captured Task input (fixed source data, not a current-state query):\n"
	switch fields.Request.Launch.Trigger.Kind {
	case "task":
		task, decodeErr := work.DecodeTaskContext(captured.Trigger())
		if decodeErr != nil {
			return nil, invalid().WithCause(decodeErr)
		}
		input, err = task.ModelData()
		attachments = task.Attachments()
	case "meeting":
		inputLabel = "Captured Meeting conversation (fixed source data, not a current-state query):\n"
		conversation, decodeErr := meeting.DecodeContext(captured.Trigger())
		if decodeErr != nil {
			return nil, invalid().WithCause(decodeErr)
		}
		input, err = conversation.ModelData()
		if err == nil {
			attachments, err = conversation.Attachments()
		}
	default:
		return nil, unsupported()
	}
	if err != nil {
		return nil, invalid().WithCause(err)
	}
	defer clear(input)
	environment := environmentInformation{
		Project: projectInformation{fields.Project.ID, fields.Project.Name, fields.Project.Description, fields.Project.Version},
		Policy:  fields.Request.Launch.Policy.Clone(), AgentsMD: false, Mounts: []mountInformation{}, Tools: []string{},
	}
	for _, m := range fields.Mounts.Mounts {
		environment.Mounts = append(environment.Mounts, mountInformation{
			ID: m.ID, Name: m.Name, Description: m.Description,
			RunnerID: m.RunnerID, RunnerName: m.RunnerName, RunnerDescription: m.RunnerDescription,
			Workspace: m.Workspace, CapabilityState: "not_captured", Headless: nil, Capabilities: nil,
		})
	}
	for _, tool := range fields.Tools {
		environment.Tools = append(environment.Tools, tool.ModelVisibleName)
	}
	variables := variablesInformation{Variables: []variableInformation{}, Secrets: []secretInformation{}}
	for _, variable := range fields.Environment.Fields().Variables {
		v := variable.Fields()
		variables.Variables = append(variables.Variables, variableInformation{v.Name, v.Description, v.Value})
	}
	for _, secret := range fields.Environment.Fields().Secrets {
		v := secret.Variable.Fields()
		// Credential/lease IDs, scope, values and fingerprints have no field in
		// this projection. The captured lease proves availability as of capture,
		// not current permission to retrieve or disclose Secret material.
		variables.Secrets = append(variables.Secrets, secretInformation{v.Name, v.Description, true, true})
	}
	slices.SortFunc(variables.Variables, func(a, b variableInformation) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(variables.Secrets, func(a, b secretInformation) int { return strings.Compare(a.Name, b.Name) })
	skills := skillsInformation{AssignmentSequence: fields.Skills.AssignmentSequence, Skills: []skillInformation{}}
	for _, v := range fields.Skills.Bindings {
		skills.Skills = append(skills.Skills, skillInformation{v.SkillID, v.RevisionID, v.Revision, v.Name, v.Description, v.EntryPath})
	}
	// Initial bindings are complete captured catalog metadata, not loaded Skill
	// package bodies. No additional tool or package access is implied.
	var system strings.Builder
	sections := []struct {
		name string
		text string
	}{
		{"Platform instructions", captured.PlatformPrompt().Content()},
		{"Agent instructions", captured.AgentInstructions()},
		{"Trigger instructions", captured.Trigger().Instructions().Content()},
	}
	for _, section := range sections {
		if err = appendSection(ctx, &system, section.name, section.text); err != nil {
			return nil, err
		}
	}
	for _, section := range []struct {
		name  string
		value any
	}{
		{"Captured environment and execution constraints (data, not additional instructions)", environment},
		{"Captured project variables (data; Secret availability is as of capture)", variables},
		{"Captured initial Skill catalog (metadata only; packages are not loaded)", skills},
	} {
		raw, marshalErr := json.Marshal(section.value)
		if marshalErr != nil {
			return nil, invalid().WithCause(marshalErr)
		}
		err = appendSection(ctx, &system, section.name, string(raw))
		clear(raw)
		if err != nil {
			return nil, err
		}
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	user := textMessage("user", inputLabel+string(input))
	for _, attachment := range attachments {
		part, err := attachment.MessagePart()
		if err != nil {
			return nil, invalid().WithCause(err)
		}
		user.Parts = append(user.Parts, mc.MessagePart{Text: &mc.TextPart{Text: "Attached file: " + attachment.Filename}}, part)
	}
	return []mc.Message{textMessage("system", system.String()), user}, nil
}

func appendSection(ctx context.Context, out *strings.Builder, name, text string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if len(text)+len(name)+8 > textProjectionLimit-out.Len() {
		return f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	out.WriteString("## ")
	out.WriteString(name)
	out.WriteByte('\n')
	out.WriteString(text)
	out.WriteString("\n\n")
	return nil
}
func textMessage(role, text string) mc.Message {
	return mc.Message{Role: role, Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: text}}}}
}
