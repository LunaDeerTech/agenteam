package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This private composition controls live opaque Tx tokens, their lock sets,
// Scan projections and original Store returns. Its authority and process port
// are deliberately not a real Project grant, ProcessGuard or PostgreSQL proof.
type artifactCallTestStore struct {
	*cleanupOwnerStore
	fixture *artifactCallFixture
}

func (s *artifactCallTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.cleanupOwnerStore.InTx(tx); err != nil {
		return nil, err
	}
	return artifactCallTestExecutor{s, tx}, nil
}

type artifactCallTestExecutor struct {
	store *artifactCallTestStore
	tx    f.Tx
}

func (e artifactCallTestExecutor) Query(context.Context, string, ...any) (*postgres.Rows, error) {
	return nil, errors.New("uncontrolled Rows are outside this ownership fixture")
}
func (e artifactCallTestExecutor) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if _, err := e.store.InTx(e.tx); err != nil {
		return cleanupOwnerRow{err: err}
	}
	return e.store.fixture.row(ctx, e.tx, query, args)
}
func (e artifactCallTestExecutor) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if _, err := e.store.InTx(e.tx); err != nil {
		return pgconn.CommandTag{}, err
	}
	return e.store.fixture.exec(ctx, e.tx, query, args)
}

type artifactCallStopFact struct {
	actor i.Actor
	cause oc.ProjectStopCause
	state string
	owner artifactStopOwner
}
type artifactCallFixture struct {
	*cleanupOwnerFixture
	store       *artifactCallTestStore
	pair        *ProjectCallOwnership
	process     oc.ProcessID
	calls       map[string]artifactCallRecord
	revoked     map[string]bool
	admitted    map[string]int64
	stops       map[string]*artifactCallStopFact
	proofChecks int
	// Reject inside the original registration callback, before its INSERT.
	rejectRegistration bool
}

func newArtifactCallFixture(t *testing.T) (*artifactCallFixture, i.ProjectID) {
	t.Helper()
	base, p := newCleanupOwnerFixture(t)
	v := &artifactCallFixture{cleanupOwnerFixture: base, process: newTestID[oc.Process](t), calls: map[string]artifactCallRecord{}, revoked: map[string]bool{}, admitted: map[string]int64{}, stops: map[string]*artifactCallStopFact{}}
	v.store = &artifactCallTestStore{cleanupOwnerStore: base.store, fixture: v}
	owners, err := NewOwnerProvider(v.store, v)
	if err != nil {
		t.Fatal(err)
	}
	v.state.store, v.state.owners = v.store, owners
	v.state.leaseSources = make(map[f.Digest]string)
	v.pair, err = newProjectCallOwnership(v.service, v, v.process, v)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Test-specific cleanups release and receive original goroutines first.
		v.store.before, v.store.after = nil, nil
		v.state.cleanupMu.Lock()
		var cleanupProjects []i.ProjectID
		for p, owner := range v.state.cleanupCalls {
			if owner.retained && !owner.active {
				cleanupProjects = append(cleanupProjects, p)
			}
		}
		v.state.cleanupMu.Unlock()
		for _, p := range cleanupProjects {
			if _, err := v.run(context.Background(), v.service, p); err != nil {
				t.Error("cleanup confirmation", err)
			}
		}
		v.pair.state.mu.Lock()
		var retained []*artifactStopCall
		for _, call := range v.pair.state.stops {
			if call.retained && !call.active {
				retained = append(retained, call)
			}
		}
		v.pair.state.mu.Unlock()
		for _, call := range retained {
			if _, err := v.service.RequestProjectStop(context.Background(), call.actor, call.cause); err != nil {
				t.Error("stop confirmation", err)
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := v.pair.Drain(ctx); err != nil {
			t.Error("original Artifact calls did not drain", err)
		}
	})
	return v, p
}

