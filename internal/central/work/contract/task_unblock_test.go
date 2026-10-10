package contract

import (
	"encoding/json"
	"reflect"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestTaskUnblockHistoryKeepsStandaloneBoundary(t *testing.T) {
	v := transitionHistoryFixture(t, TaskTransitionBlockerResolved)
	v.Payload.BlockerResolved.BlockerType = TaskBlockerTechnical
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTaskTransitionEvent(raw)
	if err != nil || !reflect.DeepEqual(v, decoded) {
		t.Fatal("Human technical resolution history", err)
	}
	standalone := TaskBlockerEventPayload{Resolved: v.Payload.BlockerResolved}
	transitionRequireFault(t, standalone.ValidateFor(TaskBlockerEventResolved), f.DependencyUnbound)
	added := TaskBlockerAddedPayload{BlockerID: v.Payload.BlockerResolved.BlockerID, BlockerType: TaskBlockerTechnical}
	transitionRequireFault(t, added.Validate(), f.DependencyUnbound)
	copy := v.Clone()
	comment := "not a per-blocker transition comment"
	copy.Payload.BlockerResolved.ResolutionComment = &comment
	if copy.Validate() == nil {
		t.Fatal("transition gained per-blocker comment")
	}
}
