package skill

import (
	"context"
	"testing"

	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func TestSkillRuntimeReadRequiresPrivateCallAndSafePath(t *testing.T) {
	inv := sc.Invocation{Intent: sc.ReadSkill}
	for _, raw := range []string{
		`{"skill_id":"00000000-0000-4000-8000-000000000001","path":"../SKILL.md"}`,
		`{"skill_id":"00000000-0000-4000-8000-000000000001","path":"/SKILL.md"}`,
		`{"skill_id":"00000000-0000-4000-8000-000000000001","path":"SKILL.md","max_bytes":262145}`,
		`{"skill_id":"00000000-0000-4000-8000-000000000001","path":"SKILL.md","object_id":"00000000-0000-4000-8000-000000000002"}`,
	} {
		if _, err := decodeRuntimeRead(inv, []byte(raw)); err == nil {
			t.Fatal("unsafe request accepted")
		}
	}
	var s *Service
	if _, err := s.ExecuteSkillReadTool(context.Background(), inv, []byte(`{}`)); err == nil {
		t.Fatal("DTO became authority")
	}
}

func TestSkillRuntimePackageTextKeepsBoundedUTF8AndExactPath(t *testing.T) {
	files, err := sc.NewTextFiles([]sc.TextFile{{Path: sc.EntryPath, UTF8Text: addSkillsText}, {Path: "references/example.txt", UTF8Text: "a中文z"}})
	if err != nil {
		t.Fatal(err)
	}
	p, err := BuildPackage(context.Background(), files)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := p.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	body, next, more, err := readRuntimePackageText(raw, runtimeReadArgs{Path: "references/example.txt", MaxBytes: 5})
	if err != nil || body != "a中" || next != 4 || !more {
		t.Fatalf("first text chunk: %q %d %v %v", body, next, more, err)
	}
	body, next, more, err = readRuntimePackageText(raw, runtimeReadArgs{Path: "references/example.txt", ByteOffset: 4, MaxBytes: 5})
	if err != nil || body != "文z" || next != 8 || more {
		t.Fatalf("continuation: %q %d %v %v", body, next, more, err)
	}
	if _, _, _, err = readRuntimePackageText(raw, runtimeReadArgs{Path: "references/example.txt", ByteOffset: 2, MaxBytes: 5}); err == nil {
		t.Fatal("mid-rune offset accepted")
	}
	if _, _, _, err = readRuntimePackageText(raw, runtimeReadArgs{Path: "other.txt", MaxBytes: 5}); err == nil {
		t.Fatal("missing file accepted")
	}
}
