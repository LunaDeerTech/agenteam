package contract

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func metadataRequest(t *testing.T, count int) ExecutionMetadataRequest {
	t.Helper()
	r := ExecutionMetadataRequest{
		ProjectID:   testID[i.Project](t),
		AgentID:     testID[i.Agent](t),
		ExecutionID: testID[i.Execution](t),
		RunnerIDs:   make([]RunnerID, count),
	}
	for n := range r.RunnerIDs {
		var err error
		r.RunnerIDs[n], err = f.ParseID[Runner](fmt.Sprintf("01900000-0000-7000-8000-%012x", n+1))
		if err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func TestExecutionMetadataRequestRequiresCanonicalBoundedIDs(t *testing.T) {
	for _, count := range []int{0, 1, 128} {
		if err := metadataRequest(t, count).Validate(); err != nil {
			t.Fatalf("valid count %d: %v", count, err)
		}
	}
	for _, name := range []string{"project", "agent", "execution", "nil", "zero", "duplicate", "descending", "oversized"} {
		t.Run(name, func(t *testing.T) {
			r := metadataRequest(t, 2)
			switch name {
			case "project":
				r.ProjectID = i.ProjectID{}
			case "agent":
				r.AgentID = i.AgentID{}
			case "execution":
				r.ExecutionID = i.ExecutionID{}
			case "nil":
				r.RunnerIDs = nil
			case "zero":
				r.RunnerIDs[0] = RunnerID{}
			case "duplicate":
				r.RunnerIDs[1] = r.RunnerIDs[0]
			case "descending":
				slices.Reverse(r.RunnerIDs)
			case "oversized":
				r = metadataRequest(t, 129)
			}
			before := slices.Clone(r.RunnerIDs)
			if r.Validate() == nil || !slices.Equal(before, r.RunnerIDs) {
				t.Fatal("invalid request accepted or normalized in place")
			}
		})
	}
}

func TestExecutionMetadataContainsOnlySafeValidatedFields(t *testing.T) {
	value := ExecutionMetadata{ID: testID[Runner](t), Name: "runner", Description: "workspace host", Version: 1}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 {
		t.Fatal("execution metadata exposed additional fields")
	}
	for _, key := range []string{"id", "name", "description", "version"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing safe field %s", key)
		}
	}
	for _, valid := range []ExecutionMetadata{
		{ID: value.ID, Name: strings.Repeat("n", 128), Description: strings.Repeat("d", 4096), Version: 1},
		{ID: value.ID, Name: "主机", Description: "", Version: 1},
	} {
		if valid.Validate() != nil {
			t.Fatal("valid safe metadata rejected")
		}
	}
	for _, name := range []string{"id", "empty-name", "long-name", "long-description", "control", "utf8", "version"} {
		t.Run(name, func(t *testing.T) {
			bad := value
			switch name {
			case "id":
				bad.ID = RunnerID{}
			case "empty-name":
				bad.Name = ""
			case "long-name":
				bad.Name = strings.Repeat("n", 129)
			case "long-description":
				bad.Description = strings.Repeat("d", 4097)
			case "control":
				bad.Description = "bad\nmetadata"
			case "utf8":
				bad.Name = string([]byte{0xff})
			case "version":
				bad.Version = 0
			}
			if bad.Validate() == nil {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
}
