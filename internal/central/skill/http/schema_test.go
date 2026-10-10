package skillhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func TestSkillOwnerHTTPStandardSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON")
	if python == "" {
		t.Skip("set explicit installed interpreter for local standard Schema validation")
	}
	m := testMetadata()
	raw, err := encodeDetail(context.Background(), m.ProjectID, m.ID, m)
	if err != nil {
		t.Fatal(err)
	}
	base := wireObject(t, raw)
	type schemaCase struct {
		Label  string `json:"label"`
		Schema string `json:"schema"`
		Value  any    `json:"value"`
		Valid  bool   `json:"valid"`
	}
	cases := []schemaCase{{"safe detail", "SkillMetadata", base, true}}
	raw, err = encodeDirectory(context.Background(), m.ProjectID, []sc.Metadata{m})
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, schemaCase{"safe directory", "SkillDirectory", wireObject(t, raw), true})
	for _, v := range []struct {
		key   string
		value any
	}{
		{"object_id", m.ID.String()}, {"manifest", map[string]any{}}, {"package_sha256", "private"}, {"body", "private"},
		{"id", m.ID.String() + "\n"}, {"project_id", "bad"}, {"name", "bad\n"}, {"description", "bad\x00"}, {"description", " \t\n"},
		{"normalized_name", nil}, {"protected", "true"}, {"current_revision", "0"}, {"version", "01"}, {"version", "9223372036854775808"}, {"version", 1},
	} {
		copy := make(map[string]any, len(base))
		for k, x := range base {
			copy[k] = x
		}
		copy[v.key] = v.value
		cases = append(cases, schemaCase{"reject " + v.key, "SkillMetadata", copy, false})
	}
	for _, items := range []any{nil, []any{}, []any{base, base}} {
		cases = append(cases, schemaCase{"bounded directory", "SkillDirectory", map[string]any{"items": items}, false})
	}
	missing := make(map[string]any, len(base))
	for k, v := range base {
		if k != "protected" {
			missing[k] = v
		}
	}
	cases = append(cases, schemaCase{"required protected", "SkillMetadata", missing, false})
	input, err := json.Marshal(map[string]any{"cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Abs("testdata/schema.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, path)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("local Schema: %v\n%s", err, output)
	}
	t.Log(string(output))
}
