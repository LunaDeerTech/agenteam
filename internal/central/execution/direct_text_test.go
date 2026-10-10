package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// These candidates use the existing controlled capture fixture. Encoding a
// candidate is not a sealed Snapshot, an admitted Loop or a database commit.
func directTextChangedJSON(t *testing.T, raw []byte, change func(map[string]json.RawMessage)) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	change(fields)
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = cursor.CanonicalJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestExecutionDirectTextCodecsKeepExactIdentityAndSafeOutput(t *testing.T) {
	facts, _ := runtimeModelCandidate(t)
	snapshot, round := facts.Snapshot, facts.Round
	decodedSnapshot, err := ec.DecodeDirectTextSnapshot(snapshot.CanonicalBytes())
	if err != nil || decodedSnapshot.Digest() != snapshot.Digest() || !bytes.Equal(decodedSnapshot.CanonicalBytes(), snapshot.CanonicalBytes()) || decodedSnapshot.Fields().Context.InputDigest() != snapshot.Fields().Context.InputDigest() {
		t.Fatal("Snapshot candidate lost fixed input", err)
	}
	decodedRound, err := ec.DecodeDirectTextRound(round.CanonicalBytes())
	if err != nil || decodedRound.Digest() != round.Digest() || !bytes.Equal(decodedRound.CanonicalBytes(), round.CanonicalBytes()) || decodedRound.Fields().Input.Digest != round.Fields().Input.Digest {
		t.Fatal("Round candidate lost exact input", err)
	}
	for _, pair := range []struct {
		raw    []byte
		digest f.Digest
	}{{snapshot.CanonicalBytes(), snapshot.Digest()}, {round.CanonicalBytes(), round.Digest()}} {
		canonical, err := cursor.CanonicalJSON(pair.raw)
		if err != nil || !bytes.Equal(canonical, pair.raw) || ec.TriggerInputDigest(pair.raw) != pair.digest {
			t.Fatal("candidate encoding is not canonical")
		}
	}
	fields := round.Fields()
	fields.Messages[0].Parts[0].Text.Text = "changed-round-private-canary"
	*fields.Input.ExecutionID = newTestID[i.Execution](t)
	if round.Fields().Messages[0].Parts[0].Text.Text == fields.Messages[0].Parts[0].Text.Text || *round.Fields().Input.ExecutionID == *fields.Input.ExecutionID {
		t.Fatal("round candidate exposed mutable fields")
	}
	rawSnapshot, rawRound := snapshot.CanonicalBytes(), round.CanonicalBytes()
	rawSnapshot[0], rawRound[0] = '!', '!'
	if ec.TriggerInputDigest(snapshot.CanonicalBytes()) != snapshot.Digest() || ec.TriggerInputDigest(round.CanonicalBytes()) != round.Digest() {
		t.Fatal("candidate exposed mutable canonical bytes")
	}
	for _, value := range []any{snapshot, round} {
		implicit, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, output := range []string{string(implicit), fmt.Sprintf("%+v %#v", value, value)} {
			if strings.Contains(output, "canary") || strings.Contains(output, "captured system") || strings.Contains(output, snapshot.Fields().Context.Input().Fields().Model.Snapshot.Endpoint) {
				t.Fatal("default output exposed captured content")
			}
		}
	}
	if snapshot.LogValue().String() != "execution_snapshot" || round.LogValue().String() != "execution_round_input" {
		t.Fatal("log output lost safe default")
	}
	var implicitSnapshot ec.DirectTextSnapshot
	var implicitRound ec.DirectTextRound
	if json.Unmarshal(snapshot.CanonicalBytes(), &implicitSnapshot) == nil || json.Unmarshal(round.CanonicalBytes(), &implicitRound) == nil {
		t.Fatal("implicit decoder bypassed explicit canonical codec")
	}
	for _, source := range [][]byte{snapshot.CanonicalBytes(), round.CanonicalBytes()} {
		for _, change := range []func(map[string]json.RawMessage){
			func(v map[string]json.RawMessage) { v["unexpected"] = json.RawMessage(`true`) },
			func(v map[string]json.RawMessage) { v["schema_version"] = json.RawMessage(`2`) },
		} {
			bad := directTextChangedJSON(t, source, change)
			if got, err := ec.DecodeDirectTextSnapshot(bad); err == nil || got.Validate() == nil {
				t.Fatal("invalid Snapshot envelope accepted")
			}
			if got, err := ec.DecodeDirectTextRound(bad); err == nil || got.Validate() == nil {
				t.Fatal("invalid Round envelope accepted")
			}
		}
	}
	if _, err = ec.DecodeDirectTextSnapshot(append([]byte(" "), snapshot.CanonicalBytes()...)); err == nil {
		t.Fatal("noncanonical Snapshot accepted")
	}
	if _, err = ec.DecodeDirectTextRound(append(round.CanonicalBytes(), []byte(" {}")...)); err == nil {
		t.Fatal("trailing Round envelope accepted")
	}
	badSnapshot := snapshot.Fields()
	badSnapshot.CreatedAt, _ = f.NewInstant(badSnapshot.Context.Input().Fields().CapturedAt.Time().Add(-time.Microsecond))
	if _, err = ec.NewDirectTextSnapshot(badSnapshot); err == nil {
		t.Fatal("Snapshot predates captured input")
	}
	for _, mutate := range []func(*ec.DirectTextRoundFields){
		func(v *ec.DirectTextRoundFields) { v.Input.RoundID = newTestID[ec.Round](t).String() },
		func(v *ec.DirectTextRoundFields) { v.Input.ExecutionID = nil },
		func(v *ec.DirectTextRoundFields) { v.Messages[0].Role = "assistant" },
	} {
		bad := round.Fields()
		mutate(&bad)
		if _, err = ec.NewDirectTextRound(bad); err == nil {
			t.Fatal("Round identity or role mismatch accepted")
		}
	}
}

