package secret

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// The Store/authority are controlled ports. The candidate, encryption, decoder,
// current-stage sequence and Match are the actual frozen production methods.
type storageReviewHistory struct {
	s       *Service
	store   *projectAuditUnitStore
	a       *projectVariableControlledAuthority
	intent  sc.ProjectVariableIntent
	plan    sc.ProjectVariableWritePlan
	stored  envelope
	result  sc.ProjectVariableWriteResultFields
	missing bool
}

func newStorageReviewHistory(t *testing.T) *storageReviewHistory {
	t.Helper()
	s, intent, plan, store, a := projectVariablePrepareFixture(t, false, false)
	original, err := s.PrepareProjectVariableWrite(context.Background(), intent, plan)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Destroy()
	state, err := s.lockProjectVariablePrepared(original)
	if err != nil {
		t.Fatal(err)
	}
	p := state.receipt
	p.dataNonce, p.ciphertext = bytes.Clone(p.dataNonce), bytes.Clone(p.ciphertext)
	p.wrapNonce, p.wrappedDEK = bytes.Clone(p.wrapNonce), bytes.Clone(p.wrappedDEK)
	meta, err := state.preparation.Fields()
	state.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	r := meta.Request.Fields()
	user, _ := f.ParseID[i.User](r.Actor.Details().UserID)
	result := sc.ProjectVariableWriteResultFields{ReceiptID: meta.ReceiptID, ProjectID: r.ProjectID, VariableID: r.VariableID, UserID: user, Identity: r.Identity, Kind: r.Kind, ExpectedVersion: r.ExpectedVersion, Ref: meta.Ref, Effect: sc.ProjectVariableReplaced, Version: 3}
	observed, err := sc.NewProjectVariableWriteObservation(result)
	if err != nil {
		t.Fatal(err)
	}
	basis, _ := plan.Basis()
	basis.Receipt, basis.VariableVersion = observed, 0
	plan, err = sc.NewProjectVariableWritePlan(a.issuer, basis, store.locks)
	if err != nil {
		t.Fatal(err)
	}
	x := &storageReviewHistory{s: s, store: store, a: a, intent: intent, plan: plan, stored: p, result: result}
	t.Cleanup(func() { clearProjectVariableEnvelope(&x.stored) })
	store.read = func(query string, args []any) postgres.Row {
		switch {
		case strings.Contains(query, "FROM agenteam_secret.secret_control"):
			return projectAuditUnitRow{values: []any{int64(2), int64(1)}}
		case strings.Contains(query, "FROM agenteam_secret.project_variable_receipts"):
			if len(args) != 2 || args[0] != x.result.ProjectID.String() {
				t.Fatal("receipt escaped original Project/command query")
			}
			if x.missing {
				return projectAuditUnitRow{err: pgx.ErrNoRows}
			}
			return projectAuditUnitRow{values: []any{x.result.ReceiptID.String(), x.result.VariableID.String(), x.result.UserID.String(), x.result.Identity.Command(), pgtype.Int8{Int64: int64(*x.result.ExpectedVersion), Valid: true}, x.result.Ref.Details().ID.String(), string(x.result.Effect), int64(x.result.Version), x.result.Deleted, x.stored.id.String()}}
		case strings.Contains(query, "FROM agenteam_secret.secret_payloads"):
			if len(args) != 1 || args[0] != x.stored.id.String() {
				t.Fatal("payload query escaped exact receipt mapping")
			}
			if strings.Contains(query, "SELECT "+payloadColumns) {
				return projectVariablePayloadRow{x.stored, int16(x.stored.ownerKind)}
			}
			return projectAuditUnitRow{values: []any{string(x.stored.scope.Details().Kind), x.stored.scope.Details().ProjectID, int16(x.stored.ownerKind), x.stored.ownerID}}
		default:
			t.Fatal("history Match must not read current canonical or business value")
			return projectAuditUnitRow{err: errors.New("unexpected review query")}
		}
	}
	return x
}

