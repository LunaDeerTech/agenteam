package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func TestAddSkillsRealPinnedBundle(t *testing.T) {
	bundle, e := AddSkills(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	id, _ := bundle.ID()
	revision, _ := bundle.Revision()
	if id != AddSkillsBundleID || revision != 1 {
		t.Fatal("identity")
	}
	p, _ := bundle.Package()
	size, _ := p.Size()
	digest, _ := p.Digest()
	if size != 2587 || digest != AddSkillsPackageSHA256 {
		t.Fatal("canonical bytes drift", size, digest)
	}
	raw, _ := p.Bytes()
	parsed, e := ParseCanonicalPackage(context.Background(), raw)
	if e != nil {
		t.Fatal(e)
	}
	again, _ := parsed.Bytes()
	if !bytes.Equal(raw, again) {
		t.Fatal("roundtrip")
	}
	m, _ := p.Manifest()
	details, _ := m.Details()
	if len(details.Files) != 1 || details.Files[0].Path != sc.EntryPath || details.Files[0].ByteSize != 2473 || details.Files[0].SHA256 != AddSkillsEntrySHA256 {
		t.Fatal("builtin manifest")
	}
	for _, instruction := range []string{"Check the skills currently available", "does not assign the skill"} {
		if !strings.Contains(addSkillsText, instruction) {
			t.Fatal("real guide missing")
		}
	}
	entry, _ := p.Entry()
	if entry.Name != AddSkillsName || entry.Description == "" {
		t.Fatal("entry")
	}
	// No automatic serialization or recursive formatting may disclose the body.
	wire, e := json.Marshal(struct {
		Bundle  BuiltinBundle
		Package Package
	}{bundle, p})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(wire), "instructions") || strings.Contains(fmt.Sprintf("%#v", struct{ private BuiltinBundle }{bundle}), "instructions") {
		t.Fatal("body leaked")
	}
	if json.Unmarshal([]byte(`{}`), &bundle) == nil {
		t.Fatal("JSON minted bundle")
	}
}
func TestAddSkillsConcurrentReadsAreIndependent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := AddSkills(context.Background())
			if e != nil {
				t.Error(e)
				return
			}
			p, _ := b.Package()
			raw, _ := p.Bytes()
			raw[0] = 0
			again, _ := p.Bytes()
			if again[0] != 'P' {
				t.Error("alias")
			}
		}()
	}
	wg.Wait()
}
