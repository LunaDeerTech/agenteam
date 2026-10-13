package mount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The original adapters and Store are controlled protocol boundaries. These
// tests do not manufacture Agent private witnesses or a real PG ProcessGuard.
type mountProjectPureStopKey struct{}
type mountProjectPureCleanupKey struct{}
type mountProjectPureRows struct {
	change      ac.ProjectRetirementChange
	snapshot    projectMountSnapshot
	proof       []any
	unsupported bool
}

func (r *mountProjectPureRows) clone() *mountProjectPureRows {
	copy := *r
	copy.snapshot = projectMountSnapshot{Head: bytes.Clone(r.snapshot.Head), References: append([]json.RawMessage{}, r.snapshot.References...), Workspaces: append([]json.RawMessage{}, r.snapshot.Workspaces...), Definitions: append([]json.RawMessage{}, r.snapshot.Definitions...)}
	copy.proof = append([]any(nil), r.proof...)
	return &copy
}
func mountProjectPureJSON(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		panic(err)
	}
	return bytes.TrimSpace(b.Bytes())
}

type mountProjectPureStore struct {
	mu                          sync.Mutex
	rows                        map[i.ProjectID]*mountProjectPureRows
	nextError                   map[i.ProjectID]error
	active                      map[f.Tx]*mountProjectPureTx
	transactions, reads, writes atomic.Int32
	outcome                     func(context.Context, f.TransactionCause, f.CommitResult) (f.CommitResult, bool)
	tail                        func(context.Context, f.TransactionCause, f.CommitResult)
}
type mountProjectPureTx struct {
	postgres.SQLExecutor
	store *mountProjectPureStore
	rows  map[i.ProjectID]*mountProjectPureRows
	dirty map[i.ProjectID]bool
	locks []f.LockRequest
}

