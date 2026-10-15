package knowledge

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (a *IndexAuthority) operation(ctx context.Context, request mc.ConsumerRequest) (*indexOperation, error) {
	if ctx == nil || request.Validate() != nil || a == nil {
		return nil, fault(f.Forbidden)
	}
	op, ok := ctx.Value(indexOperationKey{}).(*indexOperation)
	if !ok || op == nil || op.service == nil || op.service.authority != a {
		return nil, fault(f.Forbidden)
	}
	s := op.service
	s.mu.Lock()
	_, active := s.owners[op]
	s.mu.Unlock()
	if !active || request.Consumer.Kind != mc.KnowledgeConsumer || request.Consumer.Purpose != mc.KnowledgeEmbedding || request.Consumer.ProjectID != op.profile.Project || request.Consumer.OperationID != op.id || request.CallID != nil && *request.CallID != op.call || request.Input != nil && (request.Input.Digest != op.input || request.Input.SchemaVersion != 1 || request.Input.OperationID != op.id) {
		return nil, fault(f.Forbidden)
	}
	if err := op.binding.authorize(op, request, false); err != nil {
		return nil, err
	}
	if request.Resolve != nil {
		r := request.Resolve
		if op.profile.Snapshot.Validate() == nil {
			if r.Source != mc.ServingSnapshotSource || r.ServingSnapshotID == nil || *r.ServingSnapshotID != op.profile.Snapshot || r.ServingGenerationID != op.profile.ID {
				return nil, fault(f.Forbidden)
			}
		} else if op.kind != "profile" || r.Source != mc.CurrentSelectionSource || r.Selection == nil || r.Selection.Kind != "platform" || r.Selection.Selector != mc.EmbeddingSelector {
			return nil, fault(f.Forbidden)
		}
	}
	return op, nil
}

type operationFacts struct {
	ID, Project, Kind, Job, Profile, Generation, Snapshot, Call, Process, Digest, State string
	Version                                                                             int64
}

func operationRecord(ctx context.Context, x postgres.SQLExecutor, op *indexOperation) (operationFacts, error) {
	var v operationFacts
	err := x.QueryRow(ctx, `SELECT id::text,project_id::text,kind,COALESCE(job_id::text,''),profile_id::text,generation_id::text,COALESCE(snapshot_id::text,''),call_id::text,process_id::text,input_digest,state,version FROM agenteam_knowledge.model_operations WHERE id=$1 AND project_id=$2`, op.id, op.profile.Project.String()).Scan(&v.ID, &v.Project, &v.Kind, &v.Job, &v.Profile, &v.Generation, &v.Snapshot, &v.Call, &v.Process, &v.Digest, &v.State, &v.Version)
	if err != nil {
		return v, unavailable(err)
	}
	if v.ID != op.id || v.Project != op.profile.Project.String() || v.Kind != op.kind || v.Job != op.job || v.Profile != op.profile.ID || v.Generation != op.profile.ID || v.Call != op.call.String() || v.Process != op.service.authority.process.String() || v.Digest != string(op.input) {
		return v, fault(f.Forbidden)
	}
	return v, nil
}

func (a *IndexAuthority) Discover(ctx context.Context, request mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	if a == nil {
		return mc.ConsumerDependencies{}, fault(f.DependencyUnbound)
	}
	return a.discoverWith(ctx, a.store, request)
}

func (a *IndexAuthority) discoverWith(ctx context.Context, x postgres.SQLExecutor, request mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	op, err := a.operation(ctx, request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	facts, err := operationRecord(ctx, x, op)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	locks, err := indexLocks(op.profile.Project, false)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	if requiresCurrentConsumer(request.Action) && !indexResolutionLookup(ctx, op, request) && op.query != nil {
		more, err := accessLocks(ctx, op.query.invocation.Details().Actor, op.profile.Project, false)
		if err != nil {
			return mc.ConsumerDependencies{}, err
		}
		locks, err = ob.NormalizeLocks(append(locks, more...))
		if err != nil {
			return mc.ConsumerDependencies{}, err
		}
	}
	binding, err := mc.ConsumerBinding(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return mc.ConsumerDependencies{}, internal(err)
	}
	deadline, err := f.NewInstant(op.deadline)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	max := f.Sequence(1)
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: ob.DigestBytes(raw), Locks: locks, RetryPolicy: &mc.RetryPolicy{Class: mc.BoundedRetry, Deadline: &deadline, MaxAttempts: &max, Categories: []mc.ErrorCategory{}}})
}

