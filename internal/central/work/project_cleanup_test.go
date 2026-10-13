package work

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These controls execute the original WithReaders graph, live Tx checks and
// original Store returns. The additional JSON projections exercise the fixed
// cleanup SQL protocol, not SQL rollback, complete business histories, real
// Project grants or foreign-process death. Only GetTask and its call ledger are
// produced by real public Work methods here; full business histories need PG.
type workCleanupSpec struct {
	table, key string
	limit      int
}

// Independent schema/FK expectations: do not derive the adapter or expected
// order from projectCleanupStages/projectCleanupTable under test.
var workCleanupSpecs = []workCleanupSpec{
	{"task_events", "t.id::text", 24},
	{"task_blockers", "t.id::text", 100},
	{"task_launch_failures", "t.id::text", 1},
	{"task_busy_compensations", "t.id::text", 1},
	{"task_scheduler_claims", "t.id::text", 1},
	{"task_scheduler_relaunches", "t.id::text", 1},
	{"task_blocker_commands", "t.id::text", 1},
	{"task_transition_commands", "t.id::text", 1},
	{"task_commands", "t.id::text", 1},
	{"sprint_start_commands", "t.id::text", 1},
	{"structure_commands", "t.id::text", 1},
	{"tasks", "t.id::text", 16},
	{"task_order_groups", "t.sprint_id::text || ':' || t.state || ':' || t.priority", 100},
	{"task_query_generations", "t.project_id::text", 100},
	{"sprints", "t.id::text", 32},
	{"sprint_order_groups", "t.milestone_id::text", 100},
	{"milestones", "t.id::text", 32},
	{"milestone_order_groups", "t.project_id::text", 100},
	{"project_calls", "t.id::text", 100},
}

type workCleanupRow struct {
	Key    string          `json:"key"`
	Record json.RawMessage `json:"record"`
}
type workCleanupStore struct {
	*readerOwnerStore
	dataMu       sync.Mutex
	tables       map[string]map[i.ProjectID]map[string]json.RawMessage
	deletes      []string
	sizes        []int
	parameters   []string
	deleteErr    error
	shortCount   bool
	holdDelete   func(context.Context)
	readOverride func(string, []workCleanupRow) []byte
}