func (v *artifactCallFixture) AuthorizeInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject, intent i.AccessIntent) (i.AccessGrant, error) {
	if err := v.store.held(ctx, tx, subject.ProjectID); err != nil {
		return i.AccessGrant{}, err
	}
	at, _ := f.ParseInstant("2026-10-12T10:00:00Z")
	return i.NewAccessGrant(subject.Actor, identityScope(subject.ProjectID), intent, at, 7)
}
func (v *artifactCallFixture) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, process oc.ProcessID) error {
	if process != v.process {
		return failure(f.Forbidden, nil)
	}
	if _, err := v.store.InTx(tx); err != nil {
		return err
	}
	if v.rejectRegistration && ctx.Value(artifactCallRegistrationKey{}) == v.pair.state {
		return failure(f.Forbidden, nil)
	}
	return ctx.Err()
}
func (v *artifactCallFixture) ConfirmStopped(ctx context.Context, process oc.ProcessID) error {
	v.store.mu.Lock()
	live := len(v.store.live)
	v.store.mu.Unlock()
	v.mu.Lock()
	v.proofChecks++
	v.mu.Unlock()
	if live != 0 || process == v.process {
		return errors.New("process confirmation was not outside the original Tx")
	}
	return ctx.Err()
}
func (v *artifactCallFixture) DiscoverProjectStop(ctx context.Context, request oc.ProjectStopRequest) (oc.AccessDependencies, error) {
	p := request.Details().Cause.Details().ProjectID
	return oc.NewAccessDependencies(digestBytes([]byte(p.String())), []f.LockRequest{projectExclusive(p)})
}
func (v *artifactCallFixture) ValidateProjectStopInTx(ctx context.Context, tx f.Tx, request oc.ProjectStopRequest, deps oc.AccessDependencies) (oc.ProjectStopAuthorization, error) {
	d := request.Details()
	if err := v.store.held(ctx, tx, d.Cause.Details().ProjectID); err != nil {
		return oc.ProjectStopAuthorization{}, err
	}
	v.mu.Lock()
	fact := v.stops[d.Cause.Details().OperationID.String()]
	valid := fact != nil && fact.actor.Equal(d.Actor) && fact.cause.Equal(d.Cause)
	v.mu.Unlock()
	if !valid {
		return oc.ProjectStopAuthorization{}, failure(f.Forbidden, nil)
	}
	return oc.NewProjectStopAuthorization(tx, request, deps, oc.ContinueProjectStop)
}
func (v *artifactCallFixture) invocation(t *testing.T, p i.ProjectID) ac.Invocation {
	t.Helper()
	actor, err := i.NewHuman(newTestID[i.User](t), newTestID[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	out, err := ac.NewInvocation(ac.InvocationDetails{Actor: actor, ProjectID: p, AttemptID: newTestID[ac.CallAttempt](t)})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func (v *artifactCallFixture) stop(t *testing.T, p i.ProjectID) *artifactCallStopFact {
	t.Helper()
	cause, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: p, OperationID: newTestID[oc.ProjectStopOperation](t), Action: oc.ProjectStopDelete, ProjectVersion: 8})
	if err != nil {
		t.Fatal(err)
	}
	registration, _ := i.RegisterService(i.ProjectLifecycle)
	actor, err := registration.Actor(cause.Details().OperationID.String(), identityScope(p))
	if err != nil {
		t.Fatal(err)
	}
	fact := &artifactCallStopFact{actor: actor, cause: cause}
	v.mu.Lock()
	v.stops[cause.Details().OperationID.String()] = fact
	v.mu.Unlock()
	return fact
}
func artifactCallJSON(value any) postgres.Row {
	raw, err := json.Marshal(value)
	return cleanupOwnerRow{values: []any{raw}, err: err}
}
func (v *artifactCallFixture) row(ctx context.Context, tx f.Tx, query string, args []any) postgres.Row {
	if strings.Contains(query, "'Work',") && strings.Contains(query, "'Stops',") {
		v.mu.Lock()
		defer v.mu.Unlock()
		facts := artifactCleanupCalls{Work: []artifactCleanupWork{}, Stops: []artifactCleanupStop{}}
		for _, record := range v.calls {
			if record.Project.String() == args[0] {
				facts.Work = append(facts.Work, artifactCleanupWork{ID: record.ID.String(), Project: record.Project.String(), Process: record.Process.String(), Instance: record.Instance, Joined: record.Joined})
			}
		}
		for _, stop := range v.stops {
			d := stop.cause.Details()
			if stop.state != "" && d.ProjectID.String() == args[0] {
				facts.Stops = append(facts.Stops, artifactCleanupStop{Operation: d.OperationID.String(), Action: string(d.Action), State: stop.state, Process: stop.owner.process, Instance: stop.owner.instance, Version: int64(d.ProjectVersion)})
			}
		}
		sort.Slice(facts.Work, func(a, b int) bool { return facts.Work[a].ID < facts.Work[b].ID })
		sort.Slice(facts.Stops, func(a, b int) bool { return facts.Stops[a].Operation < facts.Stops[b].Operation })
		return artifactCallJSON(facts)
	}
	if strings.Contains(query, "source_lease_id IS NOT NULL") {
		v.mu.Lock()
		defer v.mu.Unlock()
		for p, fact := range v.facts {
			if args[0] == p.String() {
				return cleanupOwnerRow{values: []any{fact.source}}
			}
		}
		return cleanupOwnerRow{err: errors.New("unconfigured source Project")}
	}
	if strings.HasPrefix(query, "SELECT EXISTS") && strings.Contains(query, "agenteam_artifact.cleanup") && !strings.Contains(query, "agenteam_artifact.artifacts") {
		return cleanupOwnerRow{values: []any{false}}
	}
	if !strings.Contains(query, "agenteam_artifact.project_") {
		return v.cleanupOwnerFixture.row(ctx, tx, query, args)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case strings.Contains(query, "FROM agenteam_artifact.project_stops"):
		fact := v.stops[args[1].(string)]
		if fact == nil || fact.state == "" || fact.cause.Details().ProjectID.String() != args[0] {
			return cleanupOwnerRow{err: pgx.ErrNoRows}
		}
		if strings.Contains(query, "COALESCE(owner_process_id") {
			return cleanupOwnerRow{values: []any{fact.owner.process, fact.owner.instance}}
		}
		return cleanupOwnerRow{values: []any{string(fact.cause.Details().Action), int64(fact.cause.Details().ProjectVersion), fact.state}}
	case strings.Contains(query, "foreign_owners"):
		ids := map[string]bool{}
		for _, record := range v.calls {
			if record.Project.String() == args[0] && (record.Kind != "read" || args[1].(bool)) && (record.Process.String() != args[2] || record.Instance != args[3]) {
				ids[record.Process.String()] = true
			}
		}
		out := make([]string, 0, len(ids))
		for id := range ids {
			out = append(out, id)
		}
		sort.Strings(out)
		return artifactCallJSON(out)
	case strings.Contains(query, "jsonb_agg(record"):
		out := make([]artifactCallRecord, 0)
		capture := strings.Contains(query, "admission_version<")
		var deleteReads bool
		if capture {
			deleteReads = args[2].(bool)
		} else {
			deleteReads = args[1].(bool)
		}
		for id, record := range v.calls {
			if record.Project.String() != args[0] || record.Joined || record.Kind == "read" && !deleteReads || capture && v.admitted[id] >= args[1].(int64) {
				continue
			}
			out = append(out, record)
		}
		sort.Slice(out, func(a, b int) bool { return out[a].ID.String() < out[b].ID.String() })
		return artifactCallJSON(out)
	case strings.HasPrefix(query, "SELECT revoked_by"):
		return cleanupOwnerRow{values: []any{v.revoked[args[0].(string)]}}
	case strings.HasPrefix(query, "SELECT jsonb_build_object"):
		record, ok := v.calls[args[0].(string)]
		if !ok {
			return cleanupOwnerRow{err: pgx.ErrNoRows}
		}
		return artifactCallJSON(record)
	default:
		return cleanupOwnerRow{err: errors.New("unexpected Artifact ownership projection")}
	}
}
func (v *artifactCallFixture) exec(ctx context.Context, tx f.Tx, query string, args []any) (pgconn.CommandTag, error) {
	if !strings.Contains(query, "agenteam_artifact.project_") {
		return v.cleanupOwnerFixture.exec(ctx, tx, query, args)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	switch {
	case strings.HasPrefix(query, "INSERT INTO agenteam_artifact.project_work"):
		id, err := f.ParseID[artifactCallID](args[0].(string))
		p, pErr := f.ParseID[i.Project](args[1].(string))
		process, processErr := f.ParseID[oc.Process](args[2].(string))
		if err != nil || pErr != nil || processErr != nil {
			return pgconn.CommandTag{}, errors.New("invalid call identity")
		}
		v.calls[id.String()] = artifactCallRecord{ID: id, Project: p, Process: process, Instance: args[3].(string), Kind: args[4].(string)}
		for _, stop := range v.stops {
			if stop.state != "" && stop.cause.Details().ProjectID == p && int64(stop.cause.Details().ProjectVersion) > v.admitted[id.String()] {
				v.admitted[id.String()] = int64(stop.cause.Details().ProjectVersion)
			}
		}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.HasPrefix(query, "UPDATE agenteam_artifact.project_work SET joined_at"):
		record, ok := v.calls[args[0].(string)]
		if !ok || record.Joined || len(args) == 5 && (record.Project.String() != args[1] || record.Process.String() != args[2] || record.Instance != args[3] || record.Kind != args[4]) || len(args) == 2 && record.Process.String() != args[1] {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		record.Joined = true
		v.calls[record.ID.String()] = record
	case strings.HasPrefix(query, "UPDATE agenteam_artifact.project_work SET revoked_by"):
		v.revoked[args[0].(string)] = true
	case strings.HasPrefix(query, "INSERT INTO agenteam_artifact.project_stops"):
		fact := v.stops[args[1].(string)]
		if fact == nil || fact.cause.Details().ProjectID.String() != args[0] || string(fact.cause.Details().Action) != args[2] || int64(fact.cause.Details().ProjectVersion) != args[3] {
			return pgconn.CommandTag{}, errors.New("stop tuple changed")
		}
		fact.state, fact.owner = "stopping", artifactStopOwner{args[4].(string), args[5].(string)}
	case strings.HasPrefix(query, "UPDATE agenteam_artifact.project_stops SET state="):
		v.stops[args[1].(string)].state = "stopped"
	case strings.HasPrefix(query, "UPDATE agenteam_artifact.project_stops SET owner_process_id="):
		fact := v.stops[args[1].(string)]
		if fact.owner != (artifactStopOwner{args[4].(string), args[5].(string)}) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		fact.owner = artifactStopOwner{args[2].(string), args[3].(string)}
	default:
		return pgconn.CommandTag{}, errors.New("unexpected Artifact ownership mutation")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type artifactCallBarrier struct {
	entered chan struct{}
	release chan struct{}
	enter   sync.Once
	leave   sync.Once
}

func newArtifactCallBarrier() *artifactCallBarrier {
	return &artifactCallBarrier{entered: make(chan struct{}), release: make(chan struct{})}
}
func (b *artifactCallBarrier) hold() {
	b.enter.Do(func() { close(b.entered) })
	<-b.release
}
func (b *artifactCallBarrier) unblock() { b.leave.Do(func() { close(b.release) }) }

type artifactCallBody struct{ close *artifactCallBarrier }

func (*artifactCallBody) Read([]byte) (int, error) { return 0, io.EOF }
func (b *artifactCallBody) Close() error {
	if b.close != nil {
		b.close.hold()
	}
	return nil
}
func artifactCallPending(t *testing.T, report oc.ObjectStopReport, err error, cause oc.ProjectStopCause) {
	t.Helper()
	if err != nil || !report.Matches(oc.ArtifactStopComponent, cause) || report.Details().State != oc.ProjectStopPending {
		t.Fatal("original work incorrectly passed Stop", report.Details(), err)
	}
}

func TestArtifactProjectCallOwnsOriginalReturns(t *testing.T) {
	for _, stage := range []string{"registration-return", "registration-rejected-body-close", "body-close", "checkpoint-return"} {
		t.Run(stage, func(t *testing.T) {
			v, p := newArtifactCallFixture(t)
			stop := v.stop(t, p)
			barrier := newArtifactCallBarrier()
			body := &artifactCallBody{}
			if stage == "body-close" || stage == "registration-rejected-body-close" {
				body.close = barrier
			}
			v.rejectRegistration = stage == "registration-rejected-body-close"
			checkpointCalls := 0
			v.store.before = func(_ context.Context, cause f.TransactionCause) *f.CommitResult {
				if cause.Details().Owner == "artifact-call-return" {
					checkpointCalls++
				}
				return nil
			}
			v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if stage == "registration-return" && cause.Details().Owner == "artifact-call-register" || stage == "checkpoint-return" && cause.Details().Owner == "artifact-call-return" {
					barrier.hold()
				}
				return result
			}
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				barrier.unblock()
				if !joined {
					cleanupOwnerWait(t, done)
				}
			})
			invocation := v.invocation(t, p)
			go func() {
				_, err := v.service.UploadPayload(context.Background(), invocation, ac.UploadTarget{}, body)
				done <- err
			}()
			cleanupOwnerWait(t, barrier.entered)
			v.pair.state.mu.Lock()
			count := len(v.pair.state.owners)
			returned, settling := false, false
			registrationRejected := false
			for _, call := range v.pair.state.owners {
				returned, settling = call.returned, call.settling
				registrationRejected = call.registration.State() == f.NotCommitted
			}
			v.pair.state.mu.Unlock()
			if count != 1 || returned != (stage == "checkpoint-return") || settling != (stage == "checkpoint-return") {
				t.Fatal("outer call no longer owns its original return", count, returned, settling)
			}
			if v.rejectRegistration && (!registrationRejected || len(v.calls) != 0) {
				t.Fatal("registration rejection did not retain only the original local owner")
			}
			report, err := v.service.RequestProjectStop(context.Background(), stop.actor, stop.cause)
			artifactCallPending(t, report, err, stop.cause)
			v.pair.Stop()
			if v.pair.Joined() {
				t.Fatal("Stop/cancellation substituted for original return")
			}
			barrier.unblock()
			err = cleanupOwnerWait(t, done)
			joined = true
			if err == nil {
				t.Fatal("invalid upload unexpectedly executed business")
			}
			if !v.pair.Joined() {
				t.Fatal("actual outer return did not release original call")
			}
			if v.rejectRegistration && checkpointCalls != 0 {
				t.Fatal("rejected registration checkpointed a nonexistent ledger", checkpointCalls)
			}
		})
	}
	t.Run("skipped-callbacks", func(t *testing.T) {
		v, p := newArtifactCallFixture(t)
		invocation := v.invocation(t, p)
		record := artifactCallRecord{ID: newTestID[artifactCallID](t), Project: p, Process: v.process, Instance: v.pair.state.instance.String(), Kind: "creation"}
		v.store.before = func(context.Context, f.TransactionCause) *f.CommitResult {
			result := f.CommittedResult()
			return &result
		}
		result, err := v.pair.state.register(context.Background(), invocation, record)
		if result.State() != f.Committed || err == nil || len(v.calls) != 0 {
			t.Fatal("synthetic Committed authorized registration without its callback")
		}
		v.store.before = nil
		_, done, err := v.service.beginProjectCall(context.Background(), invocation, "creation")
		t.Cleanup(done)
		if err != nil {
			t.Fatal(err)
		}
		v.store.before = func(context.Context, f.TransactionCause) *f.CommitResult {
			result := f.CommittedResult()
			return &result
		}
		done()
		v.pair.Stop()
		if v.pair.Joined() {
			t.Fatal("synthetic Committed released checkpoint without its callback")
		}
		v.store.before = nil
		if err := v.pair.Drain(context.Background()); err != nil || !v.pair.Joined() {
			t.Fatal("original checkpoint did not release its owner", err)
		}
	})
}

func TestArtifactProjectCallRetainsOriginalUnknown(t *testing.T) {
	t.Run("registration", func(t *testing.T) {
		v, p := newArtifactCallFixture(t)
		barrier := newArtifactCallBarrier()
		attempt := newTestID[f.TransactionAttempt](t)
		var original f.CommitResult
		v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			switch cause.Details().Owner {
			case "artifact-call-register":
				original = f.UnknownResult(attempt, cause)
				return original
			case "artifact-call-return":
				return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
			default:
				return result
			}
		}
		done := make(chan error, 1)
		joined := false
		t.Cleanup(func() {
			barrier.unblock()
			if !joined {
				cleanupOwnerWait(t, done)
			}
		})
		invocation := v.invocation(t, p)
		go func() {
			_, err := v.service.UploadPayload(context.Background(), invocation, ac.UploadTarget{}, &artifactCallBody{close: barrier})
			done <- err
		}()
		cleanupOwnerWait(t, barrier.entered)
		v.pair.state.mu.Lock()
		var call *artifactCall
		for _, current := range v.pair.state.owners {
			call = current
		}
		valid := call != nil && !call.returned && call.registration.AttemptID() == attempt && call.record.ID.String() == original.Cause().Details().RecoveryRunID
		v.pair.state.mu.Unlock()
		if !valid {
			t.Fatal("registration Unknown lost original identity before body.Close returned")
		}
		business := false
		cause, _ := f.NewRecoveryCause("controlled-business", newTestID[struct{}](t).String(), p.String())
		ctx := context.WithValue(context.Background(), artifactCallContextKey{}, artifactCallWitness{v.pair.state, call})
		result := v.service.within(ctx, invocationSubject(invocation), i.Mutate, cause, nil, nil, func(context.Context, f.Tx, oc.LockedAccess) error { business = true; return nil })
		if result.State() != f.NotCommitted || business {
			t.Fatal("registration Unknown admitted business")
		}
		barrier.unblock()
		err := cleanupOwnerWait(t, done)
		joined = true
		requireCleanupOwnerUnknown(t, oc.ProjectCleanupResult{}, err, original)
		v.pair.Stop()
		if v.pair.Joined() {
			t.Fatal("failed checkpoint discarded registration owner")
		}
		v.store.after = nil
		if err := v.pair.Drain(context.Background()); err != nil || !v.pair.Joined() {
			t.Fatal("real checkpoint did not settle original registration", err)
		}
	})
	t.Run("first-business-result", func(t *testing.T) {
		v, p := newArtifactCallFixture(t)
		invocation := v.invocation(t, p)
		ctx, done, err := v.service.beginProjectCall(context.Background(), invocation, "creation")
		t.Cleanup(done)
		if err != nil {
			t.Fatal(err)
		}
		attempt := newTestID[f.TransactionAttempt](t)
		cause, _ := f.NewRecoveryCause("controlled-business", newTestID[struct{}](t).String(), p.String())
		original := f.UnknownResult(attempt, cause)
		later := f.UnknownResult(newTestID[f.TransactionAttempt](t), cause)
		called := 0
		v.store.after = func(_ context.Context, got f.TransactionCause, result f.CommitResult) f.CommitResult {
			if got.Details().Owner == "controlled-business" {
				if called > 1 {
					return later
				}
				return original
			}
			return result
		}
		business := func(context.Context, f.Tx, oc.LockedAccess) error { called++; return nil }
		result := v.service.within(ctx, invocationSubject(invocation), i.Mutate, cause, nil, nil, business)
		if called != 1 || result.AttemptID() != attempt {
			t.Fatal("controlled business did not execute under its registered live Tx")
		}
		result = v.service.within(ctx, invocationSubject(invocation), i.Mutate, cause, nil, nil, business)
		if called != 2 || result.AttemptID() != later.AttemptID() {
			t.Fatal("later original Store result was not observed")
		}
		requireCleanupOwnerUnknown(t, oc.ProjectCleanupResult{}, v.service.projectCallError(ctx, commitError(later)), original)
		v.store.after = nil
		done()
	})
}

func TestArtifactProjectStopCancelsOnlyConfirmedCapturedCalls(t *testing.T) {
	v, p := newArtifactCallFixture(t)
	b := v.add(t)
	stop := v.stop(t, p)
	ctxA, doneA, err := v.service.beginProjectCall(context.Background(), v.invocation(t, p), "creation")
	t.Cleanup(doneA)
	if err != nil {
		t.Fatal(err)
	}
	ctxB, doneB, err := v.service.beginProjectCall(context.Background(), v.invocation(t, b), "upload")
	t.Cleanup(doneB)
	if err != nil {
		t.Fatal(err)
	}
	barrier := newArtifactCallBarrier()
	v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if cause.Details().Owner == "artifact-project-stop" {
			v.mu.Lock()
			captured := len(v.revoked) > 0
			v.mu.Unlock()
			if captured {
				barrier.hold()
			}
		}
		return result
	}
	type output struct {
		report oc.ObjectStopReport
		err    error
	}
	returned := make(chan output, 1)
	joined := false
	t.Cleanup(func() {
		barrier.unblock()
		if !joined {
			cleanupOwnerWait(t, returned)
		}
	})
	go func() {
		report, err := v.service.RequestProjectStop(context.Background(), stop.actor, stop.cause)
		returned <- output{report, err}
	}()
	cleanupOwnerWait(t, barrier.entered)
	if ctxA.Err() != nil || ctxB.Err() != nil {
		t.Fatal("cancellation escaped before the gate's actual Store return")
	}
	if _, err := v.service.RequestProjectStop(context.Background(), stop.actor, stop.cause); err == nil {
		t.Fatal("same instance borrowed active Stop")
	} else {
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.ResourceBusy {
			t.Fatal(err)
		}
	}
	barrier.unblock()
	got := cleanupOwnerWait(t, returned)
	joined = true
	artifactCallPending(t, got.report, got.err, stop.cause)
	if !errors.Is(ctxA.Err(), context.Canceled) || ctxB.Err() != nil {
		t.Fatal("confirmed stop did not isolate Project A cancellation")
	}
	v.pair.state.mu.Lock()
	for _, call := range v.pair.state.owners {
		if call.returned {
			t.Error("context cancellation marked original returned")
		}
	}
	v.pair.state.mu.Unlock()
	v.store.after = nil
	doneA()
	report, err := v.service.InspectProjectStop(context.Background(), stop.actor, stop.cause)
	if err != nil || report.Details().State != oc.ProjectStopped || ctxB.Err() != nil {
		t.Fatal("returned A failed Stop or B was canceled", err)
	}
	doneB()
}

