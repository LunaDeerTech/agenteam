package project

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled receipt rows exercise the real receipt decoder and original Go
// returns. They do not simulate the full canonical read set or PG rollback.
// The complete finalFacts queries, atomic deletes and receipt belong to PG.
type finalizeTestStore struct {
	readerStore
	live, skip, remaining bool
	project               c.ProjectID
	receipt               *c.DeletionReceiptData
	readErr               error
	reads, writes         []string
	exec                  func(context.Context, string, []any, int) (pgconn.CommandTag, error)
}

func (s *finalizeTestStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	s.tx, s.locks = f.NewTx(), nil
	if s.skip {
		return f.CommittedResult()
	}
	s.live = true
	err := fn(ctx, s.tx)
	s.live = false
	if err != nil {
		var failure *f.Fault
		if !errors.As(err, &failure) {
			failure = f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err)
		}
		return f.NotCommittedResult(failure)
	}
	if s.after != nil {
		return s.after(cause)
	}
	return f.CommittedResult()
}
func (s *finalizeTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.live || tx != s.tx {
		return nil, errors.New("not original live transaction")
	}
	return s, nil
}
func (s *finalizeTestStore) RequireHeldLocks(_ context.Context, tx f.Tx, requested []f.LockRequest) error {
	if !s.live || tx != s.tx || len(requested) != 1 || len(s.locks) != 1 ||
		f.CompareLockKeys(requested[0].Key, s.locks[0].Key) != 0 || s.locks[0].Mode != f.Exclusive {
		return errors.New("original Project EX not held")
	}
	return nil
}
func (s *finalizeTestStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	s.reads = append(s.reads, query)
	if !s.live {
		return rowFunc(func(...any) error { return errors.New("expired executor") })
	}
	if s.readErr != nil {
		return rowFunc(func(...any) error { return s.readErr })
	}
	if strings.Contains(query, "FROM agenteam_project.deletion_receipts") && s.receipt != nil {
		d := s.receipt
		if len(args) != 1 || args[0] != s.project.String() {
			return rowFunc(func(...any) error { return errors.New("receipt project binding") })
		}
		return valuesRow(d.OperationID.String(), d.DeletedProjectID.String(), d.OriginalOwnerUserID.String(), d.CommandKeyHash, d.RequestDigest, d.CompletedAt.Time(), "completed")
	}
	if strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_project.projects") {
		return valuesRow(s.remaining)
	}
	return rowFunc(func(...any) error { return pgx.ErrNoRows })
}
func (s *finalizeTestStore) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if !s.live {
		return pgconn.CommandTag{}, errors.New("expired executor")
	}
	s.writes = append(s.writes, query)
	if s.exec == nil {
		return pgconn.CommandTag{}, errors.New("unexpected write during confirmation")
	}
	return s.exec(ctx, query, args, len(s.writes)-1)
}

