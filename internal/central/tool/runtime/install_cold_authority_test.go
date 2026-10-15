package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/authorization"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Controlled private-owner shape only. It does not prove a SQL row, a bound
// guard or a stopped process. The actual SIGKILL/restart test covers those.
func installColdAuthorityFixture(t *testing.T) (*InstallAuthority, *installColdRead, sc.InstallLookupRequest) {
	t.Helper()
	h, request := installHandoffFixture(t)
	a := &installAuthorityState{store: h.executor.core.data().store, process: newOperationID[oc.Process](t), active: make(map[tc.OperationID]*installHandoff)}
	authority := &InstallAuthority{data: func() *installAuthorityState { return a }}
	service := &skill.Service{}
	adapter, err := builtin.NewSkillInstallAdapter(service, authority)
	if err != nil {
		t.Fatal(err)
	}
	h.executor.source = &installSourceState{authority: authority, service: service, adapter: adapter}
	d := &InstallDispatcher{executor: &InstallExecutor{data: func() *installExecutorData { return h.executor }}, calls: make(map[tc.ToolCallIdentity]*installDispatchCall), cold: make(map[tc.ToolCallIdentity]*installColdCall)}
	c := &installColdCall{dispatcher: d, binding: h.binding, record: h.record, origin: installColdOrigin{h.attempt, newOperationID[oc.Process](t), h.request}, proved: true, busy: true, done: make(chan struct{})}
	d.cold[h.binding.Identity()] = c
	request.Intent = id.Read
	r := &installColdRead{call: c, issuer: a, executor: h.executor, ctx: context.Background(), input: authorization.InstallInput{Binding: h.binding, Call: h.call, Fingerprint: h.record.Fingerprint}}
	return authority, r, sc.InstallLookupRequest{Original: request}
}

func TestInstallColdLookupPrivateProofAndLifetime(t *testing.T) {
	a, r, request := installColdAuthorityFixture(t)
	ctx, release := newInstallColdLookup(context.Background(), r)
	defer release()
	plan, err := a.DiscoverInstallLookup(ctx, request)
	if err != nil || plan.Binding().AttemptID != r.call.origin.attempt.String() {
		t.Fatal("original cold tuple not preserved", err)
	}
	if _, err = a.DiscoverInstallExecution(ctx, request.Original); err == nil {
		t.Fatal("cold read granted live backend execution")
	}
	if _, err = a.DiscoverInstallLookup(context.Background(), request); err == nil {
		t.Fatal("DTO granted cold read")
	}
	mutate := request
	mutate.Original.Intent = id.Mutate
	if _, err = a.DiscoverInstallLookup(ctx, mutate); err == nil {
		t.Fatal("cold lookup granted mutation")
	}
	changed := request
	changed.Original.RequestID = newOperationID[f.Request](t)
	if _, err = a.DiscoverInstallLookup(ctx, changed); err == nil {
		t.Fatal("cold lookup changed original backend request")
	}
	changed = request
	changed.Original.SkillID = newOperationID[pc.Skill](t)
	if _, err = a.DiscoverInstallLookup(ctx, changed); err == nil {
		t.Fatal("cold lookup changed target")
	}
	encoded, _ := json.Marshal(plan)
	if string(encoded) != `"tool_install_cold_lookup_plan"` || fmt.Sprintf("%#v", plan) != "tool_install_cold_lookup_plan" {
		t.Fatal("plan exposed original private package")
	}
	other, closeOther := newInstallColdLookup(context.Background(), r)
	defer closeOther()
	if err = a.RequireInstallLookupInTx(other, f.Tx{}, request, plan); err == nil {
		t.Fatal("plan transferred across original read lifetime")
	}
	release()
	if _, err = a.DiscoverInstallLookup(ctx, request); err == nil {
		t.Fatal("expired cold proof remained usable")
	}
}

func TestInstallColdLookupRejectsUnprovedOrForeignOwner(t *testing.T) {
	for _, mode := range []string{"unproved", "same-process", "not-active", "not-busy", "warm-owner"} {
		t.Run(mode, func(t *testing.T) {
			a, r, request := installColdAuthorityFixture(t)
			switch mode {
			case "unproved":
				r.call.proved = false
			case "same-process":
				r.call.origin.process = r.issuer.process
			case "not-active":
				delete(r.call.dispatcher.cold, r.call.binding.Identity())
			case "not-busy":
				r.call.busy = false
			case "warm-owner":
				r.call.dispatcher.calls[r.call.binding.Identity()] = &installDispatchCall{binding: r.call.binding}
			}
			ctx, release := newInstallColdLookup(context.Background(), r)
			defer release()
			if _, err := a.DiscoverInstallLookup(ctx, request); err == nil {
				t.Fatal("unproved cold owner granted read")
			}
		})
	}
}
