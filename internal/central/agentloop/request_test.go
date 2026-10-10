package agentloop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution/prompt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	secret "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	skill "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func requestID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	id, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	requestOK(t, err)
	return id
}
func requestOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Only controlled codec material: these values are not a real preparation
// commit, running Execution, persisted Round, Consumer grant or Model call.
func requestFields(t *testing.T, purpose string) ec.PreparationInputFields {
	t.Helper()
	p, a, e := requestID[i.Project](t, 1), requestID[i.Agent](t, 2), requestID[i.Execution](t, 3)
	now, err := f.ParseInstant("2026-10-10T12:00:00.123456Z")
	requestOK(t, err)
	modelID, secretID := requestID[mc.Model](t, 4), requestID[i.ProjectVariable](t, 5)
	agent, err := ac.NewAgentConfig(ac.AgentConfigFields{Core: ac.AgentCore{ID: a, ProjectID: p, Name: "loop-agent", NormalizedName: "loop-agent", Description: "Agent description", Instructions: "fixed-agent-instructions", InjectAgentsMD: false, ModelRef: modelID, ApprovalPolicy: ac.ApprovalDefault, Lifecycle: ac.AgentActive, Version: 2, CreatedAt: now, UpdatedAt: now}, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{secretID}})
	requestOK(t, err)
	taskID, sprintID, milestoneID := requestID[wc.Task](t, 6), requestID[pc.Sprint](t, 7), requestID[wc.Milestone](t, 8)
	request := ec.PreparationRequest{ExecutionID: e, Launch: ec.LaunchRequest{ProjectID: p, AgentID: a, Trigger: ec.Trigger{Kind: "task", TaskID: taskID.String()}, Purpose: purpose, Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}, Meta: f.CommandMeta{RequestID: requestID[f.Request](t, 9), IdempotencyKey: "fixed-loop-input"}}}
	state := wc.TaskStateInProgress
	if purpose == "task/review" {
		state = wc.TaskStateInReview
	}
	task := wc.Task{ID: taskID, ProjectID: p, MilestoneID: milestoneID, SprintID: sprintID, Title: "captured-task-title", Description: "captured-task-description", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityHigh, State: state, AssigneeAgentID: &a, Plan: "captured-task-plan", ManualRank: "80000000000000000000000000000000", Version: 3, CreatedAt: now, UpdatedAt: now}
	started := wc.ActorHistory{Kind: i.Human, UserID: requestID[i.User](t, 10).String()}
	sprint := wc.Sprint{ID: sprintID, ProjectID: p, MilestoneID: milestoneID, Title: "captured-sprint", Description: "sprint-description", ManualRank: "80000000000000000000000000000000", Version: 2, CreatedAt: now, UpdatedAt: now, StartedAt: &now, StartedBy: &started, State: wc.Current}
	milestone := wc.Milestone{ID: milestoneID, ProjectID: p, Title: "captured-milestone", Description: "milestone-description", ManualRank: "80000000000000000000000000000000", Version: 1, CreatedAt: now, UpdatedAt: now}
	payload, err := json.Marshal(wc.TaskCreatedPayload{InitialState: wc.TaskStateBacklog, MilestoneID: milestoneID, SprintID: sprintID, Type: task.Type, Priority: task.Priority})
	requestOK(t, err)
	command := requestID[wc.TaskCommand](t, 11)
	history := wc.TaskEvent{ID: requestID[wc.TaskEvent](t, 12), ProjectID: p, TaskID: taskID, TaskVersion: 1, Type: wc.TaskEventCreated, Actor: wc.TaskEventActor{Type: i.Human, UserID: requestID[i.User](t, 10), Source: "task_domain"}, OperationID: command, CorrelationID: command, Payload: payload, CreatedAt: now}
	historyBytes, err := json.Marshal(history)
	requestOK(t, err)
	launchDigest, err := request.Launch.Digest()
	requestOK(t, err)
	// Use Work's formal decoder on a closed, versioned source. No mock builder
	// accepts malformed Task material or invents a successful provider call.
	triggerBytes, err := json.Marshal(struct {
		SchemaVersion f.Version     `json:"schema_version"`
		ExecutionID   i.ExecutionID `json:"execution_id"`
		LaunchDigest  f.Digest      `json:"launch_digest"`
		Input         struct {
			SchemaVersion      f.Version         `json:"schema_version"`
			Task               wc.Task           `json:"task"`
			Sprint             wc.Sprint         `json:"sprint"`
			Milestone          wc.Milestone      `json:"milestone"`
			Purpose            string            `json:"purpose"`
			UnresolvedBlockers []wc.TaskBlocker  `json:"unresolved_blockers"`
			RecentTaskEvents   []json.RawMessage `json:"recent_task_events"`
		} `json:"input"`
	}{SchemaVersion: 1, ExecutionID: e, LaunchDigest: launchDigest, Input: struct {
		SchemaVersion      f.Version         `json:"schema_version"`
		Task               wc.Task           `json:"task"`
		Sprint             wc.Sprint         `json:"sprint"`
		Milestone          wc.Milestone      `json:"milestone"`
		Purpose            string            `json:"purpose"`
		UnresolvedBlockers []wc.TaskBlocker  `json:"unresolved_blockers"`
		RecentTaskEvents   []json.RawMessage `json:"recent_task_events"`
	}{1, task, sprint, milestone, purpose, []wc.TaskBlocker{}, []json.RawMessage{historyBytes}}})
	requestOK(t, err)
	trigger, err := ec.NewCapturedTriggerInput(ec.TriggerInputRef{ProviderType: "task", SchemaVersion: 1, InputID: requestID[ec.TriggerInput](t, 13), Digest: ec.TriggerInputDigest(triggerBytes)}, triggerBytes)
	requestOK(t, err)
	scope, err := i.InProject(p)
	requestOK(t, err)
	modelRef, err := secret.NewCredentialRef(requestID[secret.Credential](t, 14), scope)
	requestOK(t, err)
	envRef, err := secret.NewCredentialRef(requestID[secret.Credential](t, 15), scope)
	requestOK(t, err)
	owner, err := secret.NewCredentialLeaseOwner(secret.ExecutionOwner, e.String())
	requestOK(t, err)
	model := mc.ResolvedModel{Snapshot: mc.ConfigSnapshot{ID: requestID[mc.Snapshot](t, 16), Identity: mc.ModelIdentity{ProviderID: requestID[mc.Provider](t, 17), ModelID: modelID, ProviderName: "provider", ModelName: "chat", ProviderModelID: "upstream", Protocol: mc.OpenAIChat, Profile: mc.OpenAIChatV1, ModelType: mc.ChatModel, AdapterRevision: adapter.OpenAIChatTextRevision}, Endpoint: "https://private-provider.example/v1", Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), HeaderOverwrite: map[string]string{}, Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}, CredentialRef: &modelRef}, Consumer: mc.Consumer{Kind: mc.AgentConsumer, Purpose: mc.AgentGeneration, ProjectID: p, AgentID: &a, ExecutionID: &e}, LeaseOwner: owner, CredentialLease: &secret.CredentialLease{LeaseID: requestID[secret.Lease](t, 18), CredentialRef: modelRef}}
	variable, err := pv.NewVariable(pv.VariableFields{ID: requestID[i.ProjectVariable](t, 19), ProjectID: p, Type: pv.VariableType, Name: "ORDINARY_SETTING", Description: "ordinary-description", Value: "ordinary-value", Version: 1, CreatedAt: now, UpdatedAt: now})
	requestOK(t, err)
	secretVariable, err := pv.NewSecretVariable(pv.SecretVariableFields{ID: secretID, ProjectID: p, Type: pv.SecretVariableType, Name: "SECRET_TOKEN", Description: "secret-description", Version: 1, CreatedAt: now, UpdatedAt: now})
	requestOK(t, err)
	binding := ec.TriggerInputDigest([]byte("original-attempt-binding"))
	environment, err := pv.NewEnvironmentCapture(pv.EnvironmentCaptureFields{Request: pv.EnvironmentCaptureRequest{ProjectID: p, AgentID: a, ExecutionID: e}, AttemptBinding: binding, AgentVersion: 2, Variables: []pv.Variable{variable}, Secrets: []pv.ExecutionSecretVariable{{Variable: secretVariable, CredentialRef: envRef, LeaseID: requestID[secret.Lease](t, 20)}}})
	requestOK(t, err)
	return ec.PreparationInputFields{Request: request, AttemptBinding: binding, CapturedAt: now, Project: pc.ProjectRef{ID: p, OwnerUserID: requestID[i.User](t, 10), Name: "loop-project", NormalizedName: "loop-project", Description: "captured-project-description", Lifecycle: pc.Active, Version: 1, CurrentSprintID: &sprintID, CreatedAt: now, UpdatedAt: now}, Agent: agent, Trigger: trigger, Model: model, Tools: []tc.ExecutionTool{}, Skills: skill.InitialSkillBindings{Request: skill.SkillCaptureRequest{ProjectID: p, AgentID: a, ExecutionID: e}, AssignmentSequence: 1, Bindings: []skill.SkillBinding{{SkillID: requestID[pc.Skill](t, 21), RevisionID: requestID[skill.Revision](t, 22), Revision: 1, AssignmentID: requestID[skill.Assignment](t, 23), AssignmentSequence: 1, Name: "Example Skill", Description: "captured-skill-description", PackageSHA256: ec.TriggerInputDigest([]byte("package")), EntryPath: skill.EntryPath}}}, Environment: environment, Mounts: mt.ExecutionMountSet{Request: mt.ExecutionMountCaptureRequest{ProjectID: p, AgentID: a, ExecutionID: e}, AgentVersion: 2, Mounts: []mt.ExecutionMountMetadata{}}, PlatformPrompt: prompt.Current()}
}
func requestContext(t *testing.T, fields ec.PreparationInputFields) ec.ExecutionContext {
	t.Helper()
	input, err := ec.NewPreparationInput(fields)
	requestOK(t, err)
	trigger, err := (work.TaskContextBuilder{}).BuildTriggerContext(context.Background(), fields.Request, fields.Trigger)
	requestOK(t, err)
	captured, err := ec.NewExecutionContext(input, trigger)
	requestOK(t, err)
	return captured
}
func requestCandidate(t *testing.T, captured ec.ExecutionContext) DirectTextRequest {
	t.Helper()
	r := captured.Input().Fields().Request
	actor, err := i.NewAgentRun(r.Launch.ProjectID, r.Launch.AgentID, r.ExecutionID)
	requestOK(t, err)
	value, err := BuildDirectTextRequest(context.Background(), captured, actor, requestID[ec.Round](t, 30).String(), requestID[mc.Call](t, 31))
	requestOK(t, err)
	return value
}

