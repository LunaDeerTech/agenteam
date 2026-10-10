package execution

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// These controls exercise the real transaction adapter and canonical decoder.
// They are not real PostgreSQL slot constraints or a Scheduler authorization.
type dispatchObservationStore struct {
	*launchStore
	row       []any
	readError error
	scanned   int
	onScan    func()
	query     string
	args      []any
}

func (s *dispatchObservationStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *dispatchObservationStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.query, s.args = query, args
	return dispatchObservationRow{store: s}
}

type dispatchObservationRow struct{ store *dispatchObservationStore }

func (r dispatchObservationRow) Scan(dest ...any) error {
	r.store.scanned++
	if r.store.onScan != nil {
		r.store.onScan()
	}
	return (launchScan{values: r.store.row, err: r.store.readError}).Scan(dest...)
}

func newDispatchObservationControl(t *testing.T) (*DispatchObservation, *dispatchObservationStore, c.LaunchRequest) {
	t.Helper()
	x := newLaunchControl(t)
	request := x.request.Clone()
	request.Lineage.DispatchID = newTestID[struct{}](t).String()
	request.Meta.IdempotencyKey = f.IdempotencyKey("scheduler_dispatch:" + request.Lineage.DispatchID)
	store := &dispatchObservationStore{launchStore: &launchStore{t: t, tx: f.NewTx(), live: true}}
	command, _ := request.Command()
	store.locks = append(dispatchObservationLocks(request.ProjectID, request.AgentID), commandLock(command))
	reader, err := NewDispatchObservation(store)
	if err != nil {
		t.Fatal(err)
	}
	return reader, store, request
}

func dispatchLookupKey(request c.LaunchRequest) c.LaunchLookupKey {
	return c.LaunchLookupKey{ProjectID: request.ProjectID, AgentID: request.AgentID, IdempotencyKey: request.Meta.IdempotencyKey}
}

func requireEmptyDispatchLookup(t *testing.T, got c.LaunchLookup, err error) {
	t.Helper()
	if err == nil || got.Found || got.Execution != nil || got.RequestDigest != "" {
		t.Fatal("failed observation returned an association", err)
	}
}

func requireEmptyAgentSlot(t *testing.T, got c.AgentSlotObservation, err error) {
	t.Helper()
	if err == nil || got.Occupied || got.ExecutionID != nil || got.Status != "" || got.CancelRequestedAt != nil {
		t.Fatal("failed observation returned slot facts", err)
	}
}

func TestExecutionDispatchObservationRequiresOriginalTransactionAndLocks(t *testing.T) {
	for _, name := range []string{"foreign-tx", "closed-tx", "missing-project", "schedule-sh", "missing-agent", "missing-command"} {
		t.Run(name, func(t *testing.T) {
			r, s, request := newDispatchObservationControl(t)
			tx := s.tx
			switch name {
			case "foreign-tx":
				tx = f.NewTx()
			case "closed-tx":
				s.live = false
			case "missing-project":
				s.locks = s.locks[1:]
			case "schedule-sh":
				s.locks[1].Mode = f.Shared
			case "missing-agent":
				s.locks = append(s.locks[:2], s.locks[3:]...)
			case "missing-command":
				s.locks = s.locks[:3]
			}
			digest, _ := request.Digest()
			got, err := r.LookupLaunchInTx(context.Background(), tx, dispatchLookupKey(request), digest, request.Lineage.DispatchID)
			requireEmptyDispatchLookup(t, got, err)
			requireCode(t, err, f.Forbidden)
			if name != "missing-command" {
				slot, err := r.AgentSlotInTx(context.Background(), tx, request.ProjectID, request.AgentID)
				requireEmptyAgentSlot(t, slot, err)
				requireCode(t, err, f.Forbidden)
			}
			if s.scanned != 0 {
				t.Fatal("read preceded transaction and complete held-lock checks")
			}
		})
	}
	r, s, request := newDispatchObservationControl(t)
	digest, _ := request.Digest()
	var unbound *DispatchObservation
	got, err := unbound.LookupLaunchInTx(context.Background(), s.tx, dispatchLookupKey(request), digest, request.Lineage.DispatchID)
	requireEmptyDispatchLookup(t, got, err)
	requireCode(t, err, f.DependencyUnbound)
	var nilStore *dispatchObservationStore
	if _, err := NewDispatchObservation(nilStore); err == nil {
		t.Fatal("typed nil Store accepted")
	}
	got, err = r.LookupLaunchInTx(context.Background(), s.tx, dispatchLookupKey(request), digest, "not-a-dispatch")
	requireEmptyDispatchLookup(t, got, err)
	requireCode(t, err, f.InvalidArgument)
	if s.scanned != 0 {
		t.Fatal("invalid binding reached SQL")
	}
}

