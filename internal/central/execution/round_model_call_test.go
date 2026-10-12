package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Package-local controls use explicit Store outcomes and a private process
// proof. They do not create a public fake ProcessGuard or claim real flock,
// committed historical 68/72 rows, PostgreSQL, or Model execution.
type roundClaimProcessControl struct {
	id                         oc.ProcessID
	store                      Store
	currentErr, requireErr     error
	currentCalls, requireCalls int
	seenTx                     f.Tx
}

func (g *roundClaimProcessControl) CurrentProcess() (oc.ProcessID, error) {
	g.currentCalls++
	return g.id, g.currentErr
}
func (g *roundClaimProcessControl) RequireCurrentProcessInTx(ctx context.Context, tx f.Tx, expected oc.ProcessID) error {
	g.requireCalls++
	g.seenTx = tx
	if expected != g.id {
		return fault(f.ConfirmationStale)
	}
	if g.requireErr != nil {
		return g.requireErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := g.store.InTx(tx)
	return err
}

func roundClaimOwnerControl(t *testing.T) (*readFileAuthorityControl, *roundClaimProcessControl, *RoundModelCallOwner) {
	t.Helper()
	store := newReadFileAuthorityControl(t)
	guard := &roundClaimProcessControl{id: newTestID[oc.Process](t), store: store}
	owner, err := newRoundModelCallOwner(store, store.authority, store, guard)
	if err != nil {
		t.Fatal("controlled owner", err)
	}
	return store, guard, owner
}

func TestExecutionRoundModelCallConstructorKeepsProcessProofPrivate(t *testing.T) {
	store, guard, _ := roundClaimOwnerControl(t)
	if guard.currentCalls != 1 || guard.requireCalls != 0 || store.live || store.writes != 0 {
		t.Fatal("constructor claimed live transaction proof or performed work")
	}
	for _, name := range []string{"missing-store", "missing-authority", "foreign-authority", "missing-project", "missing-private-guard", "missing-real-guard"} {
		t.Run(name, func(t *testing.T) {
			s, a, p, g := Store(store), store.authority, c.PreparationProjectGate(store), roundModelCallProcess(guard)
			switch name {
			case "missing-store":
				s = nil
			case "missing-authority":
				a = nil
			case "foreign-authority":
				a = &Authority{state: &authorityState{store: store.launchStore}}
			case "missing-project":
				p = nil
			case "missing-private-guard":
				g = nil
			case "missing-real-guard":
				if value, err := NewRoundModelCallOwner(s, a, p, nil); err == nil || value != nil {
					t.Fatal("public constructor accepted absent real guard")
				}
				return
			}
			if value, err := newRoundModelCallOwner(s, a, p, g); err == nil || value != nil {
				t.Fatal("missing or foreign dependency accepted")
			}
		})
	}
	for _, name := range []string{"process-error", "invalid-process"} {
		t.Run(name, func(t *testing.T) {
			marker := f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(errors.New("original process observation unavailable"))
			changed := &roundClaimProcessControl{id: guard.id, store: store}
			if name == "process-error" {
				changed.currentErr = marker
			} else {
				changed.id = oc.ProcessID{}
			}
			got, err := newRoundModelCallOwner(store, store.authority, store, changed)
			if err == nil || got != nil || changed.requireCalls != 0 {
				t.Fatal("unavailable process accepted", err)
			}
			if name == "process-error" && !errors.Is(err, marker) {
				t.Fatal("original process error lost", err)
			}
		})
	}
}

func roundClaimCandidateControl(t *testing.T) (roundModelClaimRecord, c.NextRoundModelRequest, c.NextRoundModelRequestInput, f.Instant) {
	t.Helper()
	request, parts := roundModelRequestCandidateControl(t)
	q := request.Fields()
	at, err := f.NewInstant(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	candidate := roundModelClaimRecord{fields: c.RoundModelClaimFields{
		SchemaVersion: 1, ClaimID: newTestID[c.RoundModelClaim](t), ProjectID: q.ProjectID, AgentID: q.AgentID,
		ExecutionID: q.ExecutionID, RoundInputID: q.RoundInputID, RoundInputDigest: q.RoundInputDigest,
		SnapshotID: q.SnapshotID, SnapshotDigest: q.SnapshotDigest, ProjectionDigest: q.ProjectionDigest,
		CallID: q.CallID, ProcessID: newTestID[oc.Process](t), InputDigest: request.InputDigest(), RequestDigest: request.Digest(), CreatedAt: at,
	}}
	if candidate.Validate() != nil {
		t.Fatal("invalid controlled reservation")
	}
	return candidate, request, parts, at
}

func roundClaimRecordValues(r roundModelClaimRecord) []any {
	v := r.fields
	return []any{v.ClaimID.String(), v.ProjectID.String(), v.AgentID.String(), v.ExecutionID.String(), v.RoundInputID.String(), v.SnapshotID.String(), v.CallID.String(), v.ProcessID.String(), int64(v.SchemaVersion), string(v.RoundInputDigest), string(v.SnapshotDigest), string(v.ProjectionDigest), string(v.InputDigest), string(v.RequestDigest), v.CreatedAt.Time()}
}

type roundClaimRecordSQL struct {
	Store
	row []any
	err error
}

func (s *roundClaimRecordSQL) QueryRow(context.Context, string, ...any) postgres.Row {
	return launchScan{values: append([]any(nil), s.row...), err: s.err}
}

func TestExecutionRoundModelCallBindsCompleteOriginalRequest(t *testing.T) {
	original, request, parts, at := roundClaimCandidateControl(t)
	key := roundModelCallKey{execution: original.fields.ExecutionID, next: original.fields.RoundInputID}
	pending := &roundModelCallUnknown{key: key, candidate: original}
	stored, err := loadRoundModelClaim(context.Background(), &roundClaimRecordSQL{row: roundClaimRecordValues(original)}, key.execution, key.next)
	if err != nil || stored == nil || stored.fields != original.fields || !stored.matchesRequest(request, at) || !sameRoundModelClaimObservation(pending, *stored) {
		t.Fatal("original complete reservation was not retained", err)
	}
	t.Run("same-key-changed-semantic", func(t *testing.T) {
		changed := parts
		changed.Messages = request.Request().Messages
		changed.Messages[1].Parts[0].Text.Text = "changed full semantic body"
		value, err := c.NewNextRoundModelRequest(context.Background(), changed)
		if err != nil {
			t.Fatal(err)
		}
		if value.InputDigest() == request.InputDigest() || value.Digest() == request.Digest() || original.matchesRequest(value, at) {
			t.Fatal("changed complete semantic body kept original reservation")
		}
	})
	t.Run("same-semantic-changed-logical-call", func(t *testing.T) {
		changed := parts
		changed.CallID = newTestID[mc.Call](t)
		value, err := c.NewNextRoundModelRequest(context.Background(), changed)
		if err != nil {
			t.Fatal(err)
		}
		if value.InputDigest() != request.InputDigest() || value.Digest() == request.Digest() || original.matchesRequest(value, at) {
			t.Fatal("logical CallID not bound separately from semantic digest")
		}
	})
	t.Run("source-and-creation", func(t *testing.T) {
		for _, change := range []func(*c.RoundModelClaimFields){
			func(v *c.RoundModelClaimFields) { v.RoundInputDigest = c.TriggerInputDigest([]byte("other source")) },
			func(v *c.RoundModelClaimFields) { v.SnapshotID = newTestID[c.Snapshot](t) },
			func(v *c.RoundModelClaimFields) {
				v.ProjectionDigest = c.TriggerInputDigest([]byte("other projection"))
			},
			func(v *c.RoundModelClaimFields) { v.CreatedAt, _ = f.NewInstant(at.Time().Add(-time.Nanosecond)) },
		} {
			changed := original
			change(&changed.fields)
			if changed.matchesRequest(request, at) || sameRoundModelClaimObservation(pending, changed) {
				t.Fatal("source or creation order was not bound")
			}
		}
	})
	t.Run("foreign-reservation-not-original-unknown", func(t *testing.T) {
		for _, change := range []func(*c.RoundModelClaimFields){
			func(v *c.RoundModelClaimFields) { v.ClaimID = newTestID[c.RoundModelClaim](t) },
			func(v *c.RoundModelClaimFields) { v.ProcessID = newTestID[oc.Process](t) },
		} {
			changed := original
			change(&changed.fields)
			if !changed.matchesRequest(request, at) || sameRoundModelClaimObservation(pending, changed) {
				t.Fatal("historical request agreement transferred original reservation ownership")
			}
		}
	})
	t.Run("foreign-row-and-missing", func(t *testing.T) {
		row := roundClaimRecordValues(original)
		row[3] = newTestID[i.Execution](t).String()
		if got, err := loadRoundModelClaim(context.Background(), &roundClaimRecordSQL{row: row}, key.execution, key.next); err == nil || got != nil {
			t.Fatal("foreign stored Execution accepted")
		}
		if got, err := loadRoundModelClaim(context.Background(), &roundClaimRecordSQL{}, key.execution, key.next); err != nil || got != nil {
			t.Fatal("absence fabricated a reservation", err)
		}
	})
	t.Run("owned-fields-and-safe-defaults", func(t *testing.T) {
		changed := original.Fields()
		changed.ClaimID = newTestID[c.RoundModelClaim](t)
		if original.Fields().ClaimID == changed.ClaimID {
			t.Fatal("field copy changed original receipt")
		}
		raw, err := json.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{string(raw), fmt.Sprintf("%+v", original), original.LogValue().String()} {
			for _, sensitive := range []string{original.fields.ClaimID.String(), original.fields.ProcessID.String(), string(original.fields.RequestDigest)} {
				if strings.Contains(text, sensitive) {
					t.Fatal("default receipt exposed identity")
				}
			}
		}
	})
}

// The Store supplies explicit SQL rows for scope/72 and the reservation. A
// configured job outcome deliberately makes no callback claim. Full successful
// 68/72 historical reconstruction and the INSERT pair belong to the real PG.
type roundClaimCallStore struct {
	*readFileAuthorityControl
	request             c.NextRoundModelRequest
	at                  f.Instant
	record              *roundModelClaimRecord
	outcome             string
	replayUnknown       bool
	replayCalls         int
	entered, release    chan struct{}
	jobCalls, sqlWrites int
	attempt             f.ID[f.TransactionAttempt]
	cause               f.TransactionCause
}

func (s *roundClaimCallStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *roundClaimCallStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	switch {
	case strings.Contains(query, "FROM agenteam_execution.round_model_claims"):
		if s.record == nil {
			return launchScan{}
		}
		return launchScan{values: roundClaimRecordValues(*s.record)}
	case strings.Contains(query, "FROM agenteam_execution.round_model_requests"):
		return launchScan{values: roundModelRecordRow(s.request, s.at)}
	case strings.Contains(query, "FROM agenteam_execution.executions WHERE"):
		row := s.readFileAuthorityControl.QueryRow(ctx, query, args...).(launchScan)
		q := s.request.Fields()
		launch := s.input.Fields().Request.Launch
		launch.ProjectID, launch.AgentID = q.ProjectID, q.AgentID
		raw, err := json.Marshal(launch)
		if err != nil {
			return launchScan{err: err}
		}
		digest, err := launch.Digest()
		if err != nil {
			return launchScan{err: err}
		}
		snapshot := q.SnapshotID.String()
		row.values[0], row.values[1], row.values[2] = q.ExecutionID.String(), q.ProjectID.String(), q.AgentID.String()
		row.values[6], row.values[10], row.values[13] = &snapshot, raw, string(digest)
		return row
	default:
		return s.readFileAuthorityControl.QueryRow(ctx, query, args...)
	}
}
func (s *roundClaimCallStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.sqlWrites++
	return pgconn.CommandTag{}, errors.New("unexpected reservation write in rejection control")
}
func (s *roundClaimCallStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if cause.Details().Owner == "execution.round-model-call.replay" && s.replayUnknown {
		s.replayCalls++
		return f.UnknownResult(s.attempt, cause)
	}
	if cause.Details().JobType == "execution.round-model-call.claim" && s.outcome != "" {
		s.jobCalls++
		s.cause = cause
		close(s.entered)
		<-s.release
		switch s.outcome {
		case "unknown":
			return f.UnknownResult(s.attempt, cause)
		case "not-committed":
			return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
		case "committed-without-callback":
			return f.CommittedResult()
		default:
			panic("unrecognized controlled outcome")
		}
	}
	return s.launchStore.WithinTx(ctx, cause, fn)
}
func roundClaimCallControl(t *testing.T) (*roundClaimCallStore, *roundClaimProcessControl, *RoundModelCallOwner) {
	t.Helper()
	_, request, _, at := roundClaimCandidateControl(t)
	base := newReadFileAuthorityControl(t)
	s := &roundClaimCallStore{readFileAuthorityControl: base, request: request, at: at, entered: make(chan struct{}), release: make(chan struct{}), attempt: newTestID[f.TransactionAttempt](t)}
	guard := &roundClaimProcessControl{id: newTestID[oc.Process](t), store: s}
	owner, err := newRoundModelCallOwner(s, &Authority{state: &authorityState{store: s}}, base, guard)
	if err != nil {
		t.Fatal(err)
	}
	return s, guard, owner
}

func TestExecutionRoundModelCallRequiresLiveTransactionBeforeHistory(t *testing.T) {
	for _, mode := range []string{"foreign-transaction", "missing-project", "missing-schedule", "missing-agent", "missing-execution", "foreign-process-store", "lost-process", "foreign-local-owner"} {
		t.Run(mode, func(t *testing.T) {
			s, guard, owner := roundClaimCallControl(t)
			q := s.request.Fields()
			work, err := owner.enter(context.Background(), q.ExecutionID, q.RoundInputID, false)
			if err != nil {
				t.Fatal(err)
			}
			defer owner.leave(work)
			candidate, err := owner.candidate(work, &roundModelRequestRecord{request: s.request, at: s.at})
			if err != nil {
				t.Fatal(err)
			}
			scope := &executionRecord{summary: c.Summary{ID: q.ExecutionID, ProjectID: q.ProjectID, AgentID: q.AgentID}}
			locks := roundInputLocks(q.ProjectID, q.AgentID, q.ExecutionID)
			if mode == "foreign-transaction" {
				if got, found, err := owner.claimInTx(work.ctx, f.NewTx(), work, scope, &candidate, locks); err == nil || found || got != nil {
					t.Fatal("foreign transaction accepted")
				}
			} else {
				if mode == "foreign-process-store" {
					guard.store = &launchStore{t: t}
				}
				if mode == "lost-process" {
					guard.requireErr = errors.New("original guard lost")
				}
				if mode == "foreign-local-owner" {
					old := candidate
					s.record = &old
				}
				cause, _ := f.NewRecoveryCause("control.round-model-claim", q.ExecutionID.String(), q.RoundInputID.String())
				result := s.launchStore.WithinTx(work.ctx, cause, func(ctx context.Context, tx f.Tx) error {
					held := append([]f.LockRequest(nil), locks...)
					for n, name := range []string{"missing-project", "missing-schedule", "missing-agent", "missing-execution"} {
						if mode == name {
							held = append(held[:n], held[n+1:]...)
							break
						}
					}
					if err := s.AcquireAll(ctx, tx, held); err != nil {
						return err
					}
					got, found, err := owner.claimInTx(ctx, tx, work, scope, &candidate, held)
					if err == nil || found || got != nil {
						t.Fatal("missing original transaction/lock/process/local-owner proof accepted")
					}
					if mode == "foreign-local-owner" {
						var known *f.Fault
						if !errors.As(err, &known) || known.Code != f.ResourceBusy {
							t.Fatal("same-process stored receipt did not reach original-local-owner gate", err)
						}
					}
					return nil
				})
				if result.State() != f.Committed {
					t.Fatal("controlled assertion transaction", result.State())
				}
			}
			if s.sqlWrites != 0 {
				t.Fatal("rejected prerequisite performed a write")
			}
			if strings.HasPrefix(mode, "missing-") || mode == "foreign-transaction" {
				if guard.requireCalls != 0 {
					t.Fatal("incomplete transaction/locks reached process proof")
				}
			} else if guard.requireCalls != 1 || !guard.seenTx.Valid() {
				t.Fatal("original process proof was not used on live caller transaction")
			}
		})
	}
}

func TestExecutionRoundModelCallUnknownKeepsOriginalCandidateAndReturn(t *testing.T) {
	for _, mode := range []string{"not-committed", "unknown", "committed-without-callback"} {
		t.Run(mode, func(t *testing.T) {
			s, _, owner := roundClaimCallControl(t)
			s.outcome = mode
			q := s.request.Fields()
			key := roundModelCallKey{q.ExecutionID, q.RoundInputID}
			returned := make(chan struct{})
			done := make(chan error, 1)
			t.Cleanup(func() {
				select {
				case <-s.release:
				default:
					close(s.release)
				}
				owner.Stop()
				<-returned
			})
			go func() {
				defer close(returned)
				receipt, err := owner.Claim(context.Background(), q.ExecutionID, q.RoundInputID)
				if receipt != nil {
					done <- errors.New("controlled unsuccessful Store outcome published receipt")
					return
				}
				done <- err
			}()
			select {
			case <-s.entered:
			case err := <-done:
				t.Fatal("Claim failed before reaching original write outcome", err)
			}
			select {
			case <-done:
				t.Fatal("callback or candidate creation substituted original Store return")
			default:
			}
			if s.jobCalls != 1 || s.sqlWrites != 0 {
				t.Fatal("unexpected controlled job/write count")
			}
			if mode == "unknown" {
				owner.Stop()
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if err := owner.Drain(ctx); !errors.Is(err, context.Canceled) || owner.Joined() {
					t.Fatal("Stop joined before original return", err)
				}
			}
			close(s.release)
			err := <-done
			if err == nil {
				t.Fatal("actual unsuccessful result lost")
			}
			if mode != "unknown" {
				if len(owner.pending) != 0 || len(owner.owned) != 0 {
					t.Fatal("known rollback/invalid callback fabricated original ownership")
				}
				var fault *f.Fault
				if !errors.As(err, &fault) || (mode == "not-committed" && fault.CommitState != f.NotCommitted) || (mode == "committed-without-callback" && fault.CommitState != f.Committed) {
					t.Fatal("original commit state changed", err)
				}
				return
			}
			attempt, ok := UnknownAttempt(err)
			if !ok || attempt.AttemptID() != s.attempt || attempt.Cause().Details().JobType != "execution.round-model-call.claim" || attempt.Cause().Details().JobID != q.ExecutionID.String() || attempt.Cause().Details().JobAttemptID != q.RoundInputID.String() {
				t.Fatal("original Unknown attempt lost", err)
			}
			pending := owner.pending[q.ExecutionID]
			if pending == nil || pending.key != key || pending.result.State() != f.Unknown || !pending.candidate.matchesRequest(s.request, s.at) || pending.candidate.fields.ProcessID != owner.process || pending.candidate.fields.CallID != q.CallID || len(owner.owned) != 0 {
				t.Fatal("original complete candidate/72 identity not retained")
			}
			original := pending.candidate
			if receipt, err := owner.Claim(context.Background(), q.ExecutionID, q.RoundInputID); err == nil || receipt != nil || s.jobCalls != 1 {
				t.Fatal("Unknown repeated Claim")
			}
			receipt, found, err := owner.Lookup(context.Background(), q.ExecutionID, q.RoundInputID)
			if err != nil || found || receipt != nil || owner.pending[q.ExecutionID] != pending || owner.pending[q.ExecutionID].candidate.fields != original.fields || owner.Joined() {
				t.Fatal("missing observation cleared or changed original Unknown", err)
			}
			if _, _, err = owner.Lookup(context.Background(), q.ExecutionID, newTestID[c.Round](t)); err == nil {
				t.Fatal("stopped owner allowed different recovery identity")
			}
			if err = owner.Drain(context.Background()); err == nil || owner.Joined() {
				t.Fatal("unresolved original Unknown became joined")
			}
			fresh, err := newRoundModelCallOwner(s, &Authority{state: &authorityState{store: s}}, s.readFileAuthorityControl, &roundClaimProcessControl{id: owner.process, store: s})
			if err != nil {
				t.Fatal(err)
			}
			if _, found, err = fresh.Lookup(context.Background(), q.ExecutionID, q.RoundInputID); err != nil || found || len(fresh.owned) != 0 || len(fresh.pending) != 0 {
				t.Fatal("new observer adopted original local responsibility", err)
			}
		})
	}
}

func TestExecutionRoundModelCallKnownReplayCannotCreateNewUnknownWrite(t *testing.T) {
	s, _, owner := roundClaimCallControl(t)
	q := s.request.Fields()
	key := roundModelCallKey{q.ExecutionID, q.RoundInputID}
	work, err := owner.enter(context.Background(), q.ExecutionID, q.RoundInputID, false)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := owner.candidate(work, &roundModelRequestRecord{request: s.request, at: s.at})
	owner.leave(work)
	if err != nil {
		t.Fatal(err)
	}
	// Controlled prior known receipt. This does not simulate or claim that
	// the complete historical writer committed; PG proves that initial fact.
	owner.owned[key] = candidate
	s.replayUnknown = true
	receipt, err := owner.Claim(context.Background(), q.ExecutionID, q.RoundInputID)
	result, ok := UnknownAttempt(err)
	if !ok || receipt != nil || result.AttemptID() != s.attempt || result.Cause().Details().Owner != "execution.round-model-call.replay" || s.replayCalls != 1 || s.jobCalls != 0 || s.sqlWrites != 0 {
		t.Fatal("known replay became a new write or hid actual read Unknown", err)
	}
	if len(owner.pending) != 0 || owner.owned[key].fields != candidate.fields {
		t.Fatal("read failure replaced known reservation with pending write")
	}
	owner.Stop()
	if err := owner.Drain(context.Background()); err != nil || !owner.Joined() {
		t.Fatal("returned replay read retained false pending work", err)
	}
}

type roundClaimLoanStore struct {
	*launchStore
	entered, release chan struct{}
	unknown          bool
	attempt          f.ID[f.TransactionAttempt]
}

func (s *roundClaimLoanStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := s.launchStore.WithinTx(ctx, cause, fn)
	close(s.entered)
	<-s.release
	if s.unknown {
		return f.UnknownResult(s.attempt, cause)
	}
	return result
}

func TestExecutionRoundModelCallLoanAndStoreMustActuallyReturn(t *testing.T) {
	for _, mode := range []string{"committed", "unknown", "stop-during-loan"} {
		t.Run(mode, func(t *testing.T) {
			base, _, _ := roundClaimOwnerControl(t)
			s := &roundClaimLoanStore{launchStore: base.launchStore, entered: make(chan struct{}), release: make(chan struct{}), unknown: mode == "unknown", attempt: newTestID[f.TransactionAttempt](t)}
			guard := &roundClaimProcessControl{id: newTestID[oc.Process](t), store: s}
			owner, err := newRoundModelCallOwner(s, &Authority{state: &authorityState{store: s}}, base, guard)
			if err != nil {
				t.Fatal(err)
			}
			request := base.input.Fields().Request
			next := newTestID[c.Round](t)
			work, err := owner.enter(context.Background(), request.ExecutionID, next, true)
			if err != nil {
				t.Fatal(err)
			}
			loanEntered, loanRelease := make(chan struct{}), make(chan struct{})
			loan := []byte("controlled original historical loan")
			done := make(chan error, 1)
			returned := make(chan struct{})
			t.Cleanup(func() {
				owner.Stop()
				select {
				case <-loanRelease:
				default:
					close(loanRelease)
				}
				select {
				case <-s.release:
				default:
					close(s.release)
				}
				<-returned
			})
			go func() {
				defer close(returned)
				defer owner.leave(work)
				done <- owner.readTx(work.ctx, work, "lookup", roundInputLocks(request.Launch.ProjectID, request.Launch.AgentID, request.ExecutionID), func(ctx context.Context, tx f.Tx) error {
					return lendRoundProjection(ctx, c.RoundProjectionSource{Arguments: loan}, func(source c.RoundProjectionSource) error {
						close(loanEntered)
						<-loanRelease
						if len(source.Arguments) != len(loan) {
							return errors.New("original loan descriptor changed")
						}
						return nil
					})
				})
			}()
			select {
			case <-loanEntered:
			case <-s.entered:
				t.Fatal("read failed before reaching original loan")
			case err := <-done:
				t.Fatal("read returned before reaching original loan", err)
			}
			if guard.requireCalls != 1 || !guard.seenTx.Valid() {
				t.Fatal("original live process proof not used")
			}
			if string(loan) != "controlled original historical loan" {
				t.Fatal("loan retired before original callback")
			}
			if mode == "stop-during-loan" {
				owner.Stop()
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if err := owner.Drain(ctx); !errors.Is(err, context.Canceled) || owner.Joined() {
					t.Fatal("Stop skipped original loan", err)
				}
				if string(loan) != "controlled original historical loan" {
					t.Fatal("cancel substituted actual loan return")
				}
			}
			select {
			case <-done:
				t.Fatal("returned before original loan")
			default:
			}
			close(loanRelease)
			<-s.entered
			for _, b := range loan {
				if b != 0 {
					t.Fatal("original loan not cleared after callback")
				}
			}
			select {
			case <-done:
				t.Fatal("returned before actual Store return")
			default:
			}
			if owner.Joined() {
				t.Fatal("callback completion became owner join")
			}
			close(s.release)
			err = <-done
			<-returned
			if mode == "committed" && err != nil || mode != "committed" && err == nil {
				t.Fatal("actual original read outcome changed", err)
			}
			if mode == "unknown" {
				got, ok := UnknownAttempt(err)
				if !ok || got.AttemptID() != s.attempt {
					t.Fatal("original read Unknown attempt lost", err)
				}
			}
			if err := owner.Drain(context.Background()); err != nil || !owner.Joined() {
				t.Fatal("actual returned read did not join", err)
			}
		})
	}
}
