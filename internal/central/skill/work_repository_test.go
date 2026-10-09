package skill

import (
	"context"
	"errors"
	"testing"
	"time"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type workExecutorControl struct {
	postgres.SQLExecutor
	row    skillRowValues
	writes int
	args   []any
	count  string
}

func (x *workExecutorControl) QueryRow(context.Context, string, ...any) postgres.Row { return x.row }
func (x *workExecutorControl) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	x.writes++
	x.args = args
	return pgconn.NewCommandTag(x.count), nil
}
func TestSkillWorkRetainsExactOwnerAndActualJoinRequirement(t *testing.T) {
	r := stateRow(t)
	w := workFact{id: stateID[skillWork](121), project: r.request.ProjectID, skill: r.skill, process: stateID[oc.Process](122), kind: initializationWork, phase: workRunning, fence: 1, created: r.created}
	base := []any{w.id.String(), w.project.String(), w.skill.String(), w.process.String(), string(w.kind), string(w.phase), int64(1), w.created.Time(), (*time.Time)(nil)}
	for _, name := range []string{"running", "unknown", "joined", "missing", "wrong_process", "wrong_fence", "invalid_phase", "joined_no_time", "raced_fence", "driver_error"} {
		t.Run(name, func(t *testing.T) {
			values := append([]any(nil), base...)
			x := &workExecutorControl{row: skillRowValues{values: values}, count: "UPDATE 1"}
			switch name {
			case "unknown":
				values[5] = "unknown"
			case "joined":
				values[5] = "joined"
				at := w.created.Time()
				values[8] = &at
			case "missing":
				x.row.err = pgx.ErrNoRows
			case "wrong_process":
				values[3] = stateID[oc.Process](123).String()
			case "wrong_fence":
				values[6] = int64(2)
			case "invalid_phase":
				values[5] = "cancelled"
			case "joined_no_time":
				values[5] = "joined"
			case "raced_fence":
				x.count = "UPDATE 0"
			case "driver_error":
				x.row.err = errors.New("unavailable")
			}
			e := joinWork(context.Background(), x, w)
			if name == "running" || name == "unknown" {
				if e != nil || x.writes != 1 || x.args[1] != w.process.String() || x.args[2] != int64(1) {
					t.Fatal("original owner/fence changed", e)
				}
			} else if name == "joined" {
				if e != nil || x.writes != 0 {
					t.Fatal("joined row rewritten", e)
				}
			} else {
				if e == nil {
					t.Fatal("unproven join accepted")
				}
				if name != "raced_fence" && x.writes != 0 {
					t.Fatal("invalid identity wrote joined")
				}
			}
		})
	}
}
