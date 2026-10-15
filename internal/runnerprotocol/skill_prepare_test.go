package runnerprotocol_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func skillPrepareInput() p.SkillPreparePayload {
	return p.SkillPreparePayload{
		WorkspaceName: "work", SkillID: "11111111-1111-7111-8111-111111111111", RevisionID: "22222222-2222-7222-8222-222222222222", Revision: "3", ArchiveBytes: "201", ArchiveSHA256: "sha256:" + strings.Repeat("a", 64),
		Manifest: p.SkillManifest{Format: p.SkillPackageFormat, EntryPath: "SKILL.md", Files: []p.SkillFile{{Path: "SKILL.md", MediaType: "text/markdown", ByteSize: "5", SHA256: "sha256:" + strings.Repeat("b", 64)}}},
		Transfer: p.SkillTransfer{ID: "33333333-3333-7333-8333-333333333333", Method: http.MethodGet, URL: "https://objects.example/package?signature=private-value", Headers: http.Header{"Authorization": []string{"private-header"}}, ExpiresAt: "2030-01-01T00:00:00Z"},
	}
}
func TestSkillPrepareWireAndRedaction(t *testing.T) {
	v := skillPrepareInput()
	raw, err := p.EncodeSkillPreparePayload(v)
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.DecodeSkillPreparePayload(raw)
	if err != nil || !reflect.DeepEqual(got, v) {
		t.Fatal("private payload roundtrip", err)
	}
	for _, v := range []any{v, v.Transfer} {
		encoded, err := json.Marshal(v)
		if err != nil || bytes.Contains(encoded, []byte("private-")) || strings.Contains(fmt.Sprintf("%+v", v), "private-") {
			t.Fatal("private transfer escaped default formatter")
		}
	}
	path, err := p.SkillRevisionPath(v.SkillID, v.RevisionID)
	if err != nil || path != ".agenteam-skills/11111111-1111-7111-8111-111111111111/22222222-2222-7222-8222-222222222222/package" {
		t.Fatal("fixed managed path")
	}
	result := p.SkillPrepareResult{SkillID: v.SkillID, RevisionID: v.RevisionID, Revision: v.Revision, TransferID: v.Transfer.ID, ArchiveSHA256: v.ArchiveSHA256, RelativePath: path, Durable: true}
	encoded, err := p.EncodeSkillPrepareResult(result)
	if err != nil || bytes.Contains(encoded, []byte("private-")) {
		t.Fatal("result leaked material")
	}
	parsed, err := p.DecodeSkillPrepareResult(encoded)
	if err != nil || parsed != result {
		t.Fatal("result roundtrip", err)
	}
	result.RelativePath = "user-files"
	if result.Validate() == nil {
		t.Fatal("caller-controlled target accepted")
	}
}
func TestSkillPrepareWireRejectsAmbiguity(t *testing.T) {
	raw, err := p.EncodeSkillPreparePayload(skillPrepareInput())
	if err != nil {
		t.Fatal(err)
	}
	for name, alter := range map[string]func([]byte) []byte{
		"duplicate": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"revision":"3"`), []byte(`"revision":"3","revision":"4"`), 1)
		},
		"unknown": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"method":"GET"`), []byte(`"method":"GET","allow_redirect":true`), 1)
		},
		"null": func(b []byte) []byte {
			return bytes.Replace(b, []byte(`"headers":{"Authorization":["private-header"]}`), []byte(`"headers":null`), 1)
		},
		"numeric": func(b []byte) []byte { return bytes.Replace(b, []byte(`"byte_size":"5"`), []byte(`"byte_size":5`), 1) },
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := p.DecodeSkillPreparePayload(alter(raw)); err == nil {
				t.Fatal("ambiguous private payload accepted")
			}
		})
	}
}
func TestSkillPrepareManifestRejectsUnsafePackage(t *testing.T) {
	for name, alter := range map[string]func(*p.SkillPreparePayload){
		"traversal": func(v *p.SkillPreparePayload) { v.Manifest.Files[0].Path = "../SKILL.md" },
		"case_collision": func(v *p.SkillPreparePayload) {
			v.Manifest.Files = append(v.Manifest.Files, p.SkillFile{Path: "skill.md", MediaType: "text/plain", ByteSize: "0", SHA256: v.ArchiveSHA256})
		},
		"ancestor_collision": func(v *p.SkillPreparePayload) {
			v.Manifest.Files = append(v.Manifest.Files, p.SkillFile{Path: "SKILL.md/x", MediaType: "text/plain", ByteSize: "0", SHA256: v.ArchiveSHA256})
		},
		"too_large":        func(v *p.SkillPreparePayload) { v.ArchiveBytes = "9999999999" },
		"credentials":      func(v *p.SkillPreparePayload) { v.Transfer.URL = "https://user:password@objects.example/package" },
		"header_injection": func(v *p.SkillPreparePayload) { v.Transfer.Headers.Set("Authorization", "value\r\nX-Extra: 1") },
		"wrong_entry":      func(v *p.SkillPreparePayload) { v.Manifest.EntryPath = "different.md" },
		"zero_revision":    func(v *p.SkillPreparePayload) { v.Revision = "0" },
	} {
		t.Run(name, func(t *testing.T) {
			v := skillPrepareInput()
			alter(&v)
			if v.Validate() == nil {
				t.Fatal("unsafe package input accepted")
			}
		})
	}
}
