package projectvariable

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func testKeys(t *testing.T) cursor.Keyring {
	t.Helper()
	keys, e := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"` + base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))) + `"}]}`)
	if e != nil {
		t.Fatal(e)
	}
	return keys
}
func TestVariableCursorBindsOwnerProjectNameAndGeneration(t *testing.T) {
	r, _ := runtimeRecord(t)
	keys := testKeys(t)
	binding, e := pageBinding(r.Project, r.User.String())
	if e != nil {
		t.Fatal(e)
	}
	token, e := pageToken(keys, binding, 12, r.Plan.After.Summary())
	if e != nil {
		t.Fatal(e)
	}
	after, e := pageAfter(keys, token, binding, 12)
	if e != nil || after.name != "NAME" || after.id != r.Target {
		t.Fatal("roundtrip")
	}
	if _, e = pageAfter(keys, token, binding, 13); e == nil {
		t.Fatal("generation")
	}
	for _, b := range [][2]string{{r.Project.String(), testID[i.User](90).String()}, {testID[i.Project](91).String(), r.User.String()}} {
		p, _ := f.ParseID[i.Project](b[0])
		binding, _ := pageBinding(p, b[1])
		if _, e = pageAfter(keys, token, binding, 12); e == nil {
			t.Fatal("owner/project")
		}
	}
	malformed := []cursor.Position{{Scalars: []cursor.Scalar{cursor.Integer(12)}}, {Scalars: []cursor.Scalar{mustText(t, "AGENTEAM_HOME"), mustUUID(t, r.Target.String())}, OrderGeneration: ptr(int64(12))}, {Scalars: []cursor.Scalar{mustText(t, "NAME"), mustUUID(t, r.Target.String())}}}
	for _, pos := range malformed {
		token, e := keys.Sign(binding, pos)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pageAfter(keys, token, binding, 12); e == nil {
			t.Fatal("signed malformed position")
		}
	}
}
func ptr[T any](v T) *T { return &v }
func mustText(t *testing.T, s string) cursor.Scalar {
	t.Helper()
	v, e := cursor.Text(s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func mustUUID(t *testing.T, s string) cursor.Scalar {
	t.Helper()
	v, e := cursor.UUID(s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
