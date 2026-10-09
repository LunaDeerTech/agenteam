package contract

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestTaskBlockerPageWireMaximumAndEmpty(t *testing.T) {
	v := blockerRecordFixture(t, true)
	v.Description = strings.Repeat("<", MaxTaskBlockerDescriptionBytes)
	comment := strings.Repeat(">", MaxTaskBlockerResolutionCommentBytes)
	v.ResolutionComment = &comment
	page := f.Page[TaskBlocker]{Items: make([]TaskBlocker, f.MaxPageLimit), NextCursor: strings.Repeat("c", 8192)}
	for n := range page.Items {
		page.Items[n] = v.Clone()
		page.Items[n].ID = testID[TaskBlockerIdentity](t, n+1000)
	}
	raw, err := json.Marshal(page)
	if err != nil || len(raw) > 5<<20 {
		t.Fatal("maximum escaped Blocker page does not fit", err)
	}
	var got f.Page[TaskBlocker]
	if err = json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(got, page) {
		t.Fatal("complete maximum page round trip", err)
	}
	empty, err := json.Marshal(f.Page[TaskBlocker]{})
	if err != nil || string(empty) != `{"items":[]}` {
		t.Fatal("empty page is not the closed empty projection", err)
	}
	terminal, err := json.Marshal(f.Page[TaskBlocker]{Items: []TaskBlocker{v}})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(terminal, &fields) != nil || len(fields) != 1 || fields["items"] == nil {
		t.Fatal("terminal page exposed an empty cursor")
	}
}
