package contract

import (
	"strings"
	"testing"
)

func TestAssignmentToolArgumentsClosedMutation(t *testing.T) {
	base := `{"action":"assign","agent_id":"01900000-0000-7000-8000-000000000011","expected_version":"1","skill_id":"01900000-0000-7000-8000-000000000012","enabled":true}`
	if _, err := DecodeAssignmentToolArguments([]byte(base)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		strings.Replace(base, `"enabled":true`, `"enabled":true,"enabled":false`, 1),
		strings.Replace(base, `"enabled":true`, `"enabled":null`, 1),
		strings.Replace(base, `"expected_version":"1"`, `"expected_version":"0"`, 1),
		strings.Replace(base, `"enabled":true`, `"enabled":true,"path":"SKILL.md"`, 1),
		strings.Replace(base, `"action":"assign"`, `"action":"read"`, 1),
		base + `{}`,
	} {
		if _, err := DecodeAssignmentToolArguments([]byte(raw)); err == nil {
			t.Fatal("accepted ambiguous or unauthorized assignment intent")
		}
	}
}

func TestAssignmentCatalogIsReadOnlyAndBounded(t *testing.T) {
	a, err := DecodeAssignmentToolArguments([]byte(`{"action":"catalog"}`))
	if err != nil || a.Limit != 20 || a.AgentID.Validate() == nil {
		t.Fatal("catalog default shape")
	}
	for _, raw := range []string{
		`{"action":"catalog","agent_id":"01900000-0000-7000-8000-000000000011","limit":50}`,
		`{"action":"catalog","after_skill_id":"01900000-0000-7000-8000-000000000012","after_agent_id":"01900000-0000-7000-8000-000000000011","after_agent_created_at":"2026-01-01T00:00:00Z"}`,
	} {
		if _, err := DecodeAssignmentToolArguments([]byte(raw)); err != nil {
			t.Fatal("valid bounded metadata request rejected", err)
		}
	}
	for _, raw := range []string{
		`{"action":"catalog","expected_version":"1"}`, `{"action":"catalog","enabled":false}`,
		`{"action":"catalog","limit":51}`, `{"action":"catalog","limit":-1}`, `{"action":"catalog","agent_id":null}`,
		`{"action":"catalog","agent_id":"bad"}`, `{"action":"catalog","after_agent_id":"01900000-0000-7000-8000-000000000011"}`,
		`{"action":"catalog","after_agent_created_at":"2026-01-01T00:00:00Z"}`,
		`{"action":"catalog","agent_id":"01900000-0000-7000-8000-000000000011","after_agent_id":"01900000-0000-7000-8000-000000000011","after_agent_created_at":"2026-01-01T00:00:00Z"}`,
		`{"action":"catalog","skill_id":"01900000-0000-7000-8000-000000000012"}`, `{"action":"catalog","path":"SKILL.md"}`,
		`{"action":"catalog","action":"assign"}`,
	} {
		if _, err := DecodeAssignmentToolArguments([]byte(raw)); err == nil {
			t.Fatal("catalog accepted mutation, body path or ambiguous cursor")
		}
	}
}
