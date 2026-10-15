package builtin

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

type installLookupControl struct {
	*installServiceControl
	lookup func(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error)
}

func (s *installLookupControl) LookupInstallExecution(ctx context.Context, actor id.Actor, meta f.CommandMeta, project id.ProjectID, request skill.InstallRequest) (skill.InstallReceipt, error) {
	return s.lookup(ctx, actor, meta, project, request)
}

type installLookupOperationControl struct {
	*installOperationControl
	checkLookup func(context.Context, SkillInstallCall, f.ID[f.Request]) error
}

func (a *installLookupOperationControl) RequireCurrentSkillInstallLookup(ctx context.Context, call SkillInstallCall, request f.ID[f.Request]) error {
	return a.checkLookup(ctx, call, request)
}

func TestSkillInstallLookupUsesOnlyOriginalRead(t *testing.T) {
	call := installTestCall(t, installTestPackage(t))
	want, _ := call.Details()
	actor, _ := call.Actor()
	request := installTestID[f.Request](t)
	receipt := installTestReceipt(t, call)
	reads := 0
	service := &installLookupControl{installServiceControl: &installServiceControl{install: func(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error) {
		t.Fatal("lookup called Install")
		return skill.InstallReceipt{}, nil
	}}, lookup: func(_ context.Context, got id.Actor, meta f.CommandMeta, project id.ProjectID, input skill.InstallRequest) (skill.InstallReceipt, error) {
		reads++
		if !got.Equal(actor) || meta.RequestID != request || meta.IdempotencyKey != want.Key || meta.ExpectedVersion != nil || project != want.ProjectID || input.SkillID() != want.SkillID {
			t.Fatal("lookup changed original material")
		}
		return receipt, nil
	}}
	allowed := false
	authority := &installLookupOperationControl{checkLookup: func(_ context.Context, got SkillInstallCall, gotRequest f.ID[f.Request]) error {
		details, err := got.Details()
		if err != nil || details != want || gotRequest != request || !allowed {
			return f.NewFault(f.Forbidden, f.NotStarted)
		}
		return nil
	}}
	adapter, err := NewSkillInstallAdapter(service, authority)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = adapter.Lookup(context.Background(), call, request); err == nil || reads != 0 {
		t.Fatal("bare call bypassed lookup authority")
	}
	allowed = true
	result, err := adapter.Lookup(context.Background(), call, request)
	if err != nil || result.Receipt() != receipt || reads != 1 {
		t.Fatal("original read result not projected", err)
	}
}
