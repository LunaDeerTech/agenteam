package projectvariable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5/pgconn"
)

// These tests use the actual pair and its live-Tx control Store. The private
// Project gate, SQL projection and stopped-process answer are controlled ports,
// not proof of a real Project claim, PostgreSQL rollback or process death.
type terminalControlGrantKey struct{}
type terminalControlAuthority struct {
	h       *ownershipControl
	request pc.ProjectTerminalCleanupRequest
	err     error
	calls   int
}

func (a *terminalControlAuthority) RequireProjectTerminalCleanupInTx(ctx context.Context, tx f.Tx, r pc.ProjectTerminalCleanupRequest) error {
	a.calls++
	if ctx.Value(terminalControlGrantKey{}) != a || !r.Actor.Equal(a.request.Actor) || r.Cause != a.request.Cause || r.ProjectID != a.request.ProjectID || r.Owner != a.request.Owner {
		return fault(f.Forbidden)
	}
	if err := a.h.store.RequireHeldLocks(ctx, tx, r.RequiredLocks()); err != nil {
		return err
	}
	return a.err
}

type terminalControl struct {
	h                     *ownershipControl
	authority             *terminalControlAuthority
	cleaner               *ProjectTerminalCleanup
	request               pc.ProjectTerminalCleanupRequest
	referenced, remaining bool
	ledgerReads, deletes  int
	countDelta            int64
}

func newTerminalControl(t *testing.T) *terminalControl {
	t.Helper()
	h := newOwnershipControl(t)
	// Register and close both original lanes through the pair before installing
	// the terminal projection. A closed phase alone grants no returned proof.
	for _, lane := range []projectCallLane{projectCallOrdinary, projectCallSecret} {
		_, _, finish := h.begin(t, lane, mutationCall)
		finish()
	}
	actor, cause, scope := h.lifecycle(t, pc.Delete)
	v := &terminalControl{h: h}
	v.request = pc.ProjectTerminalCleanupRequest{Actor: actor, Cause: cause, ProjectID: scope.ProjectID, Owner: pc.ProjectTerminalOwnerIdentity{Kind: pc.ProjectTerminalVariablesOwner, ProcessID: h.pair.state.process.String(), InstanceID: h.pair.state.instance.String()}}
	v.authority = &terminalControlAuthority{h: h, request: v.request}
	var err error
	v.cleaner, err = NewProjectTerminalCleanup(h.pair, v.authority)
	if err != nil {
		t.Fatal(err)
	}
	h.store.row = v.row
	h.store.exec = v.exec
	// Failed assertions must still settle retained controls through the actual
	// confirmation port; never erase ownership maps to make Drain succeed.
	t.Cleanup(func() {
		h.store.before, h.store.after, h.proof.check = nil, nil, nil
		v.authority.err = nil
		v.referenced, v.remaining = false, false
		v.cleaner.owners.mu.Lock()
		owner := v.cleaner.owners.calls[v.request.ProjectID]
		retained := owner != nil && !owner.active && owner.returned != nil
		v.cleaner.owners.mu.Unlock()
		if retained {
			r := v.request
			r.Confirmation, r.NotApplied = true, v.count() != 0
			if _, err := v.run(v.cleaner, r); err != nil {
				t.Error("terminal original did not settle", err)
			}
		}
	})
	return v
}

