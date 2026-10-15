package meetingtools

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/schema"
	"github.com/dlclark/regexp2"
	"testing"
)

func TestAgentBackendsPublishOnlyImplementedNamesAndActualResultSchemas(t *testing.T) {
	t.Cleanup(regexp2.StopTimeoutClock)
	backends, err := NewAgentBackends(&meeting.AgentService{})
	if err != nil {
		t.Fatal(err)
	}
	if len(backends) != 4 {
		t.Fatal("pending tool accidentally published")
	}
	id, err := f.NewID[c.Meeting]()
	if err != nil {
		t.Fatal(err)
	}
	project, err := f.NewID[i.Project]()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, backend := range backends {
		registration := backend.Registration()
		if seen[registration.Binding.HandlerID] {
			t.Fatal("duplicate handler")
		}
		seen[registration.Binding.HandlerID] = true
		if err = registration.Validate(context.Background()); err != nil {
			t.Fatal(registration.Definition.Name, err)
		}
		compiled, err := schema.Compile(context.Background(), registration.Definition.OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range []c.AgentResult{{Receipt: &c.Receipt{MeetingID: id, Version: 1}}, {Meetings: []c.Session{}}, {Meetings: []c.Session{{ID: id, ProjectID: project, Status: "proposed", Version: 1}}}} {
			raw, err := canonical(value)
			if err != nil {
				t.Fatal(err)
			}
			if err = compiled.Validate(context.Background(), raw); err != nil {
				t.Fatal("actual result rejected", registration.Definition.Name, err)
			}
			if value.Meetings != nil && len(value.Meetings) == 0 && string(raw) != `{"meetings":[]}` {
				t.Fatal("empty list result omitted its explicit array")
			}
		}
		if compiled.Validate(context.Background(), []byte(`{}`)) == nil {
			t.Fatal("result without a receipt or list accepted")
		}
	}
	if seen["meeting.request-decision"] || seen["meeting.update"] || seen["meeting.comment"] {
		t.Fatal("cross-owned or undefined handler registered")
	}
}
