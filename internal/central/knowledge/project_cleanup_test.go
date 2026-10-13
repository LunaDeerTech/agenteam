package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// This bounded executor checks repository preimages and query scoping. It is
// deliberately not a PostgreSQL/FK/Object-retirement authorization substitute.
type knowledgeCleanupPureSQL struct {
	postgres.SQLExecutor
	rows            map[string]map[string]json.RawMessage
	queries, writes int
	readOverride    []byte
}

var knowledgeCleanupPureTables = []string{"documents", "command_events", "object_cleanup", "publications", "work_claims", "commands", "project_object_retirements", "project_calls"}

func knowledgeCleanupPureTable(sql string) string {
	for _, table := range knowledgeCleanupPureTables {
		if strings.Contains(sql, "agenteam_knowledge."+table+" t ") {
			return table
		}
	}
	return ""
}
func knowledgeCleanupPureFields(raw []byte) (map[string]json.RawMessage, error) {
	var value map[string]json.RawMessage
	err := json.Unmarshal(raw, &value)
	return value, err
}
func knowledgeCleanupPureString(fields map[string]json.RawMessage, name string) string {
	var out string
	_ = json.Unmarshal(fields[name], &out)
	return out
}
func knowledgeCleanupPureJSON(value any) []byte {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		panic(err)
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n"))
}
func knowledgeCleanupPureRow(t *testing.T, project id.ProjectID, table string, extra map[string]any) projectCleanupRow {
	t.Helper()
	key := newID[struct{}](t).String()
	keyField := "id"
	if table == "publications" || table == "work_claims" {
		keyField = "command_id"
	}
	value := map[string]any{keyField: key, "project_id": project.String(), "untouched_payload": map[string]any{"revision": "17", "text": "<>& original"}}
	if table == "project_calls" {
		value["phase"] = "business_closed"
	}
	if table == "project_object_retirements" {
		value["phase"] = "completed"
	}
	for name, v := range extra {
		value[name] = v
	}
	return projectCleanupRow{Key: key, Record: knowledgeCleanupPureJSON(value)}
}
func (x *knowledgeCleanupPureSQL) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	x.queries++
	return publicationCheckpointRow(func(dest ...any) error {
		if x.readOverride != nil {
			*dest[0].(*[]byte) = append([]byte(nil), x.readOverride...)
			return nil
		}
		table := knowledgeCleanupPureTable(sql)
		if table == "" || len(args) < 1 || !strings.Contains(sql, "to_jsonb(t) AS record") || !strings.Contains(sql, "t.project_id=$1::uuid") {
			return errors.New("unbounded or unscoped cleanup read")
		}
		limit := 100
		if table == "commands" {
			limit = 1
		}
		if !strings.Contains(sql, fmt.Sprintf(" LIMIT %d) selected", limit)) {
			return errors.New("missing pre-aggregation row bound")
		}
		if table == "project_calls" && !strings.Contains(sql, "OR t.source_project_id=$1::uuid") {
			return errors.New("lost source project ownership")
		}
		var keys map[string]bool
		if len(args) == 2 {
			if !strings.Contains(sql, "=ANY($2::text[])") {
				return errors.New("confirmation is not restricted to original keys")
			}
			keys = map[string]bool{}
			for _, key := range args[1].([]string) {
				keys[key] = true
			}
		}
		out := make([]projectCleanupRow, 0)
		for key, raw := range x.rows[table] {
			fields, err := knowledgeCleanupPureFields(raw)
			if err != nil {
				return err
			}
			if knowledgeCleanupPureString(fields, "project_id") != args[0] && (table != "project_calls" || knowledgeCleanupPureString(fields, "source_project_id") != args[0]) {
				continue
			}
			if keys != nil && !keys[key] {
				continue
			}
			if table == "documents" && keys == nil {
				if !strings.Contains(sql, "child.project_id=t.project_id AND child.parent_document_id=t.id") {
					return errors.New("document parent selected before its leaves")
				}
				parent := false
				for _, other := range x.rows[table] {
					child, _ := knowledgeCleanupPureFields(other)
					if knowledgeCleanupPureString(child, "project_id") == args[0] && knowledgeCleanupPureString(child, "parent_document_id") == key {
						parent = true
					}
				}
				if parent {
					continue
				}
			}
			out = append(out, projectCleanupRow{Key: key, Record: append([]byte(nil), raw...)})
		}
		sort.Slice(out, func(a, b int) bool { return out[a].Key < out[b].Key })
		if len(out) > limit {
			out = out[:limit]
		}
		*dest[0].(*[]byte) = knowledgeCleanupPureJSON(out)
		return nil
	})
}
func (x *knowledgeCleanupPureSQL) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	x.writes++
	table := knowledgeCleanupPureTable(sql)
	if table == "" || len(args) != 2 || !strings.HasPrefix(sql, "DELETE FROM agenteam_knowledge.") || !strings.Contains(sql, "t.project_id=$1::uuid") || !strings.Contains(sql, "to_jsonb(t)=selected.record") {
		return pgconn.CommandTag{}, errors.New("cleanup write lost project or complete preimage")
	}
	if table == "project_calls" && (!strings.Contains(sql, "OR t.source_project_id=$1::uuid") || !strings.Contains(sql, "t.phase='business_closed'")) {
		return pgconn.CommandTag{}, errors.New("cleanup lost closed dual-project call constraint")
	}
	var selected []projectCleanupRow
	if err := json.Unmarshal([]byte(args[1].(string)), &selected); err != nil {
		return pgconn.CommandTag{}, err
	}
	count := 0
	for _, row := range selected {
		current, ok := x.rows[table][row.Key]
		if !ok || !bytes.Equal(current, row.Record) {
			continue
		}
		fields, _ := knowledgeCleanupPureFields(current)
		if knowledgeCleanupPureString(fields, "project_id") != args[0] && (table != "project_calls" || knowledgeCleanupPureString(fields, "source_project_id") != args[0]) {
			continue
		}
		delete(x.rows[table], row.Key)
		count++
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", count)), nil
}

