package contract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestSecretCommandIdentityLookupAndReceiptUnion(t *testing.T) {
	v := testSecretVariable(t)
	version := f.Version(2)
	meta := f.CommandMeta{RequestID: testID[f.Request](10), IdempotencyKey: "secret-operation"}
	if ValidateSecretCommandMeta(SecretCreateCommand, meta) != nil {
		t.Fatal("create meta rejected")
	}
	meta.ExpectedVersion = &version
	if ValidateSecretCommandMeta(SecretCreateCommand, meta) == nil || ValidateSecretCommandMeta(SecretUpdateCommand, meta) != nil {
		t.Fatal("expected presence")
	}
	if SecretCommandName(CreateCommand).Validate() == nil || CommandName(SecretCreateCommand).Validate() == nil {
		t.Fatal("command closed sets mixed")
	}
	identity, err := SecretVariableCommandIdentity(v.Fields().ProjectID, SecretCreateCommand, meta.IdempotencyKey)
	if err != nil || identity.Namespace() != "projectvariable" || identity.Command() != "project.secret_variable.create" || len(identity.OwnerIDs()) != 1 || identity.OwnerIDs()[0] != v.Fields().ProjectID.String() {
		t.Fatal("identity")
	}
	request, err := NewSecretVariableCommandLookupRequest(SecretVariableCommandLookupFields{v.Fields().ProjectID, SecretUpdateCommand, v.Fields().ID, meta.IdempotencyKey, &version})
	if err != nil {
		t.Fatal(err)
	}
	version = 9
	*request.Fields().ExpectedVersion = 7
	if *request.Fields().ExpectedVersion != 2 {
		t.Fatal("expected alias")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SecretVariableCommandLookupRequest
	if decoded.UnmarshalJSON(raw) != nil {
		t.Fatal("lookup roundtrip")
	}
	for _, bad := range []string{strings.Replace(string(raw), `"expected_version":"2"`, `"expected_version":null`, 1), strings.Replace(string(raw), `"command":`, `"Command":`, 1), string(raw[:len(raw)-1]) + `,"semantic_digest":"sha256:` + strings.Repeat("a", 64) + `"}`, string(raw[:len(raw)-1]) + `,"value":"canary"}`} {
		if decoded.UnmarshalJSON([]byte(bad)) == nil || *decoded.Fields().ExpectedVersion != 2 {
			t.Fatal("unsafe/non-atomic lookup")
		}
	}
	eventID := testID[event.EventIdentity](11)
	auditID := testID[audit.Record](12)
	receipt, err := NewSecretVariableMutation(SecretVariableMutationFields{Command: SecretCreateCommand, Changed: true, Variable: v, EventID: &eventID, AuditID: &auditID})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"value"`)) || bytes.Contains(raw, []byte("credential")) {
		t.Fatal("unsafe receipt shape")
	}
	var receiptCopy SecretVariableMutation
	if receiptCopy.UnmarshalJSON(raw) != nil || receiptCopy.Fields().Variable.Fields() != v.Fields() {
		t.Fatal("receipt roundtrip")
	}
	for _, bad := range []string{strings.Replace(string(raw), `"changed":true`, `"changed":false`, 1), strings.Replace(string(raw), `"event_id":"`+eventID.String()+`"`, `"event_id":null`, 1), strings.Replace(string(raw), `"variable":`, `"deleted":`, 1)} {
		if receiptCopy.UnmarshalJSON([]byte(bad)) == nil {
			t.Fatal("receipt invariant")
		}
	}
	noop, err := NewSecretVariableMutation(SecretVariableMutationFields{Command: SecretUpdateCommand, Variable: v})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []SecretVariableMutation{receipt, noop} {
		lookup, err := NewSecretVariableCommandLookup(SecretLookupCommitted, &r)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(lookup)
		var copy SecretVariableCommandLookup
		if copy.UnmarshalJSON(raw) != nil || copy.Receipt() == nil {
			t.Fatal("safe history")
		}
	}
	if _, err = NewSecretVariableCommandLookup(SecretLookupNotObserved, nil); err != nil {
		t.Fatal(err)
	}
	for _, status := range []SecretLookupStatus{"in_progress", "unknown", ""} {
		if _, err = NewSecretVariableCommandLookup(status, nil); err == nil {
			t.Fatal("unsupported observed state")
		}
	}
	if _, err = NewSecretVariableCommandLookup(SecretLookupNotObserved, &receipt); err == nil {
		t.Fatal("not observed with receipt")
	}
	deleted := SecretVariableDeleted{ID: v.Fields().ID, ProjectID: v.Fields().ProjectID, Type: SecretVariableType, Version: 2, DeletedAt: v.Fields().UpdatedAt}
	dr, err := NewSecretVariableMutation(SecretVariableMutationFields{Command: SecretDeleteCommand, Changed: true, Deleted: &deleted, EventID: &eventID, AuditID: &auditID})
	if err != nil {
		t.Fatal(err)
	}
	deleted.Version = 5
	if dr.Fields().Deleted.Version != 2 {
		t.Fatal("deleted alias")
	}
	raw, _ = json.Marshal(dr)
	if receiptCopy.UnmarshalJSON(raw) != nil || receiptCopy.Fields().Deleted.Version != 2 {
		t.Fatal("delete receipt")
	}
}

func TestSecretEventCatalogIsSeparateFromOrdinary(t *testing.T) {
	cat := event.NewCatalog()
	ordinary, err := RegisterVariableEvents(cat)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := RegisterSecretVariableEvents(cat)
	if err != nil {
		t.Fatal(err)
	}
	v := testSecretVariable(t)
	version := f.Version(1)
	h := event.Header{EventID: testID[event.EventIdentity](13), EventType: SecretVariableChangedName, SchemaVersion: 1, OccurredAt: v.Fields().CreatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: testID[event.Project](2)}, AggregateType: SecretVariableAggregate, AggregateID: testID[event.Aggregate](1), AggregateVersion: &version}
	p := SecretVariableChanged{testID[i.ProjectVariable](1), testID[Operation](14), Created, []string{"created"}}
	e, err := secret.NewSecretVariableChanged(h, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ordinary.Decode(e); err == nil {
		t.Fatal("secret decoded as ordinary")
	}
	if _, err = secret.Decode(e); err != nil {
		t.Fatal(err)
	}
	other, _ := RegisterSecretVariableEvents(event.NewCatalog())
	if _, err = other.Decode(e); err == nil {
		t.Fatal("foreign issuer")
	}
	raw, _ := json.Marshal(p)
	for _, key := range []string{"value", "name", "description", "credential_ref", "value_hash"} {
		bad := append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"`+key+`":"canary"}`)...)
		if _, err = secret.Restore(h, bad); err == nil {
			t.Fatal("event material/metadata accepted")
		}
	}
	bad := h
	bad.EventType = VariableChangedName
	if _, err = secret.NewSecretVariableChanged(bad, p); err == nil {
		t.Fatal("wrong event")
	}
	bad = h
	bad.AggregateID = testID[event.Aggregate](15)
	if _, err = secret.NewSecretVariableChanged(bad, p); err == nil {
		t.Fatal("wrong aggregate")
	}
	bad = h
	bad.Scope = event.Scope{Kind: event.SystemScope}
	if _, err = secret.NewSecretVariableChanged(bad, p); err == nil {
		t.Fatal("wrong scope")
	}
}