func TestIndependentStorageActualPrepareMatchesHistory(t *testing.T) {
	for _, name := range []string{"same-intent-new-session", "different-intent", "missing-observed", "cipher-corruption", "kind2-relabel", "receipt-substitution", "project-substitution", "payload-substitution"} {
		t.Run(name, func(t *testing.T) {
			x := newStorageReviewHistory(t)
			intent := projectVariableIntentFixture(t, func(r *sc.ProjectVariableWriteFields, fields *sc.ProjectVariableIntentFields) {
				if name == "same-intent-new-session" {
					session, _ := f.ParseID[i.Session]("01900000-0000-7000-8000-000000000077")
					user, _ := f.ParseID[i.User](r.Actor.Details().UserID)
					r.Actor, _ = i.NewHuman(user, session)
				}
				if name == "different-intent" {
					text := "different original description"
					fields.Description = &text
				}
			})
			plan := x.plan
			if name == "same-intent-new-session" {
				// The old plan must reject the new Session before SQL/nonces.
				beforeSQL, beforeNonce := x.store.queries, x.s.state().nonces[2].next
				if p, err := x.s.PrepareProjectVariableWrite(context.Background(), intent, plan); p != nil || err == nil || x.store.queries != beforeSQL || x.s.state().nonces[2].next != beforeNonce {
					t.Fatal("old Session plan admitted or touched nonce/SQL")
				}
				basis, _ := plan.Basis()
				basis.Request = intent.Fields().Request
				var err error
				plan, err = sc.NewProjectVariableWritePlan(x.a.issuer, basis, x.store.locks)
				if err != nil {
					t.Fatal(err)
				}
			}
			prepared, err := x.s.PrepareProjectVariableWrite(context.Background(), intent, plan)
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Destroy()
			switch name {
			case "missing-observed":
				x.missing = true
			case "cipher-corruption":
				x.stored.ciphertext[0] ^= 1
			case "kind2-relabel":
				x.stored.ownerKind = receiptOwner
			case "receipt-substitution":
				x.stored.ownerID = "01900000-0000-7000-8000-000000000078"
			case "project-substitution":
				id, _ := f.ParseID[i.Project]("01900000-0000-7000-8000-000000000078")
				x.stored.scope, _ = i.InProject(id)
			case "payload-substitution":
				x.stored.id, _ = f.ParseID[payloadMarker]("01900000-0000-7000-8000-000000000078")
			}
			beforeChecks := x.a.checks
			got, err := x.s.MatchProjectVariableIntentInTx(context.Background(), x.store.tx, prepared)
			if x.a.checks != beforeChecks+1 {
				t.Fatal("Match skipped actual current ReceiptRead stage")
			}
			if name != "same-intent-new-session" {
				if err == nil || got.Validate() == nil {
					t.Fatal("inexact/absent history published a valid observation")
				}
				if name == "different-intent" {
					projectAuditCode(t, err, f.IdempotencyKeyReused)
				}
				return
			}
			if err != nil || !got.Observed() {
				t.Fatal("same original semantics did not match under new current Session", err)
			}
			result, _ := got.Result()
			if result.ReceiptID != x.result.ReceiptID || !result.Ref.Equal(x.result.Ref) || result.Version != 3 || *result.ExpectedVersion != 7 {
				t.Fatal("history substituted current/internal identity or version")
			}
		})
	}
}

type storageReviewBlockedAuthority struct {
	*projectVariableControlledAuthority
	entered, release chan struct{}
	refusal          error
}

func (a *storageReviewBlockedAuthority) CheckInTx(ctx context.Context, tx f.Tx, r sc.ProjectVariableWriteRequest, p sc.ProjectVariableWritePlan, stage sc.ProjectVariableWriteStage) error {
	if err := a.projectVariableControlledAuthority.CheckInTx(ctx, tx, r, p, stage); err != nil {
		return err
	}
	close(a.entered)
	<-a.release
	return a.refusal
}

func storageReviewWait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("independent controlled operation did not return")
	}
}

