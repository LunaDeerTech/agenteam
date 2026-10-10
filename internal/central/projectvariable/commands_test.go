package projectvariable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func testID[T any](n int) f.ID[T] {
	v, e := f.ParseID[T](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if e != nil {
		panic(e)
	}
	return v
}
func runtimeRecord(t *testing.T) (*commandRecord, i.Actor) {
	t.Helper()
	actor, _ := i.NewHuman(testID[i.User](3), testID[i.Session](4))
	at, _ := f.ParseInstant("2026-10-09T10:00:00Z")
	request, e := c.NewVariableCreate(c.VariableCreateFields{ID: testID[i.ProjectVariable](1), Name: "NAME", Description: "description-canary", Value: "value-canary"})
	if e != nil {
		t.Fatal(e)
	}
	input := commandInput{Project: testID[i.Project](2), User: testID[i.User](3), Command: c.CreateCommand, Target: request.Fields().ID, Create: &request}
	semantic, e := input.semantic(actor, "key-canary")
	if e != nil {
		t.Fatal(e)
	}
	plan, changed, e := planVariable(input, nil, at, testID[c.Operation](6))
	if e != nil || !changed {
		t.Fatal(e)
	}
	eid := testID[event.EventIdentity](7)
	return &commandRecord{ID: testID[c.Operation](5), Project: input.Project, User: input.User, Command: input.Command, Key: "key-canary", Semantic: semantic, Target: input.Target, Input: input, State: "planned", Revision: 1, Plan: plan, EventID: &eid, Created: at}, actor
}
func code(t *testing.T, e error, want f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(e, &fault) || fault.Code != want {
		t.Fatalf("want %s got %v", want, e)
	}
}
func TestVariablePlanVersionNoopDeleteAndImmutableHistory(t *testing.T) {
	r, actor := runtimeRecord(t)
	if e := validateRecord(r, actor); e != nil {
		t.Fatal(e)
	}
	before := r.Plan.After
	value := before.Fields().Value
	version := f.Version(1)
	update, _ := c.NewVariableUpdate(c.VariableUpdateFields{Value: &value})
	in := commandInput{Project: r.Project, User: r.User, Command: c.UpdateCommand, Target: r.Target, Expected: &version, Update: &update}
	plan, changed, e := planVariable(in, &before, r.Created, r.ID)
	if e != nil || changed || !sameValue(plan.After, before) {
		t.Fatal("no-op changed")
	}
	version = 2
	_, _, e = planVariable(in, &before, r.Created, r.ID)
	code(t, e, f.VersionConflict)
	version = 1
	value = "new-value"
	update, _ = c.NewVariableUpdate(c.VariableUpdateFields{Value: &value})
	in.Update = &update
	plan, changed, e = planVariable(in, &before, r.Created, r.ID)
	if e != nil || !changed || plan.After.Fields().Version != 2 || strings.Join(plan.Fields, ",") != "value" || before.Fields().Value != "value-canary" {
		t.Fatal("update")
	}
	in.Command = c.DeleteCommand
	in.Update = nil
	plan, changed, e = planVariable(in, &before, r.Created, r.ID)
	if e != nil || !changed || !plan.Deleted || plan.After.Fields().Value != "" || plan.After.Fields().Description != "" || plan.After.Fields().Name != "NAME" {
		t.Fatal("delete")
	}
	maximum := before.Fields()
	maximum.Version = f.Version(math.MaxInt64)
	before, _ = c.NewVariable(maximum)
	version = maximum.Version
	in.Command = c.UpdateCommand
	value = maximum.Value
	update, _ = c.NewVariableUpdate(c.VariableUpdateFields{Value: &value})
	in.Update = &update
	if _, changed, e = planVariable(in, &before, r.Created, r.ID); e != nil || changed {
		t.Fatal("max no-op")
	}
	value = "different"
	update, _ = c.NewVariableUpdate(c.VariableUpdateFields{Value: &value})
	in.Update = &update
	_, _, e = planVariable(in, &before, r.Created, r.ID)
	code(t, e, f.ResourceBusy)
}
func TestVariableRecordReceiptBoundToOriginalIntent(t *testing.T) {
	r, actor := runtimeRecord(t)
	receipt, e := mutationReceipt(r, testID[ac.Record](8))
	if e != nil {
		t.Fatal(e)
	}
	r.State = "completed"
	r.Receipt = &receipt
	r.Committed = &r.Created
	if e = validateRecord(r, actor); e != nil {
		t.Fatal(e)
	}
	nextSession, _ := i.NewHuman(r.User, testID[i.Session](9))
	if e = validateRecord(r, nextSession); e != nil {
		t.Fatal("session in intent")
	}
	wrongUser, _ := i.NewHuman(testID[i.User](10), testID[i.Session](9))
	if e = validateRecord(r, wrongUser); e == nil {
		t.Fatal("wrong user")
	}
	raw, e := json.Marshal(r.Plan)
	if e != nil {
		t.Fatal(e)
	}
	var restored commandPlan
	if json.Unmarshal(raw, &restored) != nil || !sameValue(restored, r.Plan) {
		t.Fatal("plan roundtrip")
	}
	// A well-typed postimage and receipt still must implement the original input.
	original := r.Plan.After
	v := original.Fields()
	v.Value = "forged"
	r.Plan.After, _ = c.NewVariable(v)
	if e = validateRecord(r, actor); e == nil {
		t.Fatal("changed receipt intent")
	}
	r.Plan.After = original
	r.Receipt = nil
	if e = validateRecord(r, actor); e == nil {
		t.Fatal("completed missing receipt")
	}
}
func TestVariableStorageDiagnosticsNeverPublishUserData(t *testing.T) {
	e := unavailable(&pgconn.PgError{Code: "23514", Message: "name-canary", Detail: "value-canary", InternalQuery: "query-canary"})
	for current := e; current != nil; current = errors.Unwrap(current) {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			s := fmt.Sprintf(format, current)
			if strings.Contains(s, "canary") {
				t.Fatal("diagnostic leak")
			}
		}
	}
	code(t, unavailable(&pgconn.PgError{Code: "23505", ConstraintName: "variables_live_name", Detail: "value-canary"}), f.ResourceBusy)
}

