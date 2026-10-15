package memory

import (
	"context"
	"errors"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type stoppedMemoryTools interface {
	RequireStoppedMemoryToolInTx(context.Context, f.Tx, i.Actor, i.ProjectID, i.AgentID, tc.OperationID, oc.ProcessID) error
}

// LookupRetainFailure is available only to the original cold Tool Lookup. It
// records the failure of an original prepared operation after its process and
// every admitted Model owner have actually retired. It never reruns retention.
func (r *Runtime) LookupRetainFailure(ctx context.Context, actor i.Actor, namespace c.Namespace, operation tc.OperationID, input c.RetainInput) (f.Code, bool, error) {
	if r == nil || r.models == nil || input.Validate() != nil {
		return "", false, fault(f.InvalidArgument)
	}
	tools, ok := r.domain.deps.Tools.(stoppedMemoryTools)
	if !ok || nilPort(tools) {
		return "", false, fault(f.DependencyUnbound)
	}
	run, entry, err := r.domain.begin(ctx, actor, namespace, operation, "lookup-retain")
	if err != nil {
		return "", false, err
	}
	defer r.domain.end(entry)
	raw, err := encode(input)
	if err != nil {
		return "", false, err
	}
	inputDigest := digest(raw)
	locks, err := normalizeLocks(append(slices.Clone(entry.locks), namespaceLock(namespace, f.Exclusive)))
	if err != nil {
		return "", false, err
	}
	cause, err := readCause("stopped-retain-failure")
	if err != nil {
		return "", false, err
	}
	var found bool
	transact := func(write bool) f.CommitResult {
		found = false
		return r.domain.store.WithinTx(run, cause, func(ctx context.Context, tx f.Tx) error {
			if ctx.Value(callKey{}) != entry {
				return fault(f.Forbidden)
			}
			if err := r.domain.store.AcquireAll(ctx, tx, locks); err != nil {
				return portError(err)
			}
			if err := r.domain.requireAccess(ctx, tx, entry, false); err != nil {
				return err
			}
			if err := r.models.authority.guard.RequireCurrentProcessInTx(ctx, tx, r.models.authority.process); err != nil {
				return portError(err)
			}
			x, err := r.domain.store.InTx(tx)
			if err != nil {
				return portError(err)
			}
			return r.stoppedRetainInTx(ctx, tx, x, tools, entry, inputDigest, write, &found)
		})
	}
	err = transactionError(transact(true))
	if isUnknown(err) {
		// Observe only the same original canonical target; missing or still
		// prepared is not a negative receipt and must remain Unknown.
		if observeErr := transactionError(transact(false)); observeErr != nil {
			// A failed lookup cannot turn the original mutation into a known failure.
			return "", false, err
		}
		if !found {
			return "", false, err
		}
		err = nil
	}
	if err != nil || !found {
		return "", false, err
	}
	return f.DependencyUnavailable, true, nil
}

func (r *Runtime) stoppedRetainInTx(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, tools stoppedMemoryTools, entry *call, input f.Digest, write bool, found *bool) error {
	var project, agent, execution, stored, phase, process string
	var failure *string
	err := x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,execution_id::text,input_digest,phase,coalesce(prepared_process_id::text,''),failure_code FROM agenteam_memory.operations WHERE operation_id=$1`, entry.operation.String()).Scan(&project, &agent, &execution, &stored, &phase, &process, &failure)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return portError(err)
	}
	if project != entry.namespace.ProjectID.String() || agent != entry.namespace.AgentID.String() || execution != entry.actor.Details().ExecutionID || stored != string(input) {
		return fault(f.IdempotencyKeyReused)
	}
	if phase == "committed" {
		return nil
	}
	original, err := f.ParseID[oc.Process](process)
	if err != nil {
		return fault(f.ResourceBusy)
	}
	if err = tools.RequireStoppedMemoryToolInTx(ctx, tx, entry.actor, entry.namespace.ProjectID, entry.namespace.AgentID, entry.operation, original); err != nil {
		return portError(err)
	}
	if phase == "failed" {
		if failure == nil || *failure != string(f.DependencyUnavailable) {
			return fault(f.DependencyUnavailable)
		}
		*found = true
		return nil
	}
	if phase != "prepared" {
		return fault(f.InvalidState)
	}
	if !write {
		return nil
	}
	// Model retirement was established by its own formal observer during
	// initialization. An unresolved pre-admission owner cannot be inferred
	// absent here; it stays unretired and blocks this canonical transition.
	var unsettled bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_memory.model_calls WHERE operation_id=$1 AND (NOT retired OR process_id IS NULL OR process_id<>$2 OR project_id<>$3 OR agent_id<>$4 OR execution_id<>$5))`, entry.operation.String(), original.String(), project, agent, execution).Scan(&unsettled)
	if err != nil {
		return portError(err)
	}
	if unsettled {
		return fault(f.ResourceBusy)
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_memory.operations SET phase='failed',failure_code=$2,failed_at=$3 WHERE operation_id=$1 AND phase='prepared'`, entry.operation.String(), string(f.DependencyUnavailable), now().Time())
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.VersionConflict)
	}
	*found = true
	return nil
}
