//go:build integration

package objects_test

import (
	"fmt"
	"testing"
)

type metadataCostPlanNode struct {
	NodeType     string                 `json:"Node Type"`
	Relation     string                 `json:"Relation Name"`
	Rows         float64                `json:"Actual Rows"`
	Loops        float64                `json:"Actual Loops"`
	Filtered     float64                `json:"Rows Removed by Filter"`
	JoinFiltered float64                `json:"Rows Removed by Join Filter"`
	Rechecked    float64                `json:"Rows Removed by Index Recheck"`
	HeapFetches  float64                `json:"Heap Fetches"`
	SharedHit    float64                `json:"Shared Hit Blocks"`
	SharedRead   float64                `json:"Shared Read Blocks"`
	LocalHit     float64                `json:"Local Hit Blocks"`
	LocalRead    float64                `json:"Local Read Blocks"`
	Plans        []metadataCostPlanNode `json:"Plans"`
}

// A planned branch with zero loops is not execution coverage. Check the
// relation's actual scan, without requiring a particular index or node type.
func metadataCostRelationExecuted(plan metadataCostPlanNode, relation string, nonempty bool) bool {
	if plan.Relation == relation && plan.Loops > 0 && (plan.Rows > 0) == nonempty {
		return true
	}
	for _, child := range plan.Plans {
		if metadataCostRelationExecuted(child, relation, nonempty) {
			return true
		}
	}
	return false
}

func metadataCostPlanWithinFixtureBounds(plan metadataCostPlanNode) error {
	// These SELECT fixtures deliberately separate large terminal histories
	// (65/1001/10001) from small current sets: <=34 leases, <=33 readers/grants,
	// <=32 transfers, <=17 pending attempts, and <=33 history/page results.
	// A per-node allowance of 64 tuples/probes leaves room for those current
	// sets, while rejecting a scan of the smallest terminal-history fixture.
	// PostgreSQL reports rows/removed rows as per-loop averages. Multiply by
	// actual loops and also bound empty probes; Index Scan is not itself proof
	// of finite work. Buffers are already totals and must not be multiplied.
	const tuples, probes, blocks = 64, 64, 8 * 64
	if plan.Loops > 0 {
		visited := (plan.Rows + plan.Filtered + plan.JoinFiltered + plan.Rechecked) * plan.Loops
		buffered := plan.SharedHit + plan.SharedRead + plan.LocalHit + plan.LocalRead
		if visited > tuples || plan.HeapFetches > tuples || plan.Loops > probes || buffered > blocks {
			return fmt.Errorf("node=%s visited=%g heap_fetches=%g loops=%g blocks=%g", plan.NodeType, visited, plan.HeapFetches, plan.Loops, buffered)
		}
	}
	for _, child := range plan.Plans {
		if err := metadataCostPlanWithinFixtureBounds(child); err != nil {
			return err
		}
	}
	return nil
}

func TestObjectMetadataCostPlanWorkBounds(t *testing.T) {
	t.Run("actual_relation_execution", func(t *testing.T) {
		plan := metadataCostPlanNode{NodeType: "Result", Rows: 1, Loops: 1, Plans: []metadataCostPlanNode{
			{Relation: "project_work", Rows: 1, Loops: 0},
			{Relation: "object_transfers", Loops: 1},
		}}
		if metadataCostRelationExecuted(plan, "project_work", true) || metadataCostRelationExecuted(plan, "project_work", false) || metadataCostRelationExecuted(plan, "object_transfers", true) || !metadataCostRelationExecuted(plan, "object_transfers", false) {
			t.Fatal("planned or empty node was credited as nonempty execution")
		}
		plan.Plans[0].Loops = 1
		if !metadataCostRelationExecuted(plan, "project_work", true) || metadataCostRelationExecuted(plan, "project_work", false) {
			t.Fatal("actual nonempty relation was not distinguished from the empty tail")
		}
	})
	for _, test := range []struct {
		name string
		plan metadataCostPlanNode
		fail bool
	}{
		{"bounded_history_page", metadataCostPlanNode{NodeType: "Index Only Scan", Rows: 33, Filtered: 1, Loops: 1, SharedHit: 6}, false},
		{"small_sequential_table", metadataCostPlanNode{NodeType: "Seq Scan", Rows: 2, Filtered: 1, Loops: 1, SharedHit: 1}, false},
		{"unexecuted_branch", metadataCostPlanNode{NodeType: "Seq Scan", Rows: 13134, Loops: 0}, false},
		// Actual 7868 regressions, even though their parents returned one bool
		// and the total query completed within the original two-second limit.
		{"retired_transfer_scan", metadataCostPlanNode{NodeType: "Seq Scan", Rows: 81, Filtered: 1017, Loops: 1, SharedHit: 123}, true},
		{"retired_transfer_batch_join", metadataCostPlanNode{NodeType: "Bitmap Heap Scan", Rows: 81, Filtered: 16, Loops: 1}, true},
		{"retired_lease_scan", metadataCostPlanNode{NodeType: "Seq Scan", Rows: 13134, Loops: 1, SharedHit: 215}, true},
		{"revoked_grant_index_filter", metadataCostPlanNode{NodeType: "Index Scan", Filtered: 1034, Loops: 1, SharedHit: 36}, true},
		{"nested_loop_amplification", metadataCostPlanNode{NodeType: "Index Scan", Rows: 2, Loops: 33, SharedHit: 99}, true},
		{"empty_probe_amplification", metadataCostPlanNode{NodeType: "Index Scan", Loops: 65, SharedHit: 130}, true},
		{"index_recheck_work", metadataCostPlanNode{NodeType: "Bitmap Heap Scan", Rows: 1, Rechecked: 64, Loops: 1}, true},
		{"join_filter_work", metadataCostPlanNode{NodeType: "Nested Loop", Rows: 1, JoinFiltered: 64, Loops: 1}, true},
		{"buffer_work", metadataCostPlanNode{NodeType: "Index Only Scan", Loops: 1, SharedHit: 500, SharedRead: 13}, true},
		{"index_visibility_work", metadataCostPlanNode{NodeType: "Index Only Scan", Rows: 1, Loops: 1, HeapFetches: 65}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Always put the measured node below a successful one-row result.
			root := metadataCostPlanNode{NodeType: "Result", Rows: 1, Loops: 1, Plans: []metadataCostPlanNode{test.plan}}
			if err := metadataCostPlanWithinFixtureBounds(root); (err != nil) != test.fail {
				t.Fatalf("recursive work bound: got=%v reject=%t", err, test.fail)
			}
		})
	}
}