func TestDirectTextRequestProjectsCapturedSources(t *testing.T) {
	for _, purpose := range []string{"task/work", "task/review"} {
		fields := requestFields(t, purpose)
		captured := requestContext(t, fields)
		value := requestCandidate(t, captured)
		r := value.Request()
		if value.Validate() != nil || r.Validate() != nil || value.ContextDigest() != captured.Digest() || r.RetryClass != mc.AgentRetry || !r.Model.LeaseOwner.Equal(fields.Model.LeaseOwner) || r.CallID != requestID[mc.Call](t, 31) || r.Input.RoundID != requestID[ec.Round](t, 30).String() || *r.Input.ExecutionID != fields.Request.ExecutionID || !r.Consumer.Equal(fields.Model.Consumer) {
			t.Fatal("original model/identity binding lost")
		}
		if len(r.Tools) != 0 || r.ToolChoice.Kind != "none" || r.ResponseFormat.Kind != "text" || len(r.Messages) != 2 || r.Messages[0].Role != "system" || r.Messages[1].Role != "user" || len(r.Messages[0].Parts) != 1 || len(r.Messages[1].Parts) != 1 {
			t.Fatal("wrong direct-text request shape")
		}
		system, input := r.Messages[0].Parts[0].Text.Text, r.Messages[1].Parts[0].Text.Text
		last := -1
		for _, component := range []string{prompt.Current().Content(), "fixed-agent-instructions", captured.Trigger().Instructions().Content(), "Captured environment and execution constraints", "Captured project variables", "Captured initial Skill catalog"} {
			position := strings.Index(system, component)
			if position < 0 || position <= last {
				t.Fatal("missing or reordered prompt component")
			}
			last = position
		}
		for _, text := range []string{"ordinary-value", "ORDINARY_SETTING", "SECRET_TOKEN", "secret-description", `"secret":true`, `"available":true`, "Example Skill", "captured-skill-description", "captured-project-description", `"mounts":[]`, `"tools":[]`} {
			if !strings.Contains(system, text) {
				t.Fatal("captured model-visible data omitted", text)
			}
		}
		for _, text := range []string{"captured-task-title", "captured-task-description", "captured-task-plan", "captured-sprint", "captured-milestone", "recent_task_events", "task_created", purpose} {
			if !strings.Contains(input, text) {
				t.Fatal("fixed task semantic input omitted", text)
			}
		}
		if strings.Contains(input, prompt.Current().Content()) || value.InitialInput().Parts[0].Text.Text != input {
			t.Fatal("System Prompt duplicated into input candidate")
		}
		for _, hidden := range []string{fields.Model.Snapshot.Endpoint, fields.Model.Snapshot.ID.String(), fields.Model.CredentialLease.LeaseID.String(), fields.Model.Snapshot.CredentialRef.Details().ID.String(), fields.Environment.Fields().Secrets[0].LeaseID.String(), fields.Environment.Fields().Secrets[0].CredentialRef.Details().ID.String(), "credential_ref", "lease_id", "attempt_binding", "package_sha256"} {
			if strings.Contains(system+input, hidden) {
				t.Fatal("backend-only material reached model messages")
			}
		}
		// Independent encoding of the published Model format, not an expected
		// value obtained by calling the same digest function as the builder.
		raw, err := json.Marshal(struct {
			Format         int               `json:"format"`
			Messages       []mc.Message      `json:"messages"`
			ToolChoice     mc.ToolChoice     `json:"tool_choice"`
			ResponseFormat mc.ResponseFormat `json:"response_format"`
		}{1, r.Messages, mc.ToolChoice{Kind: "none"}, mc.ResponseFormat{Kind: "text"}})
		requestOK(t, err)
		sum := sha256.Sum256(raw)
		if r.Input.SchemaVersion != 1 || r.Input.Digest != f.Digest("sha256:"+hex.EncodeToString(sum[:])) {
			t.Fatal("Model input digest drifted")
		}
		again := requestCandidate(t, captured).Request()
		if again.Input.Digest != r.Input.Digest || again.Messages[0].Parts[0].Text.Text != system || again.Messages[1].Parts[0].Text.Text != input {
			t.Fatal("unchanged context produced mutable input")
		}
	}
}

