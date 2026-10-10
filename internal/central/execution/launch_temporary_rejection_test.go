package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution/internal/launchtemporary"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestLaunchTemporaryRejectionBindingAndSafeProjection(t *testing.T) {
	x := newLaunchControl(t)
	r := x.request.Clone()
	r.Meta.IdempotencyKey = "private-launch-key-canary"
	digest, err := r.Digest()
	if err != nil {
		t.Fatal(err)
	}
	raw := &pgconn.PgError{Code: "55P03", Message: "private-sql-material-canary"}
	original := f.NewFault(f.InternalError, f.NotCommitted).WithCause(raw)
	// This deliberately tests the internal binding codec, not the production
	// PostgreSQL proof. Only the real lock-contention fixture may prove minting.
	marker := launchtemporary.MintLockTimeout(digest, r.Meta.RequestID, r.Meta.IdempotencyKey, original)
	bound := original.WithCause(marker)
	for _, err := range []error{bound, fmt.Errorf("safe envelope: %w", bound)} {
		if reason, ok := c.MatchLaunchTemporaryRejection(err, r); !ok || reason != c.LaunchTemporaryLockTimeout {
			t.Fatal("original request binding lost")
		}
		var retained *pgconn.PgError
		if !errors.As(err, &retained) || retained != raw {
			t.Fatal("original diagnostic identity lost")
		}
	}
	mutations := []func(*c.LaunchRequest){
		func(v *c.LaunchRequest) { v.Meta.RequestID = newTestID[f.Request](t) },
		func(v *c.LaunchRequest) { v.Meta.IdempotencyKey = "different-key" },
		func(v *c.LaunchRequest) { v.ProjectID = newTestID[i.Project](t) },
		func(v *c.LaunchRequest) { v.AgentID = newTestID[i.Agent](t) },
		func(v *c.LaunchRequest) { v.Trigger.TaskID = newTestID[struct{}](t).String() },
		func(v *c.LaunchRequest) { v.Purpose = "task/review" },
		func(v *c.LaunchRequest) { v.Policy.DeniedToolIDs = []i.ToolID{newTestID[i.Tool](t)} },
		func(v *c.LaunchRequest) { v.Lineage.DispatchID = newTestID[struct{}](t).String() },
	}
	for n, mutate := range mutations {
		changed := r.Clone()
		mutate(&changed)
		if changed.Validate() != nil {
			t.Fatalf("changed binding %d is not a valid comparison", n)
		}
		if _, ok := c.MatchLaunchTemporaryRejection(bound, changed); ok {
			t.Fatalf("changed binding %d matched", n)
		}
	}
	for _, rejected := range []error{
		nil, raw, original,
		f.NewFault(f.InternalError, f.NotStarted).WithCause(marker),
		f.NewFault(f.CommitUnknown, f.Unknown).WithCause(marker),
		f.NewFault(f.CommitUnknown, f.NotCommitted).WithCause(marker),
		errors.Join(bound, context.Canceled), errors.Join(bound, context.DeadlineExceeded),
	} {
		if _, ok := c.MatchLaunchTemporaryRejection(rejected, r); ok {
			t.Fatal("unproven, cancelled or unknown outcome matched")
		}
	}
	if _, ok := c.MatchLaunchTemporaryRejection(bound, c.LaunchRequest{}); ok {
		t.Fatal("invalid original request matched")
	}
	if launchtemporary.MintLockTimeout("", r.Meta.RequestID, r.Meta.IdempotencyKey, raw) != nil {
		t.Fatal("invalid binding minted")
	}
	var log strings.Builder
	slog.New(slog.NewJSONHandler(&log, nil)).Error("safe", "error", marker)
	encoded, err := json.Marshal([]any{marker, bound})
	if err != nil {
		t.Fatal(err)
	}
	projections := []string{fmt.Sprintf("%v %+v %#v", bound, marker, marker), fmt.Sprintf("%#v", struct{ cause error }{marker}), string(encoded), log.String()}
	for _, projection := range projections {
		if strings.Contains(projection, "private-sql-material-canary") || strings.Contains(projection, "private-launch-key-canary") {
			t.Fatal("private diagnostic or request text escaped safe projection")
		}
	}
}

