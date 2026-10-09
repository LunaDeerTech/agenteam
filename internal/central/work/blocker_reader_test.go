package work

import (
	"context"
	"errors"
	"reflect"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This executor supplies only the count observation. Reaching the stream is
// an explicit failure sentinel, never a successful graph or database substitute.
type blockerCapacityExecutor struct {
	postgres.SQLExecutor
	counts   [5]int64
	streamed bool
	failure  error
}

type blockerCapacityRow [5]int64

func (r blockerCapacityRow) Scan(dest ...any) error {
	for n, v := range r {
		*dest[n].(*int64) = v
	}
	return nil
}
func (x *blockerCapacityExecutor) QueryRow(context.Context, string, ...any) postgres.Row {
	return blockerCapacityRow(x.counts)
}
func (x *blockerCapacityExecutor) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	x.streamed = true
	return nil, x.failure
}

func TestTaskBlockerHistoryLimitReasonAndResolveAdmission(t *testing.T) {
	r, _, _ := pureBlockerRecord(t, false)
	for _, test := range []struct {
		name    string
		counts  [5]int64
		resolve bool
		stream  bool
		fields  []f.FieldError
	}{
		{"history-full-add", [5]int64{1, 0, 4096, 0, 0}, false, false, []f.FieldError{{Path: "/blocker_id", Code: "BLOCKER_HISTORY_LIMIT"}}},
		{"history-full-resolve", [5]int64{1, 1, 4096, 1, 1}, true, true, nil},
		{"history-room-add", [5]int64{1, 0, 4095, 0, 0}, false, true, nil},
		{"project-full", [5]int64{1024, 262144, 256, 256, 256}, false, false, nil},
		{"task-full", [5]int64{1, 256, 256, 256, 256}, false, false, nil},
		{"history-corrupt-over-limit", [5]int64{1, 0, 4097, 0, 0}, false, false, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			sentinel := errors.New("count test stops before graph stream")
			x := &blockerCapacityExecutor{counts: test.counts, failure: sentinel}
			add := r.Input.Add
			if test.resolve {
				add = nil
			}
			err := validateBlockerGraph(context.Background(), &blockerScope{x: x, project: r.Project, task: r.Input.Target}, add)
			if x.streamed != test.stream {
				t.Fatal("wrong capacity admission")
			}
			if test.stream {
				if !errors.Is(err, sentinel) {
					t.Fatal("stream failure lost")
				}
				return
			}
			var fault *f.Fault
			if !errors.As(err, &fault) || fault.Code != f.ResourceBusy || !reflect.DeepEqual(fault.FieldErrors, test.fields) {
				t.Fatal("wrong capacity code or fixed field", err)
			}
		})
	}
}

func TestTaskBlockerGraphTraversesDeepBranchesAndDuplicates(t *testing.T) {
	const depth = 65000
	nodes := make([]c.TaskID, depth)
	for n := range nodes {
		nodes[n] = pureID[c.Task](t, n+100)
	}
	edges := make(map[c.TaskID][]c.TaskID, depth)
	for n := 0; n < depth-1; n++ {
		edges[nodes[n]] = []c.TaskID{nodes[n+1], nodes[n+1]}
	}
	if !blockerReachable(edges, nodes[0], nodes[depth-1]) || blockerReachable(edges, nodes[depth-1], nodes[0]) {
		t.Fatal("deep graph")
	}
	edges[nodes[depth-1]] = []c.TaskID{nodes[3]}
	if blockerReachable(edges, nodes[1], pureID[c.Task](t, depth+1000)) {
		t.Fatal("cycle termination")
	}
	unrelated := pureID[c.Task](t, depth+2000)
	edges[nodes[0]] = append([]c.TaskID{unrelated}, edges[nodes[0]]...)
	if !blockerReachable(edges, nodes[0], nodes[depth-1]) {
		t.Fatal("branch prematurely terminated")
	}
	delete(edges, nodes[depth/2])
	if blockerReachable(edges, nodes[0], nodes[depth-1]) {
		t.Fatal("removed resolved edge still traversed")
	}
}
func TestTaskBlockerGateOrderingAndOverflow(t *testing.T) {
	r, _, _ := pureBlockerRecord(t, false)
	task := r.Plan.Before
	task.State = c.TaskStateDone
	pureCode(t, blockerTaskGate(task, task.Version+1), f.TaskVersionConflict)
	pureCode(t, blockerTaskGate(task, task.Version), f.TaskTerminalImmutable)
	task.State = c.TaskStateBlocked
	pureCode(t, blockerTaskGate(task, task.Version), f.DependencyUnbound)
	task = r.Plan.Before
	if e := blockerTaskGate(task, task.Version); e != nil {
		t.Fatal(e)
	}
}
