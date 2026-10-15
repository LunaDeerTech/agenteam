package runtime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type installRecoverySQL struct {
	*installObservedSQL
	running []any
}

func (s *installRecoverySQL) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	if strings.HasPrefix(q, "SELECT a.process_id,") {
		h := s.h
		b := h.binding
		if !reflect.DeepEqual(args, []any{h.record.ID.String(), b.ExecutionID.String(), b.ProjectID.String(), b.AgentID.String(), h.attempt.String()}) {
			s.t.Fatal("running confirmation lost original attempt/source")
		}
		return controlRow{values: s.running}
	}
	return s.installObservedSQL.QueryRow(ctx, q, args...)
}

// These controlled rows test the observation decision only. Real lock
// settlement, Unknown transactions and owner retirement require the App case.
func TestInstallRecoveryExactFinalizationObservation(t *testing.T) {
	for _, name := range []string{"committed", "running", "changed-process", "changed-request", "changed-result", "changed-running-fence", "created"} {
		t.Run(name, func(t *testing.T) {
			x := &installRecoverySQL{installObservedSQL: installObservedFixture(t)}
			h := x.h
			process, err := f.ParseID[oc.Process](x.terminal[2].(string))
			if err != nil {
				t.Fatal(err)
			}
			d := &installAuthorityState{process: process}
			observed, err := loadInstallObserved(context.Background(), x, h.binding)
			if err != nil {
				t.Fatal(err)
			}
			want := observed.terminal.clone()
			x.running = []any{process.String(), h.request.String(), int64(1), int64(1), f.Version(2), string(h.record.InputDigest), string(h.record.Fingerprint)}
			switch name {
			case "running", "changed-running-fence":
				x.metadata[14], x.metadata[15] = "running", f.Version(2)
				if name == "changed-running-fence" {
					x.running[3] = int64(2)
				}
			case "changed-process":
				x.terminal[2] = newOperationID[oc.Process](t).String()
			case "changed-request":
				x.terminal[1] = newOperationID[f.Request](t).String()
			case "changed-result":
				want.Receipt.Object = newOperationID[oc.StoredObject](t)
			case "created":
				x.metadata[14], x.metadata[15] = "created", f.Version(1)
			}
			settled, err := observeInstallFinalization(context.Background(), x, d, h, want)
			switch name {
			case "committed":
				if err != nil || !settled {
					t.Fatal("original terminal not observed", err)
				}
			case "running":
				if err != nil || settled {
					t.Fatal("exact original running tuple not distinguished", err)
				}
			default:
				if err == nil || settled {
					t.Fatal("missing original proof authorized finalization")
				}
			}
		})
	}
}

func TestInstallRecoveryNeedsReturnedOriginalKnownBackend(t *testing.T) {
	for _, name := range []string{"active", "start-unknown", "backend-unknown", "missing-final-bytes", "changed-final-bytes", "foreign-owner"} {
		t.Run(name, func(t *testing.T) {
			h, _ := installHandoffFixture(t)
			if name == "missing-final-bytes" || name == "changed-final-bytes" {
				x := installObservedFixture(t)
				observed, err := loadInstallObserved(context.Background(), x, x.h.binding)
				if err != nil {
					t.Fatal(err)
				}
				h = x.h
				h.terminal = &observed.terminal
				if name == "changed-final-bytes" {
					h.finalRaw = []byte(`{"different":"result"}`)
				}
			}
			d := &installAuthorityState{store: h.executor.core.data().store, active: map[tc.OperationID]*installHandoff{h.record.ID: h}}
			h.issuer = d
			a := &InstallAuthority{data: func() *installAuthorityState { return d }}
			service := &skill.Service{}
			adapter, err := builtin.NewSkillInstallAdapter(service, a)
			if err != nil {
				t.Fatal(err)
			}
			h.executor.source = &installSourceState{authority: a, service: service, adapter: adapter}
			h.cause = mustInstallCause(t, h)
			original := commitError(f.UnknownResult(newOperationID[f.TransactionAttempt](t), h.cause))
			h.retirement = original
			if name != "active" {
				h.live = false
				close(h.done)
			}
			if name == "backend-unknown" {
				v := terminalInstall(h, builtin.SkillInstallResult{}, original)
				h.terminal = &v
			}
			if name == "foreign-owner" {
				h.issuer = &installAuthorityState{}
			}
			err = recoverInstallFinalization(context.Background(), d, h)
			if err == nil || d.active[h.record.ID] != h || h.retirement == nil {
				t.Fatal("unsettled owner was retired")
			}
			if (name == "start-unknown" || name == "backend-unknown" || name == "missing-final-bytes") && !errors.Is(err, original) {
				t.Fatal("original Unknown provenance replaced")
			}
		})
	}
}

