package contract

import (
	"encoding/json"
	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"strings"
	"testing"
)

func TestVariableOriginalIntentSingleVersionSource(t *testing.T) {
	actor, _ := i.NewHuman(testID[i.User](3), testID[i.Session](4))
	p := testID[i.Project](2)
	id := testID[i.ProjectVariable](1)
	version := f.Version(3)
	meta := f.CommandMeta{RequestID: testID[f.Request](5), IdempotencyKey: "original-key", ExpectedVersion: &version}
	original, err := VariableCommandDigest(actor, meta, p, id, DeleteCommand, nil)
	if err != nil {
		t.Fatal(err)
	}
	sameUser, _ := i.NewHuman(testID[i.User](3), testID[i.Session](6))
	next := meta
	next.RequestID = testID[f.Request](7)
	got, err := VariableCommandDigest(sameUser, next, p, id, DeleteCommand, nil)
	if err != nil || got != original {
		t.Fatal("Session/RequestID leaked into intent")
	}
	for _, change := range []func(*f.CommandMeta){func(m *f.CommandMeta) { m.ExpectedVersion = nil }, func(m *f.CommandMeta) { zero := f.Version(0); m.ExpectedVersion = &zero }} {
		bad := meta
		change(&bad)
		if _, err = VariableCommandDigest(actor, bad, p, id, DeleteCommand, nil); err == nil {
			t.Fatal("missing or zero delete version")
		}
	}
	version++
	other, err := VariableCommandDigest(actor, meta, p, id, DeleteCommand, nil)
	if err != nil || other == original {
		t.Fatal("delete version not bound")
	}
	req, _ := NewVariableCreate(VariableCreateFields{id, "NAME", "", ""})
	if _, err = VariableCommandDigest(actor, meta, p, id, CreateCommand, req); err == nil {
		t.Fatal("create accepted version")
	}
	meta.ExpectedVersion = nil
	if _, err = VariableCommandDigest(actor, meta, p, id, CreateCommand, req); err != nil {
		t.Fatal(err)
	}
	empty := ""
	r1, _ := NewVariableUpdate(VariableUpdateFields{Name: ptr("NAME")})
	r2, _ := NewVariableUpdate(VariableUpdateFields{Name: ptr("NAME"), Value: &empty})
	meta.ExpectedVersion = &version
	d1, _ := VariableCommandDigest(actor, meta, p, id, UpdateCommand, r1)
	d2, _ := VariableCommandDigest(actor, meta, p, id, UpdateCommand, r2)
	if d1 == d2 {
		t.Fatal("presence lost")
	}
}
func ptr(v string) *string { return &v }
func TestVariableReceiptUnionLookupAndHistoricalValue(t *testing.T) {
	v := testVariable(t)
	eventID := testID[event.EventIdentity](8)
	auditID := testID[audit.Record](9)
	receipt, err := NewVariableMutation(VariableMutationFields{Command: CreateCommand, Changed: true, Variable: v, EventID: &eventID, AuditID: &auditID})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var decoded VariableMutation
	if json.Unmarshal(raw, &decoded) != nil || decoded.Fields().Variable.Fields().Value != v.Fields().Value {
		t.Fatal("receipt roundtrip")
	}
	for _, bad := range []string{strings.Replace(string(raw), `"changed":true`, `"changed":false`, 1), strings.Replace(string(raw), `"event_id":"`+eventID.String()+`"`, `"event_id":null`, 1), strings.Replace(string(raw), `"variable":`, `"deleted":`, 1)} {
		if json.Unmarshal([]byte(bad), &decoded) == nil {
			t.Fatal("bad receipt union")
		}
	}
	noop, err := NewVariableMutation(VariableMutationFields{Command: UpdateCommand, Variable: v})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewVariableMutation(VariableMutationFields{Command: CreateCommand, Variable: v}); err == nil {
		t.Fatal("no-op create")
	}
	for _, r := range []VariableMutation{receipt, noop} {
		lookup, err := NewVariableCommandLookup(LookupCommitted, &r)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(lookup)
		var result VariableCommandLookup
		if json.Unmarshal(raw, &result) != nil || result.Receipt() == nil {
			t.Fatal("lookup receipt")
		}
	}
	for _, status := range []LookupStatus{LookupNotObserved, LookupInProgress} {
		if _, err = NewVariableCommandLookup(status, &receipt); err == nil {
			t.Fatal("unexpected receipt")
		}
		if _, err = NewVariableCommandLookup(status, nil); err != nil {
			t.Fatal(err)
		}
	}
	deleted := VariableDeleted{ID: v.Fields().ID, ProjectID: v.Fields().ProjectID, Type: VariableType, Version: 2, DeletedAt: v.Fields().UpdatedAt}
	dr, err := NewVariableMutation(VariableMutationFields{Command: DeleteCommand, Changed: true, Deleted: &deleted, EventID: &eventID, AuditID: &auditID})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(dr)
	if json.Unmarshal(raw, &decoded) != nil || decoded.Fields().Deleted.Version != 2 {
		t.Fatal("delete receipt")
	}
	deleted.Version = 4
	if dr.Fields().Deleted.Version != 2 {
		t.Fatal("delete alias")
	}

}