func (s *workCleanupStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.callOwnerStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func workCleanupJSON(value any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(b.Bytes()), nil
}
func (s *workCleanupStore) rows(table string, p i.ProjectID) []workCleanupRow {
	rows := []workCleanupRow{}
	if table == "project_calls" {
		s.callOwnerStore.mu.Lock()
		defer s.callOwnerStore.mu.Unlock()
		for _, r := range s.callOwnerStore.rows {
			if r.Project == p {
				raw, _ := workCleanupJSON(r)
				rows = append(rows, workCleanupRow{r.ID.String(), raw})
			}
		}
	} else {
		s.dataMu.Lock()
		defer s.dataMu.Unlock()
		for key, raw := range s.tables[table][p] {
			rows = append(rows, workCleanupRow{key, slices.Clone(raw)})
		}
	}
	slices.SortFunc(rows, func(a, b workCleanupRow) int { return strings.Compare(a.Key, b.Key) })
	return rows
}

type workCleanupExistsRow []bool

func (r workCleanupExistsRow) Scan(out ...any) error {
	if len(out) != len(r) {
		return errors.New("missing Work stage existence projection")
	}
	for n := range r {
		p, ok := out[n].(*bool)
		if !ok {
			return errors.New("wrong Work stage existence destination")
		}
		*p = r[n]
	}
	return nil
}
func (s *workCleanupStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_work.") {
		clauses := []string{}
		for _, spec := range workCleanupSpecs {
			clauses = append(clauses, "EXISTS(SELECT 1 FROM agenteam_work."+spec.table+" WHERE project_id=$1::uuid)")
		}
		if len(args) != 1 || query != "SELECT "+strings.Join(clauses, ",") {
			return callOwnerRow{err: errors.New("incorrect fixed existence order/project binding")}
		}
		p, err := f.ParseID[i.Project](args[0].(string))
		if err != nil {
			return callOwnerRow{err: err}
		}
		values := workCleanupExistsRow{}
		for _, spec := range workCleanupSpecs {
			values = append(values, len(s.rows(spec.table, p)) != 0)
		}
		return values
	}
	if !strings.Contains(query, "jsonb_build_object('key',selected.key,'record',selected.record)") {
		return s.readerOwnerStore.QueryRow(ctx, query, args...)
	}
	if len(args) < 1 || len(args) > 2 {
		return callOwnerRow{err: errors.New("incorrect cleanup read arguments")}
	}
	p, err := f.ParseID[i.Project](args[0].(string))
	if err != nil {
		return callOwnerRow{err: err}
	}
	for _, spec := range workCleanupSpecs {
		if !strings.Contains(query, "FROM agenteam_work."+spec.table+" t WHERE") {
			continue
		}
		if !strings.Contains(query, "t.project_id=$1::uuid") || !strings.Contains(query, "SELECT ("+spec.key+") AS key,to_jsonb(t) AS record") || !strings.HasSuffix(query, fmt.Sprintf("ORDER BY (%s) COLLATE \"C\" LIMIT %d) selected", spec.key, spec.limit)) {
			return callOwnerRow{err: errors.New("unbounded/wrong-key Work read")}
		}
		rows := s.rows(spec.table, p)
		if len(args) == 2 {
			keys, ok := args[1].([]string)
			if !ok || !strings.Contains(query, "AND ("+spec.key+")=ANY($2::text[])") {
				return callOwnerRow{err: errors.New("confirmation lost original key filter")}
			}
			rows = slices.DeleteFunc(rows, func(r workCleanupRow) bool { return !slices.Contains(keys, r.Key) })
		}
		if len(rows) > spec.limit {
			rows = rows[:spec.limit]
		}
		if s.readOverride != nil {
			return callOwnerRow{raw: s.readOverride(spec.table, rows)}
		}
		raw, err := workCleanupJSON(rows)
		return callOwnerRow{raw: raw, err: err}
	}
	return callOwnerRow{err: errors.New("cross-domain cleanup read")}
}
func (s *workCleanupStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(query, "DELETE FROM agenteam_work.") {
		return s.callOwnerStore.Exec(ctx, query, args...)
	}
	for _, spec := range workCleanupSpecs {
		if !strings.HasPrefix(query, "DELETE FROM agenteam_work."+spec.table+" t ") {
			continue
		}
		expected := "DELETE FROM agenteam_work." + spec.table + " t USING jsonb_to_recordset($2::jsonb) AS selected(key text,record jsonb) WHERE t.project_id=$1::uuid AND (" + spec.key + ")=selected.key AND to_jsonb(t)=selected.record"
		if spec.table == "project_calls" {
			expected += " AND t.phase='business_closed'"
		}
		if query != expected || len(args) != 2 {
			return pgconn.CommandTag{}, errors.New("cleanup lost exact full-row/key/project predicate")
		}
		p, err := f.ParseID[i.Project](args[0].(string))
		if err != nil {
			return pgconn.CommandTag{}, err
		}
		parameter := args[1].(string)
		var selected []workCleanupRow
		if json.Unmarshal([]byte(parameter), &selected) != nil || len(selected) == 0 || len(selected) > spec.limit || len(parameter) > 8<<20 {
			return pgconn.CommandTag{}, errors.New("invalid Work cleanup batch")
		}
		s.dataMu.Lock()
		s.deletes = append(s.deletes, spec.table)
		s.sizes = append(s.sizes, len(selected))
		s.parameters = append(s.parameters, parameter)
		failure, short, hold := s.deleteErr, s.shortCount, s.holdDelete
		s.dataMu.Unlock()
		if hold != nil {
			hold(ctx)
		}
		if failure != nil {
			return pgconn.CommandTag{}, failure
		}
		if short {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
		current := s.rows(spec.table, p)
		for _, want := range selected {
			found := slices.ContainsFunc(current, func(have workCleanupRow) bool {
				return want.Key == have.Key && bytes.Equal(want.Record, have.Record)
			})
			if !found {
				return pgconn.NewCommandTag("DELETE 0"), nil
			}
		}
		if spec.table == "project_calls" {
			s.callOwnerStore.mu.Lock()
			for _, row := range selected {
				if s.callOwnerStore.rows[row.Key].Phase != "business_closed" {
					s.callOwnerStore.mu.Unlock()
					return pgconn.NewCommandTag("DELETE 0"), nil
				}
			}
			for _, row := range selected {
				delete(s.callOwnerStore.rows, row.Key)
			}
			s.callOwnerStore.mu.Unlock()
		} else {
			s.dataMu.Lock()
			for _, row := range selected {
				delete(s.tables[spec.table][p], row.Key)
			}
			s.dataMu.Unlock()
		}
		return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", len(selected))), nil
	}
	return pgconn.CommandTag{}, errors.New("cross-domain cleanup DELETE")
}
func (s *workCleanupStore) writes() int {
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	return len(s.deletes)
}
func (s *workCleanupStore) put(table string, p i.ProjectID, row workCleanupRow) {
	s.dataMu.Lock()
	defer s.dataMu.Unlock()
	if s.tables[table] == nil {
		s.tables[table] = map[i.ProjectID]map[string]json.RawMessage{}
	}
	if s.tables[table][p] == nil {
		s.tables[table][p] = map[string]json.RawMessage{}
	}
	s.tables[table][p][row.Key] = slices.Clone(row.Record)
}
func (s *workCleanupStore) seed(t *testing.T, table string, p i.ProjectID, n int) {
	t.Helper()
	for k := range n {
		id := pureID[struct{}](t, 4000+k).String()
		key := id
		record := map[string]any{"project_id": p.String(), "id": id}
		switch table {
		case "task_order_groups":
			record = map[string]any{"project_id": p.String(), "milestone_id": pureID[struct{}](t, 3900).String(), "sprint_id": id, "state": "todo", "priority": "medium", "order_generation": 1}
			key = id + ":todo:medium"
		case "sprint_order_groups":
			record = map[string]any{"project_id": p.String(), "milestone_id": id, "order_generation": 1}
		case "task_query_generations":
			record, key = map[string]any{"project_id": p.String(), "query_generation": 1}, p.String()
		case "milestone_order_groups":
			record, key = map[string]any{"project_id": p.String(), "order_generation": 1}, p.String()
		}
		raw, err := workCleanupJSON(record)
		if err != nil {
			t.Fatal(err)
		}
		s.put(table, p, workCleanupRow{key, raw})
	}
}

type workCleanupLoanKey struct{}
type workCleanupAuthority struct {
	store *workCleanupStore
	actor i.Actor
	cause pc.LifecycleCause
	scope pc.ScopeRef
}

func (a *workCleanupAuthority) RequireProjectCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx.Value(workCleanupLoanKey{}) != a || !actor.Equal(a.actor) || cause != a.cause || !scope.Equal(a.scope) {
		return fault(f.Forbidden)
	}
	x, err := a.store.InTx(tx)
	if err != nil || x != a.store {
		return fault(f.Forbidden)
	}
	return a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(scope.ProjectID, f.Exclusive), taskScheduleLock(scope.ProjectID, f.Exclusive)})
}