func TestExecutionDirectTextLifecycleRequiresClosedTypedEvents(t *testing.T) {
	facts, _ := runtimeModelCandidate(t)
	catalog := event.NewCatalog()
	types, err := ec.RegisterDirectTextEvents(catalog)
	if err != nil || !types.Valid() || len(catalog.Schemas()) != 4 {
		t.Fatal("lifecycle catalog", err)
	}
	if err = catalog.Seal(); err != nil {
		t.Fatal(err)
	}
	request := facts.Snapshot.Fields().Context.Input().Fields().Request
	run := &directTextCall{request: request, snapshot: facts.Snapshot, round: facts.Round}
	state := &directTextState{deps: DirectTextDependencies{Lifecycle: types}}
	for _, entry := range []struct {
		status ec.Status
		reason string
	}{{ec.Running, "started"}, {ec.Succeeded, "completed"}, {ec.Cancelled, "cancelled"}, {ec.Failed, "model_failed"}, {ec.Failed, "incomplete_response"}, {ec.Failed, "runtime_failed"}} {
		value, err := state.lifecycleEvent(run, entry.status, entry.reason, 4, 1, facts.Snapshot.Fields().CreatedAt)
		if err != nil || value.Validate() != nil || !catalog.Owns(value) {
			t.Fatal("typed lifecycle event", err)
		}
		var payload ec.DirectTextLifecycle
		if err = json.Unmarshal(value.PayloadBytes(), &payload); err != nil || payload.Validate() != nil || payload.ExecutionID != request.ExecutionID || payload.Status != entry.status || payload.Reason != entry.reason {
			t.Fatal("lifecycle payload identity", err)
		}
		restored, err := catalog.Restore(ec.ExecutionProducer, value.Header(), value.PayloadBytes())
		if err != nil || !bytes.Equal(restored.PayloadBytes(), value.PayloadBytes()) {
			t.Fatal("closed event restoration", err)
		}
		bad := payload
		bad.Reason = "private-provider-error-canary"
		if _, err = types.New(value.Header(), bad); err == nil {
			t.Fatal("arbitrary error text entered lifecycle")
		}
		header := value.Header()
		header.AggregateID = newTestID[event.Aggregate](t)
		if _, err = types.New(header, payload); err == nil {
			t.Fatal("foreign aggregate admitted")
		}
		header = value.Header()
		header.AggregateVersion = nil
		if _, err = types.New(header, payload); err == nil {
			t.Fatal("missing canonical version admitted")
		}
		badJSON := directTextChangedJSON(t, value.PayloadBytes(), func(v map[string]json.RawMessage) { v["body"] = json.RawMessage(`"private-output-canary"`) })
		if _, err = catalog.Restore(ec.ExecutionProducer, value.Header(), badJSON); err == nil {
			t.Fatal("unknown lifecycle payload field admitted")
		}
	}
	if _, err = state.lifecycleEvent(run, ec.Preparing, "started", 4, 1, facts.Snapshot.Fields().CreatedAt); err == nil {
		t.Fatal("preparing mistaken for Started")
	}
}

