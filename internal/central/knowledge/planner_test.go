package knowledge

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type planStore struct {
	Store
	tx                      f.Tx
	entered, acquired, held int
	err                     error
}

func (s *planStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.entered++
	if tx != s.tx {
		return nil, errors.New("foreign transaction")
	}
	return nil, s.err
}
func (s *planStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	s.acquired++
	return nil
}
func (s *planStore) RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error {
	s.held++
	return nil
}

func TestMutationAcquisitionBindsIssuerAndActualTransaction(t *testing.T) {
	_, actor, q := queryFixture(t)
	request, err := kc.NewMoveMutation(actor, f.CommandMeta{RequestID: newID[f.Request](t), IdempotencyKey: "move"}, q.project, newID[kc.Document](t), kc.MoveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	locks, err := mutationLocks(request)
	if err != nil {
		t.Fatal(err)
	}
	issuer := kc.NewMutationIssuer()
	mapping, err := moveMapping(kc.MoveFacts{Current: kc.ScopeNode{ID: request.Details().DocumentID, ProjectID: q.project, ContentVersion: 1, Status: kc.Active}, TargetAncestors: []kc.ScopeNode{}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := kc.NewMutationPlan(issuer, kc.MutationPlanDetails{Request: request, DomainMapping: mapping, Locks: locks})
	if err != nil {
		t.Fatal(err)
	}
	tx := f.NewTx()
	store := &planStore{tx: tx}
	st := &serviceState{store: store, issuer: issuer, calls: make(map[*call]struct{}), changed: make(chan struct{})}
	service := &Service{data: func() *serviceState { return st }}
	foreign, err := kc.NewMutationPlan(kc.NewMutationIssuer(), plan.Details())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AcquireMutationInTx(context.Background(), tx, foreign, nil); err == nil || store.entered != 0 {
		t.Fatal("forged issuer reached Store")
	}
	if _, err = service.AcquireMutationInTx(context.Background(), f.NewTx(), plan, nil); err == nil || store.acquired != 0 {
		t.Fatal("foreign Tx acquired locks")
	}
	locked, err := service.AcquireMutationInTx(context.Background(), tx, plan, nil)
	if err != nil {
		t.Fatal(err)
	}
	if store.acquired != 1 || store.held != 1 || !locked.Matches(issuer, tx, plan, request) || locked.Matches(issuer, f.NewTx(), plan, request) {
		t.Fatal("acquisition lost exact binding")
	}
	changedActor, err := id.NewHuman(newID[id.User](t), newID[id.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	altered, err := kc.NewMoveMutation(changedActor, request.Details().Meta, q.project, request.Details().DocumentID, kc.MoveRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if locked.Matches(issuer, tx, plan, altered) {
		t.Fatal("different current caller accepted")
	}
}
