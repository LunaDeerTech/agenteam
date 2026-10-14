package approval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
	"time"
)

type bindingPlain tc.ToolCallBinding
type inputPlain tc.ToolInputReference
type bindingWire struct {
	*bindingPlain
	Actor i.ActorDetails
	Input inputPlain
}

func bindingBytes(b tc.ToolCallBinding) ([]byte, error) {
	if b.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	v := bindingPlain(b)
	return json.Marshal(bindingWire{&v, b.Actor.Details(), inputPlain(b.Input)})
}
func decodeBinding(raw []byte) (tc.ToolCallBinding, error) {
	var v bindingPlain
	wire := bindingWire{bindingPlain: &v}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&wire) != nil {
		return tc.ToolCallBinding{}, fault(f.InvalidState)
	}
	actor, err := i.NewAgentRun(v.ProjectID, v.AgentID, v.ExecutionID)
	if err != nil || actor.Details() != wire.Actor {
		return tc.ToolCallBinding{}, fault(f.InvalidState)
	}
	v.Actor = actor
	v.Input = tc.ToolInputReference(wire.Input)
	b := tc.ToolCallBinding(v)
	canonical, err := bindingBytes(b)
	if err != nil || !bytes.Equal(raw, canonical) {
		return tc.ToolCallBinding{}, fault(f.InvalidState)
	}
	return b, nil
}
func sameBinding(a, b tc.ToolCallBinding) bool {
	x, e := bindingBytes(a)
	y, g := bindingBytes(b)
	return e == nil && g == nil && bytes.Equal(x, y)
}

type record struct {
	value                         c.Request
	operation                     tc.ApprovalOperation
	requestedEvent, resolvedEvent string
}

const columns = `id,project_id,agent_id,execution_id,tool_id,tool_call_id,operation_id,fingerprint,status,version,created_at,resolved_at,resolved_by,binding_data,requested_event_id,COALESCE(resolved_event_id,'')`

func scanRecord(row pgx.Row) (*record, error) {
	var r record
	var id, project, agent, execution, tool, operation string
	var resolvedBy *string
	var created time.Time
	var resolved *time.Time
	var raw []byte
	err := row.Scan(&id, &project, &agent, &execution, &tool, &r.value.ToolCallID, &operation, &r.value.Fingerprint, &r.value.Status, &r.value.Version, &created, &resolved, &resolvedBy, &raw, &r.requestedEvent, &r.resolvedEvent)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	r.operation.Binding, err = decodeBinding(raw)
	if err != nil {
		return nil, err
	}
	b := r.operation.Binding
	r.value.ToolName = b.RunnerBinding.OperationName
	r.value.ID, err = f.ParseID[c.RequestIdentity](id)
	if err != nil {
		return nil, fault(f.InvalidState)
	}
	r.value.ProjectID, r.value.AgentID, r.value.ExecutionID, r.value.ToolID = b.ProjectID, b.AgentID, b.ExecutionID, b.Spec.ToolID
	r.value.OperationID, err = f.ParseID[tc.Operation](operation)
	if err != nil {
		return nil, fault(f.InvalidState)
	}
	r.operation.OperationID, r.operation.Fingerprint = r.value.OperationID, r.value.Fingerprint
	r.value.CreatedAt, _ = f.NewInstant(created)
	if resolved != nil {
		at, _ := f.NewInstant(*resolved)
		r.value.ResolvedAt = &at
	}
	if resolvedBy != nil {
		u, err := f.ParseID[i.User](*resolvedBy)
		if err != nil {
			return nil, fault(f.InvalidState)
		}
		r.value.ResolvedBy = &u
	}
	if project != b.ProjectID.String() || agent != b.AgentID.String() || execution != b.ExecutionID.String() || tool != b.Spec.ToolID.String() || r.value.ToolCallID != b.CallID || r.value.Validate() != nil {
		return nil, fault(f.InvalidState)
	}
	return &r, nil
}
func loadByID(ctx context.Context, x postgres.SQLExecutor, id c.RequestID) (*record, error) {
	return scanRecord(x.QueryRow(ctx, `SELECT `+columns+` FROM agenteam_governance.approval_requests WHERE id=$1`, id.String()))
}
func loadByBinding(ctx context.Context, x postgres.SQLExecutor, b tc.ToolCallBinding) (*record, error) {
	r, err := scanRecord(x.QueryRow(ctx, `SELECT `+columns+` FROM agenteam_governance.approval_requests WHERE execution_id=$1 AND round_id=$2 AND tool_call_id=$3`, b.ExecutionID.String(), b.RoundID, b.CallID))
	if err != nil || r == nil {
		return r, err
	}
	if !sameBinding(r.operation.Binding, b) {
		return nil, fault(f.ConfirmationStale)
	}
	return r, nil
}
func insertRecord(ctx context.Context, x postgres.SQLExecutor, r *record) error {
	raw, err := bindingBytes(r.operation.Binding)
	if err != nil {
		return err
	}
	v := r.value
	b := r.operation.Binding
	r.value.ToolName = b.RunnerBinding.OperationName
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_governance.approval_requests(id,project_id,agent_id,execution_id,round_id,tool_id,tool_call_id,operation_id,fingerprint,status,version,created_at,binding_data,binding_digest,requested_event_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'pending',1,$10,$11,$12,$13)`, v.ID.String(), v.ProjectID.String(), v.AgentID.String(), v.ExecutionID.String(), b.RoundID, v.ToolID.String(), v.ToolCallID, v.OperationID.String(), string(v.Fingerprint), v.CreatedAt.Time(), raw, string(oc.DigestBytes(raw)), r.requestedEvent)
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.InvalidState)
	}
	return nil
}
