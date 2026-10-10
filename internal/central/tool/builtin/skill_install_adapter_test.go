package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

// These are explicit unit controls, not a Runtime producer or a Service
// implementation. Their acceptance never establishes a registered Backend.
type installOperationControl struct {
	check func(context.Context, SkillInstallCall) error
}

func (c *installOperationControl) RequireCurrentSkillInstall(ctx context.Context, call SkillInstallCall) error {
	return c.check(ctx, call)
}

type installServiceControl struct {
	install func(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error)
}

func (s *installServiceControl) Install(ctx context.Context, actor id.Actor, meta f.CommandMeta, project id.ProjectID, request skill.InstallRequest) (skill.InstallReceipt, error) {
	return s.install(ctx, actor, meta, project, request)
}

func TestSkillInstallAdapterRequiresCurrentOperation(t *testing.T) {
	ctx := context.Background()
	pkg := installTestPackage(t)
	call := installTestCall(t, pkg)
	details, _ := call.Details()
	actor, _ := call.Actor()
	receipt := installTestReceipt(t, call)
	calls := 0
	checks := 0
	service := &installServiceControl{install: func(gotCtx context.Context, gotActor id.Actor, meta f.CommandMeta, project id.ProjectID, request skill.InstallRequest) (skill.InstallReceipt, error) {
		calls++
		if gotCtx != ctx || !gotActor.Equal(actor) || project != details.ProjectID || meta.Validate() != nil || meta.ExpectedVersion != nil || meta.IdempotencyKey != details.Key || request.Validate() != nil || request.SkillID() != details.SkillID {
			t.Fatal("call material or identity drift")
		}
		return receipt, nil
	}}
	operation := &installOperationControl{check: func(_ context.Context, got SkillInstallCall) error {
		checks++
		gotDetails, err := got.Details()
		if err != nil {
			return err
		}
		gotActor, _ := got.Actor()
		if gotDetails != details || !gotActor.Equal(actor) {
			return f.NewFault(f.ConfirmationStale, f.NotStarted)
		}
		return nil
	}}
	for _, ports := range []struct {
		service   SkillInstallService
		operation SkillInstallOperationAuthority
	}{{nil, operation}, {service, nil}, {(*installServiceControl)(nil), operation}, {service, (*installOperationControl)(nil)}} {
		if a, err := NewSkillInstallAdapter(ports.service, ports.operation); err == nil || a != nil {
			t.Fatal("unbound dependency accepted")
		}
	}
	adapter, err := NewSkillInstallAdapter(service, operation)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result, err := adapter.Execute(ctx, call, installTestID[f.Request](t))
		if err != nil || !json.Valid(result.ModelOutput()) || result.Receipt() != receipt {
			t.Fatal("typed invocation failed")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(result.ModelOutput(), &fields); err != nil || len(fields) != 3 || fields["skill_id"] == nil || fields["revision"] == nil || fields["version"] == nil {
			t.Fatal("model result is not the exact safe projection")
		}
		output := result.ModelOutput()
		output[0] = '!'
		copyReceipt := result.Receipt()
		copyReceipt.Version = 99
		if !json.Valid(result.ModelOutput()) || result.Receipt() != receipt {
			t.Fatal("caller mutation changed the retained outcome")
		}
		encoded, marshalErr := json.Marshal(result)
		if marshalErr != nil || string(encoded) != `"skill_install_result"` || fmt.Sprintf("%+v", result) != "skill_install_result" || result.LogValue().String() != "skill_install_result" {
			t.Fatal("result formatting exposed internal receipt")
		}
	}
	if calls != 2 || checks != 2 {
		t.Fatal("invocation was skipped or retried")
	}
	// A replacement target or Operation cannot inherit the original lookup.
	for _, change := range []struct {
		operation string
		skillID   pc.SkillID
	}{{details.OperationID, installTestID[pc.Skill](t)}, {installTestID[struct{}](t).String(), details.SkillID}} {
		other, err := NewSkillInstallCall(ctx, actor, details.Spec, change.operation, change.skillID, pkg)
		if err != nil {
			t.Fatal(err)
		}
		if raw, err := adapter.Execute(ctx, other, installTestID[f.Request](t)); err == nil || !emptyInstallResult(raw) {
			t.Fatal("mismatched operation called service")
		}
	}
	if calls != 2 {
		t.Fatal("rejected call reached service")
	}
	// The Service's current Agent gate remains independently authoritative.
	service.install = func(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error) {
		calls++
		return skill.InstallReceipt{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if raw, err := adapter.Execute(ctx, call, installTestID[f.Request](t)); err == nil || !emptyInstallResult(raw) {
		t.Fatal("precheck bypassed Service gate")
	}
	if calls != 3 {
		t.Fatal("unexpected automatic retry")
	}
}

func TestSkillInstallAdapterJoinsServiceAndKeepsCommitOutcome(t *testing.T) {
	pkg := installTestPackage(t)
	call := installTestCall(t, pkg)
	operation := &installOperationControl{check: func(context.Context, SkillInstallCall) error { return nil }}
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	service := &installServiceControl{install: func(ctx context.Context, _ id.Actor, _ f.CommandMeta, _ id.ProjectID, _ skill.InstallRequest) (skill.InstallReceipt, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return skill.InstallReceipt{}, ctx.Err()
	}}
	adapter, err := NewSkillInstallAdapter(service, operation)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		raw SkillInstallResult
		err error
	}
	done := make(chan outcome, 1)
	request := installTestID[f.Request](t)
	go func() { raw, err := adapter.Execute(ctx, call, request); done <- outcome{raw, err} }()
	<-entered
	cancel()
	<-cancelled
	select {
	case <-done:
		close(release)
		t.Fatal("adapter returned while Service still held")
	default:
	}
	close(release)
	got := <-done
	if !emptyInstallResult(got.raw) || !errors.Is(got.err, context.Canceled) {
		t.Fatal("cancel did not retain original synchronous tail")
	}
	unknown := f.NewFault(f.CommitUnknown, f.Unknown)
	service.install = func(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error) {
		return skill.InstallReceipt{}, unknown
	}
	if raw, err := adapter.Execute(context.Background(), call, request); !emptyInstallResult(raw) || err != unknown {
		t.Fatal("domain unknown changed")
	}
	canary := errors.New("private-install-error-canary")
	service.install = func(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error) {
		return skill.InstallReceipt{}, canary
	}
	raw, err := adapter.Execute(context.Background(), call, request)
	var fault *f.Fault
	if !emptyInstallResult(raw) || !errors.As(err, &fault) || fault.CommitState != f.Unknown || !errors.Is(err, canary) || strings.Contains(fmt.Sprint(err), canary.Error()) {
		t.Fatal("unexpected Service failure leaked or lost unknown cause")
	}
	// A successful original Service receipt must reach the Runtime even if
	// cancellation arrives just before the call returns. This adds no work
	// to the Service and does not turn an invalid receipt into success.
	committed := installTestReceipt(t, call)
	ctx, cancel = context.WithCancel(context.Background())
	service.install = func(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error) {
		cancel()
		return committed, nil
	}
	result, err := adapter.Execute(ctx, call, request)
	if err != nil || ctx.Err() != context.Canceled || result.Receipt() != committed || !json.Valid(result.ModelOutput()) {
		t.Fatal("late cancellation erased a validated committed receipt")
	}
	bad := committed
	bad.ProjectID = installTestID[id.Project](t)
	service.install = func(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error) {
		return bad, nil
	}
	if result, err = adapter.Execute(context.Background(), call, request); !emptyInstallResult(result) || !errors.Is(err, ErrSkillInstallReceipt) {
		t.Fatal("invalid receipt escaped as a partial outcome")
	}
	// Cancellation arriving during the precheck prevents Service dispatch.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	operation.check = func(context.Context, SkillInstallCall) error { cancel(); return nil }
	service.install = func(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error) {
		t.Fatal("dispatched after cancellation")
		return skill.InstallReceipt{}, nil
	}
	if raw, err = adapter.Execute(ctx, call, request); !emptyInstallResult(raw) || !errors.Is(err, context.Canceled) {
		t.Fatal("late precheck cancellation lost")
	}
}

func emptyInstallResult(result SkillInstallResult) bool {
	return result.Receipt() == (skill.InstallReceipt{}) && result.ModelOutput() == nil
}