func newFinalizeTest(t *testing.T) (*LifecycleCleanupDriver, *finalizeTestStore, *lifecycleFinalizeRound) {
	t.Helper()
	store := &finalizeTestStore{}
	command := lifecycleTestCommand(t, c.Delete)
	command.state = "completed"
	a, err := NewLifecycleAuthority(store, command.manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := NewAuthority(store, AuthorityDependencies{Lifecycle: a, Sessions: sessionFunc(func(context.Context, f.Tx, i.Actor) error { return nil })})
	if err != nil {
		t.Fatal(err)
	}
	process := &testProcess{id: testID[oc.Process](t)}
	serviceData := &serviceState{store: store, deps: Dependencies{Authority: auth, Processes: process}, calls: map[*call]struct{}{}, joined: map[string]bool{}, changed: make(chan struct{}), slots: make(chan struct{}, 1)}
	service := &Service{data: func() *serviceState { return serviceData }}
	d, err := NewLifecycleFinalizeDriver(store, service, a, process)
	if err != nil {
		t.Fatal(err)
	}
	ref := testProject(t)
	ref.ID, ref.Version, ref.Lifecycle = command.project, 2, c.Deleting
	ref.OwnerUserID, err = f.ParseID[i.User](command.user)
	if err != nil {
		t.Fatal(err)
	}
	op := command.plan.OperationID
	store.project = ref.ID
	r := &lifecycleRecord{owner: command.user, manifest: command.manifest, digest: command.plan.ManifestDigest, cleanupStage: "final",
		operation: c.LifecycleOperation{ID: op, ProjectID: ref.ID, Action: c.Delete, ProjectVersion: ref.Version, Version: 9, State: c.OperationCleaning, CreatedAt: ref.CreatedAt, UpdatedAt: ref.UpdatedAt}}
	for _, entry := range command.manifest.Entries() {
		r.participants = append(r.participants, lifecycleParticipantRecord{name: entry.Name, version: entry.ContractVersion, stop: "stopped", cleanup: "completed"})
	}
	if err = finalizeProgress(r); err != nil {
		t.Fatal("legal final-stage fixture", err)
	}
	p := &projectRecord{ref: ref, initialized: true, creation: testID[c.Creation](t), operation: &op}
	claim := lifecycleStopClaim{project: ref.ID, operation: op, process: process.id, attempt: testID[struct{}](t).String(), fence: 2, phase: "running"}
	prior := claim
	prior.attempt, prior.fence, prior.phase = testID[struct{}](t).String(), 1, "terminal"
	facts := &finalizeFacts{project: p, operation: r, command: command, counts: [4]int64{3, 1, 2, 2 * int64(len(r.participants))}, claims: []finalizeClaim{
		{Kind: "creation", ID: p.creation.String(), Process: process.id.String(), Attempt: testID[struct{}](t).String(), Fence: 1, Phase: "terminal"}, finalClaim(prior),
	}}
	hash, err := c.DeletionCommandKeyHash(ref.ID, ref.OwnerUserID, command.key)
	if err != nil {
		t.Fatal(err)
	}
	receipt := c.DeletionReceiptData{OperationID: op, DeletedProjectID: ref.ID, OriginalOwnerUserID: ref.OwnerUserID, CommandKeyHash: hash, RequestDigest: command.semantic, CompletedAt: ref.UpdatedAt}
	if err = receipt.Validate(); err != nil {
		t.Fatal(err)
	}
	stored := receipt
	store.receipt = &stored
	unknown := f.UnknownResult(testID[f.TransactionAttempt](t), claim.cause())
	return d, store, &lifecycleFinalizeRound{claim: claim, before: facts, registered: true, unknown: &unknown, receipt: &receipt}
}

func TestLifecycleFinalizeConstructionAndProgress(t *testing.T) {
	for _, mode := range []string{"nil-service", "other-store", "other-lifecycle", "other-process-port", "archive", "not-final", "missing-participant", "pending-cleanup", "not-stopped", "reason"} {
		t.Run(mode, func(t *testing.T) {
			d, store, round := newFinalizeTest(t)
			st := d.state
			var err error
			r := round.before.operation
			switch mode {
			case "nil-service":
				_, err = NewLifecycleFinalizeDriver(store, nil, st.authority, st.processes)
			case "other-store":
				_, err = NewLifecycleFinalizeDriver(&finalizeTestStore{}, st.finalizer, st.authority, st.processes)
			case "other-lifecycle":
				other, e := NewLifecycleAuthority(store, r.manifest, nil)
				if e != nil {
					t.Fatal(e)
				}
				_, err = NewLifecycleFinalizeDriver(store, st.finalizer, other, st.processes)
			case "other-process-port":
				_, err = NewLifecycleFinalizeDriver(store, st.finalizer, st.authority, &testProcess{id: st.process})
			default:
				switch mode {
				case "archive":
					r.operation.Action = c.Archive
				case "not-final":
					r.cleanupStage = "audit"
				case "missing-participant":
					r.participants = r.participants[:len(r.participants)-1]
				case "pending-cleanup":
					r.participants[0].cleanup = "pending"
				case "not-stopped":
					r.participants[0].stop = "pending"
				case "reason":
					r.participants[0].reason = c.ReasonWorkPending
				}
				err = finalizeProgress(r)
			}
			if err == nil || store.calls != 0 {
				t.Fatal("incomplete/unbound finalization accepted", err)
			}
		})
	}
}

func TestLifecycleFinalizeSnapshotBindings(t *testing.T) {
	for _, mode := range []string{"same", "registered", "project", "owner", "version", "creation", "manifest", "operation-version", "command-key", "command-plan", "count", "claim-attempt", "claim-process", "claim-fence", "claim-phase", "extra-claim"} {
		t.Run(mode, func(t *testing.T) {
			_, _, round := newFinalizeTest(t)
			before := round.before
			actual := *before
			p, op, cmd := *before.project, *before.operation, *before.command
			actual.project, actual.operation, actual.command, actual.claims = &p, &op, &cmd, slices.Clone(before.claims)
			var replacement *lifecycleStopClaim
			switch mode {
			case "registered":
				replacement = &round.claim
				actual.claims[1] = finalClaim(round.claim)
			case "project":
				p.ref.ID = testID[i.Project](t)
			case "owner":
				p.ref.OwnerUserID = testID[i.User](t)
			case "version":
				p.ref.Version++
			case "creation":
				p.creation = testID[c.Creation](t)
			case "manifest":
				op.digest = digest([]byte("other manifest"))
			case "operation-version":
				op.operation.Version++
			case "command-key":
				cmd.key = "other-original-key"
			case "command-plan":
				cmd.plan.OperationID = testID[c.Operation](t)
			case "count":
				actual.counts[0]++
			case "claim-attempt":
				actual.claims[1].Attempt = testID[struct{}](t).String()
			case "claim-process":
				actual.claims[1].Process = testID[oc.Process](t).String()
			case "claim-fence":
				actual.claims[1].Fence++
			case "claim-phase":
				actual.claims[1].Phase = "running"
			case "extra-claim":
				actual.claims = append(actual.claims, actual.claims[0])
			}
			if sameFinalizeFacts(before, &actual, replacement) != (mode == "same" || mode == "registered") {
				t.Fatal("snapshot comparison lost original binding")
			}
			if before.claims[1].Phase != "terminal" {
				t.Fatal("comparison mutated original claim")
			}
		})
	}
}

func TestLifecycleFinalizeOriginalReturns(t *testing.T) {
	d, store, round := newFinalizeTest(t)
	s := d.state.finalizer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run, done, err := s.begin(ctx, round.claim.project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	attempt := round.before.claims[0].Attempt
	s.creationReturned(run, attempt)
	cancel()
	hasCode(t, d.state.finalReturns(context.Background(), round.before), f.ResourceBusy)
	if s.state().joined[attempt] {
		t.Fatal("cancellation/private callback replaced outer return")
	}
	done()
	if err = d.state.finalReturns(context.Background(), round.before); err != nil {
		t.Fatal(err)
	}
	// A copied context after the original call ends cannot publish another join.
	s.creationReturned(run, "not-an-original-attempt")
	if s.state().joined["not-an-original-attempt"] {
		t.Fatal("expired outer call published evidence")
	}
	round.before.claims[1].Phase = "running"
	hasCode(t, d.state.finalReturns(context.Background(), round.before), f.ResourceBusy)
	foreign := testID[oc.Process](t)
	round.before.claims[1].Process = foreign.String()
	process := d.state.processes.(*testProcess)
	hasCode(t, d.state.finalReturns(context.Background(), round.before), f.ResourceBusy)
	if process.requested != foreign || store.calls != 0 {
		t.Fatal("death proof lost original process or entered writer Tx")
	}
	process.stopped = true
	if err = d.state.finalReturns(context.Background(), round.before); err != nil {
		t.Fatal("returned exact foreign-death proof", err)
	}
}

func TestLifecycleFinalizeDeleteSequence(t *testing.T) {
	steps := []string{"deletion_receipts", "commands", "lifecycle_participants", "lifecycle_operations", "creations", "work_claims", "projects"}
	for _, mode := range []string{"complete", "sql-error", "wrong-count", "cancel-after-delete"} {
		indices := []int{0}
		if mode == "sql-error" || mode == "wrong-count" {
			indices = []int{0, 1, 2, 3, 4, 5, 6}
		}
		if mode == "cancel-after-delete" {
			indices = []int{3}
		}
		for _, index := range indices {
			t.Run(fmt.Sprintf("%s/%s", mode, steps[index]), func(t *testing.T) {
				d, store, round := newFinalizeTest(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				counts := []int64{1, round.before.counts[0], round.before.counts[3], round.before.counts[2], round.before.counts[1], int64(len(round.before.claims)), 1}
				original := errors.New("original delete failure")
				store.exec = func(_ context.Context, q string, args []any, n int) (pgconn.CommandTag, error) {
					if n >= len(steps) || !strings.Contains(q, "agenteam_project."+steps[n]) {
						t.Errorf("delete order at %d", n)
					}
					if n == 0 {
						r := round.receipt
						if len(args) != 6 || args[0] != r.OperationID.String() || args[1] != r.DeletedProjectID.String() || args[2] != r.OriginalOwnerUserID.String() || args[3] != r.CommandKeyHash.String() || args[4] != r.RequestDigest.String() || !args[5].(time.Time).Equal(r.CompletedAt.Time()) {
							t.Error("receipt fields changed")
						}
					} else if len(args) != 1 || args[0] != round.claim.project.String() {
						t.Error("delete escaped original P")
					}
					if n == index && mode == "sql-error" {
						return pgconn.CommandTag{}, original
					}
					count := counts[n]
					if n == index && mode == "wrong-count" {
						count--
					}
					if n == index && mode == "cancel-after-delete" {
						cancel()
					}
					return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", count)), nil
				}
				var observed error
				result := d.state.finalTx(ctx, round, func(ctx context.Context, x postgres.SQLExecutor) error {
					observed = writeFinalize(ctx, x, round, round.before)
					return observed
				})
				if mode == "complete" {
					if observed != nil || result.State() != f.Committed || len(store.writes) != 7 {
						t.Fatal("complete controlled sequence", observed)
					}
				} else if observed == nil || result.State() != f.NotCommitted || len(store.writes) != index+1 {
					t.Fatal("failure advanced a later delete", observed, len(store.writes))
				}
				if mode == "sql-error" && !errors.Is(observed, original) {
					t.Fatal("lost original SQL error", observed)
				}
			})
		}
	}
}

func requireFinalizeUnknown(t *testing.T, err error, original f.CommitResult) {
	t.Helper()
	hasCode(t, err, f.CommitUnknown)
	saved, ok := UnknownAttempt(err)
	if !ok {
		t.Fatal("missing original Unknown result")
	}
	actual, want := saved.Cause().Details(), original.Cause().Details()
	if saved.AttemptID() != original.AttemptID() || actual.Kind != want.Kind || actual.JobType != want.JobType || actual.JobID != want.JobID || actual.JobAttemptID != want.JobAttemptID {
		t.Fatal("first Unknown provenance changed")
	}
}

// Seed the private in-memory state of the original ambiguous round only.
// This is not a public issuer or evidence that a database transaction ran.
func retainFinalizeTestRound(t *testing.T, d *LifecycleCleanupDriver, round *lifecycleFinalizeRound) {
	t.Helper()
	a := d.state.authority
	end, err := a.beginCleanupCall(context.Background(), round.claim.project)
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	if err = a.retainFinalize(round); err != nil {
		t.Fatal(err)
	}
	d.state.owners[round.claim.operation] = lifecycleCleanupOwner{project: round.claim.project, final: round}
	t.Cleanup(func() { a.releaseFinalize(round) })
}

func TestLifecycleFinalizeReceiptConfirmation(t *testing.T) {
	for _, mode := range []string{"exact", "operation", "project", "owner", "key", "digest", "time", "remaining", "registration-unknown", "missing-receipt", "scan-error", "unknown", "not-committed", "skipped-callback", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			d, store, round := newFinalizeTest(t)
			original := *round.unknown
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "operation":
				store.receipt.OperationID = testID[c.Operation](t)
			case "project":
				store.receipt.DeletedProjectID = testID[i.Project](t)
			case "owner":
				store.receipt.OriginalOwnerUserID = testID[i.User](t)
			case "key":
				store.receipt.CommandKeyHash = digest([]byte("wrong original key"))
			case "digest":
				store.receipt.RequestDigest = digest([]byte("wrong original request"))
			case "time":
				store.receipt.CompletedAt = instant(store.receipt.CompletedAt.Time().Add(time.Microsecond))
			case "remaining":
				store.remaining = true
			case "registration-unknown":
				round.unknownRegistration = true
			case "missing-receipt":
				store.receipt = nil
			case "scan-error":
				store.readErr = errors.New("original receipt read failed")
			case "unknown":
				next := f.UnknownResult(testID[f.TransactionAttempt](t), round.claim.cause())
				store.after = func(f.TransactionCause) f.CommitResult { return next }
			case "not-committed":
				store.after = func(f.TransactionCause) f.CommitResult { return f.NotCommittedResult(fault(f.ResourceBusy)) }
			case "skipped-callback":
				store.skip = true
			case "cancelled":
				cancel()
			}
			applied, err := d.state.confirmFinalize(ctx, round)
			if mode == "exact" {
				if err != nil || !applied || round.unknown != nil {
					t.Fatal("receipt-first confirmation failed", err)
				}
				if len(store.reads) != 2 || strings.Contains(store.reads[0], "FROM agenteam_project.projects") || !strings.HasPrefix(store.reads[1], "SELECT EXISTS") {
					t.Fatal("confirmation required deleted canonical rows")
				}
			} else {
				requireFinalizeUnknown(t, err, original)
				if applied || round.unknown == nil || round.unknown.AttemptID() != original.AttemptID() {
					t.Fatal("failed confirmation released original owner")
				}
			}
			if len(store.writes) != 0 {
				t.Fatal("confirmation performed a write")
			}
		})
	}
}

