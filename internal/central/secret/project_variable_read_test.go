package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// This is only a controlled port for call-order/negative tests. It supplies no
// D10 current-authority evidence and is never linked by production composition.
type projectVariableControlledAuthority struct {
	sc.ProjectVariableWriteAuthority
	issuer sc.PlanIssuer
	store  *projectAuditUnitStore
	checks int
	reject error
}

func (a *projectVariableControlledAuthority) CheckPlan(r sc.ProjectVariableWriteRequest, p sc.ProjectVariableWritePlan) error {
	if !p.Matches(a.issuer, r) {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}
func (a *projectVariableControlledAuthority) CheckInTx(_ context.Context, tx f.Tx, r sc.ProjectVariableWriteRequest, p sc.ProjectVariableWritePlan, stage sc.ProjectVariableWriteStage) error {
	a.checks++
	if tx != a.store.tx || a.store.checks == 0 || stage != sc.ProjectVariableReceiptRead {
		return errors.New("wrong controlled call order")
	}
	if err := a.CheckPlan(r, p); err != nil {
		return err
	}
	return a.reject
}
func projectVariableReadFixture(t *testing.T) (*Service, sc.ProjectVariableWriteRequest, sc.ProjectVariableWritePlan, *projectAuditUnitStore, *projectVariableControlledAuthority) {
	t.Helper()
	s, p, state := projectVariablePreparedFixture(t)
	defer p.Destroy()
	meta, err := state.preparation.Fields()
	if err != nil {
		t.Fatal(err)
	}
	store := &projectAuditUnitStore{tx: f.NewTx(), locks: append([]f.LockRequest(nil), state.locks...)}
	issuer := sc.NewPlanIssuer()
	a := &projectVariableControlledAuthority{issuer: issuer, store: store}
	plan, err := sc.NewProjectVariableWritePlan(issuer, sc.ProjectVariableWriteBasisFields{Request: meta.Request, Ref: meta.Ref, VariableVersion: 7, CredentialVersion: 3, Receipt: sc.ProjectVariableWriteNotObserved()}, store.locks)
	if err != nil {
		t.Fatal(err)
	}
	s.state().store = store
	s.state().auth.ProjectVariables = a
	store.read = func(string, []any) postgres.Row { return projectAuditUnitRow{err: pgx.ErrNoRows} }
	return s, meta.Request, plan, store, a
}
func TestProjectVariableLookupCurrentGateAndCallerTxBeforeSQL(t *testing.T) {
	for _, name := range []string{"unbound", "foreign-tx", "missing-lock", "current-denied", "wrong-issuer", "cancelled", "zero-request"} {
		t.Run(name, func(t *testing.T) {
			s, r, p, store, a := projectVariableReadFixture(t)
			tx := store.tx
			ctx := context.Background()
			switch name {
			case "unbound":
				s.state().auth.ProjectVariables = nil
			case "foreign-tx":
				tx = f.NewTx()
			case "missing-lock":
				store.locks = nil
			case "current-denied":
				a.reject = f.NewFault(f.SessionRevoked, f.NotStarted)
			case "wrong-issuer":
				a.issuer = sc.NewPlanIssuer()
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "zero-request":
				r = sc.ProjectVariableWriteRequest{}
			}
			if result, err := s.LookupProjectVariableWriteInTx(ctx, tx, r, p); err == nil || result.Validate() == nil {
				t.Fatal("invalid lookup published observation")
			}
			if store.queries != 0 {
				t.Fatal("receipt SQL ran before all gates")
			}
		})
	}
	s, r, p, store, a := projectVariableReadFixture(t)
	result, err := s.LookupProjectVariableWriteInTx(context.Background(), store.tx, r, p)
	if err != nil || result.Validate() != nil || result.Observed() || store.queries != 1 || a.checks != 1 {
		t.Fatal("authorized absence not distinct")
	}
}

func projectVariableReceiptRowValues() []any {
	return []any{
		"01900000-0000-7000-8000-000000000009", "01900000-0000-7000-8000-000000000004", "01900000-0000-7000-8000-000000000001", "project.secret_variable.update", pgtype.Int8{Int64: 7, Valid: true}, "01900000-0000-7000-8000-000000000008", "none", int64(3), false, "01900000-0000-7000-8000-000000000010",
	}
}
func TestProjectVariableLookupSafeReceiptAndExactPayloadOwner(t *testing.T) {
	for _, name := range []string{"valid", "other-writer", "other-variable", "different-ref", "invalid-version", "unknown-effect", "wrong-payload-owner"} {
		t.Run(name, func(t *testing.T) {
			s, r, p, store, _ := projectVariableReadFixture(t)
			values := projectVariableReceiptRowValues()
			owner := values[0].(string)
			switch name {
			case "other-writer":
				values[2] = "01900000-0000-7000-8000-000000000099"
			case "other-variable":
				values[1] = "01900000-0000-7000-8000-000000000099"
			case "different-ref":
				values[5] = "01900000-0000-7000-8000-000000000099"
			case "invalid-version":
				values[7] = int64(0)
			case "unknown-effect":
				values[6] = "unknown"
			case "wrong-payload-owner":
				owner = "01900000-0000-7000-8000-000000000099"
			}
			store.read = func(query string, args []any) postgres.Row {
				if strings.Contains(query, "FROM agenteam_secret.project_variable_receipts") {
					if len(args) != 2 || args[0] != r.Fields().ProjectID.String() {
						t.Fatal("receipt project binding")
					}
					return projectAuditUnitRow{values: values}
				}
				if strings.Contains(query, "FROM agenteam_secret.secret_payloads") {
					if len(args) != 1 || args[0] != values[9] {
						t.Fatal("payload binding")
					}
					return projectAuditUnitRow{values: []any{"project", r.Fields().ProjectID.String(), int16(3), owner}}
				}
				return projectAuditUnitRow{err: errors.New("unexpected query")}
			}
			got, err := s.LookupProjectVariableWriteInTx(context.Background(), store.tx, r, p)
			if name != "valid" {
				if err == nil || got.Validate() == nil {
					t.Fatal("bad stored binding accepted")
				}
				return
			}
			if err != nil || !got.Observed() {
				t.Fatal("safe receipt not observed")
			}
			fields, _ := got.Result()
			if fields.Effect != sc.ProjectVariableUnchanged || fields.Version != 3 || *fields.ExpectedVersion != 7 {
				t.Fatal("safe result changed versions")
			}
		})
	}
}
func TestProjectVariableLookupMissingHistoricalReceiptFailsClosed(t *testing.T) {
	s, r, p, store, a := projectVariableReadFixture(t)
	basis, _ := p.Basis()
	receiptID, _ := f.ParseID[sc.ProjectVariableReceipt]("01900000-0000-7000-8000-000000000009")
	// Obtain typed stable UserID from the existing Human request without a Session
	// in the persisted result; the actual port still checks the current Session.
	result := sc.ProjectVariableWriteResultFields{ReceiptID: receiptID, ProjectID: r.Fields().ProjectID, VariableID: r.Fields().VariableID, Identity: r.Fields().Identity, Kind: sc.Update, ExpectedVersion: r.Fields().ExpectedVersion, Ref: basis.Ref, Effect: sc.ProjectVariableUnchanged, Version: 3}
	if err := result.UserID.UnmarshalText([]byte(r.Fields().Actor.Details().UserID)); err != nil {
		t.Fatal(err)
	}
	observed, err := sc.NewProjectVariableWriteObservation(result)
	if err != nil {
		t.Fatal(err)
	}
	basis.Receipt = observed
	basis.VariableVersion = 0
	plan, err := sc.NewProjectVariableWritePlan(a.issuer, basis, store.locks)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.LookupProjectVariableWriteInTx(context.Background(), store.tx, r, plan)
	if err == nil || got.Validate() == nil {
		t.Fatal("lost completed receipt became unobserved")
	}
}
