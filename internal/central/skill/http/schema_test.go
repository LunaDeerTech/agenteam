package skillhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
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
	// Consume the actual Account projector, not a hand-authored Problem. The
	// existing boundary tests preserve each original fault and safe HEAD form.
	for _, code := range []f.Code{f.SessionRevoked, f.NotFound, f.InvalidState, f.ProjectNotActive, f.DependencyUnbound, f.DependencyUnavailable, f.CommitUnknown} {
		h, _, port := testHandler()
		fault := f.NewFault(code, f.NotStarted)
		if code == f.CommitUnknown {
			fault = f.NewFault(code, f.Unknown)
			fault.CauseID = testID[f.TransactionAttempt](41).String()
		}
		port.err = fault
		writer := newTestWriter()
		if serveTest(h, httptest.NewRequest("GET", testPath("/skills"), nil), writer) || writer.Code < 400 {
			t.Fatal("actual Account Problem producer failed")
		}
		problem := wireObject(t, writer.Body.Bytes())
		cases = append(cases, schemaCase{"actual Problem " + string(code), "Problem", problem, true})
		if code == f.CommitUnknown {
			for _, bad := range []struct {
				field string
				value any
			}{
				{"cause_id", fault.CauseID}, {"request_id", "bad"}, {"instance", "/api/v1?private"}, {"commit_state", "pending"},
			} {
				changed := make(map[string]any, len(problem)+1)
				for key, value := range problem {
					changed[key] = value
				}
				changed[bad.field] = bad.value
				cases = append(cases, schemaCase{"reject Problem " + bad.field, "Problem", changed, false})
			}
		}
	}
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