func TestLifecycleFinalizeConfirmationOriginalReturn(t *testing.T) {
	for _, cancelHeld := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel-%t", cancelHeld), func(t *testing.T) {
			d, store, round := newFinalizeTest(t)
			retainFinalizeTestRound(t, d, round)
			original := *round.unknown
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			store.after = func(f.TransactionCause) f.CommitResult { close(entered); <-release; return f.CommittedResult() }
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			returned := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				unblock()
				if !joined {
					select {
					case <-returned:
					case <-time.After(5 * time.Second):
						t.Error("original confirmation did not return")
					}
				}
			})
			go func() { returned <- d.Run(ctx, round.claim.project, round.claim.operation) }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("original confirmation Store not reached")
			}
			if cancelHeld {
				cancel()
			}
			if round.unknown == nil || len(store.writes) != 0 || store.live {
				t.Fatal("callback completion released Unknown or wrote")
			}
			other, err := NewLifecycleFinalizeDriver(store, d.state.finalizer, d.state.authority, d.state.processes)
			if err != nil {
				t.Fatal(err)
			}
			hasCode(t, other.Run(context.Background(), round.claim.project, round.claim.operation), f.ResourceBusy)
			if store.calls != 1 {
				t.Fatal("second driver reached SQL during original Store return")
			}
			select {
			case <-returned:
				joined = true
				t.Fatal("confirmation skipped original Store return")
			default:
			}
			unblock()
			var got error
			select {
			case got = <-returned:
				joined = true
			case <-time.After(5 * time.Second):
				t.Fatal("confirmation did not join")
			}
			if cancelHeld {
				requireFinalizeUnknown(t, got, original)
				if round.unknown == nil || d.state.owners[round.claim.operation].final != round {
					t.Fatal("cancelled confirmation retired original Unknown")
				}
				store.after = nil
				if err := d.Run(context.Background(), round.claim.project, round.claim.operation); err != nil {
					t.Fatal("later explicit read confirmation", err)
				}
			} else if got != nil || round.unknown != nil {
				t.Fatal("actual return did not settle confirmation", got)
			}
			if len(d.state.owners) != 0 || len(d.state.calls) != 0 || len(d.state.authority.cleanupCalls.finals) != 0 || len(store.writes) != 0 {
				t.Fatal("actual confirmed return left original owner or wrote")
			}
		})
	}
}