func TestKnowledgeProjectCleanupBoundedFullRowsKeepOtherProject(t *testing.T) {
	a, b := newID[id.Project](t), newID[id.Project](t)
	x := &knowledgeCleanupPureSQL{rows: map[string]map[string]json.RawMessage{}}
	preserved := map[string]projectCleanupRow{}
	for _, table := range knowledgeCleanupPureTables {
		x.rows[table] = map[string]json.RawMessage{}
		ours, other := knowledgeCleanupPureRow(t, a, table, nil), knowledgeCleanupPureRow(t, b, table, nil)
		x.rows[table][ours.Key] = ours.Record
		x.rows[table][other.Key] = other.Record
		preserved[table] = other
	}
	var parentKey string
	for key, raw := range x.rows["documents"] {
		fields, _ := knowledgeCleanupPureFields(raw)
		if knowledgeCleanupPureString(fields, "project_id") == a.String() {
			parentKey = key
		}
	}
	child := knowledgeCleanupPureRow(t, a, "documents", map[string]any{"parent_document_id": parentKey})
	x.rows["documents"][child.Key] = child.Record
	secondCommand := knowledgeCleanupPureRow(t, a, "commands", nil)
	x.rows["commands"][secondCommand.Key] = secondCommand.Record
	// A is the source of this B target call, so only this B row belongs to A's
	// cleanup as well. The unrelated B call above must survive byte-for-byte.
	dual := knowledgeCleanupPureRow(t, b, "project_calls", map[string]any{"source_project_id": a.String()})
	x.rows["project_calls"][dual.Key] = dual.Record
	var stages []string
	for range 12 {
		batch, err := readProjectCleanupBatch(context.Background(), x, a)
		if err != nil {
			t.Fatal(err)
		}
		if batch == nil {
			break
		}
		if len(stages) == 0 && (batch.stage != "documents" || len(batch.rows) != 1 || batch.rows[0].Key != child.Key) {
			t.Fatal("first cleanup did not select only the document leaf")
		}
		stages = append(stages, batch.stage)
		if err = deleteProjectCleanupBatch(context.Background(), x, a, batch); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(stages, ",") != "documents,documents,command_events,object_cleanup,publications,work_claims,commands,commands,objects,calls" {
		t.Fatalf("wrong bounded FK cleanup order: %v", stages)
	}
	for _, table := range knowledgeCleanupPureTables {
		other := preserved[table]
		if len(x.rows[table]) != 1 || !bytes.Equal(x.rows[table][other.Key], other.Record) {
			t.Fatalf("cleanup lost unrelated B facts in %s", table)
		}
	}
}

func TestKnowledgeProjectCleanupRejectsDriftAndMalformedBatches(t *testing.T) {
	t.Run("complete preimage and exact count", func(t *testing.T) {
		p := newID[id.Project](t)
		row := knowledgeCleanupPureRow(t, p, "commands", nil)
		x := &knowledgeCleanupPureSQL{rows: map[string]map[string]json.RawMessage{"commands": {row.Key: row.Record}}}
		rows, err := readProjectCleanupRows(context.Background(), x, p, "commands", nil)
		if err != nil {
			t.Fatal(err)
		}
		fields, _ := knowledgeCleanupPureFields(row.Record)
		fields["untouched_payload"] = json.RawMessage(`{"revision":"18","text":"same id, changed receipt"}`)
		changed := knowledgeCleanupPureJSON(fields)
		x.rows["commands"][row.Key] = changed
		err = deleteProjectCleanupBatch(context.Background(), x, p, &projectCleanupBatch{stage: "commands", rows: rows})
		knowledgeProjectAssertFault(t, err, f.VersionConflict)
		if !bytes.Equal(x.rows["commands"][row.Key], changed) {
			t.Fatal("same key allowed removal of a changed complete row")
		}
	})
	for _, mode := range []string{"foreign_project", "wrong_key", "duplicate", "command_batch", "general_batch", "open_call", "unfinished_object", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			p := newID[id.Project](t)
			stage, table := "documents", "documents"
			if mode == "command_batch" {
				stage, table = "commands", "commands"
			}
			if mode == "open_call" {
				stage, table = "calls", "project_calls"
			}
			if mode == "unfinished_object" {
				stage, table = "objects", "project_object_retirements"
			}
			row := knowledgeCleanupPureRow(t, p, table, nil)
			rows := []projectCleanupRow{row}
			want := f.DependencyUnavailable
			switch mode {
			case "foreign_project":
				rows[0] = knowledgeCleanupPureRow(t, newID[id.Project](t), table, nil)
			case "wrong_key":
				rows[0].Key = newID[struct{}](t).String()
			case "duplicate", "command_batch":
				rows = append(rows, row)
			case "general_batch":
				for len(rows) < 101 {
					rows = append(rows, row)
				}
			case "open_call", "unfinished_object":
				fields, _ := knowledgeCleanupPureFields(row.Record)
				fields["phase"] = json.RawMessage(`"running"`)
				rows[0].Record = knowledgeCleanupPureJSON(fields)
				want = f.ResourceBusy
			case "oversized":
				rows[0].Record = json.RawMessage(`{"id":"` + row.Key + `","project_id":"` + p.String() + `","body":"` + strings.Repeat("x", 10<<20) + `"}`)
				want = f.ResourceBusy
			}
			x := &knowledgeCleanupPureSQL{readOverride: knowledgeCleanupPureJSON(rows)}
			_, err := readProjectCleanupRows(context.Background(), x, p, stage, nil)
			knowledgeProjectAssertFault(t, err, want)
			if x.writes != 0 {
				t.Fatal("invalid read batch performed a cleanup write")
			}
		})
	}
	t.Run("large legal command does not expand HTML", func(t *testing.T) {
		p := newID[id.Project](t)
		row := knowledgeCleanupPureRow(t, p, "commands", map[string]any{"plan": strings.Repeat("<>&", 900000)})
		x := &knowledgeCleanupPureSQL{rows: map[string]map[string]json.RawMessage{"commands": {row.Key: row.Record}}}
		rows, err := readProjectCleanupRows(context.Background(), x, p, "commands", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err = deleteProjectCleanupBatch(context.Background(), x, p, &projectCleanupBatch{stage: "commands", rows: rows}); err != nil || len(x.rows["commands"]) != 0 {
			t.Fatal("bounded full JSON was unnecessarily expanded or not removed", err)
		}
	})
}

