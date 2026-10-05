package contract

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

const sampleEntry = "---\nname: Test Skill\ndescription: A real guide.\n---\n\nRead the authorized instructions.\n"

func sampleRevision(t *testing.T) RevisionMetadata {
	t.Helper()
	project, _ := f.ParseID[id.Project]("01900000-0000-7000-8000-000000000001")
	skill, _ := f.ParseID[pc.Skill]("01900000-0000-7000-8000-000000000002")
	rev, _ := f.ParseID[Revision]("01900000-0000-7000-8000-000000000003")
	object, _ := f.ParseID[oc.StoredObject]("01900000-0000-7000-8000-000000000004")
	scope, _ := id.InProject(project)
	now, _ := f.NewInstant(time.Unix(1700000000, 0))
	digest := f.Digest("sha256:" + strings.Repeat("1", 64))
	manifest, e := NewManifest([]File{{EntryPath, "text/markdown; charset=utf-8", f.Progress(len(sampleEntry)), digest}})
	if e != nil {
		t.Fatal(e)
	}
	return RevisionMetadata{rev, skill, project, 1, "Test Skill", "A real guide.", EntryPath, oc.ObjectMeta{ID: object, Scope: scope, MediaType: PackageMediaType, ByteSize: 512, SHA256: digest, State: oc.Available, Version: 1, CreatedAt: now}, manifest, digest, now}
}
func TestMetadataShapeAndExactStrings(t *testing.T) {
	v := sampleRevision(t)
	key, e := NormalizeName("Add Skills")
	if e != nil || key != "add-skills" {
		t.Fatal(key, e)
	}
	m := Metadata{v.SkillID, v.ProjectID, "Add Skills", key, "Guide", true, 9007199254740993, 9007199254740993}
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(raw), `"current_revision":"9007199254740993"`) {
		t.Fatal("precision")
	}
	var round Metadata
	if e = json.Unmarshal(raw, &round); e != nil || round != m {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(string(raw), `"protected":true`, `"protected":null`, 1), strings.Replace(string(raw), `"version"`, `"Version"`, 1), strings.TrimSuffix(string(raw), "}") + `,"name":"different"}`, strings.TrimSuffix(string(raw), "}") + `,"body":"hidden"}`} {
		before := round
		if json.Unmarshal([]byte(bad), &round) == nil || round != before {
			t.Fatal("invalid JSON mutated value")
		}
	}
	// A valid escaped description cannot outgrow the decoder's legitimate limit.
	m.Description = strings.Repeat("<", MaxDescriptionBytes)
	raw, e = json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &round); e != nil || round != m {
		t.Fatal("valid maximum description", e)
	}
}
func TestRevisionMetadataExactObjectBinding(t *testing.T) {
	v := sampleRevision(t)
	if _, e := NewRevisionMetadata(v); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*RevisionMetadata){func(m *RevisionMetadata) { m.EntryPath = "other" }, func(m *RevisionMetadata) { m.Object.State = oc.Pending }, func(m *RevisionMetadata) { m.Object.Scope = id.SystemScope() }, func(m *RevisionMetadata) { m.Object.SHA256 = f.Digest("sha256:" + strings.Repeat("2", 64)) }, func(m *RevisionMetadata) { m.Object.MediaType = "text/plain" }, func(m *RevisionMetadata) { m.Revision = 0 }} {
		bad := v
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid binding accepted")
		}
	}
	before := v
	raw, _ := json.Marshal(v)
	if json.Unmarshal(raw, &v) == nil || v.ID != before.ID {
		t.Fatal("JSON manufactured trusted object metadata")
	}
	for _, name := range []string{"", " a", "a ", "a/b", "a\\b", ".", "..", "a\x00b", string([]byte{255})} {
		if _, e := NormalizeName(name); e == nil {
			t.Fatal("bad name accepted")
		}
	}
}
