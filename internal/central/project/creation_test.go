package project

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func (s *authorityStore) AcquireAll(_ context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	if tx != s.tx {
		return errors.New("foreign tx")
	}
	s.locks = locks
	return nil
}
func (s *authorityStore) WithinTx(ctx context.Context, _ foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	if e := fn(ctx, s.tx); e != nil {
		var f *foundation.Fault
		if errors.As(e, &f) {
			return foundation.NotCommittedResult(f)
		}
		return foundation.NotCommittedResult(fault(foundation.InternalError))
	}
	return foundation.CommittedResult()
}
func TestUnboundCreateMakesNoReservationButReplaysReadyReceipt(t *testing.T) {
	actor := testActor(t)
	p := testProject(t)
	p.OwnerUserID, _ = foundation.ParseID[identity.User](actor.Details().UserID)
	request := c.CreateProjectRequest{ProjectID: p.ID, Name: p.Name, Description: p.Description}
	meta := foundation.CommandMeta{RequestID: testID[foundation.Request](t), IdempotencyKey: "first-key"}
	semantic, e := c.CreateDigest(actor, meta, request)
	if e != nil {
		t.Fatal(e)
	}
	order := []string{}
	st := &authorityStore{tx: foundation.NewTx(), order: &order}
	authority, e := NewAuthority(st, AuthorityDependencies{Sessions: sessionFunc(func(context.Context, foundation.Tx, identity.Actor) error { return nil })})
	if e != nil {
		t.Fatal(e)
	}
	state := &serviceState{store: st, deps: Dependencies{Authority: authority}, calls: map[*call]struct{}{}, changed: make(chan struct{}), slots: make(chan struct{}, 1)}
	service := &Service{data: func() *serviceState { return state }}
	st.row = func(string, ...any) postgres.Row { return rowFunc(func(...any) error { return pgx.ErrNoRows }) }
	_, e = service.CreateProject(context.Background(), actor, meta, request)
	hasCode(t, e, foundation.DependencyUnbound)
	if len(state.slots) != 0 {
		t.Fatal("unbound initializer consumed capacity")
	}
	creation := testID[c.Creation](t)
	skill := testID[c.Skill](t).String()
	revision := int64(1)
	ref, _ := json.Marshal(p)
	st.row = func(q string, _ ...any) postgres.Row {
		if strings.Contains(q, "FROM agenteam_project.deletion_receipts") {
			return rowFunc(func(...any) error { return pgx.ErrNoRows })
		}
		if strings.Contains(q, "FROM agenteam_project.projects") {
			return valuesRow(p.ID.String(), p.OwnerUserID.String(), p.Name, p.NormalizedName, p.Description, string(p.Lifecycle), int64(p.Version), nil, p.CreatedAt.Time(), p.UpdatedAt.Time(), nil, creation.String(), true, nil)
		}
		if strings.Contains(q, "FROM agenteam_project.creations") {
			return valuesRow(creation.String(), p.ID.String(), p.OwnerUserID.String(), string(meta.IdempotencyKey), semantic.String(), nil, nil, "completed", "init-key", &skill, &revision, nil, int64(3), p.CreatedAt.Time(), p.UpdatedAt.Time(), testID[struct{}](t).String(), []byte(`{}`), []byte(`{}`), ref)
		}
		t.Fatal("unexpected query", q)
		return nil
	}
	state.slots <- struct{}{} // capacity became full after this command completed
	result, e := service.CreateProject(context.Background(), actor, meta, request)
	if e != nil || result.State != c.CreationReady || result.Project.ID != p.ID {
		t.Fatal("historical replay checked binding/capacity", e)
	}
	if len(state.slots) != 1 {
		t.Fatal("replay borrowed/released another worker slot")
	}
	request.Description = "changed"
	_, e = service.CreateProject(context.Background(), actor, meta, request)
	hasCode(t, e, foundation.IdempotencyKeyReused)
}
func TestCreationCompletedResultRemainsHistoricalAndBodyGateWins(t *testing.T) {
	original := testProject(t)
	changed := original
	changed.Name = "Later"
	changed.NormalizedName = "later"
	changed.Version = 2
	id := testID[c.Creation](t)
	p := &projectRecord{ref: changed, creation: id, initialized: true}
	r := &creationRecord{owner: original.OwnerUserID, operation: c.CreationOperation{ID: id, ProjectID: original.ID, State: c.CreationCompleted}, result: &original}
	output, e := creationResult(r, p)
	if e != nil || output.Project.Name != "Demo" {
		t.Fatal("history silently recomputed", e)
	}
	p.ref.Lifecycle = c.Deleting
	_, e = creationResult(r, p)
	hasCode(t, e, foundation.ResourceDeleted)
}