type workCleanupFixture struct {
	base        *readerOwnerFixture
	pair        *ProjectCallOwnership
	store       *workCleanupStore
	authority   *workCleanupAuthority
	participant pc.ProjectLifecycleParticipant
	ctx         context.Context
}

func newWorkCleanup(t *testing.T) *workCleanupFixture {
	t.Helper()
	base := newReaderOwnerFixture(t)
	store := &workCleanupStore{readerOwnerStore: base.db, tables: map[string]map[i.ProjectID]map[string]json.RawMessage{}}
	authority, err := NewAuthority(store, base.projects)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(store, authority, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	deps := readerPairDependencies(base.pair)
	deps.Structure.Authority, deps.Tasks.Authority, deps.Transitions.Authority, deps.Blockers.Authority, deps.Sprints.Authority = authority, authority, authority, authority, authority
	deps.Tasks.Structure, deps.Transitions.Structure, deps.Blockers.Structure = reader, reader, reader
	pair, err := newProjectCallOwnershipWithReaders(store, deps, pureKeys(t), base.proof, base.pair.state.process, &callOwnerAuthority{store: base.store})
	if err != nil {
		t.Fatal(err)
	}
	registration, err := i.RegisterService(i.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	actorScope, err := i.InProject(base.p)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(base.cause.OperationID.String(), actorScope)
	if err != nil {
		t.Fatal(err)
	}
	gate := &workCleanupAuthority{store, actor, base.cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: base.p}}
	participant, err := NewProjectCleanupParticipant(pair, gate)
	if err != nil {
		t.Fatal(err)
	}
	v := &workCleanupFixture{base, pair, store, gate, participant, context.WithValue(context.Background(), workCleanupLoanKey{}, gate)}
	t.Cleanup(func() {
		base.store.after = nil
		// Recover a retained cleanup only by its original authorized read, after
		// every test barrier/goroutine has actually returned (later cleanups run first).
		pair.state.mu.Lock()
		pending := pair.state.cleanups[base.p]
		pair.state.mu.Unlock()
		if pending != nil && !pending.active {
			_, _ = v.cleanup(v.ctx, nil)
		}
		pair.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := pair.Drain(ctx); err != nil {
			t.Error("cleanup original owner did not drain", err)
		}
	})
	return v
}
func (v *workCleanupFixture) cleanup(ctx context.Context, cp *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	return v.participant.Cleanup(ctx, v.authority.actor, v.base.cause, v.authority.scope, cp)
}
func (v *workCleanupFixture) read(ctx context.Context, p i.ProjectID) error {
	want := v.base.db.tasks[p.String()]
	got, err := v.pair.TaskReader().GetTask(ctx, v.base.actor, p, want.ID)
	if err == nil && !sameValue(got, want) {
		return errors.New("public GetTask changed its result")
	}
	return err
}
func (v *workCleanupFixture) stop(ctx context.Context, p i.ProjectID, request bool) (pc.StopReport, error) {
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: p}
	if request {
		return v.pair.RequestProjectStop(ctx, v.base.actor, v.base.cause, scope)
	}
	return v.pair.InspectProjectStop(ctx, v.base.actor, v.base.cause, scope)
}
func requireWorkCleanup(t *testing.T, v *workCleanupFixture, report pc.CleanupReport, err error, state pc.CleanupState, stage string) *pc.CleanupCheckpoint {
	t.Helper()
	if err != nil || !report.Matches(pc.WorkParticipant, v.base.cause, v.authority.scope) || report.Details().State != state {
		t.Fatal("unexpected cleanup result", err, report.Details().State)
	}
	cp := report.Details().Checkpoint
	if cp == nil {
		t.Fatal("missing cleanup checkpoint")
	}
	var data struct {
		Project, Operation, Version, Stage string
	}
	var raw map[string]string
	if json.Unmarshal(cp.Bytes(), &raw) != nil {
		t.Fatal("invalid checkpoint bytes")
	}
	data.Project, data.Operation, data.Version, data.Stage = raw["project_id"], raw["operation_id"], raw["project_version"], raw["stage"]
	if cp.Schema() != 1 || !cp.Matches(pc.WorkParticipant, v.base.cause, v.authority.scope) || data.Project != v.base.p.String() || data.Operation != v.base.cause.OperationID.String() || data.Version != v.base.cause.ProjectVersion.String() || data.Stage != stage {
		t.Fatal("cleanup checkpoint drift", string(cp.Bytes()))
	}
	return cp
}