func TestDirectTextRequestRejectsUnsupportedProfiles(t *testing.T) {
	for _, mode := range []string{"tools", "effort", "tool-capability", "reasoning", "parameters", "request-overwrite", "header-overwrite", "image-input", "structured-output", "adapter", "no-credential"} {
		fields := requestFields(t, "task/work")
		s := &fields.Model.Snapshot
		switch mode {
		case "tools":
			tool := requestID[i.Tool](t, 40)
			a := fields.Agent.Fields()
			a.AllowedToolIDs = []i.ToolID{tool}
			var err error
			fields.Agent, err = ac.NewAgentConfig(a)
			requestOK(t, err)
			fields.Tools = []tc.ExecutionTool{{ToolID: tool, SpecRevision: 1, ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), BindingSnapshot: tc.BuiltinExecutionBinding{Binding: tc.BuiltinBinding{HandlerID: "skill.install", ContractRevision: 1}, ScopeResolverID: "skill.scope", RiskClassifierID: "skill.risk", Class: tc.OrdinaryTool}}}
		case "effort":
			a, effort := fields.Agent.Fields(), "high"
			a.Core.ReasoningEffort = &effort
			var err error
			fields.Agent, err = ac.NewAgentConfig(a)
			requestOK(t, err)
		case "tool-capability":
			s.Capabilities.ToolCalls = true
		case "reasoning":
			s.Capabilities.Reasoning = true
			s.Capabilities.ReasoningEfforts = []string{"high"}
		case "parameters":
			s.Parameters = json.RawMessage(`{"temperature":1}`)
		case "request-overwrite":
			s.RequestOverwrite = json.RawMessage(`{"temperature":1}`)
		case "header-overwrite":
			s.HeaderOverwrite = map[string]string{"X-Custom": "value"}
		case "image-input":
			s.Capabilities.InputModalities = []string{"text", "image"}
		case "structured-output":
			s.Capabilities.StructuredOutputModes = []string{"json_schema"}
		case "adapter":
			s.Identity.AdapterRevision = adapter.OpenAIChatStructuredRevision
		case "no-credential":
			s.CredentialRef, fields.Model.CredentialLease = nil, nil
		}
		captured := requestContext(t, fields)
		actor, err := i.NewAgentRun(fields.Request.Launch.ProjectID, fields.Request.Launch.AgentID, fields.Request.ExecutionID)
		requestOK(t, err)
		out, err := BuildDirectTextRequest(context.Background(), captured, actor, requestID[ec.Round](t, 30).String(), requestID[mc.Call](t, 31))
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.CapabilityUnsupported || out.Validate() == nil {
			t.Fatal("unsupported input was adapted silently", mode, err)
		}
	}
}

