package model

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelProjectCommandsAreExplicitlyUnboundWithoutIO(t *testing.T) {
	store := &noIOStore{}
	s, e := New(store, pureAuthority(t, store), testDependencies(t))
	if e != nil {
		t.Fatal(e)
	}
	scope, _ := id.InProject(mustID[id.Project](t))
	meta := mc.CommandMeta{Actor: testActor(t), Scope: scope, Key: "project-unbound"}
	ctx := context.Background()
	cases := []func() (mc.CommandReceipt, error){func() (mc.CommandReceipt, error) {
		return s.CreateProvider(ctx, mc.CreateProviderRequest{CommandMeta: meta})
	}, func() (mc.CommandReceipt, error) {
		return s.UpdateProvider(ctx, mc.UpdateProviderRequest{CommandMeta: meta})
	}, func() (mc.CommandReceipt, error) {
		return s.DeleteProvider(ctx, mc.DeleteProviderRequest{CommandMeta: meta})
	}, func() (mc.CommandReceipt, error) { return s.CreateModel(ctx, mc.CreateModelRequest{CommandMeta: meta}) }, func() (mc.CommandReceipt, error) { return s.UpdateModel(ctx, mc.UpdateModelRequest{CommandMeta: meta}) }, func() (mc.CommandReceipt, error) { return s.DeleteModel(ctx, mc.DeleteModelRequest{CommandMeta: meta}) }, func() (mc.CommandReceipt, error) {
		return s.UpdatePlatformSelection(ctx, mc.UpdatePlatformSelectionRequest{CommandMeta: meta})
	}}
	for _, run := range cases {
		receipt, e := run()
		requireCode(t, e, f.DependencyUnbound)
		if receipt != (mc.CommandReceipt{}) {
			t.Fatal("Project branch returned success receipt")
		}
	}
	if _, e = s.LookupCommand(ctx, LookupCommandRequest{Meta: meta, Command: "model.create"}); e == nil {
		t.Fatal("Project lookup accepted")
	}
}