func TestWorkProjectCleanupAuthorityAndOwnedReaders(t *testing.T) {
	for _, name := range []string{"nil-pair", "nil-authority", "stop-family", "human", "archive", "cancelled", "legacy-readers", "old-instance-checkpoint"} {
		t.Run(name, func(t *testing.T) {
			v := newWorkCleanup(t)
			v.store.seed(t, "task_events", v.base.p, 1)
			var report pc.CleanupReport
			var err error
			switch name {
			case "nil-pair":
				_, err = NewProjectCleanupParticipant(nil, v.authority)
			case "nil-authority":
				var gate *workCleanupAuthority
				_, err = NewProjectCleanupParticipant(v.pair, gate)
			case "stop-family":
				ctx := context.WithValue(context.Background(), callOwnerLoanKey{}, v.pair.state.authority)
				report, err = v.cleanup(ctx, nil)
			case "human":
				report, err = v.participant.Cleanup(v.ctx, v.base.actor, v.base.cause, v.authority.scope, nil)
			case "archive":
				cause := v.base.cause
				cause.Action = pc.Archive
				report, err = v.participant.Cleanup(v.ctx, v.authority.actor, cause, v.authority.scope, nil)
			case "cancelled":
				ctx, cancel := context.WithCancel(v.ctx)
				cancel()
				report, err = v.cleanup(ctx, nil)
			case "legacy-readers":
				legacy, e := newProjectCallOwnership(v.store, readerPairDependencies(v.pair), v.base.proof, v.pair.state.process, v.pair.state.authority)
				if e != nil {
					t.Fatal(e)
				}
				p, e := NewProjectCleanupParticipant(legacy, v.authority)
				if e != nil {
					t.Fatal(e)
				}
				report, err = p.Cleanup(v.ctx, v.authority.actor, v.base.cause, v.authority.scope, nil)
				requireWorkCleanup(t, v, report, err, pc.CleanupPending, "task_events")
				legacy.Stop()
				if !legacy.Joined() {
					t.Fatal("legacy Pending left an active cleanup")
				}
			case "old-instance-checkpoint":
				prior := wc.ProjectCleanupProgress{ProjectID: v.base.p, OperationID: v.base.cause.OperationID, ProjectVersion: v.base.cause.ProjectVersion, ProcessID: v.pair.state.process, InstanceID: pureID[projectCallInstance](t, 99).String(), Stage: "project_calls"}
				cp, e := wc.NewProjectCleanupCheckpoint(prior, v.base.cause, v.authority.scope)
				if e != nil {
					t.Fatal(e)
				}
				report, err = v.cleanup(v.ctx, &cp)
				next := requireWorkCleanup(t, v, report, err, pc.CleanupPending, "task_events")
				got, e := wc.DecodeProjectCleanupCheckpoint(next, v.base.cause, v.authority.scope)
				if e != nil || got.InstanceID != prior.InstanceID || got.ProcessID != prior.ProcessID {
					t.Fatal("Pending laundered the previous owner identity", e)
				}
			}
			if name != "legacy-readers" && name != "old-instance-checkpoint" && err == nil {
				t.Fatal("unbound/incorrect authority was accepted", report)
			}
			if v.store.writes() != 0 || len(v.store.rows("task_events", v.base.p)) != 1 {
				t.Fatal("rejected or unconfirmed call sent a DELETE")
			}
		})
	}
}

func TestWorkProjectCleanupFixedOrderAndBounds(t *testing.T) {
	v := newWorkCleanup(t)
	// Actual public read creates the final same-pair closed ledger row.
	if err := v.read(context.Background(), v.base.p); err != nil {
		t.Fatal(err)
	}
	if err := v.read(context.Background(), v.base.b); err != nil {
		t.Fatal(err)
	}
	wantOrder, wantSizes := []string{}, []int{}
	for _, spec := range workCleanupSpecs[:18] {
		n := 1
		// Exercise each different cap, including a large-record table's 1+1.
		switch spec.table {
		case "task_events", "task_blockers", "task_commands", "tasks", "sprints":
			n = spec.limit + 1
		}
		v.store.seed(t, spec.table, v.base.p, n)
		v.store.seed(t, spec.table, v.base.b, 1)
		wantOrder = append(wantOrder, spec.table)
		wantSizes = append(wantSizes, min(n, spec.limit))
		if n > spec.limit {
			wantOrder = append(wantOrder, spec.table)
			wantSizes = append(wantSizes, n-spec.limit)
		}
	}
	wantOrder, wantSizes = append(wantOrder, "project_calls"), append(wantSizes, 1)
	var cp *pc.CleanupCheckpoint
	for n := range wantOrder {
		report, err := v.cleanup(v.ctx, cp)
		stage, state := "completed", pc.CleanupCompleted
		if n+1 < len(wantOrder) {
			stage, state = wantOrder[n+1], pc.CleanupPending
		}
		cp = requireWorkCleanup(t, v, report, err, state, stage)
		if v.store.writes() != n+1 {
			t.Fatal("one call advanced more than one fixed batch")
		}
		for _, spec := range workCleanupSpecs {
			if len(v.store.rows(spec.table, v.base.b)) != 1 {
				t.Fatal("cleanup changed B", spec.table)
			}
		}
	}
	if !slices.Equal(v.store.deletes, wantOrder) || !slices.Equal(v.store.sizes, wantSizes) {
		t.Fatal("fixed FK order or bounded prefix changed", v.store.deletes, v.store.sizes)
	}
}

