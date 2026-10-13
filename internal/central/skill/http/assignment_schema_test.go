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

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

func TestSkillAssignmentHTTPStandardSchema(t *testing.T) {
	python := os.Getenv("AGENTEAM_SKILL_HTTP_SCHEMA_PYTHON")
	if python == "" {
		t.Skip("set explicit installed interpreter for local standard Schema validation")
	}
	_, _, p := assignmentHTTPFixture()
	raw, err := encodeAssignments(context.Background(), testMetadata().ProjectID, testID[i.Agent](62), skill.OwnerAssignmentQuery{Limit: 25}, p.page)
	if err != nil {
		t.Fatal(err)
	}
	type sample struct {
		Label  string `json:"label"`
		Schema string `json:"schema"`
		Value  any    `json:"value"`
		Valid  bool   `json:"valid"`
	}
	cases := []sample{{"actual page", "AssignmentPage", wireObject(t, raw), true}, {"assign intent", "AssignCommand", wireObject(t, []byte(assignmentHTTPBody(""))), true}, {"lookup intent", "LookupCommand", wireObject(t, []byte(assignmentHTTPBody("skill.assign"))), true}}
	raw, err = json.Marshal(p.receipt)
	if err != nil {
		t.Fatal(err)
	}
	mutation := wireObject(t, raw)
	cases = append(cases, sample{"actual committed mutation", "AssignmentMutation", mutation, true})
	for _, name := range []string{"agent_version", "assignment_sequence", "assignment", "runtime"} {
		bad := wireObject(t, raw)
		delete(bad, name)
		cases = append(cases, sample{"required " + name, "AssignmentMutation", bad, false})
	}
	bad := wireObject(t, raw)
	bad["agent_version"] = 2
	cases = append(cases, sample{"numeric version", "AssignmentMutation", bad, false})
	bad = wireObject(t, raw)
	bad["object_id"] = testID[f.Request](95).String()
	cases = append(cases, sample{"private field", "AssignmentMutation", bad, false})
	cases = append(cases, sample{"not observed", "AssignmentLookup", map[string]any{"state": "not_observed", "receipt": nil}, true}, sample{"committed", "AssignmentLookup", map[string]any{"state": "committed", "receipt": mutation}, true}, sample{"false completion", "AssignmentLookup", map[string]any{"state": "committed", "receipt": nil}, false})
	input, err := json.Marshal(map[string]any{"cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Abs("testdata/assignment_schema.py")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, python, path)
	cmd.Stdin = bytes.NewReader(input)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("local assignment Schema: %v\n%s", err, output)
	}
	t.Log(string(output))
}
