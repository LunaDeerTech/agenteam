package work

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestTaskBlockerManagementPreservesTaskAndHistoricalReceipt(t *testing.T) {
	for _, state := range []c.TaskState{c.TaskStateBacklog, c.TaskStateTodo, c.TaskStateInProgress, c.TaskStateInReview, c.TaskStateBlocked} {
		t.Run(string(state), func(t *testing.T) {
			for _, resolved := range []bool{false, true} {
				r, actor, summary := pureBlockerRecord(t, resolved)
				agent := pureID[i.Agent](t, 733)
				r.Plan.Before.State, r.Plan.After.Task.State = state, state
				r.Plan.Before.AssigneeAgentID, r.Plan.After.Task.AssigneeAgentID = &agent, &agent
				if err := blockerTaskGate(r.Plan.Before, r.Input.Expected); err != nil {
					t.Fatal("assigned nonterminal gate", err)
				}
				if err := validateBlockerRecord(r, actor); err != nil {
					t.Fatal("unchanged state and assignment", err)
				}
				if _, _, _, err := blockerEventBinding(r, actor, summary); err != nil {
					t.Fatal("real producer binding rejected", err)
				}
				raw, err := json.Marshal(r.Plan)
				var decoded blockerPlan
				if err != nil || json.Unmarshal(raw, &decoded) != nil || !sameValue(decoded, *r.Plan) {
					t.Fatal("persisted plan changed state or assignment", err)
				}
				for _, change := range []func(*c.Task){
					func(task *c.Task) { task.AssigneeAgentID = nil },
					func(task *c.Task) { next := pureID[i.Agent](t, 734); task.AssigneeAgentID = &next },
					func(task *c.Task) { task.State = c.TaskStateCancelled },
					func(task *c.Task) { task.ManualRank = "00000000000000000000000000000001" },
				} {
					original := r.Plan.After.Task.Clone()
					change(&r.Plan.After.Task)
					if validateBlockerRecord(r, actor) == nil {
						t.Fatal("Blocker command altered Task workflow")
					}
					r.Plan.After.Task = original
				}
				r.State = "completed"
				receipt, at := r.Plan.After.Clone(), r.Created
				r.Receipt, r.Committed = &receipt, &at
				if err = validateBlockerRecord(r, actor); err != nil {
					t.Fatal("historical receipt requires mutable current Task", err)
				}
			}
		})
	}
	for _, terminal := range []c.TaskState{c.TaskStateDone, c.TaskStateCancelled} {
		r, actor, _ := pureBlockerRecord(t, false)
		r.Plan.Before.State, r.Plan.After.Task.State = terminal, terminal
		pureCode(t, blockerTaskGate(r.Plan.Before, r.Input.Expected), f.TaskTerminalImmutable)
		if validateBlockerRecord(r, actor) == nil {
			t.Fatal("terminal mutation plan accepted")
		}
	}
}

type managementCountSQL struct {
	postgres.SQLExecutor
	project c.ProjectID
	task    c.TaskID
	count   int64
	err     error
	queries int
}

type managementCountRow struct {
	count int64
	err   error
}

func (r managementCountRow) Scan(out ...any) error {
	if r.err != nil {
		return r.err
	}
	*out[0].(*int64) = r.count
	return nil
}

func (x *managementCountSQL) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	x.queries++
	if !strings.Contains(query, "project_id=$1 AND task_id=$2 AND resolved_at IS NULL") || !reflect.DeepEqual(args, []any{x.project.String(), x.task.String()}) {
		return managementCountRow{err: errors.New("unscoped unresolved read")}
	}
	return managementCountRow{x.count, x.err}
}

func TestTaskBlockerManagementLastCauseAndTechnicalBoundary(t *testing.T) {
	r, _, _ := pureBlockerRecord(t, true)
	task, blocker := r.Plan.Before, *r.Plan.BlockerBefore
	task.State = c.TaskStateBlocked
	for _, count := range []int64{2, 1, 0, -1} {
		x := &managementCountSQL{project: task.ProjectID, task: task.ID, count: count}
		scope := &blockerScope{x: x, project: task.ProjectID, task: task.ID}
		err := checkStandaloneBlockerResolution(context.Background(), scope, task, blocker)
		switch count {
		case 2:
			if err != nil {
				t.Fatal("non-last blocker", err)
			}
		case 1:
			pureCode(t, err, f.LastBlockerNeedsTransfer)
		default:
			pureCode(t, err, f.InternalError)
		}
		if x.queries != 1 {
			t.Fatal("actual unresolved read missing")
		}
	}
	sentinel := errors.New("original count read failed")
	x := &managementCountSQL{project: task.ProjectID, task: task.ID, err: sentinel}
	scope := &blockerScope{x: x, project: task.ProjectID, task: task.ID}
	if err := checkStandaloneBlockerResolution(context.Background(), scope, task, blocker); !errors.Is(err, sentinel) {
		t.Fatal("failed SQL became an empty or successful result", err)
	}
	for _, kind := range []c.TaskBlockerType{c.TaskBlockerRelyOn, c.TaskBlockerWaitingForHuman, c.TaskBlockerTechnical} {
		blocker.Type = kind
		task.State = c.TaskStateTodo
		x.queries = 0
		err := checkStandaloneBlockerResolution(context.Background(), scope, task, blocker)
		if kind == c.TaskBlockerTechnical {
			pureCode(t, err, f.DependencyUnbound)
		} else if err != nil {
			t.Fatal(err)
		}
		if x.queries != 0 {
			t.Fatal("unneeded count or technical ownership query")
		}
	}
}
