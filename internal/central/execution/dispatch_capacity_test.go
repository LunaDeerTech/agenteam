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

type dispatchCapacityStore struct {
	*launchStore
	reads     int
	onRead    func()
	gateSizes []int
}

func (s *dispatchCapacityStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *dispatchCapacityStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.gateSizes = append(s.gateSizes, len(locks))
	return s.launchStore.RequireHeldLocks(ctx, tx, locks)
}

func (s *dispatchCapacityStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	s.reads++
	if !strings.HasSuffix(query, "WHERE id=$1") || len(args) != 1 {
		s.t.Fatal("capacity did not read the exact canonical Execution")
	}
	return dispatchCapacityRow{s.launchStore.QueryRow(ctx, query, args...), s.onRead}
}

type dispatchCapacityRow struct {
	row   postgres.Row
	after func()
}

func (r dispatchCapacityRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	if r.after != nil {
		r.after()
	}
	return err
}

// These controls exercise real canonical decoding under controlled Store/rows.
// They are not PostgreSQL constraints, Scheduler association authorization or
// proof that unimplemented waiting/terminal lifecycle writers are connected.
func newDispatchCapacityControl(t *testing.T) (*DispatchObservation, *dispatchCapacityStore, c.LaunchRequest) {
	t.Helper()
	x := newLaunchControl(t)
	request := x.request.Clone()
	store := &dispatchCapacityStore{launchStore: &launchStore{t: t, tx: f.NewTx(), live: true, rows: map[string][]any{}}}
	schedule, _ := f.ProjectScheduleLock(request.ProjectID.String())
	store.locks = []f.LockRequest{projectLock(request.ProjectID), {Key: schedule, Mode: f.Exclusive}}
	r, err := NewDispatchObservation(store)
	if err != nil {
		t.Fatal(err)
	}
	return r, store, request
}

func addDispatchCapacityRow(t *testing.T, s *dispatchCapacityStore, template c.LaunchRequest, state c.Status) c.AssociatedDispatch {
	t.Helper()
	request := template.Clone()
	request.AgentID = newTestID[i.Agent](t)
	request.Lineage.DispatchID = newTestID[struct{}](t).String()
	request.Meta.IdempotencyKey = f.IdempotencyKey("scheduler_dispatch:" + request.Lineage.DispatchID)
	row := occupancyRow(t, request, state, true)
	id, _ := f.ParseID[i.Execution](row[0].(string))
	digest, _ := request.Digest()
	s.rows[id.String()] = row
	return c.AssociatedDispatch{ExecutionID: id, AgentID: request.AgentID, Key: request.Meta.IdempotencyKey, Digest: digest, DispatchID: request.Lineage.DispatchID}
}

func TestExecutionDispatchCapacityBoundsAndOriginalTransaction(t *testing.T) {
	for _, mode := range []string{"foreign", "retired", "project-missing", "schedule-sh", "schedule-missing"} {
		t.Run(mode, func(t *testing.T) {
			r, s, request := newDispatchCapacityControl(t)
			tx := s.tx
			switch mode {
			case "foreign":
				tx = f.NewTx()
			case "retired":
				s.live = false
			case "project-missing":
				s.locks = s.locks[1:]
			case "schedule-sh":
				s.locks[1].Mode = f.Shared
			case "schedule-missing":
				s.locks = s.locks[:1]
			}
			count, err := r.CountAssociatedInTx(context.Background(), tx, request.ProjectID, []c.AssociatedDispatch{})
			requireCode(t, err, f.Forbidden)
			if count != 0 || s.reads != 0 {
				t.Fatal("invalid transaction/locks returned capacity")
			}
		})
	}
	r, s, request := newDispatchCapacityControl(t)
	for _, batch := range [][]c.AssociatedDispatch{nil, make([]c.AssociatedDispatch, c.MaxDispatchCapacityBatch+1)} {
		count, err := r.CountAssociatedInTx(context.Background(), s.tx, request.ProjectID, batch)
		if err == nil || count != 0 || s.reads != 0 {
			t.Fatal("nil or unbounded batch reached SQL")
		}
	}
	count, err := r.CountAssociatedInTx(context.Background(), s.tx, request.ProjectID, []c.AssociatedDispatch{})
	if err != nil || count != 0 || len(s.gateSizes) != 1 || s.gateSizes[0] != 2 {
		t.Fatal("empty batch bypassed original transaction gates", err)
	}
	item := addDispatchCapacityRow(t, s, request, c.Created)
	second := addDispatchCapacityRow(t, s, request, c.Created)
	second.DispatchID = item.DispatchID
	for _, batch := range [][]c.AssociatedDispatch{{item, item}, {item, second}} {
		count, err = r.CountAssociatedInTx(context.Background(), s.tx, request.ProjectID, batch)
		requireCode(t, err, f.InvalidArgument)
		if count != 0 || s.reads != 0 {
			t.Fatal("duplicate history was counted")
		}
	}
}

