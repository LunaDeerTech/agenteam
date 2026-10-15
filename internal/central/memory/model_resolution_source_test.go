package memory

import (
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestMemoryResolutionSourcePreservesOriginalSelection(t *testing.T) {
	project, agent := testID[i.Project](t), testID[i.Agent](t)
	operation := testID[struct{}](t).String()
	registration, _ := i.RegisterService(i.MemoryRuntime)
	scope, _ := i.InProject(project)
	actor, _ := registration.Actor(operation, scope)
	owner, _ := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, testID[mc.Call](t).String())
	version := f.Version(3)
	original := mc.ResolveRequest{Actor: actor, Consumer: mc.Consumer{Kind: mc.MemoryConsumer, ProjectID: project, AgentID: &agent, OperationID: operation, Purpose: mc.MemoryEmbedding}, Purpose: mc.MemoryEmbedding, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.EmbeddingSelector, Version: &version}, LeaseOwner: owner}
	raw, binding, err := captureModelResolution(original)
	if err != nil {
		t.Fatal(err)
	}
	// Reorder as JSONB does; current configuration is deliberately absent.
	var encoded map[string]any
	if json.Unmarshal(raw, &encoded) != nil {
		t.Fatal("invalid persisted source")
	}
	reordered, _ := json.Marshal(encoded)
	base := original.Clone()
	base.Source = ""
	base.Selection = nil
	restored, err := restoreModelResolution(reordered, binding, base)
	if err != nil || restored.Selection == nil || restored.Selection.Version == nil || *restored.Selection.Version != 3 {
		t.Fatal("original selection was not restored", err)
	}
	changed := original.Clone()
	changedVersion := f.Version(4)
	changed.Selection.Version = &changedVersion
	wrong, _, err := captureModelResolution(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restoreModelResolution(wrong, binding, base); err == nil {
		t.Fatal("changed selector revision accepted")
	}
	if _, err = restoreModelResolution(nil, binding, base); err == nil {
		t.Fatal("legacy absence became current selector")
	}
	// nil means the original caller did not request a specific selector
	// version. An observed/current version cannot be substituted on recovery.
	unversioned := original.Clone()
	unversioned.Selection.Version = nil
	unversionedRaw, unversionedBinding, err := captureModelResolution(unversioned)
	if err != nil {
		t.Fatal(err)
	}
	unversionedRestored, err := restoreModelResolution(unversionedRaw, unversionedBinding, base)
	if err != nil || unversionedRestored.Selection.Version != nil {
		t.Fatal("nil original version changed", err)
	}
	if _, err = restoreModelResolution(raw, unversionedBinding, base); err == nil {
		t.Fatal("observed version replaced original nil")
	}
	base.Consumer.ProjectID = testID[i.Project](t)
	if _, err = restoreModelResolution(raw, binding, base); err == nil {
		t.Fatal("source crossed original consumer")
	}
	serving := original.Clone()
	serving.Source = mc.ServingSnapshotSource
	serving.Selection = nil
	snapshot := testID[mc.Snapshot](t)
	serving.ServingSnapshotID = &snapshot
	serving.ServingGenerationID = testID[struct{}](t).String()
	raw, binding, err = captureModelResolution(serving)
	if err != nil {
		t.Fatal(err)
	}
	base = original.Clone()
	base.Source = ""
	base.Selection = nil
	restored, err = restoreModelResolution(raw, binding, base)
	if err != nil || restored.ServingSnapshotID == nil || *restored.ServingSnapshotID != snapshot || restored.ServingGenerationID != serving.ServingGenerationID {
		t.Fatal("serving identity changed", err)
	}
}
