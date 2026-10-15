package runtime

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func installRecoveryDispatcherFixture(h *installHandoff) (*InstallDispatcher, *installDispatchCall) {
	call := &installDispatchCall{binding: h.binding, operation: h.record.ID, done: make(chan struct{})}
	d := &InstallDispatcher{
		executor: &InstallExecutor{data: func() *installExecutorData { return h.executor }},
		calls:    map[tc.ToolCallIdentity]*installDispatchCall{h.binding.Identity(): call},
	}
	return d, call
}

// These are the exact retained post-write states of the two recovery leaves.
// They exercise the dispatcher's real final error handoff and saved call.err;
// they do not claim SQL settlement, current authority, or a physical COMMIT.
func TestInstallRecoveryDispatcherTransfersNewWriteUnknown(t *testing.T) {
	for _, source := range []string{"publication", "final-write"} {
		t.Run(source, func(t *testing.T) {
			x := installObservedFixture(t)
			h := x.h
			observed, err := loadInstallObserved(context.Background(), x, h.binding)
			if err != nil {
				t.Fatal(err)
			}
			h.cause = mustInstallCause(t, h)
			originalCause := h.cause
			if source == "publication" {
				command, err := f.NewCommandIdentity("project", []string{h.binding.ProjectID.String()}, "skill.install", h.record.Key)
				if err != nil {
					t.Fatal(err)
				}
				originalCause, err = f.NewCommandsCause(command)
				if err != nil {
					t.Fatal(err)
				}
			}
			original := commitError(f.UnknownResult(newOperationID[f.TransactionAttempt](t), originalCause))
			newAttempt := newOperationID[f.TransactionAttempt](t)
			written := commitError(f.UnknownResult(newAttempt, h.cause))
			a := &installAuthorityState{store: h.executor.core.data().store, active: map[tc.OperationID]*installHandoff{h.record.ID: h}}
			h.issuer = a
			authority := &InstallAuthority{data: func() *installAuthorityState { return a }}
			service := &skill.Service{}
			adapter, err := builtin.NewSkillInstallAdapter(service, authority)
			if err != nil {
				t.Fatal(err)
			}
			h.executor.source = &installSourceState{authority: authority, service: service, adapter: adapter}
			h.live, h.terminal, h.retirement = false, &observed.terminal, written
			h.finalRaw, err = encode(observed.terminal)
			if err != nil {
				t.Fatal(err)
			}
			close(h.done)
			d, call := installRecoveryDispatcherFixture(h)
			got := d.recordInstallRecoveryError(call, original)
			for _, actual := range []error{got, call.err} {
				var fault *f.Fault
				var commit CommitFailure
				if !errors.As(actual, &fault) || fault.CommitState != f.Unknown || fault.CauseID != newAttempt.String() || fault.RetryHint != "lookup" || !errors.As(actual, &commit) || commit.Result().AttemptID() != newAttempt {
					t.Fatal("dispatcher exposed the resolved original Unknown ahead of the new write")
				}
				if errors.Is(actual, original) || installUnknown(actual).Attempt != newAttempt {
					t.Fatal("resolved original identity remains discoverable")
				}
				if _, oldSkillCause := skill.UnknownAttempt(actual); oldSkillCause {
					t.Fatal("resolved Skill publication remained in the error tree")
				}
			}
			if a.active[h.record.ID] != h || h.retirement != written || h.live || !sameRunnerCallBinding(call.binding, h.binding) || call.operation != h.record.ID {
				t.Fatal("error handoff replaced or retired its original owner")
			}
			// No Store or adapter method is involved in this error handoff.
			if x.metadataReads != 1 || x.reads != 1 {
				t.Fatal("error handoff performed another canonical read")
			}
			// The same error value without the validated final-write bytes is
			// insufficient evidence that another physical write was attempted.
			h.finalRaw = nil
			if got = d.recordInstallRecoveryError(call, original); got != original || call.err != original {
				t.Fatal("unproven write replaced the original Unknown")
			}
		})
	}
}