func (v *terminalControl) context() context.Context {
	return context.WithValue(context.Background(), terminalControlGrantKey{}, v.authority)
}
func (v *terminalControl) count() int {
	v.h.mu.Lock()
	defer v.h.mu.Unlock()
	n := 0
	for _, row := range v.h.records {
		if row.Project == v.request.ProjectID {
			n++
		}
	}
	return n
}
func (v *terminalControl) row(ctx context.Context, tx f.Tx, query string, args []any) postgres.Row {
	if strings.Contains(query, "SELECT count(*) FROM agenteam_projectvariable.project_calls") {
		v.ledgerReads++
		return cleanupControlRow(func(out ...any) error {
			if len(args) != 1 || args[0] != v.request.ProjectID.String() || len(out) != 3 {
				return errors.New("unexpected terminal projection")
			}
			count, okCount := out[0].(*int64)
			running, okRunning := out[1].(*bool)
			raw, okRaw := out[2].(*[]byte)
			if !okCount || !okRunning || !okRaw {
				return errors.New("unexpected terminal projection types")
			}
			owners := make(map[[2]string]bool)
			v.h.mu.Lock()
			for _, row := range v.h.records {
				if row.Project != v.request.ProjectID {
					continue
				}
				*count += 1
				*running = *running || row.Phase != "business_closed"
				owners[[2]string{row.Process.String(), row.Instance.String()}] = true
			}
			v.h.mu.Unlock()
			rows := make([]map[string]string, 0, len(owners))
			for owner := range owners {
				rows = append(rows, map[string]string{"process_id": owner[0], "instance_id": owner[1]})
			}
			sort.Slice(rows, func(a, b int) bool {
				if rows[a]["process_id"] != rows[b]["process_id"] {
					return rows[a]["process_id"] < rows[b]["process_id"]
				}
				return rows[a]["instance_id"] < rows[b]["instance_id"]
			})
			var err error
			*raw, err = json.Marshal(rows)
			return err
		})
	}
	if strings.Contains(query, "FROM agenteam_projectvariable.secret_references") {
		return cleanupBool(v.referenced, nil)
	}
	if strings.Contains(query, "FROM agenteam_projectvariable.history") {
		return cleanupBool(v.remaining, nil)
	}
	if strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.project_calls") {
		v.h.mu.Lock()
		defer v.h.mu.Unlock()
		for _, row := range v.h.records {
			if row.Project == v.request.ProjectID && row.Phase == "running" {
				return cleanupBool(true, nil)
			}
		}
		return cleanupBool(false, nil)
	}
	return v.h.row(ctx, tx, query, args)
}
func (v *terminalControl) exec(ctx context.Context, tx f.Tx, query string, args []any) (pgconn.CommandTag, error) {
	if !strings.HasPrefix(query, "DELETE FROM agenteam_projectvariable.project_calls") {
		return v.h.exec(ctx, tx, query, args)
	}
	if !strings.Contains(query, "WHERE project_id=$1") || !strings.Contains(query, "phase='business_closed'") || strings.Contains(query, "LIMIT") || len(args) != 1 || args[0] != v.request.ProjectID.String() {
		return pgconn.CommandTag{}, errors.New("terminal deletion is not the fixed Project set")
	}
	v.deletes++
	v.h.mu.Lock()
	defer v.h.mu.Unlock()
	n := int64(0)
	for id, row := range v.h.records {
		if row.Project == v.request.ProjectID && row.Phase == "business_closed" {
			n++
			delete(v.h.records, id)
		}
	}
	return pgconn.NewCommandTag(fmt.Sprintf("DELETE %d", n+v.countDelta)), nil
}

func (v *terminalControl) caller(ctx context.Context, cleaner *ProjectTerminalCleanup, r pc.ProjectTerminalCleanupRequest, call func(context.Context, f.Tx) error) (f.CommitResult, error) {
	cause, err := f.NewRecoveryCause("projectvariable-terminal-caller-control", r.Cause.OperationID.String(), r.ProjectID.String())
	if err != nil {
		return f.CommitResult{}, err
	}
	result := v.h.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := v.h.store.AcquireAll(ctx, tx, r.RequiredLocks()); err != nil {
			return err
		}
		if call != nil {
			return call(ctx, tx)
		}
		if r.NotApplied {
			return cleaner.RequireProjectTerminalUnappliedInTx(ctx, tx, r)
		}
		if r.Confirmation {
			return cleaner.RequireProjectTerminalRetiredInTx(ctx, tx, r)
		}
		return cleaner.RetireProjectTerminalInTx(ctx, tx, r)
	})
	return result, nil
}
func (v *terminalControl) run(cleaner *ProjectTerminalCleanup, r pc.ProjectTerminalCleanupRequest) (f.CommitResult, error) {
	return cleaner.WithProjectTerminalCleanup(v.context(), r, func(ctx context.Context) (f.CommitResult, error) {
		return v.caller(ctx, cleaner, r, nil)
	})
}
func requireTerminalUnknown(t *testing.T, result f.CommitResult, err error, original f.CommitResult) {
	t.Helper()
	var retained commitFailure
	if result.State() != f.Unknown || result.AttemptID() != original.AttemptID() || !reflect.DeepEqual(result.Cause().Details(), original.Cause().Details()) || !errors.As(err, &retained) || retained.result.AttemptID() != original.AttemptID() || !reflect.DeepEqual(retained.result.Cause().Details(), original.Cause().Details()) {
		t.Fatal("terminal Unknown did not preserve its original attempt and cause", err)
	}
}
func requireTerminalHeld(t *testing.T, v *terminalControl) {
	t.Helper()
	if v.h.pair.Joined() || v.h.pair.Service().Joined() || v.h.pair.SecretService().Joined() {
		t.Fatal("terminal original released one of its two controls")
	}
}