func TestExecutionDispatchObservationBindsOriginalLaunchAndHistory(t *testing.T) {
	r, s, request := newDispatchObservationControl(t)
	digest, _ := request.Digest()
	key := dispatchLookupKey(request)
	for _, state := range []c.Status{c.Created, c.Waiting, c.Succeeded, c.Failed, c.Cancelled} {
		s.row = occupancyRow(t, request, state, true)
		got, err := r.LookupLaunchInTx(context.Background(), s.tx, key, digest, request.Lineage.DispatchID)
		if err != nil || !got.Found || got.Execution == nil || got.Execution.ID.String() != s.row[0] || got.Execution.Status != state || got.Execution.Trigger != request.Trigger || got.Execution.Purpose != request.Purpose || got.RequestDigest != digest {
			t.Fatal("original association or historical result lost", err)
		}
		if len(s.args) != 3 || s.args[0] != key.ProjectID.String() || s.args[1] != key.AgentID.String() || s.args[2] != key.IdempotencyKey.String() {
			t.Fatal("lookup did not use the original key")
		}
		// Observers own their projections, including the cancellation pointer.
		*got.Execution.CancelRequestedAt = f.Instant{}
		again, err := r.LookupLaunchInTx(context.Background(), s.tx, key, digest, request.Lineage.DispatchID)
		if err != nil || again.Execution.CancelRequestedAt == nil || again.Execution.CancelRequestedAt.Time().IsZero() {
			t.Fatal("caller changed a later observation", err)
		}
	}
	other := request.Clone()
	other.Purpose = "task/review"
	otherDigest, _ := other.Digest()
	got, err := r.LookupLaunchInTx(context.Background(), s.tx, key, otherDigest, request.Lineage.DispatchID)
	requireEmptyDispatchLookup(t, got, err)
	requireCode(t, err, f.IdempotencyKeyReused)
	got, err = r.LookupLaunchInTx(context.Background(), s.tx, key, digest, newTestID[struct{}](t).String())
	requireEmptyDispatchLookup(t, got, err)
	requireCode(t, err, f.ConfirmationStale)
	// Even a controlled SQL executor must not supply a canonical foreign key.
	other = request.Clone()
	other.ProjectID = newTestID[i.Project](t)
	s.row = occupancyRow(t, other, c.Created, false)
	got, err = r.LookupLaunchInTx(context.Background(), s.tx, key, digest, request.Lineage.DispatchID)
	requireEmptyDispatchLookup(t, got, err)
	requireCode(t, err, f.DependencyUnavailable)
	s.row = nil
	got, err = r.LookupLaunchInTx(context.Background(), s.tx, key, digest, request.Lineage.DispatchID)
	if err != nil || got.Found || got.Execution != nil || got.RequestDigest != "" || s.writes != 0 || !s.live {
		t.Fatal("not observed changed the caller transaction or created a fact", err)
	}
}

func TestExecutionDispatchObservationGlobalAgentSlot(t *testing.T) {
	r, s, request := newDispatchObservationControl(t)
	for _, trigger := range []string{"task", "meeting"} {
		current := request.Clone()
		if trigger == "meeting" {
			current.Trigger = c.Trigger{Kind: "meeting", MeetingID: newTestID[struct{}](t).String(), TurnID: newTestID[struct{}](t).String(), ContributionID: newTestID[struct{}](t).String(), ParticipantID: newTestID[struct{}](t).String()}
			current.Purpose, current.Lineage = "meeting/response", c.Lineage{}
		}
		for _, state := range []c.Status{c.Created, c.Preparing, c.Running, c.Waiting} {
			s.row = occupancyRow(t, current, state, true)
			got, err := r.AgentSlotInTx(context.Background(), s.tx, request.ProjectID, request.AgentID)
			if err != nil || !got.Occupied || got.ExecutionID == nil || got.ExecutionID.String() != s.row[0] || got.Status != state || got.CancelRequestedAt == nil {
				t.Fatal("active trigger or cancellation released the slot", trigger, state, err)
			}
			where := s.query[strings.Index(s.query, " WHERE "):]
			if where != " WHERE agent_id=$1 AND status IN ('created','preparing','running','waiting')" || len(s.args) != 1 || s.args[0] != request.AgentID.String() {
				t.Fatal("slot query narrowed global occupancy")
			}
		}
	}
	s.row = occupancyRow(t, request, c.Succeeded, false)
	got, err := r.AgentSlotInTx(context.Background(), s.tx, request.ProjectID, request.AgentID)
	requireEmptyAgentSlot(t, got, err)
	requireCode(t, err, f.DependencyUnavailable)
	s.row = nil
	got, err = r.AgentSlotInTx(context.Background(), s.tx, request.ProjectID, request.AgentID)
	if err != nil || got.Occupied || got.ExecutionID != nil || got.Status != "" || got.CancelRequestedAt != nil || s.writes != 0 {
		t.Fatal("free slot observation changed canonical facts", err)
	}
}

