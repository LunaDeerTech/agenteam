package contract_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
)

func TestAgentDirectoryEntrySafeProjectionAndStrictCodec(t *testing.T) {
	core := coreFixture(t)
	v := c.DirectoryEntry{ID: core.ID, ProjectID: core.ProjectID, Name: core.Name, DisplayName: core.DisplayName, TagColor: core.TagColor, Description: core.Description, Version: core.Version, CreatedAt: core.CreatedAt, UpdatedAt: core.UpdatedAt}
	raw := mustJSON(t, v)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 9 || string(fields["version"]) != `"1"` {
		t.Fatalf("unsafe directory shape: %v", err)
	}
	for _, key := range []string{"instructions", "model_ref", "allowed_secret_variable_ids", "approval_policy", "busy", "lifecycle"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("unexpected %s", key)
		}
	}
	var round c.DirectoryEntry
	if err := json.Unmarshal(raw, &round); err != nil || !reflect.DeepEqual(round, v) {
		t.Fatalf("roundtrip: %v", err)
	}
	copy := v.Clone()
	*copy.DisplayName = "changed"
	*copy.TagColor = "#000000"
	if *v.DisplayName == "changed" || *v.TagColor == "#000000" {
		t.Fatal("shared projection pointers")
	}
	v.DisplayName = nil
	v.TagColor = nil
	nullable := mustJSON(t, v)
	if !strings.Contains(string(nullable), `"display_name":null`) || !strings.Contains(string(nullable), `"tag_color":null`) {
		t.Fatal("nullable fields absent")
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"version":"1"`, `"version":1`, 1),
		strings.Replace(string(raw), `"version":"1"`, `"version":"0"`, 1),
		strings.Replace(string(raw), `"name":"Agent-One"`, `"name":"admin"`, 1),
		strings.Replace(string(raw), `"description":`, `"instructions":"private","description":`, 1),
		strings.Replace(string(raw), `"id":`, `"id":null,"id":`, 1),
		strings.Replace(string(raw), `"display_name":`+string(fields["display_name"])+",", "", 1),
	} {
		if json.Unmarshal([]byte(bad), &round) == nil {
			t.Fatal("invalid directory accepted")
		}
	}
}
