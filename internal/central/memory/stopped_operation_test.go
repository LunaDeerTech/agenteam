package memory

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"reflect"
	"strings"
	"testing"
)

type stoppedOperationRow struct {
	values []any
	err    error
}

func (r stoppedOperationRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return fault(f.InvalidState)
	}
	for n, v := range r.values {
		reflect.ValueOf(dest[n]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

type stoppedOperationSQL struct {
	postgres.SQLExecutor
	t                  *testing.T
	entry              *call
	input              f.Digest
	process            oc.ProcessID
	phase              string
	missing, unsettled bool
	writes, modelReads int
	execution          string
}

func (s *stoppedOperationSQL) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	if strings.HasPrefix(q, "SELECT project_id") {
		if len(args) != 1 || args[0] != s.entry.operation.String() {
			s.t.Fatal("wrong canonical target")
		}
		if s.missing {
			return stoppedOperationRow{err: pgx.ErrNoRows}
		}
		var failure *string
		if s.phase == "failed" {
			code := string(f.DependencyUnavailable)
			failure = &code
		}
		return stoppedOperationRow{values: []any{s.entry.namespace.ProjectID.String(), s.entry.namespace.AgentID.String(), s.execution, string(s.input), s.phase, s.process.String(), failure}}
	}
	s.modelReads++
	if len(args) != 5 || args[0] != s.entry.operation.String() || args[1] != s.process.String() || args[4] != s.execution {
		s.t.Fatal("settlement lost original tuple")
	}
	return stoppedOperationRow{values: []any{s.unsettled}}
}
func (s *stoppedOperationSQL) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(q, "UPDATE agenteam_memory.operations SET phase='failed'") || len(args) != 3 || args[0] != s.entry.operation.String() || args[1] != string(f.DependencyUnavailable) {
		s.t.Fatal("recovery wrote outside original canonical failure")
	}
	s.writes++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type stoppedOperationTool struct {
	expected oc.ProcessID
	calls    int
	denied   bool
}

func (g *stoppedOperationTool) RequireStoppedMemoryToolInTx(_ context.Context, _ f.Tx, _ i.Actor, _ i.ProjectID, _ i.AgentID, _ tc.OperationID, process oc.ProcessID) error {
	g.calls++
	if g.denied || process != g.expected {
		return fault(f.Forbidden)
	}
	return nil
}
func TestMemoryStoppedRetainRequiresDeathAndAllModelOwnersSettled(t *testing.T) {
	for _, name := range []string{"settled", "legacy", "missing", "foreign-execution", "no-death-proof", "unretired", "unknown-before", "failed-receipt", "already-committed"} {
		t.Run(name, func(t *testing.T) {
			ns := c.Namespace{ProjectID: testID[i.Project](t), AgentID: testID[i.Agent](t)}
			actor, _ := i.NewAgentRun(ns.ProjectID, ns.AgentID, testID[i.Execution](t))
			entry := &call{actor: actor, namespace: ns, operation: testID[tc.Operation](t), intent: "lookup-retain"}
			x := &stoppedOperationSQL{t: t, entry: entry, input: digest([]byte("original")), process: testID[oc.Process](t), phase: "prepared", execution: actor.Details().ExecutionID}
			gate := &stoppedOperationTool{expected: x.process}
			write := true
			switch name {
			case "legacy":
				x.process = oc.ProcessID{}
			case "missing":
				x.missing = true
			case "foreign-execution":
				x.execution = testID[i.Execution](t).String()
			case "no-death-proof":
				gate.denied = true
			case "unretired":
				x.unsettled = true
			case "unknown-before":
				write = false
			case "failed-receipt":
				x.phase = "failed"
			case "already-committed":
				x.phase = "committed"
			}
			found := false
			err := (&Runtime{}).stoppedRetainInTx(context.Background(), f.Tx{}, x, gate, entry, x.input, write, &found)
			wantFound := name == "settled" || name == "failed-receipt"
			wantError := name == "legacy" || name == "foreign-execution" || name == "no-death-proof" || name == "unretired"
			if found != wantFound || (err != nil) != wantError {
				t.Fatal("incorrect original recovery outcome", found, err)
			}
			wantWrites := 0
			if name == "settled" {
				wantWrites = 1
			}
			if x.writes != wantWrites {
				t.Fatal("absence/unknown bypassed canonical death gate")
			}
			if found && gate.calls != 1 {
				t.Fatal("negative receipt lacked private original process proof")
			}
		})
	}
}
