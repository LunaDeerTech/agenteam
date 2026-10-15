package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func TestInstallPublicationLookupDoesNotReviveDispatch(t *testing.T) {
	h, request := installHandoffFixture(t)
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
	publication, err := f.NewCommandsCause(request.Command)
	if err != nil {
		t.Fatal(err)
	}
	original := commitError(f.UnknownResult(newOperationID[f.TransactionAttempt](t), publication))
	h.retirement = original
	terminal := terminalInstall(h, builtin.SkillInstallResult{}, original)
	h.terminal = &terminal
	if _, _, err = newInstallPublicationLookup(context.Background(), d, h); err == nil {
		t.Fatal("in-flight backend borrowed returned authority")
	}
	h.live = false
	h.cancel()
	close(h.done)
	ctx, close, err := newInstallPublicationLookup(context.Background(), d, h)
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	request.Intent = id.Read
	r := sc.InstallLookupRequest{Original: request}
	plan, err := a.DiscoverInstallLookup(ctx, r)
	if err != nil || plan.Binding().AttemptID != h.attempt.String() || h.live || h.ctx.Err() == nil {
		t.Fatal("lookup revived original dispatch or changed attempt", err)
	}
	encoded, err := json.Marshal(plan)
	if err != nil || string(encoded) != `"tool_install_lookup_plan"` || fmt.Sprintf("%+v", plan) != "tool_install_lookup_plan" || fmt.Sprintf("%#v", plan) != "tool_install_lookup_plan" || slog.AnyValue(plan).Resolve().String() != "tool_install_lookup_plan" {
		t.Fatal("public lookup plan exposed private retained call material")
	}
	if _, err = a.DiscoverInstallExecution(ctx, request); err == nil {
		t.Fatal("new read proof granted original live execution")
	}
	mutate := r
	mutate.Original.Intent = id.Mutate
	if _, err = a.DiscoverInstallLookup(ctx, mutate); err == nil {
		t.Fatal("new lookup proof granted mutation")
	}
	changed := r
	changed.Original.RequestID = newOperationID[f.Request](t)
	if _, err = a.DiscoverInstallLookup(ctx, changed); err == nil {
		t.Fatal("lookup changed the original request")
	}
	close()
	if _, err = a.DiscoverInstallLookup(ctx, r); err == nil {
		t.Fatal("expired lookup borrow remained usable")
	}
	if d.active[h.record.ID] != h || h.retirement != original || h.live {
		t.Fatal("read lifecycle retired or revived original owner")
	}
}

// Exercise the production recovery entry point. The Store refuses before its
// callback; this is identity/lifetime coverage, not a SQL or current-guard test.
func TestInstallPublicationLookupPreservesOriginalUnknown(t *testing.T) {
	for _, mode := range []string{"read-failed", "cancelled", "read-unknown", "already-cancelled"} {
		t.Run(mode, func(t *testing.T) {
			h, request := installHandoffFixture(t)
			h.cause = mustInstallCause(t, h)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &installRecoveryReadFailureStore{Store: h.executor.core.data().store, t: t, mode: mode, cancel: cancel, cause: h.cause}
			core := h.executor.core.data()
			core.store = store
			h.executor.core = &Service{data: func() serviceState { return core }}
			d := &installAuthorityState{store: store, active: map[tc.OperationID]*installHandoff{h.record.ID: h}}
			h.issuer = d
			a := &InstallAuthority{data: func() *installAuthorityState { return d }}
			service := &skill.Service{}
			adapter, err := builtin.NewSkillInstallAdapter(service, a)
			if err != nil {
				t.Fatal(err)
			}
			h.executor.source = &installSourceState{authority: a, service: service, adapter: adapter}
			publication, err := f.NewCommandsCause(request.Command)
			if err != nil {
				t.Fatal(err)
			}
			original := commitError(f.UnknownResult(newOperationID[f.TransactionAttempt](t), publication))
			terminal := terminalInstall(h, builtin.SkillInstallResult{}, original)
			h.retirement, h.terminal, h.live = original, &terminal, false
			h.cancel()
			close(h.done)
			if mode == "already-cancelled" {
				cancel()
			}
			got := recoverInstallFinalization(ctx, d, h)
			dispatcher, call := installRecoveryDispatcherFixture(h)
			got = dispatcher.recordInstallRecoveryError(call, got)
			if call.err != original {
				t.Fatal("dispatcher changed the publication Unknown after a failed read")
			}
			want, actual := installUnknown(original), installUnknown(got)
			if !errors.Is(got, original) || actual == nil || actual.Attempt != want.Attempt || actual.Primary != want.Primary {
				t.Fatal("publication observer replaced the original Unknown identity")
			}
			wantCalls := 1
			if mode == "already-cancelled" {
				wantCalls = 0
			}
			if store.calls != wantCalls || d.active[h.record.ID] != h || h.retirement != original || h.live || h.terminal.Known || len(h.finalRaw) != 0 {
				t.Fatal("unconfirmed publication was finalized or original owner restarted")
			}
		})
	}
}
