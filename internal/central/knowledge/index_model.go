package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type indexOperationKey struct{}
type indexOperation struct {
	service          *IndexService
	id               string
	profile          indexProfile
	kind, job        string
	call             mc.CallID
	actor            id.Actor
	input            f.Digest
	deadline         time.Time
	query            *indexQuery
	projectCall      *projectCallEntry
	admissionUnknown bool
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	binding          indexModelBinding
	resolved         mc.ResolvedModel
	resolve          mc.ResolveRequest
	handle           mc.EmbeddingCall
	resolveUnknown   bool
	terminal         string
	done             bool
}

func indexLocks(project id.ProjectID, exclusive bool) ([]f.LockRequest, error) {
	p, err := f.ProjectLock(project.String())
	if err != nil {
		return nil, err
	}
	t, err := f.KnowledgeTreeLock(project.String())
	if err != nil {
		return nil, err
	}
	mode := f.Shared
	if exclusive {
		mode = f.Exclusive
	}
	return ob.NormalizeLocks([]f.LockRequest{{Key: p, Mode: f.Shared}, {Key: t, Mode: mode}})
}

func (s *IndexService) indexTx(ctx context.Context, project id.ProjectID, exclusive bool, query *indexQuery, allowStopping bool, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	locks, err := indexLocks(project, exclusive)
	if err != nil {
		return err
	}
	if query != nil {
		v, err := currentInvocation(ctx, query.invocation.Details().Actor, project)
		if err != nil {
			return err
		}
		locks = append(locks, v.dependencies.Locks()...)
		locks, err = ob.NormalizeLocks(locks)
		if err != nil {
			return err
		}
	}
	cause, err := readCause()
	if err != nil {
		return err
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.authority.guard.RequireCurrentProcessInTx(ctx, tx, s.authority.process); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if !allowStopping {
			if err = s.requireIndexProject(ctx, x, project); err != nil {
				return err
			}
		}
		if query != nil {
			if err = authorizeInvocationInTx(ctx, tx, s.store, query.invocation.Details().Actor, project, id.Read); err != nil {
				return err
			}
		}
		return fn(ctx, tx, x)
	})
	return txError(result)
}