func TestIndependentStorageDestroyJoinsActualMatch(t *testing.T) {
	for _, cancelCurrent := range []bool{false, true} {
		name := "original-unknown"
		if cancelCurrent {
			name = "cancelled-current-stage"
		}
		t.Run(name, func(t *testing.T) {
			x := newStorageReviewHistory(t)
			prepared, err := x.s.PrepareProjectVariableWrite(context.Background(), x.intent, x.plan)
			if err != nil {
				t.Fatal(err)
			}
			state, err := x.s.lockProjectVariablePrepared(prepared)
			if err != nil {
				t.Fatal(err)
			}
			borrowed := [][]byte{state.receipt.ciphertext, state.receipt.wrappedDEK, state.receipt.dataNonce, state.receipt.wrapNonce}
			state.mu.Unlock()
			cause := errors.New("original current-stage cause")
			original := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
			gate := &storageReviewBlockedAuthority{projectVariableControlledAuthority: x.a, entered: make(chan struct{}), release: make(chan struct{}), refusal: original}
			if cancelCurrent {
				gate.refusal = nil
			}
			x.s.state().auth.ProjectVariables = gate
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var once sync.Once
			release := func() { once.Do(func() { close(gate.release) }) }
			defer release()
			used := make(chan error, 1)
			go func() {
				got, err := x.s.MatchProjectVariableIntentInTx(ctx, x.store.tx, prepared)
				if got.Validate() == nil {
					used <- errors.New("refused current stage published observation")
					return
				}
				used <- err
			}()
			storageReviewWait(t, gate.entered)
			beforeQueries := x.store.queries
			alias := *(prepared.(*preparedProjectVariableWrite))
			started, destroyed := make(chan struct{}), make(chan struct{})
			go func() { close(started); alias.Destroy(); close(destroyed) }()
			<-started
			select {
			case <-destroyed:
				t.Fatal("Destroy claimed retired while actual Match still held")
			default:
			}
			for _, value := range borrowed {
				if bytes.Equal(value, make([]byte, len(value))) {
					t.Fatal("Destroy cleared a live prepared buffer")
				}
			}
			if cancelCurrent {
				cancel()
			}
			release()
			err = <-used
			storageReviewWait(t, destroyed)
			if !cancelCurrent && (err != original || !errors.Is(err, cause)) || cancelCurrent && !errors.Is(err, context.Canceled) {
				t.Fatal("actual Match lost its original error", err)
			}
			if x.store.queries != beforeQueries {
				t.Fatal("denied/cancelled current stage read protected history")
			}
			for _, value := range borrowed {
				if !bytes.Equal(value, make([]byte, len(value))) {
					t.Fatal("joined Destroy retained owned buffers")
				}
			}
			if x.intent.Validate() != nil {
				t.Fatal("Destroy consumed caller intent")
			}
			if _, err := prepared.Preparation(); err == nil {
				t.Fatal("retired projection survived")
			}
		})
	}
}

func TestIndependentStoragePublicMatchRejectsForgedPreparedBeforeMethods(t *testing.T) {
	x := newStorageReviewHistory(t)
	real, err := x.s.PrepareProjectVariableWrite(context.Background(), x.intent, x.plan)
	if err != nil {
		t.Fatal(err)
	}
	defer real.Destroy()
	other := newStorageReviewHistory(t)
	foreign, err := other.s.PrepareProjectVariableWrite(context.Background(), other.intent, other.plan)
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Destroy()
	calls := 0
	var typedNil *preparedProjectVariableWrite
	beforeQueries, beforeChecks := x.store.queries, x.a.checks
	for _, raw := range []sc.PreparedProjectVariableWrite{nil, typedNil, &untrustedProjectVariablePrepared{&calls}, &wrappedProjectVariablePrepared{real}, foreign} {
		got, err := x.s.MatchProjectVariableIntentInTx(context.Background(), x.store.tx, raw)
		if err == nil || got.Validate() == nil {
			t.Fatal("foreign interface acquired real prepared authority")
		}
	}
	if calls != 0 || x.store.queries != beforeQueries || x.a.checks != beforeChecks {
		t.Fatal("forged/foreign handle reached methods or authority")
	}
	if _, err := real.Preparation(); err != nil {
		t.Fatal("rejection destroyed original live prepared")
	}
}

func TestIndependentStoragePrepareSecondNonceUnknown(t *testing.T) {
	s, intent, plan, store, _ := projectVariablePrepareFixture(t, false, false)
	// The first nonce is genuinely used for kind3 sealing. The second request,
	// for the new value, reaches the original reservation-Unknown path.
	s.state().nonces[2] = &nonceRange{next: 100, end: 100}
	unknown := &projectVariableNonceUnknownStore{projectAuditUnitStore: store}
	s.state().store = unknown
	prepared, err := s.PrepareProjectVariableWrite(context.Background(), intent, plan)
	var domain *Error
	var fault *f.Fault
	if prepared != nil || !errors.As(err, &domain) || domain.Code() != NonceReservationUnknown || !errors.As(err, &fault) || fault.CommitState != f.Unknown {
		t.Fatal("mid-prepare nonce Unknown published candidate or changed classification")
	}
	if unknown.reservations != 1 || s.state().nonces[2].next != 101 || s.state().nonces[2].end != 100 {
		t.Fatal("unknown range accepted, retried or consumed nonce rewound")
	}
	if err := intent.UseValue(func(value []byte) error {
		if string(value) != "opaque-value" {
			return errors.New("caller value changed")
		}
		return nil
	}); err != nil {
		t.Fatal("failed Prepare destroyed caller material", err)
	}
}