// This SQL boundary control checks the returned classification, not a real PG race.
type variableIDRow func(...any) error

func (r variableIDRow) Scan(dest ...any) error { return r(dest...) }

type variableIDExecutor struct {
	postgres.SQLExecutor
	project     string
	insertError error
	insertTag   pgconn.CommandTag
	statements  *[]string
}

func (x variableIDExecutor) QueryRow(_ context.Context, sql string, _ ...any) postgres.Row {
	return variableIDRow(func(dest ...any) error {
		switch {
		case strings.Contains(sql, "SELECT project_id FROM agenteam_projectvariable.variables WHERE id="):
			*dest[0].(*string) = x.project
		case strings.Contains(sql, "SELECT count(*)"), strings.Contains(sql, "SELECT COALESCE"):
			*dest[0].(*int64) = 1
		case strings.Contains(sql, "SELECT EXISTS"):
			*dest[0].(*bool) = false
		default:
			return pgx.ErrNoRows
		}
		return nil
	})
}
func (x variableIDExecutor) Exec(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
	if x.statements != nil {
		*x.statements = append(*x.statements, query)
	}
	return x.insertTag, x.insertError
}

func TestVariableCreateConcurrentIDZeroRowsStopsFacts(t *testing.T) {
	r, _ := runtimeRecord(t)
	var statements []string
	err := applyPlan(context.Background(), variableIDExecutor{insertTag: pgconn.NewCommandTag("INSERT 0 0"), statements: &statements}, r)
	code(t, err, f.NotFound)
	if len(statements) != 1 || !strings.Contains(statements[0], "ON CONFLICT ON CONSTRAINT variables_pkey DO NOTHING") {
		t.Fatal("ID conflict must be a precise non-failing insert with no later generation/history write")
	}
	statements = nil
	err = applyPlan(context.Background(), variableIDExecutor{insertTag: pgconn.NewCommandTag("INSERT 0 2"), statements: &statements}, r)
	code(t, err, f.InternalError)
	if len(statements) != 1 {
		t.Fatal("invalid row count wrote later facts")
	}
}
func TestVariableCreateForeignIDClassification(t *testing.T) {
	r, _ := runtimeRecord(t)
	code(t, checkPreimage(context.Background(), variableIDExecutor{project: testID[i.Project](200).String()}, r), f.NotFound)
	same := checkPreimage(context.Background(), variableIDExecutor{project: r.Project.String()}, r)
	code(t, same, f.ResourceBusy)
	var detail *f.Fault
	if !errors.As(same, &detail) || len(detail.FieldErrors) != 1 || detail.FieldErrors[0].Path != "/variable_id" || detail.FieldErrors[0].Code != "ID_CONFLICT" {
		t.Fatal("same Project ID classification changed")
	}
}
func TestVariableCreateUnexpectedSQLFailureClassification(t *testing.T) {
	r, _ := runtimeRecord(t)
	for _, tc := range []struct {
		constraint, state string
		want              f.Code
	}{
		{"variables_pkey", "23505", f.ResourceBusy},
		{"variables_live_name", "23505", f.ResourceBusy},
		{"another_unique", "23505", f.DependencyUnavailable},
		{"variables_pkey", "23514", f.DependencyUnavailable},
	} {
		t.Run(tc.constraint+tc.state, func(t *testing.T) {
			e := &pgconn.PgError{Code: tc.state, ConstraintName: tc.constraint, Detail: "private-canary"}
			code(t, applyPlan(context.Background(), variableIDExecutor{insertError: e}, r), tc.want)
		})
	}
	// The ordinary storage classifier is not weakened for other commands.
	code(t, unavailable(&pgconn.PgError{Code: "23505", ConstraintName: "variables_pkey"}), f.ResourceBusy)
}
