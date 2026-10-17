package memory

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// ModelAuthority is constructed before the Model resolver/runtime router. Its
// only invocation grants are private, live calls from the original Memory
// runtime. The static composition has no late Service or Execution setter.
type ModelAuthority struct {
	domain       *Service
	store        Store
	registration i.ServiceRegistration
	guard        *object.ProcessGuard
	process      oc.ProcessID
	issuer       mc.PlanIssuer
	mu           sync.Mutex
	calls        map[*modelInvocation]struct{}
}
type modelInvocation struct {
	authority  *ModelAuthority
	entry      *call
	id         mc.CallID
	actor      i.Actor
	consumer   mc.Consumer
	input      mc.InputIdentity
	owner      sc.CredentialLeaseOwner
	serving    *rc.ServingIdentity
	deadline   f.Instant
	grant      f.Digest
	terminal   bool
	resolved   *mc.ResolvedModel
	resolution *mc.ResolveRequest
	attempt    *mc.AttemptIdentity
}
type modelInvocationKey struct{}

func NewModelAuthority(domain *Service, registration i.ServiceRegistration) (*ModelAuthority, error) {
	if domain == nil {
		return nil, fault(f.DependencyUnbound)
	}
	a, err := newModelAuthority(domain.store, registration)
	if err != nil {
		return nil, err
	}
	a.domain = domain
	return a, nil
}

// NewUnboundModelAuthority breaks the static Model/Audit constructor cycle.
// Only NewModels may bind it once to the same Store before any invocation can
// be admitted. An unbound router cannot authorize a Consumer request.
func NewUnboundModelAuthority(store Store, registration i.ServiceRegistration) (*ModelAuthority, error) {
	if nilPort(store) {
		return nil, fault(f.DependencyUnbound)
	}
	return newModelAuthority(store, registration)
}

