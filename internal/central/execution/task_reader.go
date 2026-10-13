package execution

import (
	"context"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const taskExecutionOrder = "created_at:desc,id:desc"
const MaxTaskExecutionPage = 100

type TaskReadDependencies struct {
	Authority *Authority
	Tasks     ec.TaskReadScope
	Cursors   cursor.Keyring
}
type TaskExecutionReader struct {
	state *projectCallState
	deps  TaskReadDependencies
	calls *callSet
}
type taskExecutionRead struct {
	runtime   bool
	task      string
	execution i.ExecutionID
	limit     int
	cursor    string
}

// ProjectReadOwnership installs historical reads without constructing drivers,
// Model Runtime or Scheduler. It cannot supply Launch/retirement capabilities.
// The enabled runtime instead constructs its reader in its original four-owner
// pair; the App selects exactly one owner before serving any request.
type ProjectReadOwnership struct{ state *projectCallState }

func NewProjectReadOwnership(store Store, deps TaskReadDependencies, guard *object.ProcessGuard, expected oc.ProcessID, stop ec.ProjectStopAuthority) (*ProjectReadOwnership, error) {
	if guard == nil {
		return nil, fault(f.DependencyUnbound)
	}
	current, err := guard.CurrentProcess()
	if err != nil {
		return nil, portError(err)
	}
	if current != expected {
		return nil, fault(f.DependencyUnbound)
	}
	return newProjectReadOwnership(store, deps, guard, expected, stop)
}
func newProjectReadOwnership(store Store, deps TaskReadDependencies, proof projectCallProcessProof, expected oc.ProcessID, stop ec.ProjectStopAuthority) (*ProjectReadOwnership, error) {
	if nilPort(store) || nilPort(proof) || nilPort(stop) || expected.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	instance, err := f.NewID[projectCallInstance]()
	if err != nil {
		return nil, portError(err)
	}
	s := &projectCallState{store: store, proof: proof, process: expected, instance: instance, authority: stop, owners: map[f.ID[projectCallID]]*projectCallEntry{}, stops: map[i.ProjectID]*ownedProjectStop{}, changed: make(chan struct{})}
	s.reader, err = newTaskExecutionReader(s, deps)
	if err != nil {
		return nil, err
	}
	return &ProjectReadOwnership{s}, nil
}
func newTaskExecutionReader(s *projectCallState, deps TaskReadDependencies) (*TaskExecutionReader, error) {
	if deps.Authority == nil || deps.Authority.state == nil || !sameProjectCallPort(s.store, deps.Authority.state.store) || nilPort(deps.Tasks) || deps.Cursors.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	if err := deps.Tasks.RequireStore(s.store); err != nil {
		return nil, portError(err)
	}
	return &TaskExecutionReader{s, deps, newCalls()}, nil
}
func (p *ProjectCallOwnership) TaskReader() *TaskExecutionReader {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.reader
}
func (p *ProjectReadOwnership) TaskReader() *TaskExecutionReader {
	if p == nil || p.state == nil {
		return nil
	}
	return p.state.reader
}
func (p *ProjectReadOwnership) Stop() {
	if p == nil || p.state == nil {
		return
	}
	s := p.state
	s.mu.Lock()
	s.stopped = true
	cancels := make([]context.CancelFunc, 0, len(s.owners))
	for _, e := range s.owners {
		cancels = append(cancels, e.cancel)
	}
	s.signalLocked()
	s.mu.Unlock()
	s.reader.calls.stop()
	for _, cancel := range cancels {
		cancel()
	}
}
func (p *ProjectReadOwnership) Drain(ctx context.Context) error {
	if p == nil || p.state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return invalid()
	}
	p.Stop()
	s := p.state
	if err := s.reader.calls.drain(ctx); err != nil {
		return err
	}
	if err := s.drainReturned(ctx, ""); err != nil {
		return err
	}
	for {
		s.mu.Lock()
		done := len(s.stops) == 0
		changed := s.changed
		s.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (p *ProjectReadOwnership) Joined() bool {
	if p == nil || p.state == nil {
		return false
	}
	s := p.state
	s.mu.Lock()
	done := s.stopped && len(s.owners) == 0 && len(s.stops) == 0
	s.mu.Unlock()
	return done && s.reader.calls.joined()
}
func (p *ProjectReadOwnership) RequestProjectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	if p == nil {
		return pc.StopReport{}, fault(f.DependencyUnbound)
	}
	return (&ProjectCallOwnership{p.state}).projectStop(ctx, actor, cause, scope, true)
}
func (p *ProjectReadOwnership) InspectProjectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	if p == nil {
		return pc.StopReport{}, fault(f.DependencyUnbound)
	}
	return (&ProjectCallOwnership{p.state}).projectStop(ctx, actor, cause, scope, false)
}
func taskReadInput(ctx context.Context, actor i.Actor, project i.ProjectID, task string) error {
	if ctx == nil {
		return invalid()
	}
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	if actor.Details().Kind != i.Human {
		return fault(f.Forbidden)
	}
	if project.Validate() != nil {
		return invalid()
	}
	if _, err := f.ParseID[struct{}](task); err != nil {
		return invalid()
	}
	return ctx.Err()
}
func (r *TaskExecutionReader) requireTask(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, task string) error {
	if err := r.deps.Authority.requireScope(ctx, tx, actor, project, i.AgentID{}, i.Read); err != nil {
		return err
	}
	return portError(r.deps.Tasks.RequireTaskReadInTx(ctx, tx, actor, project, task))
}
func (r *TaskExecutionReader) begin(ctx context.Context, actor i.Actor, project i.ProjectID, read taskExecutionRead) (context.Context, *projectCallEntry, error) {
	if r == nil || r.state == nil {
		return ctx, nil, fault(f.DependencyUnbound)
	}
	if err := taskReadInput(ctx, actor, project, read.task); err != nil {
		return ctx, nil, err
	}
	method := "list_task_executions"
	if read.execution.Validate() == nil {
		method = "get_task_execution"
	}
	if read.runtime {
		method = "get_task_execution_runtime_view"
	}
	// Each GET is a new read, not replay of a business command. The original
	// uncertain read remains owned until its own explicit shutdown confirmation.
	key, err := f.NewID[projectCallID]()
	if err != nil {
		return ctx, nil, portError(err)
	}
	identity, err := f.NewCommandIdentity("execution", []string{project.String(), read.task}, method, f.IdempotencyKey(key.String()))
	if err != nil {
		return ctx, nil, err
	}
	raw, err := json.Marshal(struct {
		User, Task, Execution, Cursor string
		Limit                         int
	}{actor.Details().UserID, read.task, read.execution.String(), read.cursor, read.limit})
	if err != nil {
		return ctx, nil, invalid()
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return ctx, nil, err
	}
	run, done, err := r.calls.begin(ctx)
	if err != nil {
		return ctx, nil, err
	}
	run, e, err := r.state.admit(run, project, projectLaneService, identity, &projectCallEntry{actor: actor, read: &read, launchDigest: digest}, false)
	if e == nil {
		done()
		return run, nil, err
	}
	r.state.mu.Lock()
	e.serviceDone = done
	r.state.mu.Unlock()
	return run, e, err
}
func (r *TaskExecutionReader) read(ctx context.Context, actor i.Actor, project i.ProjectID, request taskExecutionRead, fn func(context.Context, postgres.SQLExecutor) error) error {
	locks, err := r.deps.Tasks.TaskReadLocks(actor, project, request.task)
	if err != nil {
		return portError(err)
	}
	if request.execution.Validate() == nil {
		locks = append(locks, executionLock(request.execution))
	}
	locks, err = normalizeProjectRegistrationLocks(locks)
	if err != nil {
		return err
	}
	witness := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	cause, err := f.NewRecoveryCause("execution.task-read", witness.entry.record.ID.String(), project.String())
	if err != nil {
		return err
	}
	result := r.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := requireProjectCallInTx(ctx, tx, r.state.store); err != nil {
			return err
		}
		if err := r.requireTask(ctx, tx, actor, project, request.task); err != nil {
			return err
		}
		x, err := r.state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = fn(ctx, x); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return err
	}
	return ctx.Err()
}
func (r *TaskExecutionReader) List(ctx context.Context, actor i.Actor, project i.ProjectID, task string, page f.PageRequest) (out ec.TaskExecutionPage, err error) {
	if err = taskReadInput(ctx, actor, project, task); err != nil {
		return out, err
	}
	if page.Limit == 0 {
		page.Limit = 50
	}
	if page.Limit < 1 || page.Limit > MaxTaskExecutionPage || len(page.Cursor) > cursor.MaxTokenBytes {
		return out, invalid()
	}
	if r == nil || r.state == nil {
		return out, fault(f.DependencyUnbound)
	}
	binding, err := taskExecutionBinding(project, task, actor.Details().UserID)
	if err != nil {
		return out, err
	}
	position, err := r.taskPosition(binding, page.Cursor)
	if err != nil {
		return out, err
	}
	request := taskExecutionRead{task: task, limit: page.Limit, cursor: page.Cursor}
	ctx, entry, err := r.begin(ctx, actor, project, request)
	if entry != nil {
		defer func() {
			if err != nil {
				out = ec.TaskExecutionPage{}
			}
		}()
		defer finishProjectCall(r.state, entry, &err)
	}
	if err != nil {
		return out, err
	}
	err = r.read(ctx, actor, project, request, func(ctx context.Context, x postgres.SQLExecutor) error {
		var err error
		out.Items, err = listTaskExecutions(ctx, x, project, task, page.Limit, position)
		return err
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > page.Limit {
		out.Items = out.Items[:page.Limit]
		last := out.Items[len(out.Items)-1]
		at, _ := cursor.Instant(last.CreatedAt)
		id, _ := cursor.UUID(last.ID.String())
		next, e := r.deps.Cursors.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{at, id}})
		if e != nil {
			return out, portError(e)
		}
		out.NextCursor = &next
	}
	return out, nil
}
func (r *TaskExecutionReader) Get(ctx context.Context, actor i.Actor, project i.ProjectID, task string, id i.ExecutionID) (out ec.TaskExecution, err error) {
	if err = taskReadInput(ctx, actor, project, task); err != nil {
		return out, err
	}
	if id.Validate() != nil {
		return out, invalid()
	}
	request := taskExecutionRead{task: task, execution: id}
	ctx, entry, err := r.begin(ctx, actor, project, request)
	if entry != nil {
		defer func() {
			if err != nil {
				out = ec.TaskExecution{}
			}
		}()
		defer finishProjectCall(r.state, entry, &err)
	}
	if err != nil {
		return out, err
	}
	err = r.read(ctx, actor, project, request, func(ctx context.Context, x postgres.SQLExecutor) error {
		row, err := scanTaskExecution(x.QueryRow(ctx, taskExecutionSelect+` WHERE e.id=$1 AND e.project_id=$2 AND e.launch_request->'trigger'->>'kind'='task' AND e.launch_request->'trigger'->>'task_id'=$3`, id.String(), project.String(), task), project, task)
		if err != nil {
			return err
		}
		if row == nil {
			return fault(f.NotFound)
		}
		out = *row
		return nil
	})
	return out, err
}
