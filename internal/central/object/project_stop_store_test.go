package object

import (
	"encoding/json"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestProjectStopMappingIncludesOldCleanupClaim(t *testing.T) {
	project, _ := foundation.NewID[identity.Project]()
	process, _ := foundation.NewID[oc.Process]()
	id, _ := newWorkIdentity()
	resource, _ := newWorkIdentity()
	w := projectWork{id: id, project: project, process: process, kind: "cleanup", resource: resource, fence: 1}
	before, _ := json.Marshal(workProjection([]projectWork{w}))
	w.fence++
	after, _ := json.Marshal(workProjection([]projectWork{w}))
	if string(before) == string(after) {
		t.Fatal("claim fence missing from locked mapping")
	}
	w.joined = true
	joined, _ := json.Marshal(workProjection([]projectWork{w}))
	if string(joined) == string(after) {
		t.Fatal("join progress missing from mapping")
	}
	if stopBatchLimit != 100 || stopFanoutLimit <= stopBatchLimit {
		t.Fatal("bounded scan lost independent fanout budget")
	}
}