// NewProcessModelAuthority binds the same process that owns Model and Tool
// callbacks. An expected ID supports App construction before guard binding;
// every actual transaction still requires the live original guard.
func NewProcessModelAuthority(store Store, guard *object.ProcessGuard, registration i.ServiceRegistration, expected ...oc.ProcessID) (*ModelAuthority, error) {
	if nilPort(store) || guard == nil || len(expected) > 1 || len(expected) == 1 && expected[0].Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	process, err := guard.CurrentProcess()
	if len(expected) == 1 {
		if err == nil && process != expected[0] {
			return nil, fault(f.DependencyUnbound)
		}
		process = expected[0]
	} else if err != nil {
		return nil, portError(err)
	}
	a, err := newModelAuthority(store, registration)
	if err != nil {
		return nil, err
	}
	a.guard, a.process = guard, process
	return a, nil
}
func newModelAuthority(store Store, registration i.ServiceRegistration) (*ModelAuthority, error) {
	project, _ := f.ParseID[i.Project]("018f0000-0000-7000-8000-000000000001")
	scope, _ := i.InProject(project)
	test, err := registration.Actor(project.String(), scope)
	if err != nil || test.Details().ServiceName != i.MemoryRuntime {
		return nil, fault(f.DependencyUnbound)
	}
	return &ModelAuthority{store: store, registration: registration, issuer: mc.NewPlanIssuer(), calls: map[*modelInvocation]struct{}{}}, nil
}
func (a *ModelAuthority) bind(domain *Service) error {
	if a == nil || domain == nil || !sameStore(a.store, domain.store) {
		return fault(f.DependencyUnbound)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.domain != nil && a.domain != domain {
		return fault(f.DependencyUnbound)
	}
	a.domain = domain
	return nil
}
func (a *ModelAuthority) admit(ctx context.Context, entry *call, purpose mc.Purpose, input f.Digest, serving *rc.ServingIdentity, profile *mc.EmbeddingProfile) (context.Context, *modelInvocation, error) {
	if a == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	a.mu.Lock()
	domain := a.domain
	a.mu.Unlock()
	if domain == nil || a.guard == nil || a.process.Validate() != nil || entry == nil || entry.service != domain || ctx.Value(callKey{}) != entry || input.Validate() != nil {
		return nil, nil, fault(f.Forbidden)
	}
	switch purpose {
	case mc.MemoryExtraction, mc.MemoryConsolidation:
		if entry.intent != "retain" {
			return nil, nil, fault(f.Forbidden)
		}
	case mc.MemoryReflection:
		if entry.intent != "reflect" {
			return nil, nil, fault(f.Forbidden)
		}
	case mc.MemoryEmbedding:
		if serving != nil && serving.Validate() != nil {
			return nil, nil, fault(f.InvalidArgument)
		}
	default:
		return nil, nil, fault(f.Forbidden)
	}
	id, err := f.NewID[mc.Call]()
	if err != nil {
		return nil, nil, portError(err)
	}
	scope, _ := i.InProject(entry.namespace.ProjectID)
	actor, err := a.registration.Actor(entry.operation.String(), scope)
	if err != nil {
		return nil, nil, err
	}
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, id.String())
	if err != nil {
		return nil, nil, err
	}
	agent := entry.namespace.AgentID
	consumer := mc.Consumer{Kind: mc.MemoryConsumer, ProjectID: entry.namespace.ProjectID, AgentID: &agent, OperationID: entry.operation.String(), Purpose: purpose}
	deadline := time.Now().Add(120 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	bound, _ := f.NewInstant(deadline)
	inv := &modelInvocation{authority: a, entry: entry, id: id, actor: actor, consumer: consumer, input: mc.InputIdentity{OperationID: entry.operation.String(), Digest: input, SchemaVersion: 1}, owner: owner, deadline: bound}
	if serving != nil {
		copy := *serving
		inv.serving = &copy
	}
	raw, err := json.Marshal(struct {
		Call     mc.CallID
		Consumer mc.Consumer
		Input    mc.InputIdentity
	}{id, consumer, inv.input})
	if err != nil {
		return nil, nil, portError(err)
	}
	inv.grant = digest(raw)
	request := resolveRequest(inv)
	if profile != nil {
		if purpose != mc.MemoryEmbedding || serving != nil || profile.Validate() != nil {
			return nil, nil, fault(f.Forbidden)
		}
		version := profile.SelectionVersion
		request.Selection.Version = &version
	}
	source, sourceBinding, err := captureModelResolution(request)
	if err != nil {
		return nil, nil, err
	}
	inv.resolution = &request
	cause, err := readCause("model-call")
	if err != nil {
		return nil, nil, err
	}
	err = a.domain.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
			return portError(err)
		}
		_, err := x.Exec(ctx, `INSERT INTO agenteam_memory.model_calls(call_id,operation_id,project_id,agent_id,execution_id,purpose,input_digest,created_at,process_id,resolve_source,resolve_binding) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id.String(), entry.operation.String(), entry.namespace.ProjectID.String(), entry.namespace.AgentID.String(), entry.actor.Details().ExecutionID, string(purpose), string(input), now().Time(), a.process.String(), source, string(sourceBinding))
		return portError(err)
	})
	if err != nil {
		return nil, nil, err
	}
	a.mu.Lock()
	a.calls[inv] = struct{}{}
	a.mu.Unlock()
	return context.WithValue(ctx, modelInvocationKey{}, inv), inv, nil
}
func (a *ModelAuthority) forget(inv *modelInvocation) {
	a.mu.Lock()
	delete(a.calls, inv)
	a.mu.Unlock()
}
func (a *ModelAuthority) witness(ctx context.Context, r mc.ConsumerRequest) (*modelInvocation, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if a == nil || r.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	inv, _ := ctx.Value(modelInvocationKey{}).(*modelInvocation)
	if inv == nil || inv.authority != a || inv.entry.service != a.domain {
		return nil, fault(f.Forbidden)
	}
	a.mu.Lock()
	_, active := a.calls[inv]
	terminal := inv.terminal
	resolved := inv.resolved
	attempt := inv.attempt
	a.mu.Unlock()
	if !active || !r.Consumer.Equal(inv.consumer) || r.CallID == nil || *r.CallID != inv.id || !r.LeaseOwner.Equal(inv.owner) {
		return nil, fault(f.Forbidden)
	}
	preAdmission := r.Action == mc.RetireConsumer && r.TerminalVersion != nil
	technical := r.Action == mc.FinalizeConsumer || r.Action == mc.RetireConsumer && !preAdmission
	if preAdmission && (!terminal || *r.TerminalVersion != 1 || !r.Actor.Equal(inv.actor)) {
		return nil, fault(f.Forbidden)
	}
	if r.Action != mc.ResolveConsumer {
		if resolved == nil || r.SnapshotID != resolved.Snapshot.ID || (resolved.CredentialLease == nil) != (r.LeaseID == nil) || resolved.CredentialLease != nil && *r.LeaseID != resolved.CredentialLease.LeaseID {
			return nil, fault(f.Forbidden)
		}
	}
	if r.Attempt != nil && (r.Attempt.AttemptIndex != 1 || r.Attempt.Fence != 1 || r.Attempt.ProcessID != a.process || attempt != nil && *attempt != *r.Attempt) {
		return nil, fault(f.Forbidden)
	}
	if technical {
		d := r.Actor.Details()
		if d.Kind != i.Service || d.ServiceName != i.ModelRuntime || d.ProjectID != inv.consumer.ProjectID.String() || r.Attempt != nil && d.CauseRef != r.Attempt.InvocationID.String() || r.Action == mc.RetireConsumer && (attempt == nil || d.CauseRef != attempt.InvocationID.String()) {
			return nil, fault(f.Forbidden)
		}
	} else if !r.Actor.Equal(inv.actor) {
		return nil, fault(f.Forbidden)
	}
	if r.Input != nil && (r.Input.OperationID != inv.input.OperationID || r.Input.Digest != inv.input.Digest || r.Input.SchemaVersion != 1) {
		return nil, fault(f.Forbidden)
	}
	if r.Action == mc.ResolveConsumer {
		request := r.Resolve
		if request == nil || request.Purpose != inv.consumer.Purpose {
			return nil, fault(f.Forbidden)
		}
		if inv.serving != nil {
			if request.Source != mc.ServingSnapshotSource || request.ServingSnapshotID == nil || *request.ServingSnapshotID != inv.serving.SnapshotID || request.ServingGenerationID != inv.serving.GenerationID {
				return nil, fault(f.Forbidden)
			}
		} else {
			selector := mc.SummarySelector
			if inv.consumer.Purpose == mc.MemoryEmbedding {
				selector = mc.EmbeddingSelector
			}
			if request.Source != mc.CurrentSelectionSource || request.Selection == nil || request.Selection.Kind != "platform" || request.Selection.Selector != selector || request.ModelRef != nil {
				return nil, fault(f.Forbidden)
			}
		}
	}
	return inv, nil
}
func (a *ModelAuthority) Discover(ctx context.Context, r mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	if ctx != nil && ctx.Value(stoppedResolutionKey{}) != nil {
		return a.discoverStoppedRetirement(ctx, r)
	}
	inv, err := a.witness(ctx, r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.ConsumerBinding(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	key, _ := f.RecordLock(f.CommandRecordLock, "memory.model:"+inv.id.String())
	locks, err := normalizeLocks(append(slices.Clone(inv.entry.locks), namespaceLock(inv.entry.namespace, f.Shared), f.LockRequest{Key: key, Mode: f.Exclusive}))
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	max := f.Sequence(1)
	policy := mc.RetryPolicy{Class: mc.BoundedRetry, Deadline: &inv.deadline, MaxAttempts: &max, Categories: []mc.ErrorCategory{}}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: inv.grant, Locks: locks, RetryPolicy: &policy})
}
func (a *ModelAuthority) ValidateInTx(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	if ctx != nil && ctx.Value(stoppedResolutionKey{}) != nil {
		return a.validateStoppedRetirement(ctx, tx, r, plan)
	}
	inv, err := a.witness(ctx, r)
	if err != nil {
		return err
	}
	binding, err := mc.ConsumerBinding(r)
	if err != nil || !plan.Matches(a.issuer, binding, inv.grant) {
		return fault(f.Forbidden)
	}
	x, err := a.domain.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err := a.domain.store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return portError(err)
	}
	if a.guard == nil {
		return fault(f.DependencyUnbound)
	}
	if err := a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	recovery := r.Action == mc.ResolveConsumer && ctx.Value(modelRecoveryKey{}) == inv
	if r.Action != mc.FinalizeConsumer && r.Action != mc.RetireConsumer && !recovery {
		if err := a.domain.requireAccess(ctx, tx, inv.entry, false); err != nil {
			return err
		}
	}
	var operation, project, agent, execution, purpose, input string
	var snapshot, invocation, process, lease, attemptProcess *string
	var attemptIndex, attemptFence *int64
	var retired bool
	var terminal *int64
	err = x.QueryRow(ctx, `SELECT operation_id::text,project_id::text,agent_id::text,execution_id::text,purpose,input_digest,snapshot_id::text,invocation_id::text,retired,terminal_version,process_id::text,lease_id::text,attempt_index,attempt_process_id::text,attempt_fence FROM agenteam_memory.model_calls WHERE call_id=$1`, inv.id.String()).Scan(&operation, &project, &agent, &execution, &purpose, &input, &snapshot, &invocation, &retired, &terminal, &process, &lease, &attemptIndex, &attemptProcess, &attemptFence)
	if err != nil {
		return portError(err)
	}
	if process == nil || *process != a.process.String() || operation != inv.consumer.OperationID || project != inv.consumer.ProjectID.String() || agent != inv.consumer.AgentID.String() || execution != inv.entry.actor.Details().ExecutionID || purpose != string(inv.consumer.Purpose) || input != string(inv.input.Digest) {
		return fault(f.Forbidden)
	}
	if retired && r.Action != mc.RetireConsumer {
		return fault(f.Forbidden)
	}
	if snapshot != nil && *snapshot != r.SnapshotID.String() {
		return fault(f.Forbidden)
	}
	if r.Action == mc.ResolveConsumer {
		if recovery {
			return nil
		}
		if inv.serving != nil {
			var present bool
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_memory.namespaces n JOIN agenteam_memory.generations g ON g.generation_id=n.serving_generation_id WHERE n.project_id=$1 AND n.agent_id=$2 AND g.generation_id=$3 AND g.snapshot_id=$4 AND g.dimension=$5 AND g.status='ready' AND g.owner_version=1 AND g.retention_state='retained')`, project, agent, inv.serving.GenerationID, inv.serving.SnapshotID.String(), inv.serving.Dimension).Scan(&present)
			if err != nil {
				return portError(err)
			}
			if !present {
				return fault(f.InvalidState)
			}
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_memory.model_calls SET snapshot_id=$2 WHERE call_id=$1 AND (snapshot_id IS NULL OR snapshot_id=$2)`, inv.id.String(), r.SnapshotID.String())
		return portError(err)
	}
	if snapshot == nil {
		return fault(f.Forbidden)
	}
	if r.LeaseID != nil {
		if lease != nil && *lease != r.LeaseID.String() {
			return fault(f.Forbidden)
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_memory.model_calls SET lease_id=$2 WHERE call_id=$1 AND (lease_id IS NULL OR lease_id=$2)`, inv.id.String(), r.LeaseID.String()); err != nil {
			return portError(err)
		}
	} else if lease != nil {
		return fault(f.Forbidden)
	}
	if r.Attempt != nil {
		if attemptIndex != nil && (*attemptIndex != int64(r.Attempt.AttemptIndex) || attemptProcess == nil || *attemptProcess != r.Attempt.ProcessID.String() || attemptFence == nil || *attemptFence != int64(r.Attempt.Fence)) {
			return fault(f.Forbidden)
		}
		if invocation != nil && *invocation != r.Attempt.InvocationID.String() {
			return fault(f.Forbidden)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_memory.model_calls SET invocation_id=$2,attempt_index=$3,attempt_process_id=$4,attempt_fence=$5 WHERE call_id=$1 AND (invocation_id IS NULL OR invocation_id=$2)`, inv.id.String(), r.Attempt.InvocationID.String(), int64(r.Attempt.AttemptIndex), r.Attempt.ProcessID.String(), int64(r.Attempt.Fence))
		if err != nil {
			return portError(err)
		}
	}
	if r.Action == mc.RetireConsumer && r.TerminalVersion != nil {
		if terminal == nil || *terminal != 1 || invocation != nil || !r.Actor.Equal(inv.actor) {
			return fault(f.Forbidden)
		}
	} else if r.Action == mc.FinalizeConsumer || r.Action == mc.RetireConsumer {
		expected := ""
		if r.Attempt != nil {
			expected = r.Attempt.InvocationID.String()
		} else if invocation != nil {
			expected = *invocation
		}
		if expected == "" || r.Actor.Details().CauseRef != expected {
			return fault(f.Forbidden)
		}
	}
	if r.Attempt != nil {
		a.mu.Lock()
		if inv.attempt != nil && *inv.attempt != *r.Attempt {
			a.mu.Unlock()
			return fault(f.Forbidden)
		}
		copy := *r.Attempt
		inv.attempt = &copy
		a.mu.Unlock()
	}
	if r.Action == mc.RetireConsumer {
		_, err = x.Exec(ctx, `UPDATE agenteam_memory.model_calls SET retired=true WHERE call_id=$1`, inv.id.String())
		return portError(err)
	}
	return nil
}

var _ mc.ConsumerAuthority = (*ModelAuthority)(nil)