func TestKnowledgeProjectCleanupConfirmationUsesOnlyOriginalCompleteRows(t *testing.T) {
	for _, mode := range []string{"unchanged", "absent", "partial", "drifted"} {
		t.Run(mode, func(t *testing.T) {
			p := newID[id.Project](t)
			first, second := knowledgeCleanupPureRow(t, p, "command_events", nil), knowledgeCleanupPureRow(t, p, "command_events", nil)
			rows := []projectCleanupRow{first, second}
			sort.Slice(rows, func(a, b int) bool { return rows[a].Key < rows[b].Key })
			x := &knowledgeCleanupPureSQL{rows: map[string]map[string]json.RawMessage{"command_events": {first.Key: first.Record, second.Key: second.Record}}}
			unrelated := knowledgeCleanupPureRow(t, p, "command_events", nil)
			x.rows["command_events"][unrelated.Key] = unrelated.Record
			switch mode {
			case "absent":
				delete(x.rows["command_events"], first.Key)
				delete(x.rows["command_events"], second.Key)
			case "partial":
				delete(x.rows["command_events"], first.Key)
			case "drifted":
				fields, _ := knowledgeCleanupPureFields(first.Record)
				fields["untouched_payload"] = json.RawMessage(`{"revision":"99"}`)
				x.rows["command_events"][first.Key] = knowledgeCleanupPureJSON(fields)
			}
			applied, err := confirmProjectCleanupBatch(context.Background(), x, p, &projectCleanupBatch{stage: "command_events", rows: rows})
			if mode == "partial" || mode == "drifted" {
				knowledgeProjectAssertFault(t, err, f.ConfirmationStale)
			} else if err != nil || applied != (mode == "absent") {
				t.Fatal("wrong exact preimage confirmation", err)
			}
			if x.writes != 0 || !bytes.Equal(x.rows["command_events"][unrelated.Key], unrelated.Record) {
				t.Fatal("read-only confirmation consumed a new row or wrote")
			}
		})
	}
}