func TestExecutionDispatchObservationReadFailureAndOriginalCallJoin(t *testing.T) {
	r, s, request := newDispatchObservationControl(t)
	digest, _ := request.Digest()
	s.readError = errors.New("dispatch-storage-private-canary")
	got, err := r.LookupLaunchInTx(context.Background(), s.tx, dispatchLookupKey(request), digest, request.Lineage.DispatchID)
	requireEmptyDispatchLookup(t, got, err)
	requireCode(t, err, f.DependencyUnavailable)
	if strings.Contains(fmt.Sprintf("%+v", err), "private-canary") {
		t.Fatal("storage failure leaked through safe error")
	}
	slot, err := r.AgentSlotInTx(context.Background(), s.tx, request.ProjectID, request.AgentID)
	requireEmptyAgentSlot(t, slot, err)
	s.readError = nil
	s.row = occupancyRow(t, request, c.Created, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	s.onScan = func() { close(entered); <-release }
	type result struct {
		value c.AgentSlotObservation
		err   error
	}
	returned := make(chan result, 1)
	go func() {
		value, err := r.AgentSlotInTx(ctx, s.tx, request.ProjectID, request.AgentID)
		returned <- result{value, err}
	}()
	<-entered
	cancel()
	select {
	case <-returned:
		close(release)
		t.Fatal("adapter abandoned the original SQL call")
	default:
	}
	close(release)
	done := <-returned
	requireEmptyAgentSlot(t, done.value, done.err)
	if !errors.Is(done.err, context.Canceled) || s.writes != 0 || !s.live {
		t.Fatal("cancelled read did not join with zero result", done.err)
	}
}

type schedulerLookupGate struct {
	store *launchStore
	seen  int
}

func (g *schedulerLookupGate) RequireExecutionProjectInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID, intent i.AccessIntent) error {
	g.seen++
	if actor.Details().ServiceName != i.Scheduler || intent != i.Read {
		return fault(f.InvalidState)
	}
	if err := g.store.RequireHeldLocks(ctx, tx, dispatchObservationLocks(project, agent)); err != nil {
		return fault(f.InternalError)
	}
	// The controlled real service-access seam rejects permission. A registered
	// name and preheld locks cannot bypass the original authority callback.
	return fault(f.Forbidden)
}

func TestExecutionSchedulerLookupPreholdsScheduleWithoutGrant(t *testing.T) {
	x := newLaunchControl(t)
	dispatch := newTestID[struct{}](t).String()
	r, _ := i.RegisterService(i.Scheduler)
	scope, _ := i.InProject(x.request.ProjectID)
	actor, err := r.Actor(dispatch, scope)
	if err != nil {
		t.Fatal(err)
	}
	gate := &schedulerLookupGate{store: x.store}
	x.authority.state.services = gate
	digest, _ := x.request.Digest()
	_, err = x.service.Launch(context.Background(), actor, x.request)
	requireCode(t, err, f.Forbidden)
	_, err = x.service.LookupLaunch(context.Background(), actor, dispatchLookupKey(x.request), digest)
	requireCode(t, err, f.Forbidden)
	if gate.seen != 2 || x.discoveries != 0 || x.validations != 0 || x.captures != 0 || x.store.writes != 0 {
		t.Fatal("first lookup bypassed authority or proceeded to launch")
	}
	x.authority.state.services = nil
	_, err = x.service.LookupLaunch(context.Background(), actor, dispatchLookupKey(x.request), digest)
	requireCode(t, err, f.DependencyUnbound)
}
