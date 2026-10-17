package contract

import (
	"encoding/json"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
)

func TestTaskAttachmentsMutationContract(t *testing.T) {
	id := testID[ac.ArtifactEntity](t, 80)
	create := taskCreateFixture(t)
	create.Attachments = []ac.ArtifactID{id}
	raw, err := json.Marshal(create)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskCreate
	if err = json.Unmarshal(raw, &decoded); err != nil || len(decoded.Attachments) != 1 || decoded.Attachments[0] != id {
		t.Fatal("attachment create roundtrip", err)
	}
	clone := decoded.Clone()
	clone.Attachments[0] = testID[ac.ArtifactEntity](t, 81)
	if decoded.Attachments[0] != id {
		t.Fatal("clone mutated original attachment input")
	}
	var update TaskFieldsUpdate
	if err = json.Unmarshal([]byte(`{"attachments":[]}`), &update); err != nil || update.Attachments == nil || *update.Attachments == nil || len(*update.Attachments) != 0 {
		t.Fatal("explicit empty array must clear attachments", err)
	}
	if err = json.Unmarshal([]byte(`{"title":"Changed"}`), &update); err != nil || update.Attachments != nil {
		t.Fatal("omission must retain attachments", err)
	}
	for _, raw := range []string{
		`{"attachments":null}`,
		`{"attachments":["` + id.String() + `","` + id.String() + `"]}`,
		`{"attachments":["https://example.invalid/image.png"]}`,
		`{"attachments":[{"artifact_id":"` + id.String() + `"}]}`,
	} {
		if json.Unmarshal([]byte(raw), &TaskFieldsUpdate{}) == nil {
			t.Fatal("invalid attachment update accepted", raw)
		}
	}
	create.Attachments = nil
	for n := 0; n < 5; n++ {
		create.Attachments = append(create.Attachments, testID[ac.ArtifactEntity](t, 90+n))
	}
	if create.Validate() == nil {
		t.Fatal("more than four attachments accepted")
	}
}