func (s *IndexService) requireIndexProject(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID) error {
	var eligible bool
	if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.projects WHERE id=$1 AND initialized_at IS NOT NULL AND lifecycle IN ('active','archived'))`, project.String()).Scan(&eligible); err != nil {
		return unavailable(err)
	}
	if !eligible {
		return fault(f.InvalidState)
	}
	return nil
}

func (s *IndexService) beginModelOperation(ctx context.Context, profile indexProfile, kind, job string, texts []string, query *indexQuery) (context.Context, *indexOperation, error) {
	key, err := f.NewID[struct{}]()
	if err != nil {
		return nil, nil, unavailable(err)
	}
	call, err := f.NewID[mc.Call]()
	if err != nil {
		return nil, nil, unavailable(err)
	}
	digest, err := model.EmbeddingInputDigest(texts, profile.Dimension)
	if err != nil {
		return nil, nil, err
	}
	scope, _ := id.InProject(profile.Project)
	actor, err := s.authority.registration.Actor(key.String(), scope)
	if err != nil {
		return nil, nil, err
	}
	deadline := time.Now().Add(s.deps.Settings.ModelTimeout)
	if parent, ok := ctx.Deadline(); ok && parent.Before(deadline) {
		deadline = parent
	}
	deadline = deadline.UTC().Truncate(time.Microsecond)
	var projectCall *projectCallEntry
	if _, owned := ctx.Value(projectCallContextKey{}).(projectCallWitness); query != nil || owned {
		ctx, projectCall, err = s.documents.beginProjectCall(ctx, actor, profile.Project, nil, projectCallRead)
		if err != nil {
			return nil, nil, err
		}
	}
	run, cancel := context.WithCancel(ctx)
	op := &indexOperation{service: s, id: key.String(), profile: profile, kind: kind, job: job, call: call, actor: actor, input: digest, deadline: deadline, query: query, cancel: cancel, projectCall: projectCall}
	run = context.WithValue(run, indexOperationKey{}, op)
	op.ctx = run
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		cancel()
		s.documents.projectCallDone(projectCall, nil)
		return nil, nil, fault(f.ShuttingDown)
	}
	s.owners[op] = struct{}{}
	s.mu.Unlock()
	var invocation any
	if query != nil {
		raw, marshalErr := json.Marshal(query.invocation.Details())
		if marshalErr != nil {
			s.finishOperation(op)
			return nil, nil, internal(marshalErr)
		}
		invocation = string(raw)
	}
	var snapshot any
	if profile.Snapshot.Validate() == nil {
		snapshot = profile.Snapshot.String()
	}
	var jobValue any
	if job != "" {
		jobValue = job
	}
	err = s.indexTx(run, profile.Project, true, query, false, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		_, err := x.Exec(ctx, `INSERT INTO agenteam_knowledge.model_operations(id,project_id,kind,job_id,profile_id,generation_id,snapshot_id,call_id,process_id,invocation,input_digest,state,deadline_at) VALUES($1,$2,$3,$4,$5,$5,$6,$7,$8,$9::jsonb,$10,'active',$11)`, op.id, profile.Project.String(), kind, jobValue, profile.ID, snapshot, call.String(), s.authority.process.String(), invocation, string(digest), deadline)
		return portError(err)
	})
	if err != nil {
		var known *f.Fault
		if !errors.As(err, &known) || known.Code != f.CommitUnknown {
			s.finishOperation(op)
		} else {
			op.mu.Lock()
			op.admissionUnknown = true
			op.mu.Unlock()
			s.markOperationTerminal(op, "failed")
		}
		return nil, nil, err
	}
	return run, op, nil
}

func (s *IndexService) markOperationTerminal(op *indexOperation, state string) {
	op.mu.Lock()
	op.terminal = state
	op.mu.Unlock()
	s.mu.Lock()
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

func (s *IndexService) finishOperation(op *indexOperation) {
	op.mu.Lock()
	if op.done {
		op.mu.Unlock()
		return
	}
	op.done = true
	op.mu.Unlock()
	op.cancel()
	s.documents.projectCallDone(op.projectCall, nil)
	s.mu.Lock()
	delete(s.owners, op)
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

func (s *IndexService) resolveOperation(ctx context.Context, op *indexOperation) (mc.ResolvedModel, error) {
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, op.call.String())
	if err != nil {
		return mc.ResolvedModel{}, err
	}
	consumer := mc.Consumer{Kind: mc.KnowledgeConsumer, ProjectID: op.profile.Project, Purpose: mc.KnowledgeEmbedding, OperationID: op.id}
	request := mc.ResolveRequest{Actor: op.actor, Consumer: consumer, Purpose: mc.KnowledgeEmbedding, LeaseOwner: owner}
	if op.profile.Snapshot.Validate() == nil {
		snapshot := op.profile.Snapshot
		request.Source = mc.ServingSnapshotSource
		request.ServingSnapshotID = &snapshot
		request.ServingGenerationID = op.profile.ID
	} else {
		request.Source = mc.CurrentSelectionSource
		request.Selection = &mc.SelectionRef{Kind: "platform", Selector: mc.EmbeddingSelector}
	}
	op.mu.Lock()
	op.resolve = request
	op.mu.Unlock()
	resolved, err := s.deps.Models.ResolveModel(ctx, request)
	if err != nil {
		var known *f.Fault
		if errors.As(err, &known) && known.Code == f.CommitUnknown {
			op.mu.Lock()
			op.resolveUnknown = true
			op.mu.Unlock()
			return mc.ResolvedModel{}, err
		}
		return mc.ResolvedModel{}, err
	}
	if resolved.Validate() != nil {
		return mc.ResolvedModel{}, fault(f.InvalidState)
	}
	op.mu.Lock()
	op.resolved = resolved
	op.mu.Unlock()
	op.binding.remember(resolved)
	err = s.persistResolved(ctx, op, resolved)
	return resolved, err
}

func (s *IndexService) persistResolved(ctx context.Context, op *indexOperation, resolved mc.ResolvedModel) error {
	return s.indexTx(ctx, op.profile.Project, true, op.query, false, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.model_operations SET snapshot_id=$3 WHERE project_id=$1 AND id=$2 AND state='active' AND (snapshot_id IS NULL OR snapshot_id=$3)`, op.profile.Project.String(), op.id, resolved.Snapshot.ID.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		return nil
	})
}