func TestProjectVariableTerminalRequiresOriginalAuthority(t *testing.T) {
	for _, mode := range []string{"unbound-pair", "mismatched-store", "no-private-project-gate", "authority-rejected", "guard-rejected", "missing-project-ex", "foreign-tx", "without-with", "mode-flip", "second-tx", "skipped-callback"} {
		t.Run(mode, func(t *testing.T) {
			v := newTerminalControl(t)
			if mode == "unbound-pair" {
				if _, err := NewProjectTerminalCleanup(&ProjectCallOwnership{}, v.authority); !cleanupCode(err, f.DependencyUnbound) {
					t.Fatal("unbound pair accepted", err)
				}
				return
			}
			if mode == "mismatched-store" {
				state := v.h.pair.SecretService().state()
				original := state.store
				state.store = &ownershipControlStore{live: make(map[f.Tx][]f.LockRequest)}
				_, err := NewProjectTerminalCleanup(v.h.pair, v.authority)
				state.store = original
				if !cleanupCode(err, f.DependencyUnbound) {
					t.Fatal("pair with mismatched Store accepted", err)
				}
				return
			}
			if mode == "without-with" {
				result, _ := v.caller(v.context(), v.cleaner, v.request, nil)
				if !cleanupCode(txError(result), f.Forbidden) || v.deletes != 0 {
					t.Fatal("public request manufactured private terminal scope")
				}
				return
			}
			ctx := v.context()
			if mode == "no-private-project-gate" {
				ctx = context.Background()
			}
			if mode == "authority-rejected" {
				v.authority.err = fault(f.Forbidden)
			}
			if mode == "guard-rejected" {
				v.h.proof.check = func(context.Context, f.Tx, oc.ProcessID) error { return fault(f.Forbidden) }
			}
			r := v.request
			if mode == "second-tx" {
				r.Confirmation, r.NotApplied = true, true
			}
			_, err := v.cleaner.WithProjectTerminalCleanup(ctx, r, func(ctx context.Context) (f.CommitResult, error) {
				if mode == "skipped-callback" {
					return f.CommittedResult(), nil
				}
				if mode == "second-tx" {
					first, e := v.caller(ctx, v.cleaner, r, nil)
					if e != nil || first.State() != f.Committed {
						return first, errors.New("first live transaction rejected")
					}
				}
				return v.caller(ctx, v.cleaner, r, func(ctx context.Context, tx f.Tx) error {
					if mode == "missing-project-ex" {
						if err := v.h.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(r.ProjectID, f.Shared)}); err != nil {
							return err
						}
					}
					if mode == "foreign-tx" {
						tx = f.NewTx()
					}
					if mode == "mode-flip" || mode == "second-tx" {
						changed := r
						changed.Confirmation, changed.NotApplied = true, true
						return v.cleaner.RequireProjectTerminalUnappliedInTx(ctx, tx, changed)
					}
					return v.cleaner.RetireProjectTerminalInTx(ctx, tx, r)
				})
			})
			want := f.Forbidden
			if mode == "skipped-callback" {
				want = f.DependencyUnavailable
			}
			if !cleanupCode(err, want) || v.deletes != 0 || !v.h.pair.Joined() {
				t.Fatal("terminal gate accepted an invalid original scope", err)
			}
		})
	}
}

