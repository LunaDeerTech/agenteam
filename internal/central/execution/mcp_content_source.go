package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

var _ c.MCPContentReadAuthority = (*Authority)(nil)

// This matcher consumes the actual running owner's input. It deliberately
// accepts only canonical File/Image parts of a Tool message: text mentioning a
// ref, a user-supplied ref, or a committed projection cannot authorize a read.
func mcpContentModelSource(r c.MCPContentReadRequest, facts directTextModelFacts) (mc.Message, error) {
	if r.Validate() != nil || facts.Snapshot.Validate() != nil || facts.Round == nil || facts.Round.Validate() != nil {
		return mc.Message{}, invalid()
	}
	input := facts.Snapshot.Fields().Context.Input().Fields().Request
	if input.ExecutionID != r.ExecutionID || input.Launch.ProjectID != r.ProjectID || input.Launch.AgentID.String() != r.Actor.Details().AgentID || facts.Round.Fields().ExecutionID != r.ExecutionID {
		return mc.Message{}, fault(f.Forbidden)
	}
	if _, ok := facts.Round.(loopExecutionRound); !ok {
		return mc.Message{}, fault(f.Forbidden)
	}
	var found *mc.Message
	for _, message := range facts.Round.Fields().Messages {
		if message.Role != "tool" {
			continue
		}
		matches := false
		for _, part := range message.Parts {
			if part.ToolResult == nil {
				continue
			}
			for _, result := range part.ToolResult.Parts {
				for _, media := range []*mc.MediaPart{result.File, result.Image} {
					if media != nil && media.Ref.Validate() == nil {
						a, b := media.Ref.Details(), r.Reference.Details()
						matches = matches || a.Kind == oc.MCPFile && a.ProjectID == b.ProjectID && a.FileID == b.FileID
					}
				}
			}
		}
		if matches {
			if found != nil {
				return mc.Message{}, fault(f.Forbidden)
			}
			copy := message.Clone()
			found = &copy
		}
	}
	if found == nil {
		return mc.Message{}, fault(f.Forbidden)
	}
	return *found, nil
}

func mcpContentDependencies(r c.MCPContentReadRequest, facts directTextModelFacts) (oc.AccessDependencies, error) {
	if _, err := mcpContentModelSource(r, facts); err != nil {
		return oc.AccessDependencies{}, err
	}
	// The request's default JSON is deliberately opaque. Bind every identity
	// explicitly, including the exact current Model round and original process.
	raw, err := json.Marshal(struct {
		Format, Actor, Project, Execution, File, Materialization, Config, Connection, Runtime, Operation, Attempt, Request, URI, Process string
		Revision                                                                                                                         f.Version
		Snapshot, Round                                                                                                                  f.Digest
	}{"execution-mcp-source-v1", r.Actor.Details().AgentID, r.ProjectID.String(), r.ExecutionID.String(), r.Reference.Details().FileID, r.MaterializationID, r.ConfigID, r.ConnectionID, r.RuntimeBindingID, r.OperationID.String(), r.AttemptID.String(), r.RequestID, r.URI, facts.ProcessID.String(), r.ConfigRevision, facts.Snapshot.Digest(), facts.Round.Digest()})
	if err != nil {
		return oc.AccessDependencies{}, unavailable(nil)
	}
	return oc.NewAccessDependencies(c.TriggerInputDigest(raw), facts.Locks)
}

func (a *Authority) DiscoverMCPContentRead(ctx context.Context, r c.MCPContentReadRequest) (oc.AccessDependencies, error) {
	if r.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	facts, err := a.mcpContentPlanningScope(ctx, r)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return mcpContentDependencies(r, facts)
}

func (a *Authority) mcpContentPlanningScope(ctx context.Context, r c.MCPContentReadRequest) (directTextModelFacts, error) {
	return a.mcpContentPlanning(ctx, r, false)
}

