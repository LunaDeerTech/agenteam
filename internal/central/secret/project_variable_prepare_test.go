package secret

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func projectVariablePrepareFixture(t *testing.T, metadataOnly, completed bool) (*Service, sc.ProjectVariableIntent, sc.ProjectVariableWritePlan, *projectAuditUnitStore, *projectVariableControlledAuthority) {
	t.Helper()
	s, _, plan, store, a := projectVariableReadFixture(t)
	intent := projectVariableIntentFixture(t, func(_ *sc.ProjectVariableWriteFields, v *sc.ProjectVariableIntentFields) {
		if metadataOnly {
			v.Value = nil
		}
	})
	basis, _ := plan.Basis()
	basis.Request = intent.Fields().Request
	if completed {
		r := basis.Request.Fields()
		receipt, _ := f.ParseID[sc.ProjectVariableReceipt]("01900000-0000-7000-8000-000000000009")
		user, _ := f.ParseID[i.User](r.Actor.Details().UserID)
		var err error
		basis.Receipt, err = sc.NewProjectVariableWriteObservation(sc.ProjectVariableWriteResultFields{ReceiptID: receipt, ProjectID: r.ProjectID, VariableID: r.VariableID, UserID: user, Identity: r.Identity, Kind: r.Kind, ExpectedVersion: r.ExpectedVersion, Ref: basis.Ref, Effect: sc.ProjectVariableReplaced, Version: 3})
		if err != nil {
			t.Fatal(err)
		}
		basis.VariableVersion = 0
	}
	plan, err := sc.NewProjectVariableWritePlan(a.issuer, basis, store.locks)
	if err != nil {
		t.Fatal(err)
	}
	state := s.state()
	state.initialized = true
	state.keys = testKeys(t)
	state.nonces = map[f.Version]*nonceRange{2: {next: 100, end: 110}}
	store.read = func(query string, _ []any) postgres.Row {
		if !strings.Contains(query, "FROM agenteam_secret.secret_control") {
			return projectAuditUnitRow{err: errors.New("unexpected preparation SQL")}
		}
		return projectAuditUnitRow{values: []any{int64(2), int64(1)}}
	}
	return s, intent, plan, store, a
}

func TestProjectVariableActualPrepareSealsAndOwnsWithoutBusinessSQL(t *testing.T) {
	for _, kind := range []string{"new-value", "metadata-only", "historical-replay"} {
		t.Run(kind, func(t *testing.T) {
			s, intent, plan, store, a := projectVariablePrepareFixture(t, kind == "metadata-only", kind == "historical-replay")
			prepared, err := s.PrepareProjectVariableWrite(context.Background(), intent, plan)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Destroy()
			if store.queries != 1 || a.checks != 0 {
				t.Fatal("preparation read business rows or pretended final authorization")
			}
			func() {
				state, err := s.lockProjectVariablePrepared(prepared)
				if err != nil {
					t.Fatal(err)
				}
				defer state.mu.Unlock()
				if state.receipt.ownerKind != projectVariableReceiptOwner || len(state.receipt.ciphertext) != 48 {
					t.Fatal("intent not sealed as kind3 digest")
				}
				got, err := openEnvelope(s.state().keys, state.receipt)
				if err != nil {
					t.Fatal(err)
				}
				want, err := projectVariableIntentDigest(intent)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatal("sealed original semantics differs")
				}
				clear(got)
				clear(want)
				if kind == "new-value" {
					value, err := openEnvelope(s.state().keys, state.value)
					if err != nil || string(value) != "opaque-value" {
						t.Fatal("original value not sealed")
					}
					clear(value)
					if s.state().nonces[2].next != 102 {
						t.Fatal("wrong nonce count")
					}
				} else if state.value.id.Validate() == nil || len(state.value.ciphertext) != 0 || s.state().nonces[2].next != 101 {
					t.Fatal("metadata/replay resealed business value")
				}
			}()
			projection, err := prepared.Preparation()
			if err != nil {
				t.Fatal(err)
			}
			fields, err := projection.Fields()
			if err != nil {
				t.Fatal(err)
			}
			basis, _ := plan.Basis()
			if !fields.Ref.Equal(basis.Ref) {
				t.Fatal("candidate Ref changed")
			}
			if kind == "historical-replay" {
				old, _ := basis.Receipt.Result()
				if fields.ReceiptID != old.ReceiptID {
					t.Fatal("historical receipt changed")
				}
			}
			prepared.Destroy()
			if _, err := prepared.Preparation(); err == nil {
				t.Fatal("retired projection accepted")
			}
			if intent.Validate() != nil {
				t.Fatal("prepared retired caller Intent")
			}
		})
	}
}

func TestProjectVariablePrepareRejectsPlanBeforeNonceOrSQL(t *testing.T) {
	for _, kind := range []string{"foreign-issuer", "changed-session", "unbound"} {
		t.Run(kind, func(t *testing.T) {
			s, intent, plan, store, a := projectVariablePrepareFixture(t, false, false)
			switch kind {
			case "foreign-issuer":
				a.issuer = sc.NewPlanIssuer()
			case "changed-session":
				intent = projectVariableIntentFixture(t, func(r *sc.ProjectVariableWriteFields, _ *sc.ProjectVariableIntentFields) {
					user, _ := f.ParseID[i.User](r.Actor.Details().UserID)
					session, _ := f.ParseID[i.Session]("01900000-0000-7000-8000-000000000099")
					r.Actor, _ = i.NewHuman(user, session)
				})
			case "unbound":
				s.state().auth.ProjectVariables = nil
			}
			if prepared, err := s.PrepareProjectVariableWrite(context.Background(), intent, plan); err == nil || prepared != nil {
				t.Fatal("unverified plan prepared")
			}
			if store.queries != 0 || s.state().nonces[2].next != 100 {
				t.Fatal("unverified plan consumed nonce/SQL")
			}
		})
	}
}

type projectVariableNonceUnknownStore struct {
	*projectAuditUnitStore
	reservations int
}

func (s *projectVariableNonceUnknownStore) WithinTx(_ context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.reservations++
	id, _ := f.ParseID[f.TransactionAttempt]("01900000-0000-7000-8000-000000000022")
	return f.UnknownResult(id, cause)
}
func TestProjectVariablePrepareNonceUnknownPublishesNoCandidate(t *testing.T) {
	s, intent, plan, store, _ := projectVariablePrepareFixture(t, false, false)
	unknown := &projectVariableNonceUnknownStore{projectAuditUnitStore: store}
	s.state().store = unknown
	s.state().nonces = map[f.Version]*nonceRange{}
	prepared, err := s.PrepareProjectVariableWrite(context.Background(), intent, plan)
	var domain *Error
	var fault *f.Fault
	if prepared != nil || !errors.As(err, &domain) || domain.Code() != NonceReservationUnknown || !errors.As(err, &fault) || fault.CommitState != f.Unknown {
		t.Fatal("nonce Unknown became prepared/committed")
	}
	if unknown.reservations != 1 || len(s.state().nonces) != 0 {
		t.Fatal("unknown range reused or callback retried")
	}
}
