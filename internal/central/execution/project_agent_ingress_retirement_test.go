package execution

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These are controlled live-Tx/Applied boundaries and original SQL returns.
// Rows are supplied independently, without interpreting SQL or manufacturing
// a private Project claim. Real migration predicates and routing belong to PG.
type ingressRetirementRead struct {
	args   []any
	values []any
	err    error
}

type ingressRetirementStore struct {
	*retirementGateStore
	reads            []ingressRetirementRead
	readAt, writes   int
	deleteArgs       []any
	deleteCount      int64
	deleteErr        error
	holdRead         int
	holdDelete       bool
	entered, release chan struct{}
}

func (s *ingressRetirementStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	x, err := s.retirementGateStore.InTx(tx)
	if x == nil || err != nil {
		return x, err
	}
	return s, nil
}
func (s *ingressRetirementStore) QueryRow(ctx context.Context, _ string, args ...any) postgres.Row {
	s.steps = append(s.steps, "read")
	n := s.readAt
	s.readAt++
	if ctx != s.ctx || n >= len(s.reads) || !reflect.DeepEqual(args, s.reads[n].args) {
		return ingressRetirementRow{err: fault(f.Forbidden)}
	}
	return ingressRetirementRow{store: s, index: n, read: s.reads[n]}
}
func (s *ingressRetirementStore) Exec(ctx context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	s.steps = append(s.steps, "delete")
	s.writes++
	if ctx != s.ctx || !reflect.DeepEqual(args, s.deleteArgs) {
		return pgconn.CommandTag{}, fault(f.Forbidden)
	}
	if s.holdDelete {
		close(s.entered)
		<-s.release
	}
	if s.deleteErr != nil {
		return pgconn.CommandTag{}, s.deleteErr
	}
	return pgconn.NewCommandTag("DELETE " + f.Version(s.deleteCount).String()), nil
}

type ingressRetirementRow struct {
	store *ingressRetirementStore
	index int
	read  ingressRetirementRead
	err   error
}

func (r ingressRetirementRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if r.store.holdRead == r.index {
		close(r.store.entered)
		<-r.store.release
	}
	if r.read.err != nil {
		return r.read.err
	}
	if len(dest) != len(r.read.values) {
		return fault(f.InternalError)
	}
	for n, value := range r.read.values {
		target := reflect.ValueOf(dest[n]).Elem()
		if value == nil {
			target.SetZero()
			continue
		}
		input := reflect.ValueOf(value)
		if !input.Type().AssignableTo(target.Type()) {
			return fault(f.InternalError)
		}
		target.Set(input)
	}
	return nil
}

type ingressRetirementOwner struct {
	store  *ingressRetirementStore
	change ac.ProjectRetirementChange
	err    error
	cancel context.CancelFunc
}

func (o *ingressRetirementOwner) RequireProjectRetirementPreparedInTx(context.Context, f.Tx, ac.ProjectRetirementChange) error {
	o.store.forbidden++
	return fault(f.Forbidden)
}
func (o *ingressRetirementOwner) RequireProjectRetirementAppliedInTx(ctx context.Context, tx f.Tx, change ac.ProjectRetirementChange) error {
	o.store.steps = append(o.store.steps, "applied")
	a, b := change.Request, o.change.Request
	if ctx != o.store.ctx || tx != o.store.tx || !a.Actor.Equal(b.Actor) || a.ProjectID != b.ProjectID || a.AgentID != b.AgentID || a.Cause != b.Cause || a.ExpectedVersion != b.ExpectedVersion || !reflect.DeepEqual(change.Before.Fields(), o.change.Before.Fields()) {
		return fault(f.Forbidden)
	}
	if o.cancel != nil {
		o.cancel()
	}
	return o.err
}

