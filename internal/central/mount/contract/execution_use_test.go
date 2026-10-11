package contract_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
)

func mountUse(t *testing.T) mc.ExecutionMountUse {
	t.Helper()
	return mc.ExecutionMountUse{Mounts: captureSet(t), InputDigest: f.Digest("sha256:" + strings.Repeat("b", 64)), SnapshotDigest: f.Digest("sha256:" + strings.Repeat("c", 64))}
}

func TestExecutionMountUseContract(t *testing.T) {
	use := mountUse(t)
	if use.Validate() != nil {
		t.Fatal("complete nonempty use rejected")
	}
	clone := use.Clone()
	*clone.Mounts.Mounts[0].Description = "changed"
	clone.Mounts.Mounts[0].Workspace = "different"
	if *use.Mounts.Mounts[0].Description == "changed" || use.Mounts.Mounts[0].Workspace == "different" {
		t.Fatal("use clone aliases original metadata")
	}
	raw, err := json.Marshal(use)
	var decoded mc.ExecutionMountUse
	if err != nil || json.Unmarshal(raw, &decoded) != nil || decoded.Validate() != nil || decoded.Mounts.AgentVersion != 9007199254740993 || decoded.Mounts.Mounts[0].RunnerVersion != 9223372036854775807 {
		t.Fatal("use lost complete identity or decimal precision")
	}
	for name, change := range map[string]func(*mc.ExecutionMountUse){
		"nil-mounts": func(v *mc.ExecutionMountUse) { v.Mounts.Mounts = nil },
		"empty-mounts": func(v *mc.ExecutionMountUse) {
			v.Mounts.Mounts = []mc.ExecutionMountMetadata{}
			v.Mounts.AttemptBinding = ""
		},
		"missing-attempt":    func(v *mc.ExecutionMountUse) { v.Mounts.AttemptBinding = "" },
		"missing-input":      func(v *mc.ExecutionMountUse) { v.InputDigest = "" },
		"malformed-input":    func(v *mc.ExecutionMountUse) { v.InputDigest = "sha256:bad" },
		"missing-snapshot":   func(v *mc.ExecutionMountUse) { v.SnapshotDigest = "" },
		"malformed-snapshot": func(v *mc.ExecutionMountUse) { v.SnapshotDigest = "sha256:bad" },
	} {
		t.Run(name, func(t *testing.T) {
			v := use.Clone()
			change(&v)
			if v.Validate() == nil {
				t.Fatal("incomplete use accepted")
			}
		})
	}
	if fmt.Sprintf("%+v", use) != "execution_mount_use" || use.LogValue().String() != "execution_mount_use" {
		t.Fatal("use diagnostics expose metadata")
	}
}

func TestExecutionMountRetirementContract(t *testing.T) {
	r := mc.ExecutionMountRetirement{Version: 9223372036854775807, EventID: captureID[event.EventIdentity](t, 51)}
	if r.Validate() != nil {
		t.Fatal("complete terminal identity rejected")
	}
	raw, err := json.Marshal(r)
	var decoded mc.ExecutionMountRetirement
	if err != nil || json.Unmarshal(raw, &decoded) != nil || decoded != r {
		t.Fatal("terminal identity lost precision")
	}
	for _, invalid := range []mc.ExecutionMountRetirement{{Version: 1}, {EventID: r.EventID}, {Version: -1, EventID: r.EventID}} {
		if invalid.Validate() == nil {
			t.Fatal("incomplete terminal identity accepted")
		}
	}
}