func TestArtifactProjectStopRejectsOtherInstanceReceipts(t *testing.T) {
	for _, kind := range []string{"call-row", "stopped-receipt"} {
		t.Run(kind, func(t *testing.T) {
			v, p := newArtifactCallFixture(t)
			stop := v.stop(t, p)
			other := newTestID[artifactInstanceID](t).String()
			if kind == "call-row" {
				record := artifactCallRecord{ID: newTestID[artifactCallID](t), Project: p, Process: v.process, Instance: other, Kind: "creation", Joined: true}
				v.calls[record.ID.String()] = record
				report, err := v.service.RequestProjectStop(context.Background(), stop.actor, stop.cause)
				artifactCallPending(t, report, err, stop.cause)
			} else {
				barrier := newArtifactCallBarrier()
				var once sync.Once
				v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
					v.mu.Lock()
					stopped := stop.state == "stopped"
					v.mu.Unlock()
					if cause.Details().Owner == "artifact-project-stop" && stopped {
						first := false
						once.Do(func() { first = true })
						if first {
							barrier.hold()
						}
					}
					return result
				}
				type output struct {
					report oc.ObjectStopReport
					err    error
				}
				done := make(chan output, 1)
				joined := false
				t.Cleanup(func() {
					barrier.unblock()
					if !joined {
						cleanupOwnerWait(t, done)
					}
				})
				go func() {
					report, err := v.service.RequestProjectStop(context.Background(), stop.actor, stop.cause)
					done <- output{report, err}
				}()
				cleanupOwnerWait(t, barrier.entered)
				otherState := &serviceState{store: v.store, owners: v.state.owners}
				otherService := &Service{data: func() *serviceState { return otherState }}
				otherPair, err := newProjectCallOwnership(otherService, v, v.process, v)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					barrier.unblock()
					if !joined {
						cleanupOwnerWait(t, done)
						joined = true
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := otherPair.Drain(ctx); err != nil {
						t.Error(err)
					}
				})
				report, err := otherService.InspectProjectStop(context.Background(), stop.actor, stop.cause)
				artifactCallPending(t, report, err, stop.cause)
				v.pair.Stop()
				if v.pair.Joined() {
					t.Fatal("stopped receipt substituted for its original Store return")
				}
				barrier.unblock()
				got := cleanupOwnerWait(t, done)
				joined = true
				if got.err != nil || got.report.Details().State != oc.ProjectStopped || !v.pair.Joined() {
					t.Fatal("original stopped receipt did not finish", got.err)
				}
			}
			if v.proofChecks != 0 {
				t.Fatal("same process borrowed a foreign-process death proof")
			}
		})
	}
}