func TestExecutionDispatchCapacityChecksAllHistoryWithConstantGates(t *testing.T) {
	r, s, request := newDispatchCapacityControl(t)
	states := []c.Status{c.Created, c.Preparing, c.Running, c.Waiting, c.Succeeded, c.Failed, c.Cancelled}
	var items []c.AssociatedDispatch
	var expected int64
	// More than the global 512-lock cap, with distinct Agents/commands. Every
	// historical tuple is still verified, while each batch needs only two gates.
	for n := 0; n < 513; n++ {
		state := states[n%len(states)]
		items = append(items, addDispatchCapacityRow(t, s, request, state))
		if state == c.Created || state == c.Preparing || state == c.Running {
			expected++
		}
	}
	var total int64
	for start := 0; start < len(items); start += c.MaxDispatchCapacityBatch {
		end := min(start+c.MaxDispatchCapacityBatch, len(items))
		count, err := r.CountAssociatedInTx(context.Background(), s.tx, request.ProjectID, items[start:end])
		if err != nil {
			t.Fatal(err)
		}
		total += count
	}
	if total != expected || s.reads != len(items) || len(s.gateSizes) != 5 || !s.live || s.writes != 0 {
		t.Fatal("history was truncated, miscounted or moved to another transaction")
	}
	for _, width := range s.gateSizes {
		if width != 2 {
			t.Fatal("history reintroduced per-Agent/Command lock growth")
		}
	}
}

func TestExecutionDispatchCapacityRejectsAnyAssociationMismatch(t *testing.T) {
	for _, field := range []string{"missing", "project", "agent", "key", "digest", "dispatch", "meeting"} {
		t.Run(field, func(t *testing.T) {
			r, s, request := newDispatchCapacityControl(t)
			first := addDispatchCapacityRow(t, s, request, c.Created)
			second := addDispatchCapacityRow(t, s, request, c.Succeeded)
			project := request.ProjectID
			switch field {
			case "missing":
				delete(s.rows, second.ExecutionID.String())
			case "project":
				other := request.Clone()
				other.ProjectID = newTestID[i.Project](t)
				second = addDispatchCapacityRow(t, s, other, c.Succeeded)
			case "agent":
				second.AgentID = newTestID[i.Agent](t)
			case "key":
				second.Key = "changed-key"
			case "digest":
				second.Digest = f.Digest("sha256:" + strings.Repeat("a", 64))
			case "dispatch":
				second.DispatchID = newTestID[struct{}](t).String()
			case "meeting":
				meeting := request.Clone()
				meeting.AgentID = second.AgentID
				meeting.Meta.IdempotencyKey = second.Key
				meeting.Trigger = c.Trigger{Kind: "meeting", MeetingID: newTestID[struct{}](t).String(), TurnID: newTestID[struct{}](t).String(), ContributionID: newTestID[struct{}](t).String(), ParticipantID: newTestID[struct{}](t).String()}
				meeting.Purpose, meeting.Lineage = "meeting/response", c.Lineage{}
				row := occupancyRow(t, meeting, c.Succeeded, false)
				second.ExecutionID, _ = f.ParseID[i.Execution](row[0].(string))
				second.Digest, _ = meeting.Digest()
				s.rows[second.ExecutionID.String()] = row
			}
			count, err := r.CountAssociatedInTx(context.Background(), s.tx, project, []c.AssociatedDispatch{first, second})
			if err == nil || count != 0 || s.reads != 2 {
				t.Fatal("a broken terminal association produced partial capacity", field, err)
			}
		})
	}
}

func TestExecutionDispatchCapacityFailureCancellationAndOwnedBatch(t *testing.T) {
	r, s, request := newDispatchCapacityControl(t)
	items := []c.AssociatedDispatch{addDispatchCapacityRow(t, s, request, c.Created), addDispatchCapacityRow(t, s, request, c.Preparing)}
	s.readErr = errors.New("capacity-private-storage-canary")
	count, err := r.CountAssociatedInTx(context.Background(), s.tx, request.ProjectID, items)
	if err == nil || count != 0 || strings.Contains(fmt.Sprintf("%+v", err), "private-storage-canary") {
		t.Fatal("SQL failure became empty capacity or leaked material")
	}
	s.readErr = nil
	s.onRead = func() { items[1].Key = "caller-mutated-after-entry" }
	count, err = r.CountAssociatedInTx(context.Background(), s.tx, request.ProjectID, items)
	if err != nil || count != 2 {
		t.Fatal("caller changed the owned input batch", err)
	}
	s.onRead = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	s.onRead = func() { close(entered); <-release }
	type result struct {
		count int64
		err   error
	}
	returned := make(chan result, 1)
	go func() {
		count, err := r.CountAssociatedInTx(ctx, s.tx, request.ProjectID, items[:1])
		returned <- result{count, err}
	}()
	<-entered
	cancel()
	select {
	case <-returned:
		close(release)
		t.Fatal("cancellation abandoned original SQL")
	default:
	}
	close(release)
	done := <-returned
	if !errors.Is(done.err, context.Canceled) || done.count != 0 || !s.live || s.writes != 0 {
		t.Fatal("cancelled capacity read did not join with zero result", done.err)
	}
}