func TestLifecycleFinalizeKnownRegistrationRollback(t *testing.T) {
	d, store, round := newFinalizeTest(t)
	round.registered, round.unknown = false, nil
	retainFinalizeTestRound(t, d, round)
	// The original owner has confirmed its registration did not apply. A later
	// explicit registration now encounters a controlled known canonical failure.
	// No fake full canonical query or successful registration is supplied here.
	hasCode(t, d.Run(context.Background(), round.claim.project, round.claim.operation), f.ResourceDeleted)
	if store.calls != 1 || len(store.writes) != 0 || len(d.state.owners) != 0 || len(d.state.calls) != 0 || len(d.state.authority.cleanupCalls.finals) != 0 {
		t.Fatal("known registration rollback retained the old local slot")
	}
}

func TestLifecycleFinalizeRetainedRoundAdmission(t *testing.T) {
	d, store, round := newFinalizeTest(t)
	a := d.state.authority
	p := round.claim.project
	release, err := a.beginCleanupCall(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	if err = a.retainFinalize(round); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.releaseFinalize(round) })
	release() // Original active call returned, but its Unknown remains owned.
	other, err := NewLifecycleFinalizeDriver(store, d.state.finalizer, a, d.state.processes)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, other.Run(context.Background(), p, round.claim.operation), f.ResourceBusy)
	clone := *round
	_, err = a.beginCleanupCall(context.Background(), p, &clone)
	hasCode(t, err, f.ResourceBusy)
	if store.calls != 0 {
		t.Fatal("second driver reached SQL without original round")
	}
	for range 3 {
		b := &lifecycleFinalizeRound{claim: lifecycleStopClaim{project: testID[i.Project](t)}}
		end, e := a.beginCleanupCall(context.Background(), b.claim.project)
		if e != nil {
			t.Fatal("other P blocked", e)
		}
		if e = a.retainFinalize(b); e != nil {
			t.Fatal(e)
		}
		end()
		t.Cleanup(func() { a.releaseFinalize(b) })
	}
	_, err = a.beginCleanupCall(context.Background(), testID[i.Project](t))
	hasCode(t, err, f.ResourceBusy)
	end, err := a.beginCleanupCall(context.Background(), p, round)
	if err != nil {
		t.Fatal("original owner could not confirm at capacity", err)
	}
	end()
	a.releaseFinalize(&clone)
	_, err = a.beginCleanupCall(context.Background(), p)
	hasCode(t, err, f.ResourceBusy)
	a.releaseFinalize(round)
	// Even after admission is free, a new driver cannot infer success from a
	// receipt/absent Project without the original round and exact command facts.
	hasCode(t, other.Run(context.Background(), p, round.claim.operation), f.ResourceDeleted)
	if len(store.writes) != 0 {
		t.Fatal("unowned deleted Project was written")
	}
}

