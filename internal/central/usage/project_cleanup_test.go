package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// Independent schema/key expectations, not derived from the production stage
// selector. Bodies are controlled full-row JSON, not real invocation history.
var usageCleanupSpecs = []struct {
	table, key string
	limit      int
}{
	{"invocation_observations", "t.invocation_id::text || ':' || t.sequence::text", 100},
	{"invocations", "t.id::text", 32},
	{"execution_usage_summaries", "t.execution_id::text", 100},
	{"usage_project_calls", "t.id::text", 100},
}

type usageCleanupWireRow struct {
	Key    string          `json:"key"`
	Record json.RawMessage `json:"record"`
}
type usageCleanupFixture struct {
	v            *usageOwnerFixture
	participant  pc.ProjectLifecycleParticipant
	actor        i.Actor
	mu           sync.Mutex
	tables       map[string]map[i.ProjectID]map[string]json.RawMessage
	deletes      []string
	sizes        []int
	shortCount   bool
	readOverride func(string, []usageCleanupWireRow) []usageCleanupWireRow
}

func (v *usageCleanupFixture) rows(table string, p i.ProjectID) []usageCleanupWireRow {
	rows := []usageCleanupWireRow{}
	if table == "usage_project_calls" {
		v.v.store.mu.Lock()
		defer v.v.store.mu.Unlock()
		for _, row := range v.v.store.rows {
			if row.Project == p {
				raw, _ := json.Marshal(row)
				rows = append(rows, usageCleanupWireRow{row.ID.String(), raw})
			}
		}
	} else {
		v.mu.Lock()
		defer v.mu.Unlock()
		for key, raw := range v.tables[table][p] {
			rows = append(rows, usageCleanupWireRow{key, slices.Clone(raw)})
		}
	}
	slices.SortFunc(rows, func(a, b usageCleanupWireRow) int { return strings.Compare(a.Key, b.Key) })
	return rows
}
func (v *usageCleanupFixture) query(query string, args ...any) postgres.Row {
	if strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_model.") {
		clauses := []string{}
		for _, spec := range usageCleanupSpecs {
			clauses = append(clauses, "EXISTS(SELECT 1 FROM agenteam_model."+spec.table+" WHERE project_id=$1::uuid)")
		}
		if query != "SELECT "+strings.Join(clauses, ",") || len(args) != 1 {
			return usageErrorRow(errors.New("wrong cleanup existence scope/order"))
		}
		p, err := f.ParseID[i.Project](args[0].(string))
		if err != nil {
			return usageErrorRow(err)
		}
		return usageOwnerRow(func(out ...any) error {
			if len(out) != 4 {
				return errors.New("incomplete cleanup existence projection")
			}
			for n, spec := range usageCleanupSpecs {
				*out[n].(*bool) = len(v.rows(spec.table, p)) != 0
			}
			return nil
		})
	}
	if !strings.Contains(query, "jsonb_build_object('key',selected.key,'record',selected.record)") {
		return nil
	}
	if len(args) < 1 || len(args) > 2 {
		return usageErrorRow(errors.New("unexpected cleanup parameters"))
	}
	p, err := f.ParseID[i.Project](args[0].(string))
	if err != nil {
		return usageErrorRow(err)
	}
	for _, spec := range usageCleanupSpecs {
		if !strings.Contains(query, "FROM agenteam_model."+spec.table+" t WHERE") {
			continue
		}
		if !strings.Contains(query, "t.project_id=$1::uuid") || !strings.Contains(query, "SELECT ("+spec.key+") AS key,to_jsonb(t) AS record") || !strings.HasSuffix(query, fmt.Sprintf("ORDER BY (%s) COLLATE \"C\" LIMIT %d) selected", spec.key, spec.limit)) {
			return usageErrorRow(errors.New("unbounded or partial cleanup read"))
		}
		rows := v.rows(spec.table, p)
		if len(args) == 2 {
			keys, ok := args[1].([]string)
			if !ok || !strings.Contains(query, "AND ("+spec.key+")=ANY($2::text[])") {
				return usageErrorRow(errors.New("confirmation omitted exact keys"))
			}
			rows = slices.DeleteFunc(rows, func(row usageCleanupWireRow) bool { return !slices.Contains(keys, row.Key) })
		}
		if len(rows) > spec.limit {
			rows = rows[:spec.limit]
		}
		if v.readOverride != nil {
			rows = v.readOverride(spec.table, rows)
		}
		return usageJSONRow(rows)
	}
	return usageErrorRow(errors.New("cross-domain cleanup read"))
}
func (v *usageCleanupFixture) exec(query string, args ...any) (pgconn.CommandTag, error, bool) {
	if !strings.HasPrefix(query, "DELETE FROM ") {
		return pgconn.CommandTag{}, nil, false
	}
	for _, spec := range usageCleanupSpecs {
		if !strings.HasPrefix(query, "DELETE FROM agenteam_model."+spec.table+" t ") {
			continue
		}
		want := "DELETE FROM agenteam_model." + spec.table + " t USING jsonb_to_recordset($2::jsonb) AS selected(key text,record jsonb) WHERE t.project_id=$1::uuid AND (" + spec.key + ")=selected.key AND to_jsonb(t)=selected.record"
		if spec.table == "usage_project_calls" {
			want += " AND t.phase='business_closed'"
		}
		if query != want || len(args) != 2 {
			return pgconn.CommandTag{}, errors.New("delete omitted exact full row/key/project"), true
		}
		p, err := f.ParseID[i.Project](args[0].(string))
		if err != nil {
			return pgconn.CommandTag{}, err, true
		}
		var selected []usageCleanupWireRow
		if json.Unmarshal([]byte(args[1].(string)), &selected) != nil || len(selected) == 0 || len(selected) > spec.limit {
			return pgconn.CommandTag{}, errors.New("invalid bounded delete"), true
		}
		v.mu.Lock()
		v.deletes = append(v.deletes, spec.table)
		v.sizes = append(v.sizes, len(selected))
		short := v.shortCount
		v.mu.Unlock()
		if short {
			return pgconn.NewCommandTag("DELETE 0"), nil, true
		}
		current := v.rows(spec.table, p)
		for _, row := range selected {
			if !slices.ContainsFunc(current, func(have usageCleanupWireRow) bool {
				return row.Key == have.Key && bytes.Equal(row.Record, have.Record)
			}) {
				return pgconn.NewCommandTag("DELETE 0"), nil, true
			}
		}
		if spec.table == "usage_project_calls" {
			v.v.store.mu.Lock()
			for _, row := range selected {
				delete(v.v.store.rows, row.Key)
			}
			v.v.store.mu.Unlock()
		} else {
			v.mu.Lock()
			for _, row := range selected {
				delete(v.tables[spec.table][p], row.Key)
			}
			v.mu.Unlock()
		}
		v.v.store.mu.Lock()
		v.v.store.writes++
		v.v.store.mu.Unlock()
		return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", len(selected))), nil, true
	}
	return pgconn.CommandTag{}, errors.New("cross-domain cleanup delete"), true
}
func newUsageCleanup(t *testing.T) *usageCleanupFixture {
	t.Helper()
	base := newUsageOwner(t, nil)
	v := &usageCleanupFixture{v: base, tables: map[string]map[i.ProjectID]map[string]json.RawMessage{}}
	base.store.extraRow, base.store.extraExec = v.query, v.exec
	reg, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := i.InProject(base.p)
	v.actor, err = reg.Actor(base.cause.OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	v.participant, err = NewProjectCleanupParticipant(base.pair, base.gate)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (v *usageCleanupFixture) seed(t *testing.T, table string, p i.ProjectID, n int) {
	t.Helper()
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.tables[table] == nil {
		v.tables[table] = map[i.ProjectID]map[string]json.RawMessage{}
	}
	if v.tables[table][p] == nil {
		v.tables[table][p] = map[string]json.RawMessage{}
	}
	id := usageID[struct{}](t, n).String()
	key := id
	record := map[string]any{"project_id": p.String(), "body": "immutable original body"}
	switch table {
	case "invocation_observations":
		record["invocation_id"], record["sequence"] = id, 1
		key += ":1"
	case "execution_usage_summaries":
		record["execution_id"] = id
	default:
		record["id"] = id
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	v.tables[table][p][key] = raw
}
func (v *usageCleanupFixture) cleanup(ctx context.Context, cp *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	return v.participant.Cleanup(context.WithValue(ctx, usageOwnerLoanKey{}, v.v.gate), v.actor, v.v.cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.v.p}, cp)
}
func usageCleanupProgress(t *testing.T, v *usageCleanupFixture, r pc.CleanupReport, err error, state pc.CleanupState, stage string) *pc.CleanupCheckpoint {
	t.Helper()
	d := r.Details()
	if err != nil || d.State != state || d.Checkpoint == nil {
		t.Fatal("unexpected cleanup report", err, d.State)
	}
	p, e := uc.DecodeProjectCleanupCheckpoint(d.Checkpoint, v.v.cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.v.p})
	if e != nil || p.Stage != stage {
		t.Fatal("unexpected cleanup progress", e, p.Stage)
	}
	return d.Checkpoint
}

func TestUsageProjectCleanupBoundedFourStages(t *testing.T) {
	v := newUsageCleanup(t)
	for n := 0; n < 101; n++ {
		v.seed(t, "invocation_observations", v.v.p, 100+n)
	}
	for n := 0; n < 33; n++ {
		v.seed(t, "invocations", v.v.p, 300+n)
	}
	v.seed(t, "execution_usage_summaries", v.v.p, 400)
	for _, spec := range usageCleanupSpecs[:3] {
		v.seed(t, spec.table, v.v.b, 900)
	}
	row := projectCallRecord{ID: usageID[projectCallID](t, 401), Project: v.v.p, Process: v.v.proof.process, Instance: v.v.pair.state.instance, Lane: projectCallSummary, Phase: "business_closed", Registered: "2026-10-13T00:00:00Z", Closed: "2026-10-13T00:00:01Z"}
	v.v.store.rows[row.ID.String()] = row
	var cp *pc.CleanupCheckpoint
	for _, stage := range []string{"observations", "invocations", "invocations", "summaries", "calls", "completed"} {
		r, err := v.cleanup(context.Background(), cp)
		state := pc.CleanupPending
		if stage == "completed" {
			state = pc.CleanupCompleted
		}
		cp = usageCleanupProgress(t, v, r, err, state, stage)
	}
	if !slices.Equal(v.deletes, []string{"invocation_observations", "invocation_observations", "invocations", "invocations", "execution_usage_summaries", "usage_project_calls"}) || !slices.Equal(v.sizes, []int{100, 1, 32, 1, 1, 1}) {
		t.Fatal("cleanup changed FK order or exact batch bounds", v.deletes, v.sizes)
	}
	for _, spec := range usageCleanupSpecs[:3] {
		if len(v.rows(spec.table, v.v.b)) != 1 {
			t.Fatal("cleanup touched Project B")
		}
	}
}

func TestUsageProjectCleanupExactRowsAndCount(t *testing.T) {
	for _, mode := range []string{"short-delete", "foreign-project", "key-mismatch", "running-call"} {
		t.Run(mode, func(t *testing.T) {
			v := newUsageCleanup(t)
			v.seed(t, "invocation_observations", v.v.p, 100)
			if mode == "short-delete" {
				v.shortCount = true
			} else {
				v.readOverride = func(table string, rows []usageCleanupWireRow) []usageCleanupWireRow {
					if table != "invocation_observations" || len(rows) == 0 {
						return rows
					}
					var record map[string]any
					_ = json.Unmarshal(rows[0].Record, &record)
					switch mode {
					case "foreign-project":
						record["project_id"] = v.v.b.String()
					case "key-mismatch":
						record["sequence"] = 2
					}
					rows[0].Record, _ = json.Marshal(record)
					return rows
				}
			}
			if mode == "running-call" {
				v.readOverride = nil
				v.v.store.rows["running"] = projectCallRecord{ID: usageID[projectCallID](t, 101), Project: v.v.p, Process: v.v.proof.process, Instance: v.v.pair.state.instance, Lane: projectCallList, Phase: "running", Registered: "2026-10-13T00:00:00Z"}
			}
			r, err := v.cleanup(context.Background(), nil)
			if mode == "running-call" {
				if err != nil || r.Details().State != pc.CleanupPending {
					t.Fatal("running owner was not Pending", err)
				}
			} else if err == nil {
				t.Fatal("invalid exact cleanup accepted")
			}
			if len(v.rows("invocation_observations", v.v.p)) != 1 {
				t.Fatal("invalid cleanup removed original row")
			}
		})
	}
}

func TestUsageProjectCleanupUnknownReadOnlyOriginalReturn(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(fmt.Sprint("physical-applied=", applied), func(t *testing.T) {
			v := newUsageCleanup(t)
			v.seed(t, "invocation_observations", v.v.p, 100)
			before := v.rows("invocation_observations", v.v.p)[0]
			var original f.CommitResult
			v.v.store.after = func(_ context.Context, cause f.TransactionCause, n int, result f.CommitResult) f.CommitResult {
				if n != 2 {
					return result
				}
				if result.State() != f.Committed {
					t.Error("original cleanup callback rejected", result.Fault())
				}
				if !applied {
					v.mu.Lock()
					v.tables["invocation_observations"][v.v.p][before.Key] = slices.Clone(before.Record)
					v.mu.Unlock()
				}
				original = f.UnknownResult(usageID[f.TransactionAttempt](t, 110), cause)
				return original
			}
			_, err := v.cleanup(context.Background(), nil)
			usageUnknown(t, err, original)
			call := v.v.pair.state.cleanups[v.v.p]
			if call == nil || call.active || call.batch == nil || call.unknown == nil || call.unknown.AttemptID() != original.AttemptID() {
				t.Fatal("physical Unknown lost original batch and call")
			}
			v.v.store.after = nil
			if err = v.v.read(context.Background(), projectCallList, v.v.b); !errors.Is(err, v.v.store.readErr) {
				t.Fatal("retained cleanup blocked B", err)
			}
			if err = v.v.read(context.Background(), projectCallList, v.v.p); err == nil || errors.Is(err, v.v.store.readErr) {
				t.Fatal("same-P late call reached business SQL", err)
			}
			if _, err = v.v.stop(context.Background(), true); err == nil {
				t.Fatal("late Stop borrowed retained cleanup")
			}
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			free := func() { once.Do(func() { close(release) }) }
			v.v.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				if !reflect.DeepEqual(cause.Details(), original.Cause().Details()) {
					t.Error("cleanup confirmation changed original cause")
				}
				if result.State() != f.Committed {
					t.Error("exact physical before/absent image was not observed", result.Fault())
				}
				close(entered)
				<-release
				return result
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { defer close(returned); _, err = v.cleanup(ctx, nil) }()
			t.Cleanup(func() { cancel(); free(); usageAwait(t, returned) })
			usageAwait(t, entered)
			cancel()
			if call.unknown == nil || !call.active || len(v.deletes) != 1 {
				t.Fatal("cleanup observation retired or wrote before Store returned")
			}
			other, e := NewProjectCleanupParticipant(v.v.pair, v.v.gate)
			if e != nil {
				t.Fatal(e)
			}
			_, e = other.Cleanup(context.WithValue(context.Background(), usageOwnerLoanKey{}, v.v.gate), v.actor, v.v.cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.v.p}, nil)
			expectFault(t, e, f.ResourceBusy)
			select {
			case <-returned:
				t.Fatal("cancellation detached original confirmation Store")
			default:
			}
			free()
			usageAwait(t, returned)
			usageUnknown(t, err, original)
			if call.unknown == nil || call.unknown.AttemptID() != original.AttemptID() {
				t.Fatal("cancelled observation cleared first raw Unknown")
			}
			v.v.store.after = nil
			report, err := v.cleanup(context.Background(), nil)
			cp := usageCleanupProgress(t, v, report, err, pc.CleanupPending, "observations")
			if len(v.deletes) != 1 || v.v.pair.state.cleanups[v.v.p] != nil {
				t.Fatal("confirmed readonly call wrote or retained its released slot")
			}
			report, err = v.cleanup(context.Background(), cp)
			usageCleanupProgress(t, v, report, err, pc.CleanupCompleted, "completed")
			want := 2
			if applied {
				want = 1
			}
			if len(v.deletes) != want {
				t.Fatal("next batch did not wait for explicit call", v.deletes)
			}
		})
	}
}

