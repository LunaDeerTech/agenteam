package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

const (
	projectCleanupBatchLimit = 100
	projectCleanupBytes      = 10 << 20
)

// FK children are removed before commands. Document batches contain only leaves.
var projectCleanupStages = [...]string{"documents", "command_events", "object_cleanup", "publications", "work_claims", "commands", "objects", "calls"}

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

type projectCleanupTableSpec struct {
	table  string
	key    string
	fields []string
	limit  int
}

// Fixed identifiers and a LIMIT before aggregation. A command may retain both
// a 4 MiB plan and a 4 MiB receipt, so commands use one row and a 10 MiB cap.
func projectCleanupTable(stage string) (projectCleanupTableSpec, error) {
	spec := projectCleanupTableSpec{key: "t.id::text", fields: []string{"id"}, limit: projectCleanupBatchLimit}
	switch stage {
	case "documents", "command_events", "object_cleanup":
		spec.table = stage
	case "publications", "work_claims":
		spec.table, spec.key, spec.fields = stage, "t.command_id::text", []string{"command_id"}
		spec.limit = 100
	case "commands":
		spec.table, spec.limit = stage, 1
	case "objects":
		spec.table = "project_object_retirements"
	case "calls":
		spec.table = "project_calls"
	default:
		return projectCleanupTableSpec{}, fault(f.InvalidArgument)
	}
	return spec, nil
}

func projectCleanupRowKey(spec projectCleanupTableSpec, record map[string]json.RawMessage) (string, error) {
	parts := make([]string, len(spec.fields))
	for n, field := range spec.fields {

		if json.Unmarshal(record[field], &parts[n]) != nil {
			return "", unavailable(nil)
		}
		id, err := f.ParseID[struct{}](parts[n])
		if err != nil || id.String() != parts[n] {
			return "", unavailable(err)
		}
	}
	return strings.Join(parts, ":"), nil
}

func readProjectCleanupRows(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, stage string, keys []string) ([]projectCleanupRow, error) {
	spec, err := projectCleanupTable(stage)
	if err != nil {
		return nil, err
	}
	args := []any{project.String()}
	where := "t.project_id=$1::uuid"
	if stage == "calls" {
		where = "(t.project_id=$1::uuid OR t.source_project_id=$1::uuid)"
	}
	if stage == "documents" && keys == nil {
		where += ` AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.documents child WHERE child.project_id=t.project_id AND child.parent_document_id=t.id)`
	}
	if keys != nil {
		if len(keys) == 0 || len(keys) > spec.limit {
			return nil, fault(f.InvalidArgument)
		}
		where += " AND (" + spec.key + ")=ANY($2::text[])"
		args = append(args, keys)
	}
	query := `SELECT COALESCE(jsonb_agg(jsonb_build_object('key',selected.key,'record',selected.record) ORDER BY selected.key COLLATE "C"),'[]'::jsonb) FROM (SELECT (` + spec.key + `) AS key,to_jsonb(t) AS record FROM agenteam_knowledge.` + spec.table + ` t WHERE ` + where + ` ORDER BY (` + spec.key + `) COLLATE "C" LIMIT ` + fmt.Sprint(spec.limit) + `) selected`
	var raw []byte
	if err = x.QueryRow(ctx, query, args...).Scan(&raw); err != nil {
		return nil, unavailable(err)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) > projectCleanupBytes {
		return nil, fault(f.ResourceBusy)
	}
	var rows []projectCleanupRow
	if json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > spec.limit {
		return nil, unavailable(nil)
	}
	for n, row := range rows {
		var record map[string]json.RawMessage
		var rowProject string
		if n > 0 && rows[n-1].Key >= row.Key || json.Unmarshal(row.Record, &record) != nil || record == nil || json.Unmarshal(record["project_id"], &rowProject) != nil {
			return nil, unavailable(nil)
		}
		if rowProject != project.String() {
			var source string
			if stage != "calls" || json.Unmarshal(record["source_project_id"], &source) != nil || source != project.String() {
				return nil, unavailable(nil)
			}
		}
		key, err := projectCleanupRowKey(spec, record)
		if err != nil || key != row.Key {
			return nil, unavailable(err)
		}
		if stage == "calls" || stage == "objects" {
			expectedPhase := "business_closed"
			if stage == "objects" {
				expectedPhase = "completed"
			}
			var phase string
			if json.Unmarshal(record["phase"], &phase) != nil || phase != expectedPhase {
				return nil, fault(f.ResourceBusy)
			}
		}
	}
	return rows, ctx.Err()
}

func readProjectCleanupBatch(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) (*projectCleanupBatch, error) {
	for _, stage := range projectCleanupStages {
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
	if batch == nil || len(batch.rows) == 0 {
		return fault(f.InvalidArgument)
	}
	spec, err := projectCleanupTable(batch.stage)
	if err != nil {
		return err
	}
	if len(batch.rows) > spec.limit {
		return fault(f.InvalidArgument)
	}
	// Preserve the bounded PostgreSQL JSON representation when copying its
	// complete preimage into the DELETE parameter; do not expand HTML bytes.
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(batch.rows); err != nil {
		return unavailable(err)
	}
	raw := encoded.Bytes()
	if len(raw) > projectCleanupBytes {
		return fault(f.ResourceBusy)
	}
	query := `DELETE FROM agenteam_knowledge.` + spec.table + ` t USING jsonb_to_recordset($2::jsonb) AS selected(key text,record jsonb) WHERE ` + projectCleanupProjectPredicate(batch.stage) + ` AND (` + spec.key + `)=selected.key AND to_jsonb(t)=selected.record`
	if batch.stage == "calls" {
		query += ` AND t.phase='business_closed'`
	}
	if batch.stage == "objects" {
		query += ` AND t.phase='completed'`
	}
	tag, err := x.Exec(ctx, query, project.String(), string(raw))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != int64(len(batch.rows)) {
		return fault(f.VersionConflict)
	}
	return ctx.Err()
}
func nextProjectCleanupStage(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID) (string, error) {
	clauses := make([]string, len(projectCleanupStages))
	remaining := make([]bool, len(projectCleanupStages))
	dest := make([]any, len(projectCleanupStages))
	for n, stage := range projectCleanupStages {
		spec, err := projectCleanupTable(stage)
		if err != nil {
			return "", err
		}
		clauses[n] = `EXISTS(SELECT 1 FROM agenteam_knowledge.` + spec.table + ` t WHERE ` + projectCleanupProjectPredicate(stage) + `)`
		dest[n] = &remaining[n]
	}
	if err := x.QueryRow(ctx, "SELECT "+strings.Join(clauses, ","), project.String()).Scan(dest...); err != nil {
		return "", unavailable(err)
	}
	for n, exists := range remaining {
		if exists {
			return projectCleanupStages[n], ctx.Err()
		}
	}
	return "completed", ctx.Err()
}

// Compare only the original exact keys and complete immutable preimage. An
// Unknown confirmation is read-only: wholly absent means applied, wholly
// unchanged means rolled back, and any partial/drifted set stays unresolved.
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

func projectCleanupProjectPredicate(stage string) string {
	if stage == "calls" {
		return "(t.project_id=$1::uuid OR t.source_project_id=$1::uuid)"
	}
	return "t.project_id=$1::uuid"
}
