package work

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

func pureID[K any](t *testing.T, n int) f.ID[K] {
	t.Helper()
	v, err := f.ParseID[K](fmt.Sprintf("01900000-0000-7000-8000-%012x", n))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func pureActor(t *testing.T, session int) i.Actor {
	t.Helper()
	v, err := i.NewHuman(pureID[i.User](t, 1), pureID[i.Session](t, session))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func pureCode(t *testing.T, err error, code f.Code) {
	t.Helper()
	var v *f.Fault
	if !errors.As(err, &v) || v.Code != code {
		t.Fatalf("expected %s got %v", code, err)
	}
}
func pureKeys(t *testing.T) cursor.Keyring {
	t.Helper()
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
	v, err := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"one","keys":[{"kid":"one","key_b64":%q}]}`, key))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestWorkRankDeterministicGeometryAndNoOp(t *testing.T) {
	id := func(n int) string { return pureID[struct{}](t, n).String() }
	half, ok := middleRank("", "")
	if !ok || half != "7fffffffffffffffffffffffffffffff" {
		t.Fatal("incorrect midpoint")
	}
	rows := []rankItem{{ID: id(1), Rank: "00000000000000000000000000000001"}, {ID: id(2), Rank: "00000000000000000000000000000002"}, {ID: id(3), Rank: "fffffffffffffffffffffffffffffffe"}}
	before := slices.Clone(rows)
	noop, err := rankFor(rows, id(1), id(2), false)
	if err != nil || noop.Changed || noop.Rebalanced || !slices.Equal(noop.Items, rows) {
		t.Fatal("no-op performed maintenance", err)
	}
	moved, err := rankFor(rows, id(3), id(2), false)
	if err != nil || !moved.Changed || !moved.Rebalanced || moved.Previous != id(1) || moved.Next != id(2) {
		t.Fatal("dense reorder failed", err)
	}
	if moved.Items[0].ID != id(1) || moved.Items[1].ID != id(3) || moved.Items[2].ID != id(2) {
		t.Fatal("relative order wrong")
	}
	if !slices.Equal(rows, before) {
		t.Fatal("rank mutated caller input")
	}
	for n, v := range moved.Items {
		if c.ValidateRank(v.Rank) != nil || n > 0 && moved.Items[n-1].Rank >= v.Rank {
			t.Fatal("rebalance not strict")
		}
	}
	again, err := rankFor(rows, id(3), id(2), false)
	if err != nil || !slices.Equal(again.Items, moved.Items) {
		t.Fatal("rank algorithm nondeterministic", err)
	}
	_, err = rankFor(rows, id(1), id(1), false)
	pureCode(t, err, f.InvalidArgument)
	_, err = rankFor(rows, id(1), id(4), false)
	pureCode(t, err, f.NotFound)
	_, err = rankFor(rows, id(4), "", false)
	pureCode(t, err, f.NotFound)
	tail, err := rankFor(rows, id(4), "", true)
	if err != nil || !tail.Rebalanced || tail.Previous != id(3) || len(tail.Items) != 4 {
		t.Fatal("tail edge did not rebalance", err)
	}
	corrupt := slices.Clone(rows)
	corrupt[1].Rank = corrupt[0].Rank
	_, err = rankFor(corrupt, id(4), "", true)
	pureCode(t, err, f.InternalError)
	_, err = nextVersion(f.Version(math.MaxInt64))
	pureCode(t, err, f.InvalidState)
	_, err = nextVersion(0)
	pureCode(t, err, f.InvalidState)
}
func TestWorkRankCapacityAndConstantLockUnion(t *testing.T) {
	rows := make([]rankItem, c.MaxGroupSize)
	for n := range rows {
		rows[n] = rankItem{ID: pureID[struct{}](t, n+1).String(), Rank: fmt.Sprintf("%032x", (n+1)*1024)}
	}
	_, err := rankFor(rows, pureID[struct{}](t, 5000).String(), "", true)
	pureCode(t, err, f.ResourceBusy)
	in := commandInput{Command: c.SprintReorder, Project: pureID[i.Project](t, 7000), Target: pureID[pc.Sprint](t, 7001).String(), User: pureID[i.User](t, 1), ReorderSprint: &c.ReorderSprintRequest{MilestoneID: pureID[c.Milestone](t, 7002)}}
	locks, err := in.locks("same-key")
	if err != nil || len(locks) != 5 {
		t.Fatal("nonconstant structure locks", err)
	}
	for n := 1; n < len(locks); n++ {
		if f.CompareLockKeys(locks[n-1].Key, locks[n].Key) >= 0 {
			t.Fatal("lock union unordered")
		}
	}
	duplicates := make([]f.LockRequest, 513)
	for n := range duplicates {
		duplicates[n] = locks[0]
	}
	if _, err = oc.NormalizeLocks(duplicates); err == nil {
		t.Fatal("raw lock ceiling bypassed")
	}
}
func pureRecord(t *testing.T) (*commandRecord, i.Actor, event.Summary) {
	t.Helper()
	a := pureActor(t, 2)
	id := pureID[c.Milestone](t, 4)
	projectID := pureID[i.Project](t, 5)
	input := commandInput{Command: c.MilestoneCreate, Project: projectID, Target: id.String(), User: pureID[i.User](t, 1), CreateMilestone: &c.CreateMilestoneRequest{MilestoneID: id, Title: "private title", Description: "private body"}}
	d, err := input.semantic(a, "key")
	if err != nil {
		t.Fatal(err)
	}
	at, err := f.NewInstant(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	eventID := pureID[event.EventIdentity](t, 6)
	milestone := c.Milestone{ID: id, ProjectID: projectID, Title: input.CreateMilestone.Title, Description: input.CreateMilestone.Description, ManualRank: "7fffffffffffffffffffffffffffffff", Version: 1, CreatedAt: at, UpdatedAt: at}
	result := c.StructureMutation{Command: c.MilestoneCreate, Changed: true, Milestone: &milestone, EventID: &eventID}
	header, err := structureHeader(eventID, input, result, at)
	if err != nil {
		t.Fatal(err)
	}
	commandID := pureID[c.StructureCommand](t, 7)
	payload := c.MilestoneChanged{CommandID: commandID, ActorUserID: input.User, Change: c.CreatedChange, ChangedFields: []c.ChangedField{c.DescriptionChanged, c.RankChanged, c.TitleChanged}, Position: &c.MilestonePosition{OrderGeneration: 2}}
	typed, err := c.RegisterWorkEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	ev, err := typed.NewMilestoneChanged(header, payload)
	if err != nil {
		t.Fatal(err)
	}
	gen := f.Version(1)
	record := &commandRecord{ID: commandID, Project: projectID, User: input.User, Command: input.Command, Key: "key", Semantic: d, Input: input, Revision: 1, State: "planned", Plan: &structurePlan{After: result, GroupBefore: &gen, Header: header, Payload: ev.PayloadBytes()}, EventID: &eventID, Created: at}
	if err = validateRecord(record, a); err != nil {
		t.Fatal("valid plan rejected", err)
	}
	return record, a, ev.Summary()
}
func TestWorkPlanIssuerSessionRevisionAndTrueInputBinding(t *testing.T) {
	r, a, summary := pureRecord(t)
	binding, locks, opaque, err := eventBinding(r, a, summary)
	if err != nil {
		t.Fatal(err)
	}
	issuer := oc.NewPlanIssuer()
	deps, err := oc.NewDependencies(issuer, binding, locks, opaque)
	if err != nil {
		t.Fatal(err)
	}
	if !deps.Matches(issuer, binding) || deps.Matches(oc.NewPlanIssuer(), binding) {
		t.Fatal("issuer ownership broken")
	}
	copy := deps.Opaque()
	copy[0] = 'x'
	copyLocks := deps.Locks()
	copyLocks[0].Mode = f.Shared
	if !slices.Equal(deps.Opaque(), opaque) || !sameLocks(deps.Locks(), locks) {
		t.Fatal("dependencies alias caller")
	}
	other, _, _, err := eventBinding(r, pureActor(t, 3), summary)
	if err != nil || other == binding {
		t.Fatal("Session not bound", err)
	}
	r.Revision++
	other, _, _, err = eventBinding(r, a, summary)
	if err != nil || other == binding {
		t.Fatal("revision not bound", err)
	}
	r.Revision--
	for _, modify := range []func(*event.Summary){func(v *event.Summary) { v.Producer = "model" }, func(v *event.Summary) { v.Header.EventID = pureID[event.EventIdentity](t, 99) }, func(v *event.Summary) { v.PayloadDigest = digest([]byte("{}")) }, func(v *event.Summary) { v.Header.Scope.ProjectID = pureID[event.Project](t, 98) }} {
		changed := summary
		modify(&changed)
		if _, _, _, err = eventBinding(r, a, changed); err == nil {
			t.Fatal("replaced summary accepted")
		}
	}
	original := r.Plan.After.Milestone.Title
	r.Plan.After.Milestone.Title = "forged"
	if validateRecord(r, a) == nil {
		t.Fatal("caller postimage accepted")
	}
	r.Plan.After.Milestone.Title = original
	copyResult := r.Plan.After.Clone()
	r.State = "completed"
	r.Receipt = &copyResult
	at := r.Created
	r.Committed = &at
	if err = validateRecord(r, a); err != nil {
		t.Fatal("immutable completed fact invalid", err)
	}
	r.Receipt.Milestone.Title = "tampered"
	if validateRecord(r, a) == nil {
		t.Fatal("receipt is not exact planned result")
	}
	for _, value := range []any{r.Input, *r.Plan, *r} {
		if strings.Contains(fmt.Sprintf("%#v", value), "private") {
			t.Fatal("implicit plan/body formatting exposed data")
		}
	}
}
func TestWorkCursorBindsShapeOwnerParentAndGeneration(t *testing.T) {
	keys := pureKeys(t)
	projectID := pureID[i.Project](t, 5)
	binding, err := listBinding(projectID, pureID[i.User](t, 1).String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	rank := "7fffffffffffffffffffffffffffffff"
	id := pureID[c.Milestone](t, 4).String()
	token, err := pageToken(keys, binding, 2, rank, id)
	if err != nil {
		t.Fatal(err)
	}
	actualRank, actualID, err := pageAfter(keys, token, binding, 2)
	if err != nil || actualRank != rank || actualID != id {
		t.Fatal("position lost", err)
	}
	_, _, err = pageAfter(keys, token, binding, 3)
	pureCode(t, err, f.CursorStale)
	other, err := listBinding(projectID, pureID[i.User](t, 9).String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = pageAfter(keys, token, other, 2)
	pureCode(t, err, f.CursorInvalid)
	parent := pureID[c.Milestone](t, 4)
	other, err = listBinding(projectID, pureID[i.User](t, 1).String(), &parent)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = pageAfter(keys, token, other, 2)
	pureCode(t, err, f.CursorInvalid)
	text, err := cursor.Text(rank)
	if err != nil {
		t.Fatal(err)
	}
	uuid, err := cursor.UUID(id)
	if err != nil {
		t.Fatal(err)
	}
	g := int64(2)
	for _, position := range []cursor.Position{{Scalars: []cursor.Scalar{text, uuid}}, {Scalars: []cursor.Scalar{uuid, text}, OrderGeneration: &g}, {Scalars: []cursor.Scalar{text, uuid, uuid}, OrderGeneration: &g}} {
		bad, err := keys.Sign(binding, position)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = pageAfter(keys, bad, binding, 2)
		pureCode(t, err, f.CursorInvalid)
	}
}

// Pure denial ports exercise wiring and lifetime only. Positive authorization,
// foreign-store and actual lock behavior are exercised by the real PG fixture.
type denialStore struct{ touches atomic.Int64 }
type denialRow struct{}

func (denialRow) Scan(...any) error { return errors.New("pure SQL denied") }
func (*denialStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("pure SQL denied")
}
func (*denialStore) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, errors.New("pure SQL denied")
}
func (*denialStore) QueryRow(context.Context, string, ...any) postgres.Row { return denialRow{} }
func (s *denialStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	s.touches.Add(1)
	return nil, errors.New("pure foreign transaction denied")
}
func (*denialStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	return f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted))
}
func (*denialStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error {
	return errors.New("pure lock denied")
}
func (*denialStore) RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error {
	return errors.New("pure lock denied")
}

type denialSession struct{}

func (denialSession) RequireCurrentSession(context.Context, f.Tx, i.Actor) error {
	return f.NewFault(f.Forbidden, f.NotStarted)
}

type denialEvents struct{}

func (*denialEvents) PrepareAppend(context.Context, i.Actor, event.Event) (oc.AppendPlan, error) {
	return oc.AppendPlan{}, f.NewFault(f.Forbidden, f.NotStarted)
}
func (*denialEvents) AppendEventInTx(context.Context, f.Tx, i.Actor, event.Event, oc.AppendPlan) (oc.AppendReceipt, error) {
	return oc.AppendReceipt{}, f.NewFault(f.Forbidden, f.NotStarted)
}

type denialActivity struct{}

func (*denialActivity) TouchActivityInTx(context.Context, f.Tx, i.Actor) error {
	return f.NewFault(f.Forbidden, f.NotStarted)
}
func purePorts(t *testing.T) (*denialStore, *project.Authority, *Authority, c.WorkEvents) {
	t.Helper()
	store := &denialStore{}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: denialSession{}})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewAuthority(store, projects)
	if err != nil {
		t.Fatal(err)
	}
	events, err := c.RegisterWorkEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return store, projects, authority, events
}
func TestWorkConstructorsNilBindingsAndCallerTxBoundary(t *testing.T) {
	store, _, authority, events := purePorts(t)
	deps := Dependencies{Authority: authority, Events: &denialEvents{}, WorkEvents: events, Activity: &denialActivity{}}
	if _, err := New(store, deps); err != nil {
		t.Fatal(err)
	}
	var typedNil *denialEvents
	bad := deps
	bad.Events = typedNil
	_, err := New(store, bad)
	pureCode(t, err, f.DependencyUnbound)
	bad = deps
	bad.Activity = nil
	_, err = New(store, bad)
	pureCode(t, err, f.DependencyUnbound)
	_, err = New(&denialStore{}, deps)
	pureCode(t, err, f.DependencyUnbound)
	_, err = NewReader(store, authority, cursor.Keyring{})
	pureCode(t, err, f.InvalidArgument)
	reader, err := NewReader(store, authority, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	_, err = reader.ReadPlacementInTx(context.Background(), f.NewTx(), pureActor(t, 2), pureID[i.Project](t, 5), pureID[pc.Sprint](t, 6))
	if err == nil || store.touches.Load() != 1 {
		t.Fatal("caller foreign Tx bypassed")
	}
}
func TestWorkStopDrainIncludesRegisteredConfirmations(t *testing.T) {
	store, _, authority, events := purePorts(t)
	service, err := New(store, Dependencies{Authority: authority, Events: &denialEvents{}, WorkEvents: events, Activity: &denialActivity{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, entry, done, err := service.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	confirm, cancel := context.WithCancel(context.Background())
	service.state().mu.Lock()
	entry.confirmations[&confirmation{cancel: cancel}] = struct{}{}
	service.state().mu.Unlock()
	service.Stop()
	if ctx.Err() == nil || confirm.Err() == nil {
		t.Fatal("stop missed owned context")
	}
	short, stop := context.WithCancel(context.Background())
	stop()
	if err = service.Drain(short); !errors.Is(err, context.Canceled) {
		t.Fatal("Drain inferred join from cancellation", err)
	}
	done()
	if err = service.Drain(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = service.begin(context.Background())
	pureCode(t, err, f.ShuttingDown)
}
func TestWorkProjectGateExactClosedDiscovery(t *testing.T) {
	_, projects, _, _ := purePorts(t)
	r, a, summary := pureRecord(t)
	request, err := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: r.Project, Actor: a, Stage: oc.CurrentAccess, Event: summary})
	if err != nil {
		t.Fatal(err)
	}
	deps, err := projects.Discover(context.Background(), request)
	if err != nil || deps.Validate() != nil {
		t.Fatal("Work Project gate not bound", err)
	}
	if len(deps.Locks()) != 2 || string(deps.Opaque()) != "project.work-structure.append-v1" {
		t.Fatal("wrong gate purpose/locks")
	}
	for _, modify := range []func(*event.Summary){func(v *event.Summary) { v.Header.EventType = "work.task_changed" }, func(v *event.Summary) { v.Header.SchemaVersion = 2 }, func(v *event.Summary) { v.Header.AggregateType = "work.task" }} {
		v := summary
		modify(&v)
		request, err := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: r.Project, Actor: a, Stage: oc.CurrentAccess, Event: v})
		if err != nil {
			t.Fatal(err)
		}
		_, err = projects.Discover(context.Background(), request)
		pureCode(t, err, f.DependencyUnbound)
	}
	request, err = oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.AppendProject, ProjectID: r.Project, Actor: a, Stage: oc.NewFact, Event: summary})
	if err != nil {
		t.Fatal(err)
	}
	_, err = projects.Discover(context.Background(), request)
	pureCode(t, err, f.Forbidden)
	var shape map[string]any
	if json.Unmarshal(deps.Opaque(), &shape) == nil {
		t.Fatal("gate opaque unexpectedly writable JSON")
	}
}

// These pure ports only observe denial ordering. They do not establish a
// positive Session, Owner grant, SQL fact, or physical transaction outcome.
type gateReadStore struct {
	denialStore
	tx          f.Tx
	held        bool
	workQueries int
	lockChecks  int
}

func (s *gateReadStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return s, nil
}
func (s *gateReadStore) RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error {
	s.lockChecks++
	if !s.held {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (s *gateReadStore) QueryRow(_ context.Context, query string, _ ...any) postgres.Row {
	if strings.Contains(query, "agenteam_work.") {
		s.workQueries++
	}
	return denialRow{}
}

type gateRevokedSession struct{ calls int }

func (s *gateRevokedSession) RequireCurrentSession(context.Context, f.Tx, i.Actor) error {
	s.calls++
	return f.NewFault(f.SessionRevoked, f.NotStarted)
}
func TestWorkProducerDenialsPrecedePrivateRowReads(t *testing.T) {
	record, actor, summary := pureRecord(t)
	binding, locks, opaque, err := eventBinding(record, actor, summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"foreign-issuer-empty-locks", "missing-held-locks", "revoked-current-session", "foreign-live-tx"} {
		t.Run(name, func(t *testing.T) {
			store := &gateReadStore{tx: f.NewTx(), held: name != "missing-held-locks"}
			sessions := &gateRevokedSession{}
			projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: sessions})
			if err != nil {
				t.Fatal(err)
			}
			authority, err := NewAuthority(store, projects)
			if err != nil {
				t.Fatal(err)
			}
			issuer, actualLocks := authority.state().issuer, locks
			if name == "foreign-issuer-empty-locks" {
				issuer, actualLocks = oc.NewPlanIssuer(), nil
			}
			deps, err := oc.NewDependencies(issuer, binding, actualLocks, opaque)
			if err != nil {
				t.Fatal(err)
			}
			tx := store.tx
			if name == "foreign-live-tx" {
				tx = f.NewTx()
			}
			err = authority.ValidateAppendInTx(context.Background(), tx, actor, summary, deps, oc.CurrentAccess)
			if name == "revoked-current-session" {
				pureCode(t, err, f.SessionRevoked)
				if sessions.calls != 1 || store.lockChecks < 2 {
					t.Fatal("current gate did not follow complete lock checks")
				}
			} else {
				pureCode(t, err, f.Forbidden)
				if sessions.calls != 0 {
					t.Fatal("denied issuer/Tx/locks reached current session")
				}
			}
			if store.workQueries != 0 {
				t.Fatal("private Work SQL preceded issuer/locks/current authorization")
			}
		})
	}
}

type confirmationFailureStore struct {
	denialStore
	result f.CommitResult
}

func (s *confirmationFailureStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	return s.result
}
func TestWorkUnknownRetainsOriginalPrivateProvenance(t *testing.T) {
	actor := pureActor(t, 2)
	projectID := pureID[i.Project](t, 5)
	originals := []f.CommitResult{}
	for n, key := range []f.IdempotencyKey{"private-original-key", "private-other-key"} {
		identity, err := c.Identity(projectID, c.MilestoneUpdate, key)
		if err != nil {
			t.Fatal(err)
		}
		cause, err := f.NewCommandsCause(identity)
		if err != nil {
			t.Fatal(err)
		}
		originals = append(originals, f.UnknownResult(pureID[f.TransactionAttempt](t, 90+n), cause))
	}
	assertOriginal := func(t *testing.T, err error, original f.CommitResult, code f.Code, state f.CommitState, retry string) {
		t.Helper()
		var public *f.Fault
		var private commitFailure
		if !errors.As(err, &public) || !errors.As(err, &private) || public.Code != code || public.CommitState != state || public.RetryHint != retry || public.CauseID != original.AttemptID().String() || private.result.AttemptID() != original.AttemptID() || private.result.Cause().Details().Primary.Canonical() != original.Cause().Details().Primary.Canonical() {
			t.Fatal("original attempt/cause or safe projection lost")
		}
		for _, value := range []any{err, private, private.result, private.LogValue()} {
			for _, format := range []string{"%v", "%+v", "%#v"} {
				printed := fmt.Sprintf(format, value)
				if strings.Contains(printed, "private-original-key") || strings.Contains(printed, "private-other-key") {
					t.Fatal("implicit Unknown formatting exposed command material")
				}
			}
		}
		if private.LogValue().Kind() != slog.KindString {
			t.Fatal("private provenance exposed structured internals")
		}
	}
	for _, original := range originals {
		assertOriginal(t, txError(original), original, f.CommitUnknown, f.Unknown, "lookup")
		assertOriginal(t, notCommittedAfterUnknown(original), original, f.DependencyUnavailable, f.NotCommitted, "retry_same_key")
	}
	// The failed confirmation's different attempt must never replace the
	// uncertain writer's attempt. This port is a negative pure result fixture.
	for _, failure := range []f.CommitResult{originals[1], f.NotCommittedResult(f.NewFault(f.SessionRevoked, f.NotCommitted)), f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(context.Canceled))} {
		store := &confirmationFailureStore{result: failure}
		state := &serviceState{store: store, calls: map[*call]struct{}{}, changed: make(chan struct{})}
		service := &Service{data: func() *serviceState { return state }}
		ctx, entry, done, err := service.begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		query := c.CommandLookupRequest{ProjectID: projectID, Command: c.MilestoneUpdate, Key: "private-original-key", Semantic: digest([]byte("safe input digest"))}
		_, err = service.confirmUnknown(ctx, entry, actor, query, originals[0])
		done()
		assertOriginal(t, err, originals[0], f.CommitUnknown, f.Unknown, "lookup")
		if len(entry.confirmations) != 0 {
			t.Fatal("confirmation was not deregistered")
		}
	}
}

// This result-only pure port uses the existing Store boundary without claiming
// a positive SQL read or a physical commit. The PG fixture owns those checks.
type lookupResultStore struct {
	confirmationFailureStore
	beforeResult func(context.Context)
	calls        int
}

func (s *lookupResultStore) WithinTx(ctx context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	s.beforeResult(ctx)
	return s.result
}
func TestWorkLookupCancellationRespectsTransactionOutcome(t *testing.T) {
	actor := pureActor(t, 2)
	query := c.CommandLookupRequest{ProjectID: pureID[i.Project](t, 5), Command: c.MilestoneUpdate, Key: "lookup-cancellation-key", Semantic: digest([]byte("safe input digest"))}
	identity, err := c.Identity(query.ProjectID, query.Command, query.Key)
	if err != nil {
		t.Fatal(err)
	}
	poison := errors.New("pure transaction poison")
	unknown := f.UnknownResult(pureID[f.TransactionAttempt](t, 95), commandCause(identity))
	for _, outcome := range []struct {
		name   string
		result f.CommitResult
	}{
		{"not-committed", f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(poison))},
		{"committed", f.CommittedResult()},
		{"unknown", unknown},
	} {
		for _, ending := range []string{"open", "cancel", "deadline"} {
			t.Run(outcome.name+"/"+ending, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				if ending == "deadline" {
					cancel()
					ctx, cancel = context.WithTimeout(context.Background(), time.Millisecond)
				}
				defer cancel()
				store := &lookupResultStore{confirmationFailureStore: confirmationFailureStore{result: outcome.result}}
				store.beforeResult = func(received context.Context) {
					if received != ctx {
						t.Fatal("lookup replaced caller context")
					}
					switch ending {
					case "cancel":
						cancel()
					case "deadline":
						<-received.Done()
					}
				}
				state := &serviceState{store: store}
				service := &Service{data: func() *serviceState { return state }}
				output, err := service.lookup(ctx, actor, query)
				if store.calls != 1 || output != (c.CommandLookup{}) {
					t.Fatal("lookup skipped WithinTx or published an unobserved result")
				}
				var wantContext error
				switch ending {
				case "cancel":
					wantContext = context.Canceled
				case "deadline":
					wantContext = context.DeadlineExceeded
				}
				if !errors.Is(ctx.Err(), wantContext) {
					t.Fatal("context fixture did not reach its required state")
				}
				switch outcome.result.State() {
				case f.Committed:
					if err != nil {
						t.Fatal("delivery cancellation rewrote known commit", err)
					}
				case f.Unknown:
					pureCode(t, err, f.CommitUnknown)
					var public *f.Fault
					var private commitFailure
					if !errors.As(err, &public) || !errors.As(err, &private) || public.CommitState != f.Unknown || public.RetryHint != "lookup" || public.CauseID != unknown.AttemptID().String() || private.result.AttemptID() != unknown.AttemptID() || private.result.Cause().Details().Primary.Canonical() != identity.Canonical() {
						t.Fatal("cancellation replaced Unknown or its original provenance")
					}
					if wantContext != nil && errors.Is(err, wantContext) {
						t.Fatal("Unknown was reported as a canceled rollback")
					}
				case f.NotCommitted:
					var public *f.Fault
					if !errors.As(err, &public) || public.CommitState != f.NotCommitted {
						t.Fatal("proven rollback state lost")
					}
					if wantContext == nil {
						pureCode(t, err, f.InternalError)
						if !errors.Is(err, poison) {
							t.Fatal("uncanceled rollback lost its original fault")
						}
					} else {
						pureCode(t, err, f.DependencyUnavailable)
						if !errors.Is(err, wantContext) || errors.Is(err, poison) {
							t.Fatal("proven caller cancellation was replaced by transaction poison")
						}
					}
				}
			})
		}
	}
}
func TestWorkCanceledConfirmationKeepsOriginalWriterOutcome(t *testing.T) {
	actor := pureActor(t, 2)
	query := c.CommandLookupRequest{ProjectID: pureID[i.Project](t, 5), Command: c.MilestoneUpdate, Key: "private-writer-key", Semantic: digest([]byte("safe input digest"))}
	identity, err := c.Identity(query.ProjectID, query.Command, query.Key)
	if err != nil {
		t.Fatal(err)
	}
	original := f.UnknownResult(pureID[f.TransactionAttempt](t, 96), commandCause(identity))
	store := &lookupResultStore{confirmationFailureStore: confirmationFailureStore{result: f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted))}}
	state := &serviceState{store: store, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	service := &Service{data: func() *serviceState { return state }}
	ctx, entry, done, err := service.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	store.beforeResult = func(confirm context.Context) {
		service.Stop()
		if !errors.Is(confirm.Err(), context.Canceled) {
			t.Fatal("Stop did not cancel the registered confirmation")
		}
	}
	output, err := service.confirmUnknown(ctx, entry, actor, query, original)
	pureCode(t, err, f.CommitUnknown)
	var public *f.Fault
	var private commitFailure
	if !errors.As(err, &public) || !errors.As(err, &private) || public.CommitState != f.Unknown || public.RetryHint != "lookup" || public.CauseID != original.AttemptID().String() || private.result.AttemptID() != original.AttemptID() || private.result.Cause().Details().Primary.Canonical() != identity.Canonical() {
		t.Fatal("canceled confirmation replaced the original writer provenance")
	}
	if store.calls != 1 || output != (c.StructureMutation{}) || len(entry.confirmations) != 0 || errors.Is(err, context.Canceled) {
		t.Fatal("canceled confirmation published a result, leaked registration, or hid Unknown")
	}
}
