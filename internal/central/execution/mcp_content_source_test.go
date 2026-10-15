package execution

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func mcpSourceCandidate(t *testing.T) (directTextModelFacts, c.MCPContentReadRequest, agentloop.DirectTextRequest) {
	t.Helper()
	facts, consumer := runtimeModelCandidate(t)
	fields := facts.Round.Fields()
	project := facts.Snapshot.Fields().Context.Input().Fields().Request.Launch.ProjectID
	ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: project, FileID: newTestID[struct{}](t).String()})
	if err != nil {
		t.Fatal(err)
	}
	tool := mc.Tool{Name: "tool_01900000000070008000000000000901", InputSchema: []byte(`{"type":"object"}`)}
	fields.Messages = append(fields.Messages,
		mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &mc.ToolCallPart{ID: "original-call", Name: tool.Name, Arguments: []byte(`{}`)}}}},
		mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "original-call", Parts: []mc.ResultPart{{File: &mc.MediaPart{Ref: ref, MediaType: "application/octet-stream"}}}}}}},
	)
	r := c.MCPContentReadRequest{Actor: consumer.Actor, ProjectID: project, ExecutionID: fields.ExecutionID, Reference: ref, MaterializationID: newTestID[struct{}](t).String(), ConfigID: newTestID[struct{}](t).String(), ConnectionID: newTestID[struct{}](t).String(), RuntimeBindingID: newTestID[struct{}](t).String(), ConfigRevision: 1, OperationID: newTestID[tc.Operation](t), AttemptID: newTestID[tc.Attempt](t), RequestID: newTestID[struct{}](t).String(), URI: "repo://owned/binary"}
	request := mc.ModelRequest{Actor: consumer.Actor, CallID: fields.CallID, Consumer: consumer.Consumer, Model: facts.Snapshot.Fields().Context.Input().Fields().Model, Messages: fields.Messages, Tools: []mc.Tool{tool}, ToolChoice: mc.ToolChoice{Kind: "auto"}, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.AgentRetry}
	semantic, err := mc.NewSuccessorSemantic(context.Background(), mc.SuccessorSemanticParts{Actor: request.Actor, Model: request.Model, Messages: request.Messages, Tools: request.Tools, ToolChoice: request.ToolChoice, ResponseFormat: request.ResponseFormat, RetryClass: request.RetryClass})
	if err != nil {
		t.Fatal(err)
	}
	fields.Input.SchemaVersion, fields.Input.Digest = mc.SuccessorInputSchemaVersion, semantic.Digest()
	request.Input = fields.Input
	previous := newTestID[c.Round](t)
	round, err := c.NewLoopRound(c.LoopRoundFields{Round: fields, Tools: request.Tools, Choice: request.ToolChoice, Ordinal: 2, TranscriptStart: 4, Predecessor: &previous})
	if err != nil {
		t.Fatal(err)
	}
	facts.Round = loopExecutionRound{round}
	facts.Locks = []f.LockRequest{projectLock(project), agentLock(facts.Snapshot.Fields().Context.Input().Fields().Request.Launch.AgentID, f.Shared), executionLock(fields.ExecutionID)}
	fixed, err := agentloop.NewLoopRequest(context.Background(), fields.ContextDigest, request)
	if err != nil {
		t.Fatal(err)
	}
	return facts, r, fixed
}

func TestMCPContentSourceMatchesExactLiveInput(t *testing.T) {
	facts, request, _ := mcpSourceCandidate(t)
	if _, err := mcpContentModelSource(request, facts); err != nil {
		t.Fatal(err)
	}
	messages := facts.Round.Fields().Messages
	copy := make([]mc.Message, len(messages))
	for n := range messages {
		copy[n] = messages[n].Clone()
	}
	if !mcpContentMessagesEqual(messages, copy) {
		t.Fatal("copied function-backed canonical ref not equal")
	}
	copy[3].Parts[0].ToolResult.Parts[0].File.Ref, _ = oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.MCPFile, ProjectID: request.ProjectID, FileID: newTestID[struct{}](t).String()})
	if mcpContentMessagesEqual(messages, copy) {
		t.Fatal("substituted ref compared equal")
	}
	changed := request
	changed.Reference = copy[3].Parts[0].ToolResult.Parts[0].File.Ref
	if _, err := mcpContentModelSource(changed, facts); err == nil {
		t.Fatal("foreign content accepted")
	}
	plan, err := mcpContentDependencies(request, facts)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*c.MCPContentReadRequest){
		func(r *c.MCPContentReadRequest) { r.OperationID = newTestID[tc.Operation](t) },
		func(r *c.MCPContentReadRequest) { r.AttemptID = newTestID[tc.Attempt](t) },
		func(r *c.MCPContentReadRequest) { r.RequestID = newTestID[struct{}](t).String() },
		func(r *c.MCPContentReadRequest) { r.ConfigRevision++ },
		func(r *c.MCPContentReadRequest) { r.URI = "repo://other" },
	} {
		changed := request
		mutate(&changed)
		other, err := mcpContentDependencies(changed, facts)
		if err != nil || other.Equal(plan) {
			t.Fatal("plan failed to bind whole original provenance", err)
		}
	}
}