func (s *mountProjectPureStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions.Add(1)
	tx := f.NewTx()
	x := &mountProjectPureTx{store: s, rows: map[i.ProjectID]*mountProjectPureRows{}, dirty: map[i.ProjectID]bool{}}
	s.mu.Lock()
	for p, r := range s.rows {
		x.rows[p] = r.clone()
	}
	s.active[tx] = x
	s.mu.Unlock()
	err := fn(ctx, tx)
	r := f.CommittedResult()
	if err != nil {
		var fault *f.Fault
		if !errors.As(err, &fault) {
			fault = f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err)
		}
		r = f.NotCommittedResult(fault)
	}
	apply := r.State() == f.Committed
	if s.outcome != nil {
		r, apply = s.outcome(ctx, cause, r)
	}
	s.mu.Lock()
	delete(s.active, tx)
	if apply {
		for p := range x.dirty {
			s.rows[p] = x.rows[p]
		}
	}
	s.mu.Unlock()
	if s.tail != nil {
		s.tail(ctx, cause, r)
	}
	return r
}
func (s *mountProjectPureStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.active[tx]
	if x == nil {
		return nil, fault(f.Forbidden)
	}
	return x, nil
}
func (s *mountProjectPureStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.active[tx]
	if x == nil {
		return fault(f.Forbidden)
	}
	x.locks = append([]f.LockRequest(nil), locks...)
	return ctx.Err()
}
func (s *mountProjectPureStore) RequireHeldLocks(ctx context.Context, tx f.Tx, want []f.LockRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x := s.active[tx]
	if x == nil {
		return fault(f.Forbidden)
	}
	for _, w := range want {
		found := false
		for _, l := range x.locks {
			if l.Key.Canonical() == w.Key.Canonical() && (l.Mode == f.Exclusive || l.Mode == w.Mode) {
				found = true
			}
		}
		if !found {
			return fault(f.Forbidden)
		}
	}
	return ctx.Err()
}
func (x *mountProjectPureTx) project(raw any) (i.ProjectID, *mountProjectPureRows, error) {
	s, ok := raw.(string)
	if !ok {
		return i.ProjectID{}, nil, fault(f.InvalidArgument)
	}
	p, err := f.ParseID[i.Project](s)
	if err != nil || x.rows[p] == nil {
		return p, nil, fault(f.Forbidden)
	}
	return p, x.rows[p], nil
}
func (x *mountProjectPureTx) QueryRow(_ context.Context, q string, args ...any) postgres.Row {
	x.store.reads.Add(1)
	if len(args) == 0 {
		return controlRow{err: errors.New("unexpected unscoped Mount read")}
	}
	if strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads WHERE agent_id") {
		for _, r := range x.rows {
			if args[0] == r.change.Request.AgentID.String() {
				return controlRow{values: []any{r.snapshot.Head != nil || r.proof != nil || len(r.snapshot.References)+len(r.snapshot.Workspaces)+len(r.snapshot.Definitions) > 0}}
			}
		}
		return controlRow{err: fault(f.Forbidden)}
	}
	p, r, err := x.project(args[0])
	if err != nil {
		return controlRow{err: err}
	}
	switch {
	case strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM agenteam_tool.operations"):
		return controlRow{values: []any{r.unsupported}}
	case strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM agenteam_mount.execution_captures"):
		return controlRow{values: []any{false}}
	case strings.HasPrefix(q, "SELECT h.agent_id::text"):
		if err := x.store.nextError[p]; err != nil {
			return controlRow{err: err}
		}
		if r.snapshot.Head == nil || r.proof != nil {
			return controlRow{err: pgx.ErrNoRows}
		}
		return controlRow{values: []any{r.change.Request.AgentID.String()}}
	case strings.HasPrefix(q, "SELECT jsonb_build_object('head'"):
		if len(args) != 2 || args[1] != r.change.Request.AgentID.String() {
			return controlRow{err: fault(f.Forbidden)}
		}
		return controlRow{values: []any{mountProjectPureJSON(r.snapshot)}}
	case strings.HasPrefix(q, "SELECT project_id::text,operation_id::text"):
		if len(args) != 2 || args[1] != r.change.Request.AgentID.String() {
			return controlRow{err: fault(f.Forbidden)}
		}
		if r.proof == nil {
			return controlRow{err: pgx.ErrNoRows}
		}
		return controlRow{values: r.proof}
	case strings.HasPrefix(q, "SELECT EXISTS(SELECT 1 FROM agenteam_mount.mounts WHERE project_id"):
		remaining := len(r.snapshot.References)+len(r.snapshot.Workspaces)+len(r.snapshot.Definitions) > 0 || (r.snapshot.Head == nil) != (r.proof == nil)
		return controlRow{values: []any{remaining}}
	}
	return controlRow{err: errors.New("unexpected Mount lifecycle read")}
}
func (x *mountProjectPureTx) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	x.store.writes.Add(1)
	if len(args) == 0 {
		return pgconn.CommandTag{}, fault(f.Forbidden)
	}
	if strings.HasPrefix(q, "DELETE FROM agenteam_mount.agent_mount_heads h ") {
		if len(args) != 4 {
			return pgconn.CommandTag{}, fault(f.Forbidden)
		}
		p, r, err := x.project(args[1])
		if err != nil {
			return pgconn.CommandTag{}, err
		}
		var got, want any
		if args[0] != r.change.Request.AgentID.String() || args[2] != int64(r.change.Request.ExpectedVersion) || json.Unmarshal(args[3].([]byte), &got) != nil || json.Unmarshal(r.snapshot.Head, &want) != nil || !reflect.DeepEqual(got, want) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
		r.snapshot.Head = nil
		x.dirty[p] = true
		return pgconn.NewCommandTag("DELETE 1"), nil
	}
	p, r, err := x.project(args[0])
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	if strings.HasPrefix(q, "DELETE FROM agenteam_mount.project_agent_retirements ") {
		if len(args) != 10 || r.proof == nil {
			return pgconn.CommandTag{}, fault(f.ConfirmationStale)
		}
		want := []any{r.proof[0], r.proof[3], r.proof[1], r.proof[2], r.proof[4], string(r.proof[6].(f.Digest)), string(r.proof[8].(f.Digest)), r.proof[9]}
		if !reflect.DeepEqual(args[:8], want) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		}
		for n, pn := range []int{5, 7} {
			var got, stored any
			if json.Unmarshal(args[8+n].([]byte), &got) != nil || json.Unmarshal(r.proof[pn].([]byte), &stored) != nil || !reflect.DeepEqual(got, stored) {
				return pgconn.NewCommandTag("DELETE 0"), nil
			}
		}
		r.proof = nil
		x.dirty[p] = true
		return pgconn.NewCommandTag("DELETE 1"), nil
	}
	if strings.HasPrefix(q, "INSERT INTO agenteam_mount.project_agent_retirements(") {
		if len(args) != 10 || r.proof != nil || args[1] != r.change.Request.Cause.OperationID.String() || args[2] != int64(r.change.Request.Cause.ProjectVersion) || args[3] != r.change.Request.AgentID.String() || args[4] != int64(r.change.Request.ExpectedVersion) {
			return pgconn.CommandTag{}, fault(f.Forbidden)
		}
		var before ac.AgentConfig
		if json.Unmarshal(args[5].([]byte), &before) != nil || !reflect.DeepEqual(before.Fields(), r.change.Before.Fields()) {
			return pgconn.CommandTag{}, fault(f.ConfirmationStale)
		}
		var got, want any
		if json.Unmarshal(args[7].([]byte), &got) != nil || json.Unmarshal(mountProjectPureJSON(r.snapshot), &want) != nil || !reflect.DeepEqual(got, want) {
			return pgconn.CommandTag{}, fault(f.ConfirmationStale)
		}
		r.proof = []any{args[0], args[1], args[2], args[3], args[4], bytes.Clone(args[5].([]byte)), f.Digest(args[6].(string)), bytes.Clone(args[7].([]byte)), f.Digest(args[8].(string)), args[9]}
		x.dirty[p] = true
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	var rows *[]json.RawMessage
	switch {
	case strings.HasPrefix(q, "DELETE FROM agenteam_mount.agent_mount_refs "):
		rows = &r.snapshot.References
	case strings.HasPrefix(q, "DELETE FROM agenteam_mount.runner_workspaces "):
		rows = &r.snapshot.Workspaces
	case strings.HasPrefix(q, "DELETE FROM agenteam_mount.mounts "):
		rows = &r.snapshot.Definitions
	}
	if rows == nil || len(args) != 3 || args[1] != r.change.Request.AgentID.String() || !strings.Contains(q, "t.project_id=$1::uuid") || !strings.Contains(q, "t.agent_id=$2::uuid") || !strings.Contains(q, "to_jsonb(t)=b.value") {
		return pgconn.CommandTag{}, errors.New("unexpected/global Mount mutation")
	}
	var got, want any
	if json.Unmarshal(args[2].([]byte), &got) != nil || json.Unmarshal(mountProjectPureJSON(*rows), &want) != nil || !reflect.DeepEqual(got, want) {
		return pgconn.NewCommandTag("DELETE 0"), nil
	}
	n := len(*rows)
	*rows = []json.RawMessage{}
	x.dirty[p] = true
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", n)), nil
}

