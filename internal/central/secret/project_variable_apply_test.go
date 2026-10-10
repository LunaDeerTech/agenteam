package secret

import (
	"context"
	"errors"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// SQL/authority/Append transport are explicit controlled ports. Production
// Prepare, AEAD, Match, Apply and native Secret fact checker execute unchanged.
// This neither parses SQL in PostgreSQL nor proves D10 current authorization.
type projectVariableApplyStore struct {
	*projectAuditUnitStore
	ref              sc.CredentialRef
	purpose          sc.Purpose
	version          int64
	current          string
	payloads         map[string][]any
	receipt          []any
	execs            []string
	controlEpoch     int64
	busy, zeroChange bool
	failExec         string
}

func (s *projectVariableApplyStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.projectAuditUnitStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *projectVariableApplyStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.queries++
	row := func(v ...any) postgres.Row { return projectAuditUnitRow{values: v} }
	switch {
	case strings.Contains(query, "FROM agenteam_secret.secret_control"):
		return row(int64(2), s.controlEpoch)
	case strings.Contains(query, "FROM agenteam_secret.project_variable_receipts"):
		if s.receipt == nil {
			return projectAuditUnitRow{err: pgx.ErrNoRows}
		}
		r := s.receipt
		expected := pgtype.Int8{}
		if r[6] != nil {
			expected = pgtype.Int8{Int64: r[6].(int64), Valid: true}
		}
		if strings.Contains(query, "WHERE id=$1") {
			if args[0] != r[0] {
				return projectAuditUnitRow{err: pgx.ErrNoRows}
			}
			return row(r[1], r[2], r[3], r[4], r[5], expected, r[7], r[8], r[9], r[10], r[11])
		}
		if args[0] != r[1] || args[1] != r[4] {
			return projectAuditUnitRow{err: pgx.ErrNoRows}
		}
		return row(r[0], r[2], r[3], r[5], expected, r[7], r[8], r[9], r[10], r[11])
	case strings.Contains(query, "FROM agenteam_secret.secret_references"):
		return row(s.busy)
	case strings.Contains(query, "SELECT EXISTS(SELECT 1 FROM agenteam_secret.secrets"):
		return row(s.current != "")
	case strings.Contains(query, "FROM agenteam_secret.secrets"):
		if s.current == "" {
			return projectAuditUnitRow{err: pgx.ErrNoRows}
		}
		return row(string(s.purpose), s.version, s.current)
	case strings.Contains(query, "FROM agenteam_secret.secret_payloads"):
		p, ok := s.payloads[args[0].(string)]
		if !ok {
			return projectAuditUnitRow{err: pgx.ErrNoRows}
		}
		if strings.HasPrefix(query, "SELECT scope,") {
			return row(p[1], p[2], p[3], p[4])
		}
		return row(p...)
	}
	return projectAuditUnitRow{err: errors.New("unexpected controlled SQL")}
}
func (s *projectVariableApplyStore) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	s.execs = append(s.execs, query)
	if s.failExec != "" && strings.Contains(query, s.failExec) {
		return pgconn.CommandTag{}, errors.New("controlled write failure")
	}
	switch {
	case strings.HasPrefix(query, "INSERT INTO agenteam_secret.secret_payloads"):
		stored := append([]any(nil), args...)
		for n, value := range stored {
			if b, ok := value.([]byte); ok {
				stored[n] = append([]byte(nil), b...)
			}
		}
		s.payloads[args[0].(string)] = stored
	case strings.HasPrefix(query, "INSERT INTO agenteam_secret.secrets"):
		s.current, s.version, s.purpose = args[2].(string), 1, sc.ProjectVariable
	case strings.HasPrefix(query, "UPDATE agenteam_secret.secrets"):
		if s.zeroChange {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		s.current, s.version = args[4].(string), args[3].(int64)
	case strings.HasPrefix(query, "DELETE FROM agenteam_secret.secrets"):
		if s.zeroChange {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
		s.current = ""
	case strings.HasPrefix(query, "DELETE FROM agenteam_secret.secret_payloads"):
		delete(s.payloads, args[0].(string))
	case strings.HasPrefix(query, "DELETE FROM agenteam_secret.secret_leases"):
	case strings.HasPrefix(query, "INSERT INTO agenteam_secret.project_variable_receipts"):
		s.receipt = append([]any(nil), args...)
	default:
		return pgconn.CommandTag{}, errors.New("unexpected controlled write")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type projectVariableApplyAuthority struct {
	sc.ProjectVariableWriteAuthority
	issuer sc.PlanIssuer
	store  *projectVariableApplyStore
	stages []sc.ProjectVariableWriteStage
	deny   sc.ProjectVariableWriteStage
}

func (a *projectVariableApplyAuthority) CheckPlan(r sc.ProjectVariableWriteRequest, p sc.ProjectVariableWritePlan) error {
	if !p.Matches(a.issuer, r) {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	return nil
}
func (a *projectVariableApplyAuthority) CheckInTx(_ context.Context, tx f.Tx, r sc.ProjectVariableWriteRequest, p sc.ProjectVariableWritePlan, stage sc.ProjectVariableWriteStage) error {
	a.stages = append(a.stages, stage)
	if a.store.tx != tx || a.store.checks == 0 || !stage.Valid() {
		return errors.New("invalid controlled stage")
	}
	if err := a.CheckPlan(r, p); err != nil {
		return err
	}
	if stage == a.deny {
		return f.NewFault(f.SessionRevoked, f.NotStarted)
	}
	return nil
}

type projectVariableApplyAudit struct {
	checker *ProjectAuditAuthority
	calls   int
	err     error
	ctx     context.Context
	entry   ac.Entry
	key     ac.AppendKey
}

func (a *projectVariableApplyAudit) AppendInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) (ac.AppendReceipt, error) {
	a.calls++
	a.ctx, a.entry, a.key = ctx, entry, key
	if err := a.checker.CheckProjectAuditInTx(ctx, tx, entry, key); err != nil {
		return ac.AppendReceipt{}, err
	}
	return ac.AppendReceipt{}, a.err
}
func projectVariableApplyFixture(t *testing.T, kind sc.MutationKind, metadataOnly bool) (*Service, sc.PreparedProjectVariableWrite, *projectVariableApplyStore, *projectVariableApplyAuthority, *projectVariableApplyAudit, sc.ProjectVariableIntent) {
	t.Helper()
	s, _, plan, base, _ := projectVariablePrepareFixture(t, false, false)
	intent := projectVariableIntentFixture(t, func(r *sc.ProjectVariableWriteFields, v *sc.ProjectVariableIntentFields) {
		r.Kind = kind
		r.Identity, _ = f.NewCommandIdentity("projectvariable", []string{r.ProjectID.String()}, "project.secret_variable."+string(kind), "fixture-key")
		if kind == sc.Create {
			r.ExpectedVersion = nil
		}
		if metadataOnly || kind == sc.Delete {
			v.Value = nil
		}
		if kind == sc.Delete {
			v.Name, v.Description = nil, nil
		}
	})
	basis, _ := plan.Basis()
	basis.Request = intent.Fields().Request
	locks, err := sc.ProjectVariableWriteLocks(basis.Request, basis.Ref)
	if err != nil {
		t.Fatal(err)
	}
	base.locks = locks
	if kind == sc.Create {
		basis.VariableVersion, basis.CredentialVersion = 0, 0
	}
	store := &projectVariableApplyStore{projectAuditUnitStore: base, ref: basis.Ref, purpose: sc.ProjectVariable, version: 3, payloads: map[string][]any{}, controlEpoch: 1}
	if kind != sc.Create {
		old := projectAuditID[payloadMarker](t)
		nonce, _ := masterNonce(99)
		sealed, err := seal(s.state().keys, 2, nonce, basis.Ref.Details().Scope, valueOwner, basis.Ref.Details().ID.String(), old, []byte("prior-value"))
		if err != nil {
			t.Fatal(err)
		}
		store.current = old.String()
		store.payloads[store.current] = projectVariableEnvelopeRow(sealed)
	}
	a := &projectVariableApplyAuthority{issuer: sc.NewPlanIssuer(), store: store}
	plan, err = sc.NewProjectVariableWritePlan(a.issuer, basis, locks)
	if err != nil {
		t.Fatal(err)
	}
	checker, err := NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	appender := &projectVariableApplyAudit{checker: checker}
	s.state().store, s.state().auth.ProjectVariables, s.state().audit = store, a, appender
	p, err := s.PrepareProjectVariableWrite(context.Background(), intent, plan)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Destroy)
	return s, p, store, a, appender, intent
}
func projectVariableEnvelopeRow(p envelope) []any {
	return []any{p.id.String(), string(p.scope.Details().Kind), p.scope.Details().ProjectID, int16(p.ownerKind), p.ownerID, p.format, p.algorithm, p.dataNonce, p.ciphertext, p.wrapNonce, p.wrappedDEK, int64(p.masterVersion), int64(p.wrapRevision)}
}

func TestProjectVariableApplyActualCryptoStagesNativeFactsAndReplay(t *testing.T) {
	for _, name := range []string{"create", "replace", "delete", "none"} {
		t.Run(name, func(t *testing.T) {
			kind := sc.Update
			if name == "create" {
				kind = sc.Create
			}
			if name == "delete" {
				kind = sc.Delete
			}
			s, p, store, a, appender, _ := projectVariableApplyFixture(t, kind, name == "none")
			before := store.current
			got, err := s.ApplyProjectVariableWriteInTx(context.Background(), store.tx, p)
			if err != nil || !got.Observed() {
				t.Fatalf("actual Apply: %v", err)
			}
			r, _ := got.Result()
			wantVersion := f.Version(4)
			if name == "create" {
				wantVersion = 1
			}
			if name == "none" {
				wantVersion = 3
			}
			if r.Effect != sc.ProjectVariableEffect(name) || r.Version != wantVersion || r.Deleted != (name == "delete") {
				t.Fatal("effect/version mismatch")
			}
			if len(a.stages) != 2 || a.stages[0] != sc.ProjectVariableReceiptRead || a.stages[1] != sc.ProjectVariableNewWrite {
				t.Fatal("current stages/order")
			}
			if name == "none" {
				if appender.calls != 0 || store.current != before || len(store.execs) != 2 {
					t.Fatal("metadata-only changed value/audit")
				}
			} else if appender.calls != 1 {
				t.Fatal("missing native audit check")
			}
			if name == "replace" || name == "create" {
				payload, err := loadPayload(context.Background(), store, mustProjectVariablePayload(t, store.current))
				if err != nil {
					t.Fatal(err)
				}
				plain, err := openEnvelope(s.state().keys, payload)
				if err != nil || string(plain) != "opaque-value" {
					t.Fatal("native encrypted value")
				}
				clear(plain)
			}
			execs, audits := len(store.execs), appender.calls
			// Replay uses the encrypted original intent, even after its current value
			// is gone. It must not consult NewWrite or the canonical postimage.
			store.current = ""
			a.deny = sc.ProjectVariableNewWrite
			replay, err := s.ApplyProjectVariableWriteInTx(context.Background(), store.tx, p)
			if err != nil || !replay.Observed() || len(store.execs) != execs || appender.calls != audits || a.stages[len(a.stages)-1] != sc.ProjectVariableReceiptRead {
				t.Fatalf("receipt replay: %v", err)
			}
		})
	}
}
func mustProjectVariablePayload(t *testing.T, raw string) payloadID {
	t.Helper()
	id, err := f.ParseID[payloadMarker](raw)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestProjectVariableApplyRejectsBeforeWritesAndPropagatesFailures(t *testing.T) {
	for _, name := range []string{"receipt-gate", "write-gate", "missing-lock", "other-purpose", "version", "epoch", "busy", "zero-row", "receipt-write", "audit"} {
		t.Run(name, func(t *testing.T) {
			kind := sc.Update
			if name == "busy" {
				kind = sc.Delete
			}
			s, p, store, a, appender, _ := projectVariableApplyFixture(t, kind, false)
			switch name {
			case "receipt-gate":
				a.deny = sc.ProjectVariableReceiptRead
			case "write-gate":
				a.deny = sc.ProjectVariableNewWrite
			case "missing-lock":
				store.locks = nil
			case "other-purpose":
				store.purpose = sc.Model
			case "version":
				store.version = 99
			case "epoch":
				store.controlEpoch = 99
			case "busy":
				store.busy = true
			case "zero-row":
				store.zeroChange = true
			case "receipt-write":
				store.failExec = "INSERT INTO agenteam_secret.project_variable_receipts"
			case "audit":
				appender.err = errors.New("controlled audit rejected")
			}
			got, err := s.ApplyProjectVariableWriteInTx(context.Background(), store.tx, p)
			if err == nil || got.Validate() == nil {
				t.Fatal("failure published a result")
			}
			if name != "zero-row" && name != "receipt-write" && name != "audit" && len(store.execs) != 0 {
				t.Fatal("write happened before required checks")
			}
			if name != "audit" && appender.calls != 0 {
				t.Fatal("audit ran after earlier failure")
			}
		})
	}
}

func TestProjectVariableMatchRejectsChangedOriginalIntent(t *testing.T) {
	s, p, store, a, _, _ := projectVariableApplyFixture(t, sc.Update, false)
	if _, err := s.ApplyProjectVariableWriteInTx(context.Background(), store.tx, p); err != nil {
		t.Fatal(err)
	}
	state, err := s.lockProjectVariablePrepared(p)
	if err != nil {
		t.Fatal(err)
	}
	plan := state.plan
	state.mu.Unlock()
	changed := projectVariableIntentFixture(t, func(_ *sc.ProjectVariableWriteFields, v *sc.ProjectVariableIntentFields) {
		text := "different original description"
		v.Description = &text
	})
	other, err := s.PrepareProjectVariableWrite(context.Background(), changed, plan)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Destroy()
	before := len(store.execs)
	a.deny = sc.ProjectVariableNewWrite
	got, err := s.ApplyProjectVariableWriteInTx(context.Background(), store.tx, other)
	projectAuditCode(t, err, f.IdempotencyKeyReused)
	if got.Validate() == nil || len(store.execs) != before {
		t.Fatal("mismatched intent mutated")
	}
}

func TestProjectVariableDedicatedPurposeCannotUseLegacyPaths(t *testing.T) {
	s, p, store, _, _, intent := projectVariableApplyFixture(t, sc.Update, false)
	meta, _ := p.Preparation()
	safe, _ := meta.Fields()
	if sc.ProjectVariable.Valid() {
		t.Fatal("legacy purpose widened")
	}
	if _, _, err := loadMetadata(context.Background(), store, safe.Ref); err == nil {
		t.Fatal("legacy loader accepted dedicated stored purpose")
	}
	r := intent.Fields().Request.Fields()
	if err := validateWrite(sc.WriteRequest{Actor: r.Actor, Scope: safe.Ref.Details().Scope, Identity: r.Identity, Kind: sc.Update, Ref: safe.Ref, ExpectedVersion: 3, Purpose: sc.ProjectVariable}); err == nil {
		t.Fatal("legacy Write accepted dedicated purpose")
	}
	_ = s
}

func TestProjectVariableHistoricalReplayUsesOriginalReceiptAfterHandleRetirement(t *testing.T) {
	s, p, store, a, appender, _ := projectVariableApplyFixture(t, sc.Update, false)
	original, err := s.ApplyProjectVariableWriteInTx(context.Background(), store.tx, p)
	if err != nil {
		t.Fatal(err)
	}
	state, err := s.lockProjectVariablePrepared(p)
	if err != nil {
		t.Fatal(err)
	}
	basis, err := state.plan.Basis()
	state.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	p.Destroy()
	intent := projectVariableIntentFixture(t, func(r *sc.ProjectVariableWriteFields, _ *sc.ProjectVariableIntentFields) {
		user, _ := f.ParseID[i.User](r.Actor.Details().UserID)
		r.Actor, _ = i.NewHuman(user, projectAuditID[i.Session](t))
	})
	result, _ := original.Result()
	basis.Request, basis.Receipt, basis.VariableVersion, basis.CredentialVersion = intent.Fields().Request, original, 0, result.Version
	plan, err := sc.NewProjectVariableWritePlan(a.issuer, basis, store.locks)
	if err != nil {
		t.Fatal(err)
	}
	store.current = ""
	later, err := s.PrepareProjectVariableWrite(context.Background(), intent, plan)
	if err != nil {
		t.Fatal(err)
	}
	defer later.Destroy()
	a.deny = sc.ProjectVariableNewWrite
	before, priorAudit := len(store.execs), appender.calls
	got, err := s.ApplyProjectVariableWriteInTx(context.Background(), store.tx, later)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := got.Result()
	if err != nil || replayed.ReceiptID != result.ReceiptID || replayed.Version != 4 || len(store.execs) != before || appender.calls != priorAudit {
		t.Fatal("historical replay changed original result")
	}
	a.deny = sc.ProjectVariableReceiptRead
	if denied, err := s.ApplyProjectVariableWriteInTx(context.Background(), store.tx, later); err == nil || denied.Validate() == nil {
		t.Fatal("history bypassed current Session/Owner gate")
	}
}
