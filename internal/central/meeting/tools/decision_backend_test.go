package meetingtools

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/schema"
	"github.com/dlclark/regexp2"
)

func TestDecisionBackendSchemaAcceptsActualDomainResults(t *testing.T) {
	t.Cleanup(regexp2.StopTimeoutClock)
	b, err := NewDecisionBackend(&meeting.DecisionService{})
	if err != nil {
		t.Fatal(err)
	}
	registration := b.Registration()
	if err = registration.Validate(context.Background()); err != nil {
		t.Fatal("registration", err)
	}
	compiled, err := schema.Compile(context.Background(), registration.Definition.OutputSchema)
	if err != nil {
		t.Fatal(err)
	}
	id, err := f.NewID[c.Decision]()
	if err != nil {
		t.Fatal(err)
	}
	answer := "Use option B"
	for _, v := range []c.DecisionResult{{DecisionID: id, Version: 2, Status: "answered", Answer: &answer}, {DecisionID: id, Version: 2, Status: "skipped"}} {
		raw, err := canonical(v)
		if err != nil {
			t.Fatal(err)
		}
		if err = compiled.Validate(context.Background(), raw); err != nil {
			t.Fatal("actual resolved result rejected by published Tool schema", err)
		}
	}
}
