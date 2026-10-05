package project

import (
	"context"
	"encoding/json"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestLifecycleEventRequiresExactAcceptanceAndCanonicalPayload(t *testing.T) {
	command := lifecycleTestCommand(t, c.Delete)
	typed, err := c.RegisterProjectEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	ev, err := typed.Restore(command.header, command.plan.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(command.plan.Payload, &fields); err != nil {
		t.Fatal(err)
	}
	reordered, _ := json.MarshalIndent(fields, "", "  ")
	if err = exactEvent(ev.Summary(), command.plan.Header, reordered); err != nil {
		t.Fatal("JSONB changed event identity", err)
	}
	fields["operation_id"], _ = json.Marshal(testID[c.Operation](t))
	changed, _ := json.Marshal(fields)
	hasCode(t, exactEvent(ev.Summary(), command.plan.Header, changed), foundation.Forbidden)
	p := &projectRecord{ref: testProject(t), initialized: true}
	hasCode(t, validateLifecycleAcceptedFact(context.Background(), nil, p, command), foundation.InvalidState)
}
