package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

func sid[K any](t *testing.T) f.ID[K] {
	t.Helper()
	v, e := f.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func actor(t *testing.T) id.Actor {
	t.Helper()
	v, e := id.NewHuman(sid[id.User](t), sid[id.Session](t))
	if e != nil {
		t.Fatal(e)
	}
	return v
}

type readerStore struct {
	mu            sync.Mutex
	entered       chan struct{}
	release       chan struct{}
	tx            f.Tx
	held          []f.LockRequest
	calls         int
	executorCalls int
}

func (s *readerStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.calls++
	s.tx = f.NewTx()
	tx := s.tx
	s.mu.Unlock()
	if cause.Validate() != nil {
		return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted))
	}
	if s.entered != nil {
		close(s.entered)
		<-s.release
	}
	if e := fn(ctx, tx); e != nil {
		var ff *f.Fault
		if errors.As(e, &ff) {
			return f.NotCommittedResult(ff)
		}
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(e))
	}
	return f.CommittedResult()
}
func (s *readerStore) AcquireAll(_ context.Context, tx f.Tx, held []f.LockRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tx != s.tx {
		return errors.New("foreign tx")
	}
	s.held = append([]f.LockRequest(nil), held...)
	return nil
}
func (s *readerStore) RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error { return nil }
func (s *readerStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	s.executorCalls++
	s.mu.Unlock()
	return nil, errors.New("private database error")
}

type readerAuthority struct {
	store      *readerStore
	want       id.Actor
	deny       bool
	called     bool
	wrongGrant bool
}

func (a *readerAuthority) AuthorizeSystem(_ context.Context, tx f.Tx, got id.Actor, intent id.AccessIntent) (id.AccessGrant, error) {
	a.called = true
	if tx != a.store.tx || !got.Equal(a.want) || intent != id.Read {
		return id.AccessGrant{}, errors.New("authority stimulus")
	}
	if a.deny {
		return id.AccessGrant{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	at, _ := f.NewInstant(time.Now())
	if a.wrongGrant {
		intent = id.Mutate
	}
	return id.NewAccessGrant(got, id.SystemScope(), intent, at, 1)
}

type readerAudit struct{}

func (readerAudit) AppendInTx(context.Context, f.Tx, ac.Entry, ac.AppendKey) (ac.AppendReceipt, error) {
	return ac.AppendReceipt{}, errors.New("read called audit")
}
func TestReaderCurrentAuthorizationAndCollectedLocks(t *testing.T) {
	for _, wrong := range []bool{false, true} {
		st := &readerStore{}
		a := actor(t)
		system := &readerAuthority{store: st, want: a, deny: !wrong, wrongGrant: wrong}
		authority, e := NewAuthority(st, system)
		if e != nil {
			t.Fatal(e)
		}
		svc, e := New(authority, readerAudit{})
		if e != nil {
			t.Fatal(e)
		}
		target := sid[c.Runner](t)
		_, e = svc.Get(context.Background(), a, target)
		var ff *f.Fault
		if !errors.As(e, &ff) || ff.Code != f.Forbidden || !system.called || st.executorCalls != 0 {
			t.Fatal("read bypassed current authority", e)
		}
		if len(st.held) != 3 {
			t.Fatal("missing complete locks")
		}
		for n, l := range st.held {
			if l.Mode != f.Shared || n > 0 && f.CompareLockKeys(st.held[n-1].Key, l.Key) >= 0 {
				t.Fatal("wrong mode/order")
			}
		}
		user, _ := f.UserLock(a.Details().UserID)
		global, _ := f.SystemConfigLock("runner-management")
		gate, _ := f.SystemConfigLock("runner-control-" + target.String())
		for _, wanted := range []f.LockKey{user, global, gate} {
			found := false
			for _, l := range st.held {
				found = found || f.CompareLockKeys(l.Key, wanted) == 0
			}
			if !found {
				t.Fatal("wrong collected key")
			}
		}
	}
}
func TestReaderStopOwnsActualCallbackReturn(t *testing.T) {
	st := &readerStore{entered: make(chan struct{}), release: make(chan struct{})}
	a := actor(t)
	auth, _ := NewAuthority(st, &readerAuthority{store: st, want: a, deny: true})
	svc, _ := New(auth, readerAudit{})
	exited := make(chan error, 1)
	go func() { _, e := svc.Get(context.Background(), a, sid[c.Runner](t)); exited <- e }()
	<-st.entered
	svc.Stop()
	short, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if e := svc.Drain(short); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("reported join while callback held", e)
	}
	if e := svc.Force(short); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("force refreshed deadline", e)
	}
	_, e := svc.List(context.Background(), a, c.ListRequest{Limit: 50})
	var ff *f.Fault
	if !errors.As(e, &ff) || ff.Code != f.ShuttingDown {
		t.Fatal("new read admitted", e)
	}
	close(st.release)
	if e := <-exited; e == nil {
		t.Fatal("denial lost")
	}
	if e := svc.Drain(context.Background()); e != nil {
		t.Fatal(e)
	}
	if st.calls != 1 {
		t.Fatal("late work entered")
	}
}
func TestManagementLockOrderAndUnknownProvenance(t *testing.T) {
	a := actor(t)
	target := sid[c.Runner](t)
	intent, e := c.NewRevoke(target, c.CredentialRequest{ExpectedVersion: 2})
	if e != nil {
		t.Fatal(e)
	}
	identity, _ := intent.Identity(a, "key")
	held, e := locks(a, &target, &identity, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(held) != 4 || held[0].Mode != f.Exclusive {
		t.Fatal("command missing")
	}
	for n := 1; n < len(held); n++ {
		if f.CompareLockKeys(held[n-1].Key, held[n].Key) >= 0 {
			t.Fatal("rank inversion")
		}
	}
	cause, _ := f.NewCommandsCause(identity)
	attempt := sid[f.TransactionAttempt](t)
	original := f.UnknownResult(attempt, cause)
	e = resultError(original)
	var private commitFailure
	if !errors.As(e, &private) || private.CommitResult().AttemptID() != attempt || private.CommitResult().Cause().Details().Primary.Canonical() != identity.Canonical() {
		t.Fatal("unknown replaced provenance")
	}
}

func TestRunnerPageBudgetKeepsNextUnreadRecord(t *testing.T) {
	at, _ := f.NewInstant(time.Now())
	items := make([]c.Snapshot, 201)
	tags := make([]string, 32)
	for n := range tags {
		tags[n] = fmt.Sprintf("%02d", n) + strings.Repeat("x", 62)
	}
	for n := range items {
		items[n] = c.Snapshot{ID: sid[c.Runner](t), Name: strings.Repeat("n", 128), Description: strings.Repeat("d", 4096), Tags: tags, RootPath: "/" + strings.Repeat("r", 4095), Version: 1, CredentialGeneration: 1, Status: c.Offline, CreatedAt: at, UpdatedAt: at}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID.String() < items[j].ID.String() })
	if _, e := json.Marshal(c.Page{Items: items[:200]}); e == nil {
		t.Fatal("oversize public page accepted")
	}
	page, e := boundedPage(items, 200)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(page)
	if e != nil || len(raw) > c.MaxPageBytes {
		t.Fatal("page cap", e)
	}
	if len(page.Items) == 0 || len(page.Items) >= 200 || page.Next == nil || *page.Next != page.Items[len(page.Items)-1].ID || items[len(page.Items)].ID.String() <= page.Next.String() {
		t.Fatal("short page skipped keyset position")
	}
}