// This Store fails before its callback can observe or rewrite a durable row.
// It exercises the actual recovery entry point, not SQL/guard settlement.
type installRecoveryReadFailureStore struct {
	Store
	t      *testing.T
	mode   string
	cancel context.CancelFunc
	cause  f.TransactionCause
	calls  int
}

func (s *installRecoveryReadFailureStore) WithinTx(ctx context.Context, cause f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	if s.calls != 1 || cause.Details().Kind != s.cause.Details().Kind || cause.Details().Primary.Canonical() != s.cause.Details().Primary.Canonical() {
		s.t.Fatal("observation retried or changed the original target")
	}
	if s.mode == "cancelled" {
		s.cancel()
		if !errors.Is(ctx.Err(), context.Canceled) {
			s.t.Fatal("observation lost the caller cancellation")
		}
	}
	if s.mode == "read-unknown" {
		return f.UnknownResult(newOperationID[f.TransactionAttempt](s.t), cause)
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(ctx.Err()))
}

func TestInstallRecoveryPreservesOriginalUnknownOnReadFailure(t *testing.T) {
	for _, mode := range []string{"read-failed", "cancelled", "read-unknown", "already-cancelled"} {
		t.Run(mode, func(t *testing.T) {
			x := installObservedFixture(t)
			h := x.h
			observed, err := loadInstallObserved(context.Background(), x, h.binding)
			if err != nil {
				t.Fatal(err)
			}
			h.terminal = &observed.terminal
			h.finalRaw, err = encode(observed.terminal)
			if err != nil {
				t.Fatal(err)
			}
			h.cause = mustInstallCause(t, h)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &installRecoveryReadFailureStore{Store: h.executor.core.data().store, t: t, mode: mode, cancel: cancel, cause: h.cause}
			core := h.executor.core.data()
			core.store = store
			h.executor.core = &Service{data: func() serviceState { return core }}
			d := &installAuthorityState{store: store, active: map[tc.OperationID]*installHandoff{h.record.ID: h}}
			h.issuer = d
			authority := &InstallAuthority{data: func() *installAuthorityState { return d }}
			service := &skill.Service{}
			adapter, err := builtin.NewSkillInstallAdapter(service, authority)
			if err != nil {
				t.Fatal(err)
			}
			h.executor.source = &installSourceState{authority: authority, service: service, adapter: adapter}
			original := commitError(f.UnknownResult(newOperationID[f.TransactionAttempt](t), h.cause))
			h.retirement, h.live = original, false
			close(h.done)
			if mode == "already-cancelled" {
				cancel()
			}
			got := recoverInstallFinalization(ctx, d, h)
			dispatcher, call := installRecoveryDispatcherFixture(h)
			got = dispatcher.recordInstallRecoveryError(call, got)
			if call.err != original {
				t.Fatal("dispatcher changed the original Unknown after a failed read")
			}
			var originalFailure, actualFailure CommitFailure
			if !errors.Is(got, original) || !errors.As(original, &originalFailure) || !errors.As(got, &actualFailure) || actualFailure.Result().AttemptID() != originalFailure.Result().AttemptID() {
				t.Fatal("read failure replaced the original physical Unknown")
			}
			var actualFault *f.Fault
			if !errors.As(got, &actualFault) || actualFault.CommitState != f.Unknown || actualFault.CauseID != originalFailure.Result().AttemptID().String() || actualFault.RetryHint != "lookup" {
				t.Fatal("read failure became a known result")
			}
			wantCalls := 1
			if mode == "already-cancelled" {
				wantCalls = 0
			}
			if store.calls != wantCalls || d.active[h.record.ID] != h || h.retirement != original || h.live {
				t.Fatal("failed observation retired or redispatched the original owner")
			}
		})
	}
}
