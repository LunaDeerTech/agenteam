package contract

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func secretReferenceFixture(t *testing.T) SecretReferenceChange {
	t.Helper()
	actor, err := i.NewHuman(testID[i.User](3), testID[i.Session](4))
	if err != nil {
		t.Fatal(err)
	}
	p := testID[i.Project](2)
	command, err := f.NewCommandIdentity("project", []string{p.String()}, "agent.create", "reference-key")
	if err != nil {
		t.Fatal(err)
	}
	return SecretReferenceChange{Actor: actor, ProjectID: p, AgentID: testID[i.Agent](20), Command: command, Operation: SecretReferenceCreate, ResultOwnerVersion: 1, After: []VariableID{testID[i.ProjectVariable](1)}}
}
func TestSecretDirectoryPlansBindIdentityAndCompleteLocks(t *testing.T) {
	r := secretReferenceFixture(t)
	mapping := f.Digest("sha256:" + strings.Repeat("a", 64))
	issuer := NewSecretPlanIssuer()
	ownerIssuer := NewSecretPlanIssuer()
	locks := secretBaseLocks(r.Actor, r.ProjectID, r.Command, &r.AgentID)
	owner, err := NewSecretReferenceOwnerPlan(ownerIssuer, r, mapping, locks)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := NewSecretReferencePlan(issuer, r, mapping, locks, owner)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SecretReferenceBinding(r)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Matches(issuer, b, mapping) || plan.Matches(ownerIssuer, b, mapping) || plan.Matches(issuer, b, f.Digest("sha256:"+strings.Repeat("b", 64))) {
		t.Fatal("issuer/mapping binding")
	}
	copy := r.Clone()
	copy.After[0] = testID[i.ProjectVariable](21)
	other, _ := SecretReferenceBinding(copy)
	if other == b || plan.Matches(issuer, other, mapping) {
		t.Fatal("set binding")
	}
	copy = r.Clone()
	copy.Actor, _ = i.NewHuman(testID[i.User](3), testID[i.Session](5))
	other, _ = SecretReferenceBinding(copy)
	if other == b {
		t.Fatal("plan reused across Session")
	}
	copy = r.Clone()
	copy.AgentID = testID[i.Agent](22)
	if _, err = NewSecretReferencePlan(issuer, copy, mapping, locks, owner); err == nil {
		t.Fatal("foreign owner plan")
	}
	for n := range locks {
		incomplete := append([]f.LockRequest{}, locks[:n]...)
		incomplete = append(incomplete, locks[n+1:]...)
		if _, err = NewSecretReferenceOwnerPlan(ownerIssuer, r, mapping, incomplete); err == nil {
			t.Fatal("missing base lock")
		}
		if _, err = NewSecretReferencePlan(issuer, r, mapping, incomplete, owner); err == nil {
			t.Fatal("missing union lock")
		}
	}
	returned := plan.RequiredLocks()
	returned[0].Mode = f.Shared
	if plan.RequiredLocks()[0].Mode != f.Exclusive {
		t.Fatal("lock alias")
	}
	details := plan.Details()
	details.Locks[0].Mode = f.Shared
	if plan.Details().Locks[0].Mode != f.Exclusive {
		t.Fatal("details alias")
	}
	for _, value := range []any{plan, owner, issuer, struct{ Plan SecretReferencePlan }{plan}} {
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), "reference-key") || strings.Contains(fmt.Sprintf("%#v", value), r.AgentID.String()) {
			t.Fatal("plan serialization disclosure")
		}
	}
	var decoded SecretReferencePlan
	if json.Unmarshal([]byte(`"secret_reference_plan"`), &decoded) == nil || decoded.Validate() == nil {
		t.Fatal("plan deserialized")
	}
	request := SecretDirectoryRequest{r.Actor, r.ProjectID, r.Command, r.After}
	directory, err := NewSecretDirectoryPlan(issuer, request, mapping, locks)
	if err != nil {
		t.Fatal(err)
	}
	db, _ := SecretDirectoryBinding(request)
	if !directory.Matches(issuer, db, mapping) || directory.Matches(ownerIssuer, db, mapping) {
		t.Fatal("directory issuer")
	}
	valid := testSecretVariable(t)
	facts, err := NewSecretDirectoryFacts(request, []SecretDirectoryEntry{{ID: r.After[0], Status: SecretDirectoryValid, Variable: &valid}})
	if err != nil {
		t.Fatal(err)
	}
	entries := facts.Entries()
	entries[0].Variable = nil
	if facts.Entries()[0].Variable == nil {
		t.Fatal("facts alias")
	}
	foreign := valid.Fields()
	foreign.ProjectID = testID[i.Project](99)
	wrong, _ := NewSecretVariable(foreign)
	for _, entries := range [][]SecretDirectoryEntry{nil, {{ID: r.After[0], Status: SecretDirectoryValid, Variable: &wrong}}, {{ID: r.After[0], Status: SecretDirectoryNotInScope, Variable: &valid}}, {{ID: r.After[0], Status: "disabled"}}} {
		if _, err = NewSecretDirectoryFacts(request, entries); err == nil {
			t.Fatal("invalid directory facts")
		}
	}
	for _, status := range []SecretDirectoryStatus{SecretDirectoryNotInScope, SecretDirectoryRemoved} {
		if _, err = NewSecretDirectoryFacts(request, []SecretDirectoryEntry{{ID: r.After[0], Status: status}}); err != nil {
			t.Fatal(err)
		}
	}
}
func TestSecretReferenceChangeShapeAndSetSemantics(t *testing.T) {
	r := secretReferenceFixture(t)
	for _, change := range []func(*SecretReferenceChange){
		func(v *SecretReferenceChange) { n := f.Version(1); v.ExpectedOwnerVersion = &n },
		func(v *SecretReferenceChange) { v.Before = []VariableID{v.After[0]} },
		func(v *SecretReferenceChange) { v.After = append(v.After, v.After[0]) },
		func(v *SecretReferenceChange) { v.ProjectID = testID[i.Project](30) },
		func(v *SecretReferenceChange) { v.ResultOwnerVersion = 2 },
		func(v *SecretReferenceChange) { v.Operation = "delete" },
	} {
		copy := r.Clone()
		change(&copy)
		if copy.Validate() == nil {
			t.Fatal("invalid create reference")
		}
	}
	r.After = make([]VariableID, MaxSecretReferences)
	for n := range r.After {
		r.After[n] = testID[i.ProjectVariable](n + 1)
	}
	if r.Validate() != nil {
		t.Fatal("256 references rejected")
	}
	r.After = append(r.After, testID[i.ProjectVariable](300))
	if r.Validate() == nil {
		t.Fatal("reference cap")
	}
	r = secretReferenceFixture(t)
	r.After = nil
	b1, err := SecretReferenceBinding(r)
	if err != nil {
		t.Fatal(err)
	}
	r.After = []VariableID{}
	r.Before = []VariableID{}
	b2, _ := SecretReferenceBinding(r)
	if b1 != b2 {
		t.Fatal("nil and empty set differ")
	}
	r.Operation = SecretReferenceUpdate
	r.Command, _ = f.NewCommandIdentity("project", []string{r.ProjectID.String()}, "agent.update", "update-key")
	expected := f.Version(1)
	r.ExpectedOwnerVersion = &expected
	r.ResultOwnerVersion = 1
	if r.Validate() != nil {
		t.Fatal("unchanged reference set owner no-op")
	}
	r.After = []VariableID{testID[i.ProjectVariable](1)}
	if r.Validate() == nil {
		t.Fatal("changed set without owner version")
	}
	r.ResultOwnerVersion = 2
	if r.Validate() != nil {
		t.Fatal("versioned update rejected")
	}
	clone := r.Clone()
	expected = 9
	*clone.ExpectedOwnerVersion = 3
	if *r.ExpectedOwnerVersion != 9 {
		t.Fatal("fixture assertion")
	}
	if *clone.ExpectedOwnerVersion == *r.ExpectedOwnerVersion {
		t.Fatal("expected alias")
	}
}
