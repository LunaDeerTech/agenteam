package execution

import (
	"context"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func requireRetirementProjectEmpty(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) error {
	var remains bool
	if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1::text)`, project.String()).Scan(&remains); err != nil {
		return portError(err)
	}
	if remains {
		return fault(f.ResourceBusy)
	}
	// Referential constraints ensure the child sets have no orphan Execution.
	// The no-active Skill ingress belongs to the existing Agent/Skill retirement
	// path and intentionally survives this phase.
	if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_ingress WHERE project_id=$1::text AND execution_id IS NOT NULL)`, project.String()).Scan(&remains); err != nil {
		return portError(err)
	}
	if remains {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}

func (r *ProjectExecutionRetirement) RetireProjectExecutionCallsInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	c, err := r.control(ctx)
	if err != nil {
		return err
	}
	if !c.actor.Equal(actor) || c.cause != cause || !c.scope.Equal(scope) || c.confirming || c.unknown != nil {
		return fault(f.Forbidden)
	}
	x, err := r.requireTx(ctx, tx, c, projectCallLocks(scope.ProjectID))
	if err != nil {
		return err
	}
	if err = requireRetirementProjectEmpty(ctx, x, scope.ProjectID); err != nil {
		return err
	}
	if err = r.requireRetirementRows(ctx, x, c); err != nil {
		return err
	}
	for _, row := range c.ledger {
		if row.Phase != "business_closed" || row.Process == r.pair.process && row.Instance != r.pair.instance || row.Process != r.pair.process && !c.proven[row.Process] {
			return fault(f.ResourceBusy)
		}
		tag, err := x.Exec(ctx, `DELETE FROM agenteam_execution.project_calls WHERE id=$1::uuid AND project_id=$2::uuid AND process_id=$3::uuid AND instance_id=$4::uuid AND lane=$5 AND invocation_identity=$6 AND invocation_digest=$7 AND phase='business_closed'`, row.ID.String(), row.Project.String(), row.Process.String(), row.Instance.String(), string(row.Lane), row.Identity, row.Digest.String())
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
	}
	c.empty = true
	c.touched = true
	return nil
}

func (r *ProjectExecutionRetirement) RequireProjectExecutionsEmptyInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	c, err := r.control(ctx)
	if err != nil {
		return err
	}
	if !c.actor.Equal(actor) || c.cause != cause || !c.scope.Equal(scope) {
		return fault(f.Forbidden)
	}
	x, err := r.requireTx(ctx, tx, c, projectCallLocks(scope.ProjectID))
	if err != nil {
		return err
	}
	if err = requireRetirementProjectEmpty(ctx, x, scope.ProjectID); err != nil {
		return err
	}
	rows, err := listProjectCalls(ctx, x, scope.ProjectID)
	if err != nil {
		return err
	}
	if len(rows) != 0 {
		return fault(f.ResourceBusy)
	}
	c.touched = true
	return ctx.Err()
}

func (r *ProjectExecutionRetirement) confirmRetirementEmpty(ctx context.Context, tx f.Tx, c *executionRetirementControl) (bool, error) {
	if !c.empty {
		return false, fault(f.Forbidden)
	}
	x, err := r.requireTx(ctx, tx, c, projectCallLocks(c.scope.ProjectID))
	if err != nil {
		return false, err
	}
	if err = requireRetirementProjectEmpty(ctx, x, c.scope.ProjectID); err != nil {
		return false, err
	}
	rows, err := listProjectCalls(ctx, x, c.scope.ProjectID)
	if err != nil {
		return false, err
	}
	applied := len(rows) == 0
	if !applied && !slices.Equal(rows, c.ledger) {
		return false, fault(f.VersionConflict)
	}
	origins, err := readRetirementOrigins(ctx, x, c.scope.ProjectID, rows)
	if err != nil {
		return false, err
	}
	if !applied && !slices.Equal(origins, c.origins) {
		return false, fault(f.VersionConflict)
	}
	c.checked = true
	c.touched = true
	return applied, ctx.Err()
}
