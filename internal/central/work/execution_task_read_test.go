package work

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestExecutionTaskReaderOriginalBinding(t *testing.T) {
	pair, store := newEntryOwnership(t)
	reader := pair.TaskReader()
	port, err := NewExecutionTaskReader(reader)
	if err != nil {
		t.Fatal(err)
	}
	if port.original != reader || port.RequireStore(store) != nil {
		t.Fatal("replaced original Task reader")
	}
	if port.RequireStore(&entryRegistrationStore{}) == nil {
		t.Fatal("foreign Store accepted")
	}
	if _, err = NewExecutionTaskReader(nil); err == nil {
		t.Fatal("missing original accepted")
	}
	actor := pureActor(t, 2)
	p := pureID[i.Project](t, 3)
	task := pureID[struct{}](t, 4).String()
	locks, err := port.TaskReadLocks(actor, p, task)
	if err != nil || len(locks) != 4 {
		t.Fatal("incomplete preplanned lock union", err)
	}
	// A public adapter and a fabricated callback handle are not a live loan.
	if err = port.RequireTaskReadInTx(context.Background(), f.NewTx(), actor, p, task); err == nil {
		t.Fatal("bare Tx acquired Task scope")
	}
	if store.calls != 0 {
		t.Fatal("borrowed source opened a transaction")
	}
}