func TestWorkProjectCleanupRowsAndByteLimit(t *testing.T) {
	for _, name := range []string{"wrong-project", "duplicate-key", "unordered", "over-limit", "wrong-composite-key", "invalid-state", "invalid-priority", "running-call", "oversized-read"} {
		t.Run(name, func(t *testing.T) {
			v := newWorkCleanup(t)
			stage := "task_order_groups"
			v.store.seed(t, stage, v.base.p, 2)
			rows := v.store.rows(stage, v.base.p)
			switch name {
			case "wrong-project":
				rows[0].Record = bytes.ReplaceAll(rows[0].Record, []byte(v.base.p.String()), []byte(v.base.b.String()))
			case "duplicate-key":
				rows[1] = rows[0]
			case "unordered":
				rows[0], rows[1] = rows[1], rows[0]
			case "over-limit":
				rows = make([]workCleanupRow, 101)
			case "wrong-composite-key":
				rows[0].Key = strings.Split(rows[0].Key, ":")[0]
			case "invalid-state":
				rows[0].Record = bytes.ReplaceAll(rows[0].Record, []byte(`"todo"`), []byte(`"invented"`))
				rows[0].Key = strings.Replace(rows[0].Key, ":todo:", ":invented:", 1)
			case "invalid-priority":
				rows[0].Record = bytes.ReplaceAll(rows[0].Record, []byte(`"medium"`), []byte(`"urgent"`))
				rows[0].Key = strings.Replace(rows[0].Key, ":medium", ":urgent", 1)
			case "running-call":
				stage = "project_calls"
				raw, _ := workCleanupJSON(map[string]string{"id": rows[0].Key[:36], "project_id": v.base.p.String(), "phase": "running"})
				rows = []workCleanupRow{{rows[0].Key[:36], raw}}
			}
			v.store.readOverride = func(string, []workCleanupRow) []byte {
				if name == "oversized-read" {
					return bytes.Repeat([]byte(" "), (8<<20)+1)
				}
				raw, _ := workCleanupJSON(rows)
				return raw
			}
			_, err := readProjectCleanupRows(context.Background(), v.store, v.base.p, stage, nil)
			if err == nil || v.store.writes() != 0 {
				t.Fatal("malformed/oversized projection was accepted", name)
			}
		})
	}
	t.Run("large-html-plan", func(t *testing.T) {
		v := newWorkCleanup(t)
		key := pureID[struct{}](t, 4400).String()
		// A valid JSON plan just under the task command's 4 MiB plan budget.
		// Other command semantics are deliberately opaque to this repository test.
		plan := map[string]string{"text": strings.Repeat("<>&", ((4<<20)-32)/3)}
		raw, err := workCleanupJSON(map[string]any{"id": key, "project_id": v.base.p.String(), "plan": plan})
		if err != nil {
			t.Fatal(err)
		}
		v.store.put("task_commands", v.base.p, workCleanupRow{key, raw})
		rows, err := readProjectCleanupRows(context.Background(), v.store, v.base.p, "task_commands", nil)
		if err != nil || len(rows) != 1 {
			t.Fatal("bounded large row was unreadable", err)
		}
		if err = deleteProjectCleanupBatch(context.Background(), v.store, v.base.p, &projectCleanupBatch{stage: "task_commands", rows: rows}); err != nil {
			t.Fatal("HTML escaping expanded the original bounded plan", err)
		}
		var sent []workCleanupRow
		if len(v.store.parameters) != 1 || len(v.store.parameters[0]) > 8<<20 || json.Unmarshal([]byte(v.store.parameters[0]), &sent) != nil || len(sent) != 1 || !bytes.Equal(sent[0].Record, raw) {
			t.Fatal("large plan's full original representation changed")
		}
	})
}

func TestWorkProjectCleanupDeleteFailurePrefix(t *testing.T) {
	for _, mode := range []string{"error", "short-count"} {
		t.Run(mode, func(t *testing.T) {
			// Each failure point must preserve the fixed successful prefix and
			// cannot issue another table's DELETE or publish progress.
			for failed := range 19 {
				v := newWorkCleanup(t)
				if err := v.read(context.Background(), v.base.p); err != nil {
					t.Fatal(err)
				}
				for _, spec := range workCleanupSpecs[:18] {
					v.store.seed(t, spec.table, v.base.p, 1)
				}
				var cp *pc.CleanupCheckpoint
				for n := 0; n < failed; n++ {
					r, err := v.cleanup(v.ctx, cp)
					cp = requireWorkCleanup(t, v, r, err, pc.CleanupPending, workCleanupSpecs[n+1].table)
				}
				original := errors.New("original Work DELETE failed")
				if mode == "error" {
					v.store.deleteErr = original
				} else {
					v.store.shortCount = true
				}
				r, err := v.cleanup(v.ctx, cp)
				if err == nil || r.Details().Checkpoint != nil {
					t.Fatal("failed DELETE published progress", failed, err)
				}
				if mode == "error" && !errors.Is(err, original) {
					t.Fatal("lost original DELETE error", err)
				}
				if mode == "short-count" {
					pureCode(t, err, f.VersionConflict)
				}
				want := make([]string, failed+1)
				for n := range want {
					want[n] = workCleanupSpecs[n].table
				}
				if !slices.Equal(v.store.deletes, want) {
					t.Fatal("DELETE continued after failure", failed, v.store.deletes)
				}
				for _, spec := range workCleanupSpecs[failed:] {
					if len(v.store.rows(spec.table, v.base.p)) != 1 {
						t.Fatal("failed/later preimage changed", spec.table)
					}
				}
			}
		})
	}
}

type workCleanupAnswer struct {
	report pc.CleanupReport
	err    error
}

func waitWorkCleanup(t *testing.T, done <-chan workCleanupAnswer) workCleanupAnswer {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-time.After(5 * time.Second):
		t.Fatal("original cleanup Store call did not return")
		return workCleanupAnswer{}
	}
}