func newIngressRetirement(t *testing.T) (*ProjectAgentIngressRetirement, *ingressRetirementStore, *ingressRetirementOwner, ac.ProjectRetirementChange, []sc.ProjectAgentIngressRetirementEntry) {
	t.Helper()
	_, original, _, change := newRetirementGate(t)
	fields := change.Before.Fields()
	fields.Core.Version, change.Request.ExpectedVersion = 4, 4
	var err error
	change.Before, err = ac.NewAgentConfig(fields)
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]sc.ProjectAgentIngressRetirementEntry, 2)
	for n := range entries {
		id := retirementGateID[sc.AssignmentChange](t)
		sequence := f.Version(n + 2)
		entries[n] = sc.ProjectAgentIngressRetirementEntry{
			Change: sc.RuntimeAssignmentChange{ID: id, ProjectID: change.Request.ProjectID, AgentID: change.Request.AgentID,
				AssignmentSequence: sequence, AgentVersion: sequence, RecordedAt: fields.Core.UpdatedAt,
				Binding: sc.SkillBinding{SkillID: retirementGateID[pc.Skill](t), RevisionID: retirementGateID[sc.Revision](t), Revision: 1,
					AssignmentID: retirementGateID[sc.Assignment](t), AssignmentSequence: sequence, Name: "retirement-guide",
					Description: "Original immutable revision", PackageSHA256: c.TriggerInputDigest([]byte("package")), EntryPath: sc.EntryPath}},
			Receipt: sc.AssignmentIngressReceipt{ChangeID: id},
		}
		if err = entries[n].Validate(); err != nil {
			t.Fatal("invalid original ingress fixture", err)
		}
	}
	slices.SortFunc(entries, func(a, b sc.ProjectAgentIngressRetirementEntry) int {
		if a.Change.ID.String() < b.Change.ID.String() {
			return -1
		}
		return 1
	})
	s := &ingressRetirementStore{retirementGateStore: original, holdRead: -1, deleteCount: 2,
		deleteArgs: []any{change.Request.AgentID.String(), change.Request.ProjectID.String()}}
	s.reads = []ingressRetirementRead{
		{args: []any{change.Request.AgentID.String()}, values: []any{false}},
		{args: []any{change.Request.AgentID.String()}, values: []any{int64(2)}},
	}
	for _, entry := range entries {
		raw, err := json.Marshal(entry.Change)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = cursor.CanonicalJSON(raw)
		if err != nil {
			t.Fatal(err)
		}
		s.reads = append(s.reads, ingressRetirementRead{args: []any{entry.Change.ID.String()}, values: []any{
			entry.Change.ProjectID.String(), entry.Change.AgentID.String(), (*string)(nil), "no_active_execution", raw,
			string(c.TriggerInputDigest(raw)), entry.Change.RecordedAt.Time(),
		}})
	}
	o := &ingressRetirementOwner{store: s, change: change.Clone()}
	p, err := NewProjectAgentIngressRetirement(s, o)
	if err != nil || len(s.steps) != 0 {
		t.Fatal("constructor touched the original owner", err)
	}
	return p, s, o, change, entries
}

func TestProjectAgentIngressRetirementRequiresOriginalApplied(t *testing.T) {
	p, s, o, change, entries := newIngressRetirement(t)
	for _, pair := range []struct {
		store Store
		owner ac.ProjectRetirementOwner
	}{{nil, o}, {(*ingressRetirementStore)(nil), o}, {s, nil}, {s, (*ingressRetirementOwner)(nil)}} {
		if _, err := NewProjectAgentIngressRetirement(pair.store, pair.owner); err == nil {
			t.Fatal("unbound ingress owner accepted")
		}
	}
	var absent *ProjectAgentIngressRetirement
	retirementGateCode(t, absent.RetireProjectAgentIngressInTx(s.ctx, s.tx, change, entries), f.DependencyUnbound)
	retirementGateCode(t, p.RetireProjectAgentIngressInTx(nil, s.tx, change, entries), f.InvalidArgument)
	for _, name := range []string{"invalid-tx", "foreign-tx", "closed-tx", "nil-executor", "project-sh", "schedule-sh", "agent-sh", "expired-owner", "owner-cancelled"} {
		t.Run(name, func(t *testing.T) {
			p, s, o, change, entries := newIngressRetirement(t)
			tx, want := s.tx, f.Forbidden
			switch name {
			case "invalid-tx":
				tx, want = f.Tx{}, f.InvalidArgument
			case "foreign-tx":
				tx = f.NewTx()
			case "closed-tx":
				s.live = false
			case "nil-executor":
				s.nilExecutor, want = true, f.DependencyUnbound
			case "expired-owner":
				o.err = fault(f.Forbidden)
			case "owner-cancelled":
				var cancel context.CancelFunc
				s.ctx, cancel = context.WithCancel(s.ctx)
				defer cancel()
				o.cancel = cancel
			default:
				var key f.LockKey
				switch name {
				case "project-sh":
					key, _ = f.ProjectLock(change.Request.ProjectID.String())
				case "schedule-sh":
					key, _ = f.ProjectScheduleLock(change.Request.ProjectID.String())
				case "agent-sh":
					key, _ = f.AgentLock(change.Request.AgentID.String())
				}
				s.held[key.Canonical()] = f.Shared
			}
			err := p.RetireProjectAgentIngressInTx(s.ctx, tx, change, entries)
			if name == "owner-cancelled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("owner cancellation lost", err)
				}
			} else {
				retirementGateCode(t, err, want)
			}
			if s.readAt != 0 || s.writes != 0 || s.forbidden != 0 {
				t.Fatal("authorization rejection reached SQL or a new transaction")
			}
		})
	}
}

