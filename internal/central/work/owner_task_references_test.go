package work

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestOwnerTaskReferencesBorrowOriginalStoreWithoutOpeningReadSession(t *testing.T) {
	pair, store := newReaderEntryPair(t)
	port, err := NewOwnerTaskReferences(pair.TaskReader())
	if err != nil {
		t.Fatal(err)
	}
	if port.RequireStore(store) != nil || port.RequireStore(&entryRegistrationStore{}) == nil {
		t.Fatal("original store binding")
	}
	if _, err = NewOwnerTaskReferences(nil); err == nil {
		t.Fatal("unbound reader accepted")
	}
	actor, project, task := pureActor(t, 2), pureID[i.Project](t, 3), pureID[c.Task](t, 4)
	locks, err := port.TaskReferenceLocks(actor, project, task)
	if err != nil || len(locks) != 4 {
		t.Fatal("complete reference lock union", err)
	}
	for _, want := range []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), taskScheduleLock(project, f.Shared), taskLock(task.String(), f.Shared)} {
		found := false
		for _, got := range locks {
			if got.Mode == want.Mode && f.CompareLockKeys(got.Key, want.Key) == 0 {
				found = true
			}
		}
		if !found {
			t.Fatal("missing original identity lock")
		}
	}
	if err = port.RequireTaskReferenceInTx(context.Background(), f.NewTx(), actor, project, task); err == nil {
		t.Fatal("fabricated Tx accepted")
	}
	if store.calls != 0 {
		t.Fatal("borrowed identity check opened independent transaction")
	}
	if _, err = port.TaskReferenceLocks(i.Actor{}, project, task); err == nil {
		t.Fatal("invalid actor accepted")
	}
	if _, err = port.TaskReferenceLocks(actor, project, c.TaskID{}); err == nil {
		t.Fatal("invalid Task accepted")
	}
}
