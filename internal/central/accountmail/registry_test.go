package accountmail

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type registryProcess struct{ id c.ProcessID }

func (p registryProcess) CurrentProcess() c.ProcessID { return p.id }
func (p registryProcess) ConfirmStopped(context.Context, c.ProcessID) error {
	return fail(foundation.ResourceBusy, nil)
}
func registryAttempt(t *testing.T, r *WorkRegistry) c.DeliveryAttempt {
	t.Helper()
	job, _ := foundation.NewID[c.MailJob]()
	intent, _ := foundation.NewID[c.DeliveryIntent]()
	attempt, _ := foundation.NewID[c.Attempt]()
	h := sha256.Sum256([]byte("binding"))
	a, e := c.NewDeliveryAttempt(c.NewDeliveryIssuer(), c.DeliveryAttemptDetails{JobID: job, IntentID: intent, AttemptID: attempt, ProcessID: r.CurrentProcess(), Kind: c.TestDelivery, Fence: 1, ConfigVersion: 1, Channel: c.SMTPChannel}, foundation.Digest("sha256:"+hex.EncodeToString(h[:])))
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func TestRegistryTwoSlotsStopAfterHandoffAndActualForceJoin(t *testing.T) {
	pid, _ := foundation.NewID[c.Process]()
	r, e := NewWorkRegistry(registryProcess{pid})
	if e != nil {
		t.Fatal(e)
	}
	first, e := r.begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	second, e := r.begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.begin(context.Background()); e == nil {
		t.Fatal("third work admitted")
	}
	a := registryAttempt(t, r)
	r.StopAdmission()
	if e = first.accept(a); e != nil {
		t.Fatal("late handoff lost", e)
	}
	if e = r.RequireActive(a); e != nil {
		t.Fatal("already admitted work discarded", e)
	}
	if _, e = r.begin(context.Background()); e == nil {
		t.Fatal("new work after stop")
	}
	forged, _ := c.NewDeliveryCompletion(c.NewDeliveryIssuer(), a, c.DeliveryOutcome{Result: c.DeliverySent, Reason: c.ReasonSent})
	if r.RequireJoined(a, forged) == nil {
		t.Fatal("caller manufactured join")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if e = r.Force(ctx); !errors.Is(e, context.DeadlineExceeded) || r.Joined() {
		t.Fatal("force request called join", e)
	}
	if first.ctx.Err() == nil || second.ctx.Err() == nil {
		t.Fatal("force did not cancel actual owners")
	}
	if r.RequireActive(a) == nil {
		t.Fatal("cancelled attempt reused")
	}
	completion, e := first.seal(c.DeliveryOutcome{Result: c.DeliveryCancelled, Reason: c.ReasonCancelled})
	if e != nil || r.RequireJoined(a, completion) != nil {
		t.Fatal(e)
	}
	if r.RequireJoined(registryAttempt(t, r), completion) == nil {
		t.Fatal("completion moved to other attempt")
	}
	first.finish()
	if r.Joined() {
		t.Fatal("second owner lost")
	}
	second.finish()
	if !r.Joined() {
		t.Fatal("actual join not published")
	}
	if e = r.Drain(context.Background()); e != nil {
		t.Fatal(e)
	}
}