func (s *IndexService) embedTexts(ctx context.Context, profile indexProfile, kind, job string, texts []string, query *indexQuery) ([][]float64, error) {
	run, op, err := s.beginModelOperation(ctx, profile, kind, job, texts, query)
	if err != nil {
		return nil, err
	}
	resolved, err := s.resolveOperation(run, op)
	if err == nil {
		request := mc.EmbeddingRequest{NonchatCall: mc.NonchatCall{Actor: op.actor, CallID: op.call, Consumer: resolved.Consumer, Model: resolved, Input: mc.InputIdentity{OperationID: op.id, Digest: op.input, SchemaVersion: 1}}, Texts: append([]string(nil), texts...), ExpectedDimensions: profile.Dimension}
		handle, startErr := s.deps.Calls.BeginEmbed(run, request)
		op.mu.Lock()
		op.handle = handle
		op.mu.Unlock()
		err = startErr
		if err == nil && handle == nil {
			err = fault(f.DependencyUnavailable)
		}
		if err == nil {
			var result mc.Embeddings
			result, err = handle.Result(run)
			if err == nil {
				if result.ValidateFor(len(texts), profile.Dimension) != nil {
					err = fault(f.InvalidState)
				} else {
					vectors := make([][]float64, len(texts))
					for _, item := range result.Items {
						vectors[item.Index] = append([]float64(nil), item.Values...)
					}
					s.markOperationTerminal(op, "completed")
					cleanup, cancel := context.WithTimeout(context.WithoutCancel(run), s.deps.Settings.ModelTimeout)
					defer cancel()
					if closeErr := s.retireOperation(cleanup, op); closeErr != nil {
						return nil, closeErr
					}
					return vectors, nil
				}
			}
		}
	}
	s.markOperationTerminal(op, "failed")
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(run), s.deps.Settings.ModelTimeout)
	defer cancel()
	return nil, errors.Join(err, s.retireOperation(cleanup, op))
}

func (s *IndexService) retireOperation(ctx context.Context, op *indexOperation) error {
	op.mu.Lock()
	defer op.mu.Unlock()
	if op.done {
		return nil
	}
	ctx = context.WithValue(ctx, indexOperationKey{}, op)
	if op.admissionUnknown {
		var present bool
		if err := s.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.model_operations WHERE id=$1 AND project_id=$2 AND process_id=$3)`, op.id, op.profile.Project.String(), s.authority.process.String()).Scan(&present); err != nil {
			return unavailable(err)
		}
		op.admissionUnknown = false
		if !present {
			op.mu.Unlock()
			s.finishOperation(op)
			op.mu.Lock()
			return nil
		}
	}
	if op.resolveUnknown {
		resolved, found, err := s.deps.Models.LookupResolution(context.WithValue(ctx, indexResolutionLookupKey{}, op), op.resolve)
		if err != nil {
			return err
		}
		if found {
			op.resolved = resolved
			op.binding.remember(resolved)
		}
		op.resolveUnknown = false
	}
	if op.handle != nil {
		if err := op.handle.Close(ctx); err != nil {
			return err
		}
		if !op.handle.Joined() {
			return fault(f.DependencyUnavailable)
		}
	}
	terminal := op.terminal
	if terminal == "" {
		terminal = "failed"
	}
	err := s.indexTx(ctx, op.profile.Project, true, nil, true, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var snapshot any
		if op.resolved.Validate() == nil {
			snapshot = op.resolved.Snapshot.ID.String()
		}
		_, err := x.Exec(ctx, `UPDATE agenteam_knowledge.model_operations SET state=$3,version=2,snapshot_id=COALESCE(snapshot_id,$4::uuid) WHERE project_id=$1 AND id=$2 AND state='active' AND (snapshot_id IS NULL OR $4::uuid IS NULL OR snapshot_id=$4::uuid)`, op.profile.Project.String(), op.id, terminal, snapshot)
		return portError(err)
	})
	if err != nil {
		return err
	}
	if op.resolved.Validate() == nil {
		request := mc.ModelCallRetirementRequest{Actor: op.actor, Model: op.resolved, TerminalVersion: 2}
		plan, err := s.deps.Models.DiscoverModelCallRetirement(ctx, request)
		if err != nil {
			return err
		}
		locks, err := indexLocks(op.profile.Project, true)
		if err != nil {
			return err
		}
		locks, err = ob.NormalizeLocks(append(locks, plan.RequiredLocks()...))
		if err != nil {
			return err
		}
		cause, err := readCause()
		if err != nil {
			return err
		}
		out := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
				return portError(err)
			}
			if err := s.deps.Models.ReleaseModelCallInTx(ctx, tx, request, plan); err != nil {
				return err
			}
			retired, err := s.deps.Models.ModelCallRetiredInTx(ctx, tx, request, plan)
			if err != nil {
				return err
			}
			if !retired {
				return fault(f.DependencyUnavailable)
			}
			x, err := s.store.InTx(tx)
			if err != nil {
				return err
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_knowledge.model_operations SET state='retired',version=3 WHERE project_id=$1 AND id=$2 AND state IN ('completed','failed')`, op.profile.Project.String(), op.id)
			return portError(err)
		})
		if err = txError(out); err != nil {
			return err
		}
	}
	// Release the per-operation mutex before removing its actual owner.
	op.mu.Unlock()
	s.finishOperation(op)
	op.mu.Lock()
	return nil
}