func TestLaunchTemporaryRejectionRequiresOriginalPhysicalProof(t *testing.T) {
	x := newLaunchControl(t)
	ctx := context.Background()
	raw := &pgconn.PgError{Code: "55P03", Message: "raw driver error is not adapter proof"}
	plain := f.NewFault(f.InternalError, f.NotCommitted).WithCause(raw)
	plain.RetryHint = "retry"
	rejected := f.NotCommittedResult(plain)
	command, _ := x.request.Command()
	cause, _ := f.NewCommandsCause(command)
	attempt := newTestID[f.TransactionAttempt](t)
	unknown := f.UnknownResult(attempt, cause)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	// No exported API can manufacture postgres.LockFailed. Driver SQLSTATE,
	// Fault codes, hints, zero adapter values and unbound phases are not proof.
	for _, control := range []struct {
		ctx    context.Context
		lock   error
		result f.CommitResult
	}{
		{ctx, nil, rejected}, {ctx, raw, rejected}, {ctx, plain, rejected},
		{ctx, &postgres.Error{}, rejected}, {ctx, context.DeadlineExceeded, rejected},
		{cancelled, raw, rejected}, {nil, raw, rejected},
		{ctx, raw, f.CommittedResult()}, {ctx, raw, f.CommitResult{}}, {ctx, raw, unknown},
	} {
		err := launchCommitError(control.ctx, x.request, control.lock, control.result)
		if _, ok := c.MatchLaunchTemporaryRejection(err, x.request); ok {
			t.Fatal("unproven transaction minted a temporary rejection")
		}
		if control.result.State() == f.NotCommitted {
			var got *f.Fault
			want := control.result.Fault()
			if !errors.As(err, &got) || got.Code != want.Code || got.CommitState != want.CommitState || got.RetryHint != want.RetryHint || errors.Unwrap(got) != errors.Unwrap(want) {
				t.Fatal("ordinary physical fault metadata or original cause was replaced")
			}
		}
		if control.result.State() == f.Committed && err != nil {
			t.Fatal("committed outcome changed")
		}
	}
	err := launchCommitError(cancelled, x.request, raw, unknown)
	original, ok := UnknownAttempt(err)
	if !ok || original.AttemptID() != attempt || original.Cause().Kind() != cause.Kind() || original.Cause().Details().Primary.Canonical() != command.Canonical() {
		t.Fatal("cancellation displaced the original unknown attempt")
	}
}

type temporaryLaunchStore struct {
	*launchStore
	failAt, acquisitions int
	failure              error
	unknown              bool
	returned             bool
	original             f.CommitResult
}

func (s *temporaryLaunchStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	s.acquisitions++
	if s.acquisitions == s.failAt {
		return s.failure
	}
	return s.launchStore.AcquireAll(ctx, tx, locks)
}
func (s *temporaryLaunchStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.returned = false
	result := s.launchStore.WithinTx(ctx, cause, fn)
	if s.acquisitions == s.failAt && s.unknown {
		result = f.UnknownResult(newTestID[f.TransactionAttempt](s.t), cause)
	}
	s.returned, s.original = true, result
	return result
}

func TestLaunchTemporaryRejectionDoesNotClassifyOtherPhases(t *testing.T) {
	for _, phase := range []string{"lookup", "discovery", "final-unproven-lock", "final-unknown"} {
		x := newLaunchControl(t)
		raw := &pgconn.PgError{Code: "55P03", Message: "private-sql-material-canary"}
		store := &temporaryLaunchStore{launchStore: x.store, failure: raw}
		var err error
		x.authority, err = NewAuthority(store, launchProjects{fixture: x}, nil)
		if err != nil {
			t.Fatal(err)
		}
		x.service, err = New(store, Dependencies{Authority: x.authority, Agents: x, Task: x})
		if err != nil {
			t.Fatal(err)
		}
		switch phase {
		case "lookup":
			store.failAt = 1
		case "discovery":
			x.discoverErr = raw
		case "final-unproven-lock":
			store.failAt = 2
		case "final-unknown":
			store.failAt, store.unknown = 2, true
		}
		got, err := x.service.Launch(context.Background(), x.actor, x.request)
		if err == nil || got.Execution.ID.Validate() == nil || x.store.writes != 0 || len(x.store.rows) != 0 || x.validations != 0 || x.captures != 0 {
			t.Fatalf("%s published or continued after rejection", phase)
		}
		if _, ok := c.MatchLaunchTemporaryRejection(err, x.request); ok {
			t.Fatalf("%s acquired temporary classification", phase)
		}
		if !store.returned || x.store.live || len(x.service.calls.active) != 0 {
			t.Fatalf("%s returned before original call retirement", phase)
		}
		if phase == "final-unknown" {
			original, ok := UnknownAttempt(err)
			if !ok || original.AttemptID() != store.original.AttemptID() {
				t.Fatal("original unknown attempt lost")
			}
		}
		if strings.Contains(fmt.Sprintf("%+v", err), "private-sql-material-canary") {
			t.Fatal("unclassified rejection leaked diagnostic text")
		}
	}
}