func TestProjectAgentIngressRetirementMatchesCompleteOriginalSet(t *testing.T) {
	for _, name := range []string{"exact-two", "empty", "any-execution", "extra-ingress", "missing-ingress", "orphan-empty", "foreign-project", "queued-row", "changed-bytes", "wrong-digest", "wrong-time", "original-scan-error", "delete-count", "original-delete-error"} {
		t.Run(name, func(t *testing.T) {
			p, s, _, change, entries := newIngressRetirement(t)
			want := f.ResourceBusy
			switch name {
			case "empty":
				entries, s.reads[1].values = nil, []any{int64(0)}
				s.reads = s.reads[:2]
			case "any-execution":
				s.reads[0].values[0] = true
			case "extra-ingress":
				s.reads[1].values[0] = int64(3)
			case "missing-ingress":
				s.reads[1].values[0] = int64(1)
			case "orphan-empty":
				entries = nil
			case "foreign-project":
				s.reads[2].values[0], want = retirementGateID[i.Project](t).String(), f.ConfirmationStale
			case "queued-row":
				execution := retirementGateID[i.Execution](t).String()
				s.reads[2].values[2], s.reads[2].values[3] = &execution, "queued"
			case "changed-bytes":
				s.reads[2].values[4], want = []byte(`{}`), f.ConfirmationStale
			case "wrong-digest":
				s.reads[2].values[5], want = string(c.TriggerInputDigest([]byte("different"))), f.ConfirmationStale
			case "wrong-time":
				s.reads[2].values[6], want = entries[0].Change.RecordedAt.Time().Add(time.Microsecond), f.ConfirmationStale
			case "original-scan-error":
				s.reads[2].err, want = errors.New("private original Scan error"), f.DependencyUnavailable
			case "delete-count":
				s.deleteCount = 1
			case "original-delete-error":
				s.deleteErr, want = errors.New("private original Delete error"), f.DependencyUnavailable
			}
			err := p.RetireProjectAgentIngressInTx(s.ctx, s.tx, change, entries)
			writes := 0
			switch name {
			case "exact-two", "empty":
				if err != nil || s.readAt != len(s.reads) || !reflect.DeepEqual(s.steps[:3], []string{"borrow", "locks", "applied"}) {
					t.Fatal("complete original set did not retire", err, s.steps)
				}
				if name == "exact-two" {
					writes = 1
				}
			default:
				retirementGateCode(t, err, want)
				if name == "delete-count" || name == "original-delete-error" {
					writes = 1
				}
			}
			if s.writes != writes || s.forbidden != 0 {
				t.Fatal("unexpected writes, preparation, or independently owned transaction")
			}
		})
	}
}

