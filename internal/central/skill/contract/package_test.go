package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestTextFilesRejectUnsafePathsAndCollisions(t *testing.T) {
	for _, name := range []string{"", "/a", "a/", "a//b", "a/./b", "a/../b", "C:/a", "a\\b", "a\x00b", "a\nb", string([]byte{255}), strings.Repeat("x", MaxPathBytes+1)} {
		if ValidatePath(name) == nil {
			t.Fatalf("accepted path %q", name)
		}
	}
	for _, pair := range [][2]string{{"a", "A"}, {"caf\u00e9", "cafe\u0301"}, {"stra\u00dfe", "STRASSE"}, {"a", "A/b"}, {"skill.md", EntryPath}} {
		_, e := NewTextFiles([]TextFile{{EntryPath, sampleEntry}, {pair[0], "x"}, {pair[1], "x"}})
		if e == nil {
			t.Fatal("collision accepted")
		}
	}
	files := []TextFile{{"references/cafe\u0301.txt", "explanation\n"}, {EntryPath, sampleEntry}}
	input, e := NewTextFiles(files)
	if e != nil {
		t.Fatal(e)
	}
	files[0].UTF8Text = "mutated"
	out, _ := input.Files()
	out[0].Path = "changed"
	again, _ := input.Files()
	if again[0].Path != EntryPath || again[1].Path != "references/cafe\u0301.txt" || again[1].UTF8Text != "explanation\n" {
		t.Fatal("input normalized or aliased")
	}
	wrapped := struct{ private TextFiles }{input}
	if strings.Contains(fmt.Sprintf("%#v", wrapped), "explanation") {
		t.Fatal("recursive fmt leaked body")
	}
}
func TestTextFilesContentAndLimits(t *testing.T) {
	for _, body := range []string{"", "---\nname: A\ndescription: x\n---\n", strings.Replace(sampleEntry, "\n", "\r\n", -1), sampleEntry + "\x00", sampleEntry + string([]byte{255}), strings.Replace(sampleEntry, "description: A real guide.", "description: x\nextra: y", 1)} {
		if _, e := NewTextFiles([]TextFile{{EntryPath, body}}); e == nil {
			t.Fatal("invalid entry accepted")
		}
	}
	if _, e := NewTextFiles([]TextFile{{"other.md", sampleEntry}}); e == nil {
		t.Fatal("missing entry")
	}
	if _, e := NewTextFiles([]TextFile{{EntryPath, sampleEntry}, {"asset", strings.Repeat("x", MaxFileBytes+1)}}); e == nil {
		t.Fatal("file cap")
	}
	if _, e := NewTextFiles([]TextFile{{EntryPath, sampleEntry + strings.Repeat("x", MaxEntryBytes)}}); e == nil {
		t.Fatal("entry cap")
	}
	many := []TextFile{{EntryPath, sampleEntry}}
	for i := 0; i < MaxFiles; i++ {
		many = append(many, TextFile{fmt.Sprintf("r/%03d", i), ""})
	}
	if _, e := NewTextFiles(many); e == nil {
		t.Fatal("count cap")
	}
	if _, e := NewTextFiles(many[:MaxFiles]); e != nil {
		t.Fatal("exact count", e)
	}
	big := strings.Repeat("x", MaxFileBytes)
	if _, e := NewTextFiles([]TextFile{{EntryPath, sampleEntry}, {"a", big}, {"b", big}, {"c", big}, {"d", big}}); e == nil {
		t.Fatal("total cap")
	}
}
func TestManifestImmutableAndStrictRepositoryDecode(t *testing.T) {
	digest := f.Digest("sha256:" + strings.Repeat("a", 64))
	files := []File{{"z.txt", "text/plain", 0, digest}, {EntryPath, "text/markdown", f.Progress(len(sampleEntry)), digest}}
	m, e := NewManifest(files)
	if e != nil {
		t.Fatal(e)
	}
	files[1].Path = "changed"
	d, _ := m.Details()
	d.Files[0].SHA256 = "changed"
	raw, e := m.MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := DecodeManifest(raw)
	if e != nil {
		t.Fatal(e)
	}
	h, _ := m.Digest()
	h2, _ := decoded.Digest()
	if h != h2 {
		t.Fatal("digest changed")
	}
	for _, bad := range []string{strings.Replace(string(raw), `"byte_size":"0"`, `"byte_size":0`, 1), strings.Replace(string(raw), `"files"`, `"Files"`, 1), strings.TrimSuffix(string(raw), "}") + `,"files":[]}`, strings.Replace(string(raw), `"path":"z.txt"`, `"path":"z.txt","body":"hidden"`, 1), string(raw) + "{}"} {
		if _, e := DecodeManifest([]byte(bad)); e == nil {
			t.Fatal("bad manifest accepted")
		}
	}
	if json.Unmarshal(raw, &decoded) == nil {
		t.Fatal("generic deserialize")
	}
	again, _ := decoded.Digest()
	if again != h {
		t.Fatal("failed JSON changed manifest")
	}
	if _, e := NewManifest([]File{{EntryPath, "text/markdown", 1, digest}, {"dir", "text/plain", 0, digest}, {"dir/file", "text/plain", 0, digest}}); e == nil {
		t.Fatal("ancestor collision")
	}
}
