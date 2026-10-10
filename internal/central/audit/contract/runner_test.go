package contract

import (
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func runnerID[K any](t *testing.T) f.ID[K] {
	t.Helper()
	v, e := f.NewID[K]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestRunnerAuditClosedActionsAndMetadata(t *testing.T) {
	target := runnerID[struct{}](t).String()
	fp := f.Digest("sha256:" + strings.Repeat("a", 64))
	human, e := id.NewHuman(runnerID[id.User](t), runnerID[id.Session](t))
	if e != nil {
		t.Fatal(e)
	}
	registration, _ := id.RegisterService(id.RunnerIdentity)
	cause := runnerID[struct{}](t).String()
	service, _ := registration.Actor(cause, id.SystemScope())
	resource, _ := NewResource(RunnerResource, target)
	for _, action := range []Action{RunnerCreate, RunnerUpdate, RunnerEnrollmentIssue, RunnerEnroll, RunnerRevoke} {
		m := RunnerMetadataFields{RunnerID: target, Version: 2, CredentialGeneration: 1, ChangedFields: []string{"credential"}}
		actor := human
		switch action {
		case RunnerCreate:
			m.Version = 1
			m.ChangedFields = []string{"created"}
		case RunnerUpdate:
			m.ChangedFields = []string{"description", "name", "tags"}
		case RunnerEnroll:
			actor = service
			m.PublicKeyFingerprint = &fp
		case RunnerRevoke:
			m.PublicKeyFingerprint = &fp
		}
		metadata, e := RunnerMetadata(action, m)
		if e != nil {
			t.Fatal(action, e)
		}
		decoded, e := DecodeMetadata(action, metadata.JSON())
		if e != nil || string(decoded.JSON()) != string(metadata.JSON()) {
			t.Fatal("readback", e)
		}
		fields := EntryFields{Scope: id.SystemScope(), Actor: actor, Action: action, Outcome: Success, Resource: resource, Metadata: metadata, Associations: Associations{RunnerID: target}}
		if _, e = NewEntry(fields); e != nil {
			t.Fatal(action, e)
		}
		fields.Associations.HTTPTraceID = cause
		if _, e = NewEntry(fields); e == nil {
			t.Fatal("extra association")
		}
		fields.Associations = Associations{RunnerID: target}
		if action == RunnerEnroll {
			fields.Actor = human
		} else {
			fields.Actor = service
		}
		if _, e = NewEntry(fields); e == nil {
			t.Fatal("wrong actor kind")
		}
		if !action.Valid() || ProducerFor(action) != RunnerProducer || !RunnerResource.Valid() {
			t.Fatal("closed registration")
		}
	}
	good, _ := RunnerMetadata(RunnerUpdate, RunnerMetadataFields{RunnerID: target, Version: 2, CredentialGeneration: 1, ChangedFields: []string{"name"}})
	for _, bad := range []string{
		strings.Replace(string(good.JSON()), `"version":"2"`, `"version":"2","ver\u0073ion":"2"`, 1),
		strings.Replace(string(good.JSON()), `"version":"2"`, `"version":null`, 1),
		strings.Replace(string(good.JSON()), `"changed_fields":["name"]`, `"changed_fields":["name","description"]`, 1),
		strings.Replace(string(good.JSON()), `"changed_fields":["name"]`, `"changed_fields":["name","name"]`, 1),
		strings.Replace(string(good.JSON()), `"changed_fields":["name"]`, `"changed_fields":["root_path"]`, 1),
		strings.TrimSuffix(string(good.JSON()), "}") + `,"public_key_fingerprint":"` + string(fp) + `"}`,
		strings.TrimSuffix(string(good.JSON()), "}") + `,"token":"canary"}`,
	} {
		if _, e := DecodeMetadata(RunnerUpdate, []byte(bad)); e == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
	raw, _ := json.Marshal(Associations{RunnerID: target})
	if len(raw) == 0 {
		t.Fatal("association projection")
	}
	for _, ref := range []string{"sha256:" + strings.Repeat("a", 64), "foreign"} {
		if _, e = NewAppendKey(RunnerProducer, ref, 0); e == nil {
			t.Fatal("non UUID cause")
		}
	}
	if _, e = NewAppendKey(RunnerProducer, cause, 2); e == nil {
		t.Fatal("ordinal escaped closed range")
	}
}
