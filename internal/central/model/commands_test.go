package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func TestModelCommandDigestStableUserButCompleteMeaning(t *testing.T) {
	actor := testActor(t)
	next, _ := id.NewHuman(func() id.UserID { v, _ := f.ParseID[id.User](actor.Details().UserID); return v }(), mustID[id.Session](t))
	input := mc.ProviderInput{Name: "configuration-canary", Protocol: mc.OpenAIChat, BaseURL: "https://provider.example/v1", Options: json.RawMessage(`{}`)}
	r := commandRequest{Meta: mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "same-business-key"}, Kind: "provider.create", ProviderInput: &input}
	one, e := commandSemantic(r)
	if e != nil {
		t.Fatal(e)
	}
	ci, e := commandIdentity(r.Meta, r.Kind)
	if e != nil {
		t.Fatal(e)
	}
	r.Meta.Actor = next
	r.ProviderInput.Options = json.RawMessage(" { } ")
	two, e := commandSemantic(r)
	if e != nil || two != one {
		t.Fatal("Session or object formatting changed semantics")
	}
	cj, _ := commandIdentity(r.Meta, r.Kind)
	if ci.Canonical() != cj.Canonical() {
		t.Fatal("same User/key command lock changed")
	}
	r.ProviderInput.Name = "different meaning"
	three, e := commandSemantic(r)
	if e != nil || three == one {
		t.Fatal("different request accepted as same semantic")
	}
	r.Meta.Actor = testActor(t)
	four, _ := commandSemantic(r)
	if four == three {
		t.Fatal("different User has same semantic")
	}
}

type scanFunc func(...any) error

func (fn scanFunc) Scan(args ...any) error { return fn(args...) }

type confirmationStore struct {
	Store
	record       *commandRecord
	txs, queries int
	held         bool
	err          error
	requested    f.TransactionCause
}

func (s *confirmationStore) WithinTx(ctx context.Context, c f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.txs++
	s.requested = c
	if s.err != nil {
		return f.NotCommittedResult(fault(f.DependencyUnavailable))
	}
	if e := fn(ctx, f.NewTx()); e != nil {
		var ff *f.Fault
		if !errors.As(e, &ff) {
			panic(e)
		}
		return f.NotCommittedResult(ff)
	}
	return f.CommittedResult()
}
func (s *confirmationStore) AcquireAll(_ context.Context, _ f.Tx, locks []f.LockRequest) error {
	if len(locks) != 2 || locks[0].Key.Canonical() != commandLock(s.requested.Details().Primary).Key.Canonical() || locks[0].Mode != f.Exclusive {
		panic("missing original writer lock")
	}
	s.held = true
	return nil
}
func (s *confirmationStore) RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error {
	if !s.held {
		panic("authorization before lock")
	}
	return nil
}
func (s *confirmationStore) InTx(f.Tx) (postgres.SQLExecutor, error) { return s, nil }
func (s *confirmationStore) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	s.queries++
	if !s.held || !strings.Contains(sql, "agenteam_model.commands") || len(args) != 1 || args[0] != s.requested.Details().Primary.Canonical() {
		panic("unexpected confirmation query")
	}
	return scanFunc(func(out ...any) error {
		if s.record == nil {
			return pgx.ErrNoRows
		}
		r := s.record
		*out[0].(*string) = r.ID
		*out[1].(*string) = r.Identity
		*out[2].(*string) = r.User
		*out[3].(*string) = r.Kind
		*out[4].(*string) = r.Resource
		*out[5].(*string) = r.Phase
		*out[6].(*f.Digest) = r.Semantic
		var err error
		*out[7].(*[]byte), err = json.Marshal(r.Plan)
		if err != nil {
			panic(err)
		}
		*out[8].(*[]byte), err = json.Marshal(r.Receipt)
		if err != nil {
			panic(err)
		}
		return nil
	})
}