func TestProjectAgentIngressRetirementRejectsUnboundExpectations(t *testing.T) {
	for _, name := range []string{"duplicate", "unsorted", "foreign-project", "foreign-agent", "future-version", "wrong-receipt", "queued-receipt", "invalid-change"} {
		t.Run(name, func(t *testing.T) {
			p, s, _, change, entries := newIngressRetirement(t)
			switch name {
			case "duplicate":
				entries[1] = entries[0]
			case "unsorted":
				entries[0], entries[1] = entries[1], entries[0]
			case "foreign-project":
				entries[0].Change.ProjectID = retirementGateID[i.Project](t)
			case "foreign-agent":
				entries[0].Change.AgentID = retirementGateID[i.Agent](t)
			case "future-version":
				entries[0].Change.AgentVersion = change.Request.ExpectedVersion + 1
			case "wrong-receipt":
				entries[0].Receipt.ChangeID = entries[1].Change.ID
			case "queued-receipt":
				execution := retirementGateID[i.Execution](t)
				entries[0].Receipt.Queued, entries[0].Receipt.ExecutionID = true, &execution
			case "invalid-change":
				entries[0].Change.Binding.EntryPath = "different"
			}
			retirementGateCode(t, p.RetireProjectAgentIngressInTx(s.ctx, s.tx, change, entries), f.InvalidArgument)
			if s.readAt != 0 || s.writes != 0 || s.forbidden != 0 {
				t.Fatal("public expectations reached SQL without a complete matching set")
			}
		})
	}
}

func TestProjectAgentIngressRetirementRecoveryKeepsOwner(t *testing.T) {
	for _, name := range []string{"empty", "remaining", "expired-owner", "original-scan-error"} {
		t.Run(name, func(t *testing.T) {
			p, s, o, change, _ := newIngressRetirement(t)
			s.reads = s.reads[:1]
			switch name {
			case "remaining":
				s.reads[0].values[0] = true
			case "expired-owner":
				o.err = fault(f.Forbidden)
			case "original-scan-error":
				s.reads[0].err = errors.New("original confirmation Scan error")
			}
			err := p.RequireProjectAgentIngressRetiredInTx(s.ctx, s.tx, change)
			switch name {
			case "empty":
				if err != nil || s.readAt != 1 || !reflect.DeepEqual(s.steps[:3], []string{"borrow", "locks", "applied"}) {
					t.Fatal("original confirmation failed", err)
				}
			case "remaining":
				retirementGateCode(t, err, f.ResourceBusy)
			case "expired-owner":
				retirementGateCode(t, err, f.Forbidden)
				if s.readAt != 0 {
					t.Fatal("empty rows substituted for the original owner")
				}
			case "original-scan-error":
				retirementGateCode(t, err, f.DependencyUnavailable)
			}
			if s.writes != 0 || s.forbidden != 0 {
				t.Fatal("confirmation performed a mutation or opened a new transaction")
			}
		})
	}
}

func TestProjectAgentIngressRetirementWaitsForOriginalSQL(t *testing.T) {
	for _, name := range []string{"read", "delete", "confirmation"} {
		t.Run(name, func(t *testing.T) {
			p, s, _, change, entries := newIngressRetirement(t)
			ctx, cancel := context.WithCancel(s.ctx)
			defer cancel()
			s.ctx, s.entered, s.release = ctx, make(chan struct{}), make(chan struct{})
			switch name {
			case "read":
				s.holdRead = 2
			case "delete":
				s.holdDelete = true
			case "confirmation":
				s.holdRead, s.reads = 0, s.reads[:1]
			}
			done := make(chan struct{})
			var err error
			var once sync.Once
			release := func() { once.Do(func() { close(s.release) }) }
			go func() {
				defer close(done)
				if name == "confirmation" {
					err = p.RequireProjectAgentIngressRetiredInTx(ctx, s.tx, change)
				} else {
					err = p.RetireProjectAgentIngressInTx(ctx, s.tx, change, entries)
				}
			}()
			t.Cleanup(func() {
				release()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("original ingress retirement call did not return after release")
					<-done
				}
			})
			select {
			case <-s.entered:
			case <-time.After(time.Second):
				t.Fatal("original SQL barrier not reached")
			}
			cancel()
			select {
			case <-done:
				t.Fatal("cancellation substituted for the original SQL return")
			default:
			}
			release()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("original SQL did not return after release")
			}
			if !errors.Is(err, context.Canceled) || s.forbidden != 0 || name != "delete" && s.writes != 0 {
				t.Fatal("late original SQL return published success or leaked ownership", err)
			}
		})
	}
}
