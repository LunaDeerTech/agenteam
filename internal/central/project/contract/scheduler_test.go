package contract

import (
	"encoding/json"
	"testing"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestSchedulerConfigPresenceAndExistingDigest(t *testing.T) {
	actor, meta, project := testHuman(2, 10), testMeta(true), testID[i.Project](1)
	name := "Name"
	before, err := UpdateDigest(actor, meta, project, UpdateProjectRequest{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	// Literal old canonical parameter shape: adding an absent capability must
	// not change saved original command digests or historical replay.
	original := digestBytes("agenteam.project.command.canonical-v1", []byte(`{"actor_user_id":"01960000-0000-7000-8000-000000000002","command":"update","expected_version":"3","parameters":{"name":"Name","normalized_name":"name"},"project_id":"01960000-0000-7000-8000-000000000001"}`))
	if before != original {
		t.Fatal("absent Scheduler changed historical canonical bytes")
	}
	for _, raw := range []string{
		`{"scheduler_enabled":false,"scheduler_max_concurrency":null}`,
		`{"scheduler_enabled":true,"scheduler_max_concurrency":2}`,
	} {
		var value ProjectSchedulerConfig
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal("explicit configuration rejected", err)
		}
		got, err := json.Marshal(value)
		if err != nil || string(got) != raw {
			t.Fatal("configuration lost presence", err)
		}
	}
	for _, raw := range []string{`{}`, `{"scheduler_enabled":null,"scheduler_max_concurrency":null}`, `{"scheduler_enabled":true,"scheduler_max_concurrency":0}`, `{"scheduler_enabled":true,"scheduler_max_concurrency":-1}`, `{"scheduler_enabled":true,"scheduler_max_concurrency":1.5}`, `{"scheduler_enabled":true,"scheduler_max_concurrency":null,"other":1}`, `{"scheduler_enabled":true,"scheduler_enabled":false,"scheduler_max_concurrency":null}`} {
		var value ProjectSchedulerConfig
		if json.Unmarshal([]byte(raw), &value) == nil {
			t.Fatal("invalid config accepted", raw)
		}
	}
	var nullPatch UpdateProjectRequest
	if json.Unmarshal([]byte(`{"scheduler":null}`), &nullPatch) == nil {
		t.Fatal("null Scheduler patch accepted")
	}
	limit := int64(2)
	config := ProjectSchedulerConfig{Enabled: true, MaxConcurrency: &limit}
	first, err := UpdateDigest(actor, meta, project, UpdateProjectRequest{Scheduler: &config})
	if err != nil {
		t.Fatal(err)
	}
	copy := config.Clone()
	limit = 3
	if *copy.MaxConcurrency != 2 {
		t.Fatal("clone aliases request limit")
	}
	second, _ := UpdateDigest(actor, meta, project, UpdateProjectRequest{Scheduler: &config})
	config.MaxConcurrency = nil
	third, _ := UpdateDigest(actor, meta, project, UpdateProjectRequest{Scheduler: &config})
	config.Enabled = false
	fourth, _ := UpdateDigest(actor, meta, project, UpdateProjectRequest{Scheduler: &config})
	if first == second || second == third || third == fourth {
		t.Fatal("explicit scheduling change omitted from digest")
	}
}
