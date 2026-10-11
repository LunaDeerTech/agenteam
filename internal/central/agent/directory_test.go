package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func directoryKeys(t *testing.T) cursor.Keyring {
	t.Helper()
	v, e := cursor.LoadKeyring(`{"format":1,"current_kid":"directory","keys":[{"kid":"directory","key_b64":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}]}`)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestAgentDirectoryPublicationAndScopedCursor(t *testing.T) {
	in, at := planFixture(t)
	config, _, err := planConfiguration(in, nil, at)
	if err != nil {
		t.Fatal(err)
	}
	entry := directoryProjection(config)
	mutation, err := c.NewAgentMutation(c.AgentMutationFields{Agent: config, Changed: true, EventIDs: []ec.EventID{commandID[ec.EventIdentity](t, "01900000-0000-7000-8000-000000000020")}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal([]c.AgentMutation{mutation})
	if err != nil || directoryCreation(raw, entry) != nil {
		t.Fatal("valid original initialization receipt", err)
	}
	for _, bad := range [][]byte{[]byte(`null`), []byte(`[]`), append(append([]byte{'['}, raw[1:len(raw)-1]...), append([]byte{','}, raw[1:]...)...)} {
		if directoryCreation(bad, entry) == nil {
			t.Fatal("missing/duplicate initialization accepted")
		}
	}
	foreign := entry
	foreign.ID = commandID[i.Agent](t, "01900000-0000-7000-8000-000000000021")
	if directoryCreation(raw, foreign) == nil {
		t.Fatal("foreign publication accepted")
	}
	keys := directoryKeys(t)
	binding, err := directoryBinding(in.project, in.actor.Details().UserID)
	if err != nil {
		t.Fatal(err)
	}
	instant, _ := cursor.Instant(at)
	id, _ := cursor.UUID(entry.ID.String())
	token, err := keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{instant, id}})
	if err != nil {
		t.Fatal(err)
	}
	position, err := directoryAfter(keys, token, binding)
	if err != nil || position.at != at || position.id != entry.ID {
		t.Fatal("canonical position lost", err)
	}
	changed := binding
	changed.Order = "name:asc,id:asc"
	owner, _ := directoryBinding(in.project, "01900000-0000-7000-8000-000000000022")
	project, _ := directoryBinding(commandID[i.Project](t, "01900000-0000-7000-8000-000000000023"), in.actor.Details().UserID)
	for _, b := range []cursor.Binding{changed, owner, project} {
		if _, e := directoryAfter(keys, token, b); e == nil {
			t.Fatal("cursor crossed original owner/project/order")
		}
	}
	if _, err = directoryAfter(keys, token+"x", binding); err == nil {
		t.Fatal("tampered cursor accepted")
	}
	if directoryBefore(entry, position) {
		t.Fatal("same item can repeat")
	}
	older := entry
	older.CreatedAt, _ = f.NewInstant(at.Time().Add(-time.Microsecond))
	if !directoryBefore(older, position) {
		t.Fatal("older fixed ordering rejected")
	}
}

// These explicit controlled SQL boundaries prove ordering, failure and owned
// joins. Actual list SQL, initialization and current Owner are covered by PG.
type directoryControlStore struct {
	Store
	t                *testing.T
	live, authorized bool
	locks            []f.LockRequest
	calls, reads     int
	queryErr         error
	unknown          bool
	entered          chan context.Context
	release          chan struct{}
}

func (s *directoryControlStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	s.live = true
	defer func() { s.live = false }()
	err := fn(ctx, f.Tx{})
	if s.unknown {
		return f.UnknownResult(commandID[f.TransactionAttempt](s.t, "01900000-0000-7000-8000-000000000028"), cause)
	}
	if err != nil {
		var fault *f.Fault
		if errors.As(err, &fault) {
			return f.NotCommittedResult(fault)
		}
		return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
	}
	return f.CommittedResult()
}
func (s *directoryControlStore) AcquireAll(_ context.Context, _ f.Tx, locks []f.LockRequest) error {
	s.locks = append([]f.LockRequest(nil), locks...)
	return nil
}
func (s *directoryControlStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || !s.authorized {
		s.t.Fatal("SQL before original Owner/Tx")
	}
	return s, nil
}
func (s *directoryControlStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	s.reads++
	return nil, s.queryErr
}
func (s *directoryControlStore) QueryRow(ctx context.Context, _ string, _ ...any) postgres.Row {
	s.reads++
	if s.entered != nil {
		s.entered <- ctx
		<-s.release
	}
	return executionConfigurationRow{pgx.ErrNoRows}
}
func directoryControlled(t *testing.T) (*DirectoryReader, *directoryControlStore, i.Actor, i.ProjectID, i.AgentID, *error) {
	t.Helper()
	request, _, grant := executionConfigurationFixture(t)
	s := &directoryControlStore{t: t, queryErr: errors.New("private-storage-canary")}
	var deny error
	projects := &executionConfigurationProjects{check: func(_ context.Context, _ f.Tx, a i.Actor, p i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
		if !s.live || len(s.locks) == 0 || a.Details() != request.Actor.Details() || p != request.ProjectID || intent != i.Read {
			t.Fatal("wrong current Owner scope")
		}
		if deny != nil {
			return pc.ProjectAccess{}, deny
		}
		s.authorized = true
		return grant, nil
	}}
	authority, err := NewAuthority(s, projects)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewDirectoryReader(s, authority, directoryKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	if got, e := NewDirectoryReader(&directoryControlStore{}, authority, directoryKeys(t)); e == nil || got != nil {
		t.Fatal("foreign Store accepted")
	}
	return reader, s, request.Actor, request.ProjectID, request.AgentID, &deny
}
func TestAgentDirectoryCurrentOwnerAndFailurePublishNothing(t *testing.T) {
	r, s, a, p, id, deny := directoryControlled(t)
	*deny = fault(f.Forbidden)
	if out, e := r.GetAgent(context.Background(), a, p, id); e == nil || out.ID.Validate() == nil || s.reads != 0 {
		t.Fatal("unauthorized existence leak")
	}
	if out, e := r.ListAgents(context.Background(), a, p, f.DefaultPageRequest()); e == nil || len(out.Items) != 0 || s.reads != 0 {
		t.Fatal("unauthorized list leak")
	}
	*deny = nil
	_, err := r.GetAgent(context.Background(), a, p, id)
	requireCode(t, err, f.NotFound)
	if len(s.locks) != 3 || s.locks[2].Key.Canonical() != agentLock(id, f.Shared).Key.Canonical() || s.locks[2].Mode != f.Shared {
		t.Fatal("Get original lock set")
	}
	out, err := r.ListAgents(context.Background(), a, p, f.DefaultPageRequest())
	requireCode(t, err, f.DependencyUnavailable)
	if len(out.Items) != 0 || strings.Contains(err.Error(), "private-storage-canary") || len(s.locks) != 2 || s.locks[1].Mode != f.Exclusive {
		t.Fatal("list failure/project gate")
	}
	s.unknown = true
	out, err = r.ListAgents(context.Background(), a, p, f.DefaultPageRequest())
	requireCode(t, err, f.CommitUnknown)
	if original, ok := UnknownAttempt(err); !ok || original.AttemptID().Validate() != nil || len(out.Items) != 0 {
		t.Fatal("read Unknown provenance or partial output")
	}
	r.Stop()
	if !r.Joined() {
		t.Fatal("completed reads retained")
	}
}
func TestAgentDirectoryCancellationWaitsForOriginalRead(t *testing.T) {
	r, s, a, p, id, _ := directoryControlled(t)
	s.entered = make(chan context.Context, 1)
	s.release = make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	t.Cleanup(func() {
		once.Do(func() { close(s.release) })
		r.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = r.Drain(ctx)
	})
	go func() { _, err := r.GetAgent(context.Background(), a, p, id); done <- err }()
	var original context.Context
	select {
	case original = <-s.entered:
	case <-time.After(time.Second):
		t.Fatal("original SQL not entered")
	}
	r.Stop()
	if original.Err() == nil || r.Joined() {
		t.Fatal("Stop returned before cancel/original read")
	}
	short, cancel := context.WithCancel(context.Background())
	cancel()
	if r.Drain(short) == nil {
		t.Fatal("Drain ignored actual read")
	}
	select {
	case <-done:
		t.Fatal("read returned before SQL")
	default:
	}
	once.Do(func() { close(s.release) })
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled/missing read succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("read did not actually return")
	}
	if err := r.Drain(context.Background()); err != nil || !r.Joined() {
		t.Fatal("actual return did not retire", err)
	}
}