type mountProjectPure struct {
	store              *mountProjectPureStore
	pair               *ProjectLifecycleOwnership
	participant        pc.ProjectLifecycleParticipant
	process            oc.ProcessID
	agents, executions *mountProjectPureCalls
	change             ac.ProjectRetirementChange
	claims             map[i.ProjectID]ac.ProjectRetirementChange
}
type mountProjectPureCalls struct {
	v          *mountProjectPure
	mu         sync.Mutex
	active     map[i.ProjectID]int
	requests   atomic.Int32
	readConfig atomic.Int32
	hold       func(context.Context, i.ProjectID)
}

func (c *mountProjectPureCalls) RequestProjectStop(ctx context.Context, a i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	c.requests.Add(1)
	if ctx.Value(mountProjectPureStopKey{}) != c.v {
		return pc.StopReport{}, fault(f.Forbidden)
	}
	c.mu.Lock()
	c.active[scope.ProjectID]++
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.active[scope.ProjectID]--; c.mu.Unlock() }()
	if c.hold != nil {
		c.hold(ctx, scope.ProjectID)
	}
	if err := ctx.Err(); err != nil {
		return pc.StopReport{}, err
	}
	return pc.NewStopReport(pc.RunnerManagedParticipant, cause, scope, pc.StopDetails{State: pc.Stopped})
}
func (c *mountProjectPureCalls) InspectProjectStop(ctx context.Context, a i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return c.RequestProjectStop(ctx, a, cause, scope)
}
func (c *mountProjectPureCalls) ProjectControlJoined(p i.ProjectID) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.active[p] == 0
}
func (c *mountProjectPureCalls) Joined() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range c.active {
		if n != 0 {
			return false
		}
	}
	return true
}
func (c *mountProjectPureCalls) RequireProjectCallsReturnedInTx(ctx context.Context, tx f.Tx, a i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if err := c.v.RequireProjectCleanupInTx(ctx, tx, a, cause, scope); err != nil {
		return err
	}
	if !c.ProjectControlJoined(scope.ProjectID) {
		return fault(f.ResourceBusy)
	}
	return nil
}
func (c *mountProjectPureCalls) ReadRunnerManagedConfigurationInTx(ctx context.Context, tx f.Tx, a i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, id i.AgentID) (ac.AgentConfig, error) {
	c.readConfig.Add(1)
	if err := c.RequireProjectCallsReturnedInTx(ctx, tx, a, cause, scope); err != nil {
		return ac.AgentConfig{}, err
	}
	if err := c.v.store.RequireHeldLocks(ctx, tx, projectLifecycleLocks(scope.ProjectID, id)); err != nil {
		return ac.AgentConfig{}, err
	}
	x, err := c.v.store.InTx(tx)
	if err != nil {
		return ac.AgentConfig{}, err
	}
	r := x.(*mountProjectPureTx).rows[scope.ProjectID]
	if r == nil || r.change.Request.AgentID != id {
		return ac.AgentConfig{}, fault(f.Forbidden)
	}
	return r.change.Before, nil
}
func (v *mountProjectPure) tuple(a i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) bool {
	c, ok := v.claims[scope.ProjectID]
	return ok && a.Equal(c.Request.Actor) && cause == c.Request.Cause && scope.Kind == pc.ProjectScope
}
func (v *mountProjectPure) WithProjectStop(ctx context.Context, a i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, fn func(context.Context) error) error {
	if !v.tuple(a, cause, scope) {
		return fault(f.Forbidden)
	}
	return fn(context.WithValue(ctx, mountProjectPureStopKey{}, v))
}
func (v *mountProjectPure) RequireProjectStopInTx(ctx context.Context, tx f.Tx, a i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx.Value(mountProjectPureStopKey{}) != v || !v.tuple(a, cause, scope) {
		return fault(f.Forbidden)
	}
	return v.store.RequireHeldLocks(ctx, tx, projectLifecycleLocks(scope.ProjectID, i.AgentID{}))
}
func (v *mountProjectPure) RequireProjectCleanupInTx(ctx context.Context, tx f.Tx, a i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx.Value(mountProjectPureCleanupKey{}) != v || !v.tuple(a, cause, scope) {
		return fault(f.Forbidden)
	}
	return v.store.RequireHeldLocks(ctx, tx, projectLifecycleLocks(scope.ProjectID, i.AgentID{}))
}
func (v *mountProjectPure) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, p oc.ProcessID) error {
	if p != v.process {
		return fault(f.Forbidden)
	}
	_, err := v.store.InTx(tx)
	if err != nil {
		return err
	}
	return ctx.Err()
}
func (*mountProjectPure) ConfirmStopped(context.Context, oc.ProcessID) error {
	return fault(f.Forbidden)
}
func newMountProjectPure(t *testing.T) *mountProjectPure {
	t.Helper()
	change, snapshot := mountProjectPureSnapshot(t)
	v := &mountProjectPure{change: change, process: retirementRefID[oc.Process](t, 81), claims: map[i.ProjectID]ac.ProjectRetirementChange{change.Request.ProjectID: change}, store: &mountProjectPureStore{rows: map[i.ProjectID]*mountProjectPureRows{change.Request.ProjectID: {change: change, snapshot: snapshot}}, active: map[f.Tx]*mountProjectPureTx{}}}
	v.agents = &mountProjectPureCalls{v: v, active: map[i.ProjectID]int{}}
	v.executions = &mountProjectPureCalls{v: v, active: map[i.ProjectID]int{}}
	var err error
	v.pair, err = newProjectLifecycleOwnership(v.store, v.agents, v.executions, v, v.process, v)
	if err != nil {
		t.Fatal(err)
	}
	v.participant, err = NewProjectCleanupParticipant(v.pair, v)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (v *mountProjectPure) cleanup(ctx context.Context, cp *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	r := v.change.Request
	return v.participant.Cleanup(context.WithValue(ctx, mountProjectPureCleanupKey{}, v), r.Actor, r.Cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: r.ProjectID}, cp)
}
func mountProjectPureWait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("original Mount boundary was not reached")
	}
}