func TestMCPContentSourceRequiresPrivateCurrentModelOwner(t *testing.T) {
	facts, request, fixed := mcpSourceCandidate(t)
	control := newLaunchControl(t)
	if _, err := control.authority.DiscoverMCPContentRead(context.Background(), request); err == nil {
		t.Fatal("bare DTO granted source plan")
	}
	owner := &directTextState{authority: control.authority, store: control.authority.state.store, process: facts.ProcessID, processes: &preparationProcesses{process: facts.ProcessID}, calls: map[i.ExecutionID]*directTextCall{}}
	run := &directTextCall{owner: owner, request: facts.Snapshot.Fields().Context.Input().Fields().Request, snapshot: facts.Snapshot, round: facts.Round, runtimeLocks: facts.Locks, started: true, phase: "calling", invocation: &directTextInvocation{request: fixed}}
	owner.calls[request.ExecutionID] = run
	ctx := context.WithValue(context.Background(), directTextContextKey{}, run)
	if _, err := control.authority.DiscoverMCPContentRead(ctx, request); err != nil {
		t.Fatal("original private planning candidate rejected", err)
	}
	for _, phase := range []string{"starting", "tool-pending", "tool-writing", "closing"} {
		run.phase = phase
		if _, err := control.authority.DiscoverMCPContentRead(ctx, request); err == nil {
			t.Fatal("non-Model phase granted new source read", phase)
		}
	}
	run.phase = "calling"
	run.allJoined = true
	if _, err := control.authority.DiscoverMCPContentRead(ctx, request); err == nil {
		t.Fatal("retired invocation granted source read")
	}
	run.allJoined = false
	delete(owner.calls, request.ExecutionID)
	if _, err := control.authority.DiscoverMCPContentRead(ctx, request); err == nil {
		t.Fatal("unregistered original context granted source read")
	}
	if control.store.writes != 0 {
		t.Fatal("planning wrote state")
	}
}

func TestMCPContentSourceReaderTailDoesNotGrantNewRead(t *testing.T) {
	facts, request, fixed := mcpSourceCandidate(t)
	control := newLaunchControl(t)
	owner := &directTextState{authority: control.authority, store: control.authority.state.store, process: facts.ProcessID, processes: &preparationProcesses{process: facts.ProcessID}, calls: map[i.ExecutionID]*directTextCall{}}
	run := &directTextCall{owner: owner, request: facts.Snapshot.Fields().Context.Input().Fields().Request, snapshot: facts.Snapshot, round: facts.Round, runtimeLocks: facts.Locks, started: true, phase: "closing", invocation: &directTextInvocation{request: fixed}}
	owner.calls[request.ExecutionID] = run
	original, cancel := context.WithCancel(context.WithValue(context.Background(), directTextContextKey{}, run))
	cancel()
	cleanup := context.WithoutCancel(original)
	process, err := f.ParseID[oc.Process](facts.ProcessID.String())
	if err != nil {
		t.Fatal(err)
	}
	access, err := oc.NewMaintenanceAccess(oc.AccessRequestDetails{Operation: oc.ReleaseReaderAccess, InstanceID: process, ObjectID: newTestID[oc.StoredObject](t), LeaseID: newTestID[oc.Lease](t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = control.authority.DiscoverMCPContentRead(cleanup, request); err == nil {
		t.Fatal("closing call granted a new read")
	}
	actor, err := control.authority.MCPContentReadActor(cleanup)
	if err != nil || !actor.Equal(request.Actor) {
		t.Fatal("original cleanup lost identity", err)
	}
	plan, err := control.authority.DiscoverMCPContentReadTail(cleanup, request, access)
	if err != nil {
		t.Fatal("original reader tail rejected", err)
	}
	normal, err := mcpContentDependencies(request, facts)
	if err != nil || normal.Equal(plan) {
		t.Fatal("read plan and reader tail were interchangeable", err)
	}
	changed := access.Details()
	changed.LeaseID = newTestID[oc.Lease](t)
	other, _ := oc.NewMaintenanceAccess(changed)
	otherPlan, err := control.authority.DiscoverMCPContentReadTail(cleanup, request, other)
	if err != nil || otherPlan.Equal(plan) {
		t.Fatal("tail plan omitted exact Object lease", err)
	}
	changed.InstanceID = newTestID[oc.Process](t)
	other, _ = oc.NewMaintenanceAccess(changed)
	if _, err = control.authority.DiscoverMCPContentReadTail(cleanup, request, other); err == nil {
		t.Fatal("foreign Process borrowed reader tail")
	}
	changed = access.Details()
	changed.Operation = oc.FinishWriterAccess
	changed.LeaseID = oc.LeaseID{}
	changed.AttemptID = newTestID[oc.Attempt](t)
	other, _ = oc.NewMaintenanceAccess(changed)
	if _, err = control.authority.DiscoverMCPContentReadTail(cleanup, request, other); err == nil {
		t.Fatal("source reader tail authorized a writer")
	}
	run.allJoined = true
	if _, err = control.authority.DiscoverMCPContentReadTail(cleanup, request, access); err == nil {
		t.Fatal("joined Model retained cleanup context")
	}
	if _, err = control.authority.MCPContentReadActor(context.Background()); err == nil {
		t.Fatal("public caller obtained source identity")
	}
}
