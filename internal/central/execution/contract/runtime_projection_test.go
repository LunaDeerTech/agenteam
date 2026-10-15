package contract

import (
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestRuntimeProjectionRequiresActualStateAndBoundedPublicItems(t *testing.T) {
	e, _ := f.NewID[i.Execution]()
	p, _ := f.NewID[i.Project]()
	r, _ := f.NewID[Round]()
	v := RuntimeProjection{Schema: PublicRuntimeSchema, ExecutionID: e, ProjectID: p, ExecutionVersion: 3, Status: Running, TranscriptThrough: 2, Items: []RuntimeProjectionItem{
		{ID: "state", Seq: 1, Revision: 3, Type: "state", State: &RuntimeStateData{Running}},
		{ID: "round/" + r.String() + "/0", Seq: 517, Revision: 1, Type: "tool", RoundID: r, TranscriptSequence: 2, Tool: &RuntimeToolData{CallID: "call-1", Name: "memory.recall"}},
	}}
	if v.Validate() != nil {
		t.Fatal("canonical pending tool rejected")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RuntimeProjection
	if json.Unmarshal(raw, &decoded) != nil || decoded.Validate() != nil {
		t.Fatal("public item wire lost typed identity")
	}
	for _, mutate := range []func(*RuntimeProjection){
		func(v *RuntimeProjection) { v.Status = Waiting },
		func(v *RuntimeProjection) { v.TranscriptThrough = 1 },
		func(v *RuntimeProjection) { v.Items[1].Type = "reasoning" },
		func(v *RuntimeProjection) { v.Items[1].Text = &RuntimeTextData{Content: "private"} },
		func(v *RuntimeProjection) { v.Items[1].Revision = 2 },
		func(v *RuntimeProjection) { v.Items[1].Seq++ },
		func(v *RuntimeProjection) { v.Items[1].ID += "0" },
	} {
		bad := v
		bad.Items = append([]RuntimeProjectionItem(nil), v.Items...)
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("unproven or contradictory runtime item accepted")
		}
	}
}
