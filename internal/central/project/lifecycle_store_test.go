package project

import (
	"encoding/json"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func lifecycleTestCommand(t *testing.T, action c.LifecycleAction) *lifecycleCommandRecord {
	t.Helper()
	project := testProject(t)
	manifest := registryTestManifest(t, registryTestEntries())
	digest, _ := manifest.Digest()
	operation := testID[c.Operation](t)
	name, to := c.ArchiveCommand, c.Archiving
	var confirmation *c.DeleteProjectRequest
	if action == c.Delete {
		name, to = c.DeleteCommand, c.Deleting
		confirmation = &c.DeleteProjectRequest{NormalizedCurrentPath: "owner/demo", Permanent: true}
	}
	header, err := eventHeader(testID[event.EventIdentity](t), project.ID, c.LifecycleChangedEventName, 2, project.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := c.RegisterProjectEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	ev, err := typed.NewLifecycleChanged(header, c.LifecycleChangedPayload{OperationID: &operation, From: c.Active, To: to, Action: action})
	if err != nil {
		t.Fatal(err)
	}
	rawHeader, _ := ev.HeaderJSON()
	return &lifecycleCommandRecord{id: testID[struct{}](t).String(), user: project.OwnerUserID.String(), project: project.ID, name: name, key: "lifecycle-key", semantic: digest, state: "planned", manifest: manifest, header: header,
		plan: lifecycleCommandPlan{ExpectedVersion: 1, OperationID: operation, From: c.Active, To: to, Action: action, Manifest: manifest.Entries(), ManifestDigest: digest, Confirmation: confirmation, Header: rawHeader, Payload: ev.PayloadBytes()}}
}

func TestLifecyclePlanPreservesJSONBIdentityAndRejectsDrift(t *testing.T) {
	for _, action := range []c.LifecycleAction{c.Archive, c.Delete} {
		command := lifecycleTestCommand(t, action)
		if _, _, err := validateLifecyclePlan(command.project, command.name, command.plan); err != nil {
			t.Fatal(err)
		}
		copy := *command
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(command.plan.Header, &fields); err != nil {
			t.Fatal(err)
		}
		copy.plan.Header, _ = json.MarshalIndent(fields, "", "  ")
		if !sameLifecyclePlan(command, &copy) {
			t.Fatal("JSONB key order or whitespace replaced the plan")
		}
		for _, edit := range []func(*lifecycleCommandPlan){
			func(p *lifecycleCommandPlan) { p.ExpectedVersion++ },
			func(p *lifecycleCommandPlan) { p.OperationID = testID[c.Operation](t) },
			func(p *lifecycleCommandPlan) {
				p.ManifestDigest = foundation.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			},
			func(p *lifecycleCommandPlan) { p.Manifest = p.Manifest[:len(p.Manifest)-1] },
			func(p *lifecycleCommandPlan) { p.To = c.Archived },
		} {
			copy := *command
			edit(&copy.plan)
			if sameLifecyclePlan(command, &copy) {
				t.Fatal("different plan compared equal")
			}
			_, _, err := validateLifecyclePlan(command.project, command.name, copy.plan)
			hasCode(t, err, foundation.DependencyUnavailable)
		}
	}
}

func TestLifecycleProgressValidatesEntireStoredSetBeforeDTOCap(t *testing.T) {
	command := lifecycleTestCommand(t, c.Delete)
	at, _ := foundation.NewInstant(time.Now())
	r := lifecycleRecord{owner: command.user, manifest: command.manifest, digest: command.plan.ManifestDigest,
		operation: c.LifecycleOperation{ID: command.plan.OperationID, ProjectID: command.project, Action: c.Delete, ProjectVersion: 2, Version: 2, State: c.OperationStopping, CreatedAt: at, UpdatedAt: at}}
	for _, entry := range command.manifest.Entries() {
		r.participants = append(r.participants, lifecycleParticipantRecord{name: entry.Name, version: entry.ContractVersion, stop: "required", cleanup: "required"})
	}
	r.participants[0].stop = "pending"
	for range 101 {
		r.participants[0].refs = append(r.participants[0].refs, c.PendingRef{Participant: c.ArtifactObjectParticipant, Kind: "object", ID: testID[c.ResourceIdentity](t)})
	}
	if err := projectLifecycleProgress(&r); err != nil || len(r.operation.PendingResources) != 100 || !r.operation.PendingRefsTruncated {
		t.Fatal("valid progress cap", err)
	}
	r.participants[0].refs[100].Kind = "unregistered"
	hasCode(t, projectLifecycleProgress(&r), foundation.DependencyUnavailable)
	r.participants[0].refs[100].Kind = "object"
	r.participants[0].version++
	hasCode(t, projectLifecycleProgress(&r), foundation.DependencyUnavailable)
	r.participants[0].version--
	r.participants = r.participants[:len(r.participants)-1]
	hasCode(t, projectLifecycleProgress(&r), foundation.DependencyUnavailable)
}

func TestLifecycleStoredManifestAndJSONFailClosed(t *testing.T) {
	command := lifecycleTestCommand(t, c.Archive)
	manifest, err := storedLifecycleManifest(command.plan.Manifest, command.plan.ManifestDigest)
	if err != nil || manifest.Require() != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{}`, `{"unknown":true}`, `{} {}`} {
		var plan lifecycleCommandPlan
		err := decodeLifecycleJSON([]byte(raw), &plan)
		if err == nil {
			_, _, err = validateLifecyclePlan(command.project, command.name, plan)
		}
		hasCode(t, err, foundation.DependencyUnavailable)
	}
}
