package work

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

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