func mountProjectPureAdd(t *testing.T, v *mountProjectPure, n int) ac.ProjectRetirementChange {
	t.Helper()
	c, snapshot := mountProjectPureSnapshot(t)
	fields := c.Before.Fields()
	oldP, oldA := fields.Core.ProjectID, fields.Core.ID
	fields.Core.ProjectID = retirementRefID[i.Project](t, 1+n*100)
	fields.Core.ID = retirementRefID[i.Agent](t, 2+n*100)
	raw := mountProjectPureJSON(snapshot)
	for _, pair := range [][2]string{{oldP.String(), fields.Core.ProjectID.String()}, {oldA.String(), fields.Core.ID.String()}} {
		raw = bytes.ReplaceAll(raw, []byte(pair[0]), []byte(pair[1]))
	}
	for k := range 3 {
		id := retirementRefID[i.Mount](t, 31+n*100+k)
		raw = bytes.ReplaceAll(raw, []byte(retirementRefID[i.Mount](t, 31+k).String()), []byte(id.String()))
		raw = bytes.ReplaceAll(raw, []byte(retirementRefID[struct{}](t, 51+k).String()), []byte(retirementRefID[struct{}](t, 51+n*100+k).String()))
		if k < len(fields.AllowedMountIDs) {
			fields.AllowedMountIDs[k] = id
		}
	}
	fields.AllowedSecretVariableIDs = []i.ProjectVariableID{retirementRefID[i.ProjectVariable](t, 42+n*100)}
	var err error
	c.Before, err = ac.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	c.Request.ProjectID, c.Request.AgentID = fields.Core.ProjectID, fields.Core.ID
	c.Request.Cause.OperationID = retirementRefID[pc.Operation](t, 10+n*100)
	registration, _ := i.RegisterService(i.ProjectLifecycle)
	scope, _ := i.InProject(c.Request.ProjectID)
	c.Request.Actor, err = registration.Actor(c.Request.Cause.OperationID.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	v.claims[c.Request.ProjectID] = c
	v.store.rows[c.Request.ProjectID] = &mountProjectPureRows{change: c, snapshot: snapshot}
	return c
}

func TestMountProjectLifecycleBoundedControls(t *testing.T) {
	v := newMountProjectPure(t)
	requests := []ac.ProjectRetirementChange{v.change}
	for n := 1; n < 5; n++ {
		requests = append(requests, mountProjectPureAdd(t, v, n))
	}
	entered := make(chan i.ProjectID, 4)
	release, returned := make(chan struct{}), make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	v.agents.hold = func(_ context.Context, p i.ProjectID) {
		if p != requests[4].Request.ProjectID {
			entered <- p
			<-release
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	t.Cleanup(func() { open(); mountProjectPureWait(t, returned) })
	for _, c := range requests[:4] {
		wg.Add(1)
		go func(c ac.ProjectRetirementChange) {
			defer wg.Done()
			r := c.Request
			report, err := v.pair.RequestProjectStop(context.Background(), r.Actor, r.Cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: r.ProjectID})
			if err == nil && report.Details().State != pc.Stopped {
				err = errors.New("returned original stop remained pending")
			}
			errs <- err
		}(c)
	}
	go func() { wg.Wait(); close(returned) }()
	for range 4 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("independent Project control was serialized")
		}
	}
	r := requests[4].Request
	if _, err := v.pair.RequestProjectStop(context.Background(), r.Actor, r.Cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: r.ProjectID}); !retirementRefCode(err, f.ResourceBusy) {
		t.Fatal("fifth active control exceeded bound", err)
	}
	if v.agents.requests.Load() != 4 || v.pair.Joined() {
		t.Fatal("capacity rejection reached a business owner or retired existing calls")
	}
	open()
	mountProjectPureWait(t, returned)
	for range 4 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if !v.pair.Joined() {
		t.Fatal("returned bounded controls did not retire")
	}
}