func TestLifecycleFinalizeWaitsForProjectCall(t *testing.T) {
	d, store, round := newFinalizeTest(t)
	a, service := d.state.authority, d.state.finalizer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, done, err := service.begin(ctx, round.claim.project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(done)
	_, doneB, err := service.begin(context.Background(), testID[i.Project](t))
	if err != nil {
		t.Fatal("independent B was blocked", err)
	}
	t.Cleanup(doneB)
	release, err := a.beginCleanupCall(context.Background(), round.claim.project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	hasCode(t, a.retainFinalize(round), f.ResourceBusy)
	cancel()
	hasCode(t, a.retainFinalize(round), f.ResourceBusy)
	if len(a.cleanupCalls.projectCalls) != 2 {
		t.Fatal("cancellation retired original call registration")
	}
	done()
	if err = a.retainFinalize(round); err != nil {
		t.Fatal("original A returned, B should not block registration", err)
	}
	t.Cleanup(func() { a.releaseFinalize(round) })
	_, lateDone, err := service.begin(context.Background(), round.claim.project)
	hasCode(t, err, f.ResourceBusy)
	if lateDone != nil || len(a.cleanupCalls.projectCalls) != 1 {
		t.Fatal("late same-P public call admitted")
	}
	a.releaseFinalize(round)
	_, nextDone, err := service.begin(context.Background(), round.claim.project)
	if err != nil {
		t.Fatal("released original final round still blocked P", err)
	}
	nextDone()
	doneB()
	if len(a.cleanupCalls.projectCalls) != 0 || len(service.state().calls) != 0 || store.calls != 0 {
		t.Fatal("call return left custody or issued SQL")
	}
}