func TestProjectVariableTerminalWaitsForProjectOwners(t *testing.T) {
	for _, mode := range []string{"ordinary-read", "ordinary-mutation", "secret-read", "secret-mutation", "retained-metadata-cleanup", "other-project"} {
		t.Run(mode, func(t *testing.T) {
			v := newTerminalControl(t)
			var finish func()
			if mode == "retained-metadata-cleanup" {
				a := &cleanupControlAuthority{h: v.h, actor: v.request.Actor, cause: v.request.Cause}
				cleaner, err := NewProjectCleanup(v.h.pair, a)
				if err != nil {
					t.Fatal(err)
				}
				scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.request.ProjectID}
				finish = func() {
					cleaner.owners.mu.Lock()
					owner := cleaner.owners.calls[v.request.ProjectID]
					retained := owner != nil && !owner.active && owner.returned != nil
					cleaner.owners.mu.Unlock()
					if retained {
						if _, err := cleaner.Cleanup(context.Background(), v.request.Actor, v.request.Cause, scope, nil); err != nil {
							t.Error("metadata confirmation failed", err)
						}
					}
				}
				t.Cleanup(finish)
				v.h.store.before = func(_ context.Context, cause f.TransactionCause) *f.CommitResult {
					if ownershipCause(cause) == "projectvariable-project-cleanup" {
						r := f.UnknownResult(testID[f.TransactionAttempt](240), cause)
						return &r
					}
					return nil
				}
				_, err = cleaner.Cleanup(context.Background(), v.request.Actor, v.request.Cause, scope, nil)
				v.h.store.before = nil
				if !cleanupCode(err, f.CommitUnknown) {
					t.Fatal("metadata control did not retain original Unknown", err)
				}
			} else {
				begin := v.h.pair.Service().beginProject
				if strings.HasPrefix(mode, "secret") {
					begin = v.h.pair.SecretService().beginProject
				}
				kind := readCall
				if strings.HasSuffix(mode, "mutation") {
					kind = mutationCall
				}
				project := v.request.ProjectID
				if mode == "other-project" {
					project = testID[i.Project](241)
				}
				run, _, done, err := begin(context.Background(), project, kind)
				if err != nil {
					t.Fatal(err)
				}
				finish = done
				t.Cleanup(finish)
				defer func() {
					if run.Err() != nil {
						t.Error("terminal cleanup stopped original business")
					}
				}()
			}
			result, err := v.run(v.cleaner, v.request)
			if mode == "other-project" {
				if err != nil || result.State() != f.Committed || v.deletes != 1 || v.h.pair.Joined() {
					t.Fatal("another Project became a global terminal barrier", err)
				}
				return
			}
			if !cleanupCode(err, f.ResourceBusy) || v.deletes != 0 || v.ledgerReads != 0 {
				t.Fatal("terminal cleanup crossed original Project ownership", err)
			}
		})
	}
}

func TestProjectVariableTerminalChecksLedgerAndProofs(t *testing.T) {
	for _, mode := range []string{"closed-set", "running", "same-process-other-instance", "foreign-busy", "foreign-stopped", "identity-drift", "references", "metadata", "row-count"} {
		t.Run(mode, func(t *testing.T) {
			v := newTerminalControl(t)
			foreign := testID[oc.Process](242)
			changeRows := func(change func(*projectCallRecord)) {
				v.h.mu.Lock()
				defer v.h.mu.Unlock()
				for id, row := range v.h.records {
					change(&row)
					v.h.records[id] = row
				}
			}
			switch mode {
			case "closed-set":
				// More than the old metadata batch is one terminal set. These extra
				// ledger rows are explicit SQL controls, not fabricated local calls.
				v.h.mu.Lock()
				for n := 0; n < 101; n++ {
					id := testID[projectCallIdentity](1000 + n)
					v.h.records[id.String()] = projectCallRecord{ID: id, Project: v.request.ProjectID, Process: v.h.pair.state.process, Instance: v.h.pair.state.instance, Lane: projectCallOrdinary, Class: "mutation", Phase: "business_closed"}
				}
				other := testID[projectCallIdentity](1200)
				v.h.records[other.String()] = projectCallRecord{ID: other, Project: testID[i.Project](241), Phase: "running"}
				v.h.mu.Unlock()
			case "running":
				changeRows(func(r *projectCallRecord) { r.Phase = "running" })
			case "same-process-other-instance":
				changeRows(func(r *projectCallRecord) { r.Instance = testID[projectCallInstance](243) })
			case "foreign-busy", "foreign-stopped":
				changeRows(func(r *projectCallRecord) { r.Process = foreign })
			case "references":
				v.referenced = true
			case "metadata":
				v.remaining = true
			case "row-count":
				v.countDelta = -1
			}
			preflightReturned, proofs := false, 0
			v.h.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if ownershipCause(cause) == "projectvariable-terminal-preflight" {
					preflightReturned = true
					if mode == "identity-drift" {
						changeRows(func(r *projectCallRecord) { r.Instance = testID[projectCallInstance](244) })
					}
				}
				return result
			}
			if mode == "foreign-stopped" {
				v.h.proof.death = func(ctx context.Context, process oc.ProcessID) error {
					v.h.store.mu.Lock()
					live := len(v.h.store.live)
					v.h.store.mu.Unlock()
					if !preflightReturned || live != 0 || process != foreign {
						return fault(f.Forbidden)
					}
					proofs++
					return ctx.Err()
				}
			}
			result, err := v.run(v.cleaner, v.request)
			success := mode == "closed-set" || mode == "foreign-stopped"
			if success {
				if err != nil || result.State() != f.Committed || v.count() != 0 || v.deletes != 1 || v.ledgerReads != 2 {
					t.Fatal("closed terminal set did not retire exactly once", err)
				}
				if mode == "foreign-stopped" && proofs != 1 {
					t.Fatal("foreign identity did not use its original process proof")
				}
				if mode == "closed-set" {
					v.h.mu.Lock()
					remaining := len(v.h.records)
					v.h.mu.Unlock()
					if remaining != 1 {
						t.Fatal("terminal set crossed Project boundary")
					}
				}
			} else {
				want, deletes := f.ResourceBusy, 0
				if mode == "row-count" {
					want, deletes = f.VersionConflict, 1
				}
				if !cleanupCode(err, want) || v.deletes != deletes {
					t.Fatal("terminal set accepted unproven or incomplete facts", err)
				}
			}
		})
	}
}