func requiresCurrentConsumer(action mc.ConsumerAction) bool {
	return action == mc.ResolveConsumer || action == mc.InvokeConsumer || action == mc.ReadCredentialConsumer
}

func (a *IndexAuthority) ValidateInTx(ctx context.Context, tx f.Tx, request mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	op, err := a.operation(ctx, request)
	if err != nil {
		return err
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	current, err := a.discoverWith(ctx, x, request)
	if err != nil {
		return err
	}
	if plan.Validate() != nil || !plan.Matches(a.issuer, current.Details().Binding, current.Details().Mapping) {
		return fault(f.ResourceBusy)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, current.RequiredLocks()); err != nil {
		return portError(err)
	}
	if err = a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	facts, err := operationRecord(ctx, x, op)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(facts)
	if err != nil || ob.DigestBytes(raw) != plan.Details().Mapping {
		return fault(f.ResourceBusy)
	}
	if facts.Snapshot != "" && facts.Snapshot != request.SnapshotID.String() {
		return fault(f.Forbidden)
	}
	if request.Action != mc.ResolveConsumer && facts.Snapshot == "" {
		return fault(f.Forbidden)
	}
	if requiresCurrentConsumer(request.Action) && !indexResolutionLookup(ctx, op, request) {
		if facts.State != "active" {
			return fault(f.InvalidState)
		}
		if err = op.service.requireIndexProject(ctx, x, op.profile.Project); err != nil {
			return err
		}
		if op.query != nil {
			if err = authorizeInvocationInTx(ctx, tx, a.store, op.query.invocation.Details().Actor, op.profile.Project, "read"); err != nil {
				return err
			}
		}
		var eligible bool
		switch op.kind {
		case "profile":
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2 AND state='building' AND (snapshot_id IS NULL OR snapshot_id=$3))`, op.profile.Project.String(), op.profile.ID, request.SnapshotID.String()).Scan(&eligible)
		case "query":
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_state st JOIN agenteam_knowledge.index_profiles p ON p.project_id=st.project_id AND p.id=st.serving_profile_id WHERE st.project_id=$1 AND p.id=$2 AND p.snapshot_id=$3 AND p.state='active')`, op.profile.Project.String(), op.profile.ID, request.SnapshotID.String()).Scan(&eligible)
		case "index":
			err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.index_jobs j JOIN agenteam_knowledge.documents d ON d.project_id=j.project_id AND d.id=j.document_id JOIN agenteam_knowledge.index_profiles p ON p.project_id=j.project_id AND p.id=j.profile_id WHERE j.project_id=$1 AND j.id=$2 AND j.profile_id=$3 AND j.state='running' AND j.process_id=$4 AND d.status='active' AND d.content_version=j.document_version AND p.snapshot_id=$5 AND p.state IN ('building','active'))`, op.profile.Project.String(), op.job, op.profile.ID, a.process.String(), request.SnapshotID.String()).Scan(&eligible)
		default:
			return fault(f.Forbidden)
		}
		if err != nil {
			return unavailable(err)
		}
		if !eligible {
			return fault(f.VersionConflict)
		}
	} else if request.Action == mc.RetireConsumer && request.TerminalVersion != nil {
		if *request.TerminalVersion != 2 || (facts.Version != 2 && !(facts.State == "retired" && facts.Version == 3)) || facts.State != "completed" && facts.State != "failed" && facts.State != "retired" {
			return fault(f.Forbidden)
		}
	}
	return op.binding.authorize(op, request, true)
}

var _ mc.ConsumerAuthority = (*IndexAuthority)(nil)
