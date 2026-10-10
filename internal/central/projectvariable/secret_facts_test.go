package projectvariable

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"strings"
)

type secretFactsStore struct {
	*secretAuthorityStore
	variable, history secretControlRow
}

func (s *secretFactsStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.secretAuthorityStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *secretFactsStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.Contains(q, "FROM agenteam_projectvariable.secret_history") {
		s.queries++
		return s.history
	}
	if strings.Contains(q, "FROM agenteam_projectvariable.variables WHERE project_id") {
		s.queries++
		return s.variable
	}
	return s.secretAuthorityStore.QueryRow(ctx, q, args...)
}

type secretFactsFixture struct {
	store       *secretFactsStore
	authority   *Authority
	plan        *secretMutationPlan
	preparation sc.ProjectVariablePreparationFields
	observation sc.ProjectVariableWriteObservation
	metadata    sc.ProjectVariableIntentMetadata
	summary     event.Summary
	ctx         context.Context
	deps        oc.Dependencies
	entry       ac.Entry
	key         ac.AppendKey
}

// This fixture supplies safe D04 projections and controlled repository rows.
// It exercises actual D10 proof checking, not actual Apply/Audit/SQL execution.
func newSecretFactsFixture(t *testing.T) *secretFactsFixture {
	t.Helper()
	r := secretStoredRecord(t, c.SecretCreateCommand, true, true)
	request := secretAuthorityRequest(t, r, 30)
	v := r.Receipt.Fields().Variable.Fields()
	o, _ := r.Observation.Result()
	plan := &secretMutationPlan{Request: request, After: r.Receipt.Fields().Variable, Fields: []string{"created"}, Operation: r.ID, History: testID[c.Operation](42), Event: *r.Receipt.Fields().EventID, At: r.CommittedAt}
	locks, err := sc.ProjectVariableWriteLocks(request, o.Ref)
	if err != nil {
		t.Fatal(err)
	}
	credential := o.Ref.Details().ID.String()
	version := int64(o.Version)
	fields, _ := canonical(plan.Fields)
	s := &secretFactsStore{secretAuthorityStore: &secretAuthorityStore{tx: f.NewTx(), locks: locks, record: r},
		variable: secretControlRow{values: []any{v.ID.String(), v.ProjectID.String(), v.Type, v.Name, v.Description, int64(v.Version), v.CreatedAt.Time(), v.UpdatedAt.Time(), nil, &credential, &version}},
		history:  secretControlRow{values: []any{plan.History.String(), v.ProjectID.String(), v.ID.String(), plan.Operation.String(), int64(v.Version), string(c.Created), fields, r.User.String(), v.UpdatedAt.Time(), plan.Event.String()}}}
	authority, err := NewAuthority(s)
	if err != nil {
		t.Fatal(err)
	}
	preparation := sc.ProjectVariablePreparationFields{Request: request, ReceiptID: o.ReceiptID, Ref: o.Ref}
	ctx, err := secretDiscoveryContext(context.Background(), authority, plan, preparation, locks, true)
	if err != nil {
		t.Fatal("discovery context", err)
	}
	payload, _ := canonical(secretPlanPayload(plan))
	summary := event.Summary{Producer: c.SecretVariableProducer, Header: secretPlanHeader(plan), PayloadDigest: digest(payload)}
	deps, err := authority.DiscoverAppend(ctx, request.Fields().Actor, summary)
	if err != nil {
		t.Fatal("actual discovery binding", err)
	}
	entry, key, err := secretPlanAudit(plan)
	if err != nil {
		t.Fatal("audit entry", err)
	}
	name, description := v.Name, v.Description
	return &secretFactsFixture{s, authority, plan, preparation, r.Observation, sc.ProjectVariableIntentMetadata{Request: request, Name: &name, Description: &description, ValuePresent: true}, summary, ctx, deps, entry, key}
}
func TestSecretOwnerDiscoveryCannotMintMutationOrCrossIssuerFacts(t *testing.T) {
	x := newSecretFactsFixture(t)
	actor := x.plan.Request.Fields().Actor
	if x.store.queries != 0 {
		t.Fatal("private discovery performed SQL")
	}
	if _, err := x.authority.DiscoverAppend(context.Background(), actor, x.summary); err == nil {
		t.Fatal("public event alone minted dependencies")
	}
	code(t, x.authority.CheckProjectAuditInTx(x.ctx, x.store.tx, x.entry, x.key), f.Forbidden)
	code(t, x.authority.ValidateAppendInTx(x.ctx, x.store.tx, actor, x.summary, x.deps, oc.NewFact), f.Forbidden)
	other, _ := NewAuthority(x.store)
	if _, err := other.DiscoverAppend(x.ctx, actor, x.summary); err == nil {
		t.Fatal("same Store different issuer accepted")
	}
	otherSession, _ := i.NewHuman(testID[i.User](3), testID[i.Session](31))
	if _, err := x.authority.DiscoverAppend(x.ctx, otherSession, x.summary); err == nil {
		t.Fatal("discovery actor Session lost")
	}
	if x.store.queries != 0 {
		t.Fatal("missing private proof reached repository")
	}
	for _, mutate := range []func(*event.Summary){func(v *event.Summary) { v.Header.EventID = testID[event.EventIdentity](55) }, func(v *event.Summary) { v.PayloadDigest = digest([]byte("other payload")) }, func(v *event.Summary) { v.Header.EventType = c.VariableChangedName }, func(v *event.Summary) { v.Producer = "work" }} {
		bad := x.summary
		mutate(&bad)
		if _, err := x.authority.DiscoverAppend(x.ctx, actor, bad); err == nil {
			t.Fatal("different event accepted")
		}
	}
}
func TestSecretOwnerAuditWitnessRequiresSameLiveTxFullActorLocksAndPostimage(t *testing.T) {
	for _, name := range []string{"valid", "foreign-tx", "retired-tx", "locks-lost", "wrong-session", "wrong-key", "wrong-canonical", "wrong-history", "wrong-d04-owner"} {
		t.Run(name, func(t *testing.T) {
			x := newSecretFactsFixture(t)
			observation := x.observation
			if name == "wrong-d04-owner" {
				o, _ := observation.Result()
				o.VariableID = testID[i.ProjectVariable](80)
				var err error
				observation, err = sc.NewProjectVariableWriteObservation(o)
				if err != nil {
					t.Fatal("valid wrong tuple required", err)
				}
			}
			ctx, err := secretMutationContext(x.ctx, x.authority, x.store.tx, observation, x.entry, x.key)
			if name == "wrong-d04-owner" {
				if err == nil {
					t.Fatal("wrong actual tuple minted mutation proof")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tx, entry, key := x.store.tx, x.entry, x.key
			switch name {
			case "foreign-tx":
				tx = f.NewTx()
			case "retired-tx":
				x.store.tx = f.Tx{}
			case "locks-lost":
				x.store.locks = x.store.locks[:1]
			case "wrong-session":
				v := entry.Fields()
				v.Actor, _ = i.NewHuman(testID[i.User](3), testID[i.Session](77))
				entry, err = ac.NewEntry(v)
				if err != nil {
					t.Fatal(err)
				}
			case "wrong-key":
				key, err = ac.NewAppendKey(ac.ProjectVariableProducer, digest([]byte("other cause")).String(), 0)
				if err != nil {
					t.Fatal(err)
				}
			case "wrong-canonical":
				x.store.variable.values[3] = "OTHER_NAME"
			case "wrong-history":
				x.store.history.values[3] = testID[c.Operation](79).String()
			}
			err = x.authority.CheckProjectAuditInTx(ctx, tx, entry, key)
			if name == "valid" {
				if err != nil || x.store.queries != 2 {
					t.Fatal("actual postimage/history not checked", err, x.store.queries)
				}
			} else if err == nil {
				t.Fatal("invalid same-Tx fact accepted")
			}
		})
	}
}
func TestSecretOwnerOutboxRequiresActualAuditReturnAndCompleteD04BoundRecord(t *testing.T) {
	for _, name := range []string{"valid", "no-audit", "missing-completed", "different-audit", "different-event", "wrong-d04-receipt", "history-corrupt"} {
		t.Run(name, func(t *testing.T) {
			x := newSecretFactsFixture(t)
			ctx, err := secretMutationContext(x.ctx, x.authority, x.store.tx, x.observation, x.entry, x.key)
			if err != nil {
				t.Fatal(err)
			}
			if name != "no-audit" {
				ctx, err = secretAuditCompletedContext(ctx, ac.AppendReceipt{AuditID: *x.store.record.Receipt.Fields().AuditID, CreatedAt: x.plan.At})
				if err != nil {
					t.Fatal(err)
				}
			}
			switch name {
			case "missing-completed":
				x.store.record = nil
			case "different-audit", "different-event":
				m := x.store.record.Receipt.Fields()
				if name == "different-audit" {
					id := testID[ac.Record](81)
					m.AuditID = &id
				} else {
					id := testID[event.EventIdentity](82)
					m.EventID = &id
				}
				x.store.record.Receipt, err = c.NewSecretVariableMutation(m)
				if err != nil {
					t.Fatal(err)
				}
			case "wrong-d04-receipt":
				o, _ := x.store.record.Observation.Result()
				o.ReceiptID = testID[sc.ProjectVariableReceipt](83)
				x.store.record.Observation, err = sc.NewProjectVariableWriteObservation(o)
				if err != nil {
					t.Fatal(err)
				}
			case "history-corrupt":
				x.store.history.values[7] = testID[i.User](84).String()
			}
			err = x.authority.ValidateAppendInTx(ctx, x.store.tx, x.plan.Request.Fields().Actor, x.summary, x.deps, oc.NewFact)
			if name == "valid" {
				if err != nil || x.store.queries != 3 {
					t.Fatal("completed+postimage+history not checked", err, x.store.queries)
				}
			} else if err == nil {
				t.Fatal("incomplete fact accepted")
			}
		})
	}
}
