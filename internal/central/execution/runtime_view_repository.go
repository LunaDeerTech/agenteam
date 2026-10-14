package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func finalRuntimeSnapshot(round executionRound, terminal *directTextTerminal) (*ec.RuntimeSnapshot, error) {
	r := round.Fields()
	if terminal == nil || (r.Input.SchemaVersion != 1 && !isLoopRound(round)) || !terminal.status.Terminal() || terminal.at.Time().Before(r.CreatedAt.Time()) {
		return nil, invalid()
	}
	schema := ec.DurableFinalTextV1
	if isLoopRound(round) {
		schema = ec.DurableFinalTextV2
	}
	s := &ec.RuntimeSnapshot{Schema: schema, Generation: r.SnapshotID, IncludedUpdateProgress: 1, ExecutionVersion: terminal.version, Items: []ec.RuntimeTextItem{}}
	if terminal.status != ec.Succeeded {
		if terminal.response != nil || len(terminal.responseBytes) != 0 {
			return nil, invalid()
		}
		return s, nil
	}
	if terminal.response == nil || !stableDirectTextResponse(*terminal.response, r.CallID) {
		return nil, invalid()
	}
	var text strings.Builder
	for _, part := range terminal.response.Message.Parts {
		if text.Len()+len(part.Text.Text) > ec.MaxRuntimeTextBytes {
			return nil, invalid()
		}
		text.WriteString(part.Text.Text)
	}
	s.Items = append(s.Items, ec.RuntimeTextItem{ID: r.ID, ExecutionID: r.ExecutionID, Seq: 1, Type: "text", Status: "completed", Revision: 1, StartedAt: r.CreatedAt, UpdatedAt: terminal.at, CompletedAt: terminal.at, SourceReference: ec.RuntimeRoundReference{Kind: "execution_round", ID: r.ID}, Data: ec.RuntimeTextData{Content: text.String()}})
	return s, nil
}