func TestKnowledgeProjectCleanupUnknownWaitsForOriginalConfirmation(t *testing.T) {
	x := newKnowledgeProjectPureFixture(t)
	actor, cause, scope := x.stopIdentity(t, x.a)
	participant, err := NewProjectCleanupParticipant(x.pair, x.gate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewProjectCleanupParticipant(x.pair, x.gate)
	if err != nil {
		t.Fatal(err)
	}
	call, err := x.pair.state.borrowProjectCleanup(context.Background(), actor, cause, scope, nil, x.gate)
	if err != nil {
		t.Fatal(err)
	}
	row := knowledgeCleanupPureRow(t, x.a, "documents", nil)
	call.batch = &projectCleanupBatch{stage: "documents", rows: []projectCleanupRow{row}}
	call.observed = true
	original := f.UnknownResult(newID[f.TransactionAttempt](t), x.pair.state.projectCleanupCause(call))
	firstErr := x.pair.state.retainProjectCleanup(call, original)
	x.pair.state.returnProjectCleanup(call)
	// The prior batch is physically absent. Its Unknown still requires the
	// original read-only confirmation to return before another control starts.
	x.store.extraQuery = func(sql string, args ...any) postgres.Row {
		return publicationCheckpointRow(func(dest ...any) error {
			if !strings.Contains(sql, "FROM agenteam_knowledge.documents t") || len(args) != 2 || args[0] != x.a.String() || len(args[1].([]string)) != 1 || args[1].([]string)[0] != row.Key {
				return errors.New("confirmation did not retain original project and keys")
			}
			*dest[0].(*[]byte) = []byte("[]")
			return nil
		})
	}
	x.store.result = func(tx *knowledgeProjectPureTx, _ f.CommitResult) (f.CommitResult, bool) {
		return f.UnknownResult(newID[f.TransactionAttempt](t), tx.cause), false
	}
	_, err = participant.Cleanup(context.Background(), actor, cause, scope, nil)
	knowledgeProjectAssertUnknown(t, err, original)
	if !errors.Is(err, firstErr) {
		t.Fatal("cleanup replaced its first retained failure")
	}
	x.store.result = nil
	entered, release, joined := make(chan struct{}), make(chan struct{}), make(chan struct{})
	x.store.tail = func(tx *knowledgeProjectPureTx) {
		if tx.cause.Details().Owner == "knowledge-project-cleanup" {
			if tx.writes != 0 {
				t.Error("cleanup confirmation wrote")
			}
			close(entered)
			<-release
		}
	}
	type answer struct {
		report pc.CleanupReport
		err    error
	}
	result := make(chan answer, 1)
	go func() {
		defer close(joined)
		report, err := participant.Cleanup(context.Background(), actor, cause, scope, nil)
		result <- answer{report, err}
	}()
	defer func() {
		contentReaderRelease(release)
		contentReaderAwait(t, joined, "cleanup original confirmation did not return")
	}()
	contentReaderAwait(t, entered, "cleanup confirmation Store tail not reached")
	before, _, writes := x.store.counts()
	_, err = second.Cleanup(context.Background(), actor, cause, scope, nil)
	knowledgeProjectAssertFault(t, err, f.ResourceBusy)
	_, err = x.pair.RequestProjectStop(context.Background(), actor, cause, scope)
	knowledgeProjectAssertFault(t, err, f.ResourceBusy)
	after, _, afterWrites := x.store.counts()
	if before != after || writes != afterWrites {
		t.Fatal("second adapter bypassed original cleanup confirmation")
	}
	if _, err = x.pair.Service().GetDocument(context.Background(), x.actor, x.b, x.document); err != nil {
		t.Fatal("project cleanup retired unrelated B", err)
	}
	contentReaderRelease(release)
	contentReaderAwait(t, joined, "cleanup confirmation actual return missing")
	got := <-result
	if got.err != nil || got.report.Details().State != pc.CleanupPending {
		t.Fatal("observed last row removal was promoted to CleanupCompleted", got.err)
	}
	x.store.tail = nil
	if x.pair.state.cleanups[x.a] != nil {
		t.Fatal("returned confirmed cleanup control was retained")
	}
}

func TestKnowledgeProjectRetainedCleanupCancellationPreservesOriginal(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		name := "leaf_pending_without_error"
		if uncertain {
			name = "first_unknown"
		}
		t.Run(name, func(t *testing.T) {
			x := newKnowledgeProjectPureFixture(t)
			actor, cause, scope := x.stopIdentity(t, x.a)
			participant, err := NewProjectCleanupParticipant(x.pair, x.gate)
			if err != nil {
				t.Fatal(err)
			}
			call, err := x.pair.state.borrowProjectCleanup(context.Background(), actor, cause, scope, nil, x.gate)
			if err != nil {
				t.Fatal(err)
			}
			call.leafPending = true
			var original f.CommitResult
			if uncertain {
				original = f.UnknownResult(newID[f.TransactionAttempt](t), x.pair.state.projectCleanupCause(call))
				x.pair.state.retainProjectCleanup(call, original)
			}
			x.pair.state.returnProjectCleanup(call)
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = participant.Cleanup(cancelled, actor, cause, scope, nil)
			if uncertain {
				knowledgeProjectAssertUnknown(t, err, original)
			} else if !errors.Is(err, context.Canceled) {
				t.Fatal("retained error-free leaf returned nil instead of the original cancellation", err)
			}
			if x.pair.state.cleanups[x.a] != call || call.active || !call.leafPending {
				t.Fatal("cancelled retry consumed or activated the original retained control")
			}
			if transactions, queries, writes := x.store.counts(); transactions != 0 || queries != 0 || writes != 0 {
				t.Fatal("cancelled retained retry began another physical transaction")
			}
		})
	}
}