func TestProjectVariableTerminalWaitsForOriginalStoreReturn(t *testing.T) {
	v := newTerminalControl(t)
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := ownershipRelease(release)
	v.h.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
		if ownershipCause(cause) == "projectvariable-terminal-caller-control" {
			close(entered)
			<-release
		}
		return result
	}
	var saved context.Context
	done := make(chan error, 1)
	joined := false
	t.Cleanup(func() {
		unblock()
		if !joined {
			ownershipWait(t, done)
		}
	})
	go func() {
		_, err := v.cleaner.WithProjectTerminalCleanup(v.context(), v.request, func(ctx context.Context) (f.CommitResult, error) {
			saved = context.WithoutCancel(ctx)
			return v.caller(ctx, v.cleaner, v.request, nil)
		})
		done <- err
	}()
	ownershipWait(t, entered)
	requireTerminalHeld(t, v)
	other, err := NewProjectTerminalCleanup(v.h.pair, v.authority)
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := v.run(other, v.request)
	if !cleanupCode(err, f.ResourceBusy) || blocked.State() != f.NotCommitted {
		t.Fatal("second constructor borrowed an active terminal original", err)
	}
	select {
	case <-done:
		joined = true
		t.Fatal("terminal callback returned before original Store")
	default:
	}
	unblock()
	err = ownershipWait(t, done)
	joined = true
	v.h.store.after = nil
	if err != nil || !v.h.pair.Joined() || v.deletes != 1 {
		t.Fatal("actual Store return failed to release both controls", err)
	}
	result, _ := v.caller(saved, v.cleaner, v.request, nil)
	if !cleanupCode(txError(result), f.Forbidden) || v.deletes != 1 {
		t.Fatal("saved context revived an expired terminal scope")
	}
	blocked, err = v.cleaner.WithProjectTerminalCleanup(saved, v.request, func(context.Context) (f.CommitResult, error) { return f.CommittedResult(), nil })
	if !cleanupCode(err, f.Forbidden) || blocked.State() != f.NotCommitted {
		t.Fatal("saved scope borrowed a new terminal call", err)
	}
}