func TestUsageProjectCleanupUnknownRejectsPartialOrDriftedRows(t *testing.T) {
	for _, mode := range []string{"partial", "changed-body"} {
		t.Run(mode, func(t *testing.T) {
			v := newUsageCleanup(t)
			v.seed(t, "invocation_observations", v.v.p, 100)
			v.seed(t, "invocation_observations", v.v.p, 101)
			before := v.rows("invocation_observations", v.v.p)
			var original f.CommitResult
			v.v.store.after = func(_ context.Context, cause f.TransactionCause, n int, result f.CommitResult) f.CommitResult {
				if n != 2 {
					return result
				}
				v.mu.Lock()
				for _, row := range before {
					v.tables["invocation_observations"][v.v.p][row.Key] = slices.Clone(row.Record)
				}
				if mode == "partial" {
					delete(v.tables["invocation_observations"][v.v.p], before[0].Key)
				} else {
					v.tables["invocation_observations"][v.v.p][before[0].Key] = bytes.Replace(before[0].Record, []byte("original body"), []byte("changed body"), 1)
				}
				v.mu.Unlock()
				original = f.UnknownResult(usageID[f.TransactionAttempt](t, 111), cause)
				return original
			}
			_, err := v.cleanup(context.Background(), nil)
			usageUnknown(t, err, original)
			v.v.store.after = nil
			_, err = v.cleanup(context.Background(), nil)
			usageUnknown(t, err, original)
			if len(v.deletes) != 1 || v.v.pair.state.cleanups[v.v.p] == nil {
				t.Fatal("partial/drifted full-row image cleared Unknown or mutated")
			}
		})
	}
}

func TestUsageProjectCleanupWaitsForEarlierStopTail(t *testing.T) {
	v := newUsageCleanup(t)
	v.seed(t, "invocation_observations", v.v.p, 100)
	entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	v.v.store.after = func(_ context.Context, cause f.TransactionCause, n int, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "usage-project-stop" && n == 1 {
			close(entered)
			<-release
		}
		return result
	}
	var stopErr error
	go func() { defer close(returned); _, stopErr = v.v.stop(context.Background(), false) }()
	t.Cleanup(func() { free(); usageAwait(t, returned) })
	usageAwait(t, entered)
	r, err := v.cleanup(context.Background(), nil)
	usageCleanupProgress(t, v, r, err, pc.CleanupPending, "observations")
	if len(v.deletes) != 0 {
		t.Fatal("cleanup overtook original Stop Store return")
	}
	free()
	usageAwait(t, returned)
	if stopErr != nil {
		t.Fatal(stopErr)
	}
	v.v.store.after = nil
	r, err = v.cleanup(context.Background(), nil)
	usageCleanupProgress(t, v, r, err, pc.CleanupCompleted, "completed")
	if len(v.deletes) != 1 {
		t.Fatal("returned Stop did not release cleanup")
	}
}
