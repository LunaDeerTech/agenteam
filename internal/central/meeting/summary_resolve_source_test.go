package meeting

import (
	"context"
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func summarySourceFixture(t *testing.T, source summaryResolveSource) (*SummaryAuthority, *summaryOwner, summaryCallRecord, mc.StoppedResolutionRequest) {
	t.Helper()
	a, owner, _, original := summaryOwnerFixture(t)
	process := summaryTestID[oc.Process](t)
	processText := process.String()
	row := summaryCallRecord{call: owner.call, input: owner.input, purpose: original.Consumer.Purpose, digest: original.Input.Digest, preparedProcess: &processText}
	request, err := summaryResolveRequest(original.Actor, row, source)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := mc.ResolveBinding(request)
	if err != nil {
		t.Fatal(err)
	}
	row.resolveSource, err = json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	text := binding.String()
	row.resolveBinding = &text
	return a, owner, row, mc.StoppedResolutionRequest{Resolve: request, CallID: row.call, Input: *original.Input, PreparedProcessID: process}
}

func TestSummaryOriginalResolvePreservesNilAndExactSelectionVersion(t *testing.T) {
	for _, version := range []*f.Version{nil, new(f.Version(7))} {
		_, _, row, request := summarySourceFixture(t, summaryResolveSource{Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.MeetingSummarySelector, Version: version}})
		decoded, err := decodeSummaryResolve(request.Resolve.Actor, row)
		if err != nil || (decoded.Selection.Version == nil) != (version == nil) || version != nil && *decoded.Selection.Version != *version {
			t.Fatal("original selector version changed", err)
		}
		forged := request.Clone()
		if version == nil {
			forged.Resolve.Selection.Version = new(f.Version(7))
		} else {
			forged.Resolve.Selection.Version = nil
		}
		if matchStoppedSummaryResolve(row, forged) == nil {
			t.Fatal("replacement selector version authorized cleanup")
		}
	}
}

func TestSummaryOriginalResolveRequiresDurableBindingAndPrivateStoppedOwner(t *testing.T) {
	a, owner, row, request := summarySourceFixture(t, summaryResolveSource{Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.MeetingSummarySelector}})
	w := &summaryStoppedResolutionOwner{owner: owner, request: request}
	ctx := context.WithValue(context.Background(), summaryStoppedResolutionKey{}, w)
	if _, err := a.stoppedResolutionOwner(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := matchStoppedSummaryResolve(row, request); err != nil {
		t.Fatal(err)
	}
	if _, err := a.stoppedResolutionOwner(context.Background(), request); err == nil {
		t.Fatal("bare original DTO became cleanup authority")
	}
	for _, change := range []string{"process", "call", "input"} {
		forged := request.Clone()
		switch change {
		case "process":
			forged.PreparedProcessID = summaryTestID[oc.Process](t)
		case "call":
			forged.CallID = summaryTestID[mc.Call](t)
		case "input":
			forged.Input.SchemaVersion++
		}
		if _, err := a.stoppedResolutionOwner(ctx, forged); err == nil {
			t.Fatal("foreign stopped request accepted", change)
		}
		if matchStoppedSummaryResolve(row, forged) == nil {
			t.Fatal("foreign durable tuple accepted", change)
		}
	}
	legacy := row
	legacy.resolveSource, legacy.resolveBinding = nil, nil
	if _, err := decodeSummaryResolve(request.Resolve.Actor, legacy); !isUnknown(err) {
		t.Fatal("legacy missing source was reconstructed", err)
	}
	row.resolveSource = []byte(`{"source":"current_selection","selection":{"kind":"platform","selector":"meeting_summary"},"unexpected":true}`)
	if _, err := decodeSummaryResolve(request.Resolve.Actor, row); err == nil {
		t.Fatal("foreign source shape accepted")
	}
	owner.active = false
	if _, err := a.stoppedResolutionOwner(ctx, request); err == nil {
		t.Fatal("retired private owner accepted")
	}
}

func TestSummaryOriginalServingSourceKeepsGenerationAndSnapshot(t *testing.T) {
	a, owner, row, request := summarySourceFixture(t, summaryResolveSource{Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.MeetingSummarySelector}})
	_ = a
	_ = owner
	snapshot := summaryTestID[mc.Snapshot](t)
	source := summaryResolveSource{Source: mc.ServingSnapshotSource, Snapshot: &snapshot, Generation: row.input.GenerationID.String()}
	resolved, err := summaryResolveRequest(request.Resolve.Actor, row, source)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := mc.ResolveBinding(resolved)
	if err != nil {
		t.Fatal(err)
	}
	row.resolveSource, _ = json.Marshal(source)
	text := binding.String()
	row.resolveBinding = &text
	decoded, err := decodeSummaryResolve(request.Resolve.Actor, row)
	if err != nil || decoded.Selection != nil || decoded.ServingSnapshotID == nil || *decoded.ServingSnapshotID != snapshot || decoded.ServingGenerationID != row.input.GenerationID.String() {
		t.Fatal("fixed serving source changed", err)
	}
	source.Generation = summaryTestID[struct{}](t).String()
	if _, err := summaryResolveRequest(request.Resolve.Actor, row, source); err == nil {
		t.Fatal("foreign serving generation accepted")
	}
}