func runtimeSnapshotBytes(s *ec.RuntimeSnapshot) ([]byte, error) {
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > ec.MaxRuntimeViewBytes {
		return nil, invalid()
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil || len(raw) > ec.MaxRuntimeViewBytes {
		return nil, invalid()
	}
	return raw, nil
}

// Called only from the original DirectText terminal transaction, after the
// original Model handle joined. No body, credentials, metadata or errors from
// that handle enter this projection: only its validated public text parts.
func saveDirectTextRuntime(ctx context.Context, x postgres.SQLExecutor, run *directTextCall) error {
	if run == nil || run.owner == nil {
		return invalid()
	}
	if run.owner.tools != nil && run.owner.multi == nil {
		return nil
	}
	s, err := finalRuntimeSnapshot(run.round, run.terminal)
	if err != nil {
		return err
	}
	raw, err := runtimeSnapshotBytes(s)
	if err != nil {
		return err
	}
	r := run.round.Fields()
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.runtime_snapshots(execution_id,project_id,snapshot_id,round_id,terminal_status,terminal_version,completed_at,content,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, r.ExecutionID.String(), run.request.Launch.ProjectID.String(), r.SnapshotID.String(), r.ID.String(), string(run.terminal.status), int64(run.terminal.version), run.terminal.at.Time(), raw, string(ec.TriggerInputDigest(raw)))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	return ctx.Err()
}

// A terminal COMMIT Unknown can be confirmed only with the exact complete
// projection from the original candidate, not merely a visible terminal row.
func confirmDirectTextRuntime(ctx context.Context, x postgres.SQLExecutor, run *directTextCall) error {
	if run == nil || run.owner == nil {
		return invalid()
	}
	if run.owner.tools != nil && run.owner.multi == nil {
		return nil
	}
	s, err := finalRuntimeSnapshot(run.round, run.terminal)
	if err != nil {
		return err
	}
	expected, err := runtimeSnapshotBytes(s)
	if err != nil {
		return err
	}
	r := run.round.Fields()
	var raw []byte
	var digest string
	err = x.QueryRow(ctx, `SELECT content,digest FROM agenteam_execution.runtime_snapshots WHERE execution_id=$1 AND project_id=$2 AND snapshot_id=$3 AND round_id=$4 AND terminal_status=$5 AND terminal_version=$6 AND completed_at=$7`, r.ExecutionID.String(), run.request.Launch.ProjectID.String(), r.SnapshotID.String(), r.ID.String(), string(run.terminal.status), int64(run.terminal.version), run.terminal.at.Time()).Scan(&raw, &digest)
	if err != nil {
		return unavailable(err)
	}
	if !bytes.Equal(raw, expected) || digest != string(ec.TriggerInputDigest(expected)) {
		return unavailable(nil)
	}
	return ctx.Err()
}

type runtimeProjection struct {
	raw                                      []byte
	digest, project, snapshot, round, status *string
	version                                  *int64
	at                                       *time.Time
}
type taskRuntimeRow struct {
	row        postgres.Row
	projection *runtimeProjection
}

func (r taskRuntimeRow) Scan(dest ...any) error {
	p := r.projection
	return r.row.Scan(append(dest, &p.raw, &p.digest, &p.project, &p.snapshot, &p.round, &p.status, &p.version, &p.at)...)
}

func loadTaskRuntimeView(ctx context.Context, x postgres.SQLExecutor, project i.ProjectID, task string, id i.ExecutionID) (ec.TaskRuntimeView, error) {
	var out ec.TaskRuntimeView
	p := runtimeProjection{}
	// Canonical state, reason and the immutable complete runtime projection are
	// selected in one statement; no cross-query read-committed snapshot gap.
	query := `SELECT current.*,v.content,v.digest,v.project_id::text,v.snapshot_id::text,v.round_id::text,v.terminal_status,v.terminal_version,v.completed_at FROM (` + taskExecutionSelect + ` WHERE e.id=$1 AND e.project_id=$2 AND e.launch_request->'trigger'->>'kind'='task' AND e.launch_request->'trigger'->>'task_id'=$3) current LEFT JOIN agenteam_execution.runtime_snapshots v ON v.execution_id=current.id`
	e, err := scanTaskExecution(taskRuntimeRow{x.QueryRow(ctx, query, id.String(), project.String(), task), &p}, project, task)
	if err != nil {
		return out, err
	}
	if e == nil {
		return out, fault(f.NotFound)
	}
	out.Execution = *e
	if p.raw == nil {
		if p.digest != nil || p.project != nil || p.snapshot != nil || p.round != nil || p.status != nil || p.version != nil || p.at != nil {
			return ec.TaskRuntimeView{}, unavailable(nil)
		}
		out.Availability = ec.RuntimePending
		if e.Status.Terminal() {
			out.Availability = ec.RuntimeNotRecorded
		}
		return out, ctx.Err()
	}
	if len(p.raw) > ec.MaxRuntimeViewBytes || p.digest == nil || *p.digest != string(ec.TriggerInputDigest(p.raw)) || p.project == nil || *p.project != project.String() || p.snapshot == nil || e.SnapshotID == nil || *p.snapshot != e.SnapshotID.String() || p.round == nil || p.status == nil || *p.status != string(e.Status) || p.version == nil || *p.version != int64(e.Version) || p.at == nil || e.CompletedAt == nil || !p.at.Equal(e.CompletedAt.Time()) {
		return ec.TaskRuntimeView{}, unavailable(nil)
	}
	var snapshot ec.RuntimeSnapshot
	if err = decodeDirectTextJSON(p.raw, &snapshot); err != nil {
		return ec.TaskRuntimeView{}, unavailable(err)
	}
	canonical, err := runtimeSnapshotBytes(&snapshot)
	if err != nil || !bytes.Equal(canonical, p.raw) {
		return ec.TaskRuntimeView{}, unavailable(nil)
	}
	if _, err = f.ParseID[ec.Round](*p.round); err != nil {
		return ec.TaskRuntimeView{}, unavailable(nil)
	}
	if len(snapshot.Items) == 1 && snapshot.Items[0].ID.String() != *p.round {
		return ec.TaskRuntimeView{}, unavailable(nil)
	}
	out.Availability, out.Snapshot = ec.RuntimeAvailable, &snapshot
	if err = out.ValidateProjection(); err != nil {
		return ec.TaskRuntimeView{}, err
	}
	return out, ctx.Err()
}