func TestWorkProjectCleanupOriginalReaderAndStopReturns(t *testing.T) {
	for _, stage := range []string{"work.task.read", "work-call-return", "work-project-stop"} {
		t.Run(stage, func(t *testing.T) {
			v := newWorkCleanup(t)
			v.store.seed(t, "task_events", v.base.p, 1)
			entered, release := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			collected := false
			t.Cleanup(func() {
				unblock()
				if !collected {
					_ = readerWaitError(t, done)
				}
				v.base.store.after = nil
			})
			var held atomic.Bool
			v.base.store.after = func(ctx context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				owner := readerOriginalOwner(v.pair, ctx, cause)
				isA := owner != nil && owner.record.Project == v.base.p || stage == "work-project-stop" && cause.Details().CheckpointRef == v.base.p.String()
				if cause.Details().Owner == stage && isA && held.CompareAndSwap(false, true) {
					if result.State() != f.Committed {
						t.Error("original read/close/Stop did not reach its committed callback")
					}
					close(entered)
					<-release
				}
				return result
			}
			go func() {
				if stage == "work-project-stop" {
					_, err := v.stop(context.Background(), v.base.p, true)
					done <- err
				} else {
					done <- v.read(context.Background(), v.base.p)
				}
			}()
			waitCall(t, entered, "original reader/Stop Store tail not reached")
			r, err := v.cleanup(v.ctx, nil)
			requireWorkCleanup(t, v, r, err, pc.CleanupPending, "task_events")
			if v.store.writes() != 0 {
				t.Fatal("cleanup treated callback/closed ledger as original return")
			}
			if err = v.read(context.Background(), v.base.b); err != nil {
				t.Fatal("A's retained tail blocked B", err)
			}
			select {
			case <-done:
				collected = true
				t.Fatal("held original call returned before release")
			default:
			}
			unblock()
			err = readerWaitError(t, done)
			collected = true
			if err != nil {
				t.Fatal(err)
			}
			v.base.store.after = nil
			r, err = v.cleanup(v.ctx, nil)
			state, next := pc.CleanupPending, "project_calls"
			if stage == "work-project-stop" {
				state, next = pc.CleanupCompleted, "completed"
			}
			requireWorkCleanup(t, v, r, err, state, next)
			if v.store.writes() != 1 {
				t.Fatal("next explicit cleanup failed to progress after original return")
			}
		})
	}
}

func TestWorkProjectCleanupOwnStoreTailAndLateAdmission(t *testing.T) {
	for _, stage := range []string{"preflight", "mutation", "exec"} {
		t.Run(stage, func(t *testing.T) {
			v := newWorkCleanup(t)
			v.store.seed(t, "task_events", v.base.p, 1)
			entered, release := make(chan struct{}), make(chan struct{})
			done := make(chan workCleanupAnswer, 1)
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			collected := false
			ctx, cancel := context.WithCancel(v.ctx)
			t.Cleanup(func() {
				unblock()
				cancel()
				if !collected {
					_ = waitWorkCleanup(t, done)
				}
				v.base.store.after = nil
				v.store.holdDelete = nil
			})
			var held atomic.Bool
			if stage == "exec" {
				v.store.holdDelete = func(context.Context) { close(entered); <-release }
			} else {
				v.base.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
					ready := stage == "preflight" && v.store.writes() == 0 || stage == "mutation" && v.store.writes() == 1
					if cause.Details().Owner == "work-project-cleanup" && ready && held.CompareAndSwap(false, true) {
						close(entered)
						<-release
					}
					return result
				}
			}
			go func() { r, e := v.cleanup(ctx, nil); done <- workCleanupAnswer{r, e} }()
			waitCall(t, entered, "cleanup did not reach its original tail")
			copy, err := NewProjectCleanupParticipant(v.pair, v.authority)
			if err != nil {
				t.Fatal(err)
			}
			calls, _, _ := v.base.store.counts()
			_, err = copy.Cleanup(v.ctx, v.authority.actor, v.base.cause, v.authority.scope, nil)
			pureCode(t, err, f.ResourceBusy)
			for _, request := range []bool{false, true} {
				_, err = v.stop(context.Background(), v.base.p, request)
				pureCode(t, err, f.ResourceBusy)
			}
			pureCode(t, v.read(context.Background(), v.base.p), f.ResourceBusy)
			after, _, _ := v.base.store.counts()
			if calls != after {
				t.Fatal("late same-P adapter/Stop/read entered Store during cleanup")
			}
			if err = v.read(context.Background(), v.base.b); err != nil {
				t.Fatal("A cleanup blocked real B reader", err)
			}
			r, err := v.stop(context.Background(), v.base.b, false)
			requireCallStop(t, r, err, pc.Stopped)
			cancel()
			v.pair.Stop()
			if v.pair.Joined() {
				t.Fatal("global stop/cancel replaced original cleanup return")
			}
			select {
			case <-done:
				collected = true
				t.Fatal("cancellation bypassed the held original return")
			default:
			}
			unblock()
			out := waitWorkCleanup(t, done)
			collected = true
			if out.err == nil || out.report.Details().Checkpoint != nil || !v.pair.Joined() {
				t.Fatal("cancelled original call published progress or kept a known returned control", out.err)
			}
		})
	}
}

func requireWorkCleanupUnknown(t *testing.T, r pc.CleanupReport, err error, first f.CommitResult) {
	t.Helper()
	requireCallUnknown(t, err, first)
	if r.Details().Checkpoint != nil {
		t.Fatal("Unknown published a cleanup checkpoint")
	}
}