func TestProjectVariableTerminalRetainsUnknownUntilConfirmation(t *testing.T) {
	for _, mode := range []string{"applied", "not-applied"} {
		t.Run(mode, func(t *testing.T) {
			v := newTerminalControl(t)
			var original f.CommitResult
			if mode == "applied" {
				v.h.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
					if ownershipCause(cause) == "projectvariable-terminal-caller-control" {
						original = f.UnknownResult(testID[f.TransactionAttempt](245), cause)
						return original
					}
					return result
				}
			} else {
				// No caller callback executes: this controlled Unknown leaves the
				// original closed facts intact, without simulating a DB rollback.
				v.h.store.before = func(_ context.Context, cause f.TransactionCause) *f.CommitResult {
					if ownershipCause(cause) == "projectvariable-terminal-caller-control" {
						original = f.UnknownResult(testID[f.TransactionAttempt](246), cause)
						return &original
					}
					return nil
				}
			}
			result, err := v.run(v.cleaner, v.request)
			requireTerminalUnknown(t, result, err, original)
			requireTerminalHeld(t, v)
			v.h.store.before, v.h.store.after = nil, nil
			deletes, rows := v.deletes, v.count()
			other, err := NewProjectTerminalCleanup(v.h.pair, v.authority)
			if err != nil {
				t.Fatal(err)
			}
			before := len(v.h.store.causes)
			_, err = v.run(other, v.request)
			var retained commitFailure
			if !errors.As(err, &retained) || retained.result.AttemptID() != original.AttemptID() || len(v.h.store.causes) != before {
				t.Fatal("new constructor repeated an Unknown application", err)
			}
			wrong := v.request
			wrong.Cause.ProjectVersion++
			if _, err = v.run(other, wrong); !cleanupCode(err, f.InvalidState) || len(v.h.store.causes) != before {
				t.Fatal("new tuple bypassed the original terminal owner", err)
			}
			r := v.request
			r.Confirmation = true
			v.authority.err = fault(f.Forbidden)
			result, err = v.run(other, r)
			requireTerminalUnknown(t, result, err, original)
			v.authority.err = nil
			if mode == "not-applied" {
				result, err = v.run(other, r)
				requireTerminalUnknown(t, result, err, original)
				if v.count() != rows || v.deletes != deletes {
					t.Fatal("applied confirmation accepted nonempty closed facts", err)
				}
				requireTerminalHeld(t, v)
				r.NotApplied = true
			}
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := ownershipRelease(release)
			v.h.store.after = func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if ownershipCause(cause) == "projectvariable-terminal-caller-control" {
					close(entered)
					<-release
				}
				return result
			}
			done := make(chan error, 1)
			joined := false
			t.Cleanup(func() {
				unblock()
				if !joined {
					ownershipWait(t, done)
				}
			})
			go func() { _, err := v.run(other, r); done <- err }()
			ownershipWait(t, entered)
			requireTerminalHeld(t, v)
			if v.deletes != deletes || v.count() != rows {
				t.Fatal("confirmation mutated original terminal facts")
			}
			unblock()
			err = ownershipWait(t, done)
			joined = true
			v.h.store.after = nil
			if err != nil || !v.h.pair.Joined() || v.deletes != deletes {
				t.Fatal("actual confirmation return failed to release original", err)
			}
			if mode == "not-applied" {
				result, err = v.run(other, v.request)
				if err != nil || result.State() != f.Committed || v.deletes != deletes+1 || v.count() != 0 {
					t.Fatal("later explicit application did not retire confirmed original facts", err)
				}
			}
		})
	}
}

func TestProjectVariableTerminalPreflightUnknownDoesNotOwnMutation(t *testing.T) {
	v := newTerminalControl(t)
	var original f.CommitResult
	v.h.store.before = func(_ context.Context, cause f.TransactionCause) *f.CommitResult {
		if ownershipCause(cause) == "projectvariable-terminal-preflight" {
			original = f.UnknownResult(testID[f.TransactionAttempt](247), cause)
			return &original
		}
		return nil
	}
	called := false
	result, err := v.cleaner.WithProjectTerminalCleanup(v.context(), v.request, func(context.Context) (f.CommitResult, error) {
		called = true
		return f.CommittedResult(), nil
	})
	requireTerminalUnknown(t, result, err, original)
	if called || v.deletes != 0 || !v.h.pair.Joined() {
		t.Fatal("read-only preflight Unknown invented a retained mutation")
	}
	v.h.store.before = nil
	result, err = v.run(v.cleaner, v.request)
	if err != nil || result.State() != f.Committed || v.deletes != 1 || !v.h.pair.Joined() {
		t.Fatal("explicit application remained blocked by preflight Unknown", err)
	}
}
