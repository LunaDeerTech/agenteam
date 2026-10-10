package projectvariable

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func secretStoredRecord(t *testing.T, command c.SecretCommandName, value, changed bool) *secretCommandRecord {
	t.Helper()
	at, err := f.ParseInstant("2026-10-09T10:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	r := &secretCommandRecord{ID: testID[c.Operation](1), Project: testID[i.Project](2), User: testID[i.User](3),
		Command: command, Key: "private-command-canary", Target: testID[i.ProjectVariable](4), CommittedAt: at,
		NamePresent: command != c.SecretDeleteCommand, DescriptionPresent: command == c.SecretCreateCommand, ValuePresent: value}
	version, credentialVersion := f.Version(7), f.Version(2)
	kind, _ := secretMutationKind(command)
	effect := sc.ProjectVariableUnchanged
	if kind == sc.Create {
		version, credentialVersion, effect = 1, 1, sc.ProjectVariableCreated
	} else {
		expected := version
		r.Expected = &expected
		if changed {
			version++
		}
		if value || kind == sc.Delete {
			credentialVersion++
		}
		if value {
			effect = sc.ProjectVariableReplaced
		}
		if kind == sc.Delete {
			effect = sc.ProjectVariableDeleted
		}
	}
	identity, err := c.SecretVariableCommandIdentity(r.Project, command, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := i.InProject(r.Project)
	ref, _ := sc.NewCredentialRef(testID[sc.Credential](5), scope)
	r.Observation, err = sc.NewProjectVariableWriteObservation(sc.ProjectVariableWriteResultFields{
		ReceiptID: testID[sc.ProjectVariableReceipt](6), ProjectID: r.Project, VariableID: r.Target, UserID: r.User,
		Identity: identity, Kind: kind, ExpectedVersion: r.Expected, Ref: ref, Effect: effect, Version: credentialVersion, Deleted: kind == sc.Delete})
	if err != nil {
		t.Fatal(err)
	}
	fields := c.SecretVariableMutationFields{Command: command, Changed: changed}
	if kind == sc.Delete {
		fields.Deleted = &c.SecretVariableDeleted{ID: r.Target, ProjectID: r.Project, Type: c.SecretVariableType, Version: version, DeletedAt: at}
	} else {
		fields.Variable, err = c.NewSecretVariable(c.SecretVariableFields{ID: r.Target, ProjectID: r.Project, Type: c.SecretVariableType,
			Name: "CONFIG", Description: "safe-metadata", Version: version, CreatedAt: at, UpdatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
	}
	if changed {
		eid, aid := testID[event.EventIdentity](7), testID[ac.Record](8)
		fields.EventID, fields.AuditID = &eid, &aid
	}
	r.Receipt, err = c.NewSecretVariableMutation(fields)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSecretOwnerStoredRecordsFiveEffectsAndCrossBindings(t *testing.T) {
	cases := []struct {
		name           string
		command        c.SecretCommandName
		value, changed bool
	}{
		{"create", c.SecretCreateCommand, true, true},
		{"replace", c.SecretUpdateCommand, true, true},
		{"metadata", c.SecretUpdateCommand, false, true},
		{"noop", c.SecretUpdateCommand, false, false},
		{"delete", c.SecretDeleteCommand, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := secretStoredRecord(t, tc.command, tc.value, tc.changed)
			if err := validateSecretRecord(r); err != nil {
				t.Fatal("valid distinct Variable/Credential versions rejected", err)
			}
			mutations := []func(*secretCommandRecord){
				func(v *secretCommandRecord) { v.Project = testID[i.Project](21) },
				func(v *secretCommandRecord) { v.Target = testID[i.ProjectVariable](22) },
				func(v *secretCommandRecord) { v.User = testID[i.User](23) },
				func(v *secretCommandRecord) { v.Key = "different-key" },
				func(v *secretCommandRecord) { v.Observation = sc.ProjectVariableWriteNotObserved() },
				func(v *secretCommandRecord) { v.Receipt = c.SecretVariableMutation{} },
				func(v *secretCommandRecord) { v.ValuePresent = !v.ValuePresent },
			}
			for n, mutate := range mutations {
				bad := *r
				mutate(&bad)
				if validateSecretRecord(&bad) == nil {
					t.Fatalf("bad stored binding %d accepted", n)
				}
			}
		})
	}
	noop := secretStoredRecord(t, c.SecretUpdateCommand, false, false)
	noop.CommittedAt, _ = f.ParseInstant("2026-10-09T10:00:01Z")
	if validateSecretRecord(noop) != nil {
		t.Fatal("no-op must preserve prior updated_at")
	}
	changed := secretStoredRecord(t, c.SecretUpdateCommand, false, true)
	changed.CommittedAt = noop.CommittedAt
	if validateSecretRecord(changed) == nil {
		t.Fatal("changed receipt time does not match command")
	}
}

func TestSecretOwnerObservationEqualityUsesActualFields(t *testing.T) {
	r := secretStoredRecord(t, c.SecretUpdateCommand, true, true)
	original, _ := r.Observation.Result()
	changes := []func(*sc.ProjectVariableWriteResultFields){
		func(v *sc.ProjectVariableWriteResultFields) { v.ReceiptID = testID[sc.ProjectVariableReceipt](20) },
		func(v *sc.ProjectVariableWriteResultFields) { v.UserID = testID[i.User](21) },
		func(v *sc.ProjectVariableWriteResultFields) { v.VariableID = testID[i.ProjectVariable](22) },
		func(v *sc.ProjectVariableWriteResultFields) { v.Version++ },
		func(v *sc.ProjectVariableWriteResultFields) { expected := f.Version(8); v.ExpectedVersion = &expected },
		func(v *sc.ProjectVariableWriteResultFields) {
			v.Ref, _ = sc.NewCredentialRef(testID[sc.Credential](23), original.Ref.Details().Scope)
		},
	}
	for n, mutate := range changes {
		fields := original
		mutate(&fields)
		other, err := sc.NewProjectVariableWriteObservation(fields)
		if err != nil {
			t.Fatal("test requires a independently valid safe observation", err)
		}
		x, _ := json.Marshal(other)
		y, _ := json.Marshal(r.Observation)
		if string(x) != string(y) {
			t.Fatal("opaque representation premise changed")
		}
		if sameSecretObservation(other, r.Observation) {
			t.Fatalf("observation field change %d ignored", n)
		}
	}
	if !sameSecretObservation(r.Observation, r.Observation) || sameSecretObservation(sc.ProjectVariableWriteObservation{}, sc.ProjectVariableWriteObservation{}) ||
		!sameSecretObservation(sc.ProjectVariableWriteNotObserved(), sc.ProjectVariableWriteNotObserved()) ||
		sameSecretObservation(r.Observation, sc.ProjectVariableWriteNotObserved()) {
		t.Fatal("observation validity or absence confused")
	}
}

func TestSecretOwnerRecordLookupSessionSeparationAndSafeFormatting(t *testing.T) {
	r := secretStoredRecord(t, c.SecretCreateCommand, true, true)
	query, err := c.NewSecretVariableCommandLookupRequest(c.SecretVariableCommandLookupFields{
		ProjectID: r.Project, Command: r.Command, TargetID: r.Target, IdempotencyKey: r.Key})
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []int{30, 31} {
		actor, _ := i.NewHuman(r.User, testID[i.Session](session))
		request, err := secretLookupWriteRequest(actor, query)
		if err != nil || !secretRecordMatchesRequest(r, request) {
			t.Fatal("stable writer new Session must remain same original identity")
		}
		if request.Fields().Actor.Details().SessionID != actor.Details().SessionID {
			t.Fatal("current Session lost from authority request")
		}
	}
	other, _ := i.NewHuman(testID[i.User](32), testID[i.Session](33))
	request, err := secretLookupWriteRequest(other, query)
	if err != nil || secretRecordMatchesRequest(r, request) {
		t.Fatal("wrong writer matched")
	}
	encoded, err := json.Marshal(struct{ Record *secretCommandRecord }{r})
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{fmt.Sprintf("%#v", r), fmt.Sprintf("%+v", r), string(encoded), r.LogValue().String()} {
		if strings.Contains(out, string(r.Key)) || strings.Contains(out, r.Target.String()) || !strings.Contains(out, "secret_variable_record") {
			t.Fatal("internal record leaked")
		}
	}
}