func TestWorkProjectCleanupUnknownOriginalBatch(t *testing.T) {
	for _, physical := range []string{"applied", "not-applied", "preflight"} {
		t.Run(physical, func(t *testing.T) {
			v := newWorkCleanup(t)
			v.store.seed(t, "task_events", v.base.p, 2)
			v.store.seed(t, "task_blockers", v.base.p, 1)
			before := v.store.rows("task_events", v.base.p)
			var first f.CommitResult
			var masked bool
			v.base.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				expectedWrites := 1
				if physical == "preflight" {
					expectedWrites = 0
				}
				if cause.Details().Owner == "work-project-cleanup" && result.State() == f.Committed && !masked && v.store.writes() == expectedWrites {
					masked = true
					// Explicitly select the physical outcome. This Store is not an
					// implementation of database rollback.
					if physical == "not-applied" {
						for _, row := range before {
							v.store.put("task_events", v.base.p, row)
						}
					}
					first = f.UnknownResult(pureID[f.TransactionAttempt](t, 920), cause)
					return first
				}
				return result
			}
			r, err := v.cleanup(v.ctx, nil)
			requireWorkCleanupUnknown(t, r, err, first)
			v.base.store.after = nil
			writes := v.store.writes()
			copy, err := NewProjectCleanupParticipant(v.pair, v.authority)
			if err != nil {
				t.Fatal(err)
			}
			v.participant = copy
			calls, _, _ := v.base.store.counts()
			for _, request := range []bool{false, true} {
				_, err = v.stop(context.Background(), v.base.p, request)
				pureCode(t, err, f.ResourceBusy)
			}
			pureCode(t, v.read(context.Background(), v.base.p), f.ResourceBusy)
			after, _, _ := v.base.store.counts()
			if calls != after {
				t.Fatal("retained Unknown allowed late same-P Stop/read SQL")
			}
			if err = v.read(context.Background(), v.base.b); err != nil {
				t.Fatal(err)
			}
			stop, err := v.stop(context.Background(), v.base.b, false)
			requireCallStop(t, stop, err, pc.Stopped)
			r, err = v.cleanup(context.Background(), nil)
			requireWorkCleanupUnknown(t, r, err, first)
			v.base.store.after = func(_ context.Context, cause f.TransactionCause, _ int, _ f.CommitResult) f.CommitResult {
				return f.UnknownResult(pureID[f.TransactionAttempt](t, 921), cause)
			}
			r, err = v.cleanup(v.ctx, nil)
			requireWorkCleanupUnknown(t, r, err, first)
			v.base.store.after = nil
			if physical != "preflight" {
				// Mixed presence or changed full bytes never proves one atomic batch.
				changed := before[0]
				if physical == "not-applied" {
					var record map[string]any
					_ = json.Unmarshal(changed.Record, &record)
					record["type"] = "drifted-event"
					changed.Record, _ = workCleanupJSON(record)
				}
				v.store.put("task_events", v.base.p, changed)
				r, err = v.cleanup(v.ctx, nil)
				requireWorkCleanupUnknown(t, r, err, first)
				if physical == "applied" {
					v.store.dataMu.Lock()
					delete(v.store.tables["task_events"][v.base.p], changed.Key)
					v.store.dataMu.Unlock()
				} else {
					v.store.put("task_events", v.base.p, before[0])
				}
			}
			entered, release := make(chan struct{}), make(chan struct{})
			done := make(chan workCleanupAnswer, 1)
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			ctx, cancel := context.WithCancel(v.ctx)
			collected := false
			t.Cleanup(func() {
				unblock()
				cancel()
				if !collected {
					_ = waitWorkCleanup(t, done)
				}
				v.base.store.after = nil
			})
			v.base.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				a, b := cause.Details(), first.Cause().Details()
				if a.Owner != b.Owner || a.RecoveryRunID != b.RecoveryRunID || a.CheckpointRef != b.CheckpointRef || result.State() != f.Committed {
					t.Error("readonly confirmation lost original cause or validation")
				}
				close(entered)
				<-release
				return result
			}
			go func() { r, e := v.cleanup(ctx, nil); done <- workCleanupAnswer{r, e} }()
			waitCall(t, entered, "readonly confirmation original tail not reached")
			cancel()
			_, err = copy.Cleanup(v.ctx, v.authority.actor, v.base.cause, v.authority.scope, nil)
			pureCode(t, err, f.ResourceBusy)
			v.pair.state.mu.Lock()
			retained := v.pair.state.cleanups[v.base.p]
			valid := retained != nil && retained.active && retained.unknown != nil && retained.unknown.AttemptID() == first.AttemptID()
			v.pair.state.mu.Unlock()
			if !valid || v.store.writes() != writes {
				t.Fatal("cancel/callback replaced original confirmation return or wrote")
			}
			select {
			case <-done:
				collected = true
				t.Fatal("confirmation returned before original Store")
			default:
			}
			unblock()
			out := waitWorkCleanup(t, done)
			collected = true
			requireWorkCleanupUnknown(t, out.report, out.err, first)
			v.base.store.after = nil
			r, err = v.cleanup(v.ctx, nil)
			cp := requireWorkCleanup(t, v, r, err, pc.CleanupPending, "task_events")
			if v.store.writes() != writes {
				t.Fatal("successful confirmation sent a new DELETE")
			}
			r, err = v.cleanup(v.ctx, cp)
			stage, state := "task_blockers", pc.CleanupPending
			if physical == "applied" {
				stage, state = "completed", pc.CleanupCompleted
			}
			requireWorkCleanup(t, v, r, err, state, stage)
			if v.store.writes() != writes+1 {
				t.Fatal("next explicit call did not execute exactly one next batch")
			}
		})
	}
}