func TestMountProjectLifecycleOriginalReturn(t *testing.T) {
	for _, phase := range []string{"agent", "execution", "store"} {
		t.Run(phase, func(t *testing.T) {
			v := newMountProjectPure(t)
			r := v.change.Request
			entered, release, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			open := func() { once.Do(func() { close(release) }) }
			hold := func(context.Context, i.ProjectID) { close(entered); <-release }
			if phase == "agent" {
				v.agents.hold = hold
			} else if phase == "execution" {
				v.executions.hold = hold
			} else {
				v.store.tail = func(context.Context, f.TransactionCause, f.CommitResult) { close(entered); <-release }
			}
			t.Cleanup(func() { open(); mountProjectPureWait(t, returned) })
			var err error
			go func() {
				defer close(returned)
				_, err = v.pair.RequestProjectStop(context.Background(), r.Actor, r.Cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: r.ProjectID})
			}()
			mountProjectPureWait(t, entered)
			if v.pair.Joined() {
				t.Fatal("held original owner was reported joined")
			}
			if _, e := v.pair.InspectProjectStop(context.Background(), r.Actor, r.Cause, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: r.ProjectID}); !retirementRefCode(e, f.ResourceBusy) {
				t.Fatal("same Project borrowed the active control", e)
			}
			v.pair.Stop()
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if e := v.pair.Drain(cancelled); !errors.Is(e, context.Canceled) || v.pair.Joined() {
				t.Fatal("cancellation substituted for actual original return", e)
			}
			select {
			case <-returned:
				t.Fatal("held original method returned early")
			default:
			}
			open()
			mountProjectPureWait(t, returned)
			if err == nil || !v.pair.Joined() {
				t.Fatal("original cancellation/return tail was lost", err)
			}
		})
	}
}
