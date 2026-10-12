package scheduler

import (
	"bytes"
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

const projectCleanupBatchLimit = 100

type projectCleanupRow struct {
	Key    string          `json:"key"`
	Record json.RawMessage `json:"record"`
}
type projectCleanupBatch struct {
	stage string
	rows  []projectCleanupRow
}

func (b *projectCleanupBatch) contains(key string) bool {
	for _, row := range b.rows {
		if row.Key == key {
			return true
		}
	}
	return false
}

// The table and column names are closed implementation constants, never
// checkpoint SQL. Business safe_id is TEXT; ownership_id in 00081 is UUID.
func projectCleanupTable(stage string) (table, key, cast string, err error) {
	switch stage {
	case "runtimes":
		return "task_runtimes", "task_id", "text", nil
	case "relaunch_visits":
		return "relaunch_visits", "id", "text", nil
	case "dispatches":
		return "dispatches", "id", "text", nil
	case "calls":
		return "project_calls", "id", "uuid", nil
	}
	return "", "", "", invalid()
}

func readProjectCleanupRows(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, stage string, keys []string) ([]projectCleanupRow, error) {
	table, key, cast, err := projectCleanupTable(stage)
	if err != nil {
		return nil, err
	}
	args := []any{project.String()}
	where := "t.project_id=$1::" + cast
	if keys != nil {
		if len(keys) == 0 || len(keys) > projectCleanupBatchLimit {
			return nil, invalid()
		}
		where += " AND t." + key + "=ANY($2::" + cast + "[])"
		args = append(args, keys)
	}
	query := `SELECT COALESCE(jsonb_agg(jsonb_build_object('key',selected.key,'record',selected.record) ORDER BY selected.key),'[]'::jsonb) FROM (SELECT t.` + key + `::text AS key,to_jsonb(t) AS record FROM agenteam_scheduler.` + table + ` t WHERE ` + where + ` ORDER BY t.` + key + ` LIMIT 100) selected`
	var raw []byte
	if err = x.QueryRow(ctx, query, args...).Scan(&raw); err != nil {
		return nil, portError(err)
	}
	var rows []projectCleanupRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > projectCleanupBatchLimit {
		return nil, unavailable(nil)
	}
	for n, row := range rows {
		var record map[string]json.RawMessage
		var rowKey, rowProject string
		_, idErr := f.ParseID[struct{}](row.Key)
		if idErr != nil || n > 0 && rows[n-1].Key >= row.Key || json.Unmarshal(row.Record, &record) != nil || record == nil || json.Unmarshal(record[key], &rowKey) != nil || rowKey != row.Key || json.Unmarshal(record["project_id"], &rowProject) != nil || rowProject != project.String() {
			return nil, unavailable(nil)
		}
		if stage == "calls" {
			var phase string
			if json.Unmarshal(record["phase"], &phase) != nil || phase != "business_closed" {
				return nil, fault(f.ResourceBusy)
			}
		}
	}
	return rows, ctx.Err()
}

func readProjectCleanupBatch(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) (*projectCleanupBatch, error) {
	for _, stage := range []string{"runtimes", "relaunch_visits", "dispatches", "calls"} {
		rows, err := readProjectCleanupRows(ctx, x, project, stage, nil)
		if err != nil {
			return nil, err
		}
		if len(rows) != 0 {
			return &projectCleanupBatch{stage: stage, rows: rows}, nil
		}
	}
	return nil, nil
}

func deleteProjectCleanupBatch(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, batch *projectCleanupBatch) error {
	if batch == nil || len(batch.rows) == 0 || len(batch.rows) > projectCleanupBatchLimit {
		return invalid()
	}
	table, key, cast, err := projectCleanupTable(batch.stage)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(batch.rows)
	if err != nil {
		return unavailable(err)
	}
	query := `DELETE FROM agenteam_scheduler.` + table + ` t USING jsonb_to_recordset($2::jsonb) AS selected(key text,record jsonb) WHERE t.project_id=$1::` + cast + ` AND t.` + key + `=selected.key::` + cast + ` AND to_jsonb(t)=selected.record`
	if batch.stage == "calls" {
		query += ` AND t.phase='business_closed'`
	}
	tag, err := x.Exec(ctx, query, project.String(), string(raw))
	if err != nil {
		return portError(err)
	}
	if tag.RowsAffected() != int64(len(batch.rows)) {
		return fault(f.VersionConflict)
	}
	return ctx.Err()
}

func nextProjectCleanupStage(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) (string, error) {
	var remaining [4]bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_scheduler.task_runtimes WHERE project_id=$1::text),EXISTS(SELECT 1 FROM agenteam_scheduler.relaunch_visits WHERE project_id=$1::text),EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),EXISTS(SELECT 1 FROM agenteam_scheduler.project_calls WHERE project_id=$1::text::uuid)`, project.String()).Scan(&remaining[0], &remaining[1], &remaining[2], &remaining[3])
	if err != nil {
		return "", portError(err)
	}
	for n, stage := range []string{"runtimes", "relaunch_visits", "dispatches", "calls"} {
		if remaining[n] {
			return stage, ctx.Err()
		}
	}
	return "completed", ctx.Err()
}

// Only the saved keys from the original physical batch are examined. Empty
// arbitrary tables are never used to discard an unobserved caller's Unknown.
// PostgreSQL's transaction atomicity permits the exact original batch to be
// wholly present (rollback) or wholly absent (applied), never a partial set.
func confirmProjectCleanupBatch(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, batch *projectCleanupBatch) (bool, error) {
	keys := make([]string, len(batch.rows))
	for n, row := range batch.rows {
		keys[n] = row.Key
	}
	current, err := readProjectCleanupRows(ctx, x, project, batch.stage, keys)
	if err != nil {
		return false, err
	}
	if len(current) == 0 {
		return true, nil
	}
	if len(current) != len(batch.rows) {
		return false, fault(f.ConfirmationStale)
	}
	for n, row := range current {
		if row.Key != batch.rows[n].Key || !bytes.Equal(row.Record, batch.rows[n].Record) {
			return false, fault(f.ConfirmationStale)
		}
	}
	return false, nil
}