func TestWorkProjectCleanupForeignProofAndLedgerRecheck(t *testing.T) {
	for _, mode := range []string{"same-process-other-instance", "foreign-alive", "foreign-stopped", "changed-after-proof"} {
		t.Run(mode, func(t *testing.T) {
			v := newWorkCleanup(t)
			if err := v.read(context.Background(), v.base.p); err != nil {
				t.Fatal(err)
			}
			v.store.seed(t, "task_events", v.base.p, 1)
			// Start from an actual returned reader row. Changing its owner models
			// an imported durable row; the proof port is explicitly controlled.
			v.base.store.mu.Lock()
			var original projectCallRecord
			for _, row := range v.base.store.rows {
				original = row
			}
			row := original
			row.Instance = pureID[projectCallInstance](t, 970)
			if mode != "same-process-other-instance" {
				row.Process = pureID[oc.Process](t, 971)
			}
			v.base.store.rows[row.ID.String()] = row
			v.base.store.mu.Unlock()
			v.base.proof.confirm = func(_ context.Context, process oc.ProcessID) error {
				if process != row.Process {
					return errors.New("foreign proof used another process")
				}
				if mode == "foreign-alive" {
					return fault(f.ResourceBusy)
				}
				if mode == "changed-after-proof" {
					changed := row
					changed.Instance = pureID[projectCallInstance](t, 972)
					v.base.store.put(changed)
				}
				return nil
			}
			entered, release := make(chan struct{}), make(chan struct{})
			done := make(chan workCleanupAnswer, 1)
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			collected := false
			t.Cleanup(func() {
				unblock()
				if !collected {
					_ = waitWorkCleanup(t, done)
				}
				v.base.store.after = nil
			})
			var held atomic.Bool
			v.base.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				if cause.Details().Owner == "work-project-cleanup" && held.CompareAndSwap(false, true) {
					close(entered)
					<-release
				}
				return result
			}
			go func() { r, e := v.cleanup(v.ctx, nil); done <- workCleanupAnswer{r, e} }()
			waitCall(t, entered, "preflight original Store did not reach tail")
			if v.base.proof.confirms != 0 || v.store.writes() != 0 {
				t.Fatal("proof or DELETE ran before preflight original return")
			}
			unblock()
			out := waitWorkCleanup(t, done)
			collected = true
			v.base.store.after = nil
			switch mode {
			case "same-process-other-instance", "foreign-alive":
				requireWorkCleanup(t, v, out.report, out.err, pc.CleanupPending, "task_events")
			case "foreign-stopped":
				requireWorkCleanup(t, v, out.report, out.err, pc.CleanupPending, "project_calls")
			case "changed-after-proof":
				pureCode(t, out.err, f.ResourceBusy)
				if out.report.Details().Checkpoint != nil {
					t.Fatal("ledger drift published progress")
				}
			}
			wantWrites, wantProof := 0, 1
			if mode == "foreign-stopped" {
				wantWrites = 1
			}
			if mode == "same-process-other-instance" {
				wantProof = 0
			}
			if v.store.writes() != wantWrites || v.base.proof.confirms != wantProof {
				t.Fatal("unexpected foreign proof/mutation boundary")
			}
		})
	}
}

func TestWorkProjectCleanupLastLedgerUnknownAndJoin(t *testing.T) {
	v := newWorkCleanup(t)
	if err := v.read(context.Background(), v.base.p); err != nil {
		t.Fatal(err)
	}
	var first f.CommitResult
	v.base.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "work-project-cleanup" && v.store.writes() == 1 {
			first = f.UnknownResult(pureID[f.TransactionAttempt](t, 930), cause)
			return first
		}
		return result
	}
	r, err := v.cleanup(v.ctx, nil)
	requireWorkCleanupUnknown(t, r, err, first)
	if len(v.store.rows("project_calls", v.base.p)) != 0 || !slices.Equal(v.store.deletes, []string{"project_calls"}) {
		t.Fatal("last original ledger was not physically retired")
	}
	v.base.store.after = nil
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan workCleanupAnswer, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	collected := false
	t.Cleanup(func() {
		unblock()
		if !collected {
			_ = waitWorkCleanup(t, done)
		}
		v.base.store.after = nil
	})
	v.base.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
		if cause.Details().RecoveryRunID != first.Cause().Details().RecoveryRunID || result.State() != f.Committed {
			t.Error("empty final ledger did not confirm original exact deleted key")
		}
		close(entered)
		<-release
		return result
	}
	go func() { r, e := v.cleanup(v.ctx, nil); done <- workCleanupAnswer{r, e} }()
	waitCall(t, entered, "last-ledger readonly confirmation did not reach tail")
	v.pair.Stop()
	if v.pair.Joined() || v.store.writes() != 1 {
		t.Fatal("empty tables/global Stop replaced actual confirmation return")
	}
	unblock()
	out := waitWorkCleanup(t, done)
	collected = true
	requireWorkCleanup(t, v, out.report, out.err, pc.CleanupPending, "task_events")
	if !v.pair.Joined() || v.store.writes() != 1 {
		t.Fatal("confirmed original cleanup did not join without another DELETE")
	}
	v.base.store.after = nil
}
