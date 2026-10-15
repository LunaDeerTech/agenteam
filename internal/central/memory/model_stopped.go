package memory

import (
	"context"
	"encoding/json"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type stoppedModelRow struct {
	Call            mc.CallID        `json:"call"`
	Operation       tc.OperationID   `json:"operation"`
	Project         i.ProjectID      `json:"project"`
	Agent           i.AgentID        `json:"agent"`
	Execution       i.ExecutionID    `json:"execution"`
	Purpose         mc.Purpose       `json:"purpose"`
	Input           f.Digest         `json:"input"`
	Snapshot        *mc.SnapshotID   `json:"snapshot"`
	Invocation      *mc.InvocationID `json:"invocation"`
	Process         *oc.ProcessID    `json:"process"`
	Lease           *sc.LeaseID      `json:"lease"`
	AttemptIndex    *f.Sequence      `json:"attempt_index"`
	AttemptProcess  *oc.ProcessID    `json:"attempt_process"`
	AttemptFence    *f.Sequence      `json:"attempt_fence"`
	Retired         bool             `json:"retired"`
	TerminalVersion *f.Version       `json:"terminal_version"`
	ResolveSource   json.RawMessage  `json:"resolve_source"`
	ResolveBinding  *f.Digest        `json:"resolve_binding"`
}

func loadStoppedModelRow(ctx context.Context, x postgres.SQLExecutor, call mc.CallID) (stoppedModelRow, error) {
	var row stoppedModelRow
	var raw []byte
	err := x.QueryRow(ctx, `SELECT jsonb_build_object('call',call_id,'operation',operation_id,'project',project_id,'agent',agent_id,'execution',execution_id,
 'purpose',purpose,'input',input_digest,'snapshot',snapshot_id,'invocation',invocation_id,'process',process_id,'lease',lease_id,
 'attempt_index',attempt_index::text,'attempt_process',attempt_process_id,'attempt_fence',attempt_fence::text,'retired',retired,
 'terminal_version',terminal_version::text,'resolve_source',resolve_source,'resolve_binding',resolve_binding)
 FROM agenteam_memory.model_calls WHERE call_id=$1`, call.String()).Scan(&raw)
	if err != nil {
		return row, portError(err)
	}
	if json.Unmarshal(raw, &row) != nil || row.Call != call || row.Call.Validate() != nil || row.Operation.Validate() != nil || row.Project.Validate() != nil || row.Agent.Validate() != nil || row.Execution.Validate() != nil || row.Input.Validate() != nil {
		return row, fault(f.DependencyUnavailable)
	}
	if row.Process == nil || row.Process.Validate() != nil {
		return row, fault(f.ResourceBusy)
	}
	for _, valid := range []bool{row.Snapshot == nil || row.Snapshot.Validate() == nil, row.Invocation == nil || row.Invocation.Validate() == nil, row.Lease == nil || row.Lease.Validate() == nil} {
		if !valid {
			return row, fault(f.DependencyUnavailable)
		}
	}
	if row.Purpose != mc.MemoryEmbedding && row.Purpose != mc.MemoryExtraction && row.Purpose != mc.MemoryConsolidation && row.Purpose != mc.MemoryReflection {
		return row, fault(f.Forbidden)
	}
	if row.AttemptIndex != nil || row.AttemptProcess != nil || row.AttemptFence != nil {
		if row.Invocation == nil || row.AttemptIndex == nil || row.AttemptProcess == nil || row.AttemptFence == nil || *row.AttemptIndex != 1 || row.AttemptFence.Validate() != nil || *row.AttemptProcess != *row.Process {
			return row, fault(f.DependencyUnavailable)
		}
	}
	return row, nil
}
func (row stoppedModelRow) mapping() (f.Digest, error) {
	raw, err := json.Marshal(row)
	if err != nil {
		return "", portError(err)
	}
	return digest(raw), nil
}
func (row stoppedModelRow) request(a *ModelAuthority) (mc.StoppedCallObservationRequest, error) {
	if a == nil || row.Snapshot == nil || row.Snapshot.Validate() != nil {
		return mc.StoppedCallObservationRequest{}, fault(f.ResourceBusy)
	}
	scope, err := i.InProject(row.Project)
	if err != nil {
		return mc.StoppedCallObservationRequest{}, err
	}
	actor, err := a.registration.Actor(row.Operation.String(), scope)
	if err != nil {
		return mc.StoppedCallObservationRequest{}, err
	}
	agent := row.Agent
	request := mc.StoppedCallObservationRequest{Actor: actor, Consumer: mc.Consumer{Kind: mc.MemoryConsumer, ProjectID: row.Project, AgentID: &agent, OperationID: row.Operation.String(), Purpose: row.Purpose}, CallID: row.Call, Input: mc.InputIdentity{OperationID: row.Operation.String(), Digest: row.Input, SchemaVersion: 1}, SnapshotID: *row.Snapshot}
	return request, request.Validate()
}

type stoppedModelKey struct{}
type stoppedModelWitness struct {
	authority *ModelAuthority
	row       stoppedModelRow
	current   oc.ProcessID
	mapping   f.Digest
	request   mc.StoppedCallObservationRequest
	active    atomic.Bool
}

func (a *ModelAuthority) stoppedWitness(ctx context.Context, r mc.StoppedCallObservationRequest) (*stoppedModelWitness, error) {
	if a == nil || ctx == nil || r.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	w, _ := ctx.Value(stoppedModelKey{}).(*stoppedModelWitness)
	if w == nil || w.authority != a || !w.active.Load() || w.current != a.process || w.row.Process == nil || *w.row.Process == a.process {
		return nil, fault(f.Forbidden)
	}
	expected, err := mc.StoppedCallObservationBinding(w.request)
	actual, e := mc.StoppedCallObservationBinding(r)
	if err != nil || e != nil || expected != actual {
		return nil, fault(f.Forbidden)
	}
	return w, checkContext(ctx)
}
func (a *ModelAuthority) DiscoverStoppedCall(ctx context.Context, r mc.StoppedCallObservationRequest) (mc.ConsumerDependencies, error) {
	w, err := a.stoppedWitness(ctx, r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.StoppedCallObservationBinding(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	key, _ := f.RecordLock(f.CommandRecordLock, "memory.model:"+w.row.Call.String())
	project, _ := f.ProjectLock(w.row.Project.String())
	locks, err := normalizeLocks([]f.LockRequest{{Key: key, Mode: f.Exclusive}, {Key: project, Mode: f.Shared}, namespaceLock(c.Namespace{ProjectID: w.row.Project, AgentID: w.row.Agent}, f.Exclusive)})
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: w.mapping, Locks: locks})
}
func (a *ModelAuthority) ValidateStoppedCallInTx(ctx context.Context, tx f.Tx, r mc.StoppedCallObservationRequest, plan mc.ConsumerDependencies) error {
	w, err := a.stoppedWitness(ctx, r)
	if err != nil {
		return err
	}
	binding, err := mc.StoppedCallObservationBinding(r)
	if err != nil || !plan.Matches(a.issuer, binding, w.mapping) || plan.Details().RetryPolicy != nil {
		return fault(f.Forbidden)
	}
	if a.guard == nil {
		return fault(f.DependencyUnbound)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return portError(err)
	}
	if err := a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	row, err := loadStoppedModelRow(ctx, x, w.row.Call)
	if err != nil {
		return err
	}
	actual, err := row.mapping()
	if err != nil || actual != w.mapping {
		return fault(f.ConfirmationStale)
	}
	return checkContext(ctx)
}

var _ mc.StoppedCallAuthority = (*ModelAuthority)(nil)