func (a *Authority) mcpContentPlanning(ctx context.Context, r c.MCPContentReadRequest, tail bool) (directTextModelFacts, error) {
	facts, err := a.directTextModelPlanningScope(ctx, false)
	if err != nil {
		return directTextModelFacts{}, err
	}
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return directTextModelFacts{}, err
	}
	s := run.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if run.phase != "calling" && !(tail && run.phase == "closing") || run.allJoined || run.invocation == nil {
		return directTextModelFacts{}, fault(f.Forbidden)
	}
	request, round := run.invocation.request.Request(), facts.Round.Fields()
	if !request.Actor.Equal(r.Actor) || request.CallID != round.CallID || !reflect.DeepEqual(request.Input, round.Input) || !mcpContentMessagesEqual(request.Messages, round.Messages) {
		return directTextModelFacts{}, fault(f.Forbidden)
	}
	return facts, ctx.Err()
}

func (a *Authority) ValidateMCPContentReadInTx(ctx context.Context, tx f.Tx, r c.MCPContentReadRequest, expected oc.AccessDependencies) error {
	if expected.Validate() != nil {
		return invalid()
	}
	actual, err := a.requireMCPContentRead(ctx, tx, r)
	if err != nil {
		return err
	}
	if !actual.Equal(expected) {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

func (a *Authority) RequireMCPContentReadInTx(ctx context.Context, tx f.Tx, r c.MCPContentReadRequest) error {
	_, err := a.requireMCPContentRead(ctx, tx, r)
	return err
}

func (a *Authority) requireMCPContentRead(ctx context.Context, tx f.Tx, r c.MCPContentReadRequest) (oc.AccessDependencies, error) {
	if r.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	// Planning's calling phase is required again. A live Tool dispatch or an
	// original closing/retirement call does not grant a new detached read.
	if _, err := a.mcpContentPlanningScope(ctx, r); err != nil {
		return oc.AccessDependencies{}, err
	}
	facts, err := a.directTextModelScope(ctx, tx, false)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if _, err = a.directTextCurrentAgent(ctx, tx); err != nil {
		return oc.AccessDependencies{}, err
	}
	return a.requireMCPContentProvenance(ctx, tx, r, facts)
}

func (a *Authority) requireMCPContentProvenance(ctx context.Context, tx f.Tx, r c.MCPContentReadRequest, facts directTextModelFacts) (oc.AccessDependencies, error) {
	message, err := mcpContentModelSource(r, facts)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	deps, err := mcpContentDependencies(r, facts)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	x, err := a.state.store.InTx(tx)
	if err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	var round, callID, invocation, messageDigest, transcriptDigest string
	var raw []byte
	var sequence int64
	err = x.QueryRow(ctx, `SELECT q.round_id::text,q.call_id,q.invocation_id::text,q.message_digest,t.digest,t.message,q.transcript_sequence FROM agenteam_execution.loop_tool_results q JOIN agenteam_execution.transcript_entries t ON (t.execution_id,t.sequence,t.round_id)=(q.execution_id,q.transcript_sequence,q.round_id) WHERE q.execution_id=$1 AND q.operation_id=$2 AND q.attempt_id=$3 AND q.request_id=$4`, r.ExecutionID.String(), r.OperationID.String(), r.AttemptID.String(), r.RequestID).Scan(&round, &callID, &invocation, &messageDigest, &transcriptDigest, &raw, &sequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	if err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	encoded, err := mc.EncodeCanonicalMessage(message)
	if err != nil {
		return oc.AccessDependencies{}, portError(err)
	}
	canonical, err := directTextJSON(json.RawMessage(encoded))
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if !bytes.Equal(canonical, raw) || messageDigest != string(c.TriggerInputDigest(raw)) || transcriptDigest != messageDigest || sequence < 1 || f.Sequence(sequence) > roundInputThrough(facts.Round) {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	original, err := loadDirectTextRound(ctx, x, r.ExecutionID, round)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if original == nil || original.snapshot.Digest() != facts.Snapshot.Digest() {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	binding, err := toolBinding(original)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if binding.CallID != callID || binding.InvocationID.String() != invocation || !binding.Actor.Equal(r.Actor) {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	if binding.MCPBinding.Validate() == nil {
		m := binding.MCPBinding
		if m.ProjectID != r.ProjectID || m.ConfigID != r.ConfigID || m.ConnectionID != r.ConnectionID || m.RuntimeBindingID != r.RuntimeBindingID || m.ConfigRevision != r.ConfigRevision {
			return oc.AccessDependencies{}, fault(f.Forbidden)
		}
	} else {
		if binding.Binding != (tc.BuiltinBinding{HandlerID: "mcp.read-resource", ContractRevision: 1}) {
			return oc.AccessDependencies{}, fault(f.Forbidden)
		}
		var args struct {
			Connection string `json:"connection_id"`
			URI        string `json:"uri"`
		}
		if decodeDirectTextJSON(original.toolPending.canonical, &args) != nil || args.Connection != r.ConnectionID || args.URI != r.URI {
			return oc.AccessDependencies{}, fault(f.Forbidden)
		}
	}
	return deps, ctx.Err()
}

// The shared durable codec binds exact business references while ordinary
// Message JSON stays redacted. Equality itself does not authorize a read.
func mcpContentMessagesEqual(a, b []mc.Message) bool {
	left, err := mc.EncodeCanonicalMessages(a)
	if err != nil {
		return false
	}
	right, err := mc.EncodeCanonicalMessages(b)
	return err == nil && bytes.Equal(left, right)
}

func (a *Authority) MCPContentReadActor(ctx context.Context) (id.Actor, error) {
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return id.Actor{}, err
	}
	s := run.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	if !run.started || run.phase != "calling" && run.phase != "closing" || run.allJoined || run.invocation == nil {
		return id.Actor{}, fault(f.Forbidden)
	}
	actor := run.invocation.request.Request().Actor
	if actor.Validate() != nil {
		return id.Actor{}, fault(f.Forbidden)
	}
	return actor, ctx.Err()
}

func mcpContentTailDependencies(r c.MCPContentReadRequest, access oc.AccessRequest, facts directTextModelFacts) (oc.AccessDependencies, error) {
	if access.Validate() != nil || facts.ProcessID.Validate() != nil || access.Details().Kind != oc.MaintenanceAccess || access.Details().Operation != oc.ReleaseReaderAccess || access.Details().InstanceID.String() != facts.ProcessID.String() {
		return oc.AccessDependencies{}, fault(f.Forbidden)
	}
	original, err := mcpContentDependencies(r, facts)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	d := access.Details()
	raw, err := json.Marshal(struct {
		Format                 string
		Mapping                f.Digest
		Object, Lease, Process string
	}{"execution-mcp-source-tail-v1", original.Mapping(), d.ObjectID.String(), d.LeaseID.String(), d.InstanceID.String()})
	if err != nil {
		return oc.AccessDependencies{}, invalid()
	}
	return oc.NewAccessDependencies(c.TriggerInputDigest(raw), facts.Locks)
}

func (a *Authority) DiscoverMCPContentReadTail(ctx context.Context, r c.MCPContentReadRequest, access oc.AccessRequest) (oc.AccessDependencies, error) {
	facts, err := a.mcpContentPlanning(ctx, r, true)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return mcpContentTailDependencies(r, access, facts)
}

func (a *Authority) ValidateMCPContentReadTailInTx(ctx context.Context, tx f.Tx, r c.MCPContentReadRequest, access oc.AccessRequest, expected oc.AccessDependencies) error {
	if expected.Validate() != nil {
		return invalid()
	}
	facts, err := a.mcpContentPlanning(ctx, r, true)
	if err != nil {
		return err
	}
	actual, err := mcpContentTailDependencies(r, access, facts)
	if err != nil {
		return err
	}
	if !actual.Equal(expected) {
		return fault(f.ConfirmationStale)
	}
	if err = a.state.store.RequireHeldLocks(ctx, tx, facts.Locks); err != nil {
		return portError(err)
	}
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return err
	}
	if run.owner.multi == nil || nilPort(run.owner.multi.guard) {
		return fault(f.DependencyUnbound)
	}
	if err = run.owner.multi.guard.RequireCurrentProcessInTx(ctx, tx, run.owner.multiProcess()); err != nil {
		return portError(err)
	}
	_, err = a.requireMCPContentProvenance(ctx, tx, r, facts)
	return err
}

var _ c.MCPContentReadTailAuthority = (*Authority)(nil)