func TestExecutionDirectTextRejectsUnboundAndForeignOwners(t *testing.T) {
	r := newLaunchControl(t)
	if driver, err := NewDirectTextDriver(r.store, r.authority, DirectTextDependencies{}); err == nil || driver != nil {
		t.Fatal("missing real dependencies admitted")
	}
	if driver, err := NewDirectTextDriver(nil, r.authority, DirectTextDependencies{}); err == nil || driver != nil {
		t.Fatal("missing store admitted")
	}
	if _, err := NewDirectTextEventAuthority(nil); err == nil {
		t.Fatal("missing event owner admitted")
	}
	events, err := NewDirectTextEventAuthority(r.authority)
	if err != nil {
		t.Fatal(err)
	}
	facts, request := runtimeModelCandidate(t)
	receipt := ec.DirectTextReceipt{ExecutionID: facts.Round.Fields().ExecutionID, SnapshotID: facts.Snapshot.Fields().ID, RoundID: facts.Round.Fields().ID, Status: ec.Running, Version: 3}
	// Even package-local fragments lack actual registry admission. These are
	// negative controls, never a fake successful runtime or terminal proof.
	unregistered := &directTextCall{request: facts.Snapshot.Fields().Context.Input().Fields().Request, owner: &directTextState{store: r.store, authority: r.authority, calls: map[i.ExecutionID]*directTextCall{}}}
	contexts := []context.Context{nil, context.Background(), context.WithValue(context.Background(), directTextContextKey{}, receipt), context.WithValue(context.Background(), directTextContextKey{}, unregistered)}
	for _, ctx := range contexts {
		if owner, err := r.authority.directTextOwner(ctx); err == nil || owner != nil {
			t.Fatal("public or unregistered owner admitted")
		}
		if value, err := r.authority.directTextModelPlanningScope(ctx, false); err == nil || value.Snapshot.Validate() == nil {
			t.Fatal("foreign context obtained Model planning facts")
		}
		if value, err := r.authority.directTextRetirementScope(ctx, f.NewTx()); err == nil || value.Snapshot.Validate() == nil {
			t.Fatal("foreign context obtained terminal retirement proof")
		}
		if _, err := events.DiscoverAppend(ctx, request.Actor, event.Summary{}); err == nil {
			t.Fatal("foreign context authorized lifecycle event")
		}
	}
	if r.store.writes != 0 {
		t.Fatal("rejected owner wrote state")
	}
	if _, err := ec.NewDirectTextSnapshot(ec.DirectTextSnapshotFields{}); err == nil {
		t.Fatal("empty Snapshot promoted to proof")
	}
	// A returned call with an uncertain outcome still owns its registry entry.
	// This isolates bookkeeping/Stop, not SQL observation or Loop success.
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	uncertain := f.NewFault(f.CommitUnknown, f.Unknown)
	state := &directTextState{calls: map[i.ExecutionID]*directTextCall{}, changed: make(chan struct{})}
	run := &directTextCall{owner: state, ctx: runCtx, cancel: cancel, request: ec.PreparationRequest{ExecutionID: receipt.ExecutionID}, unresolved: uncertain, invocation: &directTextInvocation{}}
	state.calls[receipt.ExecutionID] = run
	driver := &DirectTextDriver{state: state}
	state.returned(run)
	if state.calls[receipt.ExecutionID] != run || runCtx.Err() != nil || !run.returned || driver.Joined() {
		t.Fatal("returned Unknown retired its original owner")
	}
	driver.Stop()
	if !errors.Is(runCtx.Err(), context.Canceled) || driver.Joined() {
		t.Fatal("Stop did not cancel, or falsely joined retained Unknown")
	}
	if err := driver.Drain(context.Background()); !errors.Is(err, uncertain) || state.calls[receipt.ExecutionID] != run {
		t.Fatal("Drain lost original Unknown ownership", err)
	}
	// Model only the registry step following an independent exact observation.
	// Clearing this field is not evidence that such an observation occurred.
	state.mu.Lock()
	run.unresolved = nil
	state.mu.Unlock()
	state.returned(run)
	if !driver.Joined() || len(state.calls) != 0 || driver.Drain(context.Background()) != nil {
		t.Fatal("resolved and actually returned owner did not retire")
	}
}