func TestModelUnknownConfirmationNeverAdoptsDifferentSemanticReceipt(t *testing.T) {
	actor := testActor(t)
	meta := mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "private-unknown-key"}
	ci, _ := commandIdentity(meta, "provider.create")
	cause, _ := f.NewCommandsCause(ci)
	attempt := mustID[f.TransactionAttempt](t)
	original := f.UnknownResult(attempt, cause)
	expected := hash([]byte("original-A"))
	other := hash([]byte("different-B"))
	resource := mustID[mc.Provider](t).String()
	for _, scenario := range []string{"same", "different", "absent", "confirmation-error", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			store := &confirmationStore{}
			row := &commandRecord{ID: mustID[struct{}](t).String(), Identity: ci.Canonical(), User: actor.Details().UserID, Kind: "provider.create", Resource: resource, Phase: "committed", Semantic: expected, Receipt: &mc.CommandReceipt{Kind: "provider.create", ResourceID: resource, Version: 1}}
			row.Plan = mutationPlan{Semantic: expected, Receipt: *row.Receipt, Metadata: ac.ModelMetadataFields{Version: 1, ChangedFields: []string{"created"}}}
			store.record = row
			if scenario == "different" {
				row.Semantic = other
			}
			if scenario == "absent" {
				store.record = nil
			}
			if scenario == "confirmation-error" {
				store.err = errors.New("confirmation-failed")
			}
			sessionCalls := 0
			a, e := NewAuthority(store, Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error {
				sessionCalls++
				if scenario == "revoked" {
					return fault(f.SessionRevoked)
				}
				return nil
			}), System: systemFunc(func(_ context.Context, _ f.Tx, a id.Actor, i id.AccessIntent) (id.AccessGrant, error) {
				return validGrant(a, i)
			})})
			if e != nil {
				t.Fatal(e)
			}
			svc, e := New(store, a, testDependencies(t))
			if e != nil {
				t.Fatal(e)
			}
			receipt, e := svc.replay(context.Background(), actor, ci, expected, &original)
			if scenario == "same" {
				if e != nil || receipt != *row.Receipt {
					t.Fatal("same committed fact not confirmed", e)
				}
				return
			}
			if receipt != (mc.CommandReceipt{}) {
				t.Fatal("unconfirmed receipt returned")
			}
			if scenario == "different" {
				requireCode(t, e, f.IdempotencyKeyReused)
				return
			}
			var unknown *UnknownCommandError
			if !errors.As(e, &unknown) || unknown.AttemptID() != attempt || unknown.Cause().Details().Primary.Canonical() != ci.Canonical() {
				t.Fatal("original Unknown identity lost", e)
			}
			if scenario == "revoked" && (sessionCalls != 1 || store.queries != 0) {
				t.Fatal("receipt queried before current authorization")
			}
			if strings.Contains(fmt.Sprintf("%+v", unknown), "private-unknown-key") {
				t.Fatal("unknown formatted command key")
			}
		})
	}
}

func TestModelPreparationFailureRechecksConcurrentCanonicalReceipt(t *testing.T) {
	actor := testActor(t)
	meta := mc.CommandMeta{Actor: actor, Scope: id.SystemScope(), Key: "concurrent-same-key"}
	identity, _ := commandIdentity(meta, "model.delete")
	expected := hash([]byte("original-delete"))
	for _, scenario := range []string{"same", "different", "absent", "revoked", "original-unknown", "original-unknown-confirmation-failed"} {
		t.Run(scenario, func(t *testing.T) {
			store := &confirmationStore{}
			prior := &commandRecord{ID: mustID[struct{}](t).String(), Identity: identity.Canonical(), User: actor.Details().UserID, Kind: "model.delete", Resource: mustID[mc.Model](t).String(), Phase: "committed", Semantic: expected}
			prior.Receipt = &mc.CommandReceipt{Kind: prior.Kind, ResourceID: prior.Resource, Version: 2}
			prior.Plan = mutationPlan{Semantic: expected, Receipt: *prior.Receipt, Metadata: ac.ModelMetadataFields{Version: 2, ChangedFields: []string{"deleted"}}}
			store.record = prior
			if scenario == "different" {
				prior.Semantic = hash([]byte("other-delete"))
			}
			if scenario == "absent" || strings.HasPrefix(scenario, "original-unknown") {
				store.record = nil
			}
			if scenario == "original-unknown-confirmation-failed" {
				store.err = errors.New("confirmation unavailable")
			}
			auth, err := NewAuthority(store, Authorizations{Sessions: sessionFunc(func(context.Context, f.Tx, id.Actor) error {
				if scenario == "revoked" {
					return fault(f.SessionRevoked)
				}
				return nil
			}), System: systemFunc(func(_ context.Context, _ f.Tx, a id.Actor, intent id.AccessIntent) (id.AccessGrant, error) {
				return validGrant(a, intent)
			})})
			if err != nil {
				t.Fatal(err)
			}
			service, err := New(store, auth, testDependencies(t))
			if err != nil {
				t.Fatal(err)
			}
			var original error = fault(f.NotFound)
			if strings.HasPrefix(scenario, "original-unknown") {
				cause, _ := f.NewCommandsCause(identity)
				original = &UnknownCommandError{cause: cause, attempt: mustID[f.TransactionAttempt](t)}
			}
			receipt, err := service.recheckPreparation(context.Background(), actor, identity, expected, original)
			switch scenario {
			case "same":
				if err != nil || receipt != *prior.Receipt {
					t.Fatal("obsolete preparation failure won over receipt", err)
				}
			case "different":
				requireCode(t, err, f.IdempotencyKeyReused)
			case "revoked":
				requireCode(t, err, f.SessionRevoked)
				if store.queries != 0 {
					t.Fatal("revoked Session read receipt")
				}
			default:
				if err != original || receipt != (mc.CommandReceipt{}) {
					t.Fatal("absent canonical fact changed original error")
				}
			}
		})
	}
}
