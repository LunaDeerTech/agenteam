package projectvariable

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

const projectCleanupBatch = 100

// Recovery is read-only. Project EX and the private current claim are already
// held by the caller. A known return observes only whether work remains.
func projectCleanupEmpty(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) (bool, error) {
	var remains bool
	err := x.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM agenteam_projectvariable.history WHERE project_id=$1)
 OR EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_history WHERE project_id=$1)
 OR EXISTS(SELECT 1 FROM agenteam_projectvariable.commands WHERE project_id=$1)
 OR EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_commands WHERE project_id=$1)
 OR EXISTS(SELECT 1 FROM agenteam_projectvariable.variables WHERE project_id=$1)
 OR EXISTS(SELECT 1 FROM agenteam_projectvariable.project_generations WHERE project_id=$1)
 OR EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_project_generations WHERE project_id=$1)`, project.String()).Scan(&remains)
	if err != nil {
		return false, unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		return false, canceled(err)
	}
	return !remains, nil
}

// Immutable environments and their binding rows are a separate retirement
// protocol. Even an empty captured environment blocks this metadata cleanup.
// A foreign environment referencing one of these Variables also blocks it.
func requireProjectCleanupUnreferenced(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) error {
	var referenced bool
	err := x.QueryRow(ctx, `SELECT
 EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_references WHERE project_id=$1::text::uuid)
 OR EXISTS(SELECT 1 FROM agenteam_projectvariable.execution_environments WHERE project_id=$1::text)
 OR EXISTS(SELECT 1 FROM agenteam_projectvariable.execution_secret_references r JOIN agenteam_projectvariable.variables v ON v.id=r.variable_id WHERE v.project_id=$1::text::uuid)`, project.String()).Scan(&referenced)
	if err != nil {
		return unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		return canceled(err)
	}
	if referenced {
		return fault(f.ResourceBusy)
	}
	return nil
}

func cleanupProjectMetadata(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) (bool, error) {
	// A nonempty batch returns immediately, so a later table is never deleted
	// while earlier FK children remain. These are fixed private identifiers;
	// neither the caller nor an opaque checkpoint can supply a SQL name.
	for _, table := range []struct{ name, key string }{
		{"history", "id"},
		{"secret_history", "id"},
		{"commands", "id"},
		{"secret_commands", "id"},
		{"variables", "id"},
		{"project_generations", "project_id"},
		{"secret_project_generations", "project_id"},
	} {
		query := `WITH batch AS (SELECT ` + table.key + ` FROM agenteam_projectvariable.` + table.name + ` WHERE project_id=$1 ORDER BY ` + table.key + ` LIMIT 100)
 DELETE FROM agenteam_projectvariable.` + table.name + ` WHERE project_id=$1 AND ` + table.key + ` IN (SELECT ` + table.key + ` FROM batch)`
		tag, err := x.Exec(ctx, query, project.String())
		if err != nil {
			return false, unavailable(err)
		}
		if err = ctx.Err(); err != nil {
			return false, canceled(err)
		}
		if count := tag.RowsAffected(); count < 0 || count > projectCleanupBatch {
			return false, unavailable(nil)
		} else if count != 0 {
			return false, nil
		}
	}
	// No D04 table is read or changed. Closed project_calls remain protected by
	// their original immutable trigger, for the later terminal owner step.
	return true, nil
}
