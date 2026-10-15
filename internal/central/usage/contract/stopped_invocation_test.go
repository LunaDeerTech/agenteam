package contract

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func stoppedInvocation(t *testing.T, dispatch Dispatch) StoppedInvocationRequest {
	t.Helper()
	identity := invocationRequest(t).Identity
	v := invocation(t)
	a := identity.Attempt
	v.ID, v.CallID, v.AttemptIndex, v.ProcessID, v.Fence = a.InvocationID, a.CallID, a.AttemptIndex, a.ProcessID, a.Fence
	v.Consumer, v.SnapshotID, v.Dispatch, v.Final = identity.Consumer.Clone(), identity.SnapshotID, dispatch, nil
	if dispatch != Sent {
		v.DispatchedAt = nil
	}
	r := StoppedInvocationRequest{Identity: identity, BeforeSequence: 3, Before: v, AfterSequence: 4, After: v.Clone()}
	terminal := Unknown
	switch dispatch {
	case Reserved:
		r.After.Dispatch, terminal, r.AfterSequence = NotSent, Cancelled, 5
	case Authorized:
		r.After.Dispatch, r.AfterSequence = DispatchUnknown, 5
	case NotSent:
		terminal = Cancelled
	}
	r.After.Final = &Final{Status: terminal, Version: 1, FinishedAt: v.StartedAt}
	return r
}

func TestStoppedInvocationHonestTransitions(t *testing.T) {
	for _, state := range []Dispatch{Reserved, Authorized, Sent, NotSent, DispatchUnknown} {
		t.Run(string(state), func(t *testing.T) {
			r := stoppedInvocation(t, state)
			must(t, r.Validate())
			binding, err := StoppedInvocationBinding(r)
			must(t, err)
			for _, mutate := range []func(*StoppedInvocationRequest){
				func(v *StoppedInvocationRequest) { v.AfterSequence++ },
				func(v *StoppedInvocationRequest) { v.BeforeSequence = f.Sequence(math.MaxInt64) },
				func(v *StoppedInvocationRequest) { v.After.Final.Status = Succeeded },
				func(v *StoppedInvocationRequest) { v.After.ProviderRequestID = "invented" },
				func(v *StoppedInvocationRequest) {
					v.After.Usage = mc.Usage{Source: mc.ProviderUsage, InputTokens: ptr(mc.TokenCount(1))}
				},
				func(v *StoppedInvocationRequest) { v.Identity.Attempt.Fence++ },
			} {
				bad := r.Clone()
				mutate(&bad)
				reject(t, bad.Validate())
			}
			changed := r.Clone()
			changed.Identity.Input.SchemaVersion++
			other, err := StoppedInvocationBinding(changed)
			if err != nil || binding == other {
				t.Fatal("original input missing from recovery binding", err)
			}
			raw, err := json.Marshal(r)
			must(t, err)
			if strings.Contains(string(raw)+fmt.Sprintf("%#v", r), string(r.Identity.Input.Digest)) || json.Unmarshal(raw, &changed) == nil {
				t.Fatal("recovery request exposed private input or became wire authority")
			}
		})
	}
}

func TestStoppedInvocationPreservesKnownFinal(t *testing.T) {
	r := stoppedInvocation(t, Sent)
	r.Before, r.BeforeSequence = r.After.Clone(), r.AfterSequence
	must(t, r.Validate())
	r.After.Final.Status = Failed
	reject(t, r.Validate())
	r.After = r.Before.Clone()
	r.AfterSequence++
	reject(t, r.Validate())
}