func TestDirectTextRequestKeepsIsolationAndRejectsIdentityAndCancellation(t *testing.T) {
	fields := requestFields(t, "task/work")
	captured := requestContext(t, fields)
	value := requestCandidate(t, captured)
	original := value.Request()
	copy := value.Request()
	copy.Messages[0].Parts[0].Text.Text = "changed"
	copy.Messages[1].Parts[0].Text.Text = "changed"
	copy.Model.Snapshot.Parameters[0] = '!'
	*copy.Input.ExecutionID = requestID[i.Execution](t, 60)
	copy.Model.CredentialLease.LeaseID = requestID[secret.Lease](t, 61)
	initial := value.InitialInput()
	initial.Parts[0].Text.Text = "changed"
	got := value.Request()
	if got.Messages[0].Parts[0].Text.Text != original.Messages[0].Parts[0].Text.Text || got.Messages[1].Parts[0].Text.Text != original.Messages[1].Parts[0].Text.Text || !bytes.Equal(got.Model.Snapshot.Parameters, original.Model.Snapshot.Parameters) || *got.Input.ExecutionID != *original.Input.ExecutionID || got.Model.CredentialLease.LeaseID != original.Model.CredentialLease.LeaseID {
		t.Fatal("candidate aliased an explicit caller projection")
	}
	encoded, err := json.Marshal(value)
	requestOK(t, err)
	for _, display := range []string{string(encoded), fmt.Sprintf("%+v %#v", value, value), value.LogValue().String()} {
		if strings.Contains(display, "fixed-agent") || strings.Contains(display, "ordinary-value") || strings.Contains(display, "captured-task") || strings.Contains(display, fields.Model.CredentialLease.LeaseID.String()) {
			t.Fatal("implicit candidate output leaked material")
		}
	}
	if string(encoded) != `"agent_text_request"` || json.Unmarshal(encoded, new(DirectTextRequest)) == nil {
		t.Fatal("candidate became a public serialization capability")
	}
	for _, mode := range []string{"human", "project", "agent", "execution", "round", "call", "zero-context", "nil-context", "cancel", "unknown-scene", "malformed-task"} {
		p, a, e := fields.Request.Launch.ProjectID, fields.Request.Launch.AgentID, fields.Request.ExecutionID
		ctx, cancel := context.WithCancel(context.Background())
		var callContext context.Context = ctx
		input, round, call := captured, requestID[ec.Round](t, 30).String(), requestID[mc.Call](t, 31)
		switch mode {
		case "project":
			p = requestID[i.Project](t, 70)
		case "agent":
			a = requestID[i.Agent](t, 71)
		case "execution":
			e = requestID[i.Execution](t, 72)
		case "round":
			round = "unpersisted-not-an-id"
		case "call":
			call = mc.CallID{}
		case "zero-context":
			input = ec.ExecutionContext{}
		case "nil-context":
			callContext = nil
		case "cancel":
			cancel()
		case "unknown-scene", "malformed-task":
			component, xerr := ec.NewPromptComponent("unknown/task/v1", "private-material-canary")
			requestOK(t, xerr)
			source := fields.Trigger
			if mode == "malformed-task" {
				raw := []byte(`{"incomplete":true}`)
				ref := source.Ref()
				ref.Digest = ec.TriggerInputDigest(raw)
				source, xerr = ec.NewCapturedTriggerInput(ref, raw)
				requestOK(t, xerr)
			}
			d := fields
			d.Trigger = source
			base, xerr := ec.NewPreparationInput(d)
			requestOK(t, xerr)
			trigger, xerr := ec.NewTriggerContext(fields.Request, source, component)
			requestOK(t, xerr)
			input, xerr = ec.NewExecutionContext(base, trigger)
			requestOK(t, xerr)
		}
		actor, err := i.NewAgentRun(p, a, e)
		requestOK(t, err)
		if mode == "human" {
			actor, err = i.NewHuman(requestID[i.User](t, 10), requestID[i.Session](t, 73))
			requestOK(t, err)
		}
		out, err := BuildDirectTextRequest(callContext, input, actor, round, call)
		cancel()
		if err == nil || out.Validate() == nil || strings.Contains(err.Error(), "private-material") {
			t.Fatal("invalid/foreign candidate returned", mode, err)
		}
		if mode == "cancel" && !errors.Is(err, context.Canceled) {
			t.Fatal("original cancellation lost", err)
		}
	}
}
