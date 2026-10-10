package contract

import (
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestIndependentRunnerVariableAuditPartition(t *testing.T) {
	const target = "01900000-0000-7000-8000-000000000006"
	var entries []EntryFields
	for _, action := range []Action{ProjectVariableCreate, ProjectVariableUpdate, ProjectVariableDelete, ProjectSecretVariableCreate, ProjectSecretVariableUpdate, ProjectSecretVariableDelete} {
		e := projectEntry(t, ProjectUpdate)
		fields := ProjectVariableMetadataFields{VariableID: target, Version: 2, ChangedFields: []string{"description"}}
		switch action {
		case ProjectVariableCreate, ProjectSecretVariableCreate:
			fields.Version, fields.ChangedFields = 1, []string{"created"}
		case ProjectVariableDelete, ProjectSecretVariableDelete:
			fields.ChangedFields = []string{"deleted"}
		}
		var err error
		if ProjectSecretVariableAction(action) {
			e.Metadata, err = ProjectSecretVariableMetadata(action, fields)
		} else {
			e.Metadata, err = ProjectVariableMetadata(action, fields)
		}
		if err != nil {
			t.Fatal(err)
		}
		e.Action, e.Resource = action, Resource{}
		e.Resource, err = NewResource(ProjectVariableResource, target)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	for _, action := range []Action{RunnerCreate, RunnerUpdate, RunnerEnrollmentIssue, RunnerEnroll, RunnerRevoke} {
		e := projectEntry(t, ProjectUpdate)
		e.Action, e.Scope, e.Associations = action, i.SystemScope(), Associations{RunnerID: target}
		e.Resource, _ = NewResource(RunnerResource, target)
		fields := RunnerMetadataFields{RunnerID: target, Version: 2, CredentialGeneration: 1, ChangedFields: []string{"credential"}}
		if action == RunnerCreate {
			fields.Version, fields.ChangedFields = 1, []string{"created"}
		}
		if action == RunnerUpdate {
			fields.ChangedFields = []string{"name"}
		}
		if action == RunnerEnroll {
			reg, err := i.RegisterService(i.RunnerIdentity)
			if err != nil {
				t.Fatal(err)
			}
			e.Actor, err = reg.Actor(target, e.Scope)
			if err != nil {
				t.Fatal(err)
			}
			fp := f.Digest("sha256:" + strings.Repeat("a", 64))
			fields.PublicKeyFingerprint = &fp
		}
		var err error
		e.Metadata, err = RunnerMetadata(action, fields)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	for _, original := range entries {
		t.Run(string(original.Action), func(t *testing.T) {
			if _, err := NewEntry(original); err != nil {
				t.Fatal("valid merged action rejected", err)
			}
			decoded, err := DecodeMetadata(original.Action, original.Metadata.JSON())
			if err != nil || string(decoded.JSON()) != string(original.Metadata.JSON()) {
				t.Fatal("lost merged decoder", err)
			}
			want := ProjectVariableProducer
			if RunnerAction(original.Action) {
				want = RunnerProducer
			}
			if ProducerFor(original.Action) != want {
				t.Fatal("wrong producer")
			}
			for _, other := range entries {
				if original.Action == other.Action {
					continue
				}
				bad := original
				bad.Action = other.Action
				if _, err := NewEntry(bad); err == nil {
					t.Fatal("metadata relabeled as other action", other.Action)
				}
			}
			bad := original
			if RunnerAction(original.Action) {
				bad.Scope = projectEntry(t, ProjectUpdate).Scope
			} else {
				bad.Scope = i.SystemScope()
			}
			if _, err := NewEntry(bad); err == nil {
				t.Fatal("cross scope entry accepted")
			}
		})
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, producer := range []Producer{ProjectVariableProducer, KnowledgeProducer, RunnerProducer} {
		for _, cause := range []string{digest, target} {
			for _, ordinal := range []int64{-1, 0, 1, 2} {
				_, err := NewAppendKey(producer, cause, ordinal)
				want := cause == digest && ordinal == 0
				if producer == RunnerProducer {
					want = cause == target && (ordinal == 0 || ordinal == 1)
				}
				if (err == nil) != want {
					t.Fatal("cross-producer cause rule", producer, cause == target, ordinal)
				}
			}
		}
	}
}
