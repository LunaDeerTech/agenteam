package contract

import (
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestSkillPrepareUnknownResultClosedShape(t *testing.T) {
	skill, _ := f.ParseID[pc.Skill]("01900000-0000-7000-8000-000000000001")
	revision, _ := f.ParseID[Revision]("01900000-0000-7000-8000-000000000002")
	mount, _ := f.ParseID[id.Mount]("01900000-0000-7000-8000-000000000003")
	r := PrepareUnknownResult{SkillID: skill, RevisionID: revision, Revision: 1, MountID: mount, Outcome: "unknown", MayHavePrepared: true}
	r.Error.Code = f.DependencyUnavailable
	raw, err := EncodePrepareUnknownResult(r)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePrepareUnknownResult(raw)
	if err != nil || decoded != r {
		t.Fatal("returned unknown lost identity", err)
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"outcome":"unknown"`, `"outcome":"success"`, 1),
		strings.Replace(string(raw), `"may_have_prepared":true`, `"may_have_prepared":false`, 1),
		strings.Replace(string(raw), `"revision":"1"`, `"revision":1`, 1),
		strings.Replace(string(raw), `"revision":"1"`, `"revision":"0"`, 1),
		strings.Replace(string(raw), `"code":"DEPENDENCY_UNAVAILABLE"`, `"code":"COMMIT_UNKNOWN"`, 1),
		strings.Replace(string(raw), `"error":{`, `"error":{"path":"unproven",`, 1),
		strings.Replace(string(raw), `"outcome":`, `"outcome":"unknown","outcome":`, 1),
		strings.TrimSuffix(string(raw), "}") + `,"path":"unproven"}`,
		strings.Replace(string(raw), `"may_have_prepared":true`, `"may_have_prepared":null`, 1),
	} {
		if _, err = DecodePrepareUnknownResult([]byte(bad)); err == nil {
			t.Fatal("unknown result accepted an invalid or usable-path claim")
		}
	}
}
