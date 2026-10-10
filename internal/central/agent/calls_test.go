package agent

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestAgentStopWaitsForOriginalCall(t *testing.T) {
	calls := newCalls()
	ctx, done, err := calls.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	calls.stop()
	if ctx.Err() == nil {
		t.Fatal("original call not canceled")
	}
	if calls.joined() {
		t.Fatal("cancellation treated as join")
	}
	deadline, cancel := context.WithCancel(context.Background())
	cancel()
	if err := calls.drain(deadline); !errors.Is(err, context.Canceled) {
		t.Fatal("held call drain did not preserve deadline")
	}
	if calls.joined() {
		t.Fatal("failed drain retired original call")
	}
	_, _, err = calls.begin(context.Background())
	var problem *f.Fault
	if !errors.As(err, &problem) || problem.Code != f.ShuttingDown {
		t.Fatal("post-stop admission succeeded")
	}
	done()
	done()
	if err := calls.drain(context.Background()); err != nil || !calls.joined() {
		t.Fatal("actual return did not join", err)
	}
	calls.stop()
}