func TestArtifactProjectStopIncludesCleanupAndSources(t *testing.T) {
	t.Run("cleanup-original-return", func(t *testing.T) {
		v, p := newArtifactCallFixture(t)
		stop := v.stop(t, p)
		report, err := v.service.RequestProjectStop(context.Background(), stop.actor, stop.cause)
		if err != nil || !report.Matches(oc.ArtifactStopComponent, stop.cause) || report.Details().State != oc.ProjectStopped {
			t.Fatal("current Delete did not obtain its original stopped receipt", err)
		}
		operation, err := f.ParseID[oc.CleanupOperation](stop.cause.Details().OperationID.String())
		if err != nil {
			t.Fatal(err)
		}
		cleanupCause, err := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: p, OperationID: operation, Version: stop.cause.Details().ProjectVersion})
		if err != nil {
			t.Fatal(err)
		}
		v.facts[p].actor, v.facts[p].cause = stop.actor, cleanupCause
		v.facts[p].state = "completed"
		barrier := newArtifactCallBarrier()
		v.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if cause.Details().Owner == "artifact-cleanup" {
				barrier.hold()
			}
			return result
		}
		done := make(chan error, 1)
		joined := false
		t.Cleanup(func() {
			barrier.unblock()
			if !joined {
				cleanupOwnerWait(t, done)
			}
		})
		go func() {
			out, err := v.run(context.Background(), v.service, p)
			if err == nil && out.State != oc.CleanupCompleted {
				err = errors.New("current Delete receipt did not permit completed cleanup replay")
			}
			done <- err
		}()
		cleanupOwnerWait(t, barrier.entered)
		v.pair.Stop()
		if v.pair.Joined() || v.facts[p].checks == 0 {
			t.Fatal("cleanup original bypassed its gate or was not owned")
		}
		barrier.unblock()
		err = cleanupOwnerWait(t, done)
		joined = true
		if err != nil {
			t.Fatal("owned cleanup failed", err)
		}
		if !v.pair.Joined() {
			t.Fatal("cleanup original did not release control on actual return")
		}
	})
	t.Run("source-project-isolation", func(t *testing.T) {
		v, p := newArtifactCallFixture(t)
		b := v.add(t)
		stopA, stopB := v.stop(t, p), v.stop(t, b)
		key := digestBytes([]byte("controlled live source"))
		lease, err := oc.NewSourceLease(oc.NewSourceIssuer(), newTestID[oc.Lease](t))
		if err != nil {
			t.Fatal(err)
		}
		v.state.leases[key], v.state.leaseProjects[key] = lease, p
		v.state.leaseSources[key] = "" // Known System source; only destination A owns it.
		report, err := v.service.RequestProjectStop(context.Background(), stopA.actor, stopA.cause)
		artifactCallPending(t, report, err, stopA.cause)
		report, err = v.service.RequestProjectStop(context.Background(), stopB.actor, stopB.cause)
		if err != nil || report.Details().State != oc.ProjectStopped {
			t.Fatal("A source blocked B", err)
		}
		v.state.leaseSources[key] = b.String()
		report, err = v.service.InspectProjectStop(context.Background(), stopB.actor, stopB.cause)
		artifactCallPending(t, report, err, stopB.cause)
		v.state.leaseSources[key] = ""
		delete(v.state.leaseProjects, key)
		report, err = v.service.InspectProjectStop(context.Background(), stopB.actor, stopB.cause)
		artifactCallPending(t, report, err, stopB.cause)
	})
	t.Run("legacy-new-is-unbound", func(t *testing.T) {
		v, p := newArtifactCallFixture(t)
		stop := v.stop(t, p)
		keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}]}`)
		if err != nil {
			t.Fatal(err)
		}
		legacy, err := New(v.store, v.state.owners, &struct{ oc.Objects }{}, &struct{ oc.Uploads }{}, &struct{ oc.Cleaner }{}, nil, nil, &struct{ au.Appender }{}, keys)
		if err != nil {
			t.Fatal(err)
		}
		report, err := legacy.RequestProjectStop(context.Background(), stop.actor, stop.cause)
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound || report.Details().State != oc.ProjectStopPending || report.Details().SafeReason != oc.ProjectStopDependencyUnbound {
			t.Fatal("legacy constructor claimed Stop ownership", report.Details(), err)
		}
	})
}
