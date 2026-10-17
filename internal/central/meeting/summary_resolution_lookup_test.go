package meeting

import (
	"context"
	"testing"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestSummaryResolutionLookupRequiresOriginalPrivateRequest(t *testing.T) {
	_, owner, _, original := summaryOwnerFixture(t)
	resolve := mc.ResolveRequest{Actor: original.Actor, Consumer: original.Consumer, Purpose: original.Consumer.Purpose, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.SummarySelector}, LeaseOwner: original.LeaseOwner}
	call := &summaryModelCall{owner: owner, resolve: &resolve, resolutionUnknown: true}
	request := original.Clone()
	request.Action, request.Resolve = mc.ResolveConsumer, &resolve
	ctx := context.WithValue(context.Background(), summaryResolutionLookupKey{}, call)
	if !summaryResolutionLookup(ctx, owner, request) {
		t.Fatal("original read-only lookup rejected")
	}
	if summaryResolutionLookup(context.Background(), owner, request) {
		t.Fatal("bare original request granted recovery authority")
	}
	for _, field := range []string{"source", "generation", "action", "owner", "settled"} {
		t.Run(field, func(t *testing.T) {
			candidate := request.Clone()
			candidateOwner := owner
			switch field {
			case "source":
				candidate.Resolve.Selection = nil
				candidate.Resolve.Source = mc.ServingSnapshotSource
				candidate.Resolve.ServingSnapshotID = &original.SnapshotID
				candidate.Resolve.ServingGenerationID = owner.input.GenerationID.String()
			case "generation":
				candidate.Resolve.Consumer.OperationID = summaryTestID[struct{}](t).String()
			case "action":
				candidate.Action = mc.ReadCredentialConsumer
			case "owner":
				candidateOwner = &summaryOwner{}
			case "settled":
				call.resolutionUnknown = false
				defer func() { call.resolutionUnknown = true }()
			}
			if summaryResolutionLookup(ctx, candidateOwner, candidate) {
				t.Fatal("foreign or resolved request received cleanup authority")
			}
		})
	}
}
